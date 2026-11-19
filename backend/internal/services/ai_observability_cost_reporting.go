package services

import "clawreef/internal/repository"

type costOverviewReporter interface {
	CostOverviewReport(int, int, string, repository.SessionUsageFilter) (*repository.CostOverviewReport, error)
}

func getSQLCostOverview(reporter costOverviewReporter, query CostQuery) (*CostOverview, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 20, 100)
	report, err := reporter.CostOverviewReport(page, limit, query.Search, repository.SessionUsageFilter{Since: query.Since, Until: query.Until, Cursor: query.Cursor})
	if err != nil {
		return nil, err
	}
	total := report.Totals
	result := &CostOverview{TotalPromptTokens: total.PromptTokens, TotalCompletionTokens: total.CompletionTokens, TotalTokens: total.TotalTokens, TotalEstimatedCost: total.EstimatedCost, TotalInternalCost: total.InternalCost, Currency: total.Currency, TotalRecentRecords: report.Total, NextCursor: report.NextCursor, Page: page, Limit: limit, UserSummary: []CostAggregateItem{}, InstanceSummary: []CostAggregateItem{}, TopModels: []CostBreakdownItem{}, TopUsers: []CostBreakdownItem{}, DailyTrend: []CostTrendPoint{}, ModelTrends: []CostTrendSeries{}, UserTrends: []CostTrendSeries{}, RecentRecords: []CostRecordView{}}
	aggregate := func(v repository.CostOverviewGroup) CostAggregateItem {
		c := v.Totals
		return CostAggregateItem{Label: v.Label, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, TotalTokens: c.TotalTokens, EstimatedCost: c.EstimatedCost, InternalCost: c.InternalCost}
	}
	breakdown := func(v repository.CostOverviewGroup) CostBreakdownItem {
		c := v.Totals
		return CostBreakdownItem{Label: v.Label, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, TotalTokens: c.TotalTokens, EstimatedCost: c.EstimatedCost, InternalCost: c.InternalCost}
	}
	for _, v := range report.Users {
		result.UserSummary = append(result.UserSummary, aggregate(v))
	}
	for _, v := range report.Instances {
		result.InstanceSummary = append(result.InstanceSummary, aggregate(v))
	}
	for _, v := range report.Users[:min(5, len(report.Users))] {
		result.TopUsers = append(result.TopUsers, breakdown(v))
	}
	for _, v := range report.Models[:min(5, len(report.Models))] {
		result.TopModels = append(result.TopModels, breakdown(v))
	}
	point := func(v repository.CostOverviewDay) CostTrendPoint {
		c := v.Totals
		return CostTrendPoint{Day: v.Day, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, TotalTokens: c.TotalTokens, EstimatedCost: c.EstimatedCost, InternalCost: c.InternalCost}
	}
	for _, v := range report.Daily {
		result.DailyTrend = append(result.DailyTrend, point(v))
	}
	series := func(days []repository.CostOverviewDay) []CostTrendSeries {
		out := []CostTrendSeries{}
		positions := map[string]int{}
		for _, v := range days {
			idx, ok := positions[v.Label]
			if !ok {
				idx = len(out)
				positions[v.Label] = idx
				out = append(out, CostTrendSeries{Label: v.Label, Points: []CostTrendPoint{}})
			}
			out[idx].Points = append(out[idx].Points, point(v))
		}
		return out
	}
	result.ModelTrends = series(report.ModelDays)
	result.UserTrends = series(report.UserDays)
	for _, v := range report.Items {
		c := v.Record
		result.RecentRecords = append(result.RecentRecords, CostRecordView{ID: c.ID, TraceID: c.TraceID, UserID: c.UserID, Username: v.Username, InstanceID: c.InstanceID, InstanceName: v.InstanceName, ModelName: c.ModelName, ProviderType: c.ProviderType, PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, TotalTokens: c.TotalTokens, EstimatedCost: c.EstimatedCost, InternalCost: c.InternalCost, Currency: c.Currency, RecordedAt: c.RecordedAt})
	}
	return result, nil
}
