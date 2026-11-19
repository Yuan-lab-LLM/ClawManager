package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type tracePageRepository interface {
	TracePage(context.Context, string, string, int) (*repository.TracePageResult, error)
	TraceBody(context.Context, string, string, int, int) (*repository.TraceBodyChunk, error)
}
type TraceBodyLocator struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

const (
	TraceFlowBatchLimit    = 8
	traceFlowReadBytes     = 8 << 20
	traceFlowPreviousBytes = 2 << 20
	traceFlowResponseBytes = 256 << 10
	traceFlowFieldBytes    = 2 << 10
	traceFlowNodeLimit     = 128
)

type AuditTraceFlowPreview struct {
	FlowNodes  []AuditFlowNode `json:"flow_nodes"`
	Processed  int             `json:"processed"`
	Incomplete bool            `json:"incomplete"`
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

func (s *aiObservabilityService) GetTraceFlow(ctx context.Context, trace string, ids []int, previousID int) (*AuditTraceFlowPreview, error) {
	if trace == "" || len(trace) > 100 || len(ids) == 0 || len(ids) > TraceFlowBatchLimit {
		return nil, fmt.Errorf("invalid trace preview request")
	}
	repo, ok := s.invocationRepo.(interface {
		TraceFlowInvocation(context.Context, string, int, *int64) (models.ModelInvocation, error)
	})
	if !ok {
		return nil, fmt.Errorf("trace preview unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := &AuditTraceFlowPreview{FlowNodes: []AuditFlowNode{}}
	var transcript []auditPayloadMessage
	var previous models.ModelInvocation
	if previousID != 0 {
		remaining := int64(traceFlowPreviousBytes)
		var err error
		previous, err = repo.TraceFlowInvocation(ctx, trace, previousID, &remaining)
		if err != nil || !hasTraceFlowBodies(previous) {
			result.Incomplete = true
		} else {
			transcript = append(parseAuditRequestMessages(previous.RequestPayload), parseAuditResponseMessages(previous.ResponsePayload)...)
		}
	}
	remainingRead := int64(traceFlowReadBytes)
	remainingResponse := traceFlowResponseBytes - 256 // Leave room for the response envelope.
	for _, id := range ids {
		if result.Processed > 0 && (len(result.FlowNodes) == traceFlowNodeLimit || remainingResponse < 1024) {
			break
		}
		readBefore := remainingRead
		invocation, err := repo.TraceFlowInvocation(ctx, trace, id, &remainingRead)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			if errors.Is(err, repository.ErrTraceFlowBudget) && readBefore < traceFlowReadBytes && result.Processed > 0 {
				break
			}
			result.Processed++
			result.Incomplete = true
			transcript = nil
			continue
		}
		if result.Processed == 0 && previousID != 0 && (previous.CreatedAt.After(invocation.CreatedAt) || (previous.CreatedAt.Equal(invocation.CreatedAt) && previous.ID >= id)) {
			transcript = nil
			result.Incomplete = true
		}
		var nodes []AuditFlowNode
		nodes, transcript = buildAuditInvocationFlow(invocation, transcript, false)
		preview, used, limited := boundTraceFlowNodes(nodes, remainingResponse, traceFlowNodeLimit-len(result.FlowNodes))
		if limited && result.Processed > 0 {
			break
		}
		result.FlowNodes = append(result.FlowNodes, preview...)
		remainingResponse -= used
		result.Processed++
		result.Incomplete = result.Incomplete || limited || !hasTraceFlowBodies(invocation)
	}
	return result, nil
}

func hasTraceFlowBodies(invocation models.ModelInvocation) bool {
	return invocation.RequestPayload != nil && strings.TrimSpace(*invocation.RequestPayload) != "" &&
		invocation.ResponsePayload != nil && strings.TrimSpace(*invocation.ResponsePayload) != ""
}

func boundTraceFlowNodes(nodes []AuditFlowNode, availableBytes, availableNodes int) (preview []AuditFlowNode, used int, limited bool) {
	// Keep the invocation card even when its preceding messages fill the preview.
	llmIndex := slices.IndexFunc(nodes, func(node AuditFlowNode) bool { return node.Kind == "llm_call" })
	llm, llmBytes := traceFlowPreviewNode(nodes[llmIndex])
	if llmBytes > availableBytes || availableNodes < 1 {
		return nil, 0, true
	}
	used = llmBytes
	remainingNodes := availableNodes - 1
	for i, node := range nodes {
		if i == llmIndex {
			preview = append(preview, llm)
			continue
		}
		if remainingNodes == 0 || limited {
			limited = true
			continue
		}
		node, size := traceFlowPreviewNode(node)
		if used+size > availableBytes {
			limited = true
			continue
		}
		preview = append(preview, node)
		used += size
		remainingNodes--
	}
	return preview, used, limited
}

func traceFlowPreviewNode(node AuditFlowNode) (AuditFlowNode, int) {
	for _, value := range []*string{&node.Title, &node.RequestID, &node.Model, &node.Status, &node.Summary, &node.InputPayload, &node.OutputPayload} {
		if len(*value) > traceFlowFieldBytes {
			end := traceFlowFieldBytes
			for !utf8.RuneStart((*value)[end]) {
				end--
			}
			*value = (*value)[:end]
			node.PreviewTruncated = true
		}
	}
	raw, _ := json.Marshal(node)
	return node, len(raw) + 1
}
