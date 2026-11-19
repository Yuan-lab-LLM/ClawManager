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

func normalizeSessionUsageTimeRange(since, until *time.Time) (*time.Time, *time.Time, error) {
	if until == nil {
		now := time.Now().UTC()
		until = &now
	}
	if since == nil {
		start := until.Add(-30 * 24 * time.Hour)
		since = &start
	}
	if err := validateSessionUsageTimeRange(since, until); err != nil {
		return nil, nil, err
	}
	if until.Sub(*since) > 90*24*time.Hour {
		return nil, nil, fmt.Errorf("synchronous queries cover at most 90 days; use the history archive cursor/export API")
	}
	return since, until, nil
}
