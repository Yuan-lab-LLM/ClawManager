package services

import (
	"context"
	"strings"
	"testing"
)

func TestUpgradeMySQLOpenCodeLeaderAndBusyMembersUseTeamContract(t *testing.T) {
	sess, u, _ := recoveryActionsDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := sess.SQL().Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO teams(id) VALUES (910)`)
	exec(`INSERT INTO team_members(id,instance_id,team_id,member_key,role,runtime_type,availability,status) VALUES (910,481,910,'leader','leader','opencode','idle','idle'),(911,482,910,'worker','worker','openclaw','idle','idle')`)
	check := func(want bool, part string) {
		t.Helper()
		_, blockers, e := u.inspectTeamConsistency(context.Background(), map[int]struct{}{910: {}})
		if e != nil || (len(blockers) > 0) != want || (want && !strings.Contains(strings.Join(blockers, ";"), part)) {
			t.Fatalf("blockers=%v err=%v", blockers, e)
		}
	}
	check(false, "")
	exec(`UPDATE team_members SET availability='busy' WHERE id=910`)
	check(true, "opencode member")
	exec(`UPDATE team_members SET availability='idle' WHERE id=910`)
	exec(`UPDATE team_members SET role='worker',runtime_type='openclaw' WHERE id=910`)
	exec(`UPDATE team_members SET role='leader',runtime_type='opencode',status='busy' WHERE id=911`)
	check(true, "opencode member")
	exec(`UPDATE team_members SET status='idle',runtime_type='hermes' WHERE id=911`)
	check(true, "Leader must be")
	exec(`UPDATE team_members SET runtime_type='openclaw' WHERE id=911`)
	check(false, "")
}
