package services

import (
	"context"
	"errors"
	"time"
)

const workspaceUsageEntryLimit = 100000
const workspaceUsageTimeout = 8 * time.Second

// Retain a slot until the helper is actually reaped. A hard-mounted NFS RPC
// may stay in kernel I/O after SIGKILL; never accumulate unbounded processes.
var workspaceUsageHelpers = make(chan struct{}, 4)
var errWorkspaceUsageUnavailable = errors.New("workspace usage unavailable")

func (s *workspaceFileService) MeasureWorkspaceUsage(ctx context.Context, scope WorkspaceFileScope) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if scope.TeamID != 0 {
		return 0, errWorkspaceUsageUnavailable
	}
	// Use the same native path and per-instance identity even for local roots.
	uid, gid, err := validateWorkspaceNFSScope(s.nfsRoot, scope)
	if err != nil {
		return 0, errWorkspaceUsageUnavailable
	}
	select {
	case workspaceUsageHelpers <- struct{}{}:
	default:
		return 0, errWorkspaceUsageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceUsageTimeout)
	defer cancel()
	type result struct {
		n   int64
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-workspaceUsageHelpers }()
		response, _, _, err := runWorkspaceNFSHelper(ctx, workspaceNFSRequest{Action: "measure_usage", Root: s.nfsRoot, Scope: scope, UID: uid, GID: gid, Limit: workspaceUsageEntryLimit, UploadMax: WorkspaceUploadMaxBytes()}, nil, nil)
		if err != nil {
			done <- result{err: err}
			return
		}
		if response.Size < 0 {
			done <- result{err: errWorkspaceUsageUnavailable}
			return
		}
		done <- result{n: response.Size}
	}()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case result := <-done:
		return result.n, result.err
	}
}
