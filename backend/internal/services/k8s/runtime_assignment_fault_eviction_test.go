package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
	"time"
)

func TestFaultEvictionAssignmentFenceIsExactAndExclusive(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "old-pod"
	a.Request = json.RawMessage(`{"gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	cm := assignmentCM(t, client)
	cm.UID = types.UID("assignment-uid")
	cm.ResourceVersion = "1"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	sec, _ := client.CoreV1().Secrets("tenant").Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	sec.UID = "secret-uid"
	sec.ResourceVersion = "1"
	client.CoreV1().Secrets("tenant").Update(ctx, sec, metav1.UpdateOptions{})
	snap, err := s.FaultEvictionSource(ctx, a.Endpoint, a.GatewayID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FenceFaultEviction(ctx, snap, "op-a"); err != nil {
		t.Fatal(err)
	}
	if err = s.FenceFaultEviction(ctx, snap, "op-a"); err != nil {
		t.Fatal(err)
	}
	if err = s.FenceFaultEviction(ctx, snap, "op-b"); err == nil {
		t.Fatal("second operation stole source")
	}
	cm = assignmentCM(t, client)
	if cm.Data["desired_state"] != "draining" {
		t.Fatal("source not fenced")
	}
	cm.Data["pod_uid"] = "replacement"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	if err = s.FenceFaultEviction(ctx, snap, "op-a"); err == nil {
		t.Fatal("replacement source accepted")
	}
}

func TestFaultEvictionReceiptRetiresOnlyOldAssignmentAndPreservesEvidence(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "old-pod"
	a.Request = json.RawMessage(`{"gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	cm := assignmentCM(t, client)
	cm.UID = "assignment-uid"
	cm.ResourceVersion = "1"
	cm.Data["container_stop_intent"] = "old-native-intent"
	cm.Data["container_termination_proof"] = "old-native-proof"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	sec, _ := client.CoreV1().Secrets("tenant").Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	sec.UID = "secret-uid"
	sec.ResourceVersion = "1"
	client.CoreV1().Secrets("tenant").Update(ctx, sec, metav1.UpdateOptions{})
	snap, err := s.FaultEvictionSource(ctx, a.Endpoint, a.GatewayID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FenceFaultEviction(ctx, snap, "op"); err != nil {
		t.Fatal(err)
	}
	scope := runtimeeviction.Scope{Version: 1, OperationID: "op", Namespace: "tenant", PodName: "old", PodUID: "old-pod", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", RuntimeType: "openclaw", GatewayID: "gw-7-3", WorkspaceRoot: "/workspaces", InstanceID: 7, UserID: 4, Generation: 3}
	receipt := &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: "op", PodUID: "old-pod", ContainerName: "slot-0", NodeUID: "node-uid", NodeBootID: "boot", LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC()}
	bad := *receipt
	bad.PodUID = "other"
	if err = s.ConfirmFaultEvictionStopped(ctx, snap, scope, &bad); err == nil {
		t.Fatal("wrong source proof accepted")
	}
	if err = s.ConfirmFaultEvictionStopped(ctx, snap, scope, receipt); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmFaultEvictionStopped(ctx, snap, scope, receipt); err != nil {
		t.Fatal(err)
	}
	cm = assignmentCM(t, client)
	if cm.Data["desired_state"] != "stopped" || cm.Data["container_termination_proof"] != "" || !strings.Contains(cm.Annotations["clawmanager.io/fault-eviction-receipt-3"], "old-native-proof") {
		t.Fatal("source evidence was discarded or retirement incomplete")
	}
	next := a
	next.Generation = 4
	next.GatewayID = "gw-7-4"
	next.PodUID = "new-pod"
	if err = s.EnsureDesired(ctx, next); err != nil {
		t.Fatal(err)
	}
	cm = assignmentCM(t, client)
	if cm.Annotations[faultEvictionAnnotation] != "" || cm.Annotations["clawmanager.io/fault-eviction-receipt-3"] == "" {
		t.Fatal("new generation retained active old fence or lost audit")
	}
	if err = s.ConfirmFaultEvictionStopped(ctx, snap, scope, receipt); err == nil {
		t.Fatal("old proof retired a new generation")
	}
}
