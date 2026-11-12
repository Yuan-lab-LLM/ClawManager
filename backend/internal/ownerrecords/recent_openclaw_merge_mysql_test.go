package ownerrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regress the merge boundary: recent reads must load acknowledged 10.2 cold
// progress before crossing into the isolated reader, under the new budgets.
func TestSyncResilienceMySQLRecentReusesOpenClawColdAcknowledgement(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("real SQLite and dedicated MySQL required; run in Linux fixture")
	}
	s := resilienceStore(t)
	s.HistoryRoot = t.TempDir()
	workspace := filepath.Join(s.HistoryRoot, "openclaw", "user-99", "instance-17")
	agent := filepath.Join(workspace, "home", ".openclaw", "agents", "main")
	if err := os.MkdirAll(filepath.Join(agent, "agent"), 0700); err != nil {
		t.Fatal(err)
	}
	i := Identity{Scope: 99, Instance: 17, Owner: "merge@example.test", Runtime: "openclaw", Mode: "lite"}
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(17,99,?,'openclaw','lite',?,'fixture-token')`, i.Owner, workspace)
	item := scanItem{identity: i, workspace: workspace}
	row, err := s.Session.SQL().QueryRow(`SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=17`)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&item.generation); err != nil {
		t.Fatal(err)
	}
	work, err := s.captureScanRuntime(context.Background(), &item)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := openClaw102TestSnapshot(t, true)
	a := snapshot.Cold[0]
	// The immutable metadata matches already-committed history. An invalid blob
	// makes this fail if the merge accidentally drops acknowledgement loading.
	schema := `CREATE TABLE session_windows(session_id TEXT PRIMARY KEY,updated_at INTEGER);
CREATE TABLE transcript_events(session_id TEXT,seq INTEGER,event_json TEXT,event_zstd BLOB,event_utf8_bytes INTEGER,PRIMARY KEY(session_id,seq));
CREATE TABLE session_transcript_cold_archives(session_id TEXT,generation TEXT,archive_name TEXT,archive_sha256 TEXT,event_count INTEGER,raw_bytes INTEGER,archive_bytes INTEGER,last_seq INTEGER,storage TEXT,archive_blob BLOB);
INSERT INTO session_windows VALUES('s1',1792026001000);`
	schema += fmt.Sprintf("INSERT INTO session_transcript_cold_archives VALUES('s1','g1','%s','%s',%d,%d,%d,%d,'sqlite',X'00');", a.Name, a.SHA256, a.Events, a.RawBytes, a.ArchiveBytes, a.LastSeq)
	seed := exec.Command("sqlite3", filepath.Join(agent, "agent", "openclaw-agent.sqlite"))
	seed.Stdin = strings.NewReader(schema)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	key := Key("oc102-cold-v1", agent, a.Session, a.Generation, a.SHA256, a.Storage)
	ack := openClawColdAcknowledgement{Latest: NativeQuestion{ID: "original-question", Session: "s1", Text: "Retained user question", At: float64(time.Now().Unix())}, AgentKey: Key(agent), Session: a.Session, Generation: a.Generation, SHA256: a.SHA256}
	if err := s.saveOpenClawColdProgress(work, item, &openClawColdProgress{confirmed: map[string]openClawColdAcknowledgement{key: ack}}); err != nil {
		t.Fatal(err)
	}
	if err := s.cacheRecentQuestion(work, i, workspace); err != nil {
		t.Fatal("recent read lost cold history acknowledgement", err)
	}
	// Exercise the actual scheduled worker too, rather than only its legacy
	// direct helper. A recent read must never acknowledge unseen full facts.
	resilienceExec(t, s, `DELETE FROM northbound_owner_records WHERE record_type='recent_question'`)
	ctx, cancel := context.WithTimeout(context.Background(), nativeScanBudget)
	defer cancel()
	if _, err := s.scanRecentQuestions(ctx, 0, map[uint64]string{}); err != nil {
		t.Fatal("scheduled recent scan failed", err)
	}
	row, err = s.Session.SQL().QueryRow(`SELECT payload FROM northbound_owner_records WHERE record_type='recent_question' AND instance_id=17`)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var recent RecentQuestion
	if err := json.Unmarshal(raw, &recent); err != nil || recent.Text != ack.Latest.Text {
		t.Fatal("retained recent question missing", err)
	}
}
