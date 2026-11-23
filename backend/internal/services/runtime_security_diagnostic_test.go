package services

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/netip"
	"strings"
	"testing"

	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
)

func TestRuntimeLeasePreservesOnlySanitizedInventoryClassification(t *testing.T) {
	a, _, _, cm, pod := securityFixture(t)
	cm.UID, cm.ResourceVersion = "assignment-7", "123"
	calls := 0
	rawErr := errors.New("raw-sentinel-secret")
	a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		calls++
		if calls == 2 {
			return nil, nil, rawErr
		}
		return cm, []corev1.Pod{*pod}, nil
	})
	lease, err := a.IssueLease(context.Background(), runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}, netip.MustParseAddr("10.2.0.3"))
	d := RuntimeSecurityFailureDiagnostic(err)
	if err == nil || err.Error() != errRuntimeSecurity.Error() || errors.Is(err, rawErr) || runtimeauth.IsUnavailable(err) ||
		calls != 2 || lease != (RuntimeSecurityLease{}) || d.Stage != "issue" || d.Reason != "authorization_denied" ||
		d.AssignmentUID != "assignment-7" || d.AssignmentResourceVersion != "123" {
		t.Fatalf("lost safe classification: calls=%d error=%v diagnostic=%+v", calls, err, d)
	}
}

func TestPublicationSnapshotDenialLogsSafeStage(t *testing.T) {
	a, _, _, cm, pod := securityFixture(t)
	cm.UID, cm.ResourceVersion = "assignment-7", "123"
	pod.UID = "replaced-pod"
	a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		return cm, []corev1.Pod{*pod}, nil
	})
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	client := &slotRuntimeAgentClient{security: a, namespace: "test"}
	err := client.confirmPublishedSecurity(context.Background(), cm, RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7", AgentType: "opencode", SecurityProfile: RuntimeSecurityProfile})
	if err == nil || !strings.Contains(output.String(), "reason=pod_identity_conflict stage=pod") || !strings.Contains(output.String(), `assignment_uid="assignment-7" assignment_rv="123"`) {
		t.Fatalf("snapshot denial not logged safely: err=%v log=%s", err, output.String())
	}
}
