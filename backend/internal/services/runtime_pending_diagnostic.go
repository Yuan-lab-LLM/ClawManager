package services

import (
	"context"
	"errors"
	"strings"
)

// Fixed codes only: errors can contain provider responses, credentials or SQL.
// Classification is diagnostic and never changes retry/admission decisions.
func pendingStartReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	message := err.Error()
	for _, entry := range []struct{ text, code string }{
		{"pending runtime target is not ready", "target_not_ready"},
		{"runtime target has no current ready", "target_not_ready"},
		{"pending retry requires an older confirmed stopped assignment", "predecessor_not_stopped"},
		{"pending generation business Secret already exists", "already_published"},
		{"missing predecessor assignment", "predecessor_missing"},
		{"selected Runtime Pod is no longer live and schedulable", "placement_not_current"},
		{"selected Runtime registration changed", "registration_changed"},
		{"managed Team preview unavailable", "preview_admission"},
		{"pending_authority:", "authority"},
		{"pending_placement:", "placement"},
		{"pending_parameters:", "parameters"},
	} {
		if strings.Contains(message, entry.text) {
			return entry.code
		}
	}
	return "pending_unknown"
}
