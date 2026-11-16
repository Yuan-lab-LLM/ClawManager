package ownerrecords

import (
	"context"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == HistoryReaderArgument {
		os.Exit(RunHistoryReaderCLI(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

func TestIsolatedDSHInspectionReusesBoundFacts(t *testing.T) {
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 1, Owner: "fixture@example.test", Runtime: "deepseek-harness", Mode: "lite"}
	ws := filepath.Join(root, "deepseek-harness/user-1/instance-1")
	dir := filepath.Join(ws, "home/.dsh/sessions/fixture")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	data := `{"type":"session","id":"s"}
{"type":"turn/start","data":{"turn":1}}
{"type":"user/message","time":1789610460000,"data":{"id":"u","source":{"kind":"user"},"content":"hello"}}
{"type":"assistant/message","data":{"turn":1,"message":{"content":"ok"}}}
{"type":"turn/end","time":1789610470000,"data":{"turn":1,"reason":{"kind":"completed"}}}
`
	encoder, e := zstd.NewWriter(nil)
	if e != nil {
		t.Fatal(e)
	}
	defer encoder.Close()
	if e = os.WriteFile(filepath.Join(dir, "session.jsonl.zstd"), encoder.EncodeAll([]byte(data), nil), 0600); e != nil {
		t.Fatal(e)
	}
	s := &Store{HistoryRoot: root}
	ctx := withDSHBatch(context.Background())
	item := scanItem{identity: i, workspace: ws, generation: "generation-one"}
	recent, e := s.inspectNative(ctx, item, true, time.Time{}, "")
	if e != nil || len(recent.Questions) != 1 || len(recent.Facts) != 1 || len(recent.Confirmed) != 1 {
		t.Fatal("combined projection lost facts", e, recent)
	}
	nativeReaderSlots <- struct{}{}
	facts, e := s.inspectNative(ctx, item, false, time.Time{}, "")
	item.generation = "generation-two"
	_, changedErr := s.inspectNative(ctx, item, false, time.Time{}, "")
	<-nativeReaderSlots
	if e != nil || len(facts.Questions) != 1 {
		t.Fatal("same batch decompressed again", facts, e)
	}
	if changedErr != errReaderBusy {
		t.Fatal("cross-generation result reuse", changedErr)
	}
}

func TestIsolatedReaderValidatesPathAndEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 2, Owner: "fixture@example.test", Runtime: "openclaw", Mode: "lite"}
	ws := filepath.Join(root, "openclaw", "user-1", "instance-2")
	if e := os.MkdirAll(filepath.Join(ws, "home"), 0700); e != nil {
		t.Fatal(e)
	}
	s := &Store{HistoryRoot: root}
	r, e := s.isolatedHistory(context.Background(), historyReadRequest{Operation: "empty", Root: root, Workspace: ws, Identity: i})
	if e != nil || !r.Empty {
		t.Fatal("accessible empty source must be normal", r, e)
	}
	r, e = s.isolatedHistory(context.Background(), historyReadRequest{Operation: "empty", Root: root, Workspace: root, Identity: i})
	if e == nil || r.Empty {
		t.Fatal("foreign path accepted")
	}
	var out strings.Builder
	if RunHistoryReaderCLI(strings.NewReader(`{"Operation":"unexpected"}`), &out) == 0 {
		t.Fatal("unknown operation accepted")
	}
}

func TestIsolatedReaderBusyCannotAccumulateProcesses(t *testing.T) {
	nativeReaderSlots <- struct{}{}
	defer func() { <-nativeReaderSlots }()
	s := &Store{}
	for n := 0; n < 100; n++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, e := s.isolatedHistory(ctx, historyReadRequest{})
		cancel()
		if e != errReaderBusy {
			t.Fatal("unreaped reader slot was ignored", e)
		}
	}
}

func TestIsolatedOpenClawColdProgressSurvivesProcessBoundary(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("real SQLite required; execute in the pinned Linux acceptance image")
	}
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 17, Owner: "fixture@example.test", Runtime: "openclaw", Mode: "lite"}
	ws := filepath.Join(root, "openclaw", "user-1", "instance-17")
	db := filepath.Join(ws, "home/.openclaw/agents/main/agent/openclaw-agent.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0700); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := openClaw102TestSnapshot(t, true)
	a := snapshot.Cold[0]
	sql := `CREATE TABLE session_windows(session_id TEXT PRIMARY KEY,updated_at INTEGER);
CREATE TABLE transcript_events(session_id TEXT,seq INTEGER,event_json TEXT,event_zstd BLOB,event_utf8_bytes INTEGER,PRIMARY KEY(session_id,seq));
CREATE TABLE session_transcript_cold_archives(session_id TEXT,generation TEXT,archive_name TEXT,archive_sha256 TEXT,event_count INTEGER,raw_bytes INTEGER,archive_bytes INTEGER,last_seq INTEGER,storage TEXT,archive_blob BLOB);
INSERT INTO session_windows VALUES('s1',1792026001000);`
	sql += fmt.Sprintf("INSERT INTO session_transcript_cold_archives VALUES('s1','g1','%s','%s',%d,%d,%d,%d,'sqlite',X'%s');", a.Name, a.SHA256, a.Events, a.RawBytes, a.ArchiveBytes, a.LastSeq, a.Blob)
	seed := exec.Command("sqlite3", db)
	seed.Stdin = strings.NewReader(sql)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	p := &openClawColdProgress{previous: map[string]openClawColdAcknowledgement{}, confirmed: map[string]openClawColdAcknowledgement{}}
	ctx := context.WithValue(context.Background(), openClawHistoryProgressKey{}, p)
	s := &Store{HistoryRoot: root}
	facts, err := s.isolatedNativeRead(ctx, ws, i, false, time.Time{})
	if err != nil || len(facts) != 1 || len(p.confirmed) != 1 || len(p.previous) != 0 {
		t.Fatalf("child lost unacknowledged cold progress: facts=%d confirmed=%d err=%v", len(facts), len(p.confirmed), err)
	}
	// Simulate the caller acknowledging only after every fact committed. A later
	// recent read must reuse that exact archive acknowledgement and its excerpt,
	// without decompressing the deliberately invalid replacement blob.
	p.previous, p.confirmed = p.confirmed, map[string]openClawColdAcknowledgement{}
	if out, err := exec.Command("sqlite3", db, "UPDATE session_transcript_cold_archives SET archive_blob=X'00';").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	recent, err := s.isolatedNativeRead(ctx, ws, i, true, time.Time{})
	if err != nil || len(recent) != 1 || recent[0].ID != facts[0].ID || len(p.confirmed) != 1 {
		t.Fatalf("parent acknowledgement did not reach child: facts=%d err=%v", len(recent), err)
	}
}
