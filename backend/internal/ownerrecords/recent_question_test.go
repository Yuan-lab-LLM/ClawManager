package ownerrecords

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestRecentQuestionPlainTextAndPrivacy(t *testing.T) {
	for _, raw := range []string{"api_key=abcdefghijklmnop", "Bearer abcdefghijklmnop", "password: hidden", "-----BEGIN PRIVATE KEY----- hidden"} {
		got := questionExcerpt(raw)
		if strings.Contains(got, "abcdefghijklmnop") || strings.Contains(got, "hidden") {
			t.Fatal("secret not suppressed")
		}
	}
	if got := questionExcerpt("<b>测试</b>\n\u202e下一步"); got != "测试 下一步" {
		t.Fatal(got)
	}
	if len([]rune(questionExcerpt(strings.Repeat("问", 300)))) != 121 {
		t.Fatal("excerpt unbounded")
	}
}

func TestRecentQuestionIndependentOfCompletion(t *testing.T) {
	for _, tc := range []struct{ runtime, data string }{
		{"openclaw", `{"type":"session","id":"s"}
{"type":"message","id":"u","timestamp":"2026-09-18T00:00:00Z","message":{"role":"user","content":"new question","__openclaw":{"senderIsOwner":true}}}
{"type":"message","id":"a","timestamp":"2026-09-18T00:01:00Z","message":{"role":"user","content":"automatic","__openclaw":{"senderIsOwner":false}}}`},
		{"deepseek-harness", `{"type":"session","id":"s"}
{"type":"user/message","time":1789689600000,"data":{"id":"u","source":{"kind":"user"},"content":[{"type":"text","text":"new question"}]}}
{"type":"user/message","time":1789689660000,"data":{"id":"a","source":{"kind":"plugin"},"content":"automatic"}}`},
	} {
		qs, err := parseRecentNativeLog(tc.runtime, []byte(tc.data))
		if err != nil || len(qs) != 1 || qs[0].Text != "new question" || qs[0].At <= 0 {
			t.Fatal(tc.runtime, err, len(qs))
		}
	}
}

func TestRecentSQLiteQueries(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python SQLite unavailable")
	}
	script := `import sqlite3,json,sys
p=json.load(sys.stdin);c=sqlite3.connect(':memory:');c.row_factory=sqlite3.Row
c.executescript(p['fixture']);c.execute('PRAGMA query_only=ON')
print(json.dumps([dict(r) for r in c.execute(p['query'].split(';',1)[1])]))`
	for _, tc := range []struct{ name, query, fixture string }{
		{"hermes", hermesRecentSQL, `CREATE TABLE sessions(id TEXT,source TEXT,parent_session_id TEXT);CREATE TABLE messages(id INTEGER,session_id TEXT,role TEXT,content TEXT,display_kind TEXT,_compressed_summary INTEGER,active INTEGER,timestamp REAL);
INSERT INTO sessions VALUES('s','desktop',NULL);
INSERT INTO messages VALUES(1,'s','user','old',NULL,0,1,1),(2,'s','user','latest',NULL,0,1,2),(3,'s','user','auto','auto_continue',0,1,3),(4,'s','user','compressed',NULL,1,1,4),(5,'s','user','deleted',NULL,0,0,5);`},
		{"opencode", openCodeRecentSQL, `CREATE TABLE session(id TEXT,parent_id TEXT);CREATE TABLE message(id TEXT,session_id TEXT,data TEXT);CREATE TABLE part(id TEXT,message_id TEXT,data TEXT);
INSERT INTO session VALUES('s',NULL),('child','s');
INSERT INTO message VALUES('u','s','{"role":"user","time":{"created":2000}}'),('a','s','{"role":"assistant","time":{"created":3000}}'),('auto','s','{"role":"user","time":{"created":4000}}'),('child-u','child','{"role":"user","time":{"created":5000}}');
INSERT INTO part VALUES('1','u','{"type":"text","text":"latest"}'),('2','a','{"type":"text","text":"answer"}'),('3','auto','{"type":"text","text":"automatic","synthetic":true}'),('4','child-u','{"type":"text","text":"subagent"}');`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"query": tc.query, "fixture": tc.fixture})
			cmd := exec.Command(python, "-c", script)
			cmd.Stdin = strings.NewReader(string(payload))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture failed: %v %s", err, out)
			}
			var qs []NativeQuestion
			if err = json.Unmarshal(out, &qs); err != nil || len(qs) != 1 || qs[0].Text != "latest" || qs[0].At != 2 {
				t.Fatal("latest human projection failed", err)
			}
		})
	}
}
