package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type tracePageRepository interface {
	TracePage(context.Context, string, string, int) (*repository.TracePageResult, error)
	TraceBody(context.Context, string, string, int, int) (*repository.TraceBodyChunk, error)
}
type TraceBodyLocator struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

func (s *aiObservabilityService) GetTracePage(trace, cursor string, limit int) (*AuditTraceDetail, error) {
	repo, ok := s.invocationRepo.(tracePageRepository)
	if !ok {
		return nil, fmt.Errorf("trace pagination unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	page, e := repo.TracePage(ctx, trace, cursor, limit)
	if e != nil {
		return nil, e
	}
	result := &AuditTraceDetail{TraceID: trace, NextCursor: page.NextCursor, Invocations: []models.ModelInvocation{}, AuditEvents: []models.AuditEvent{}, CostRecords: []models.CostRecord{}, RiskHits: []models.RiskHit{}, Messages: []models.ChatMessageRecord{}}
	for _, record := range page.Records {
		result.BodyRecords = append(result.BodyRecords, TraceBodyLocator{Kind: record.Kind, ID: record.ID})
		switch record.Kind {
		case "model_invocations":
			var v models.ModelInvocation
			e = json.Unmarshal(record.Data, &v)
			result.Invocations = append(result.Invocations, v)
		case "audit_events":
			var v models.AuditEvent
			e = json.Unmarshal(record.Data, &v)
			result.AuditEvents = append(result.AuditEvents, v)
		case "cost_records":
			var v models.CostRecord
			e = json.Unmarshal(record.Data, &v)
			result.CostRecords = append(result.CostRecords, v)
		case "risk_hits":
			var v models.RiskHit
			e = json.Unmarshal(record.Data, &v)
			result.RiskHits = append(result.RiskHits, v)
		case "chat_messages":
			var v models.ChatMessageRecord
			e = json.Unmarshal(record.Data, &v)
			result.Messages = append(result.Messages, v)
		}
		if e != nil {
			return nil, e
		}
	}
	result.Username = s.resolveTraceUsername(result.Invocations, result.AuditEvents, result.CostRecords, result.Messages)
	result.FlowNodes = buildAuditFlowNodes(result.Invocations)
	return result, nil
}
func (s *aiObservabilityService) GetTraceBody(trace, kind string, id, offset int) (*repository.TraceBodyChunk, error) {
	repo, ok := s.invocationRepo.(tracePageRepository)
	if !ok {
		return nil, fmt.Errorf("trace pagination unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return repo.TraceBody(ctx, trace, kind, id, offset)
}
