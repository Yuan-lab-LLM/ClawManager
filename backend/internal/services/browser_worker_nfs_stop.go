package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

const nfsBrowserStopFinalizer = "clawmanager.io/nfs-browser-writer"
const nfsBrowserEpochAnnotation = "clawmanager.io/nfs-browser-epoch"
const nfsBrowserStopAnnotation = "clawmanager.io/nfs-browser-stop"

type nfsBrowserPodEvidence struct {
	Name             string
	Complete         bool
	ContainersSHA256 string
}
type nfsBrowserRetiredEpoch struct {
	ControllerName string
	ControllerUID  string
	Epoch          string
	Pods           map[string]nfsBrowserPodEvidence
}
type nfsBrowserStopEvidence struct {
	ControllerName string
	Retired        []nfsBrowserRetiredEpoch
	ControllerKind string
	Epoch          string
	Admitted       bool
	Stopping       bool
	Stopped        bool
	DeploymentUID  string
	Pods           map[string]nfsBrowserPodEvidence
}

// Retired Pod names remain occupied forever. Positive container termination
// authorizes another uniquely named epoch, never removal of the old finalizer.
// This also fences earlier ambiguous API Creates for the retired ordinal.
func (s *BrowserWorkerService) stopNFSBrowserWriter(ctx context.Context, instanceID int, onlyLegacy bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	evidence, revision, err := s.loadNFSBrowserRegistry(ctx, instanceID, true)
	if err != nil {
		return err
	}
	if evidence.Admitted && evidence.ControllerKind != "StatefulSet" {
		return errNFSBrowserLegacyFence
	}
	if err := s.verifyNFSBrowserTombstones(ctx, instanceID, evidence); err != nil {
		return err
	}
	if evidence.Stopped {
		return nil
	}
	namespace := s.namespace()
	pods := s.client.Clientset.CoreV1().Pods(namespace)
	deployment, err := s.getBrowserWorkload(ctx, instanceID, evidence.ControllerKind)
	selector := "app=clawbrowser,instance-id=" + strconv.Itoa(instanceID) + ",managed-by=" + browserWorkerManagedBy
	if apierrors.IsNotFound(err) {
		remaining, listErr := pods.List(ctx, metav1.ListOptions{LabelSelector: selector})
		if listErr != nil {
			return listErr
		}
		if len(remaining.Items) != 0 {
			return fmt.Errorf("browser writer owner is missing; termination is unconfirmed")
		}
		if evidence.Admitted && !evidence.Stopped {
			return fmt.Errorf("admitted browser owner disappeared without termination proof")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if deployment.UID == "" {
		return fmt.Errorf("browser Deployment UID is unavailable")
	}
	if !evidence.Admitted || evidence.DeploymentUID == "" || evidence.DeploymentUID != string(deployment.UID) {
		return fmt.Errorf("browser Deployment has no matching durable admission")
	}
	if raw := deployment.Annotations[nfsBrowserStopAnnotation]; raw != "" {
		var old nfsBrowserStopEvidence
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &old) != nil || old.DeploymentUID != evidence.DeploymentUID || old.Epoch != evidence.Epoch || old.Pods == nil {
			return fmt.Errorf("browser stop receipt identity is invalid")
		}
		evidence.Stopping = true
		for uid, record := range old.Pods {
			if prior, ok := evidence.Pods[uid]; ok && prior.Name != record.Name {
				return fmt.Errorf("browser stop Pod identity changed")
			}
			if !evidence.Pods[uid].Complete {
				evidence.Pods[uid] = record
			}
		}
	}
	if onlyLegacy && evidence.ControllerKind == "StatefulSet" && !evidence.Stopping && !evidence.Stopped {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			for _, mount := range container.VolumeMounts {
				if container.Name == "browser" && mount.Name == "browser-profile" && mount.SubPath == fmt.Sprintf("browsers/user-%s/instance-%d/profile", deployment.Labels["user-id"], instanceID) {
					return nil
				}
			}
		}
	}
	evidence.Stopping = true
	if err = s.persistNFSBrowserRegistry(ctx, instanceID, &revision, evidence); err != nil {
		return err
	}

	for uid, record := range evidence.Pods {
		if uid == "" || record.Name == "" {
			return fmt.Errorf("browser stop receipt lacks Pod identity")
		}
		if record.Complete {
			decoded, err := hex.DecodeString(record.ContainersSHA256)
			if err != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("browser termination receipt lacks container identity")
			}
		}
	}
	current, err := pods.List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, pod := range current.Items {
		if evidence.Epoch != "" && pod.Annotations[nfsBrowserEpochAnnotation] != evidence.Epoch {
			if !knownNFSBrowserRetiredPod(evidence, &pod) {
				return fmt.Errorf("browser has an untracked execution epoch")
			}
			continue
		}
		if !evidence.Stopped {
			if err := validateNFSBrowserOrdinal(evidence, &pod, s.nfsBrowserControllerName(instanceID, evidence)); err != nil {
				return err
			}
		}
		if pod.UID == "" {
			return fmt.Errorf("browser Pod UID is unavailable")
		}
		key := string(pod.UID)
		if old, ok := evidence.Pods[key]; ok && old.Name != pod.Name {
			return fmt.Errorf("browser Pod stop receipt changed identity")
		}
		if _, ok := evidence.Pods[key]; !ok {
			evidence.Pods[key] = nfsBrowserPodEvidence{Name: pod.Name}
		}
	}
	if len(evidence.Pods) > 64 {
		return fmt.Errorf("browser stop receipt exceeds bounded Pod inventory")
	}
	if len(evidence.Pods) == 0 && !evidence.Stopped {
		return fmt.Errorf("browser Deployment reports an unobserved writer")
	}
	persist := func() error {
		if err := s.persistNFSBrowserRegistry(ctx, instanceID, &revision, evidence); err != nil {
			return err
		}
		stopRequested := deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest, err := s.getBrowserWorkload(ctx, instanceID, evidence.ControllerKind)
			if err != nil {
				return err
			}
			if string(latest.UID) != evidence.DeploymentUID {
				return fmt.Errorf("browser Deployment identity changed during stop")
			}
			if raw := latest.Annotations[nfsBrowserStopAnnotation]; raw != "" {
				var previous nfsBrowserStopEvidence
				if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &previous) != nil || previous.DeploymentUID != evidence.DeploymentUID || previous.Epoch != evidence.Epoch {
					return fmt.Errorf("browser stop receipt changed identity")
				}
				for uid, record := range previous.Pods {
					if uid == "" || record.Name == "" {
						return fmt.Errorf("browser stop receipt lacks Pod identity")
					}
					if record.Complete {
						decoded, err := hex.DecodeString(record.ContainersSHA256)
						if err != nil || len(decoded) != sha256.Size {
							return fmt.Errorf("browser stop receipt lacks container identity")
						}
					}
					current, ok := evidence.Pods[uid]
					if !ok {
						evidence.Pods[uid] = record
					} else if current.Name != record.Name {
						return fmt.Errorf("browser stop receipt changed Pod identity")
					} else if record.Complete {
						if current.Complete && current.ContainersSHA256 != record.ContainersSHA256 {
							return fmt.Errorf("browser terminal container identity changed")
						}
						evidence.Pods[uid] = record
					}
				}
			}
			if len(evidence.Pods) > 64 {
				return fmt.Errorf("browser stop receipt exceeds bounded Pod inventory")
			}
			data, err := json.Marshal(evidence)
			if err != nil {
				return err
			}
			if latest.Annotations == nil {
				latest.Annotations = map[string]string{}
			}
			latest.Annotations[nfsBrowserStopAnnotation] = string(data)
			if stopRequested {
				zero := int32(0)
				latest.Spec.Replicas = &zero
			}
			deployment, err = s.updateBrowserWorkload(ctx, latest, evidence.ControllerKind)
			return err
		})
	}
	// Persist observed identities BEFORE a Pod can disappear while installing its
	// finalizer. A failed update leaves admission closed for a later reconciliation.
	if err = persist(); err != nil {
		return err
	}
	for uid, record := range evidence.Pods {
		if record.Complete {
			continue
		}
		pod, err := pods.Get(ctx, record.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("browser writer %s termination unconfirmed: %w", uid, err)
		}
		if string(pod.UID) != uid {
			return fmt.Errorf("browser Pod name was reused before termination proof")
		}
		if hash, done := nfsBrowserPodTerminated(pod); done {
			record.Complete, record.ContainersSHA256 = true, hash
			evidence.Pods[uid] = record
			continue
		}
		hasFinalizer := false
		for _, value := range pod.Finalizers {
			if value == nfsBrowserStopFinalizer {
				hasFinalizer = true
			}
		}
		if !hasFinalizer {
			pod.Finalizers = append(pod.Finalizers, nfsBrowserStopFinalizer)
			if _, err = pods.Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}
	zero := int32(0)
	deployment.Spec.Replicas = &zero
	if err = persist(); err != nil {
		return err
	}

	for uid, record := range evidence.Pods {
		pinned := types.UID(uid)
		if err = pods.Delete(ctx, record.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pinned}}); err != nil {
			return err
		}
	}
	// Return on caller cancellation without discarding the ledger/finalizers.
	// A later controller retry must observe real termination before proceeding.
	for {
		pending, changed := false, false
		for uid, record := range evidence.Pods {
			if record.Complete {
				continue
			}
			pod, err := pods.Get(ctx, record.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("browser writer %s termination unconfirmed: %w", uid, err)
			}
			if string(pod.UID) != uid {
				return fmt.Errorf("browser Pod identity changed before termination proof")
			}
			if hash, done := nfsBrowserPodTerminated(pod); done {
				record.Complete, record.ContainersSHA256 = true, hash
				evidence.Pods[uid] = record
				changed = true
			} else {
				pending = true
			}
		}
		if changed {
			if err = persist(); err != nil {
				return err
			}
		}
		if !pending {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	remaining, err := pods.List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, pod := range remaining.Items {
		if evidence.Epoch != "" && pod.Annotations[nfsBrowserEpochAnnotation] != evidence.Epoch {
			if !knownNFSBrowserRetiredPod(evidence, &pod) {
				return fmt.Errorf("browser has an untracked execution epoch")
			}
			continue
		}
		record, ok := evidence.Pods[string(pod.UID)]
		if !ok || !record.Complete {
			return fmt.Errorf("browser writer appeared during stop; reconciliation required")
		}
	}
	evidence.Stopped = true
	if err = s.persistNFSBrowserRegistry(ctx, instanceID, &revision, evidence); err != nil {
		return err
	}
	// Never free this name: an earlier timed-out Create can still commit.
	return s.verifyNFSBrowserTombstones(ctx, instanceID, evidence)
}

func nfsBrowserPodTerminated(pod *corev1.Pod) (string, bool) {
	if pod == nil || (pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded) {
		return "", false
	}
	var identities []string
	check := func(specs []corev1.Container, statuses []corev1.ContainerStatus) bool {
		byName := map[string]corev1.ContainerStatus{}
		for _, status := range statuses {
			byName[status.Name] = status
		}
		for _, container := range specs {
			status, ok := byName[container.Name]
			if !ok || status.State.Terminated == nil {
				return false
			}
			id := status.ContainerID
			if id == "" {
				id = status.State.Terminated.ContainerID
			}
			if id == "" {
				return false
			}
			identities = append(identities, container.Name+":"+id+":"+status.State.Terminated.FinishedAt.String())
		}
		return true
	}
	if !check(pod.Spec.InitContainers, pod.Status.InitContainerStatuses) || !check(pod.Spec.Containers, pod.Status.ContainerStatuses) {
		return "", false
	}
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if status.State.Terminated == nil || status.ContainerID == "" {
			return "", false
		}
		identities = append(identities, status.Name+":"+status.ContainerID+":"+status.State.Terminated.FinishedAt.String())
	}
	if len(identities) == 0 {
		return "", false
	}
	sort.Strings(identities)
	encoded, _ := json.Marshal(identities)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), true
}
