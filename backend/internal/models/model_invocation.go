package models

import "time"

// ModelInvocation stores a governed LLM request/response record.
type ModelInvocation struct {
	ID                  int        `db:"id,primarykey,autoincrement" json:"id"`
	TraceID             string     `db:"trace_id" json:"trace_id"`
	SessionID           *string    `db:"session_id" json:"session_id,omitempty"`
	RequestID           string     `db:"request_id" json:"request_id"`
	UserID              *int       `db:"user_id" json:"user_id,omitempty"`
	InstanceID          *int       `db:"instance_id" json:"instance_id,omitempty"`
	InstanceMode        *string    `db:"instance_mode" json:"instance_mode,omitempty"`
	RuntimeType         *string    `db:"runtime_type" json:"runtime_type,omitempty"`
	GatewayID           *string    `db:"gateway_id" json:"gateway_id,omitempty"`
	RuntimePodID        *int64     `db:"runtime_pod_id" json:"runtime_pod_id,omitempty"`
	GatewayKeyID        *int       `db:"gateway_key_id" json:"gateway_key_id,omitempty"`
	ModelID             *int       `db:"model_id" json:"model_id,omitempty"`
	ProviderType        string     `db:"provider_type" json:"provider_type"`
	RequestedModel      string     `db:"requested_model" json:"requested_model"`
	ActualProviderModel string     `db:"actual_provider_model" json:"actual_provider_model"`
	TrafficClass        string     `db:"traffic_class" json:"traffic_class"`
	RequestPayload      *string    `db:"request_payload" json:"request_payload,omitempty"`
	ResponsePayload     *string    `db:"response_payload" json:"response_payload,omitempty"`
	PromptTokens        int        `db:"prompt_tokens" json:"prompt_tokens"`
	CompletionTokens    int        `db:"completion_tokens" json:"completion_tokens"`
	TotalTokens         int        `db:"total_tokens" json:"total_tokens"`
	CachedTokens        *int       `db:"cached_tokens" json:"cached_tokens,omitempty"`
	ReasoningTokens     *int       `db:"reasoning_tokens" json:"reasoning_tokens,omitempty"`
	LatencyMs           *int       `db:"latency_ms" json:"latency_ms,omitempty"`
	TTFTMs              *int       `db:"ttft_ms" json:"ttft_ms,omitempty"`
	GenerationMs        *int       `db:"generation_ms" json:"generation_ms,omitempty"`
	TokensPerSecond     *float64   `db:"tokens_per_second" json:"tokens_per_second,omitempty"`
	AttemptNo           int        `db:"attempt_no" json:"attempt_no"`
	IsStreaming         bool       `db:"is_streaming" json:"is_streaming"`
	Status              string     `db:"status" json:"status"`
	ErrorMessage        *string    `db:"error_message" json:"error_message,omitempty"`
	CreatedAt           time.Time  `db:"created_at" json:"created_at"`
	CompletedAt         *time.Time `db:"completed_at" json:"completed_at,omitempty"`
}

// TableName returns the table name for model invocations.
func (m ModelInvocation) TableName() string {
	return "model_invocations"
}
