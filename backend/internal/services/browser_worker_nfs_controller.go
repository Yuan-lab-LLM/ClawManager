package services

import (
	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func (s *BrowserWorkerService) browserControllerKind() string {
	if s.client.RuntimeSandboxProfile == cmk8s.RuntimeNFSRootlessProfile {
		return "StatefulSet"
	}
	return "Deployment"
}

// A common read-only Deployment-shaped view keeps readiness and stop checks
// identical for both controller kinds. NFS API writes always use StatefulSets.
func browserStatefulSetView(set *appsv1.StatefulSet) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: set.ObjectMeta, Spec: appsv1.DeploymentSpec{Replicas: set.Spec.Replicas, Selector: set.Spec.Selector, Template: set.Spec.Template}, Status: appsv1.DeploymentStatus{ObservedGeneration: set.Status.ObservedGeneration, Replicas: set.Status.Replicas, UpdatedReplicas: set.Status.UpdatedReplicas, ReadyReplicas: set.Status.ReadyReplicas, AvailableReplicas: set.Status.AvailableReplicas}}
}
func (s *BrowserWorkerService) getBrowserWorkload(ctx context.Context, id int, kind string) (*appsv1.Deployment, error) {
	if kind != "StatefulSet" {
		return s.client.Clientset.AppsV1().Deployments(s.namespace()).Get(ctx, s.name(id), metav1.GetOptions{})
	}
	evidence, _, err := s.loadNFSBrowserRegistry(ctx, id, false)
	if err != nil {
		return nil, err
	}
	set, err := s.client.Clientset.AppsV1().StatefulSets(s.namespace()).Get(ctx, s.nfsBrowserControllerName(id, evidence), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return browserStatefulSetView(set), nil
}
func desiredNFSBrowserStatefulSet(dep *appsv1.Deployment) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{ObjectMeta: dep.ObjectMeta, Spec: appsv1.StatefulSetSpec{Replicas: dep.Spec.Replicas, Selector: dep.Spec.Selector, Template: dep.Spec.Template, ServiceName: strings.TrimSuffix(dep.Name, "-"+dep.Spec.Template.Annotations[nfsBrowserEpochAnnotation]), PodManagementPolicy: appsv1.OrderedReadyPodManagement, UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType}}}
}
func (s *BrowserWorkerService) createNFSBrowserWorkload(ctx context.Context, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	set, err := s.client.Clientset.AppsV1().StatefulSets(s.namespace()).Create(ctx, desiredNFSBrowserStatefulSet(dep), metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return browserStatefulSetView(set), nil
}
func (s *BrowserWorkerService) updateBrowserWorkload(ctx context.Context, dep *appsv1.Deployment, kind string) (*appsv1.Deployment, error) {
	if kind != "StatefulSet" {
		return s.client.Clientset.AppsV1().Deployments(s.namespace()).Update(ctx, dep, metav1.UpdateOptions{})
	}
	sets := s.client.Clientset.AppsV1().StatefulSets(s.namespace())
	set, err := sets.Get(ctx, dep.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if set.UID != dep.UID || set.ResourceVersion != dep.ResourceVersion {
		return nil, fmt.Errorf("browser controller identity/version changed")
	}
	set.ObjectMeta = dep.ObjectMeta
	set.Spec.Replicas = dep.Spec.Replicas
	set.Spec.Template = dep.Spec.Template
	set, err = sets.Update(ctx, set, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return browserStatefulSetView(set), nil
}
func (s *BrowserWorkerService) applyNFSBrowserWorkload(ctx context.Context, instance *models.Instance, deployment *appsv1.Deployment) error {
	if err := s.removeStoppedNFSBrowserDeployment(ctx, instance.ID); err != nil {
		return err
	}
	evidence, revision, err := s.loadNFSBrowserRegistry(ctx, instance.ID, false)
	if err != nil {
		return err
	}
	if evidence.Admitted && evidence.ControllerKind != "StatefulSet" {
		return errNFSBrowserLegacyFence
	}
	if err = s.verifyNFSBrowserTombstones(ctx, instance.ID, evidence); err != nil {
		return err
	}
	if evidence.Admitted && !evidence.Stopped {
		existing, err := s.getBrowserWorkload(ctx, instance.ID, "StatefulSet")
		if err != nil {
			return err
		}
		if string(existing.UID) != evidence.DeploymentUID {
			return fmt.Errorf("browser controller identity changed")
		}
		template := deployment.Spec.Template.DeepCopy()
		if template.Annotations == nil {
			template.Annotations = map[string]string{}
		}
		template.Annotations[nfsBrowserEpochAnnotation] = evidence.Epoch
		if existing.Spec.Template.Labels[nfsBrowserEpochAnnotation] != "" {
			template.Labels[nfsBrowserEpochAnnotation] = evidence.Epoch
		}
		unchanged := !evidence.Stopping && equality.Semantic.DeepDerivative(*template, existing.Spec.Template) && existing.Spec.Replicas != nil && deployment.Spec.Replicas != nil && *existing.Spec.Replicas == *deployment.Spec.Replicas
		if unchanged {
			pod, podErr := s.client.Clientset.CoreV1().Pods(s.namespace()).Get(ctx, existing.Name+"-0", metav1.GetOptions{})
			if podErr == nil {
				unchanged = pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded
			} else if !apierrors.IsNotFound(podErr) {
				return podErr
			} else if len(evidence.Pods) > 0 {
				return fmt.Errorf("admitted browser Pod disappeared without termination proof")
			}
		}
		if unchanged {
			return s.observeNFSBrowserPods(ctx, instance.ID)
		}
		if err = s.stopNFSBrowserWriter(ctx, instance.ID, false); err != nil {
			return err
		}
		evidence, revision, err = s.loadNFSBrowserRegistry(ctx, instance.ID, false)
		if err != nil {
			return err
		}
	}
	if err = s.verifyNFSBrowserTombstones(ctx, instance.ID, evidence); err != nil {
		return err
	}
	retired := evidence.Retired
	if evidence.Admitted {
		if !evidence.Stopped {
			return fmt.Errorf("prior browser epoch is not stopped")
		}
		retired = append(retired, nfsBrowserRetiredEpoch{ControllerName: s.nfsBrowserControllerName(instance.ID, evidence), ControllerUID: evidence.DeploymentUID, Epoch: evidence.Epoch, Pods: evidence.Pods})
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	epoch := hex.EncodeToString(nonce[:])
	name := s.name(instance.ID) + "-" + epoch
	evidence = nfsBrowserStopEvidence{ControllerKind: "StatefulSet", ControllerName: name, Admitted: true, Epoch: epoch, Pods: map[string]nfsBrowserPodEvidence{}, Retired: retired}
	deployment.Name = name
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[nfsBrowserEpochAnnotation] = epoch
	deployment.Spec.Template.Labels[nfsBrowserEpochAnnotation] = epoch
	deployment.Spec.Selector.MatchLabels[nfsBrowserEpochAnnotation] = epoch
	if err = s.persistNFSBrowserRegistry(ctx, instance.ID, &revision, evidence); err != nil {
		return err
	}
	applied, err := s.createNFSBrowserWorkload(ctx, deployment)
	if err != nil {
		return s.nfsBrowserResourceMutationError(err)
	}
	if applied.UID == "" {
		return s.nfsBrowserResourceMutationError(fmt.Errorf("created browser has no controller UID"))
	}
	evidence.DeploymentUID = string(applied.UID)
	if err = s.persistNFSBrowserRegistry(ctx, instance.ID, &revision, evidence); err != nil {
		return s.nfsBrowserResourceMutationError(err)
	}
	return s.observeNFSBrowserPods(ctx, instance.ID)
}

func (s *BrowserWorkerService) nfsBrowserControllerName(id int, evidence nfsBrowserStopEvidence) string {
	if evidence.ControllerName != "" {
		return evidence.ControllerName
	}
	return s.name(id)
}

// There is deliberately no timeout, TTL, pruning, or GC. SQL and API name
// occupancy must both survive retirement. The existing journal-size cap fails
// closed rather than discarding an old fence to admit another epoch.
func (s *BrowserWorkerService) verifyNFSBrowserTombstones(ctx context.Context, id int, evidence nfsBrowserStopEvidence) error {
	retired := append([]nfsBrowserRetiredEpoch(nil), evidence.Retired...)
	if evidence.Stopped {
		retired = append(retired, nfsBrowserRetiredEpoch{ControllerName: s.nfsBrowserControllerName(id, evidence), ControllerUID: evidence.DeploymentUID, Epoch: evidence.Epoch, Pods: evidence.Pods})
	}
	for _, epoch := range retired {
		owner, err := s.client.Clientset.AppsV1().StatefulSets(s.namespace()).Get(ctx, epoch.ControllerName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("retired browser controller is unavailable: %w", err)
		}
		if string(owner.UID) != epoch.ControllerUID || owner.Spec.Replicas == nil || *owner.Spec.Replicas != 0 {
			return fmt.Errorf("retired browser controller identity changed")
		}
		if len(epoch.Pods) != 1 {
			return fmt.Errorf("retired browser ordinal proof is incomplete")
		}
		for uid, record := range epoch.Pods {
			pod, err := s.client.Clientset.CoreV1().Pods(s.namespace()).Get(ctx, record.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("retired browser name tombstone is unavailable: %w", err)
			}
			proof := nfsBrowserStopEvidence{ControllerKind: "StatefulSet", DeploymentUID: epoch.ControllerUID, Epoch: epoch.Epoch, Pods: epoch.Pods}
			hash, terminal := nfsBrowserPodTerminated(pod)
			if pod.DeletionTimestamp == nil || string(pod.UID) != uid || !record.Complete || !terminal || hash != record.ContainersSHA256 || validateNFSBrowserOrdinal(proof, pod, epoch.ControllerName) != nil {
				return fmt.Errorf("retired browser name tombstone proof changed")
			}
		}
	}
	return nil
}

// Historical ReplicaSet requests have no durable per-execution admission gate.
// Even a finalizer-protected template cannot fence a delayed Create from starting.
func (s *BrowserWorkerService) removeStoppedNFSBrowserDeployment(ctx context.Context, id int) error {
	_, err := s.client.Clientset.AppsV1().Deployments(s.namespace()).Get(ctx, s.name(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return errNFSBrowserLegacyFence
}

func knownNFSBrowserRetiredPod(evidence nfsBrowserStopEvidence, pod *corev1.Pod) bool {
	for _, old := range evidence.Retired {
		if old.Epoch == pod.Annotations[nfsBrowserEpochAnnotation] {
			record, ok := old.Pods[string(pod.UID)]
			hash, done := nfsBrowserPodTerminated(pod)
			if ok && record.Name == pod.Name && record.Complete && done && record.ContainersSHA256 == hash {
				return true
			}
		}
	}
	return false
}
