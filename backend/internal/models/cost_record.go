package models

import "time"

// CostRecord stores token and money accounting snapshots for a model call.
type CostRecord struct {
	ID               int       `db:"id,primarykey,autoincrement" json:"id"`
	TraceID          string    `db:"trace_id" json:"trace_id"`
	SessionID        *string   `db:"session_id" json:"session_id,omitempty"`
	RequestID        *string   `db:"request_id" json:"request_id,omitempty"`
	UserID           *int      `db:"user_id" json:"user_id,omitempty"`
	InstanceID       *int      `db:"instance_id" json:"instance_id,omitempty"`
	InvocationID     *int      `db:"invocation_id" json:"invocation_id,omitempty"`
	ModelID          *int      `db:"model_id" json:"model_id,omitempty"`
	ProviderType     string    `db:"provider_type" json:"provider_type"`
	ModelName        string    `db:"model_name" json:"model_name"`
	Currency         string    `db:"currency" json:"currency"`
	PromptTokens     int       `db:"prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int       `db:"completion_tokens" json:"completion_tokens"`
	TotalTokens      int       `db:"total_tokens" json:"total_tokens"`
	InputUnitPrice   float64   `db:"input_unit_price" json:"input_unit_price"`
	OutputUnitPrice  float64   `db:"output_unit_price" json:"output_unit_price"`
	EstimatedCost    float64   `db:"estimated_cost" json:"estimated_cost"`
	ActualCost       *float64  `db:"actual_cost" json:"actual_cost,omitempty"`
	InternalCost     float64   `db:"internal_cost" json:"internal_cost"`
	RecordedAt       time.Time `db:"recorded_at" json:"recorded_at"`
}

// TableName returns the table name for cost records.
func (c CostRecord) TableName() string {
	return "cost_records"
}
