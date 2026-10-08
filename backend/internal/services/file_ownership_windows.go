//go:build windows

package services

// Windows development environments do not expose Unix ownership. Runtime
// deployments use the Unix implementation above.
var setFileOwnership = func(_ string, _, _ int) error { return nil }
