package ownerrecords

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Real SQLite fixture, not a mocked SELECT. The fixture is generated only in
// t.TempDir and contains no Runtime/user data or external model requests.
func TestOpenClaw102LargeSQLiteSnapshotWithinReadBudget(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("real sqlite3 fixture requires the acceptance image")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw", "user-1", "instance-17")
	db := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "agent", "openclaw-agent.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `CREATE TABLE session_windows(session_id TEXT PRIMARY KEY,updated_at INTEGER); CREATE TABLE transcript_events(session_id TEXT,seq INTEGER,event_json TEXT,event_zstd BLOB,event_utf8_bytes INTEGER,PRIMARY KEY(session_id,seq)); CREATE TABLE session_transcript_cold_archives(session_id TEXT,generation TEXT,archive_name TEXT,archive_sha256 TEXT,event_count INTEGER,raw_bytes INTEGER,archive_bytes INTEGER,last_seq INTEGER,storage TEXT,archive_blob BLOB); INSERT INTO session_windows VALUES('s1',1792026001000);`
	// A realistic multi-megabyte snapshot: bounded individual events, many
	// records. SQLite's -json wrapper must not re-escape the complete JSON cell.
	for n, event := range openClaw102TestEvents() {
		fixture += "INSERT INTO transcript_events VALUES('s1'," + strconv.Itoa(n) + ",'" + strings.ReplaceAll(event, "'", "''") + "',NULL,NULL);"
	}
	fixture += `WITH RECURSIVE n(i) AS (SELECT 100 UNION ALL SELECT i+1 FROM n WHERE i<576) INSERT INTO transcript_events SELECT 's1',i,json_object('type','diagnostic','padding',hex(zeroblob(2048))),NULL,NULL FROM n;`
	seed := exec.Command("sqlite3", db)
	seed.Stdin = strings.NewReader(fixture)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := ReadNativeHistory(ctx, root, workspace, Identity{Scope: 1, Instance: 17, Runtime: "openclaw"})
	if err != nil || len(got) != 1 || got[0].ID != "same-run-before-migration" {
		t.Fatalf("large native snapshot must fit the existing read budget: questions=%d err=%v", len(got), err)
	}
	after, err := os.ReadFile(db)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read changed native database", err)
	}
}

func TestOpenClaw102SQLiteMustNotDisappearWhenJSONLRetired(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("real sqlite3 fixture requires the acceptance image")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw", "user-1", "instance-17")
	db := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "agent", "openclaw-agent.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0700); err != nil {
		t.Fatal(err)
	}
	events := []string{
		`{"type":"session","id":"retained-session"}`,
		`{"type":"message","id":"u1","timestamp":"2026-10-15T01:00:00Z","message":{"role":"user","idempotencyKey":"retained-run","__openclaw":{"senderIsOwner":true},"content":"fixture"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-10-15T01:00:01Z","message":{"role":"assistant","stopReason":"stop","content":"answer"}}`,
	}
	fixture := `CREATE TABLE session_windows(session_id TEXT PRIMARY KEY,updated_at INTEGER); CREATE TABLE transcript_events(session_id TEXT,seq INTEGER,event_json TEXT,event_zstd BLOB,event_utf8_bytes INTEGER,PRIMARY KEY(session_id,seq)); CREATE TABLE session_transcript_cold_archives(session_id TEXT,generation TEXT,archive_name TEXT,archive_sha256 TEXT,event_count INTEGER,raw_bytes INTEGER,archive_bytes INTEGER,last_seq INTEGER,storage TEXT,archive_blob BLOB); INSERT INTO session_windows VALUES('retained-session',1792026001000);`
	for i, e := range events {
		encoded, _ := json.Marshal(e)
		// SQLite json_extract safely decodes the fixture string, without an
		// SQL quoting convention that differs from production event payloads.
		fixture += "INSERT INTO transcript_events VALUES('retained-session'," + strconv.Itoa(i) + ",json_extract('" + strings.ReplaceAll(string(encoded), "'", "''") + "','$'),NULL,NULL);"
	}
	cmd := exec.Command("sqlite3", db, fixture)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	// Retired JSONL cannot override, duplicate or poison the canonical store.
	legacy := filepath.Join(filepath.Dir(filepath.Dir(db)), "sessions")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "retired.jsonl"), []byte("invalid-retired-content"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNativeHistory(context.Background(), root, workspace, Identity{Scope: 1, Instance: 17, Runtime: "openclaw"})
	if err != nil || len(got) != 1 || got[0].Session != "retained-session" || got[0].ID != "retained-run" {
		t.Fatalf("authoritative SQLite history disappeared: %+v %v", got, err)
	}
}

func TestOpenClaw102SQLiteReadsCommittedWALWithoutIgnoringOrChangingIt(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("acceptance image sqlite3 required")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw", "user-1", "instance-17")
	db := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "agent", "openclaw-agent.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0700); err != nil {
		t.Fatal(err)
	}
	seed := exec.Command("sqlite3", db)
	input, err := seed.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := seed.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); seed.Wait() }()
	dDL := `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;
CREATE TABLE session_windows(session_id TEXT PRIMARY KEY,updated_at INTEGER);
CREATE TABLE transcript_events(session_id TEXT,seq INTEGER,event_json TEXT,event_zstd BLOB,event_utf8_bytes INTEGER,PRIMARY KEY(session_id,seq));
CREATE TABLE session_transcript_cold_archives(session_id TEXT,generation TEXT,archive_name TEXT,archive_sha256 TEXT,event_count INTEGER,raw_bytes INTEGER,archive_bytes INTEGER,last_seq INTEGER,storage TEXT,archive_blob BLOB);
INSERT INTO session_windows VALUES('s1',1792026001000);
`
	z, _ := zstd.NewWriter(nil)
	for n, e := range openClaw102TestEvents() {
		dDL += "INSERT INTO transcript_events VALUES('s1'," + strconv.Itoa(n) + ",NULL,X'" + hex.EncodeToString(z.EncodeAll([]byte(e), nil)) + "'," + strconv.Itoa(len(e)) + ");\n"
	}
	z.Close()
	if _, err := input.Write([]byte(dDL + "SELECT 'fixture-ready';\n")); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(output)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "fixture-ready" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("SQLite writer fixture did not become ready")
	}
	wal, err := os.ReadFile(db + "-wal")
	if err != nil || len(wal) == 0 {
		t.Fatal("test did not create real WAL", err)
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadNativeHistory(context.Background(), root, workspace, Identity{Scope: 1, Instance: 17, Runtime: "openclaw"})
	if network, _ := openClawSQLiteNetworkFilesystem(db); network {
		if err == nil || got != nil {
			t.Fatal("cross-host WAL history was accepted on network storage")
		}
		after, _ := os.ReadFile(db)
		afterWAL, _ := os.ReadFile(db + "-wal")
		if !bytes.Equal(before, after) || !bytes.Equal(wal, afterWAL) {
			t.Fatal("network WAL refusal changed original content")
		}
		return
	}
	if err != nil || len(got) != 1 || got[0].ID != "same-run-before-migration" {
		t.Fatal("committed WAL was ignored", got, err)
	}
	after, _ := os.ReadFile(db)
	afterWAL, _ := os.ReadFile(db + "-wal")
	if !bytes.Equal(before, after) || !bytes.Equal(wal, afterWAL) {
		t.Fatal("statistics changed DB/WAL content")
	}
}

func TestOpenClaw102AcknowledgesColdOnlyAfterCallerCommitsFacts(t *testing.T) {
	p := &openClawColdProgress{previous: map[string]openClawColdAcknowledgement{}, confirmed: map[string]openClawColdAcknowledgement{}}
	ctx := context.WithValue(context.Background(), openClawHistoryProgressKey{}, p)
	s, _ := openClaw102TestSnapshot(t, true)
	budget := int64(openClaw102ReadBytes)
	got, err := decodeOpenClaw102Snapshot(ctx, t.TempDir(), "/owned-agent", s, false, time.Time{}, &budget)
	if err != nil || len(got) != 1 || len(p.confirmed) != 1 || len(p.previous) != 0 {
		t.Fatal("acknowledged before Apply committed", got, err)
	}
	// Caller commits every fact, then durably saves these identifiers/excerpt.
	p.previous = p.confirmed
	p.confirmed = map[string]openClawColdAcknowledgement{}
	s.Cold[0].Blob = "" // query must avoid materializing an acknowledged blob
	budget = 1
	got, err = decodeOpenClaw102Snapshot(ctx, t.TempDir(), "/owned-agent", s, false, time.Time{}, &budget)
	if err != nil || len(got) != 0 || budget != 1 {
		t.Fatal("unchanged cold history was decompressed again", got, err)
	}
	got, err = decodeOpenClaw102Snapshot(ctx, t.TempDir(), "/owned-agent", s, true, time.Time{}, &budget)
	if err != nil || len(got) != 1 || strings.Contains(got[0].Text, "fixture-secret") {
		t.Fatal("latest excerpt not retained", got, err)
	}
	s.Cold[0].Generation = "new-generation"
	if _, err := decodeOpenClaw102Snapshot(ctx, t.TempDir(), "/owned-agent", s, false, time.Time{}, &budget); err == nil {
		t.Fatal("changed generation reused old acknowledgement")
	}
	if _, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "/owned-agent", s, false, time.Time{}, &budget); err == nil {
		t.Fatal("new instance/owner context reused acknowledgement")
	}
}

func openClaw102TestEvents() []string {
	return []string{
		`{"type":"session","id":"s1"}`,
		`{"type":"message","id":"u1","timestamp":"2026-10-15T01:00:00Z","message":{"role":"user","idempotencyKey":"same-run-before-migration","__openclaw":{"senderIsOwner":true},"content":"password=fixture-secret-do-not-show"}}`,
		`{"type":"message","id":"tool","parentId":"u1","message":{"role":"assistant","stopReason":"toolUse","content":"working"}}`,
		`{"type":"message","id":"a1","parentId":"tool","timestamp":"2026-10-15T01:00:01Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}`,
		`{"type":"message","id":"auto","parentId":"a1","timestamp":"2026-10-15T01:00:02Z","message":{"role":"user","__openclaw":{"senderIsOwner":false},"content":"scheduled"}}`,
		`{"type":"message","id":"a2","parentId":"auto","timestamp":"2026-10-15T01:00:03Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}`,
	}
}

func openClaw102TestSnapshot(t *testing.T, cold bool) (openClaw102Snapshot, []byte) {
	t.Helper()
	s := openClaw102Snapshot{Sessions: []string{"s1"}}
	events := openClaw102TestEvents()
	z, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if !cold {
		for n, e := range events {
			row := openClaw102Event{Session: "s1", Seq: int64(n)}
			if n%2 == 0 {
				row.Plain = &events[n]
			} else {
				row.Compressed = hex.EncodeToString(z.EncodeAll([]byte(e), nil))
				row.Bytes = int64(len(e))
			}
			s.Hot = append(s.Hot, row)
		}
		return s, nil
	}
	var envelope bytes.Buffer
	header, _ := json.Marshal(map[string]any{"kind": "header", "version": 1, "sessionId": "s1", "generation": "g1"})
	envelope.Write(header)
	envelope.WriteByte('\n')
	var rawBytes int64
	for n, e := range events {
		raw, _ := json.Marshal(map[string]any{"kind": "event", "row": map[string]any{"seq": n, "event_json": e, "created_at": 1792026000000 + n}})
		envelope.Write(raw)
		envelope.WriteByte('\n')
		rawBytes += int64(len(e))
		if n > 0 {
			rawBytes++
		}
	}
	compressed := z.EncodeAll(envelope.Bytes(), nil)
	hash := sha256.Sum256(compressed)
	sha := hex.EncodeToString(hash[:])
	s.Cold = []openClaw102Cold{{Session: "s1", Generation: "g1", SHA256: sha, Name: sha + ".jsonl.zst", Events: len(events), RawBytes: rawBytes, ArchiveBytes: int64(len(compressed)), LastSeq: int64(len(events) - 1), Storage: "sqlite", Blob: hex.EncodeToString(compressed)}}
	return s, compressed
}

func TestOpenClaw102HotAndColdKeepLegacyMessageIdentity(t *testing.T) {
	legacy, _ := ParseNativeLog("openclaw", []byte(strings.Join(openClaw102TestEvents(), "\n")+"\n"))
	if len(legacy) != 1 {
		t.Fatal(legacy)
	}
	for _, cold := range []bool{false, true} {
		s, _ := openClaw102TestSnapshot(t, cold)
		budget := int64(openClaw102ReadBytes)
		got, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, false, time.Time{}, &budget)
		if err != nil || !reflect.DeepEqual(got, legacy) {
			t.Fatalf("identity changed cold=%v: %+v %v", cold, got, err)
		}
		budget = openClaw102ReadBytes
		recent, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, true, time.Time{}, &budget)
		if err != nil || len(recent) != 1 || strings.Contains(recent[0].Text, "fixture-secret") || recent[0].ID != legacy[0].ID {
			t.Fatal("unsafe/lost recent projection", recent, err)
		}
		budget = openClaw102ReadBytes
		got, err = decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, false, time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), &budget)
		if err != nil || len(got) != 0 {
			t.Fatal("pre-T0 question counted", got, err)
		}
	}
}

func TestOpenClaw102ColdFilesAreVerifiedWithoutWarmingLiveStore(t *testing.T) {
	root := t.TempDir()
	agent := filepath.Join(root, "main")
	s, compressed := openClaw102TestSnapshot(t, true)
	dir := filepath.Join(agent, "sessions", "cold")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, s.Cold[0].Name)
	if err := os.WriteFile(p, compressed, 0600); err != nil {
		t.Fatal(err)
	}
	s.Cold[0].Storage = "file"
	s.Cold[0].Blob = ""
	before, _ := os.Stat(p)
	budget := int64(openClaw102ReadBytes)
	got, err := decodeOpenClaw102Snapshot(context.Background(), root, agent, s, false, time.Time{}, &budget)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("cold archive was modified")
	}
	if _, err := os.Stat(filepath.Join(agent, "agent")); !os.IsNotExist(err) {
		t.Fatal("live database was warmed/created")
	}
	if err := os.WriteFile(p, append(compressed, 0), 0600); err != nil {
		t.Fatal(err)
	}
	budget = openClaw102ReadBytes
	if got, err := decodeOpenClaw102Snapshot(context.Background(), root, agent, s, false, time.Time{}, &budget); err == nil || got != nil {
		t.Fatal("corrupt cold transcript silently counted")
	}
}

func TestOpenClaw102RejectsIncompleteUnknownAndUnboundedHistory(t *testing.T) {
	for _, mutate := range []func(*openClaw102Snapshot){
		func(s *openClaw102Snapshot) { s.Hot = s.Hot[1:] },
		func(s *openClaw102Snapshot) { s.Hot[1].Compressed = "00" },
		func(s *openClaw102Snapshot) { s.Hot[1].Bytes++ },
		func(s *openClaw102Snapshot) { s.Hot[1].Seq = s.Hot[0].Seq },
		func(s *openClaw102Snapshot) { s.Sessions = nil },
		func(s *openClaw102Snapshot) { s.Hot[0].Oversized = true },
	} {
		s, _ := openClaw102TestSnapshot(t, false)
		mutate(&s)
		budget := int64(openClaw102ReadBytes)
		if got, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, false, time.Time{}, &budget); err == nil || got != nil {
			t.Fatal("incomplete input acknowledged")
		}
	}
	for _, mutate := range []func(*openClaw102Snapshot){
		func(s *openClaw102Snapshot) { s.Cold[0].Generation = "changed" },
		func(s *openClaw102Snapshot) { s.Cold[0].Events++ },
		func(s *openClaw102Snapshot) { s.Cold[0].RawBytes++ },
		func(s *openClaw102Snapshot) { s.Cold[0].LastSeq++ },
		func(s *openClaw102Snapshot) {
			s.Cold[0].SHA256 = strings.Repeat("/", 64)
			s.Cold[0].Name = s.Cold[0].SHA256 + ".jsonl.zst"
			s.Cold[0].Storage = "file"
			s.Cold[0].Blob = ""
		},
	} {
		s, _ := openClaw102TestSnapshot(t, true)
		mutate(&s)
		budget := int64(openClaw102ReadBytes)
		if got, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, false, time.Time{}, &budget); err == nil || got != nil {
			t.Fatal("invalid cold input acknowledged")
		}
	}
	s, _ := openClaw102TestSnapshot(t, false)
	budget := int64(8)
	if _, err := decodeOpenClaw102Snapshot(context.Background(), t.TempDir(), "", s, false, time.Time{}, &budget); err == nil {
		t.Fatal("aggregate budget bypassed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget = openClaw102ReadBytes
	if _, err := decodeOpenClaw102Snapshot(ctx, t.TempDir(), "", s, false, time.Time{}, &budget); err == nil {
		t.Fatal("canceled scan acknowledged")
	}
}
