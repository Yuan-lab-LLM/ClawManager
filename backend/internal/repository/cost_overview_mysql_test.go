package repository

import (
	"testing"
	"time"
)

func TestCostOverviewSQLFullScopeCursorAndCurrencies(t *testing.T) {
	s := reportingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	since := now.Truncate(24 * time.Hour).Add(-24 * time.Hour)
	until := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
	for i := 1; i <= 1005; i++ {
		if _, e := s.SQL().Exec(`INSERT INTO cost_records(trace_id,user_id,instance_id,provider_type,model_name,estimated_cost,total_tokens,recorded_at)VALUES(?,1,9,'openai','m',0.01,10,?)`, "trace", now); e != nil {
			t.Fatal(e)
		}
	}
	seedCostSummaryFixture(t, s)
	repo := NewCostRecordRepository(s).(*costRecordRepository)
	first, e := repo.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until})
	if e != nil {
		t.Fatal(e)
	}
	if first.Total != 1005 || first.Totals.TotalTokens != 10050 || len(first.Items) != 20 || first.NextCursor == "" {
		t.Fatalf("full ledger scope: %+v", first)
	}
	if len(first.Daily) != 1 || first.Daily[0].Totals.TotalTokens != 10050 || len(first.Models) != 1 || first.Models[0].Totals.TotalTokens != 10050 {
		t.Fatalf("full-day rollup lost groups: %+v", first)
	}
	filtered, e := repo.CostOverviewReport(1, 20, "operator", SessionUsageFilter{Since: &since, Until: &until})
	if e != nil || filtered.Total != 1005 || filtered.Totals.TotalTokens != 10050 {
		t.Fatalf("searched full ledger scope: %+v %v", filtered, e)
	}
	empty, e := repo.CostOverviewReport(1, 20, "missing-operator", SessionUsageFilter{Since: &since, Until: &until})
	if e != nil || empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("empty search: %+v %v", empty, e)
	}
	if _, e = s.SQL().Exec(`INSERT INTO cost_records(trace_id,user_id,instance_id,provider_type,model_name,estimated_cost,total_tokens,recorded_at)VALUES('late',1,9,'openai','m',100,100,?)`, now.Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	seedCostSummaryFixture(t, s)
	second, e := repo.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until, Cursor: first.NextCursor})
	if e != nil || second.Total != 1005 || second.Items[0].Record.ID >= first.Items[19].Record.ID {
		t.Fatalf("stable cursor: %+v %v", second, e)
	}
	if _, e = s.SQL().Exec(`INSERT INTO cost_records(trace_id,user_id,instance_id,provider_type,model_name,estimated_cost,total_tokens,currency,recorded_at)VALUES('eur',1,9,'openai','m',1,1,'EUR',?)`, now); e != nil {
		t.Fatal(e)
	}
	seedCostSummaryFixture(t, s)
	if _, e = repo.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until}); e != ErrMixedCostCurrencies {
		t.Fatalf("mixed currency accepted: %v", e)
	}
	old, e := repo.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until, Cursor: first.NextCursor})
	if e != nil || old.Total != 1005 || old.Totals.TotalTokens != 10050 || old.Totals.Currency != "USD" {
		t.Fatalf("late currency polluted old cursor: %+v %v", old, e)
	}
}
