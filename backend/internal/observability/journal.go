package observability

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"clawreef/internal/backupgate"
)

var ErrOverloaded = errors.New("observability durable capacity exhausted; request was not admitted")
var ErrIdentityConflict = errors.New("observability event identity conflicts with previously accepted content")
var ErrRecoveryRequired = errors.New("request was previously admitted; execution outcome requires recovery; do not repeat upstream")

type Limits struct{ GlobalBytes, OwnerBytes, RequestBytes int64 }
type Event struct {
	UID       string          `json:"event_uid"`
	Owner     string          `json:"owner"`
	RequestID string          `json:"request_id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}
type RecoveryState struct {
	Owner      string    `json:"owner"`
	RequestID  string    `json:"request_id"`
	State      string    `json:"state"`
	Pending    int       `json:"pending"`
	AdmittedAt time.Time `json:"admitted_at"`
}
type journalFrame struct {
	Type      string    `json:"type"`
	Owner     string    `json:"owner,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	At        time.Time `json:"at,omitempty"`
	Event     *Event    `json:"event,omitempty"`
	UID       string    `json:"uid,omitempty"`
}
type journalState struct {
	RecoveryState
	events    map[string]Event
	delivered map[string]bool
	offset    int64
	size      int64
	file      string
}
type Journal struct {
	dir    string
	crypto *EncryptedObjects
	limits Limits
}

func NewJournal(dir string, key []byte, limits Limits) (*Journal, error) {
	if !filepath.IsAbs(dir) || len(key) != 32 {
		return nil, errors.New("journal requires absolute persistent directory and stable 32-byte encryption key")
	}
	if limits.RequestBytes == 0 {
		limits.RequestBytes = 64 << 20
	}
	if limits.GlobalBytes == 0 {
		limits.GlobalBytes = 2 << 30
	}
	if limits.OwnerBytes == 0 {
		limits.OwnerBytes = 512 << 20
	}
	if limits.RequestBytes < 65536 || limits.OwnerBytes < limits.RequestBytes || limits.GlobalBytes < limits.OwnerBytes {
		return nil, errors.New("invalid durable journal limits")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("journal directory must be a real directory")
	}
	j := &Journal{dir: dir, crypto: &EncryptedObjects{master: append([]byte(nil), key...)}, limits: limits}
	// Reading all existing admissions authenticates the configured key before serving.
	e = j.locked(context.Background(), func() error { _, e := j.states(); return e })
	return j, e
}
func (j *Journal) key(owner, request string) string {
	h := hmac.New(sha256.New, j.crypto.master)
	h.Write([]byte("journal-v1\x00" + owner + "\x00" + request))
	return hex.EncodeToString(h.Sum(nil)) + ".wal"
}
func (j *Journal) locked(ctx context.Context, fn func() error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(j.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (j *Journal) read(name string) (*journalState, error) {
	return j.readMode(name, false)
}
func (j *Journal) readMode(name string, headerOnly bool) (*journalState, error) {
	f, e := os.Open(filepath.Join(j.dir, name))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	s := &journalState{events: map[string]Event{}, delivered: map[string]bool{}, file: name, size: info.Size()}
	for s.offset+4 <= s.size {
		var header [4]byte
		if _, e = f.ReadAt(header[:], s.offset); e != nil {
			return nil, e
		}
		n := int64(binary.BigEndian.Uint32(header[:]))
		if n == 0 {
			break
		}
		if n < 28 || n > s.size-s.offset-4 {
			return nil, fmt.Errorf("corrupt journal frame %s at %d", name, s.offset)
		}
		encrypted := make([]byte, n)
		if _, e = f.ReadAt(encrypted, s.offset+4); e != nil {
			return nil, e
		}
		plain, e := j.crypto.open("journal-v1", encrypted, []byte(name))
		if e != nil {
			return nil, fmt.Errorf("authenticate journal %s: %w", name, e)
		}
		var frame journalFrame
		if e = json.Unmarshal(plain, &frame); e != nil {
			return nil, e
		}
		switch frame.Type {
		case "admit":
			s.Owner = frame.Owner
			s.RequestID = frame.RequestID
			s.State = "executing_or_ambiguous"
			s.AdmittedAt = frame.At
		case "event":
			if frame.Event == nil {
				return nil, errors.New("missing journal event")
			}
			s.events[frame.Event.UID] = *frame.Event
		case "delivered":
			s.delivered[frame.UID] = true
		case "terminal":
			s.State = "terminal"
		default:
			return nil, errors.New("unknown journal frame")
		}
		s.offset += 4 + n
		if headerOnly {
			break
		}
	}
	if s.Owner == "" || s.RequestID == "" {
		return nil, errors.New("journal admission missing")
	}
	for uid := range s.events {
		if !s.delivered[uid] {
			s.Pending++
		}
	}
	return s, nil
}
func (j *Journal) states() ([]*journalState, error) {
	entries, e := os.ReadDir(j.dir)
	if e != nil {
		return nil, e
	}
	out := make([]*journalState, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".admitting-") {
			if !entry.Type().IsRegular() {
				return nil, errors.New("unsafe unpublished journal entry")
			}
			if e := os.Remove(filepath.Join(j.dir, entry.Name())); e != nil {
				return nil, e
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".wal") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, errors.New("unsafe journal entry")
		}
		s, e := j.readMode(entry.Name(), true)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}
func (j *Journal) append(s *journalState, frame journalFrame, control bool) error {
	return j.appendAt(s, frame, control, s.file)
}
func (j *Journal) appendAt(s *journalState, frame journalFrame, control bool, path string) error {
	raw, e := json.Marshal(frame)
	if e != nil {
		return e
	}
	encrypted, e := j.crypto.seal("journal-v1", raw, []byte(s.file))
	if e != nil {
		return e
	}
	reserve := int64(32 << 10)
	if control {
		reserve = 0
	}
	if s.offset+8+int64(len(encrypted))+reserve > s.size {
		return ErrOverloaded
	}
	f, e := os.OpenFile(filepath.Join(j.dir, path), os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	// Persist both ciphertext and the next end marker before publishing length.
	// A shorter retry can overwrite an unpublished longer frame: its remaining
	// ciphertext must never be interpreted as the next frame's length.
	if _, e = f.WriteAt(encrypted, s.offset+4); e != nil {
		return e
	}
	var header [4]byte
	if _, e = f.WriteAt(header[:], s.offset+4+int64(len(encrypted))); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	binary.BigEndian.PutUint32(header[:], uint32(len(encrypted)))
	if _, e = f.WriteAt(header[:], s.offset); e != nil {
		return e
	}
	return f.Sync()
}
func (j *Journal) syncDir() error {
	f, e := os.Open(j.dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (j *Journal) Admit(ctx context.Context, owner, request string) error {
	if owner == "" || request == "" {
		return errors.New("admission owner and request identity required")
	}
	release, e := backupgate.HoldWorkUnit(ctx, "observability.admit")
	if e != nil {
		return e
	}
	defer release()
	return j.locked(ctx, func() error {
		states, e := j.states()
		if e != nil {
			return e
		}
		var total, owned int64
		for _, s := range states {
			total += s.size
			if s.Owner == owner {
				owned += s.size
			}
			if s.Owner == owner && s.RequestID == request {
				return ErrRecoveryRequired
			}
		}
		if total+j.limits.RequestBytes > j.limits.GlobalBytes || owned+j.limits.RequestBytes > j.limits.OwnerBytes {
			return ErrOverloaded
		}
		name := j.key(owner, request)
		tmp, e := os.CreateTemp(j.dir, ".admitting-")
		if e != nil {
			return e
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		// Actually allocate terminal capacity before external execution. Truncate alone
		// would create a sparse promise that can fail when the accepted call completes.
		if e = preallocateJournal(tmp, j.limits.RequestBytes); e != nil {
			tmp.Close()
			return e
		}
		if e = tmp.Sync(); e != nil {
			tmp.Close()
			return e
		}
		if e = tmp.Close(); e != nil {
			return e
		}
		s := &journalState{file: name, size: j.limits.RequestBytes}
		if e = j.appendAt(s, journalFrame{Type: "admit", Owner: owner, RequestID: request, At: time.Now().UTC()}, true, filepath.Base(tmpName)); e != nil {
			return e
		}
		if e = os.Rename(tmpName, filepath.Join(j.dir, name)); e != nil {
			return e
		}
		return j.syncDir()
	})
}
func (j *Journal) Append(ctx context.Context, event Event) error {
	if event.UID == "" || event.Kind == "" || !json.Valid(event.Payload) {
		return errors.New("invalid durable event")
	}
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(event.Owner, event.RequestID))
		if e != nil {
			return e
		}
		if prior, ok := s.events[event.UID]; ok {
			if prior.Kind != event.Kind || !sameEventPayload(prior.Payload, event.Payload) {
				return ErrIdentityConflict
			}
			return nil
		}
		if len(s.events) >= 128 {
			return ErrOverloaded
		}
		if s.State == "terminal" {
			return errors.New("admission already terminal")
		}
		return j.append(s, journalFrame{Type: "event", Event: &event}, false)
	})
}
func (j *Journal) Finish(ctx context.Context, owner, request string) error {
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(owner, request))
		if e != nil {
			return e
		}
		if s.State == "terminal" {
			return nil
		}
		return j.append(s, journalFrame{Type: "terminal"}, true)
	})
}
func (j *Journal) Delivered(ctx context.Context, event Event) error {
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(event.Owner, event.RequestID))
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if s.delivered[event.UID] {
			return nil
		}
		return j.append(s, journalFrame{Type: "delivered", UID: event.UID}, true)
	})
}
func (j *Journal) Recovery(ctx context.Context) ([]RecoveryState, error) {
	var out []RecoveryState
	e := j.locked(ctx, func() error {
		states, e := j.states()
		if e != nil {
			return e
		}
		for _, s := range states {
			current, e := j.read(s.file)
			if e != nil {
				return e
			}
			out = append(out, current.RecoveryState)
		}
		return nil
	})
	return out, e
}
func (j *Journal) Replay(ctx context.Context, limit int, sink func(context.Context, Event) error) (int, error) {
	return j.ReplayWithTerminal(ctx, limit, sink, nil)
}
func (j *Journal) ReplayWithTerminal(ctx context.Context, limit int, sink func(context.Context, Event) error, terminal func(context.Context, RecoveryState) error) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	release, e := backupgate.HoldWorkUnit(ctx, "observability.replay")
	if e != nil {
		return 0, e
	}
	defer release()
	var states []*journalState
	if e = j.locked(ctx, func() error { var err error; states, err = j.states(); return err }); e != nil {
		return 0, e
	}
	count := 0
	for _, s := range states {
		if e = j.locked(ctx, func() error { var err error; s, err = j.read(s.file); return err }); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return count, e
		}
		uids := make([]string, 0, len(s.events))
		for uid := range s.events {
			uids = append(uids, uid)
		}
		sort.Slice(uids, func(i, k int) bool {
			a, b := s.events[uids[i]], s.events[uids[k]]
			if a.Kind == "model_invocations" && b.Kind != "model_invocations" {
				return true
			}
			if b.Kind == "model_invocations" && a.Kind != "model_invocations" {
				return false
			}
			return uids[i] < uids[k]
		})
		for _, uid := range uids {
			if count >= limit {
				return count, nil
			}
			if s.delivered[uid] {
				continue
			}
			event := s.events[uid]
			if e = sink(ctx, event); e != nil {
				return count, e
			}
			if e = j.Delivered(ctx, event); e != nil {
				return count, e
			}
			count++
		}
		if e = j.locked(ctx, func() error {
			current, e := j.read(s.file)
			if errors.Is(e, os.ErrNotExist) {
				return nil
			}
			if e != nil {
				return e
			}
			if current.State != "terminal" || current.Pending != 0 {
				return nil
			}
			if terminal != nil {
				if e = terminal(ctx, current.RecoveryState); e != nil {
					return e
				}
			}
			if e = os.Remove(filepath.Join(j.dir, s.file)); e != nil {
				return e
			}
			return j.syncDir()
		}); e != nil {
			return count, e
		}
	}
	return count, nil
}

// Snapshot returns authenticated journal files for an already-held backup gate.
// The caller must include the stable key in the separately encrypted secret backup.
func (j *Journal) Snapshot(ctx context.Context) (map[string][]byte, error) {
	out := map[string][]byte{}
	e := j.locked(ctx, func() error {
		states, e := j.states()
		if e != nil {
			return e
		}
		total := int64(0)
		for _, s := range states {
			s, e = j.read(s.file)
			if e != nil {
				return e
			}
			total += s.offset
			if total > 64<<20 {
				return errors.New("journal snapshot exceeds 64 MiB; use streaming filesystem backup")
			}
			f, e := os.Open(filepath.Join(j.dir, s.file))
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(f, s.offset))
			f.Close()
			if e != nil {
				return e
			}
			out[s.file] = b
		}
		return nil
	})
	return out, e
}

func sameEventPayload(a, b json.RawMessage) bool {
	var left, right map[string]json.RawMessage
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return bytes.Equal(a, b)
	}
	for _, field := range []string{"id", "created_at", "recorded_at", "completed_at"} {
		delete(left, field)
		delete(right, field)
	}
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return bytes.Equal(x, y)
}

func writeJournalReservation(file *os.File, size int64) error {
	chunk := make([]byte, 64<<10)
	for remaining := size; remaining > 0; {
		n := int64(len(chunk))
		if n > remaining {
			n = remaining
		}
		if _, e := file.Write(chunk[:n]); e != nil {
			return e
		}
		remaining -= n
	}
	return nil
}

// CheckCompletionReserve runs immediately before each external attempt, leaving
// a known envelope and event count for its terminal records. It never consumes
// the separate control-frame reserve used to seal and acknowledge the journal.
func (j *Journal) CheckCompletionReserve(ctx context.Context, owner, request string, bytes int64, events int) error {
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(owner, request))
		if e != nil {
			return e
		}
		if s.State == "terminal" {
			return ErrRecoveryRequired
		}
		if s.size-s.offset-(32<<10) < bytes || 128-len(s.events) < events {
			return errors.New("completion capacity unavailable; no further provider attempt started; prior execution state retained")
		}
		f, e := os.OpenFile(filepath.Join(j.dir, s.file), os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		return f.Sync()
	})
}

// EventDigest excludes transport-assigned IDs/timestamps, retaining all business
// values. SQL receipts use this identity after the original history row expires.
func EventDigest(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil {
		for _, field := range []string{"id", "created_at", "recorded_at", "completed_at"} {
			delete(fields, field)
		}
		raw, _ = json.Marshal(fields)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
