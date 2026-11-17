package services

import (
	"clawreef/internal/repository"
	"clawreef/internal/utils"
)

type sessionUsageReporter interface {
	ListSessionUsageReport(int, int, int, string, repository.SessionUsageFilter) (*repository.SessionUsageReport, error)
	ListAdminSessionUsageReport(int, int, string, repository.SessionUsageFilter) (*repository.AdminSessionUsageReport, error)
}

func mapSessionReportSummary(in repository.SessionUsageReportSummary) (InstanceSessionUsageSummary, InstanceSessionUsageCompliance) {
	summary := InstanceSessionUsageSummary{TotalPromptTokens: in.PromptTokens, TotalCompletionTokens: in.CompletionTokens, TotalTokens: in.TotalTokens, TotalEstimatedCost: in.EstimatedCost, Currency: in.Currency, SessionCount: in.SessionCount}
	if latest := in.Latest; latest != nil {
		summary.LatestRequestedModel = latest.RequestedModel
		summary.LatestActualModel = latest.ActualProviderModel
		summary.LatestModelStatus = latest.Status
		summary.LatestModelAt = &latest.CreatedAt
		summary.LatestLatencyMs = latest.LatencyMs
		summary.LatestTTFTMs = latest.TTFTMs
	}
	return summary, InstanceSessionUsageCompliance{FallbackSessionCount: in.FallbackSessionCount, HasFallbackSessions: in.FallbackSessionCount > 0, RecentFallbackAuditCount: in.RecentFallbackAuditCount}
}

func getSQLSessionUsage(reporter sessionUsageReporter, instanceID int, query InstanceSessionUsageQuery) (*InstanceSessionUsageResult, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 20, 100)
	report, err := reporter.ListSessionUsageReport(instanceID, page, limit, query.Search, repository.SessionUsageFilter{Since: query.Since, Until: query.Until})
	if err != nil {
		return nil, err
	}
	summary, compliance := mapSessionReportSummary(report.Summary)
	result := &InstanceSessionUsageResult{Summary: summary, Compliance: compliance, Total: report.Total, Page: page, Limit: limit, Items: make([]InstanceSessionUsageItem, 0, len(report.Items))}
	for _, item := range report.Items {
		result.Items = append(result.Items, mapSessionReportItem(item))
	}
	return result, nil
}

func mapSessionReportItem(item repository.SessionUsageReportItem) InstanceSessionUsageItem {
	return InstanceSessionUsageItem{SessionID: item.SessionID, SessionKey: utils.FormatOpenClawSessionKey(item.SessionID), Title: item.Title, PromptTokens: item.PromptTokens, CompletionTokens: item.CompletionTokens, TotalTokens: item.TotalTokens, EstimatedCost: item.EstimatedCost, Currency: item.Currency, InvocationCount: item.InvocationCount, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt}
}

func getSQLAdminSessionUsage(reporter sessionUsageReporter, query InstanceSessionUsageOverviewQuery) (*InstanceSessionUsageOverview, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 20, 100)
	report, err := reporter.ListAdminSessionUsageReport(page, limit, query.Search, repository.SessionUsageFilter{Since: query.Since, Until: query.Until})
	if err != nil {
		return nil, err
	}
	summary, _ := mapSessionReportSummary(report.Summary)
	result := &InstanceSessionUsageOverview{Summary: summary, Total: report.Total, Page: page, Limit: limit, Items: make([]InstanceSessionUsageOverviewItem, 0, len(report.Items))}
	for _, item := range report.Items {
		summary, compliance := mapSessionReportSummary(item.Summary)
		result.Items = append(result.Items, InstanceSessionUsageOverviewItem{InstanceID: item.InstanceID, InstanceName: item.InstanceName, InstanceType: item.InstanceType, UserID: item.UserID, Summary: summary, Compliance: compliance})
	}
	return result, nil
}
