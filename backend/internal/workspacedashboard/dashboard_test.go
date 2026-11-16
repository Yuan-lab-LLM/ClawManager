package workspacedashboard

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestWindowShanghaiCalendarAndEqualComparison(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 15, 0, 0, time.UTC)
	w, err := makeWindow("7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if !w.Start.Equal(time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)) {
		t.Fatal(w)
	}
	if w.End.Sub(w.Start) != w.PreviousEnd.Sub(w.PreviousStart) {
		t.Fatal("unequal comparison", w)
	}
	if !w.PreviousEnd.Equal(w.End.AddDate(0, 0, -7)) {
		t.Fatal(w)
	}
	today, _ := makeWindow("today", now)
	if !today.Start.Equal(time.Date(2026, 11, 11, 16, 0, 0, 0, time.UTC)) {
		t.Fatal(today)
	}
	if _, err = makeWindow("all", now); err == nil {
		t.Fatal("invalid range accepted")
	}
}

func TestTaskSemanticsAndPayloadPrivacy(t *testing.T) {
	for native, want := range map[string]string{"succeeded": "succeeded", "failed": "failed", "running": "running", "dispatched": "running", "pending": "pending", "stale": "ended"} {
		if got := teamStatus(native); got != want {
			t.Fatalf("%s => %s", native, got)
		}
	}
	if got := taskTitle(`{"title":"Prepare report","access_token":"secret","messages":[{"content":"private"}]}`); got != "Prepare report" {
		t.Fatal(got)
	}
	if got := taskTitle(`{"access_token":"secret","messages":[{"content":"private"}]}`); got != "团队任务" {
		t.Fatal(got)
	}
	if got := taskTitle(`{"title":{"token":"secret"}}`); got != "团队任务" {
		t.Fatal(got)
	}
}

func TestMissingObservationsStayNull(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 15, 0, 0, time.UTC)
	w, _ := makeWindow("7d", now)
	d := emptyDashboard("7d", now, w)
	if len(d.Observations) != 7 || d.Observations[0].Total != nil || d.Usage.TotalTokens != nil {
		t.Fatalf("invented history: %+v", d)
	}
	if len(d.Activity) != 365 || d.Activity[0].Observed {
		t.Fatal("invented activity")
	}
}

func TestObservationWorkerStopsWithLifecycleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { (&Store{}).Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observation worker ignored cancellation")
	}
}

func TestObservationRetainsDistinctOfflineStartingAndLegacyGaps(t *testing.T) {
	var legacy Observation
	if err := json.Unmarshal([]byte(`{"at":"2026-11-12T00:00:00Z","total":3,"unknown":0}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Offline != nil || legacy.Starting != nil {
		t.Fatal("legacy gaps invented")
	}
}
