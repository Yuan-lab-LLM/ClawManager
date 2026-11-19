package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func forceEvidenceAssertReadOnly(t *testing.T, kube *fake.Clientset, from int) {
	t.Helper()
	for _, a := range kube.Actions()[from:] {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("evidence changed cluster state: %s %s", a.GetVerb(), a.GetResource().Resource)
		}
	}
}

func forceSourceEvidenceFixture(t *testing.T) (*slotRuntimeAgentClient, *fake.Clientset, k8s.RuntimeAssignment, json.RawMessage) {
	t.Helper()
	ctx := context.Background()
	pod := slotTestPod()
	pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}, {Name: "slot-1", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20003"}}}}
	kube := fake.NewSimpleClientset(pod)
	assignments := k8s.NewRuntimeAssignmentService(kube, "runtime")
	want := k8s.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: "opencode", Endpoint: "https://10.0.0.2:19090", GatewayID: "gw-7-1", PodUID: "pod-uid", Request: json.RawMessage(`{"gateway_port":20000}`), Environment: map[string]string{"FIXTURE": "private-do-not-print"}}
	if err := assignments.EnsureDesired(ctx, want); err != nil {
		t.Fatal(err)
	}
	intent := runtimeSlotStopIntent{Version: 1, OperationID: strings.Repeat("1", 32), WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: strings.Repeat("3", 64), PodUID: want.PodUID, Slot: "slot-0", GatewayID: want.GatewayID, InstanceID: 7, UserID: 2, Generation: 1, Graceful: true}
	message := runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}
	commitment, _ := containerTerminationCommitment(message)
	finished := metav1.NewTime(time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://source-old", Message: commitment, FinishedAt: finished}}}}
	if _, err := kube.CoreV1().Pods("runtime").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	proof, _ := json.Marshal(runtimeSlotContainerProof{runtimeSlotTerminationMessage: message, ContainerID: "containerd://source-old", FinishedAt: finished.Time})
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	cm.Data["desired_state"] = "stopped"
	cm.Data["container_stop_intent"] = string(mustForceJSON(t, intent))
	cm.Data["container_termination_proof"] = string(proof)
	cm.Data["container_stop_released"] = "true"
	if _, err := kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	c := &slotRuntimeAgentClient{kube: kube, namespace: "runtime", assignments: assignments}
	c.workspaceWriteFence = func(context.Context, runtimeauth.Identity) error {
		t.Fatal("readonly evidence invoked write fence")
		return nil
	}
	c.workspaceExternalStop = func(context.Context, runtimeauth.Identity) error {
		t.Fatal("readonly evidence invoked writer cleanup")
		return nil
	}
	c.workspaceExternalAbsence = func(_ context.Context, id runtimeauth.Identity) error {
		if id.InstanceID != 7 || id.UserID != 2 || id.Generation != 1 || id.PodUID != "pod-uid" {
			t.Fatal("external absence scope changed")
		}
		return nil
	}
	// Native plans intentionally omit original request/credentials. Exercise
	// the actual pre-capsule shape rather than inventing a frozen raw request.
	want.Request = nil
	want.Environment = nil
	return c, kube, want, proof
}

func mustForceJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestOpenCodeForceEvidenceSourceIsExactAndReadOnly(t *testing.T) {
	c, kube, want, proof := forceSourceEvidenceFixture(t)
	from := len(kube.Actions())
	if err := c.openCodeForceReadOnlySourceProof(context.Background(), want, proof); err != nil {
		t.Fatal(err)
	}
	forceEvidenceAssertReadOnly(t, kube, from)
}

func TestOpenCodeForceEvidenceSourceRedactedRequestSurvivesCheckpointRoundTrip(t *testing.T) {
	c, kube, want, proof := forceSourceEvidenceFixture(t)
	var persisted k8s.RuntimeAssignment
	if err := json.Unmarshal(mustForceJSON(t, want), &persisted); err != nil {
		t.Fatal(err)
	}
	if string(persisted.Request) != "null" {
		t.Fatal("fixture must exercise persisted JSON null")
	}
	from := len(kube.Actions())
	if err := c.openCodeForceReadOnlySourceProof(context.Background(), persisted, proof); err != nil {
		t.Fatal(err)
	}
	forceEvidenceAssertReadOnly(t, kube, from)
}

func TestOpenCodeForceEvidenceSourceRefusesDrift(t *testing.T) {
	for _, name := range []string{"running", "unreleased", "changed-frozen", "changed-current", "changed-request", "wrong-generation", "wrong-pod", "missing-pod", "external-unknown"} {
		t.Run(name, func(t *testing.T) {
			c, kube, want, proof := forceSourceEvidenceFixture(t)
			ctx := context.Background()
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
			switch name {
			case "running":
				cm.Data["desired_state"] = "running"
			case "unreleased":
				cm.Data["container_stop_released"] = "false"
			case "changed-frozen":
				proof = json.RawMessage(strings.Replace(string(proof), "source-old", "another", 1))
			case "changed-current":
				cm.Data["container_termination_proof"] = strings.Replace(string(proof), "source-old", "another", 1)
			case "changed-request":
				cm.Data["request"] = `{"gateway_port":20003}`
			case "wrong-generation":
				want.Generation++
			case "wrong-pod":
				want.PodUID = "another"
			case "missing-pod":
				_ = kube.CoreV1().Pods("runtime").Delete(ctx, "pool", metav1.DeleteOptions{})
			case "external-unknown":
				c.workspaceExternalAbsence = func(context.Context, runtimeauth.Identity) error { return fmt.Errorf("unconfirmed") }
			}
			_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			from := len(kube.Actions())
			if err := c.openCodeForceReadOnlySourceProof(ctx, want, proof); err == nil {
				t.Fatal("unproved source admitted")
			}
			forceEvidenceAssertReadOnly(t, kube, from)
		})
	}
}

func forceNativeEvidenceFixture(t *testing.T) (*slotRuntimeAgentClient, *fake.Clientset, []openCodeUpgradeInstance, models.RuntimePod, *corev1.ConfigMap, *corev1.Secret) {
	t.Helper()
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := slotTestPod()
	old := models.RuntimePod{Namespace: "runtime", PodName: pod.Name, PodUID: &uid, AgentEndpoint: &endpoint}
	items := []openCodeUpgradeInstance{{ID: 7, Owner: 2, Generation: 1, State: "quiesced"}, {ID: 8, Owner: 2, Generation: 1, State: "quiesced"}}
	linuxUID, _ := runtimeGatewayLinuxIDs(7, nil)
	req := RuntimeAgentWorkspaceRequest{ExpectedPodUID: uid, RolloutID: "14", SnapshotID: "opencode-14", UserID: 2, InstanceID: 7, Generation: 1, LeaseToken: runtimeUpgradeLeaseToken(14, 7), PreserveSessionSQLite: true, OfficialDBCheck: true, UID: linuxUID, GID: linuxUID}
	material, _ := canonicalNativeMaintenance(map[string]any{"endpoint": endpoint, "pod_uid": uid, "action": "compatibility", "request": req})
	digest := sha256.Sum256(material)
	identity := hex.EncodeToString(digest[:])
	op := runtimeNativeUpgradeOperation{OperationID: strings.Repeat("1", 32), Action: "compatibility", Request: req}
	op.Request.LeaseToken = ""
	child, _ := canonicalNativeMaintenance(op)
	childDigest := sha256.Sum256(child)
	intent := runtimeSlotStopIntent{Version: 1, OperationID: op.OperationID, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: hex.EncodeToString(childDigest[:]), PodUID: uid, Slot: "slot-0", GatewayID: "gw-7-1", InstanceID: 7, UserID: 2, Generation: 1, Kind: "maintenance"}
	proof := runtimeSlotContainerProof{runtimeSlotTerminationMessage: runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}, ContainerID: "containerd://maintenance", FinishedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-maint-" + identity[:40], Namespace: "runtime", UID: types.UID("record-uid"), Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance"}}, Data: map[string]string{"request_digest": identity, "operation_id": op.OperationID, "endpoint": endpoint, "pod_uid": uid, "action": "compatibility", "instance_id": "7", "user_id": "2", "generation": "1", "state": "released", "intent": string(mustForceJSON(t, intent)), "termination_proof": string(mustForceJSON(t, proof))}}
	cm.Data["result_secret"] = cm.Name + "-result"
	yes := true
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: cm.Data["result_secret"], Namespace: "runtime", UID: types.UID("result-uid"), Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-result", "clawmanager.io/operation-id": op.OperationID}}, Immutable: &yes, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"result": mustForceJSON(t, runtimeNativeMaintenanceResult{Intent: intent, Confirmed: true, Error: "OpenCode native payload must be an immutable root-owned regular file"})}}
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-maint-owner-7", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-owner"}}, Data: map[string]string{"state": "released", "record_name": cm.Name, "operation_id": cm.Data["operation_id"]}}
	kube := fake.NewSimpleClientset(pod, cm, secret, owner)
	return &slotRuntimeAgentClient{kube: kube, namespace: "runtime"}, kube, items, old, cm, secret
}

func TestOpenCodeForceEvidenceNativeRequiresImmutableKnownFailure(t *testing.T) {
	c, kube, items, pod, _, _ := forceNativeEvidenceFixture(t)
	from := len(kube.Actions())
	proof, err := c.openCodeForceNativeEvidence(context.Background(), 14, items, pod)
	if err != nil || !json.Valid(proof) || !strings.Contains(string(proof), "record-uid") {
		t.Fatalf("missing immutable failure proof: %s %v", proof, err)
	}
	forceEvidenceAssertReadOnly(t, kube, from)
}

func TestOpenCodeForceEvidenceNativeRefusesUnknownWritesAndTampering(t *testing.T) {
	for _, name := range []string{"missing-record", "unreleased", "non-compatibility", "wrong-digest", "wrong-endpoint", "wrong-generation", "foreign-instance", "mutable-result", "uncertain-result", "wrong-error", "successful-result", "wrong-intent", "wrong-secret", "active-owner", "missing-owner", "cross-instance-owner"} {
		t.Run(name, func(t *testing.T) {
			c, kube, items, pod, cm, secret := forceNativeEvidenceFixture(t)
			ctx := context.Background()
			var result runtimeNativeMaintenanceResult
			_ = json.Unmarshal(secret.Data["result"], &result)
			switch name {
			case "missing-record":
				_ = kube.CoreV1().ConfigMaps("runtime").Delete(ctx, cm.Name, metav1.DeleteOptions{})
			case "unreleased":
				cm.Data["state"] = "running"
			case "non-compatibility":
				cm.Data["action"] = "migrate"
			case "wrong-digest":
				cm.Data["request_digest"] = strings.Repeat("f", 64)
			case "wrong-endpoint":
				cm.Data["endpoint"] = "https://10.0.0.3:19090"
			case "wrong-generation":
				cm.Data["generation"] = "2"
			case "foreign-instance":
				items = items[1:]
			case "mutable-result":
				no := false
				secret.Immutable = &no
			case "uncertain-result":
				result.Uncertain = true
			case "wrong-error":
				result.Error = "other refusal"
			case "successful-result":
				result.Error = ""
				result.Result = json.RawMessage(`{"compatible":true}`)
			case "wrong-intent":
				result.Intent.AssignmentDigest = strings.Repeat("e", 64)
			case "wrong-secret":
				secret.Labels["clawmanager.io/operation-id"] = strings.Repeat("e", 32)
			case "active-owner":
				owner, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-maint-owner-7", metav1.GetOptions{})
				owner.Data["state"] = "active"
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, owner, metav1.UpdateOptions{})
			case "missing-owner":
				_ = kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-maint-owner-7", metav1.DeleteOptions{})
			case "cross-instance-owner":
				_, _ = kube.CoreV1().ConfigMaps("runtime").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-maint-owner-8", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-owner"}}, Data: map[string]string{"state": "released", "record_name": cm.Name, "operation_id": cm.Data["operation_id"]}}, metav1.CreateOptions{})
			}
			if name != "missing-record" {
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			}
			secret.Data["result"] = mustForceJSON(t, result)
			_, _ = kube.CoreV1().Secrets("runtime").Update(ctx, secret, metav1.UpdateOptions{})
			from := len(kube.Actions())
			if _, err := c.openCodeForceNativeEvidence(ctx, 14, items, pod); err == nil {
				t.Fatal("unproved native outcome admitted")
			}
			forceEvidenceAssertReadOnly(t, kube, from)
		})
	}
}
