package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

// Optional: legacy deployment fakes need not implement native ownership proof.
type RuntimePodControllerResolver interface {
	ResolveRuntimePodController(context.Context, string, string, string) (RuntimePodControllerIdentity, error)
}
type RuntimePodControllerIdentity struct {
	Namespace, PodName, PodUID, DeploymentName, DeploymentUID, RuntimeType string
}

func (s *runtimeDeploymentService) ResolveRuntimePodController(ctx context.Context, namespace, podName, uid string) (RuntimePodControllerIdentity, error) {
	empty := RuntimePodControllerIdentity{}
	if s == nil || s.client == nil || namespace == "" || strings.TrimSpace(uid) == "" {
		return empty, fmt.Errorf("current native Pod identity is required")
	}
	var pod *corev1.Pod
	if podName != "" {
		current, err := s.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return empty, err
		}
		pod = current
	} else {
		list, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return empty, err
		}
		for i := range list.Items {
			current := &list.Items[i]
			if current.Namespace == namespace && string(current.UID) == uid {
				if pod != nil {
					return empty, fmt.Errorf("native Pod UID is ambiguous")
				}
				pod = current
			}
		}
	}
	if pod == nil || pod.Namespace != namespace || string(pod.UID) != uid {
		return empty, fmt.Errorf("native Pod namespace/name/UID changed")
	}
	ref := metav1.GetControllerOf(pod)
	if ref == nil || ref.Kind != "ReplicaSet" {
		return empty, fmt.Errorf("native Pod must have a ReplicaSet controller")
	}
	rs, err := s.client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return empty, err
	}
	if rs.UID != ref.UID {
		return empty, fmt.Errorf("native ReplicaSet UID changed")
	}
	owner := metav1.GetControllerOf(rs)
	if owner == nil || owner.Kind != "Deployment" {
		return empty, fmt.Errorf("native ReplicaSet must have a Deployment controller")
	}
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return empty, err
	}
	if deployment.UID != owner.UID || deployment.UID == "" {
		return empty, fmt.Errorf("native Deployment UID changed")
	}
	return RuntimePodControllerIdentity{Namespace: namespace, PodName: pod.Name, PodUID: uid, DeploymentName: deployment.Name, DeploymentUID: string(deployment.UID), RuntimeType: strings.ToLower(strings.TrimSpace(deployment.Labels["clawmanager.io/runtime-type"]))}, nil
}
