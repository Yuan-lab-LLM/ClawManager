package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestManagedPreviewImageRequiresTrustedImmutableReadyPool(t *testing.T) {
	for _, scenario := range []string{"valid", "presentation-tag", "bare-digest", "docker-pullable", "containerd-digest", "tag", "malformed-digest", "old-image", "wrong-image-id", "empty-image-id", "unknown-runtime-prefix", "not-running", "unready", "no-slots", "wrong-identity"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, kube, _, pod := securityFixture(t)
			image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("a", 64)
			pod.Spec.Containers = []corev1.Container{{Name: "runtime-agent", Image: image}, {Name: "slot-0", Image: image}}
			for _, c := range pod.Spec.Containers {
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: c.Name, Image: image, ImageID: image, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
			}
			images := map[string]string{"opencode": image}
			id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: string(pod.UID)}
			switch scenario {
			case "tag":
				images["opencode"] = "clawmanager.local/opencode-lite:latest"
			case "malformed-digest":
				images["opencode"] = "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("z", 64)
			case "old-image":
				pod.Spec.Containers[1].Image = "clawmanager.local/opencode-lite:old"
			case "presentation-tag":
				for i := range pod.Status.ContainerStatuses {
					pod.Status.ContainerStatuses[i].Image = "clawmanager.local/opencode-lite:container-build-tag"
				}
			case "bare-digest":
				pod.Status.ContainerStatuses[1].ImageID = "sha256:" + strings.Repeat("a", 64)
			case "docker-pullable":
				pod.Status.ContainerStatuses[1].ImageID = "docker-pullable://" + image
			case "containerd-digest":
				pod.Status.ContainerStatuses[1].ImageID = "containerd://sha256:" + strings.Repeat("a", 64)
			case "wrong-image-id":
				pod.Status.ContainerStatuses[1].ImageID = "sha256:" + strings.Repeat("b", 64)
			case "empty-image-id":
				pod.Status.ContainerStatuses[1].ImageID = ""
			case "unknown-runtime-prefix":
				pod.Status.ContainerStatuses[1].ImageID = "unexpected://" + image
			case "not-running":
				pod.Status.ContainerStatuses[1].State = corev1.ContainerState{}
			case "unready":
				pod.Status.ContainerStatuses[1].Ready = false
			case "no-slots":
				pod.Spec.Containers = pod.Spec.Containers[:1]
				pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
			case "wrong-identity":
				id.Generation++
			}
			if _, err := kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err := a.VerifyManagedPreviewImage(context.Background(), id, images)
			wantValid := scenario == "valid" || scenario == "presentation-tag" || scenario == "bare-digest" || scenario == "docker-pullable" || scenario == "containerd-digest"
			if (err == nil) != wantValid {
				t.Fatalf("incorrect image authorization: %v", err)
			}
		})
	}
}
