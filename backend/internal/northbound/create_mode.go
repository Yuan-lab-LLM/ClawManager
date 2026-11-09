package northbound

import "strings"

func validateNorthboundCreateMode(mode, expected string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" && mode != expected {
		return apiError(422, "VALIDATION_ERROR", "instance_mode must match the Lite or Pro creation endpoint", nil)
	}
	return nil
}
