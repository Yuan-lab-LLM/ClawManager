package aigateway

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Only classify a response received from an upstream deployment. Local gateway
// authorization runs before dispatch and must never be retried. Unknown/WAF/
// content-policy 403 responses are not evidence of model-access permissions.
func isProviderModelPermissionDenied(response *ProxyResponse) bool {
	if response == nil || response.StatusCode != http.StatusForbidden {
		return false
	}
	var payload struct {
		Error struct {
			Code json.RawMessage `json:"code"`
			Type string          `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(response.Body, &payload) != nil {
		return false
	}
	var code string
	_ = json.Unmarshal(payload.Error.Code, &code)
	for _, value := range []string{code, payload.Error.Type} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "permission_error", "model_access_denied", "model_not_allowed":
			return true
		}
	}
	return false
}
