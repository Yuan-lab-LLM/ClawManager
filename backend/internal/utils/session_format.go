package utils

import "strings"

// FormatOpenClawSessionKey extracts the display session key from a stored session ID.
func FormatOpenClawSessionKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	for _, prefix := range []string{"agent:openclaw:", "agent:hermes:"} {
		if strings.HasPrefix(sessionID, prefix) {
			return strings.TrimPrefix(sessionID, prefix)
		}
	}
	if strings.HasPrefix(sessionID, "agent:") {
		parts := strings.SplitN(sessionID, ":", 3)
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			return parts[2]
		}
	}
	return sessionID
}

// NormalizeOpenClawSessionID maps a runtime session key to the canonical stored session ID.
func NormalizeOpenClawSessionID(sessionKey string, runtimeType string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return sessionKey
	}
	if strings.HasPrefix(sessionKey, "agent:") {
		return sessionKey
	}
	switch strings.ToLower(strings.TrimSpace(runtimeType)) {
	case "hermes":
		return "agent:hermes:" + sessionKey
	default:
		return "agent:openclaw:" + sessionKey
	}
}

// IsTraceFallbackSessionID reports whether a session ID was generated per trace.
func IsTraceFallbackSessionID(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	return strings.HasPrefix(sessionID, "sess_")
}
