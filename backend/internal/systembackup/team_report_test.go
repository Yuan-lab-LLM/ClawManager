package systembackup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/systembackup/team"
)

func TestTeamRecoverySummaryPreservesCapturedAndTerminalStates(t *testing.T) {
	var collector teamReportCollector
	for _, status := range []string{"running", "running", "succeeded"} {
		row := team.Row{"id": team.Value("9001"), "status": team.Value(status), "workflow_state": team.Value("executing"), "payload_json": team.Value("secret-task-body"), "result_json": team.Value("private-partial-result")}
		if status == "succeeded" {
			row["workflow_state"] = team.Value("completed")
		}
		change, e := team.Normalize("team_tasks", row, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		collector.record(change)
	}
	collector.record(team.Change{Table: "team_events", Before: team.Row{"payload_json": team.Value("secret-event")}})
	collector.record(team.Change{Table: "users", Before: team.Row{"status": team.Value("secret-user")}})
	summary := collector.summary(false)
	if summary.SQLReadbackVerified || len(summary.Tables) != 2 || len(summary.States) != 4 {
		t.Fatal(summary)
	}
	running, completed := false, false
	for _, s := range summary.States {
		if s.Field != "status" {
			continue
		}
		if *s.Captured == "running" && *s.Restored == "failed" && s.Rows == 2 {
			running = true
		}
		if *s.Captured == "succeeded" && *s.Restored == "succeeded" && s.Rows == 1 {
			completed = true
		}
	}
	if !running || !completed {
		t.Fatal("states incorrectly summarized")
	}
	raw, e := json.Marshal(summary)
	if e != nil {
		t.Fatal(e)
	}
	for _, forbidden := range []string{"secret", "private", "9001", "payload_json", "result_json"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("sensitive fields in summary")
		}
	}
	if !collector.summary(true).SQLReadbackVerified {
		t.Fatal("verified result missing")
	}
}
