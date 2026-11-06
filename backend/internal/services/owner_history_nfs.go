package services

import (
	"clawreef/internal/ownerrecords"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const ownerHistoryHelperCommand = "owner-history-helper"

// ConfigureOwnerHistoryNFSIO preserves the existing parsers and SQL projection;
// only private filesystem reads move to a process with the instance identity.
func ConfigureOwnerHistoryNFSIO(store *ownerrecords.Store, profile string) {
	if store == nil || profile != "nfs-rootless-v1" {
		return
	}
	store.HistoryRoot = filepath.Clean(store.HistoryRoot)
	store.HistoryReader = func(ctx context.Context, root, workspace string, identity ownerrecords.Identity, recent bool, cutoff ...time.Time) ([]ownerrecords.NativeQuestion, error) {
		request, err := ownerHistoryRequest(root, workspace, identity)
		if err != nil {
			return nil, err
		}
		request.Action, request.HistoryRecent, request.HistoryCutoff = "history", recent, cutoff
		response, _, ended, err := runWorkspaceNFSHelperCommand(ctx, ownerHistoryHelperCommand, request, nil, nil)
		if ended {
			_ = os.Remove(request.HistoryTempPath)
		}
		return response.Questions, err
	}
	store.FingerprintReader = func(ctx context.Context, root, workspace string, identity ownerrecords.Identity) (string, error) {
		request, err := ownerHistoryRequest(root, workspace, identity)
		if err != nil {
			return "", err
		}
		request.Action = "fingerprint"
		response, _, ended, err := runWorkspaceNFSHelperCommand(ctx, ownerHistoryHelperCommand, request, nil, nil)
		if ended {
			_ = os.Remove(request.HistoryTempPath)
		}
		return response.Fingerprint, err
	}
}
func ownerHistoryRequest(root, workspace string, identity ownerrecords.Identity) (workspaceNFSRequest, error) {
	request := workspaceNFSRequest{Root: root, Scope: WorkspaceFileScope{InstanceID: int(identity.Instance), UserID: int(identity.Scope), WorkspacePath: workspace}, HistoryIdentity: identity}
	if identity.Mode != "lite" || uint64(request.Scope.InstanceID) != identity.Instance || uint64(request.Scope.UserID) != identity.Scope || len(identity.Owner) > 255 {
		return request, ErrWorkspacePathInvalid
	}
	switch identity.Runtime {
	case RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness:
	default:
		return request, ErrWorkspacePathInvalid
	}
	if workspace != RuntimeWorkspacePathWithRoot(root, identity.Runtime, request.Scope.UserID, request.Scope.InstanceID) {
		return request, ErrWorkspacePathEscape
	}
	uid, gid, err := validateWorkspaceNFSScope(root, request.Scope)
	if err != nil {
		return request, fmt.Errorf("native history identity: %w", err)
	}
	request.UID, request.GID = uid, gid
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return request, err
	}
	request.HistoryTempPath = "/tmp/cm-owner-history-" + hex.EncodeToString(nonce[:])
	return request, nil
}
