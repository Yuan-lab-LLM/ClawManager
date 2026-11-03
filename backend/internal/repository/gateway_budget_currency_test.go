package repository

import "testing"

func TestBudgetUsageNeverAddsDifferentCurrencies(t *testing.T) {
	u := &GatewayKeyBudgetUsage{Spent: 11, CurrencyReviewRequired: true, CostsByCurrency: map[string]float64{"USD": 1, "CNY": 10}}
	finalizeBudgetUsageCurrencies(u)
	if u.Currency != "USD" || u.AllTimeEstimatedCost != 1 || u.CostsByCurrency["CNY"] != 10 {
		t.Fatalf("currencies mixed: %+v", u)
	}
	if u.Spent != 0 || u.UnverifiedLegacySpent == nil || *u.UnverifiedLegacySpent != 11 || u.Remaining != nil {
		t.Fatalf("unverified spend exposed as USD: %+v", u)
	}
	u = &GatewayKeyBudgetUsage{Spent: 2, CostsByCurrency: map[string]float64{"USD": 2}}
	finalizeBudgetUsageCurrencies(u)
	if u.Spent != 2 || u.UnverifiedLegacySpent != nil {
		t.Fatalf("valid USD budget changed: %+v", u)
	}
}
