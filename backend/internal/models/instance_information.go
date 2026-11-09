package models

import "regexp"

var instanceSecret = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|api[_-]?key|authorization)\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
var instanceBearer = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)
var instanceURLCredentials = regexp.MustCompile(`(https?://)[^\s/@]+:[^\s/@]+@`)

// SanitizeInstanceError is applied to list DTOs, never persisted back to storage.
func SanitizeInstanceError(value string) string {
	value = instanceURLCredentials.ReplaceAllString(value, "${1}[redacted]@")
	value = instanceBearer.ReplaceAllString(value, "Bearer [redacted]")
	value = instanceSecret.ReplaceAllString(value, "${1}[redacted]")
	return value
}
