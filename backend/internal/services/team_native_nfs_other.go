//go:build !linux

package services

import (
	"context"
	"fmt"
	"io"
)

func runTeamNativeHelper(ctx context.Context, request teamNativeRequest) (bool, error) {
	return true, fmt.Errorf("NFS Team helper requires Linux")
}
func RunTeamWorkspaceHelper(args []string, in io.Reader, out io.Writer) (bool, error) {
	return false, nil
}
