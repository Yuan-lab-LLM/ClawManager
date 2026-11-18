package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
)

var northboundSectionFields = map[string][]string{
	"access":  {"api_enabled", "owner_teams_enabled", "owner_team_creation_user_id"},
	"callers": {"require_explicit_callers"},
	"traffic": {"challenge_ttl_seconds", "access_token_ttl_seconds", "refresh_token_ttl_seconds", "core_request_timeout_seconds", "challenge_rate_per_minute", "login_rate_per_minute", "account_login_rate_per_minute", "create_rate_per_minute", "query_rate_per_minute", "share_rate_per_minute", "max_pending_operations", "operation_tick_milliseconds", "operation_lease_seconds", "operation_max_attempts"},
	"runtime": {"allowed_lite_types", "allowed_pro_types", "lite_cpu_cores", "lite_memory_gb", "lite_disk_gb", "pro_cpu_cores", "pro_memory_gb", "pro_disk_gb"},
}

func (s *NorthboundAdminService) SaveSettingsSection(ctx context.Context, actor int, section string, version int64, fields map[string]json.RawMessage) (*models.NorthboundAdminSettings, error) {
	allowed, ok := northboundSectionFields[section]
	if !ok {
		return nil, fmt.Errorf("unknown settings section")
	}
	for key, value := range fields {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found || string(value) == "null" {
			return nil, fmt.Errorf("invalid field for settings section: %s", key)
		}
	}
	current, err := s.repo.GetAdminSettings()
	if err != nil {
		return nil, err
	}
	if current.Version != version {
		return nil, ErrNorthboundSettingsConflict
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err = json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}
	for key, value := range fields {
		merged[key] = value
	}
	raw, err = json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	var next models.NorthboundAdminSettings
	if err = json.Unmarshal(raw, &next); err != nil {
		return nil, fmt.Errorf("invalid settings value")
	}
	return s.SaveSettings(ctx, actor, &next)
}
