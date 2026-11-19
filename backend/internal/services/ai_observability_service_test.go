package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func auditToolLoopInvocations() []models.ModelInvocation {
	startedAt := time.Date(2026, 3, 26, 7, 53, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	secondStartedAt := startedAt.Add(10 * time.Second)
	secondCompletedAt := secondStartedAt.Add(2 * time.Second)

	return []models.ModelInvocation{
		{
			ID:                  11,
			TraceID:             "trc_weather",
			RequestID:           "req_1",
			RequestedModel:      "auto",
			ActualProviderModel: "gpt-weather",
			Status:              models.ModelInvocationStatusCompleted,
			RequestPayload:      stringPointer(`{"messages":[{"role":"user","content":"北京今天天气怎么样"}]}`),
			ResponsePayload:     stringPointer(`{"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]}}]}`),
			CreatedAt:           startedAt,
			CompletedAt:         &completedAt,
		},
		{
			ID:                  12,
			TraceID:             "trc_weather",
			RequestID:           "req_2",
			RequestedModel:      "auto",
			ActualProviderModel: "gpt-weather",
			Status:              models.ModelInvocationStatusCompleted,
			RequestPayload:      stringPointer(`{"messages":[{"role":"user","content":"北京今天天气怎么样"},{"role":"assistant","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},{"role":"tool","tool_call_id":"call_weather","name":"get_weather","content":"{\"temp_c\":25}"}]}`),
			ResponsePayload:     stringPointer(`{"choices":[{"index":0,"message":{"role":"assistant","content":"北京今天多云，25C。"}}]}`),
			CreatedAt:           secondStartedAt,
			CompletedAt:         &secondCompletedAt,
		},
	}
}

func TestBuildAuditFlowNodesBuildsSingleToolLoopTimeline(t *testing.T) {
	nodes := buildAuditFlowNodes(auditToolLoopInvocations())
	kinds := make([]string, 0, len(nodes))
	for _, node := range nodes {
		kinds = append(kinds, node.Kind)
	}

	expectedKinds := []string{
		"user_message",
		"llm_call",
		"tool_call",
		"tool_output",
		"llm_call",
		"assistant_response",
	}
	if !reflect.DeepEqual(kinds, expectedKinds) {
		t.Fatalf("expected flow kinds %v, got %v", expectedKinds, kinds)
	}

	if nodes[2].Title != "get_weather" {
		t.Fatalf("expected tool call title get_weather, got %q", nodes[2].Title)
	}
	if nodes[3].Summary != "call_weather" {
		t.Fatalf("expected tool output summary to include tool_call_id, got %q", nodes[3].Summary)
	}
	if nodes[5].OutputPayload != "北京今天多云，25C。" {
		t.Fatalf("expected final assistant response content, got %q", nodes[5].OutputPayload)
	}
}

type traceFlowFixture struct {
	repository.ModelInvocationRepository
	invocations map[int]models.ModelInvocation
	bytes       map[int]int64
}

func (f traceFlowFixture) TraceFlowInvocation(_ context.Context, trace string, id int, remaining *int64) (models.ModelInvocation, error) {
	invocation, ok := f.invocations[id]
	if !ok || trace != invocation.TraceID {
		return models.ModelInvocation{}, sql.ErrNoRows
	}
	if f.bytes[id] > *remaining {
		return models.ModelInvocation{}, repository.ErrTraceFlowBudget
	}
	*remaining -= f.bytes[id]
	return invocation, nil
}

func TestTraceFlowPreviewToolLoop(t *testing.T) {
	invocations := auditToolLoopInvocations()
	invocations[1].ResponsePayload = stringPointer("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"北京今天多云，25C。\"}}]}\n\ndata: [DONE]\n")
	service := aiObservabilityService{invocationRepo: traceFlowFixture{invocations: map[int]models.ModelInvocation{11: invocations[0], 12: invocations[1]}}}
	for _, tc := range []struct {
		name     string
		ids      []int
		previous int
		kinds    []string
	}{
		{"whole batch", []int{11, 12}, 0, []string{"user_message", "llm_call", "tool_call", "tool_output", "llm_call", "assistant_response"}},
		{"cross batch", []int{12}, 11, []string{"tool_output", "llm_call", "assistant_response"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preview, err := service.GetTraceFlow(context.Background(), "trc_weather", tc.ids, tc.previous)
			if err != nil || preview.Incomplete || preview.Processed != len(tc.ids) {
				t.Fatalf("preview %+v, %v", preview, err)
			}
			var kinds []string
			for _, node := range preview.FlowNodes {
				kinds = append(kinds, node.Kind)
				if node.Kind == "llm_call" && (node.InputPayload != "" || node.OutputPayload != "") {
					t.Fatal("LLM call exposes its raw payload")
				}
			}
			if !reflect.DeepEqual(kinds, tc.kinds) {
				t.Fatalf("expected %v, got %v", tc.kinds, kinds)
			}
			if got := preview.FlowNodes[len(preview.FlowNodes)-1].OutputPayload; got != "北京今天多云，25C。" {
				t.Fatalf("unexpected streamed response preview %q", got)
			}
		})
	}
}

func TestTraceFlowPreviewContinuesWithinReadBudget(t *testing.T) {
	invocations := auditToolLoopInvocations()
	service := aiObservabilityService{invocationRepo: traceFlowFixture{
		invocations: map[int]models.ModelInvocation{11: invocations[0], 12: invocations[1]},
		bytes:       map[int]int64{11: 2 << 20, 12: 7 << 20},
	}}
	first, err := service.GetTraceFlow(context.Background(), "trc_weather", []int{11, 12}, 0)
	if err != nil || first.Processed != 1 || first.Incomplete {
		t.Fatalf("unexpected first batch %+v %v", first, err)
	}
	second, err := service.GetTraceFlow(context.Background(), "trc_weather", []int{12}, 11)
	if err != nil || second.Processed != 1 || second.Incomplete || len(second.FlowNodes) != 3 || second.FlowNodes[0].Kind != "tool_output" {
		t.Fatalf("unexpected continuation %+v %v", second, err)
	}
	missing, err := service.GetTraceFlow(context.Background(), "outside", []int{11, 12}, 0)
	if err != nil || missing.Processed != 2 || !missing.Incomplete || len(missing.FlowNodes) != 0 {
		t.Fatalf("unavailable IDs did not advance %+v %v", missing, err)
	}
}

func TestTraceFlowPreviewBoundsOutputAndKeepsLLMCall(t *testing.T) {
	invocation := auditToolLoopInvocations()[0]
	tool := `,{ "role":"tool", "name":"example", "content":"` + strings.Repeat("界<", 1500) + `"}`
	invocation.RequestPayload = stringPointer(`{"messages":[{"role":"user","content":"prompt"}` + strings.Repeat(tool, 130) + `]}`)
	service := aiObservabilityService{invocationRepo: traceFlowFixture{invocations: map[int]models.ModelInvocation{11: invocation}}}
	preview, err := service.GetTraceFlow(context.Background(), "trc_weather", []int{11}, 0)
	if err != nil || preview.Processed != 1 || !preview.Incomplete {
		t.Fatalf("unexpected bounded preview %v", err)
	}
	raw, err := json.Marshal(preview)
	if err != nil || len(raw) > traceFlowResponseBytes || len(preview.FlowNodes) > traceFlowNodeLimit {
		t.Fatalf("preview exceeds budget: %d bytes, %d nodes, %v", len(raw), len(preview.FlowNodes), err)
	}
	if len(preview.FlowNodes) < 3 || preview.FlowNodes[len(preview.FlowNodes)-1].Kind != "llm_call" {
		t.Fatal("bounded preview lost its invocation card")
	}
	node := preview.FlowNodes[1]
	if node.Kind != "tool_output" || !node.PreviewTruncated || len(node.OutputPayload) > traceFlowFieldBytes || !utf8.ValidString(node.OutputPayload) {
		t.Fatal("tool preview is missing valid UTF-8 truncation")
	}
}

func TestBuildAuditFlowNodesIgnoresHistoricalMessagesInFlowAndInvocationInput(t *testing.T) {
	startedAt := time.Date(2026, 3, 26, 9, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	secondStartedAt := startedAt.Add(5 * time.Second)
	secondCompletedAt := secondStartedAt.Add(2 * time.Second)

	invocations := []models.ModelInvocation{
		{
			ID:                  21,
			TraceID:             "trc_history_trimmed",
			RequestID:           "req_hist_1",
			RequestedModel:      "auto",
			ActualProviderModel: "gpt-weather",
			Status:              models.ModelInvocationStatusCompleted,
			RequestPayload:      stringPointer(`{"messages":[{"role":"system","content":"system prompt"},{"role":"user","content":"Previous turn question"},{"role":"assistant","content":"Previous turn answer"},{"role":"user","content":"Current turn question"}],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`),
			ResponsePayload:     stringPointer(`{"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_current","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}]}`),
			CreatedAt:           startedAt,
			CompletedAt:         &completedAt,
		},
		{
			ID:                  22,
			TraceID:             "trc_history_trimmed",
			RequestID:           "req_hist_2",
			RequestedModel:      "auto",
			ActualProviderModel: "gpt-weather",
			Status:              models.ModelInvocationStatusCompleted,
			RequestPayload:      stringPointer(`{"messages":[{"role":"system","content":"system prompt"},{"role":"user","content":"Previous turn question"},{"role":"assistant","content":"Previous turn answer"},{"role":"user","content":"Current turn question"},{"role":"assistant","tool_calls":[{"id":"call_current","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Shanghai\"}"}}]},{"role":"tool","tool_call_id":"call_current","name":"get_weather","content":"{\"temp_c\":16}"}],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`),
			ResponsePayload:     stringPointer(`{"choices":[{"index":0,"message":{"role":"assistant","content":"Shanghai is 16C."}}]}`),
			CreatedAt:           secondStartedAt,
			CompletedAt:         &secondCompletedAt,
		},
	}

	nodes := buildAuditFlowNodes(invocations)
	if len(nodes) != 6 {
		t.Fatalf("expected 6 flow nodes, got %d", len(nodes))
	}
	if nodes[0].Kind != "user_message" || nodes[0].OutputPayload != "Current turn question" {
		t.Fatalf("expected first flow node to be current user turn, got %#v", nodes[0])
	}
	if strings.Contains(nodes[1].InputPayload, "Previous turn question") || strings.Contains(nodes[1].InputPayload, "system prompt") {
		t.Fatalf("expected llm input payload to exclude historical messages, got %q", nodes[1].InputPayload)
	}

	var llmInput map[string]any
	if err := json.Unmarshal([]byte(nodes[1].InputPayload), &llmInput); err != nil {
		t.Fatalf("expected llm input payload to stay valid json: %v", err)
	}
	rawMessages, ok := llmInput["messages"].([]any)
	if !ok || len(rawMessages) != 1 {
		t.Fatalf("expected sanitized llm input to keep only current-turn messages, got %#v", llmInput["messages"])
	}
}

func stringPointer(value string) *string {
	return &value
}
