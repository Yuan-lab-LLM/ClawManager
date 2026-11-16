package services

import (
	"strings"
	"time"

	"clawreef/internal/models"
)

var runtimeRecoveryBackoff = [...]time.Duration{30 * time.Second, 120 * time.Second, 300 * time.Second}

func runtimeRecoveryFailure(state models.InstanceRuntimeRecovery, now time.Time, reason string) (models.InstanceRuntimeRecovery, bool) {
	state.HealthySince = nil
	state.Reason = reason
	if state.Attempts >= len(runtimeRecoveryBackoff) {
		state.NextRetryAt = nil
		return state, false
	}
	if state.NextRetryAt == nil {
		next := now.Add(runtimeRecoveryBackoff[state.Attempts])
		state.NextRetryAt = &next
	}
	return state, !now.Before(*state.NextRetryAt)
}

func runtimeRecoveryHealthy(state models.InstanceRuntimeRecovery, now time.Time, fresh bool) models.InstanceRuntimeRecovery {
	if !fresh {
		state.HealthySince = nil
		return state
	}
	state.NextRetryAt = nil
	state.Phase = ""
	if state.HealthySince == nil {
		state.HealthySince = &now
	}
	if now.Sub(*state.HealthySince) >= 5*time.Minute {
		state.Attempts = 0
		state.Reason = ""
		state.SourcePodID = 0
		state.SourceGatewayPort = 0
	}
	return state
}

// Fresh nonempty runtime details must establish their own retryable failure.
// An old SQL crash cannot authorize reset of a new opaque startup error.
func runtimeRecoveryFreshFailureBlocked(code, message string) bool {
	if runtimeRecoveryPermanentDetails(code, message) {
		return true
	}
	if strings.TrimSpace(code) == "" && strings.TrimSpace(message) == "" {
		return false
	}
	for _, detail := range []string{code, message} {
		if _, retryable := runtimeRecoveryFailureReason(models.Instance{Status: "error"}, &models.InstanceRuntimeBinding{State: "error", ErrorMessage: &detail}); retryable {
			return false
		}
	}
	return true
}

func runtimeRecoveryPermanentDetails(code, message string) bool {
	details := code + " " + message
	return runtimeRecoveryPermanentFailure(models.Instance{RuntimeErrorCode: &code, RuntimeErrorMessage: &details}, nil)
}

func runtimeRecoveryPermanentFailure(instance models.Instance, binding *models.InstanceRuntimeBinding) bool {
	code := ""
	if instance.RuntimeErrorCode != nil {
		code = strings.ToUpper(*instance.RuntimeErrorCode)
	}
	switch code {
	case "MODEL_CONFIGURATION_REQUIRED", "INVALID_CONFIGURATION", "AUTHENTICATION_FAILED", "INVALID_CREDENTIALS", "MODEL_NOT_FOUND":
		return true
	}
	message := ""
	if instance.RuntimeErrorMessage != nil {
		message = *instance.RuntimeErrorMessage
	}
	if binding != nil && binding.ErrorMessage != nil {
		message += " " + *binding.ErrorMessage
	}
	message = strings.ToLower(message)
	for _, marker := range []string{"invalid api key", "invalid_api_key", "unauthorized", "invalid credentials", "authentication failed", "model not found", "unknown model", "invalid model", "configuration required", "invalid configuration", "missing api key", "permission denied", "missing_llm_credentials", "dashboard_auth_failed", "invalid_gateway_identity", "incompatible_workspace_version", "workspace_unusable", "invalid_control_ui_origin", "invalid_trusted_proxies", "unsupported_hermes_protocol"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func runtimeRecoveryFailureReason(instance models.Instance, binding *models.InstanceRuntimeBinding) (string, bool) {
	if instance.Status != "running" && instance.Status != "error" && instance.Status != "creating" {
		return "", false
	}
	if runtimeRecoveryPermanentFailure(instance, binding) {
		return "", false
	}
	if binding == nil {
		return "", false
	}
	state := strings.ToLower(strings.TrimSpace(binding.State))
	if state != "failed" && state != "error" && state != "unhealthy" && state != "stopped" {
		return "", false
	}
	message := ""
	if binding.ErrorMessage != nil {
		message = strings.TrimSpace(*binding.ErrorMessage)
	}
	if message == "" && instance.RuntimeErrorMessage != nil {
		message = strings.TrimSpace(*instance.RuntimeErrorMessage)
	}
	normalized := strings.ToLower(message)
	// These are the isolated Gateway safeGatewayError codes. Keep the list
	// explicit: generic gateway_start_failed may conceal a bad configuration.
	switch normalized {
	case "gateway_process_exited", "container_reset_required", "port_conflict", "http_health_failed", "dashboard_not_ready", "dashboard_http_unavailable", "gateway_health_unavailable", "websocket_unavailable":
		return message, true
	}
	// A failed state alone may represent a permanent startup error. Only concrete
	// exit/resource/health failures permit the automatic recovery path.
	for _, marker := range []string{"process exited", "gateway exited unexpectedly", "exit status ", "signal: killed", "out of memory", "oom", "health check failed", "readiness probe failed", "slot fault", "slot failed", "runtime gateway failed"} {
		if strings.Contains(normalized, marker) {
			return message, true
		}
	}
	if state == "unhealthy" {
		return "runtime gateway unhealthy", true
	}
	return "", false
}
