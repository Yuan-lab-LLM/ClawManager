package ownerrecords

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"
)

// A reader which cannot be reaped must not be replaced after an App restart.
// This fence never expires on a timer: a timeout is not proof of process exit.
// It fences only native filesystem reads, never SQL aggregation or serving.
type readerGuard struct {
	store      *Store
	key, token string
}

var readerGuardRetries = struct {
	sync.Mutex
	guards map[string]*readerGuard
}{guards: map[string]*readerGuard{}}

func readerNodeIdentity() string {
	if node := os.Getenv("NODE_NAME"); node != "" {
		return node
	}
	if boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		return strings.TrimSpace(string(boot))
	}
	host, _ := os.Hostname()
	return host
}

func (s *Store) acquireReaderGuard(ctx context.Context) (*readerGuard, error) {
	if s.Session == nil {
		return nil, nil
	} // pure reader tests have no database
	key := Key("native-reader", s.HistoryRoot, readerNodeIdentity())
	readerGuardRetries.Lock()
	previous := readerGuardRetries.guards[key]
	readerGuardRetries.Unlock()
	if previous != nil {
		previous.release()
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	g := &readerGuard{store: s, key: key, token: hex.EncodeToString(token)}
	_, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,payload) VALUES('native_reader_guard',?,0,'idle',JSON_OBJECT()) ON DUPLICATE KEY UPDATE record_key=record_key`, key)
	if err != nil {
		return nil, err
	}
	result, err := s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='updating',source_key=?,occurred_at=UTC_TIMESTAMP(),payload=JSON_OBJECT('reason','reader_busy') WHERE record_type='native_reader_guard' AND record_key=? AND status='idle'`, g.token, key)
	if err != nil {
		g.release() // no process was started, even if the SQL acknowledgement was lost
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		g.release()
		return nil, err
	}
	if n != 1 {
		return nil, errReaderBusy
	}
	return g, nil
}

// Called only after Start failed or Wait proved that the process was reaped.
func (g *readerGuard) release() {
	if g == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncMetadataBudget)
	defer cancel()
	_, err := g.store.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='idle',payload=JSON_OBJECT() WHERE record_type='native_reader_guard' AND record_key=? AND source_key=? AND status='updating'`, g.key, g.token)
	readerGuardRetries.Lock()
	defer readerGuardRetries.Unlock()
	if err != nil {
		readerGuardRetries.guards[g.key] = g
	} else {
		delete(readerGuardRetries.guards, g.key)
	}
}
