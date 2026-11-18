package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func ConfigureBrowserWorkspaceNFSIO(worker *BrowserWorkerService, profile, root string, writes repository.WorkspaceWriteRepository, guard WorkspaceUpgradeMutationGuard) {
	if worker == nil || profile != "nfs-rootless-v1" {
		return
	}
	files := &workspaceFileService{}
	ConfigureWorkspaceNFSIO(files, profile, root, writes)
	ConfigureWorkspaceUpgradeGuard(files, guard)
	worker.nfsWorkspace = files
}

func (s *BrowserWorkerService) patchNFSBrowserConfig(ctx context.Context, instance *models.Instance, enabled bool) error {
	return s.withNFSBrowserMutation(ctx, instance, func() (bool, error) {
		host := fmt.Sprintf("%s.%s.svc.cluster.local:9222", s.name(instance.ID), s.namespace())
		return PatchNFSBrowserConfig(ctx, s.nfsWorkspace.nfsRoot, instance, host, enabled)
	})
}

func (s *BrowserWorkerService) withNFSBrowserMutation(ctx context.Context, instance *models.Instance, run func() (bool, error)) error {
	files := s.nfsWorkspace
	if files == nil || files.nfsWrites == nil || files.mutationGuard == nil || instance == nil || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		return fmt.Errorf("NFS browser workspace write fencing is unavailable")
	}
	scope := WorkspaceFileScope{InstanceID: instance.ID, UserID: instance.UserID, RuntimeGeneration: instance.RuntimeGeneration}
	return files.withMutation(ctx, scope, func() error {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		intent := models.WorkspaceWriteIntent{Operation: hex.EncodeToString(nonce[:]), InstanceID: instance.ID, OwnerID: instance.UserID, Generation: instance.RuntimeGeneration, Action: "edit"}
		if err := files.nfsWrites.Begin(ctx, intent); err != nil {
			return err
		}
		ended, err := run()
		if ended && !errors.Is(err, ErrWorkspaceNFSUncertain) {
			err = errors.Join(err, files.settleNFSIntents(ctx, []models.WorkspaceWriteIntent{intent}))
		}
		return err
	})
}

func configureNFSBrowserWorker(deployment *appsv1.Deployment, instance *models.Instance) error {
	if instance == nil || instance.ID <= 0 || instance.UserID <= 0 || uint64(instance.ID)+RuntimeLinuxIDBase >= 1<<32-1 {
		return fmt.Errorf("persisted browser instance identity is required")
	}
	deployment.Spec.Template.Finalizers = append(deployment.Spec.Template.Finalizers, nfsBrowserStopFinalizer)
	pod := &deployment.Spec.Template.Spec
	pod.HostUsers = boolptr(true)
	uid := int64(RuntimeLinuxID(instance.ID))
	prepare := &pod.InitContainers[0]
	profileSubPath := fmt.Sprintf("browsers/user-%d/instance-%d/profile", instance.UserID, instance.ID)
	prepare.VolumeMounts[0].SubPath = profileSubPath
	pod.Containers[0].VolumeMounts[0].SubPath = profileSubPath
	pod.Containers[0].VolumeMounts[0].MountPath = "/config/chromium"
	pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts,
		corev1.VolumeMount{Name: "browser-profile", MountPath: "/config/Downloads", SubPath: fmt.Sprintf("browsers/user-%d/instance-%d/downloads", instance.UserID, instance.ID)},
		corev1.VolumeMount{Name: "browser-local-state", MountPath: "/config"})
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "browser-local-state", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	prepare.Args = []string{"umask 077; mkdir -p /workspace"}
	prepare.SecurityContext = &corev1.SecurityContext{RunAsUser: &uid, RunAsGroup: &uid, RunAsNonRoot: boolptr(true), AllowPrivilegeEscalation: boolptr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	// Only the new local emptyDir is mounted here. Root's pinned image init
	// writes local supervisor state; exact precreated app files need no recursive
	// ownership repair and persistent NFS leaves are never exposed to this step.
	zero := int64(0)
	localScript := fmt.Sprintf("umask 077; mkdir -p /workspace/log/chromium /workspace/xdg/cache/fontconfig; touch /workspace/log/chromium/output.log /workspace/log/chromium/error.log; chown %d:%d /workspace/log/chromium/output.log /workspace/log/chromium/error.log /workspace/log/chromium /workspace/log /workspace/xdg/cache/fontconfig /workspace/xdg/cache /workspace/xdg /workspace", uid, uid)
	pod.InitContainers = append(pod.InitContainers, corev1.Container{Name: "prepare-local-state", Image: prepare.Image, ImagePullPolicy: prepare.ImagePullPolicy, Command: []string{"/bin/sh", "-c"}, Args: []string{localScript}, VolumeMounts: []corev1.VolumeMount{{Name: "browser-local-state", MountPath: "/workspace"}}, SecurityContext: &corev1.SecurityContext{RunAsUser: &zero, RunAsGroup: &zero, AllowPrivilegeEscalation: boolptr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"CHOWN"}}}})
	browser := &pod.Containers[0]
	// The image's trusted init configures the application identity. Do not let
	// its default ownership repair recurse through existing NFS profile data.
	browser.Env = append(browser.Env, corev1.EnvVar{Name: "USER_ID", Value: strconv.FormatInt(uid, 10)}, corev1.EnvVar{Name: "GROUP_ID", Value: strconv.FormatInt(uid, 10)}, corev1.EnvVar{Name: "TAKE_CONFIG_OWNERSHIP", Value: "0"})
	return nil
}

// PrepareNFSBrowserProfile may run only after the legacy browser writer stopped.
// A single guarded SQL intent must span its grant and atomic migration helpers.
func PrepareNFSBrowserProfile(ctx context.Context, root string, instance *models.Instance) (bool, error) {
	request, err := nfsInstanceManagementRequest(root, RuntimeTypeOpenClaw, instance)
	if err != nil {
		return true, err
	}
	request.Action, request.UID, request.GID = "browser_parents", 10001, 10001
	_, _, ended, err := runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	if err != nil || !ended {
		return ended, err
	}
	request.Action, request.GID = "browser_grant", RuntimeLinuxID(instance.ID)
	_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	if err != nil || !ended {
		return ended, err
	}
	request.Action, request.UID = "browser_migrate", RuntimeLinuxID(instance.ID)
	_, _, ended, err = runWorkspaceNFSHelperCommand(ctx, workspaceManagementHelperCommand, request, nil, nil)
	return ended, err
}

func (s *BrowserWorkerService) nfsBrowserResourceMutationError(err error) error {
	if err != nil && s.client.RuntimeSandboxProfile == "nfs-rootless-v1" {
		return errors.Join(ErrWorkspaceNFSUncertain, err)
	}
	return err
}
