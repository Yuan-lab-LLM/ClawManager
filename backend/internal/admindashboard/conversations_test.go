package admindashboard

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Execute the production grouping/join predicates against real relational rows.
// Only the MySQL timestamp/null-safe-equality spelling is adapted for SQLite.
func TestAdminConversationSQLDeduplicatesTurnsAndScopesReplies(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python SQLite fixture runner unavailable")
	}
	script := `import datetime,json,sqlite3,sys
c=sqlite3.connect(':memory:')
c.create_function('TIMESTAMPDIFF',3,lambda unit,start,end:int((datetime.datetime.fromisoformat(end)-datetime.datetime.fromisoformat(start)).total_seconds()/3600))
c.executescript('CREATE TABLE chat_messages(trace_id TEXT,session_id TEXT,user_id INTEGER,instance_id INTEGER,invocation_id INTEGER,role TEXT,sequence_no INTEGER,created_at TEXT); CREATE TABLE model_invocations(id INTEGER,trace_id TEXT,session_id TEXT,user_id INTEGER,instance_id INTEGER,status TEXT,traffic_class TEXT);')
def message(trace,at,role='user',seq=1,user=1,instance=9,session='s',inv=None):
 c.execute('INSERT INTO chat_messages VALUES(?,?,?,?,?,?,?,?)',(trace,session,user,instance,inv,role,seq,at))
def reply(trace,at,inv,user=1,instance=9,session='s',status='completed',traffic='llm',role='assistant'):
 c.execute('INSERT INTO model_invocations VALUES(?,?,?,?,?,?,?)',(inv,trace,session,user,instance,status,traffic))
 message(trace,at,role,2,user,instance,session,inv)
# One root, several tool/model rounds and duplicate persisted root => one turn.
message('a','2026-11-15T16:01:00Z');message('a','2026-11-15T16:01:00Z')
reply('a','2026-11-15T16:02:00Z',1);reply('a','2026-11-15T16:03:00Z',2)
message('a','2026-11-15T16:04:00Z','tool',3)
message('a','2026-11-15T16:04:00Z','user',4) # replay suffix is not a new root
# A second real user turn in the same session is a different trace.
message('b','2026-11-15T17:01:00Z');reply('b','2026-11-15T17:02:00Z',3)
# User-scoped trace reuse and CM messages with no instance are independent.
message('a','2026-11-15T18:01:00Z',user=2);reply('a','2026-11-15T18:02:00Z',4,user=2)
message('cm','2026-11-15T18:01:00Z',instance=None);reply('cm','2026-11-15T18:02:00Z',5,instance=None)
# Failed/non-LLM/tool-only/unlinked/no-user/no-root cases are not confirmed turns.
for trace,inv,status,traffic,role in [('failed',6,'failed','llm','assistant'),('other',7,'completed','embedding','assistant'),('tool',8,'completed','llm','tool')]:
 message(trace,'2026-11-15T18:10:00Z');reply(trace,'2026-11-15T18:11:00Z',inv,status=status,traffic=traffic,role=role)
message('unlinked','2026-11-15T18:10:00Z');message('unlinked','2026-11-15T18:11:00Z','assistant',2)
reply('no-user','2026-11-15T18:11:00Z',9)
message('no-root','2026-11-15T18:10:00Z',seq=3);reply('no-root','2026-11-15T18:11:00Z',10)
# Replies with a different scope/session/instance cannot confirm a root.
for trace,inv,kwargs in [('foreign-user',11,{'user':2}),('foreign-instance',12,{'instance':10}),('foreign-session',13,{'session':'elsewhere'})]:
 message(trace,'2026-11-15T18:10:00Z');reply(trace,'2026-11-15T18:11:00Z',inv,**kwargs)
# The reporting interval is exclusive at the end, including reply confirmation.
message('before','2026-11-15T15:59:00Z');reply('before','2026-11-15T16:01:00Z',14)
message('after','2026-11-16T02:30:00Z');reply('after','2026-11-16T02:31:00Z',15)
message('reply-later','2026-11-15T18:10:00Z');reply('reply-later','2026-11-16T02:30:00Z',16)
q=sys.stdin.read().replace('TIMESTAMPDIFF(HOUR,',"TIMESTAMPDIFF('HOUR',").replace('<=>','IS')
print(json.dumps(sorted(c.execute(q,('2026-11-15T16:00:00Z','2026-11-15T16:00:00Z','2026-11-16T02:30:00Z','2026-11-16T02:30:00Z')).fetchall())))`
	cmd := exec.Command(python, "-c", script)
	cmd.Stdin = strings.NewReader(conversationTurnsSQL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SQL fixture failed: %v %s", err, out)
	}
	var got [][2]int
	if json.Unmarshal(out, &got) != nil || len(got) != 3 || got[0] != [2]int{0, 1} || got[1] != [2]int{1, 1} || got[2] != [2]int{2, 2} {
		t.Fatalf("deduplicated hourly turns: %s", out)
	}
}
