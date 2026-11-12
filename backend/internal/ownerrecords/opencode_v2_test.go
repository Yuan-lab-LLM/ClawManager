package ownerrecords

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeV2HistoryExcludesAutomationAndFailedTurns(t *testing.T) {
	cli, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("SQLite CLI unavailable; exercised in Linux image")
	}
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 2, Owner: "owner@example.test", Runtime: "opencode", Mode: "lite"}
	workspace := filepath.Join(root, "opencode/user-1/instance-2")
	path := filepath.Join(workspace, "home/.local/share/opencode/opencode.db")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `CREATE TABLE session_v2(id TEXT,parent_id TEXT,metadata TEXT);
CREATE TABLE session_message(id TEXT,session_id TEXT,type TEXT,seq INTEGER,time_created INTEGER,data TEXT);
INSERT INTO session_v2 VALUES('human',NULL,'{}'),('team',NULL,'{"clawmanager.team":"175"}'),('child','human','{}');
INSERT INTO session_message VALUES
('u1','human','user',1,1000,'{"text":"first human"}'),
('a1','human','assistant',2,2000,'{"finish":"stop","time":{"completed":3000},"content":[{"type":"text","text":"answer"}]}'),
('a2','human','assistant',3,3000,'{"finish":"stop","time":{"completed":3500},"content":[{"type":"text","text":"same result"}]}'),
('u2','human','user',4,4000,'{"text":"second human"}'),
('a3','human','assistant',5,5000,'{"finish":"stop","time":{"completed":5000},"content":[{"type":"text","text":"answer"}]}'),
('uf','human','user',6,6000,'{"text":"failed human"}'),
('af','human','assistant',7,6500,'{"error":{"type":"provider"},"content":[{"type":"text","text":"failure"}]}'),
('auto','human','synthetic',8,7000,'{"text":"continue automatically"}'),
('autoanswer','human','assistant',9,7500,'{"finish":"stop","time":{"completed":7500},"content":[{"type":"text","text":"automation answer"}]}'),
('u3','human','user',10,8000,'{"text":"latest pending human"}'),
('tool','human','assistant',11,8500,'{"finish":"tool_calls","time":{"completed":8500},"content":[{"type":"reasoning","text":"private"}]}'),
('ut','team','user',1,99000,'{"text":"internal assignment"}'),
('at','team','assistant',2,99900,'{"finish":"stop","time":{"completed":99900},"content":[{"type":"text","text":"internal result"}]}'),
('uc','child','user',1,99000,'{"text":"child input"}');`
	cmd := exec.Command(cli, path)
	cmd.Stdin = strings.NewReader(fixture)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture: %v %s", e, output)
	}
	before, _ := os.ReadFile(path)
	questions, err := ReadNativeHistory(context.Background(), root, workspace, i)
	if err != nil || len(questions) != 2 || questions[0].ID != "u1" || questions[0].At != 3 || questions[1].ID != "u2" {
		t.Fatalf("wrong human facts: %+v %v", questions, err)
	}
	recent, err := readNativeHistory(context.Background(), root, workspace, i, true)
	if err != nil || len(recent) != 1 || recent[0].ID != "u3" || recent[0].Text != "latest pending human" {
		t.Fatalf("wrong recent prompt: %+v %v", recent, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("analytics changed Runtime database")
	}
}
