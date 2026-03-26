package services

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestBuildAuditFlowNodesBuildsSingleToolLoopTimeline(t *testing.T) {
	startedAt := time.Date(2026, 3, 26, 7, 53, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	secondStartedAt := startedAt.Add(10 * time.Second)
	secondCompletedAt := secondStartedAt.Add(2 * time.Second)

	invocations := []models.ModelInvocation{
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

	nodes := buildAuditFlowNodes(invocations)
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
