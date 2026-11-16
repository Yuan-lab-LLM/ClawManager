package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAcknowledgedWorkItemContextKeepsOnlyAcceptedPublicResult(t *testing.T) {
	now := time.Now()
	raw := `{"completionId":"receipt-one","summary":"Delivered","resultMarkdown":"Actual answer","reasoning":"private-canary"}`
	item := models.TeamWorkItem{Status: models.TeamTaskStatusSucceeded, FinishedAt: &now, ResultJSON: &raw}
	v := acknowledgedWorkItemContext(item)
	if v["resultMarkdown"] != "Actual answer" {
		t.Fatalf("accepted answer missing: %#v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "private-canary") {
		t.Fatal("raw payload leaked")
	}
	item.Status = models.TeamTaskStatusRunning
	if acknowledgedWorkItemContext(item) != nil {
		t.Fatal("unfinished work exposed as accepted")
	}
	item.Status = models.TeamTaskStatusSucceeded
	item.FinishedAt = nil
	if acknowledgedWorkItemContext(item) != nil {
		t.Fatal("missing accepted terminal evidence")
	}
}

func TestTeamWorkflowProjectionRealRedisMonotonic(t *testing.T) {
	rawURL := os.Getenv("TEST_TEAM_CONTEXT_REDIS_URL")
	if rawURL == "" {
		t.Skip("requires isolated Redis")
	}
	if !strings.HasPrefix(rawURL, "redis://127.0.0.1:") {
		t.Fatal("only disposable loopback Redis allowed")
	}
	bus, err := newRedisBus(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	key := "test:m01:workflow-projection"
	defer bus.Del(context.Background(), key)
	put := func(ledger int, terminal, complete bool) {
		encoded, _ := json.Marshal(map[string]interface{}{"rootTaskId": "team-501-task-1", "ledgerVersion": ledger, "terminal": terminal, "assignmentLedgerComplete": complete})
		publishTeamWorkflowContext(bus, key, encoded)
	}
	put(7, false, true)
	put(6, false, true)
	put(7, false, false)
	read := func() map[string]interface{} {
		raw, ok, err := bus.Get(context.Background(), key)
		if err != nil || !ok {
			t.Fatalf("projection missing: %v", err)
		}
		var v map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := read(); v["ledgerVersion"] != float64(7) || v["assignmentLedgerComplete"] != true {
		t.Fatalf("stale projection won: %#v", v)
	}
	put(8, true, true)
	put(8, false, true)
	if read()["terminal"] != true {
		t.Fatal("terminal projection regressed")
	}
	put(9, false, true)
	if read()["ledgerVersion"] != float64(9) {
		t.Fatal("advisory cache overruled a newer SQL business version")
	}
}
