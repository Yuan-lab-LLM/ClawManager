package models

import "time"

// LLMModel stores an admin-managed AI model configuration.
type LLMModel struct {
	ID                  int     `db:"id,primarykey,autoincrement" json:"id"`
	VisibilityScope     string  `db:"visibility_scope" json:"visibility_scope"`
	AccessPolicyVersion int     `db:"access_policy_version" json:"access_policy_version"`
	AllowedUserIDs      []int   `db:"-" json:"allowed_user_ids"`
	UpdateAccessPolicy  bool    `db:"-" json:"-"`
	DisplayName         string  `db:"display_name" json:"display_name"`
	Description         *string `db:"description" json:"description,omitempty"`
	ProviderType        string  `db:"provider_type" json:"provider_type"`
	ProtocolType        string  `db:"protocol_type" json:"protocol_type,omitempty"`
	// Operation capabilities are explicit administrator choices, not inferred
	// from vision support or a provider model name. Legacy entries remain chat.
	ModelKind              string `db:"model_kind" json:"model_kind"`
	ImageGenerationEnabled bool   `db:"image_generation_enabled" json:"image_generation_enabled"`
	ImageEditEnabled       bool   `db:"image_edit_enabled" json:"image_edit_enabled"`
	// A platform budget estimate, NOT a provider invoice or a token price.
	// nil disables paid image dispatch until an administrator sets a price.
	ImagePricePerImage *float64 `db:"image_price_per_image" json:"image_price_per_image"`
	// ModelGroup is the stable logical name clients route to. Multiple records
	// may share one group, representing independent provider deployments.
	ModelGroup string `db:"model_group" json:"model_group,omitempty"`
	// DeploymentPriority is evaluated within a model group; lower values are
	// preferred and higher values are eligible as failover deployments.
	DeploymentPriority int `db:"deployment_priority" json:"deployment_priority"`
	// MaxConcurrentRequests caps in-flight calls to this physical deployment.
	// Zero keeps the deployment uncapped while still reporting current load.
	MaxConcurrentRequests int `db:"max_concurrent_requests" json:"max_concurrent_requests"`
	// ContextWindowTokens is the effective prompt/context capacity exposed to
	// managed agent runtimes. Zero means unknown and leaves the runtime default
	// untouched.
	ContextWindowTokens  int    `db:"context_window_tokens" json:"context_window_tokens"`
	QualityTier          int    `db:"quality_tier" json:"quality_tier"`
	BaseURL              string `db:"base_url" json:"base_url"`
	ProviderModelName    string `db:"provider_model_name" json:"provider_model_name"`
	ReasoningEnabled     bool   `db:"reasoning_enabled" json:"reasoning_enabled"`
	ReasoningControlMode string `db:"reasoning_control_mode" json:"reasoning_control_mode"`
	SupportsReasoning    bool   `db:"-" json:"supports_reasoning"`
	SupportsTools        bool   `db:"supports_tools" json:"supports_tools"`
	// ToolsSupportOverride is an administrator assertion independent of discovery.
	// NULL retains legacy/automatic behavior; false is an explicit opt-out.
	ToolsSupportOverride  *bool              `db:"tools_support_override" json:"tools_support_override"`
	SupportsVision        bool               `db:"supports_vision" json:"supports_vision"`
	VisionSupportOverride *bool              `db:"vision_support_override" json:"vision_support_override"`
	CapabilitySource      string             `db:"capability_source" json:"capability_source,omitempty"`
	ReasoningControl      string             `db:"-" json:"reasoning_control,omitempty"`
	APIKey                *string            `db:"api_key" json:"api_key,omitempty"`
	APIKeySecretRef       *string            `db:"api_key_secret_ref" json:"api_key_secret_ref,omitempty"`
	IsSecure              bool               `db:"is_secure" json:"is_secure"`
	IsActive              bool               `db:"is_active" json:"is_active"`
	InputPrice            float64            `db:"input_price" json:"input_price"`
	OutputPrice           float64            `db:"output_price" json:"output_price"`
	Currency              string             `db:"currency" json:"currency"`
	CreatedAt             time.Time          `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time          `db:"updated_at" json:"updated_at"`
	ProviderModelsJSON    *string            `db:"provider_models_json" json:"-"`
	ProviderModels        []LLMProviderModel `db:"-" json:"provider_models"`
	CatalogProviderName   string             `db:"-" json:"catalog_provider_name,omitempty"`
}

// TableName returns the table name for the LLM model.
func (m LLMModel) TableName() string {
	return "llm_models"
}
