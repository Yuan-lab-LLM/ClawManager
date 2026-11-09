package ownerrecords

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Optional read-only projections from the user's acceptance instances. The
// projection retains IDs, event types and text-presence, never conversation text.
func TestNativeAcceptanceProjection(t *testing.T) {
	raw := os.Getenv("OWNER_HISTORY_PROJECTION")
	if raw == "" {
		t.Skip("acceptance projection not supplied")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	var histories map[string][]json.RawMessage
	if err = json.Unmarshal(decoded, &histories); err != nil {
		t.Fatal(err)
	}
	for runtime, files := range histories {
		total := 0
		for _, file := range files {
			var lines []json.RawMessage
			if err = json.Unmarshal(file, &lines); err != nil {
				t.Fatal(err)
			}
			var data []byte
			for _, line := range lines {
				data = append(data, line...)
				data = append(data, '\n')
			}
			questions, err := ParseNativeLog(runtime, data)
			if err != nil {
				t.Fatal(runtime, err)
			}
			total += len(questions)
			recent, recentErr := parseRecentNativeLog(runtime, data)
			if recentErr != nil {
				t.Fatal(runtime, "recent projection", recentErr)
			}
			if len(questions) > 0 && len(recent) == 0 {
				t.Fatal(runtime, "recent human question evidence missing")
			}
			for _, q := range recent {
				if q.At <= 0 || q.Text == "" {
					t.Fatal(runtime, "recent question timestamp/text missing")
				}
			}
		}
		if total == 0 {
			t.Fatal(runtime, "existing completed test conversations not recognized")
		}
		t.Log(runtime, "confirmed questions", total)
	}
}

func TestOpenClawNativeAncestryAndHumanEvidence(t *testing.T) {
	data := `{"type":"session","id":"session-1"}
{"type":"message","id":"u1","message":{"role":"user","idempotencyKey":"run-1","__openclaw":{"senderIsOwner":true},"content":"same"}}
{"type":"message","id":"tool","parentId":"u1","message":{"role":"assistant","stopReason":"toolUse","content":"working"}}
{"type":"message","id":"result","parentId":"tool","message":{"role":"toolResult","content":"done"}}
{"type":"message","id":"a1","parentId":"result","timestamp":"2026-09-17T02:00:00Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}
{"type":"message","id":"u2","parentId":"a1","message":{"role":"user","idempotencyKey":"run-2","__openclaw":{"senderIsOwner":true},"content":"same"}}
{"type":"message","id":"wrong","parentId":"unknown","timestamp":"2026-09-17T02:00:00Z","message":{"role":"assistant","stopReason":"stop","content":"unrelated"}}
{"type":"message","id":"a2","parentId":"u2","timestamp":"2026-09-17T02:01:00Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}
{"type":"message","id":"auto","parentId":"a2","message":{"role":"user","__openclaw":{"senderIsOwner":false},"content":"scheduled"}}
{"type":"message","id":"a3","parentId":"auto","timestamp":"2026-09-17T02:02:00Z","message":{"role":"assistant","stopReason":"stop","content":"reply"}}`
	q, err := ParseNativeLog("openclaw", []byte(data))
	if err != nil || len(q) != 2 || q[0].ID != "run-1" || q[1].ID != "run-2" {
		t.Fatal(q, err)
	}
	if q, err := ParseNativeLog("openclaw", []byte(data+"\n{")); err == nil || q != nil {
		t.Fatal("partial record reported as complete")
	}
}
func TestDSHOnlyCompletedHumanTurns(t *testing.T) {
	data := `{"type":"session","id":"s1"}
{"type":"turn/start","data":{"turn":1}}
{"type":"user/message","data":{"id":"u1","source":{"kind":"user"},"content":[{"type":"text","text":"same"}]}}
{"type":"user/message","data":{"id":"system","source":{"kind":"plugin"},"content":[{"type":"text","text":"system"}]}}
{"type":"assistant/message","data":{"turn":1,"message":{"content":[{"type":"text","text":"reply"}]}}}
{"type":"turn/end","time":1789610460000,"data":{"turn":99,"reason":{"kind":"completed"}}}
{"type":"turn/end","time":1789610460000,"data":{"turn":1,"reason":{"kind":"completed"}}}`
	q, err := ParseNativeLog("deepseek-harness", []byte(data))
	if err != nil || len(q) != 1 || q[0].ID != "u1" || q[0].At != 1789610460 {
		t.Fatal(q, err)
	}
	q, err = ParseNativeLog("deepseek-harness", []byte(strings.ReplaceAll(data, `"completed"`, `"failed"`)))
	if err != nil || len(q) != 0 {
		t.Fatal("failed turn counted", q, err)
	}
}
func TestHistoryRefusesForeignWorkspace(t *testing.T) {
	_, err := ReadNativeHistory(context.Background(), t.TempDir(), "/other-instance", Identity{Scope: 1, Instance: 2, Runtime: "hermes"})
	if err == nil {
		t.Fatal("foreign workspace accepted")
	}
}

// Run fixed SQL against SQLite itself, not a mock. Only an in-memory fixture is
// created; no Runtime file or user database is opened by this test.
func TestNativeSQLiteQueries(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python SQLite fixture runner unavailable")
	}
	script := `import sqlite3,json,sys
p=json.load(sys.stdin);c=sqlite3.connect(':memory:');c.row_factory=sqlite3.Row
c.executescript(p['fixture'])
c.execute('PRAGMA query_only=ON')
q=p['query'].split(';',1)[1]
print(json.dumps([dict(r) for r in c.execute(q)]))`
	for _, tc := range []struct{ name, query, fixture string }{
		{"hermes", hermesQuestionSQL, `CREATE TABLE sessions(id TEXT,source TEXT,parent_session_id TEXT);CREATE TABLE messages(id INTEGER,session_id TEXT,role TEXT,content TEXT,display_kind TEXT,_compressed_summary INTEGER,active INTEGER,finish_reason TEXT,tool_calls TEXT,timestamp REAL);
 INSERT INTO sessions VALUES('s','desktop',NULL);
 INSERT INTO messages VALUES(1,'s','user','same',NULL,0,1,NULL,NULL,1),(2,'s','assistant','tool',NULL,0,1,'tool_calls','[{}]',2),(3,'s','assistant','answer',NULL,0,1,'stop',NULL,3),(4,'s','user','same',NULL,0,1,NULL,NULL,4),(5,'s','assistant','answer',NULL,0,1,'stop',NULL,5),(6,'s','user','auto','auto_continue',0,1,NULL,NULL,6),(7,'s','assistant','answer',NULL,0,1,'stop',NULL,7);`},
		{"opencode", openCodeQuestionSQL, `CREATE TABLE session(id TEXT,parent_id TEXT);CREATE TABLE message(id TEXT,session_id TEXT,data TEXT);CREATE TABLE part(message_id TEXT,data TEXT);
 INSERT INTO session VALUES('s',NULL);
 INSERT INTO message VALUES('u1','s','{"role":"user"}'),('a1','s','{"role":"assistant","parentID":"u1","finish":"stop","time":{"completed":3000}}'),('a2','s','{"role":"assistant","parentID":"u1","finish":"stop","time":{"completed":4000}}'),('u2','s','{"role":"user"}'),('a3','s','{"role":"assistant","parentID":"u2","finish":"stop","time":{"completed":5000}}'),('auto','s','{"role":"user"}'),('a4','s','{"role":"assistant","parentID":"auto","finish":"stop","time":{"completed":6000}}');
 INSERT INTO part VALUES('u1','{"type":"text","text":"same"}'),('a1','{"type":"text","text":"answer"}'),('a2','{"type":"text","text":"answer"}'),('u2','{"type":"text","text":"same"}'),('a3','{"type":"text","text":"answer"}'),('auto','{"type":"text","text":"system","synthetic":true}'),('a4','{"type":"text","text":"answer"}');`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"query": tc.query, "fixture": tc.fixture})
			cmd := exec.Command(python, "-c", script)
			cmd.Stdin = strings.NewReader(string(payload))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("SQLite fixture: %v %s", err, out)
			}
			var q []NativeQuestion
			if err = json.Unmarshal(out, &q); err != nil || len(q) != 2 || q[0].At != 3 || q[1].At != 5 {
				t.Fatal(string(out), err)
			}
		})
	}
}
