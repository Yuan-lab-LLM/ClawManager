package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SQL fence/binding checks belong to the caller. These reads must never call
// the ordinary stop helper, whose fence/cleanup callbacks can mutate state.
func (c *slotRuntimeAgentClient) openCodeForceReadOnlySourceProof(ctx context.Context, want k8s.RuntimeAssignment, frozen json.RawMessage) error {
	if c.assignments == nil || c.kube == nil || want.RuntimeType != "opencode" || want.PodUID == "" {
		return fmt.Errorf("OpenCode force source authority unavailable")
	}
	actual, state, intent, raw, err := c.assignments.ReadContainerStop(ctx, want.Endpoint, want.GatewayID)
	if err != nil {
		return err
	}
	if state != "stopped" || actual.InstanceID != want.InstanceID || actual.UserID != want.UserID || actual.Generation != want.Generation || actual.PodUID != want.PodUID || actual.RuntimeType != want.RuntimeType || intent == nil || !validContainerIntent(*intent) {
		return fmt.Errorf("OpenCode force source assignment is not the exact stopped generation")
	}
	if request := bytes.TrimSpace(want.Request); len(request) != 0 && !bytes.Equal(request, []byte("null")) {
		actualRequest, actualErr := canonicalNativeMaintenance(actual.Request)
		wantRequest, wantErr := canonicalNativeMaintenance(want.Request)
		if actualErr != nil || wantErr != nil || !bytes.Equal(actualRequest, wantRequest) {
			return fmt.Errorf("OpenCode force source frozen assignment request changed")
		}
	}
	// Plans intentionally redact request/environment; the actual generation's
	// immutable Secret and C1 slot still bind the original stopped execution.
	original, originalState, err := c.assignments.OpenCodeUpgradeSource(ctx, want.InstanceID)
	if err != nil {
		return err
	}
	if originalState != "stopped" || original.InstanceID != actual.InstanceID || original.UserID != actual.UserID || original.Generation != actual.Generation || original.PodUID != actual.PodUID || original.Endpoint != actual.Endpoint || original.GatewayID != actual.GatewayID || original.RuntimeType != actual.RuntimeType || !bytes.Equal(original.Request, actual.Request) {
		return fmt.Errorf("OpenCode force source immutable generation changed")
	}
	cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, fmt.Sprintf("lite-instance-%d", want.InstanceID), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "lite-instance" {
		return fmt.Errorf("OpenCode force source assignment ownership changed")
	}
	var saved, current runtimeSlotContainerProof
	if decodeContainerProof(frozen, &saved) != nil || decodeContainerProof(raw, &current) != nil || saved != current || current.runtimeSlotStopIntent != *intent || !current.CleanupConfirmed || current.AuxiliaryExitPending || current.ContainerID == "" || current.FinishedAt.IsZero() {
		return fmt.Errorf("OpenCode force source frozen C1 proof changed or is incomplete")
	}
	if err = c.assignments.OpenCodeUpgradeStopReleased(ctx, want.InstanceID, *intent); err != nil {
		return err
	}
	pod, err := c.containerStopPod(ctx, want.Endpoint, want.PodUID)
	if err != nil {
		return err
	}
	slot, err := containerSlotName(pod, actual.Request)
	if err != nil {
		return err
	}
	if slot != intent.Slot {
		return fmt.Errorf("OpenCode force source request no longer identifies frozen C1 slot")
	}
	observed, err := runtimeSlotTerminationProof(pod, *intent)
	if err != nil {
		return err
	}
	if observed.runtimeSlotTerminationMessage != current.runtimeSlotTerminationMessage || observed.ContainerID != current.ContainerID || !observed.FinishedAt.Equal(current.FinishedAt) {
		return fmt.Errorf("OpenCode force source kubelet C1 differs from frozen proof")
	}
	return c.requireExternalWriterAbsence(ctx, runtimeauth.Identity{InstanceID: want.InstanceID, UserID: want.UserID, Generation: want.Generation, PodUID: want.PodUID})
}

func (c *slotRuntimeAgentClient) openCodeForceNativeEvidence(ctx context.Context, rolloutID int64, items []openCodeUpgradeInstance, oldPod models.RuntimePod) (json.RawMessage, error) {
	if c.kube == nil || c.namespace == "" || oldPod.Namespace != c.namespace || oldPod.PodUID == nil || *oldPod.PodUID == "" || stringValue(oldPod.AgentEndpoint) == "" || rolloutID < 1 || len(items) == 0 {
		return nil, fmt.Errorf("OpenCode force native evidence scope incomplete")
	}
	pod, err := c.containerStopPod(ctx, *oldPod.AgentEndpoint, *oldPod.PodUID)
	if err != nil {
		return nil, err
	}
	if pod.Name != oldPod.PodName {
		return nil, fmt.Errorf("OpenCode force old maintenance Pod changed")
	}
	byID := map[int]openCodeUpgradeInstance{}
	for _, item := range items {
		if item.ID < 1 || item.Owner < 1 || item.Generation < 1 || byID[item.ID].ID != 0 {
			return nil, fmt.Errorf("OpenCode force native instance scope invalid")
		}
		byID[item.ID] = item
	}
	maps, err := c.kube.CoreV1().ConfigMaps(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-native-maintenance"})
	if err != nil {
		return nil, err
	}
	sort.Slice(maps.Items, func(i, j int) bool { return maps.Items[i].Name < maps.Items[j].Name })
	evidence := []map[string]string{}
	executed := map[int]map[string]string{}
	for _, cm := range maps.Items {
		id, parseErr := strconv.Atoi(cm.Data["instance_id"])
		item, relevant := byID[id]
		if !relevant && cm.Data["pod_uid"] != *oldPod.PodUID {
			continue
		}
		if parseErr != nil || strconv.Itoa(id) != cm.Data["instance_id"] || !relevant || cm.DeletionTimestamp != nil || cm.UID == "" || cm.Data["state"] != "released" || cm.Data["action"] != "compatibility" || cm.Data["pod_uid"] != *oldPod.PodUID || cm.Data["endpoint"] != *oldPod.AgentEndpoint || cm.Data["generation"] != strconv.Itoa(item.Generation) || cm.Data["user_id"] != strconv.Itoa(item.Owner) || !stopTokenValid(cm.Data["operation_id"]) {
			return nil, fmt.Errorf("OpenCode force refuses unknown or writing native maintenance history")
		}
		linuxUID, _ := runtimeGatewayLinuxIDs(item.ID, nil)
		req := RuntimeAgentWorkspaceRequest{ExpectedPodUID: *oldPod.PodUID, RolloutID: strconv.FormatInt(rolloutID, 10), SnapshotID: fmt.Sprintf("opencode-%d", rolloutID), UserID: item.Owner, InstanceID: item.ID, Generation: item.Generation, LeaseToken: runtimeUpgradeLeaseToken(rolloutID, item.ID), PreserveSessionSQLite: true, OfficialDBCheck: true, UID: linuxUID, GID: linuxUID}
		material, err := canonicalNativeMaintenance(map[string]any{"endpoint": *oldPod.AgentEndpoint, "pod_uid": *oldPod.PodUID, "action": "compatibility", "request": req})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(material)
		identity := hex.EncodeToString(digest[:])
		if cm.Data["request_digest"] != identity || cm.Name != "lite-maint-"+identity[:40] {
			return nil, fmt.Errorf("OpenCode force old native request digest changed")
		}
		op := runtimeNativeUpgradeOperation{OperationID: cm.Data["operation_id"], Action: "compatibility", Request: req}
		op.Request.LeaseToken = ""
		child, err := canonicalNativeMaintenance(op)
		if err != nil {
			return nil, err
		}
		childDigest := sha256.Sum256(child)
		var intent runtimeSlotStopIntent
		var proof runtimeSlotContainerProof
		if decodeContainerProof([]byte(cm.Data["intent"]), &intent) != nil || !validContainerIntent(intent) || intent.Kind != "maintenance" || intent.Graceful || intent.OperationID != op.OperationID || intent.PodUID != *oldPod.PodUID || intent.InstanceID != item.ID || intent.UserID != item.Owner || intent.Generation != item.Generation || intent.AssignmentDigest != hex.EncodeToString(childDigest[:]) || decodeContainerProof([]byte(cm.Data["termination_proof"]), &proof) != nil || proof.runtimeSlotStopIntent != intent || !proof.CleanupConfirmed || proof.AuxiliaryExitPending || proof.ContainerID == "" || proof.FinishedAt.IsZero() {
			return nil, fmt.Errorf("OpenCode force old native termination is unconfirmed")
		}
		if cm.Data["result_secret"] != cm.Name+"-result" {
			return nil, fmt.Errorf("OpenCode force old native result reference changed")
		}
		secret, err := c.kube.CoreV1().Secrets(c.namespace).Get(ctx, cm.Data["result_secret"], metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		var result runtimeNativeMaintenanceResult
		data := secret.Data["result"]
		if secret.UID == "" || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-result" || secret.Labels["clawmanager.io/operation-id"] != op.OperationID || len(data) > 1<<20 || json.Unmarshal(data, &result) != nil || result.Intent != intent || !result.Confirmed || result.Uncertain || result.Error != "OpenCode native payload must be an immutable root-owned regular file" || len(bytes.TrimSpace(result.Result)) != 0 && !bytes.Equal(bytes.TrimSpace(result.Result), []byte("null")) {
			return nil, fmt.Errorf("OpenCode force requires the immutable known payload refusal")
		}
		resultDigest := sha256.Sum256(data)
		evidence = append(evidence, map[string]string{"record_name": cm.Name, "record_uid": string(cm.UID), "request_sha256": identity, "operation_id": op.OperationID, "pod_uid": *oldPod.PodUID, "result_name": secret.Name, "result_uid": string(secret.UID), "result_sha256": hex.EncodeToString(resultDigest[:])})
		executed[item.ID] = evidence[len(evidence)-1]
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("OpenCode force requires an immutable failed native compatibility receipt")
	}
	for id := range byID {
		owner, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, fmt.Sprintf("lite-maint-owner-%d", id), metav1.GetOptions{})
		entry, hasExecution := executed[id]
		if apierrors.IsNotFound(err) && !hasExecution {
			continue
		}
		if err != nil {
			return nil, err
		}
		if owner.DeletionTimestamp != nil || owner.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-owner" || owner.Data["state"] != "released" {
			return nil, fmt.Errorf("OpenCode force native owner remains active or unknown")
		}
		if !hasExecution || owner.Data["record_name"] != entry["record_name"] || owner.Data["operation_id"] != entry["operation_id"] {
			return nil, fmt.Errorf("OpenCode force native owner has unknown history")
		}
	}
	return json.Marshal(evidence)
}
