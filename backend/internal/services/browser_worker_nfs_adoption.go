package services

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

// Record known historical identities for external recovery, never as authority
// to stop or migrate a Deployment-backed writer. A timed-out ReplicaSet Create
// may still commit after any observed generation/list barrier. Neither patched
// finalizers nor current terminal Pods rule out a late unadmitted NFS writer.
var errNFSBrowserLegacyFence = fmt.Errorf("historical NFS browser Deployment requires an external quiescence fence; automatic cutover is unavailable")

func (s *BrowserWorkerService) recordNFSBrowserLegacyExposure(ctx context.Context, id int) (nfsBrowserStopEvidence, int, error) {
	evidence := nfsBrowserStopEvidence{Admitted: true, Stopping: true, Pods: map[string]nfsBrowserPodEvidence{}}
	dep, err := s.client.Clientset.AppsV1().Deployments(s.namespace()).Get(ctx, s.name(id), metav1.GetOptions{})
	if err != nil {
		return evidence, 0, fmt.Errorf("existing browser owner identity unavailable: %w", err)
	}
	if dep.UID == "" || dep.DeletionTimestamp != nil || dep.Labels["instance-id"] != strconv.Itoa(id) || dep.Labels["managed-by"] != browserWorkerManagedBy || dep.Labels["user-id"] == "" {
		return evidence, 0, fmt.Errorf("existing browser owner identity is invalid")
	}
	if s.instances != nil {
		instance, err := s.instances.GetByID(id)
		if err != nil {
			return evidence, 0, err
		}
		if instance == nil || strconv.Itoa(instance.UserID) != dep.Labels["user-id"] {
			return evidence, 0, fmt.Errorf("existing browser owner changed")
		}
	}
	pods, err := s.client.Clientset.CoreV1().Pods(s.namespace()).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=" + strconv.Itoa(id)})
	if err != nil {
		return evidence, 0, err
	}
	if len(pods.Items) == 0 || len(pods.Items) > 64 {
		return evidence, 0, fmt.Errorf("existing browser has no complete current writer inventory")
	}
	evidence.DeploymentUID = string(dep.UID)
	evidence.Epoch = dep.Spec.Template.Annotations[nfsBrowserEpochAnnotation]
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if pod.UID == "" || owner == nil || owner.Kind != "ReplicaSet" || pod.Labels["user-id"] != dep.Labels["user-id"] || pod.Labels["managed-by"] != browserWorkerManagedBy || pod.Annotations[nfsBrowserEpochAnnotation] != evidence.Epoch {
			return evidence, 0, fmt.Errorf("existing browser Pod identity is invalid")
		}
		rs, err := s.client.Clientset.AppsV1().ReplicaSets(s.namespace()).Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil {
			return evidence, 0, err
		}
		parent := metav1.GetControllerOf(rs)
		if rs.UID != owner.UID || parent == nil || parent.Kind != "Deployment" || parent.UID != dep.UID {
			return evidence, 0, fmt.Errorf("existing browser Pod owner chain changed")
		}
		evidence.Pods[string(pod.UID)] = nfsBrowserPodEvidence{Name: pod.Name}
	}
	revision := 0
	err = s.persistNFSBrowserRegistry(ctx, id, &revision, evidence)
	if err == nil {
		err = errNFSBrowserLegacyFence
	}
	return evidence, revision, err
}

func validateNFSBrowserOrdinal(evidence nfsBrowserStopEvidence, pod *corev1.Pod, name string) error {
	if evidence.ControllerKind != "StatefulSet" {
		return nil
	}
	owner := metav1.GetControllerOf(pod)
	if pod.Name != name+"-0" || owner == nil || owner.Kind != "StatefulSet" || owner.Name != name || string(owner.UID) != evidence.DeploymentUID || pod.Annotations[nfsBrowserEpochAnnotation] != evidence.Epoch {
		return fmt.Errorf("browser ordinal has no matching durable owner/epoch")
	}
	retained := false
	for _, f := range pod.Finalizers {
		if f == nfsBrowserStopFinalizer {
			retained = true
		}
	}
	if !retained {
		return fmt.Errorf("browser ordinal lost its writer finalizer")
	}
	for uid := range evidence.Pods {
		if uid != string(pod.UID) {
			return fmt.Errorf("browser ordinal changed before a new durable admission")
		}
	}
	return nil
}
