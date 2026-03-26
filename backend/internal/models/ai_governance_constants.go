package models

const (
	// Traffic classes
	TrafficClassDesktopStream = "desktop_stream"
	TrafficClassLLM           = "llm"
	TrafficClassToolMCP       = "tool_mcp"
	TrafficClassGenericEgress = "generic_egress"
)

const (
	// Model invocation statuses
	ModelInvocationStatusPending   = "pending"
	ModelInvocationStatusCompleted = "completed"
	ModelInvocationStatusFailed    = "failed"
	ModelInvocationStatusBlocked   = "blocked"
)

const (
	// Audit severities
	AuditSeverityInfo  = "info"
	AuditSeverityWarn  = "warn"
	AuditSeverityError = "error"
)

const (
	// Risk severities
	RiskSeverityLow    = "low"
	RiskSeverityMedium = "medium"
	RiskSeverityHigh   = "high"
)

const (
	// Risk actions
	RiskActionAllow            = "allow"
	RiskActionRouteSecureModel = "route_secure_model"
	RiskActionBlock            = "block"
)
