package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"path"
	"reflect"
	"sort"
	"strconv"
)

type RuntimeOrphanPublication struct {
	Audit         RuntimeStoppedSourceAudit `json:"audit"`
	SecretUID     string                    `json:"secret_uid"`
	SecretVersion string                    `json:"secret_version"`
	CMUID         string                    `json:"cm_uid,omitempty"`
	CMGeneration  int                       `json:"cm_generation,omitempty"`
	CurrentSource *RuntimeFaultAssignment   `json:"current_source,omitempty"`
}

func (s *RuntimeAssignmentService) SnapshotRuntimeOrphanPublications(ctx context.Context, source *RuntimeOrphanSource, owners []runtimeeviction.PodLedgerOwner, endpoint string) ([]RuntimeOrphanPublication, error) {
	records := map[string]RuntimeStoppedSourceAudit{}
	for _, owner := range owners {
		if owner.PodUID != source.PodUID || owner.RuntimeType != source.RuntimeType {
			return nil, fmt.Errorf("protected orphan ledger source differs")
		}
		a := RuntimeStoppedSourceAudit{InstanceID: owner.InstanceID, UserID: owner.UserID, Generation: owner.Generation, RuntimeType: owner.RuntimeType, PodUID: owner.PodUID, Endpoint: endpoint, GatewayID: fmt.Sprintf("gw-%d-%d", owner.InstanceID, owner.Generation), WorkspacePath: path.Join(source.WorkspaceRoot, owner.RuntimeType, fmt.Sprintf("user-%d", owner.UserID), fmt.Sprintf("instance-%d", owner.InstanceID)), Stopped: true}
		records[a.GatewayID] = a
	}
	maps, e := s.client.CoreV1().ConfigMaps(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if e != nil {
		return nil, e
	}
	for _, cm := range maps.Items {
		if cm.Data["pod_uid"] != source.PodUID {
			continue
		}
		a, _, e := storedRuntimeAssignment(&cm)
		if e != nil {
			return nil, e
		}
		if a.RuntimeType != source.RuntimeType {
			return nil, fmt.Errorf("orphan assignment runtime type differs")
		}
		record := RuntimeStoppedSourceAudit{InstanceID: a.InstanceID, UserID: a.UserID, Generation: a.Generation, RuntimeType: a.RuntimeType, PodUID: a.PodUID, Endpoint: a.Endpoint, GatewayID: a.GatewayID, WorkspacePath: assignmentWorkspace(a), Stopped: true}
		if previous, ok := records[record.GatewayID]; ok && (previous.UserID != record.UserID || previous.WorkspacePath != record.WorkspacePath) {
			return nil, fmt.Errorf("orphan ledger and immutable assignment disagree")
		}
		records[record.GatewayID] = record
	}
	result := []RuntimeOrphanPublication{}
	for _, a := range records {
		if e = ValidateRuntimeStoppedSourceAudit(a, a.InstanceID, a.UserID, a.Generation, a.RuntimeType); e != nil {
			return nil, e
		}
		if a.WorkspacePath != path.Join(source.WorkspaceRoot, a.RuntimeType, fmt.Sprintf("user-%d", a.UserID), fmt.Sprintf("instance-%d", a.InstanceID)) {
			return nil, fmt.Errorf("orphan workspace is not canonical")
		}
		secret, e := s.client.CoreV1().Secrets(s.namespace).Get(ctx, runtimeAssignmentSecretName(a.InstanceID, a.Generation), metav1.GetOptions{})
		if e != nil {
			return nil, fmt.Errorf("immutable orphan generation credential required: %w", e)
		}
		if secret.UID == "" || secret.ResourceVersion == "" || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque {
			return nil, fmt.Errorf("orphan credential identity is not immutable")
		}
		for k, v := range map[string]string{"clawmanager.io/instance-id": strconv.Itoa(a.InstanceID), "clawmanager.io/user-id": strconv.Itoa(a.UserID), "clawmanager.io/generation": strconv.Itoa(a.Generation), "clawmanager.io/runtime-type": a.RuntimeType} {
			if secret.Labels[k] != v {
				return nil, fmt.Errorf("orphan credential owner changed")
			}
		}
		publication := RuntimeOrphanPublication{Audit: a, SecretUID: string(secret.UID), SecretVersion: secret.ResourceVersion}
		cm, e := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(a.InstanceID), metav1.GetOptions{})
		if !apierrors.IsNotFound(e) {
			if e != nil {
				return nil, e
			}
			current, _, e := storedRuntimeAssignment(cm)
			if e != nil {
				return nil, e
			}
			if current.UserID != a.UserID || current.RuntimeType != a.RuntimeType || current.Generation < a.Generation || cm.UID == "" {
				return nil, fmt.Errorf("latest assignment owner or generation differs")
			}
			publication.CMUID = string(cm.UID)
			publication.CMGeneration = current.Generation
			if current.Generation == a.Generation {
				if current.PodUID != a.PodUID || current.Endpoint != a.Endpoint || assignmentWorkspace(current) != a.WorkspacePath {
					return nil, fmt.Errorf("same-generation source differs")
				}
				snapshot, e := s.FaultEvictionSource(ctx, current.Endpoint, current.GatewayID)
				if e != nil {
					return nil, e
				}
				publication.CurrentSource = snapshot
			}
		}
		result = append(result, publication)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Audit.InstanceID == result[j].Audit.InstanceID {
			return result[i].Audit.Generation < result[j].Audit.Generation
		}
		return result[i].Audit.InstanceID < result[j].Audit.InstanceID
	})
	return result, nil
}
func (s *RuntimeAssignmentService) FenceRuntimeOrphanPublications(ctx context.Context, records []RuntimeOrphanPublication, operation string) error {
	for _, r := range records {
		if r.CurrentSource == nil {
			continue
		}
		if e := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, e := s.FaultEvictionSource(ctx, r.Audit.Endpoint, r.Audit.GatewayID)
			if e != nil {
				return e
			}
			if !sameFaultAssignment(current, r.CurrentSource) {
				return fmt.Errorf("orphan assignment source changed")
			}
			cm, e := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(r.Audit.InstanceID), metav1.GetOptions{})
			if e != nil {
				return e
			}
			a, _, e := storedRuntimeAssignment(cm)
			if e != nil {
				return e
			}
			if string(cm.UID) != r.CMUID || !reflect.DeepEqual(a, r.CurrentSource.RuntimeAssignment) {
				return fmt.Errorf("orphan assignment changed while fencing")
			}
			if old := cm.Annotations[orphanOperation]; old != "" && old != operation {
				return fmt.Errorf("another orphan operation owns this publication")
			}
			if cm.Annotations == nil {
				cm.Annotations = map[string]string{}
			}
			cm.Annotations[orphanOperation] = operation
			if cm.Data["desired_state"] != "stopped" {
				cm.Data["desired_state"] = "draining"
			}
			if cm.Labels == nil {
				cm.Labels = map[string]string{}
			}
			cm.Labels["clawmanager.io/needs-reconcile"] = "false"
			_, e = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
			return e
		}); e != nil {
			return e
		}
	}
	return nil
}

func (s *RuntimeAssignmentService) ConfirmRuntimeOrphanPublications(ctx context.Context, records []RuntimeOrphanPublication, scope runtimeeviction.PodScope, receipt *runtimeeviction.PodReceipt) error {
	if e := runtimeeviction.ValidatePodReceipt(scope, receipt); e != nil {
		return e
	}
	for _, r := range records {
		if r.Audit.PodUID != scope.PodUID || r.Audit.RuntimeType != scope.RuntimeType {
			return fmt.Errorf("Pod receipt does not cover orphan publication")
		}
		raw, _ := json.Marshal(r.Audit)
		proof, _ := json.Marshal(struct {
			Scope   runtimeeviction.PodScope    `json:"scope"`
			Receipt *runtimeeviction.PodReceipt `json:"receipt"`
		}{scope, receipt})
		auditKey := RuntimeStoppedSourceAnnotationPrefix + strconv.Itoa(r.Audit.Generation)
		receiptKey := "clawmanager.io/orphan-stop-receipt-" + strconv.Itoa(r.Audit.Generation)
		if e := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			secret, e := s.client.CoreV1().Secrets(s.namespace).Get(ctx, runtimeAssignmentSecretName(r.Audit.InstanceID, r.Audit.Generation), metav1.GetOptions{})
			if e != nil {
				return e
			}
			if string(secret.UID) != r.SecretUID || secret.Immutable == nil || !*secret.Immutable {
				return fmt.Errorf("orphan credential was replaced")
			}
			if secret.ResourceVersion != r.SecretVersion && secret.Annotations[receiptKey] != string(proof) {
				return fmt.Errorf("orphan credential changed since admission")
			}
			if secret.Annotations == nil {
				secret.Annotations = map[string]string{}
			}
			if old := secret.Annotations[auditKey]; old != "" && old != string(raw) {
				return fmt.Errorf("orphan historical proof conflicts")
			}
			secret.Annotations[auditKey] = string(raw)
			secret.Annotations[receiptKey] = string(proof)
			_, e = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			return e
		}); e != nil {
			return e
		}
		if r.CMUID == "" {
			continue
		}
		if e := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cm, e := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(r.Audit.InstanceID), metav1.GetOptions{})
			if e != nil {
				return e
			}
			current, _, e := storedRuntimeAssignment(cm)
			if e != nil {
				return e
			}
			if string(cm.UID) != r.CMUID || current.Generation != r.CMGeneration || current.UserID != r.Audit.UserID || current.RuntimeType != r.Audit.RuntimeType {
				return fmt.Errorf("latest orphan assignment owner changed")
			}
			if r.CurrentSource != nil {
				if !reflect.DeepEqual(current, r.CurrentSource.RuntimeAssignment) || cm.Annotations[orphanOperation] != scope.OperationID {
					return fmt.Errorf("orphan assignment fence changed")
				}
				cm.Data["desired_state"] = "stopped"
				if cm.Labels == nil {
					cm.Labels = map[string]string{}
				}
				cm.Labels["clawmanager.io/needs-reconcile"] = "false"
				native := map[string]string{}
				for _, key := range []string{"container_stop_intent", "container_termination_proof", "container_stop_released"} {
					if value := cm.Data[key]; value != "" {
						native[key] = value
						delete(cm.Data, key)
					}
				}
				if len(native) > 0 {
					archive, _ := json.Marshal(native)
					if cm.Annotations == nil {
						cm.Annotations = map[string]string{}
					}
					cm.Annotations["clawmanager.io/orphan-native-evidence"] = string(archive)
				}
			}
			if cm.Annotations == nil {
				cm.Annotations = map[string]string{}
			}
			if old := cm.Annotations[auditKey]; old != "" && old != string(raw) {
				return fmt.Errorf("orphan stopped source audit conflicts")
			}
			cm.Annotations[auditKey] = string(raw)
			cm.Annotations[receiptKey] = string(proof)
			_, e = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
			return e
		}); e != nil {
			return e
		}
	}
	return nil
}
