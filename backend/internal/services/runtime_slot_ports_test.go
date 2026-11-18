package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestSlotAllocationRefreshesPersistedQuarantineFromFreshProof(t *testing.T) {
	for _, scenario := range []string{"idle", "stopped-owner-idle", "pod-error", "occupied", "unknown", "partial-inventory", "wrong-uid", "replacement", "conflict", "no-record", "assignment-reserved"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			physical := auditTestPod()
			capacity := 1
			if scenario == "partial-inventory" {
				physical = auditRoutingPod(2, 3)
				capacity = 2
			}
			kube := fake.NewSimpleClientset(physical)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: capacity, AgentEndpoint: &endpoint, PodUID: &uid}
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/slots/") || r.Header.Get("X-ClawManager-Control-Token") != "token" {
					t.Fatalf("unexpected mutation or unauthenticated inspection: %s %s", r.Method, r.URL.Path)
				}
				snapshot := auditFreeSnapshot()
				switch scenario {
				case "occupied":
					snapshot = auditOccupiedSnapshot("running")
				case "stopped-owner-idle":
					snapshot = auditOccupiedSnapshot("stopped")
					snapshot["idle_confirmed"] = true
				case "unknown":
					delete(snapshot, "idle_confirmed")
				case "partial-inventory":
					if strings.Contains(r.URL.Path, "slot-1") {
						return nil, errors.New("unreachable sibling")
					}
				case "wrong-uid":
					snapshot["pod_uid"] = "another-pod"
				case "replacement":
					physical.UID = "replacement"
					_, _ = kube.CoreV1().Pods("runtime").Update(ctx, physical, metav1.UpdateOptions{})
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			record := runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: pod.RuntimeType, Ports: map[int]string{20000: "old cleanup pending", 20999: "unobserved port"}, InspectedAt: time.Now().Add(-time.Hour)}
			if scenario == "pod-error" || scenario == "unknown" || scenario == "partial-inventory" {
				record.PodError = "old inventory unavailable"
			}
			if scenario != "no-record" {
				if err := client.writeSlotAudit(ctx, pod, nil, record); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "assignment-reserved" {
				req, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: pod.RuntimeType, PodUID: uid, GatewayPort: 20000})
				if err := client.assignments.EnsureDesired(ctx, k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: pod.RuntimeType, PodUID: uid, Endpoint: endpoint, GatewayID: "gw-7-1", Request: req}); err != nil {
					t.Fatal(err)
				}
			}
			writes := 0
			kube.PrependReactor("update", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
				writes++
				if scenario == "conflict" {
					// Model a newer writer winning after our read. A stale clear
					// must fail without overwriting or retrying this quarantine.
					cm := a.(k8stesting.UpdateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
					newer := record
					newer.Ports = map[int]string{20000: "newer quarantine", 20999: "unobserved port"}
					b, _ := json.Marshal(newer)
					cm.Data["record"] = string(b)
					cm.ResourceVersion = "newer"
					if err := kube.Tracker().Update(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, cm, "runtime"); err != nil {
						t.Fatal(err)
					}
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, runtimeSlotAuditName(uid), errors.New("newer quarantine"))
				}
				return false, nil, nil
			})
			kube.ClearActions()
			ports, err := client.reservedGatewayPorts(ctx, pod)
			if calls == 0 {
				t.Fatal("persisted quarantine bypassed fresh inspection")
			}
			success := scenario == "idle" || scenario == "stopped-owner-idle" || scenario == "pod-error" || scenario == "no-record" || scenario == "assignment-reserved"
			if success && err != nil {
				t.Fatalf("fresh inspection remained blocked: %v", err)
			}
			if scenario == "conflict" && (!apierrors.IsConflict(err) || writes != 1) {
				t.Fatalf("CAS conflict admitted or retried: ports=%v err=%v writes=%d", ports, err, writes)
			}
			if scenario == "replacement" && err == nil {
				t.Fatal("replacement identity admitted")
			}
			_, blocked := ports[20000]
			if (scenario == "idle" || scenario == "stopped-owner-idle" || scenario == "pod-error" || scenario == "no-record") && blocked {
				t.Fatalf("positive fresh exit proof did not clear quarantine: %v", ports)
			}
			if !success && err == nil && !blocked {
				t.Fatalf("uncertain slot admitted: %v", ports)
			}
			if scenario == "assignment-reserved" && !blocked {
				t.Fatal("fresh runtime proof released a persisted assignment")
			}
			cm, persisted, readErr := client.readSlotAudit(ctx, pod)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "no-record" {
				if cm != nil || writes != 0 {
					t.Fatal("unquarantined inspection added a record")
				}
				for _, action := range kube.Actions() {
					if action.GetResource().Resource == "configmaps" && (action.GetVerb() == "create" || action.GetVerb() == "update") {
						t.Fatal("unquarantined inspection performed a write")
					}
				}
				return
			}
			if scenario == "conflict" && persisted.Ports[20000] != "newer quarantine" {
				t.Fatalf("concurrent quarantine lost: %+v", persisted)
			}
			if persisted.Ports[20999] == "" {
				t.Fatal("unobserved quarantine was cleared")
			}
			if success && persisted.Ports[20000] != "" {
				t.Fatalf("fresh positive proof not persisted: %+v", persisted)
			}
			if scenario == "pod-error" && persisted.PodError != "" {
				t.Fatalf("complete valid inventory did not clear pod error: %+v", persisted)
			}
			if (scenario == "unknown" || scenario == "partial-inventory") && persisted.PodError == "" {
				t.Fatal("incomplete inventory cleared pod error")
			}
			if (scenario == "occupied" || scenario == "wrong-uid" || scenario == "replacement" || scenario == "conflict") && persisted.Ports[20000] == "" {
				t.Fatal("unproven slot quarantine was cleared")
			}
		})
	}
}

// A pod-wide error can be replaced by a complete valid inventory even when one
// physical slot remains occupied. That slot still receives no free grant.
func TestSlotAllocationClearsPodErrorWithoutAdmittingOccupiedSlot(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset(auditTestPod())
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, PodUID: &uid, AgentEndpoint: &endpoint}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		b, _ := json.Marshal(auditOccupiedSnapshot("running"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	if err := client.writeSlotAudit(ctx, pod, nil, runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: pod.RuntimeType, PodError: "old inventory unavailable", Ports: map[int]string{}}); err != nil {
		t.Fatal(err)
	}
	ports, err := client.reservedGatewayPorts(ctx, pod)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ports[20000]; !ok {
		t.Fatal("occupied slot admitted")
	}
	_, record, err := client.readSlotAudit(ctx, pod)
	if err != nil || record.PodError != "" {
		t.Fatalf("valid inventory failed to replace pod-wide error: %+v %v", record, err)
	}
}
