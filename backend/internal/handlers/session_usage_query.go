package handlers

import (
	"fmt"
	"strings"
	"time"
)

func parseOptionalRFC3339(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}

func validateSessionUsageTimeRange(since, until *time.Time) error {
	if since != nil && until != nil && !until.After(*since) {
		return fmt.Errorf("until must be after since")
	}
	return nil
}
