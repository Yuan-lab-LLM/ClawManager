package services

import (
	"clawreef/internal/repository"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

func (s *BrowserWorkerService) loadNFSBrowserRegistry(ctx context.Context, id int, initialize bool) (nfsBrowserStopEvidence, int, error) {
	var evidence nfsBrowserStopEvidence
	registry, ok := s.repo.(repository.BrowserWriterRegistry)
	if !ok {
		return evidence, 0, fmt.Errorf("durable browser writer registry is unavailable")
	}
	raw, revision, err := registry.LoadBrowserWriter(ctx, id)
	if err != nil {
		return evidence, revision, err
	}
	if revision == 0 {
		if !initialize {
			return evidence, 0, fmt.Errorf("browser writer admission history is missing")
		}
		config, err := s.repo.Get(id)
		if err != nil {
			return evidence, 0, err
		}
		if config != nil {
			return s.recordNFSBrowserLegacyExposure(ctx, id)
		}
		// No desired row does not erase an already-created controller identity.
		_, ownerErr := s.client.Clientset.AppsV1().Deployments(s.namespace()).Get(ctx, s.name(id), metav1.GetOptions{})
		if !apierrors.IsNotFound(ownerErr) {
			if ownerErr != nil {
				return evidence, 0, ownerErr
			}
			return evidence, 0, fmt.Errorf("browser controller exists without durable admission history")
		}
		sets, ownerErr := s.client.Clientset.AppsV1().StatefulSets(s.namespace()).List(ctx, metav1.ListOptions{})
		if ownerErr != nil {
			return evidence, 0, ownerErr
		}
		for _, set := range sets.Items {
			if set.Name == s.name(id) || set.Labels["instance-id"] == strconv.Itoa(id) {
				return evidence, 0, fmt.Errorf("browser controller exists without durable admission history")
			}
		}

		pods, err := s.client.Clientset.CoreV1().Pods(s.namespace()).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=" + strconv.Itoa(id)})
		if err != nil {
			return evidence, 0, err
		}
		if len(pods.Items) > 0 {
			return evidence, 0, fmt.Errorf("browser Pods exist without durable admission history")
		}
		evidence.Pods = map[string]nfsBrowserPodEvidence{}
		err = s.persistNFSBrowserRegistry(ctx, id, &revision, evidence)
		return evidence, revision, err
	}
	if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &evidence) != nil || evidence.Pods == nil {
		return evidence, revision, fmt.Errorf("browser writer registry is invalid")
	}
	if evidence.ControllerKind != "" && evidence.ControllerKind != "StatefulSet" {
		return evidence, revision, fmt.Errorf("browser controller kind is invalid")
	}
	if evidence.Stopped && !evidence.Admitted {
		return evidence, revision, fmt.Errorf("browser stop receipt has no admission")
	}
	if len(evidence.Pods) > 64 || (!evidence.Admitted && len(evidence.Pods) > 0) || (evidence.Stopped && len(evidence.Pods) == 0) {
		return evidence, revision, fmt.Errorf("browser writer inventory is invalid")
	}
	for uid, record := range evidence.Pods {
		if uid == "" || record.Name == "" || (evidence.Stopped && !record.Complete) {
			return evidence, revision, fmt.Errorf("browser writer receipt is incomplete")
		}
		if record.Complete {
			sum, err := hex.DecodeString(record.ContainersSHA256)
			if err != nil || len(sum) != sha256.Size {
				return evidence, revision, fmt.Errorf("browser writer container receipt is invalid")
			}
		}
	}

	seen := map[string]bool{}
	for _, old := range evidence.Retired {
		if old.ControllerName == "" || old.ControllerUID == "" || len(old.Pods) != 1 || seen[old.ControllerName] || old.ControllerName == s.nfsBrowserControllerName(id, evidence) {
			return evidence, revision, fmt.Errorf("retired browser registry identity is invalid")
		}
		seen[old.ControllerName] = true
		for uid, pod := range old.Pods {
			hash, err := hex.DecodeString(pod.ContainersSHA256)
			if uid == "" || pod.Name != old.ControllerName+"-0" || !pod.Complete || err != nil || len(hash) != sha256.Size {
				return evidence, revision, fmt.Errorf("retired browser registry proof is incomplete")
			}
		}
	}
	return evidence, revision, nil
}
func (s *BrowserWorkerService) persistNFSBrowserRegistry(ctx context.Context, id int, revision *int, evidence nfsBrowserStopEvidence) error {
	registry, ok := s.repo.(repository.BrowserWriterRegistry)
	if !ok {
		return fmt.Errorf("durable browser writer registry is unavailable")
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	if err = registry.StoreBrowserWriter(ctx, id, *revision, string(data)); err != nil {
		return err
	}
	*revision++
	return nil
}

// Observe from initial creation and each reconcile. The template finalizer keeps
// controller-deleted Pods visible until their exact termination is recorded.
func (s *BrowserWorkerService) observeNFSBrowserPods(ctx context.Context, id int) error {
	evidence, revision, err := s.loadNFSBrowserRegistry(ctx, id, false)
	if err != nil {
		return err
	}
	if !evidence.Admitted || evidence.Stopped {
		return nil
	}
	list, err := s.client.Clientset.CoreV1().Pods(s.namespace()).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=" + strconv.Itoa(id) + ",managed-by=" + browserWorkerManagedBy})
	if err != nil {
		return err
	}
	if err := s.verifyNFSBrowserTombstones(ctx, id, evidence); err != nil {
		return err
	}
	changed := false
	for _, pod := range list.Items {
		if evidence.Epoch != "" && pod.Annotations[nfsBrowserEpochAnnotation] != evidence.Epoch {
			if !knownNFSBrowserRetiredPod(evidence, &pod) {
				return fmt.Errorf("browser has an untracked execution epoch")
			}
			continue
		}
		if err := validateNFSBrowserOrdinal(evidence, &pod, s.nfsBrowserControllerName(id, evidence)); err != nil {
			return err
		}
		uid := string(pod.UID)
		if uid == "" {
			return fmt.Errorf("browser Pod identity unavailable")
		}
		if record, ok := evidence.Pods[uid]; ok {
			if record.Name != pod.Name {
				return fmt.Errorf("browser Pod identity changed")
			}
		} else {
			evidence.Pods[uid] = nfsBrowserPodEvidence{Name: pod.Name}
			changed = true
		}
	}
	if len(evidence.Pods) > 64 {
		return fmt.Errorf("browser writer inventory exceeds bound")
	}
	if changed {
		return s.persistNFSBrowserRegistry(ctx, id, &revision, evidence)
	}
	return nil
}
