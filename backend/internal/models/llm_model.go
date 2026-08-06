package models

import "time"

// LLMModel stores an admin-managed AI model configuration.
type LLMModel struct {
	ID                int       `db:"id,primarykey,autoincrement" json:"id"`
	DisplayName       string    `db:"display_name" json:"display_name"`
	Description       *string   `db:"description" json:"description,omitempty"`
	ProviderType      string    `db:"provider_type" json:"provider_type"`
	ProtocolType      string    `db:"protocol_type" json:"protocol_type,omitempty"`
	BaseURL           string    `db:"base_url" json:"base_url"`
	ProviderModelName string    `db:"provider_model_name" json:"provider_model_name"`
	APIKey            *string   `db:"api_key" json:"api_key,omitempty"`
	APIKeySecretRef   *string   `db:"api_key_secret_ref" json:"api_key_secret_ref,omitempty"`
	IsSecure          bool      `db:"is_secure" json:"is_secure"`
	IsActive          bool      `db:"is_active" json:"is_active"`
	Priority          int       `db:"priority" json:"priority"`
	InputPrice        float64   `db:"input_price" json:"input_price"`
	OutputPrice       float64   `db:"output_price" json:"output_price"`
	Currency          string    `db:"currency" json:"currency"`
	CreatedAt         time.Time `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time `db:"updated_at" json:"updated_at"`
}

// TableName returns the table name for the LLM model.
func (m LLMModel) TableName() string {
	return "llm_models"
}
