package services

import (
	"context"
	"fmt"
	"maps"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// modelActivityLegacyCutoff permits retiring only NULL-lease requests started
// before every observed current application process. New processes must always
// write leased requests. Any incomplete or changing rollout retains legacy rows.
func modelActivityLegacyCutoff(ctx context.Context, kube kubernetes.Interface, namespace, podName string) (*time.Time, error) {
	if kube == nil || namespace == "" || podName == "" {
		return nil, fmt.Errorf("legacy model activity proof requires exact Kubernetes identity")
	}
	own, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	process, err := modelActivityLegacyProcessForPod(own)
	if err != nil {
		return nil, err
	}
	if own.Namespace != namespace || own.Name != podName || own.DeletionTimestamp != nil {
		return nil, fmt.Errorf("legacy model activity current Pod identity is unstable")
	}
	owner := modelActivityLegacyController(own, "ReplicaSet")
	if owner == nil {
		return nil, fmt.Errorf("legacy model activity current Pod has unknown controller")
	}
	rs, err := kube.AppsV1().ReplicaSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if rs.UID != owner.UID || rs.Namespace != namespace {
		return nil, fmt.Errorf("legacy model activity ReplicaSet identity changed")
	}
	deploymentOwner := modelActivityLegacyController(rs, "Deployment")
	if deploymentOwner == nil {
		return nil, fmt.Errorf("legacy model activity ReplicaSet has unknown controller")
	}
	deployment, err := kube.AppsV1().Deployments(namespace).Get(ctx, deploymentOwner.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if deployment.UID != deploymentOwner.UID || deployment.Namespace != namespace || deployment.DeletionTimestamp != nil {
		return nil, fmt.Errorf("legacy model activity Deployment identity changed")
	}

	var previous map[types.UID]modelActivityLegacyProcess
	var cutoff time.Time
	for pass := 0; pass < 2; pass++ {
		sets, err := kube.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		byUID := make(map[types.UID]*appsv1.ReplicaSet, len(sets.Items))
		for i := range sets.Items {
			byUID[sets.Items[i].UID] = &sets.Items[i]
		}
		listedOwnSet := byUID[rs.UID]
		if listedOwnSet == nil || listedOwnSet.Name != rs.Name || !modelActivityLegacyOwnedBy(listedOwnSet, "Deployment", deployment.Name, deployment.UID) {
			return nil, fmt.Errorf("legacy model activity ReplicaSet ownership changed")
		}
		pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		current := make(map[types.UID]modelActivityLegacyProcess)
		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			application := false
			for _, container := range pod.Spec.Containers {
				if container.Name == "clawmanager-app" {
					application = true
				}
			}
			podOwner := modelActivityLegacyController(pod, "ReplicaSet")
			var set *appsv1.ReplicaSet
			if podOwner != nil {
				set = byUID[podOwner.UID]
			}
			if set == nil || set.Name != podOwner.Name {
				// An orphaned CM Pod could be a live process from a deleted old RS.
				if application {
					return nil, fmt.Errorf("legacy model activity Pod %s has unknown ownership", pod.Name)
				}
				continue
			}
			setOwner := modelActivityLegacyController(set, "Deployment")
			if setOwner == nil {
				if application {
					return nil, fmt.Errorf("legacy model activity ReplicaSet %s has unknown ownership", set.Name)
				}
				continue
			}
			if setOwner.UID != deployment.UID {
				continue
			}
			if setOwner.Name != deployment.Name {
				return nil, fmt.Errorf("legacy model activity Deployment owner mismatch")
			}
			observed, err := modelActivityLegacyProcessForPod(pod)
			if err != nil {
				return nil, err
			}
			if observed.image != process.image || observed.imageID != process.imageID {
				return nil, fmt.Errorf("legacy model activity Pod %s still runs another application image", pod.Name)
			}
			current[pod.UID] = observed
			if cutoff.IsZero() || observed.startedAt.Before(cutoff) {
				cutoff = observed.startedAt
			}
		}
		if observed, ok := current[own.UID]; !ok || observed != process {
			return nil, fmt.Errorf("legacy model activity current application process changed")
		}
		if pass > 0 && !maps.Equal(previous, current) {
			return nil, fmt.Errorf("legacy model activity application processes changed during proof")
		}
		previous = current
	}
	fresh, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	freshProcess, err := modelActivityLegacyProcessForPod(fresh)
	if err != nil {
		return nil, err
	}
	if fresh.UID != own.UID || fresh.DeletionTimestamp != nil || freshProcess != process || !modelActivityLegacyOwnedBy(fresh, "ReplicaSet", rs.Name, rs.UID) {
		return nil, fmt.Errorf("legacy model activity current Pod changed during proof")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &cutoff, nil
}

type modelActivityLegacyProcess struct {
	image, imageID, containerID string
	startedAt                   time.Time
}

func modelActivityLegacyProcessForPod(pod *corev1.Pod) (modelActivityLegacyProcess, error) {
	var process modelActivityLegacyProcess
	if pod.UID == "" || pod.Status.Phase != corev1.PodRunning {
		return process, fmt.Errorf("legacy model activity Pod %s is not running with known identity", pod.Name)
	}
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == "clawmanager-app" {
			process.image = container.Image
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "clawmanager-app" && ready && status.Ready && status.State.Running != nil {
			process.imageID, process.containerID, process.startedAt = status.ImageID, status.ContainerID, status.State.Running.StartedAt.Time
		}
	}
	if process.image == "" || process.imageID == "" || process.containerID == "" || process.startedAt.IsZero() {
		return modelActivityLegacyProcess{}, fmt.Errorf("legacy model activity Pod %s lacks a ready identified application process", pod.Name)
	}
	return process, nil
}

func modelActivityLegacyController(object metav1.Object, kind string) *metav1.OwnerReference {
	owner := metav1.GetControllerOf(object)
	if owner == nil || owner.Kind != kind || owner.APIVersion != "apps/v1" || owner.Name == "" || owner.UID == "" {
		return nil
	}
	return owner
}

func modelActivityLegacyOwnedBy(object metav1.Object, kind, name string, uid types.UID) bool {
	owner := modelActivityLegacyController(object, kind)
	return owner != nil && owner.Name == name && owner.UID == uid
}
