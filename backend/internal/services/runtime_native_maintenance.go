package services

import (
	"bytes"
	"clawreef/internal/runtimeauth"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

type runtimeNativeUpgradeOperation struct {
	OperationID string                       `json:"operation_id"`
	Action      string                       `json:"action"`
	Request     RuntimeAgentWorkspaceRequest `json:"request"`
}

type runtimeNativeMaintenanceResult struct {
	Intent    runtimeSlotStopIntent `json:"intent"`
	Result    json.RawMessage       `json:"result,omitempty"`
	Error     string                `json:"error,omitempty"`
	Confirmed bool                  `json:"confirmed"`
	Uncertain bool                  `json:"uncertain,omitempty"`
}

// Object sorting makes operation digests independent of the two repositories'
// request struct declaration order. Numeric identities retain their JSON value.
func canonicalNativeMaintenance(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var object map[string]any
	if err = d.Decode(&object); err != nil || object == nil {
		return nil, fmt.Errorf("invalid native maintenance object")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing native maintenance object")
	}
	return json.Marshal(object)
}

func (c *slotRuntimeAgentClient) nativeMaintenanceProfile(ctx context.Context, endpoint string) (*corev1.Pod, error) {
	pod, err := c.containerStopPod(ctx, endpoint, "")
	if err != nil {
		return nil, err
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return nil, nil
	}
	return pod, nil
}

func (c *slotRuntimeAgentClient) ensureNativeMaintenance(ctx context.Context, endpoint string, pod *corev1.Pod, action string, req RuntimeAgentWorkspaceRequest) (*corev1.ConfigMap, runtimeNativeUpgradeOperation, error) {
	var empty runtimeNativeUpgradeOperation
	if req.InstanceID < 1 || req.UserID < 1 || req.Generation < 1 || req.RolloutID == "" {
		return nil, empty, fmt.Errorf("native maintenance identity is incomplete")
	}
	uid, _ := runtimeGatewayLinuxIDs(req.InstanceID, nil)
	if req.UID != 0 && req.UID != uid || req.GID != 0 && req.GID != uid {
		return nil, empty, fmt.Errorf("native maintenance requires the derived instance identity")
	}
	req.UID, req.GID = uid, uid
	material, err := canonicalNativeMaintenance(map[string]any{"endpoint": endpoint, "pod_uid": string(pod.UID), "action": action, "request": req})
	if err != nil {
		return nil, empty, err
	}
	digest := sha256.Sum256(material)
	identity := hex.EncodeToString(digest[:])
	name := "lite-maint-" + identity[:40]
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return nil, empty, err
	}
	want := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance", "clawmanager.io/needs-reconcile": "true"}}, Data: map[string]string{"request_digest": identity, "operation_id": hex.EncodeToString(token[:]), "endpoint": endpoint, "pod_uid": string(pod.UID), "action": action, "instance_id": strconv.Itoa(req.InstanceID), "user_id": strconv.Itoa(req.UserID), "generation": strconv.Itoa(req.Generation), "state": "reserved"}}
	cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Create(ctx, want, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		cm, err = c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, empty, err
	}
	if cm.Labels["clawmanager.io/purpose"] != "lite-native-maintenance" || cm.Data["request_digest"] != identity || cm.Data["endpoint"] != endpoint || cm.Data["pod_uid"] != string(pod.UID) || cm.Data["action"] != action || !stopTokenValid(cm.Data["operation_id"]) {
		return nil, empty, fmt.Errorf("native maintenance request changed")
	}
	if cm.Data["state"] != "released" {
		if err = c.acquireNativeMaintenanceOwner(ctx, cm, req.InstanceID); err != nil {
			return nil, empty, err
		}
	}
	return cm, runtimeNativeUpgradeOperation{OperationID: cm.Data["operation_id"], Action: action, Request: req}, nil
}

// Kubernetes authority outlives NFS lock recovery and coordinator/node loss.
// An unknown old operation is never replaced merely because its flock expired.
func (c *slotRuntimeAgentClient) acquireNativeMaintenanceOwner(ctx context.Context, operation *corev1.ConfigMap, instanceID int) error {
	name := fmt.Sprintf("lite-maint-owner-%d", instanceID)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		owner, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, name, metav1.GetOptions{})
		creating := apierrors.IsNotFound(err)
		if err != nil && !creating {
			return err
		}
		if creating {
			owner = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-owner"}}}
		}
		if !creating {
			if owner.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-owner" {
				return fmt.Errorf("native maintenance owner is invalid")
			}
			if owner.Data["operation_id"] == operation.Data["operation_id"] {
				return nil
			}
			if owner.Data["state"] != "released" {
				previous, readErr := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, owner.Data["record_name"], metav1.GetOptions{})
				if readErr != nil || previous.Data["state"] != "released" || previous.Data["operation_id"] != owner.Data["operation_id"] || previous.Data["termination_proof"] == "" || previous.Data["result_secret"] == "" {
					return fmt.Errorf("%w: previous native maintenance termination is unconfirmed", ErrRuntimeUpgradeBusy)
				}
			}
		}
		owner.Data = map[string]string{"operation_id": operation.Data["operation_id"], "record_name": operation.Name, "state": "active"}
		if creating {
			_, err = c.kube.CoreV1().ConfigMaps(c.namespace).Create(ctx, owner, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				return apierrors.NewConflict(corev1.Resource("configmaps"), name, err)
			}
			return err
		}
		_, err = c.kube.CoreV1().ConfigMaps(c.namespace).Update(ctx, owner, metav1.UpdateOptions{})
		return err
	})
}

func (c *slotRuntimeAgentClient) releaseNativeMaintenanceOwner(ctx context.Context, operation *corev1.ConfigMap, instanceID int) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		owner, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, fmt.Sprintf("lite-maint-owner-%d", instanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if owner.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-owner" {
			return fmt.Errorf("native maintenance owner is invalid")
		}
		if owner.Data["operation_id"] != operation.Data["operation_id"] {
			return nil
		}
		owner.Data["state"] = "released"
		_, err = c.kube.CoreV1().ConfigMaps(c.namespace).Update(ctx, owner, metav1.UpdateOptions{})
		return err
	})
}

func (c *slotRuntimeAgentClient) updateNativeMaintenance(ctx context.Context, name, operationID string, values map[string]string, complete bool) (*corev1.ConfigMap, error) {
	var result *corev1.ConfigMap
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if cm.Labels["clawmanager.io/purpose"] != "lite-native-maintenance" || cm.Data["operation_id"] != operationID {
			return fmt.Errorf("native maintenance authority changed")
		}
		for key, value := range values {
			if key == "state" {
				rank := map[string]int{"reserved": 0, "running": 1, "terminated": 2, "completed": 3, "released": 4}
				previous, previousOK := rank[cm.Data[key]]
				next, nextOK := rank[value]
				if !previousOK || !nextOK {
					return fmt.Errorf("invalid native maintenance transition")
				}
				if previous > next {
					continue
				}
			}
			if previous := cm.Data[key]; (key == "intent" || key == "termination_proof" || key == "result_secret") && previous != "" && previous != value {
				return fmt.Errorf("immutable native maintenance evidence changed")
			}
			cm.Data[key] = value
		}
		cm.Labels["clawmanager.io/needs-reconcile"] = strconv.FormatBool(!complete && cm.Data["state"] != "released")
		result, err = c.kube.CoreV1().ConfigMaps(c.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
	return result, err
}

func (c *slotRuntimeAgentClient) saveNativeMaintenanceResult(ctx context.Context, cm *corev1.ConfigMap, result runtimeNativeMaintenanceResult) (string, error) {
	data, err := json.Marshal(result)
	if err != nil || len(data) > 1<<20 {
		return "", fmt.Errorf("invalid native maintenance result")
	}
	immutable := true
	name := cm.Name + "-result"
	want := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-result", "clawmanager.io/operation-id": cm.Data["operation_id"]}}, Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{"result": data}}
	_, err = c.kube.CoreV1().Secrets(c.namespace).Create(ctx, want, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		stored, readErr := c.kube.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if readErr != nil {
			return "", readErr
		}
		if stored.Immutable == nil || !*stored.Immutable || stored.Labels["clawmanager.io/operation-id"] != cm.Data["operation_id"] || !bytes.Equal(stored.Data["result"], data) {
			return "", fmt.Errorf("native maintenance result changed")
		}
		err = nil
	}
	return name, err
}

func (c *slotRuntimeAgentClient) runNativeMaintenance(ctx context.Context, endpoint, action string, req RuntimeAgentWorkspaceRequest, pod *corev1.Pod, out any) error {
	ctx, cancel := context.WithTimeout(ctx, runtimeAgentUpgradeTimeout)
	defer cancel()
	cm, operation, err := c.ensureNativeMaintenance(ctx, endpoint, pod, action, req)
	if err != nil {
		return err
	}
	identity := runtimeauth.Identity{InstanceID: operation.Request.InstanceID, UserID: operation.Request.UserID, Generation: operation.Request.Generation, PodUID: string(pod.UID)}
	mutates := action == "migrate" || action == "restore"
	if mutates {
		if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
			return err
		}
		if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
			return err
		}
	}
	child := operation
	child.Request.LeaseToken = ""
	material, err := canonicalNativeMaintenance(child)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(material)
	expectedDigest := hex.EncodeToString(digest[:])
	var intent runtimeSlotStopIntent
	if raw := cm.Data["intent"]; raw != "" {
		if decodeContainerProof([]byte(raw), &intent) != nil {
			return fmt.Errorf("invalid persisted maintenance intent")
		}
	}
	if cm.Data["state"] != "released" {
		path := "/v1/native-upgrades/start"
		if cm.Data["result_secret"] != "" {
			// Restore only the exact durable finished session after coordinator restart.
			// A missing record must fail closed, never dispatch another child.
			path = "/v1/native-upgrades/resume"
		}
		var pending struct {
			Pending bool                  `json:"pending"`
			Intent  runtimeSlotStopIntent `json:"intent"`
		}
		if err = c.doUpgrade(ctx, http.MethodPost, endpoint, path, operation, &pending); err != nil {
			return err
		}
		candidate := pending.Intent
		if !pending.Pending || !validContainerIntent(candidate) || candidate.Kind != "maintenance" || candidate.OperationID != operation.OperationID || candidate.PodUID != string(pod.UID) || candidate.InstanceID != operation.Request.InstanceID || candidate.UserID != operation.Request.UserID || candidate.Generation != operation.Request.Generation || candidate.AssignmentDigest != expectedDigest || candidate.Graceful {
			return fmt.Errorf("native operation did not return its exact maintenance binding")
		}
		if intent.OperationID != "" && intent != candidate {
			return fmt.Errorf("maintenance execution binding changed")
		}
		intent = candidate
		encoded, _ := json.Marshal(intent)
		cm, err = c.updateNativeMaintenance(ctx, cm.Name, operation.OperationID, map[string]string{"intent": string(encoded), "state": "running"}, false)
		if err != nil {
			return err
		}
	}
	if !validContainerIntent(intent) || intent.Kind != "maintenance" || intent.OperationID != operation.OperationID || intent.PodUID != string(pod.UID) || intent.InstanceID != operation.Request.InstanceID || intent.UserID != operation.Request.UserID || intent.Generation != operation.Request.Generation || intent.AssignmentDigest != expectedDigest || intent.Graceful {
		return fmt.Errorf("native maintenance intent is missing")
	}
	var proof runtimeSlotContainerProof
	if raw := cm.Data["termination_proof"]; raw != "" {
		if decodeContainerProof([]byte(raw), &proof) != nil || proof.runtimeSlotStopIntent != intent || !proof.CleanupConfirmed || proof.ContainerID == "" || proof.FinishedAt.IsZero() {
			return fmt.Errorf("invalid persisted maintenance termination proof")
		}
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	wait := func() error {
		select {
		case <-ctx.Done():
			return errors.Join(ErrRuntimeUpgradeBusy, ctx.Err())
		case <-ticker.C:
			return nil
		}
	}
	for proof.ContainerID == "" {
		current, readErr := c.kube.CoreV1().Pods(c.namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if readErr != nil {
			return readErr
		}
		if string(current.UID) != intent.PodUID {
			return fmt.Errorf("native maintenance Pod changed")
		}
		observed, proofErr := runtimeSlotTerminationProof(current, intent)
		if proofErr != nil {
			if err = wait(); err != nil {
				return err
			}
			continue
		}
		proof = *observed
		encoded, _ := json.Marshal(proof)
		cm, err = c.updateNativeMaintenance(ctx, cm.Name, operation.OperationID, map[string]string{"termination_proof": string(encoded), "state": "terminated"}, false)
		if err != nil {
			return err
		}
	}
	var result runtimeNativeMaintenanceResult
	if secret := cm.Data["result_secret"]; secret != "" {
		stored, err := c.kube.CoreV1().Secrets(c.namespace).Get(ctx, secret, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if stored.Immutable == nil || !*stored.Immutable || stored.Labels["clawmanager.io/operation-id"] != operation.OperationID || json.Unmarshal(stored.Data["result"], &result) != nil {
			return fmt.Errorf("native result Secret is invalid")
		}
	} else {
		if mutates {
			if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
				return err
			}
			if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
				return err
			}
		}
		for {
			err = c.doUpgrade(ctx, http.MethodPost, endpoint, "/v1/native-upgrades/"+operation.OperationID+"/finish", proof, &result)
			if err == nil {
				break
			}
			if errors.Is(err, ErrRuntimeAgentUnsupported) || errors.Is(err, ErrRuntimeAgentNotFound) || strings.Contains(err.Error(), "status 401") || strings.Contains(err.Error(), "status 403") {
				return err
			}
			if err = wait(); err != nil {
				return err
			}
		}
		if !result.Confirmed || result.Uncertain || result.Intent != intent || len(result.Error) > 4096 || len(result.Result) > 1<<20 || len(result.Result) > 0 && !json.Valid(result.Result) {
			return fmt.Errorf("native result is not bound to confirmed container teardown")
		}
		secret, err := c.saveNativeMaintenanceResult(ctx, cm, result)
		if err != nil {
			return err
		}
		cm, err = c.updateNativeMaintenance(ctx, cm.Name, operation.OperationID, map[string]string{"result_secret": secret, "state": "completed"}, false)
		if err != nil {
			return err
		}
	}
	if !result.Confirmed || result.Uncertain || result.Intent != intent {
		return fmt.Errorf("persisted native result identity changed")
	}
	if cm.Data["state"] != "released" {
		var release struct {
			OperationID string `json:"operation_id"`
			Released    bool   `json:"released"`
		}
		if err = c.do(ctx, http.MethodPost, endpoint, "/v1/native-upgrades/"+operation.OperationID+"/release", proof, &release); err != nil {
			return err
		}
		if !release.Released || release.OperationID != operation.OperationID {
			return fmt.Errorf("maintenance slot release is unconfirmed")
		}
		if _, err = c.updateNativeMaintenance(ctx, cm.Name, operation.OperationID, map[string]string{"state": "released"}, true); err != nil {
			return err
		}
	}
	if err = c.releaseNativeMaintenanceOwner(ctx, cm, operation.Request.InstanceID); err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	if len(result.Result) == 0 {
		return fmt.Errorf("native maintenance returned no result")
	}
	return json.Unmarshal(result.Result, out)
}

func (c *slotRuntimeAgentClient) PreflightWorkspace(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentWorkspacePreflight, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.PreflightWorkspace(ctx, endpoint, req)
	}
	var result RuntimeAgentWorkspacePreflight
	err = c.runNativeMaintenance(ctx, endpoint, "inventory", req, pod, &result)
	return &result, err
}
func (c *slotRuntimeAgentClient) PreflightUpgradeCompatibility(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.PreflightUpgradeCompatibility(ctx, endpoint, req)
	}
	var result RuntimeAgentUpgradeCompatibility
	err = c.runNativeMaintenance(ctx, endpoint, "compatibility", req, pod, &result)
	return &result, err
}
func (c *slotRuntimeAgentClient) MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.MigrateSessionSQLite(ctx, endpoint, req)
	}
	var result RuntimeAgentSessionSQLiteMigration
	err = c.runNativeMaintenance(ctx, endpoint, "migrate", req, pod, &result)
	return &result, err
}
func (c *slotRuntimeAgentClient) SessionSQLiteMigrationStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.SessionSQLiteMigrationStatus(ctx, endpoint, req)
	}
	var result RuntimeAgentSessionSQLiteMigration
	err = c.runNativeMaintenance(ctx, endpoint, "migration-status", req, pod, &result)
	return &result, err
}
func (c *slotRuntimeAgentClient) RestoreSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.RestoreSessionSQLite(ctx, endpoint, req)
	}
	var result RuntimeAgentSessionSQLiteRestore
	err = c.runNativeMaintenance(ctx, endpoint, "restore", req, pod, &result)
	return &result, err
}
func (c *slotRuntimeAgentClient) SessionSQLiteRestoreStatus(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	pod, err := c.nativeMaintenanceProfile(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return c.runtimeAgentHTTPClient.SessionSQLiteRestoreStatus(ctx, endpoint, req)
	}
	var result RuntimeAgentSessionSQLiteRestore
	err = c.runNativeMaintenance(ctx, endpoint, "restore-status", req, pod, &result)
	return &result, err
}
