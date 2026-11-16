//go:build !linux

package services

import "context"

func measureWorkspaceUsageTree(context.Context, string, int) (int64, error) {
	return 0, errWorkspaceUsageUnavailable
}
