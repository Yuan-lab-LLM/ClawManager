package services

import (
	"bytes"
	"clawreef/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

type openCodeForceInactiveClient interface {
	openCodeForceInactiveTarget(context.Context, models.RuntimePod, json.RawMessage) error
}
type openCodeForceNativeReceipt struct {
	RecordName    string `json:"record_name"`
	RecordUID     string `json:"record_uid"`
	RequestSHA256 string `json:"request_sha256"`
	OperationID   string `json:"operation_id"`
	PodUID        string `json:"pod_uid"`
	ResultName    string `json:"result_name"`
	ResultUID     string `json:"result_uid"`
	ResultSHA256  string `json:"result_sha256"`
}

// Only force retirement can recognize a proved, released maintenance history.
// It grants no allocation, normal idle or writer permission and changes no book.
func (c *slotRuntimeAgentClient) openCodeForceInactiveTarget(ctx context.Context, pod models.RuntimePod, verified json.RawMessage) error {
	var receipts []openCodeForceNativeReceipt
	if json.Unmarshal(verified, &receipts) != nil || len(receipts) == 0 || pod.RuntimeType != RuntimeTypeOpenCode {
		return forceRetryConflict("OpenCode inactive target receipt scope missing")
	}
	slots, err := c.observeRuntimeSlots(ctx, pod)
	if err != nil {
		return err
	}
	if len(slots) == 0 || len(slots) != pod.Capacity {
		return forceRetryConflict("OpenCode inactive target physical inventory incomplete")
	}
	physical, err := c.containerStopPod(ctx, stringValue(pod.AgentEndpoint), stringValue(pod.PodUID))
	if err != nil {
		return err
	}
	for _, slot := range slots {
		if slot.Snapshot.NFSWriterOwned {
			return forceRetryConflict("OpenCode inactive target still owns a native writer")
		}
		if slot.free() {
			continue
		}
		s := slot.Snapshot
		a := s.Assignment
		if !slot.ReadOK || s.PodUID != stringValue(pod.PodUID) || !stopTokenValid(s.WorkerEpoch) || s.IdleConfirmed == nil || *s.IdleConfirmed || s.Quarantined == nil || *s.Quarantined || s.Gateways == nil || len(s.Gateways) != 0 || a == nil || a.State != "stopped" || a.PodUID != s.PodUID || a.InstanceID < 1 || a.UserID < 1 || a.Generation < 1 || a.GatewayID != fmt.Sprintf("gw-%d-%d", a.InstanceID, a.Generation) || !stopTokenValid(a.WorkerEpoch) || a.WorkerEpoch == s.WorkerEpoch || !validOpenCodeSHA(s.Digest) {
			return forceRetryConflict("OpenCode inactive target has unknown slot ownership")
		}
		var matched *openCodeForceNativeReceipt
		for i := range receipts {
			if receipts[i].OperationID == s.MaintenanceOperationID && receipts[i].PodUID == s.PodUID {
				if matched != nil {
					return forceRetryConflict("OpenCode inactive target has ambiguous receipt")
				}
				matched = &receipts[i]
			}
		}
		if matched == nil {
			return forceRetryConflict("OpenCode inactive target maintenance is not the verified failure")
		}
		cm, e := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, matched.RecordName, metav1.GetOptions{})
		if e != nil {
			return e
		}
		if string(cm.UID) != matched.RecordUID || cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "lite-native-maintenance" || cm.Data["state"] != "released" || cm.Data["action"] != "compatibility" || cm.Data["operation_id"] != matched.OperationID || cm.Data["pod_uid"] != s.PodUID || cm.Data["endpoint"] != stringValue(pod.AgentEndpoint) || cm.Data["request_digest"] != matched.RequestSHA256 || cm.Data["result_secret"] != matched.ResultName || cm.Data["instance_id"] != strconv.Itoa(a.InstanceID) || cm.Data["user_id"] != strconv.Itoa(a.UserID) || cm.Data["generation"] != strconv.Itoa(a.Generation) {
			return forceRetryConflict("OpenCode inactive target maintenance authority changed")
		}
		var intent runtimeSlotStopIntent
		var saved runtimeSlotContainerProof
		if decodeContainerProof([]byte(cm.Data["intent"]), &intent) != nil || !validContainerIntent(intent) || intent.Kind != "maintenance" || intent.OperationID != s.MaintenanceOperationID || intent.PodUID != s.PodUID || intent.Slot != slot.Name || intent.GatewayID != a.GatewayID || intent.InstanceID != a.InstanceID || intent.UserID != a.UserID || intent.Generation != a.Generation || intent.WorkerEpoch != a.WorkerEpoch || intent.AssignmentDigest != s.Digest || decodeContainerProof([]byte(cm.Data["termination_proof"]), &saved) != nil || saved.runtimeSlotStopIntent != intent || !saved.CleanupConfirmed || saved.AuxiliaryExitPending || saved.ContainerID == "" || saved.FinishedAt.IsZero() {
			return forceRetryConflict("OpenCode inactive target C1 identity changed")
		}
		observed, e := runtimeSlotTerminationProof(physical, intent)
		if e != nil {
			return e
		}
		if observed.runtimeSlotTerminationMessage != saved.runtimeSlotTerminationMessage || observed.ContainerID != saved.ContainerID || !observed.FinishedAt.Equal(saved.FinishedAt) {
			return forceRetryConflict("OpenCode inactive target physical C1 differs")
		}
		running := false
		for _, status := range physical.Status.ContainerStatuses {
			if status.Name == slot.Name && status.Ready && status.State.Running != nil && status.ContainerID != "" && status.ContainerID != saved.ContainerID {
				running = true
			}
		}
		if !running {
			return forceRetryConflict("OpenCode inactive target has no distinct running worker")
		}
		secret, e := c.kube.CoreV1().Secrets(c.namespace).Get(ctx, matched.ResultName, metav1.GetOptions{})
		if e != nil {
			return e
		}
		resultBytes := secret.Data["result"]
		digest := sha256.Sum256(resultBytes)
		var result runtimeNativeMaintenanceResult
		if len(resultBytes) > 1<<20 || json.Unmarshal(resultBytes, &result) != nil || result.Intent != intent || !result.Confirmed || result.Uncertain || result.Error != "OpenCode native payload must be an immutable root-owned regular file" || len(bytes.TrimSpace(result.Result)) != 0 && !bytes.Equal(bytes.TrimSpace(result.Result), []byte("null")) || string(secret.UID) != matched.ResultUID || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || hex.EncodeToString(digest[:]) != matched.ResultSHA256 || secret.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-result" || secret.Labels["clawmanager.io/operation-id"] != intent.OperationID {
			return forceRetryConflict("OpenCode inactive target immutable result changed")
		}
		owner, e := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, "lite-maint-owner-"+strconv.Itoa(a.InstanceID), metav1.GetOptions{})
		if e != nil {
			return e
		}
		if owner.DeletionTimestamp != nil || owner.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-owner" || owner.Data["state"] != "released" || owner.Data["record_name"] != cm.Name || owner.Data["operation_id"] != intent.OperationID {
			return forceRetryConflict("OpenCode inactive target maintenance owner changed")
		}
	}
	after, err := c.containerStopPod(ctx, stringValue(pod.AgentEndpoint), stringValue(pod.PodUID))
	if err != nil {
		return err
	}
	if after.UID != physical.UID {
		return forceRetryConflict("OpenCode inactive target changed during inspection")
	}
	return nil
}

// A fixed public stage identifies the failed gate without exposing native output.
type OpenCodeForcePreflightError struct {
	stage string
	cause error
}

func (e *OpenCodeForcePreflightError) Error() string {
	return "OpenCode force preflight refused at " + e.stage
}
func (e *OpenCodeForcePreflightError) Unwrap() error { return e.cause }
func OpenCodeForcePreflightStage(err error) string {
	var failure *OpenCodeForcePreflightError
	if errors.As(err, &failure) {
		switch failure.stage {
		case "source_controller", "old_target_slots", "source_stop", "native_receipt", "lease_absence", "image_contract":
			return failure.stage
		}
	}
	return "operation"
}
