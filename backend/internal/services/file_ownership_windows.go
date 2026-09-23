//go:build windows

package services

// Windows development environments do not expose Unix ownership. Runtime
// deployments use the Unix implementation above.
func setFileOwnership(_ string, _, _ int) error {
	return nil
}
