package aigateway

import "strings"

const defaultManagedSessionKey = "main"

// IsManagedInstanceType reports whether an instance type participates in managed
// runtime LLM governance (OpenClaw / Hermes).
func IsManagedInstanceType(instanceType string) bool {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "openclaw", "hermes":
		return true
	default:
		return false
	}
}

// HasExplicitSessionIdentity returns true when the caller supplied a stable
// session identifier via header or request body fields.
func HasExplicitSessionIdentity(req ChatCompletionRequest) bool {
	if req.OpenClawSessionKey != nil && strings.TrimSpace(*req.OpenClawSessionKey) != "" {
		return true
	}
	if normalizeOptionalString(req.SessionID) != "" {
		return true
	}
	if normalizeOptionalString(req.User) != "" {
		return true
	}
	return false
}

// ApplyManagedInstanceSessionDefaults fills in the default OpenClaw/Hermes session
// key for instance gateway token calls that omitted explicit session identity.
func ApplyManagedInstanceSessionDefaults(req *ChatCompletionRequest, gatewayAuthType, instanceType string) {
	if req == nil {
		return
	}
	if strings.TrimSpace(gatewayAuthType) != "instance" {
		return
	}
	instanceType = strings.TrimSpace(instanceType)
	if !IsManagedInstanceType(instanceType) {
		return
	}
	req.ManagedAgentType = stringPtr(instanceType)
	if HasExplicitSessionIdentity(*req) {
		return
	}
	req.OpenClawSessionKey = stringPtr(defaultManagedSessionKey)
}

func sessionNormalizationRuntimeType(req ChatCompletionRequest) string {
	if req.ManagedAgentType != nil {
		if runtimeType := strings.TrimSpace(*req.ManagedAgentType); runtimeType != "" {
			return runtimeType
		}
	}
	if req.RuntimeType != nil {
		return strings.TrimSpace(*req.RuntimeType)
	}
	return ""
}
