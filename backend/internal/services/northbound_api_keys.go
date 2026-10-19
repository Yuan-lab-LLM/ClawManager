package services

import (
	"clawreef/internal/nbkeys"
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type APIKeyList struct {
	UserID  int                `json:"user_id"`
	Version int64              `json:"version"`
	Keys    []nbkeys.PublicKey `json:"keys"`
}
type APIKeyMutation struct {
	UserID    int       `json:"user_id"`
	Version   int64     `json:"version"`
	Action    string    `json:"action"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	Enabled   bool      `json:"enabled"`
	ExpiresAt time.Time `json:"expires_at"`
}
type APIKeyMutationResult struct {
	APIKeyList
	Secret string `json:"secret,omitempty"`
}

func (s *NorthboundAdminService) ListAPIKeys(userID int) (*APIKeyList, error) {
	policy, err := s.repo.GetCallerPolicy(userID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, errors.New("请先在账号授权中明确配置该账号，创建 Key 不会自动改变账号策略")
	}
	keys, err := nbkeys.Decode(policy.APIKeysJSON)
	if err != nil {
		return nil, err
	}
	ceiling := policy.Scopes
	if !policy.Enabled {
		ceiling = nil
	}
	result := &APIKeyList{UserID: userID, Version: policy.APIKeysVersion, Keys: []nbkeys.PublicKey{}}
	for _, k := range keys {
		result.Keys = append(result.Keys, k.Public(ceiling, time.Now()))
	}
	return result, nil
}

func validateKeyMutation(req *APIKeyMutation, ceiling []string) error {
	req.Name = strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(req.Name) < 1 || utf8.RuneCountInString(req.Name) > 80 || strings.IndexFunc(req.Name, unicode.IsControl) >= 0 {
		return errors.New("Key 名称须为 1–80 个字符，不能包含控制字符")
	}
	if !req.ExpiresAt.After(time.Now()) || req.ExpiresAt.After(time.Now().AddDate(10, 0, 0)) {
		return errors.New("请选择未来十年内的有效期")
	}
	req.Scopes = normalizeValues(req.Scopes)
	if len(req.Scopes) == 0 {
		return errors.New("至少选择一个接口权限")
	}
	for _, scope := range req.Scopes {
		if _, ok := allowedNorthboundScopes[scope]; !ok {
			return errors.New("不支持的接口权限")
		}
		allowed := false
		for _, s := range ceiling {
			if s == scope {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("Key 权限不能超出账号授权范围，请刷新配置")
		}
	}
	return nil
}

func (s *NorthboundAdminService) MutateAPIKey(ctx context.Context, actor int, req APIKeyMutation) (*APIKeyMutationResult, error) {
	policy, err := s.repo.GetCallerPolicy(req.UserID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, errors.New("请先配置账号授权，不会自动创建账号或扩大权限")
	}
	if req.Version != policy.APIKeysVersion {
		return nil, ErrNorthboundSettingsConflict
	}
	keys, err := nbkeys.Decode(policy.APIKeysJSON)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range keys {
		if keys[i].ID == req.ID {
			idx = i
			break
		}
	}
	raw := ""
	affected := req.ID
	switch req.Action {
	case "create", "rotate", "update":
		user, err := s.users.GetByID(req.UserID)
		if err != nil {
			return nil, err
		}
		if user == nil || !user.IsActive || !policy.Enabled {
			return nil, errors.New("所属账号及其北向授权必须启用")
		}
		if req.Action != "create" && (idx < 0 || keys[idx].RevokedAt != nil) {
			return nil, errors.New("Key 不存在或已撤销")
		}
		if req.Action == "rotate" {
			req.Name = keys[idx].Name + " · 轮换"
			req.Scopes = keys[idx].Scopes
		}
		if err := validateKeyMutation(&req, policy.Scopes); err != nil {
			return nil, err
		}
		if req.Action == "update" {
			keys[idx].Name = req.Name
			keys[idx].Scopes = req.Scopes
			keys[idx].Enabled = req.Enabled
			keys[idx].ExpiresAt = req.ExpiresAt.UTC()
		} else {
			if len(keys) >= nbkeys.MaxKeys {
				return nil, errors.New("每账号最多保存 20 把 Key，请先移除已撤销 Key 的配置记录")
			}
			var key nbkeys.Key
			key, raw, err = nbkeys.New(req.UserID, req.Name, req.Scopes, req.ExpiresAt, actor)
			if err != nil {
				return nil, err
			}
			if req.Action == "create" {
				key.Enabled = req.Enabled
			}
			keys = append(keys, key)
			affected = key.ID
		}
	case "disable", "revoke", "remove":
		if idx < 0 {
			return nil, errors.New("Key 不存在")
		}
		if req.Action == "remove" {
			if keys[idx].RevokedAt == nil {
				return nil, errors.New("只能移除已撤销的 Key 配置，审计记录仍保留")
			}
			keys = append(keys[:idx], keys[idx+1:]...)
		} else {
			keys[idx].Enabled = false
			if req.Action == "revoke" && keys[idx].RevokedAt == nil {
				now := time.Now().UTC()
				keys[idx].RevokedAt = &now
			}
		}
	default:
		return nil, errors.New("不支持的 Key 操作")
	}
	ok, err := s.repo.UpdateAPIKeys(ctx, req.UserID, req.Version, policy.Enabled, policy.ScopesJSON, keys)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNorthboundSettingsConflict
	}
	// Explicit whitelist: never serialize stored keys or their hashes into audit.
	_ = s.repo.RecordSettingsAudit(ctx, actor, "api_key."+req.Action, nil, map[string]any{"user_id": req.UserID, "key_id": affected, "version": req.Version + 1})
	list, err := s.ListAPIKeys(req.UserID)
	if err != nil {
		return nil, err
	}
	return &APIKeyMutationResult{APIKeyList: *list, Secret: raw}, nil
}
