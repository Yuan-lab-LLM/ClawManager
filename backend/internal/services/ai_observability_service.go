package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// AuditQuery contains query options for AI audit list views.
type AuditQuery struct {
	Page   int
	Limit  int
	Search string
	Status string
	Model  string
}

// AuditListResult is the paginated audit list response.
type AuditListResult struct {
	Items []AuditListItem `json:"items"`
	Total int             `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
}

// AuditListItem is a flattened list row for AI audit views.
type AuditListItem struct {
	TraceID             string     `json:"trace_id"`
	RequestID           string     `json:"request_id"`
	SessionID           *string    `json:"session_id,omitempty"`
	UserID              *int       `json:"user_id,omitempty"`
	Username            string     `json:"username,omitempty"`
	InstanceID          *int       `json:"instance_id,omitempty"`
	RequestedModel      string     `json:"requested_model"`
	ActualProviderModel string     `json:"actual_provider_model"`
	ProviderType        string     `json:"provider_type"`
	Status              string     `json:"status"`
	PromptTokens        int        `json:"prompt_tokens"`
	CompletionTokens    int        `json:"completion_tokens"`
	TotalTokens         int        `json:"total_tokens"`
	LatencyMs           *int       `json:"latency_ms,omitempty"`
	ErrorMessage        *string    `json:"error_message,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

// AuditTraceDetail bundles invocation, audit event, and cost records for a trace.
type AuditTraceDetail struct {
	TraceID     string                     `json:"trace_id"`
	Username    string                     `json:"username,omitempty"`
	Invocations []models.ModelInvocation   `json:"invocations"`
	AuditEvents []models.AuditEvent        `json:"audit_events"`
	CostRecords []models.CostRecord        `json:"cost_records"`
	RiskHits    []models.RiskHit           `json:"risk_hits"`
	Messages    []models.ChatMessageRecord `json:"messages"`
	FlowNodes   []AuditFlowNode            `json:"flow_nodes"`
}

// AuditFlowNode is a synthesized node in a trace flow visualization.
type AuditFlowNode struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	RequestID     string    `json:"request_id,omitempty"`
	InvocationID  *int      `json:"invocation_id,omitempty"`
	Model         string    `json:"model,omitempty"`
	Status        string    `json:"status,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	InputPayload  string    `json:"input_payload,omitempty"`
	OutputPayload string    `json:"output_payload,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// CostOverview contains a compact cost summary for admin reporting.
type CostOverview struct {
	TotalPromptTokens     int                 `json:"total_prompt_tokens"`
	TotalCompletionTokens int                 `json:"total_completion_tokens"`
	TotalTokens           int                 `json:"total_tokens"`
	TotalEstimatedCost    float64             `json:"total_estimated_cost"`
	TotalInternalCost     float64             `json:"total_internal_cost"`
	Currency              string              `json:"currency"`
	UserSummary           []CostAggregateItem `json:"user_summary"`
	InstanceSummary       []CostAggregateItem `json:"instance_summary"`
	TopModels             []CostBreakdownItem `json:"top_models"`
	TopUsers              []CostBreakdownItem `json:"top_users"`
	DailyTrend            []CostTrendPoint    `json:"daily_trend"`
	ModelTrends           []CostTrendSeries   `json:"model_trends"`
	UserTrends            []CostTrendSeries   `json:"user_trends"`
	RecentRecords         []CostRecordView    `json:"recent_records"`
	TotalRecentRecords    int                 `json:"total_recent_records"`
	Page                  int                 `json:"page"`
	Limit                 int                 `json:"limit"`
}

// CostQuery contains query options for cost list views.
type CostQuery struct {
	Page   int
	Limit  int
	Search string
}

// CostBreakdownItem is a grouped summary row.
type CostBreakdownItem struct {
	Label            string  `json:"label"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	InternalCost     float64 `json:"internal_cost"`
}

// CostAggregateItem is a grouped summary row for user or instance aggregates.
type CostAggregateItem struct {
	Label            string  `json:"label"`
	Meta             string  `json:"meta,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	InternalCost     float64 `json:"internal_cost"`
}

// CostTrendPoint is a day bucket for charting token and cost trends.
type CostTrendPoint struct {
	Day              string  `json:"day"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	InternalCost     float64 `json:"internal_cost"`
}

// CostTrendSeries is a labeled chart series used for model/user trends.
type CostTrendSeries struct {
	Label  string           `json:"label"`
	Points []CostTrendPoint `json:"points"`
}

// CostRecordView is a recent cost record decorated with usernames.
type CostRecordView struct {
	ID               int       `json:"id"`
	TraceID          string    `json:"trace_id"`
	UserID           *int      `json:"user_id,omitempty"`
	Username         string    `json:"username,omitempty"`
	InstanceID       *int      `json:"instance_id,omitempty"`
	InstanceName     string    `json:"instance_name,omitempty"`
	ModelName        string    `json:"model_name"`
	ProviderType     string    `json:"provider_type"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	EstimatedCost    float64   `json:"estimated_cost"`
	InternalCost     float64   `json:"internal_cost"`
	Currency         string    `json:"currency"`
	RecordedAt       time.Time `json:"recorded_at"`
}

// AIObservabilityService provides read APIs for audit and cost reporting.
type AIObservabilityService interface {
	ListAuditItems(query AuditQuery) (*AuditListResult, error)
	GetTraceDetail(traceID string) (*AuditTraceDetail, error)
	GetCostOverview(query CostQuery) (*CostOverview, error)
}

type aiObservabilityService struct {
	invocationRepo  repository.ModelInvocationRepository
	auditRepo       repository.AuditEventRepository
	costRepo        repository.CostRecordRepository
	riskHitRepo     repository.RiskHitRepository
	chatMessageRepo repository.ChatMessageRepository
	llmModelRepo    repository.LLMModelRepository
	instanceRepo    repository.InstanceRepository
	userRepo        repository.UserRepository
}

// NewAIObservabilityService creates a new observability reporting service.
func NewAIObservabilityService(
	invocationRepo repository.ModelInvocationRepository,
	auditRepo repository.AuditEventRepository,
	costRepo repository.CostRecordRepository,
	riskHitRepo repository.RiskHitRepository,
	chatMessageRepo repository.ChatMessageRepository,
	llmModelRepo repository.LLMModelRepository,
	instanceRepo repository.InstanceRepository,
	userRepo repository.UserRepository,
) AIObservabilityService {
	return &aiObservabilityService{
		invocationRepo:  invocationRepo,
		auditRepo:       auditRepo,
		costRepo:        costRepo,
		riskHitRepo:     riskHitRepo,
		chatMessageRepo: chatMessageRepo,
		llmModelRepo:    llmModelRepo,
		instanceRepo:    instanceRepo,
		userRepo:        userRepo,
	}
}

func (s *aiObservabilityService) ListAuditItems(query AuditQuery) (*AuditListResult, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 20, 100)

	items, err := s.invocationRepo.ListRecent(expandFetchWindow(page, limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list audit items: %w", err)
	}
	events, err := s.auditRepo.ListRecent(expandFetchWindow(page, limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list recent audit events: %w", err)
	}
	costs, err := s.costRepo.ListRecent(expandFetchWindow(page, limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list recent cost records: %w", err)
	}

	usernames, _ := s.loadUsernamesForAudit(items, events, costs)
	costByTrace := aggregateRecentCostByTrace(costs)
	traceItems := aggregateInvocationsByTrace(items, usernames)

	search := strings.ToLower(strings.TrimSpace(query.Search))
	status := strings.ToLower(strings.TrimSpace(query.Status))
	model := strings.ToLower(strings.TrimSpace(query.Model))

	filtered := make([]AuditListItem, 0, len(traceItems))
	seenTraceIDs := make(map[string]struct{}, len(traceItems))
	for _, item := range traceItems {
		if status != "" && strings.ToLower(item.Status) != status {
			continue
		}
		if model != "" && !strings.Contains(strings.ToLower(item.RequestedModel), model) && !strings.Contains(strings.ToLower(item.ActualProviderModel), model) {
			continue
		}
		if search != "" {
			haystacks := []string{
				strings.ToLower(item.TraceID),
				strings.ToLower(item.RequestID),
				strings.ToLower(item.RequestedModel),
				strings.ToLower(item.ActualProviderModel),
				strings.ToLower(item.ProviderType),
				strings.ToLower(item.Username),
			}
			matched := false
			for _, candidate := range haystacks {
				if strings.Contains(candidate, search) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		filtered = append(filtered, item)
		seenTraceIDs[item.TraceID] = struct{}{}
	}

	for _, event := range events {
		if event.TraceID == "" {
			continue
		}
		if _, exists := seenTraceIDs[event.TraceID]; exists {
			continue
		}

		username := usernames[valueOrZero(event.UserID)]
		if username == "" {
			if cost := costByTrace[event.TraceID]; cost != nil && cost.UserID != nil {
				username = usernames[*cost.UserID]
			}
		}
		fallbackItem := AuditListItem{
			TraceID:             event.TraceID,
			RequestID:           valueOrString(event.RequestID),
			SessionID:           event.SessionID,
			UserID:              firstAvailableUserID(event.UserID, costByTrace[event.TraceID]),
			Username:            username,
			InstanceID:          firstAvailableInstanceID(event.InstanceID, costByTrace[event.TraceID]),
			RequestedModel:      "Auto",
			ActualProviderModel: inferActualProviderLabel(event, costByTrace[event.TraceID]),
			ProviderType:        "gateway",
			Status:              inferAuditStatusFromEvent(event),
			PromptTokens:        valueOrCostPromptTokens(costByTrace[event.TraceID]),
			CompletionTokens:    valueOrCostCompletionTokens(costByTrace[event.TraceID]),
			TotalTokens:         valueOrCostTotalTokens(costByTrace[event.TraceID]),
			CreatedAt:           event.CreatedAt,
			CompletedAt:         nil,
		}

		if search != "" {
			haystacks := []string{
				strings.ToLower(fallbackItem.TraceID),
				strings.ToLower(fallbackItem.RequestID),
				strings.ToLower(fallbackItem.RequestedModel),
				strings.ToLower(fallbackItem.ActualProviderModel),
				strings.ToLower(fallbackItem.ProviderType),
				strings.ToLower(username),
				strings.ToLower(event.EventType),
				strings.ToLower(event.Message),
			}
			matched := false
			for _, candidate := range haystacks {
				if strings.Contains(candidate, search) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if status != "" && strings.ToLower(fallbackItem.Status) != status {
			continue
		}
		if model != "" && !strings.Contains(strings.ToLower(fallbackItem.RequestedModel), model) && !strings.Contains(strings.ToLower(fallbackItem.ActualProviderModel), model) {
			continue
		}

		filtered = append(filtered, fallbackItem)
		seenTraceIDs[event.TraceID] = struct{}{}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)
	start, end := paginateBounds(total, page, limit)
	if start >= total {
		return &AuditListResult{
			Items: []AuditListItem{},
			Total: total,
			Page:  page,
			Limit: limit,
		}, nil
	}

	return &AuditListResult{
		Items: filtered[start:end],
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

func (s *aiObservabilityService) GetTraceDetail(traceID string) (*AuditTraceDetail, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, fmt.Errorf("trace id is required")
	}

	invocations, err := s.invocationRepo.ListByTraceID(traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load trace invocations: %w", err)
	}
	events, err := s.auditRepo.ListByTraceID(traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load trace audit events: %w", err)
	}
	costs, err := s.costRepo.ListByTraceID(traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load trace cost records: %w", err)
	}
	riskHits, err := s.riskHitRepo.ListByTraceID(traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load trace risk hits: %w", err)
	}
	messages, err := s.chatMessageRepo.ListByTraceID(traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load trace messages: %w", err)
	}

	if len(invocations) == 0 {
		invocations = synthesizeInvocationsFromTrace(traceID, events, costs, messages)
	}

	username := s.resolveTraceUsername(invocations, events, costs, messages)

	return &AuditTraceDetail{
		TraceID:     traceID,
		Username:    username,
		Invocations: invocations,
		AuditEvents: events,
		CostRecords: costs,
		RiskHits:    riskHits,
		Messages:    messages,
		FlowNodes:   buildAuditFlowNodes(invocations),
	}, nil
}

func (s *aiObservabilityService) GetCostOverview(query CostQuery) (*CostOverview, error) {
	page, limit := normalizePageLimit(query.Page, query.Limit, 20, 100)
	search := strings.ToLower(strings.TrimSpace(query.Search))

	records, err := s.costRepo.ListRecent(expandFetchWindow(page, limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list cost records: %w", err)
	}

	userIDs := make(map[int]struct{})
	instanceIDs := make(map[int]struct{})
	for _, record := range records {
		if record.UserID != nil {
			userIDs[*record.UserID] = struct{}{}
		}
		if record.InstanceID != nil {
			instanceIDs[*record.InstanceID] = struct{}{}
		}
	}

	usernames := make(map[int]string, len(userIDs))
	for userID := range userIDs {
		user, err := s.userRepo.GetByID(userID)
		if err == nil && user != nil {
			usernames[userID] = user.Username
		}
	}

	instanceNames := make(map[int]string, len(instanceIDs))
	for instanceID := range instanceIDs {
		instance, err := s.instanceRepo.GetByID(instanceID)
		if err == nil && instance != nil {
			instanceNames[instanceID] = instance.Name
		}
	}

	filteredRecords := make([]models.CostRecord, 0, len(records))
	for _, record := range records {
		if search != "" {
			haystacks := []string{
				strings.ToLower(record.TraceID),
				strings.ToLower(record.ModelName),
				strings.ToLower(record.ProviderType),
				strings.ToLower(usernames[valueOrZero(record.UserID)]),
			}
			matched := false
			for _, candidate := range haystacks {
				if strings.Contains(candidate, search) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filteredRecords = append(filteredRecords, record)
	}

	overview := &CostOverview{
		Currency:           "USD",
		UserSummary:        []CostAggregateItem{},
		InstanceSummary:    []CostAggregateItem{},
		TopModels:          []CostBreakdownItem{},
		TopUsers:           []CostBreakdownItem{},
		DailyTrend:         []CostTrendPoint{},
		ModelTrends:        []CostTrendSeries{},
		UserTrends:         []CostTrendSeries{},
		RecentRecords:      []CostRecordView{},
		TotalRecentRecords: len(filteredRecords),
		Page:               page,
		Limit:              limit,
	}

	modelTotals := map[string]*CostBreakdownItem{}
	userTotals := map[string]*CostBreakdownItem{}
	userAggregate := map[string]*CostAggregateItem{}
	instanceAggregate := map[string]*CostAggregateItem{}

	for _, record := range filteredRecords {
		overview.TotalPromptTokens += record.PromptTokens
		overview.TotalCompletionTokens += record.CompletionTokens
		overview.TotalTokens += record.TotalTokens
		overview.TotalEstimatedCost += record.EstimatedCost
		overview.TotalInternalCost += record.InternalCost
		if strings.TrimSpace(record.Currency) != "" {
			overview.Currency = record.Currency
		}

		modelRow := modelTotals[record.ModelName]
		if modelRow == nil {
			modelRow = &CostBreakdownItem{Label: record.ModelName}
			modelTotals[record.ModelName] = modelRow
		}
		aggregateBreakdown(modelRow, record)

		username := usernames[valueOrZero(record.UserID)]
		userLabel := username
		if userLabel == "" {
			if record.UserID != nil {
				userLabel = fmt.Sprintf("User #%d", *record.UserID)
			} else {
				userLabel = "Unknown"
			}
		}
		userRow := userTotals[userLabel]
		if userRow == nil {
			userRow = &CostBreakdownItem{Label: userLabel}
			userTotals[userLabel] = userRow
		}
		aggregateBreakdown(userRow, record)

		userSummary := userAggregate[userLabel]
		if userSummary == nil {
			userSummary = &CostAggregateItem{Label: userLabel}
			userAggregate[userLabel] = userSummary
		}
		aggregateSummary(userSummary, record)

		instanceLabel := "Unassigned"
		instanceMeta := ""
		if record.InstanceID != nil {
			instanceLabel = instanceNames[*record.InstanceID]
			if instanceLabel == "" {
				instanceLabel = fmt.Sprintf("Instance #%d", *record.InstanceID)
			}
			instanceMeta = fmt.Sprintf("ID %d", *record.InstanceID)
		}
		instanceSummary := instanceAggregate[instanceLabel]
		if instanceSummary == nil {
			instanceSummary = &CostAggregateItem{Label: instanceLabel, Meta: instanceMeta}
			instanceAggregate[instanceLabel] = instanceSummary
		}
		aggregateSummary(instanceSummary, record)

		overview.RecentRecords = append(overview.RecentRecords, CostRecordView{
			ID:               record.ID,
			TraceID:          record.TraceID,
			UserID:           record.UserID,
			Username:         username,
			InstanceID:       record.InstanceID,
			InstanceName:     instanceNames[valueOrZero(record.InstanceID)],
			ModelName:        record.ModelName,
			ProviderType:     record.ProviderType,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			EstimatedCost:    record.EstimatedCost,
			InternalCost:     record.InternalCost,
			Currency:         record.Currency,
			RecordedAt:       record.RecordedAt,
		})
	}

	if len(overview.RecentRecords) > 0 {
		start, end := paginateBounds(len(overview.RecentRecords), page, limit)
		if start < len(overview.RecentRecords) {
			overview.RecentRecords = overview.RecentRecords[start:end]
		} else {
			overview.RecentRecords = []CostRecordView{}
		}
	}

	overview.TopModels = s.completeModelBreakdowns(modelTotals)
	overview.TopUsers = s.completeUserBreakdowns(userTotals)
	overview.UserSummary = sortAggregateItems(userAggregate, 6)
	overview.InstanceSummary = sortAggregateItems(instanceAggregate, 6)
	overview.DailyTrend = aggregateDailyTrend(filteredRecords, 7)
	overview.ModelTrends = aggregateNamedTrends(filteredRecords, usernames, overview.TopModels, "model", 7)
	overview.UserTrends = aggregateNamedTrends(filteredRecords, usernames, overview.TopUsers, "user", 7)
	return overview, nil
}

func (s *aiObservabilityService) completeModelBreakdowns(modelTotals map[string]*CostBreakdownItem) []CostBreakdownItem {
	items := cloneBreakdowns(modelTotals)

	activeModels, err := s.llmModelRepo.ListActive()
	if err == nil {
		existing := make(map[string]struct{}, len(items))
		for _, item := range items {
			existing[item.Label] = struct{}{}
		}
		for _, model := range activeModels {
			if _, ok := existing[model.DisplayName]; ok {
				continue
			}
			items = append(items, CostBreakdownItem{Label: model.DisplayName})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].EstimatedCost == items[j].EstimatedCost {
			if items[i].TotalTokens == items[j].TotalTokens {
				return items[i].Label < items[j].Label
			}
			return items[i].TotalTokens > items[j].TotalTokens
		}
		return items[i].EstimatedCost > items[j].EstimatedCost
	})

	if len(items) > 5 {
		items = items[:5]
	}
	return items
}

func (s *aiObservabilityService) completeUserBreakdowns(userTotals map[string]*CostBreakdownItem) []CostBreakdownItem {
	items := cloneBreakdowns(userTotals)

	users, err := s.userRepo.List(0, 1000)
	if err == nil {
		existing := make(map[string]struct{}, len(items))
		for _, item := range items {
			existing[item.Label] = struct{}{}
		}
		for _, user := range users {
			if !user.IsActive {
				continue
			}
			if _, ok := existing[user.Username]; ok {
				continue
			}
			items = append(items, CostBreakdownItem{Label: user.Username})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].EstimatedCost == items[j].EstimatedCost {
			if items[i].TotalTokens == items[j].TotalTokens {
				return items[i].Label < items[j].Label
			}
			return items[i].TotalTokens > items[j].TotalTokens
		}
		return items[i].EstimatedCost > items[j].EstimatedCost
	})

	if len(items) > 5 {
		items = items[:5]
	}
	return items
}

func cloneBreakdowns(items map[string]*CostBreakdownItem) []CostBreakdownItem {
	result := make([]CostBreakdownItem, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	return result
}

func sortAggregateItems(items map[string]*CostAggregateItem, maxItems int) []CostAggregateItem {
	result := make([]CostAggregateItem, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].EstimatedCost == result[j].EstimatedCost {
			if result[i].TotalTokens == result[j].TotalTokens {
				return result[i].Label < result[j].Label
			}
			return result[i].TotalTokens > result[j].TotalTokens
		}
		return result[i].EstimatedCost > result[j].EstimatedCost
	})
	if maxItems > 0 && len(result) > maxItems {
		result = result[:maxItems]
	}
	return result
}

func normalizePageLimit(page, limit, defaultLimit, maxLimit int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}

func expandFetchWindow(page, limit int) int {
	window := page * limit * 5
	if window < 200 {
		return 200
	}
	if window > 1000 {
		return 1000
	}
	return window
}

func paginateBounds(total, page, limit int) (int, int) {
	start := (page - 1) * limit
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > total {
		end = total
	}
	return start, end
}

func aggregateDailyTrend(records []models.CostRecord, days int) []CostTrendPoint {
	labels := recentDayLabels(days)
	byDay := make(map[string]*CostTrendPoint, len(labels))
	for _, day := range labels {
		byDay[day] = &CostTrendPoint{Day: day}
	}

	for _, record := range records {
		day := record.RecordedAt.Format("2006-01-02")
		point, ok := byDay[day]
		if !ok {
			continue
		}
		accumulateTrendPoint(point, record)
	}

	points := make([]CostTrendPoint, 0, len(labels))
	for _, day := range labels {
		points = append(points, *byDay[day])
	}
	return points
}

func aggregateNamedTrends(records []models.CostRecord, usernames map[int]string, topItems []CostBreakdownItem, dimension string, days int) []CostTrendSeries {
	if len(topItems) == 0 {
		return []CostTrendSeries{}
	}

	labels := recentDayLabels(days)
	allowed := make(map[string]struct{}, len(topItems))
	for _, item := range topItems {
		allowed[item.Label] = struct{}{}
	}

	seriesMap := make(map[string]map[string]*CostTrendPoint, len(topItems))
	for label := range allowed {
		seriesMap[label] = make(map[string]*CostTrendPoint, len(labels))
		for _, day := range labels {
			seriesMap[label][day] = &CostTrendPoint{Day: day}
		}
	}

	for _, record := range records {
		day := record.RecordedAt.Format("2006-01-02")
		var label string
		switch dimension {
		case "model":
			label = record.ModelName
		case "user":
			username := usernames[valueOrZero(record.UserID)]
			if username != "" {
				label = username
			} else if record.UserID != nil {
				label = fmt.Sprintf("User #%d", *record.UserID)
			} else {
				label = "Unknown"
			}
		default:
			continue
		}

		dayPoints, ok := seriesMap[label]
		if !ok {
			continue
		}
		point, ok := dayPoints[day]
		if !ok {
			continue
		}
		accumulateTrendPoint(point, record)
	}

	series := make([]CostTrendSeries, 0, min(len(topItems), 5))
	for _, item := range topItems {
		pointsByDay, ok := seriesMap[item.Label]
		if !ok {
			continue
		}
		points := make([]CostTrendPoint, 0, len(labels))
		for _, day := range labels {
			points = append(points, *pointsByDay[day])
		}
		series = append(series, CostTrendSeries{
			Label:  item.Label,
			Points: points,
		})
		if len(series) >= 5 {
			break
		}
	}
	return series
}

func accumulateTrendPoint(point *CostTrendPoint, record models.CostRecord) {
	point.PromptTokens += record.PromptTokens
	point.CompletionTokens += record.CompletionTokens
	point.TotalTokens += record.TotalTokens
	point.EstimatedCost += record.EstimatedCost
	point.InternalCost += record.InternalCost
}

func recentDayLabels(days int) []string {
	if days <= 0 {
		days = 7
	}
	labels := make([]string, 0, days)
	now := time.Now()
	for offset := days - 1; offset >= 0; offset-- {
		labels = append(labels, now.AddDate(0, 0, -offset).Format("2006-01-02"))
	}
	return labels
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *aiObservabilityService) loadUsernames(items []models.ModelInvocation) (map[int]string, error) {
	userIDs := make(map[int]struct{})
	for _, item := range items {
		if item.UserID != nil {
			userIDs[*item.UserID] = struct{}{}
		}
	}

	usernames := make(map[int]string, len(userIDs))
	for userID := range userIDs {
		user, err := s.userRepo.GetByID(userID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			usernames[userID] = user.Username
		}
	}
	return usernames, nil
}

func (s *aiObservabilityService) loadUsernamesForAudit(items []models.ModelInvocation, events []models.AuditEvent, costs []models.CostRecord) (map[int]string, error) {
	userIDs := make(map[int]struct{})
	for _, item := range items {
		if item.UserID != nil {
			userIDs[*item.UserID] = struct{}{}
		}
	}
	for _, event := range events {
		if event.UserID != nil {
			userIDs[*event.UserID] = struct{}{}
		}
	}
	for _, cost := range costs {
		if cost.UserID != nil {
			userIDs[*cost.UserID] = struct{}{}
		}
	}

	usernames := make(map[int]string, len(userIDs))
	for userID := range userIDs {
		user, err := s.userRepo.GetByID(userID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			usernames[userID] = user.Username
		}
	}
	return usernames, nil
}

func (s *aiObservabilityService) resolveTraceUsername(
	invocations []models.ModelInvocation,
	events []models.AuditEvent,
	costs []models.CostRecord,
	messages []models.ChatMessageRecord,
) string {
	userID := firstTraceUserID(invocations, events, costs, messages)
	if userID == nil {
		return ""
	}

	user, err := s.userRepo.GetByID(*userID)
	if err != nil || user == nil {
		return ""
	}
	return user.Username
}

func firstTraceUserID(
	invocations []models.ModelInvocation,
	events []models.AuditEvent,
	costs []models.CostRecord,
	messages []models.ChatMessageRecord,
) *int {
	for _, item := range invocations {
		if item.UserID != nil {
			return item.UserID
		}
	}
	for _, event := range events {
		if event.UserID != nil {
			return event.UserID
		}
	}
	for _, cost := range costs {
		if cost.UserID != nil {
			return cost.UserID
		}
	}
	for _, message := range messages {
		if message.UserID != nil {
			return message.UserID
		}
	}
	return nil
}

func synthesizeInvocationsFromTrace(
	traceID string,
	events []models.AuditEvent,
	costs []models.CostRecord,
	messages []models.ChatMessageRecord,
) []models.ModelInvocation {
	if len(events) == 0 && len(costs) == 0 && len(messages) == 0 {
		return []models.ModelInvocation{}
	}

	requestID := firstTraceRequestID(events, costs, messages)
	sessionID := firstTraceSessionID(events, costs, messages)
	userID := firstTraceUserID(nil, events, costs, messages)
	instanceID := firstTraceInstanceID(events, costs, messages)
	requestedModel := inferRequestedModel(events)
	actualModel := inferActualModel(events, costs)
	status := inferTraceStatus(events, costs)
	createdAt := firstTraceTimestamp(events, costs, messages)

	var errorMessage *string
	if status == models.ModelInvocationStatusBlocked || status == models.ModelInvocationStatusFailed {
		if message := firstRelevantEventMessage(events); message != "" {
			errorMessage = &message
		}
	}

	return []models.ModelInvocation{
		{
			ID:                  0,
			TraceID:             traceID,
			SessionID:           sessionID,
			RequestID:           requestID,
			UserID:              userID,
			InstanceID:          instanceID,
			ProviderType:        "gateway",
			RequestedModel:      requestedModel,
			ActualProviderModel: actualModel,
			TrafficClass:        models.TrafficClassLLM,
			Status:              status,
			ErrorMessage:        errorMessage,
			CreatedAt:           createdAt,
		},
	}
}

func firstTraceRequestID(events []models.AuditEvent, costs []models.CostRecord, messages []models.ChatMessageRecord) string {
	for _, event := range events {
		if event.RequestID != nil && strings.TrimSpace(*event.RequestID) != "" {
			return strings.TrimSpace(*event.RequestID)
		}
	}
	for _, cost := range costs {
		if cost.RequestID != nil && strings.TrimSpace(*cost.RequestID) != "" {
			return strings.TrimSpace(*cost.RequestID)
		}
	}
	for _, message := range messages {
		if message.RequestID != nil && strings.TrimSpace(*message.RequestID) != "" {
			return strings.TrimSpace(*message.RequestID)
		}
	}
	return "synthetic"
}

func firstTraceSessionID(events []models.AuditEvent, costs []models.CostRecord, messages []models.ChatMessageRecord) *string {
	for _, event := range events {
		if event.SessionID != nil && strings.TrimSpace(*event.SessionID) != "" {
			value := strings.TrimSpace(*event.SessionID)
			return &value
		}
	}
	for _, cost := range costs {
		if cost.SessionID != nil && strings.TrimSpace(*cost.SessionID) != "" {
			value := strings.TrimSpace(*cost.SessionID)
			return &value
		}
	}
	for _, message := range messages {
		if strings.TrimSpace(message.SessionID) != "" {
			value := strings.TrimSpace(message.SessionID)
			return &value
		}
	}
	return nil
}

func firstTraceInstanceID(events []models.AuditEvent, costs []models.CostRecord, messages []models.ChatMessageRecord) *int {
	for _, event := range events {
		if event.InstanceID != nil {
			return event.InstanceID
		}
	}
	for _, cost := range costs {
		if cost.InstanceID != nil {
			return cost.InstanceID
		}
	}
	for _, message := range messages {
		if message.InstanceID != nil {
			return message.InstanceID
		}
	}
	return nil
}

func firstTraceTimestamp(events []models.AuditEvent, costs []models.CostRecord, messages []models.ChatMessageRecord) time.Time {
	for _, event := range events {
		if !event.CreatedAt.IsZero() {
			return event.CreatedAt
		}
	}
	for _, cost := range costs {
		if !cost.RecordedAt.IsZero() {
			return cost.RecordedAt
		}
	}
	for _, message := range messages {
		if !message.CreatedAt.IsZero() {
			return message.CreatedAt
		}
	}
	return time.Now()
}

func inferRequestedModel(events []models.AuditEvent) string {
	for _, event := range events {
		if strings.Contains(event.Message, "model ") {
			parts := strings.Split(event.Message, "model ")
			candidate := strings.TrimSpace(parts[len(parts)-1])
			if candidate != "" {
				return candidate
			}
		}
	}
	return "Auto"
}

func inferActualModel(events []models.AuditEvent, costs []models.CostRecord) string {
	for _, cost := range costs {
		if strings.TrimSpace(cost.ModelName) != "" {
			return cost.ModelName
		}
	}
	for _, event := range events {
		if strings.Contains(event.EventType, "rerouted") {
			return "secure-route"
		}
	}
	return "not-routed"
}

func inferTraceStatus(events []models.AuditEvent, costs []models.CostRecord) string {
	if len(costs) > 0 {
		return models.ModelInvocationStatusCompleted
	}
	for _, event := range events {
		switch {
		case strings.Contains(event.EventType, "completed"):
			return models.ModelInvocationStatusCompleted
		case strings.Contains(event.EventType, "blocked"):
			return models.ModelInvocationStatusBlocked
		case strings.Contains(event.EventType, "failed"):
			return models.ModelInvocationStatusFailed
		}
	}
	return models.ModelInvocationStatusPending
}

func firstRelevantEventMessage(events []models.AuditEvent) string {
	for _, event := range events {
		if strings.Contains(event.EventType, "blocked") || strings.Contains(event.EventType, "failed") {
			return strings.TrimSpace(event.Message)
		}
	}
	return ""
}

func aggregateInvocationsByTrace(items []models.ModelInvocation, usernames map[int]string) []AuditListItem {
	type traceAggregate struct {
		item         AuditListItem
		hasCompleted bool
		hasBlocked   bool
		hasFailed    bool
		hasPending   bool
	}

	byTrace := make(map[string]*traceAggregate, len(items))
	for _, invocation := range items {
		traceID := strings.TrimSpace(invocation.TraceID)
		if traceID == "" {
			continue
		}

		aggregate := byTrace[traceID]
		if aggregate == nil {
			requestID := strings.TrimSpace(invocation.RequestID)
			aggregate = &traceAggregate{
				item: AuditListItem{
					TraceID:             traceID,
					RequestID:           requestID,
					SessionID:           invocation.SessionID,
					UserID:              invocation.UserID,
					Username:            usernames[valueOrZero(invocation.UserID)],
					InstanceID:          invocation.InstanceID,
					RequestedModel:      invocation.RequestedModel,
					ActualProviderModel: invocation.ActualProviderModel,
					ProviderType:        invocation.ProviderType,
					CreatedAt:           invocation.CreatedAt,
					CompletedAt:         invocation.CompletedAt,
				},
			}
			byTrace[traceID] = aggregate
		}

		if invocation.CreatedAt.Before(aggregate.item.CreatedAt) {
			aggregate.item.CreatedAt = invocation.CreatedAt
			if trimmed := strings.TrimSpace(invocation.RequestID); trimmed != "" {
				aggregate.item.RequestID = trimmed
			}
		}
		if aggregate.item.CompletedAt == nil || (invocation.CompletedAt != nil && invocation.CompletedAt.After(*aggregate.item.CompletedAt)) {
			aggregate.item.CompletedAt = invocation.CompletedAt
		}

		if aggregate.item.SessionID == nil && invocation.SessionID != nil {
			aggregate.item.SessionID = invocation.SessionID
		}
		if aggregate.item.UserID == nil && invocation.UserID != nil {
			aggregate.item.UserID = invocation.UserID
			aggregate.item.Username = usernames[valueOrZero(invocation.UserID)]
		}
		if aggregate.item.Username == "" {
			aggregate.item.Username = usernames[valueOrZero(invocation.UserID)]
		}
		if aggregate.item.InstanceID == nil && invocation.InstanceID != nil {
			aggregate.item.InstanceID = invocation.InstanceID
		}
		if strings.TrimSpace(aggregate.item.RequestedModel) == "" && strings.TrimSpace(invocation.RequestedModel) != "" {
			aggregate.item.RequestedModel = invocation.RequestedModel
		}
		if strings.TrimSpace(invocation.ActualProviderModel) != "" {
			aggregate.item.ActualProviderModel = invocation.ActualProviderModel
		}
		if strings.TrimSpace(invocation.ProviderType) != "" {
			aggregate.item.ProviderType = invocation.ProviderType
		}
		if aggregate.item.ErrorMessage == nil && invocation.ErrorMessage != nil && strings.TrimSpace(*invocation.ErrorMessage) != "" {
			aggregate.item.ErrorMessage = invocation.ErrorMessage
		}

		aggregate.item.PromptTokens += invocation.PromptTokens
		aggregate.item.CompletionTokens += invocation.CompletionTokens
		aggregate.item.TotalTokens += invocation.TotalTokens
		if invocation.LatencyMs != nil {
			if aggregate.item.LatencyMs == nil {
				aggregate.item.LatencyMs = pointerToInt(0)
			}
			totalLatency := *aggregate.item.LatencyMs + *invocation.LatencyMs
			aggregate.item.LatencyMs = pointerToInt(totalLatency)
		}

		switch strings.ToLower(strings.TrimSpace(invocation.Status)) {
		case models.ModelInvocationStatusCompleted:
			aggregate.hasCompleted = true
		case models.ModelInvocationStatusBlocked:
			aggregate.hasBlocked = true
		case models.ModelInvocationStatusFailed:
			aggregate.hasFailed = true
		default:
			aggregate.hasPending = true
		}
	}

	result := make([]AuditListItem, 0, len(byTrace))
	for _, aggregate := range byTrace {
		switch {
		case aggregate.hasFailed:
			aggregate.item.Status = models.ModelInvocationStatusFailed
		case aggregate.hasBlocked:
			aggregate.item.Status = models.ModelInvocationStatusBlocked
		case aggregate.hasPending && !aggregate.hasCompleted:
			aggregate.item.Status = models.ModelInvocationStatusPending
		default:
			aggregate.item.Status = models.ModelInvocationStatusCompleted
		}
		result = append(result, aggregate.item)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

type auditPayloadRequest struct {
	Messages []auditPayloadMessage `json:"messages"`
}

type auditPayloadResponse struct {
	Choices []struct {
		Index   int                 `json:"index"`
		Message auditPayloadMessage `json:"message"`
	} `json:"choices"`
}

type auditStreamChunk struct {
	Choices []struct {
		Index int                 `json:"index"`
		Delta auditPayloadMessage `json:"delta"`
	} `json:"choices"`
}

type auditPayloadMessage struct {
	Role       string          `json:"role"`
	Content    interface{}     `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []auditToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type auditToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Index    *int                   `json:"index,omitempty"`
	Function *auditToolCallFunction `json:"function,omitempty"`
}

type auditToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func buildAuditFlowNodes(invocations []models.ModelInvocation) []AuditFlowNode {
	if len(invocations) == 0 {
		return []AuditFlowNode{}
	}

	ordered := append([]models.ModelInvocation(nil), invocations...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})

	nodes := make([]AuditFlowNode, 0, len(ordered)*3)
	transcript := make([]auditPayloadMessage, 0)
	for _, invocation := range ordered {
		requestMessages := parseAuditRequestMessages(invocation.RequestPayload)
		newMessages := diffAuditMessages(transcript, requestMessages)
		nodes = append(nodes, buildRequestMessageNodes(newMessages, invocation)...)
		nodes = append(nodes, buildInvocationNode(invocation, sanitizeAuditRequestPayload(invocation.RequestPayload)))

		responseMessages := parseAuditResponseMessages(invocation.ResponsePayload)
		nodes = append(nodes, buildResponseMessageNodes(responseMessages, invocation)...)
		transcript = append(cloneAuditMessages(requestMessages), cloneAuditMessages(responseMessages)...)
	}

	return nodes
}

func parseAuditRequestMessages(payload *string) []auditPayloadMessage {
	if payload == nil || strings.TrimSpace(*payload) == "" {
		return []auditPayloadMessage{}
	}

	var request auditPayloadRequest
	if err := json.Unmarshal([]byte(*payload), &request); err != nil {
		return []auditPayloadMessage{}
	}
	return currentAuditTurnMessages(request.Messages)
}

func parseAuditResponseMessages(payload *string) []auditPayloadMessage {
	if payload == nil {
		return []auditPayloadMessage{}
	}

	trimmed := strings.TrimSpace(*payload)
	if trimmed == "" {
		return []auditPayloadMessage{}
	}
	if strings.HasPrefix(trimmed, "data:") {
		return parseAuditStreamMessages(trimmed)
	}

	var response auditPayloadResponse
	if err := json.Unmarshal([]byte(trimmed), &response); err != nil {
		return []auditPayloadMessage{}
	}

	items := make([]auditPayloadMessage, 0, len(response.Choices))
	for _, choice := range response.Choices {
		items = append(items, choice.Message)
	}
	return items
}

func parseAuditStreamMessages(payload string) []auditPayloadMessage {
	lines := strings.Split(payload, "\n")
	byChoice := make(map[int]*auditPayloadMessage)
	order := make([]int, 0)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		chunkPayload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if chunkPayload == "" || chunkPayload == "[DONE]" {
			continue
		}

		var chunk auditStreamChunk
		if err := json.Unmarshal([]byte(chunkPayload), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			message := byChoice[choice.Index]
			if message == nil {
				message = &auditPayloadMessage{Role: "assistant"}
				byChoice[choice.Index] = message
				order = append(order, choice.Index)
			}
			if role := strings.TrimSpace(choice.Delta.Role); role != "" {
				message.Role = role
			}
			message.Content = mergeAuditContent(message.Content, choice.Delta.Content)
			mergeAuditToolCalls(&message.ToolCalls, choice.Delta.ToolCalls)
		}
	}

	sort.Ints(order)
	items := make([]auditPayloadMessage, 0, len(order))
	for _, index := range order {
		message := byChoice[index]
		if message == nil || !hasAuditMessageBody(*message) {
			continue
		}
		items = append(items, *message)
	}
	return items
}

func diffAuditMessages(existing, current []auditPayloadMessage) []auditPayloadMessage {
	limit := len(existing)
	if len(current) < limit {
		limit = len(current)
	}

	matched := 0
	for matched < limit && auditMessagesEquivalent(existing[matched], current[matched]) {
		matched++
	}

	return cloneAuditMessages(current[matched:])
}

func cloneAuditMessages(messages []auditPayloadMessage) []auditPayloadMessage {
	if len(messages) == 0 {
		return []auditPayloadMessage{}
	}

	cloned := make([]auditPayloadMessage, 0, len(messages))
	for _, message := range messages {
		copyMessage := message
		if len(message.ToolCalls) > 0 {
			copyMessage.ToolCalls = append([]auditToolCall(nil), message.ToolCalls...)
		}
		cloned = append(cloned, copyMessage)
	}
	return cloned
}

func buildRequestMessageNodes(messages []auditPayloadMessage, invocation models.ModelInvocation) []AuditFlowNode {
	nodes := make([]AuditFlowNode, 0, len(messages))
	for index, message := range messages {
		content := strings.TrimSpace(flattenAuditMessageContent(message.Content))
		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case "user":
			nodes = append(nodes, AuditFlowNode{
				ID:            fmt.Sprintf("request-user-%d-%d", invocation.ID, index),
				Kind:          "user_message",
				Title:         "User Prompt",
				RequestID:     invocation.RequestID,
				InvocationID:  pointerToInt(invocation.ID),
				Model:         invocation.RequestedModel,
				OutputPayload: content,
				CreatedAt:     invocation.CreatedAt,
			})
		case "tool":
			title := strings.TrimSpace(message.Name)
			if title == "" {
				title = "Tool Output"
			}
			summary := strings.TrimSpace(message.ToolCallID)
			nodes = append(nodes, AuditFlowNode{
				ID:            fmt.Sprintf("request-tool-%d-%d", invocation.ID, index),
				Kind:          "tool_output",
				Title:         title,
				RequestID:     invocation.RequestID,
				InvocationID:  pointerToInt(invocation.ID),
				Model:         invocation.RequestedModel,
				Summary:       summary,
				OutputPayload: content,
				CreatedAt:     invocation.CreatedAt,
			})
		}
	}
	return nodes
}

func buildInvocationNode(invocation models.ModelInvocation, inputPayload string) AuditFlowNode {
	summary := strings.TrimSpace(invocation.RequestedModel)
	if actual := strings.TrimSpace(invocation.ActualProviderModel); actual != "" {
		if summary != "" {
			summary = fmt.Sprintf("%s -> %s", summary, actual)
		} else {
			summary = actual
		}
	}

	return AuditFlowNode{
		ID:            fmt.Sprintf("invocation-%d", invocation.ID),
		Kind:          "llm_call",
		Title:         "LLM Call",
		RequestID:     invocation.RequestID,
		InvocationID:  pointerToInt(invocation.ID),
		Model:         invocation.ActualProviderModel,
		Status:        invocation.Status,
		Summary:       summary,
		InputPayload:  inputPayload,
		OutputPayload: valueOrPayload(invocation.ResponsePayload),
		CreatedAt:     invocation.CreatedAt,
	}
}

func buildResponseMessageNodes(messages []auditPayloadMessage, invocation models.ModelInvocation) []AuditFlowNode {
	nodes := make([]AuditFlowNode, 0, len(messages))
	for index, message := range messages {
		if len(message.ToolCalls) > 0 {
			for toolIndex, toolCall := range message.ToolCalls {
				title := "Tool Call"
				if toolCall.Function != nil && strings.TrimSpace(toolCall.Function.Name) != "" {
					title = strings.TrimSpace(toolCall.Function.Name)
				}
				summary := strings.TrimSpace(toolCall.ID)
				inputPayload := ""
				if toolCall.Function != nil {
					inputPayload = strings.TrimSpace(toolCall.Function.Arguments)
				}
				nodes = append(nodes, AuditFlowNode{
					ID:           fmt.Sprintf("response-tool-call-%d-%d-%d", invocation.ID, index, toolIndex),
					Kind:         "tool_call",
					Title:        title,
					RequestID:    invocation.RequestID,
					InvocationID: pointerToInt(invocation.ID),
					Model:        invocation.ActualProviderModel,
					Summary:      summary,
					InputPayload: inputPayload,
					CreatedAt:    coalesceTime(invocation.CompletedAt, invocation.CreatedAt),
				})
			}
			continue
		}

		content := strings.TrimSpace(flattenAuditMessageContent(message.Content))
		if content == "" {
			continue
		}
		nodes = append(nodes, AuditFlowNode{
			ID:            fmt.Sprintf("response-assistant-%d-%d", invocation.ID, index),
			Kind:          "assistant_response",
			Title:         "Assistant Response",
			RequestID:     invocation.RequestID,
			InvocationID:  pointerToInt(invocation.ID),
			Model:         invocation.ActualProviderModel,
			OutputPayload: content,
			CreatedAt:     coalesceTime(invocation.CompletedAt, invocation.CreatedAt),
		})
	}
	return nodes
}

func mergeAuditContent(existing, incoming interface{}) interface{} {
	existingText := strings.TrimSpace(flattenAuditMessageContent(existing))
	incomingText := strings.TrimSpace(flattenAuditMessageContent(incoming))
	switch {
	case existingText == "":
		return incoming
	case incomingText == "":
		return existing
	default:
		return existingText + incomingText
	}
}

func currentAuditTurnMessages(messages []auditPayloadMessage) []auditPayloadMessage {
	startIndex := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			startIndex = index
			break
		}
	}
	return cloneAuditMessages(messages[startIndex:])
}

func sanitizeAuditRequestPayload(payload *string) string {
	if payload == nil {
		return ""
	}

	trimmed := strings.TrimSpace(*payload)
	if trimmed == "" {
		return ""
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return trimmed
	}

	currentTurn := parseAuditRequestMessages(payload)
	if len(currentTurn) == 0 {
		return trimmed
	}

	encodedMessages, err := json.Marshal(currentTurn)
	if err != nil {
		return trimmed
	}
	raw["messages"] = encodedMessages

	sanitized, err := json.Marshal(raw)
	if err != nil {
		return trimmed
	}
	return string(sanitized)
}

func mergeAuditToolCalls(target *[]auditToolCall, incoming []auditToolCall) {
	if len(incoming) == 0 {
		return
	}

	for _, item := range incoming {
		index := len(*target)
		if item.Index != nil && *item.Index >= 0 {
			index = *item.Index
		}
		for len(*target) <= index {
			*target = append(*target, auditToolCall{})
		}

		current := &(*target)[index]
		if trimmed := strings.TrimSpace(item.ID); trimmed != "" {
			current.ID = trimmed
		}
		if trimmed := strings.TrimSpace(item.Type); trimmed != "" {
			current.Type = trimmed
		}
		if item.Function != nil {
			if current.Function == nil {
				current.Function = &auditToolCallFunction{}
			}
			if trimmed := strings.TrimSpace(item.Function.Name); trimmed != "" && current.Function.Name == "" {
				current.Function.Name = trimmed
			}
			if strings.TrimSpace(item.Function.Arguments) != "" {
				current.Function.Arguments += item.Function.Arguments
			}
		}
	}
}

func auditMessagesEquivalent(left, right auditPayloadMessage) bool {
	if !strings.EqualFold(strings.TrimSpace(left.Role), strings.TrimSpace(right.Role)) {
		return false
	}
	if strings.TrimSpace(left.Name) != strings.TrimSpace(right.Name) {
		return false
	}
	if strings.TrimSpace(left.ToolCallID) != strings.TrimSpace(right.ToolCallID) {
		return false
	}
	if strings.TrimSpace(flattenAuditMessageContent(left.Content)) != strings.TrimSpace(flattenAuditMessageContent(right.Content)) {
		return false
	}
	if len(left.ToolCalls) != len(right.ToolCalls) {
		return false
	}
	for index := range left.ToolCalls {
		if !auditToolCallsEquivalent(left.ToolCalls[index], right.ToolCalls[index]) {
			return false
		}
	}
	return true
}

func auditToolCallsEquivalent(left, right auditToolCall) bool {
	if strings.TrimSpace(left.ID) != strings.TrimSpace(right.ID) || strings.TrimSpace(left.Type) != strings.TrimSpace(right.Type) {
		return false
	}
	leftName, leftArgs := "", ""
	if left.Function != nil {
		leftName = strings.TrimSpace(left.Function.Name)
		leftArgs = strings.TrimSpace(left.Function.Arguments)
	}
	rightName, rightArgs := "", ""
	if right.Function != nil {
		rightName = strings.TrimSpace(right.Function.Name)
		rightArgs = strings.TrimSpace(right.Function.Arguments)
	}
	return leftName == rightName && leftArgs == rightArgs
}

func hasAuditMessageBody(message auditPayloadMessage) bool {
	return strings.TrimSpace(flattenAuditMessageContent(message.Content)) != "" || len(message.ToolCalls) > 0
}

func flattenAuditMessageContent(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if text := flattenAuditStructuredContentPart(item); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	case map[string]interface{}:
		if text := flattenAuditStructuredContentPart(value); text != "" {
			return text
		}
	}

	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(data)
}

func flattenAuditStructuredContentPart(content interface{}) string {
	part, ok := content.(map[string]interface{})
	if !ok {
		return ""
	}

	if text, ok := part["text"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := part["input_text"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := part["output_text"].(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func coalesceTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	return fallback
}

func valueOrPayload(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func aggregateBreakdown(target *CostBreakdownItem, record models.CostRecord) {
	target.PromptTokens += record.PromptTokens
	target.CompletionTokens += record.CompletionTokens
	target.TotalTokens += record.TotalTokens
	target.EstimatedCost += record.EstimatedCost
	target.InternalCost += record.InternalCost
}

func aggregateSummary(target *CostAggregateItem, record models.CostRecord) {
	target.PromptTokens += record.PromptTokens
	target.CompletionTokens += record.CompletionTokens
	target.TotalTokens += record.TotalTokens
	target.EstimatedCost += record.EstimatedCost
	target.InternalCost += record.InternalCost
}

func sortBreakdowns(items map[string]*CostBreakdownItem) []CostBreakdownItem {
	result := make([]CostBreakdownItem, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].EstimatedCost == result[j].EstimatedCost {
			return result[i].TotalTokens > result[j].TotalTokens
		}
		return result[i].EstimatedCost > result[j].EstimatedCost
	})
	return result
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func valueOrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueOrEventLabel(eventType string) string {
	trimmed := strings.TrimSpace(eventType)
	if trimmed == "" {
		return "gateway.event"
	}
	return trimmed
}

func inferAuditStatusFromEvent(event models.AuditEvent) string {
	switch {
	case strings.Contains(event.EventType, "completed"):
		return models.ModelInvocationStatusCompleted
	case strings.Contains(event.EventType, "blocked"):
		return models.ModelInvocationStatusBlocked
	case strings.Contains(event.EventType, "failed"):
		return models.ModelInvocationStatusFailed
	default:
		return models.ModelInvocationStatusPending
	}
}

func aggregateRecentCostByTrace(costs []models.CostRecord) map[string]*models.CostRecord {
	byTrace := make(map[string]*models.CostRecord, len(costs))
	for _, cost := range costs {
		existing := byTrace[cost.TraceID]
		if existing == nil {
			copy := cost
			byTrace[cost.TraceID] = &copy
			continue
		}
		existing.PromptTokens += cost.PromptTokens
		existing.CompletionTokens += cost.CompletionTokens
		existing.TotalTokens += cost.TotalTokens
		existing.EstimatedCost += cost.EstimatedCost
		existing.InternalCost += cost.InternalCost
		if existing.UserID == nil && cost.UserID != nil {
			existing.UserID = cost.UserID
		}
		if existing.InstanceID == nil && cost.InstanceID != nil {
			existing.InstanceID = cost.InstanceID
		}
		if strings.TrimSpace(existing.ModelName) == "" && strings.TrimSpace(cost.ModelName) != "" {
			existing.ModelName = cost.ModelName
		}
		if strings.TrimSpace(existing.ProviderType) == "" && strings.TrimSpace(cost.ProviderType) != "" {
			existing.ProviderType = cost.ProviderType
		}
	}
	return byTrace
}

func firstAvailableUserID(primary *int, cost *models.CostRecord) *int {
	if primary != nil {
		return primary
	}
	if cost != nil {
		return cost.UserID
	}
	return nil
}

func firstAvailableInstanceID(primary *int, cost *models.CostRecord) *int {
	if primary != nil {
		return primary
	}
	if cost != nil {
		return cost.InstanceID
	}
	return nil
}

func inferActualProviderLabel(event models.AuditEvent, cost *models.CostRecord) string {
	if cost != nil && strings.TrimSpace(cost.ModelName) != "" {
		return cost.ModelName
	}
	return valueOrEventLabel(event.EventType)
}

func valueOrCostPromptTokens(cost *models.CostRecord) int {
	if cost == nil {
		return 0
	}
	return cost.PromptTokens
}

func valueOrCostCompletionTokens(cost *models.CostRecord) int {
	if cost == nil {
		return 0
	}
	return cost.CompletionTokens
}

func valueOrCostTotalTokens(cost *models.CostRecord) int {
	if cost == nil {
		return 0
	}
	return cost.TotalTokens
}

func pointerToInt(value int) *int {
	return &value
}
