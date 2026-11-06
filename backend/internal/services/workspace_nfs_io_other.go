//go:build !linux

package services

import (
	"context"
	"fmt"
	"io"
	"os"
)

func runWorkspaceNFSHelper(context.Context, workspaceNFSRequest, io.Reader, func(workspaceNFSEvent) error) (workspaceNFSResponse, *os.File, bool, error) {
	return workspaceNFSResponse{}, nil, true, fmt.Errorf("NFS workspace credentials require Linux")
}
func runWorkspaceNFSHelperCommand(context.Context, string, workspaceNFSRequest, io.Reader, func(workspaceNFSEvent) error) (workspaceNFSResponse, *os.File, bool, error) {
	return workspaceNFSResponse{}, nil, true, fmt.Errorf("NFS workspace credentials require Linux")
}
func RunWorkspaceIOHelper(args []string, _ io.Reader, _ io.Writer) (bool, error) {
	if len(args) > 0 && args[0] == ownerHistoryHelperCommand {
		return true, fmt.Errorf("private NFS history requires Linux")
	}
	if len(args) > 0 && (args[0] == WorkspaceIOHelperCommand || args[0] == workspaceManagementHelperCommand || args[0] == workspaceOperationLockHelperCommand) {
		return true, fmt.Errorf("NFS workspace credentials require Linux")
	}
	return false, nil
}

func workspaceNFSTransportUncertain(error) bool { return false }
