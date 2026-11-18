package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const workspaceManagementHelperCommand = "workspace-management-helper"

func nfsInstanceManagementRequest(root, runtimeType string, instance *models.Instance) (workspaceNFSRequest, error) {
	var request workspaceNFSRequest
	if instance == nil || instance.WorkspacePath == nil {
		return request, ErrWorkspacePathInvalid
	}
	request.Root = filepath.Clean(root)
	request.Scope = WorkspaceFileScope{InstanceID: instance.ID, UserID: instance.UserID, RuntimeGeneration: instance.RuntimeGeneration, WorkspacePath: *instance.WorkspacePath}
	if request.Scope.WorkspacePath != RuntimeWorkspacePathWithRoot(request.Root, runtimeType, instance.UserID, instance.ID) {
		return request, ErrWorkspacePathEscape
	}
	uid, gid, err := validateWorkspaceNFSScope(request.Root, request.Scope)
	request.UID, request.GID = uid, gid
	return request, err
}

// ProbeNFSInstanceWorkspace checks existing persisted data without creating or
// repairing any path. The caller supplies the authenticated database identity.
func ProbeNFSInstanceWorkspace(ctx context.Context, root, runtimeType string, instance *models.Instance) error {
	request, err := nfsInstanceManagementRequest(root, runtimeType, instance)
	if err != nil {
		return err
	}
	request.Action = "probe"
	_, _, _, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	return err
}

// EraseNFSInstanceWorkspace must be called only after Runtime writer teardown.
// SQL admission additionally requires a closed generation gate and no binding.
// The deletion receipt is settled only once every dispatched helper has exited.
func EraseNFSInstanceWorkspace(ctx context.Context, root, runtimeType string, instance *models.Instance, writes repository.WorkspaceWriteRepository) error {
	request, err := nfsInstanceManagementRequest(root, runtimeType, instance)
	if err != nil {
		return err
	}
	if writes == nil {
		return fmt.Errorf("NFS workspace erase tracking is not configured")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	intent := models.WorkspaceWriteIntent{Operation: hex.EncodeToString(nonce[:]), InstanceID: instance.ID, OwnerID: instance.UserID, Generation: instance.RuntimeGeneration, Action: "erase"}
	if err = writes.Begin(ctx, intent); err != nil {
		return err
	}
	request.Action = "clear"
	_, _, ended, err := runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	if err == nil && ended {
		request.Action, request.UID = "remove", 10001
		_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	}
	if err == nil && ended && runtimeType == RuntimeTypeOpenClaw {
		request.Action, request.UID = "browser_clear", RuntimeLinuxID(instance.ID)
		_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
		if err == nil && ended {
			request.Action, request.UID = "browser_remove", 10001
			_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
		}
	}
	if ended && !errors.Is(err, ErrWorkspaceNFSUncertain) {
		service := &workspaceFileService{nfsWrites: writes}
		err = errors.Join(err, service.settleNFSIntents(ctx, []models.WorkspaceWriteIntent{intent}))
	}
	return err
}

// PatchNFSBrowserConfig performs only the existing fixed protected-browser JSON
// edit. Its caller holds the instance mutation guard and durable write intent.
func PatchNFSBrowserConfig(ctx context.Context, root string, instance *models.Instance, host string, enabled bool) (ended bool, err error) {
	request, err := nfsInstanceManagementRequest(root, RuntimeTypeOpenClaw, instance)
	if err != nil {
		return true, err
	}
	if !validNFSBrowserHost(instance.ID, host) {
		return true, ErrWorkspacePathInvalid
	}
	request.Action, request.BrowserHost, request.BrowserEnabled = "browser_config", host, enabled
	_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	return ended, err
}

func validNFSBrowserHost(instanceID int, host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil || port != "9222" {
		return false
	}
	prefix := fmt.Sprintf("clawbrowser-%d.", instanceID)
	suffix := ".svc.cluster.local"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	namespace := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if len(namespace) == 0 || len(namespace) > 63 || namespace[0] == '-' || namespace[len(namespace)-1] == '-' {
		return false
	}
	for _, c := range namespace {
		if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// ConfigureRuntimeUpgradeNFSIO enables credential-scoped permanent operation
// locks on the same storage root used by Runtime migration workers.
func ConfigureRuntimeUpgradeNFSIO(service *RuntimeUpgradeService, profile string) {
	if service != nil {
		service.nfsIOProfile = profile
	}
}

func workspaceManagementMutation(action string) bool {
	switch action {
	case "clear", "remove", "browser_config", "browser_grant", "browser_parents", "browser_migrate", "browser_clear", "browser_remove":
		return true
	}
	return false
}
