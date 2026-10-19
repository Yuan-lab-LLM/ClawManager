package northbound

import (
	"clawreef/internal/nbkeys"
	"time"
)

func keyPrincipal(provider RuntimeSettingsProvider, userID int, keyID, raw string) (*Principal, error) {
	invalid := apiError(401, "AUTH_INVALID", "Invalid API key", nil)
	if provider == nil {
		return nil, invalid
	}
	policy, err := provider.CallerPolicy(userID)
	if err != nil {
		return nil, apiError(503, "DEPENDENCY_UNAVAILABLE", "Authentication service unavailable", nil)
	}
	if policy == nil || !policy.Enabled {
		return nil, invalid
	}
	keys, err := nbkeys.Decode(policy.APIKeysJSON)
	if err != nil {
		return nil, apiError(503, "DEPENDENCY_UNAVAILABLE", "Authentication configuration unavailable", nil)
	}
	for _, key := range keys {
		if key.ID != keyID {
			continue
		}
		if !key.Active(time.Now()) || (raw != "" && !key.Verify(raw)) {
			return nil, invalid
		}
		return &Principal{UserID: userID, AuthType: "api_key", KeyID: key.ID, Scopes: key.EffectiveScopes(policy.Scopes, time.Now())}, nil
	}
	return nil, invalid
}

func (s *AuthService) AuthenticateAPIKey(raw string) (*Principal, error) {
	uid, id, err := nbkeys.Parse(raw)
	if err != nil {
		return nil, apiError(401, "AUTH_INVALID", "Invalid API key", nil)
	}
	principal, err := keyPrincipal(s.runtimeSettings, uid, id, raw)
	if err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(uid)
	if err != nil {
		return nil, apiError(503, "DEPENDENCY_UNAVAILABLE", "Authentication service unavailable", nil)
	}
	if user == nil || !user.IsActive {
		return nil, apiError(401, "AUTH_INVALID", "Invalid API key", nil)
	}
	return principal, nil
}

func operationScope(operationType string) string {
	switch operationType {
	case OperationTypeLiteInstance:
		return ScopeLiteCreate
	case OperationTypeProInstance:
		return ScopeProCreate
	case OperationTypeLiteRestart:
		return ScopeLiteRestart
	case OperationTypeProRestart:
		return ScopeProRestart
	case OperationTypeLiteReset:
		return ScopeLiteReset
	case OperationTypeProReset:
		return ScopeProReset
	case OperationTypeLiteDelete:
		return ScopeLiteDelete
	case OperationTypeProDelete:
		return ScopeProDelete
	}
	return ""
}
