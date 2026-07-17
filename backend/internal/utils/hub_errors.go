package utils

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type HubError struct {
	Code    string
	Message string
	Details map[string]string
}

func (e *HubError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func NewHubError(code, message string, details map[string]string) *HubError {
	return &HubError{Code: code, Message: message, Details: details}
}

// HandleHubError maps skill hub domain errors to HTTP responses.
func HandleHubError(c *gin.Context, err error) {
	var hubErr *HubError
	if errors.As(err, &hubErr) {
		switch hubErr.Code {
		case "skill_package_md5_mismatch":
			Error(c, http.StatusBadRequest, hubErr.Code)
		default:
			Error(c, http.StatusBadRequest, hubErr.Error())
		}
		return
	}
	switch err.Error() {
	case "skill_not_scanned", "skill_risk_blocked", "skill_tags_required", "skill_not_in_library", "skill is not published to hub":
		Error(c, http.StatusBadRequest, err.Error())
	case "skill_package_pending":
		Error(c, http.StatusConflict, err.Error())
	case "skill_package_materialize_failed", "skill_package_materializing":
		Error(c, http.StatusConflict, err.Error())
	case "skill_attach_forbidden", "access denied":
		Error(c, http.StatusForbidden, err.Error())
	default:
		HandleError(c, err)
	}
}
