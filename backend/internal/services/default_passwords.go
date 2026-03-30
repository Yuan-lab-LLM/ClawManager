package services

// Seeded/default passwords and hashes used by setup flows.
const (
	DefaultAdminPassword = "admin123"
	DefaultUserPassword  = "user123"

	// DefaultAdminPasswordHash matches DefaultAdminPassword.
	DefaultAdminPasswordHash = "$2a$10$pbenze514mwv3pvQySQBVOsF5J4DBXL2kVo1hLa8JFhQu5x3AKvBi"

	legacyBrokenAdminSeedHash = "$2a$10$N9qo8uLOickgx2ZMRZoMy.MqrzL9wGC3qD3Q.ZHqQH6t3q7l1L5uG"
)

func DefaultPasswordForRole(role string) string {
	if role == "admin" {
		return DefaultAdminPassword
	}
	return DefaultUserPassword
}

func IsKnownBrokenAdminSeedHash(hash string) bool {
	switch hash {
	case legacyBrokenAdminSeedHash:
		return true
	default:
		return false
	}
}
