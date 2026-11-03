package services

import (
	"context"
	"encoding/hex"
	"strings"

	"clawreef/internal/runtimeauth"
)

// VerifyManagedPreviewImage binds rollout readiness to the administrator's
// immutable pool images and the actual Ready containers in the same assignment
// observation. Tags and business-reported plugin versions cannot retire a
// whole Team's legacy signing authority.
func (a *RuntimeSecurityAuthority) VerifyManagedPreviewImage(ctx context.Context, expected runtimeauth.Identity, images map[string]string) error {
	snapshot, err := a.validatedSnapshot(ctx, expected.InstanceID)
	if err != nil || snapshot.identity != expected || snapshot.pod == nil || snapshot.pod.Spec.HostNetwork {
		return errRuntimeSecurity
	}
	image := images[snapshot.request.AgentType]
	parts := strings.Split(image, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || strings.Contains(parts[0], "@") || len(parts[1]) != 64 || strings.ToLower(parts[1]) != parts[1] {
		return errRuntimeSecurity
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return errRuntimeSecurity
	}
	pod := snapshot.pod
	if len(pod.Spec.Containers) != len(pod.Status.ContainerStatuses) {
		return errRuntimeSecurity
	}
	slots := 0
	for _, container := range pod.Spec.Containers {
		if container.Image != image {
			return errRuntimeSecurity
		}
		if strings.HasPrefix(container.Name, "slot-") {
			slots++
		}
		matches := 0
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name {
				matches++
				// CRI may report a local tag in Image even for an immutable Pod
				// reference. Bind the observed running artifact to the approved
				// digest, rather than trusting that presentation string.
				if !status.Ready || status.State.Running == nil || !approvedRuntimeImageID(status.ImageID, image, parts[1]) {
					return errRuntimeSecurity
				}
			}
		}
		if matches != 1 {
			return errRuntimeSecurity
		}
	}
	if slots == 0 || ctx.Err() != nil {
		return errRuntimeSecurity
	}
	return nil
}

func approvedRuntimeImageID(observed, image, digest string) bool {
	switch observed {
	case image, "docker-pullable://" + image, "sha256:" + digest,
		"containerd://sha256:" + digest, "docker://sha256:" + digest:
		return true
	default:
		return false
	}
}
