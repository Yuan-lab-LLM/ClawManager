package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	k8sservice "clawreef/internal/services/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSlotReconcileRecoversLostCreateAndDrainingResponse(t *testing.T) {
	kube := fake.NewSimpleClientset(slotTestPod())
	fail, creates, deletes := true, 0, 0
	transport := slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if fail {
			return nil, fmt.Errorf("interrupted HTTP request")
		}
		body := `{"gateway_id":"gw-7-1","state":"starting","gateway_port":20000}`
		if r.Method == "POST" {
			creates++
			var req RuntimeAgentCreateGatewayRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.PodUID != "pod-uid" || req.Environment["API_KEY"] != "private" {
				t.Fatal("lost assignment identity/Secret during recovery")
			}
		} else if r.Method == "DELETE" {
			deletes++
			body = `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: transport}).(*slotRuntimeAgentClient)
	req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000, Environment: map[string]string{"API_KEY": "private"}}
	if _, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", req); err == nil {
		t.Fatal("expected interrupted create")
	}
	fail = false
	if err := client.ReconcileAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatalf("create recovery count=%d", creates)
	}
	fail = true
	if err := client.DeleteGateway(context.Background(), "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("expected interrupted stop")
	}
	fail = false
	if err := client.ReconcileAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || deletes != 1 {
		t.Fatal("draining assignment resurrected or not confirmed")
	}
	if err := client.ReconcileAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || deletes != 1 {
		t.Fatal("stopped assignment was replayed")
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	if cm.Data["desired_state"] != "stopped" {
		t.Fatal("reconciled stop not persisted")
	}
}

func TestSlotReconcileRejectsReplacedPodAndTamperedSecret(t *testing.T) {
	for _, mode := range []string{"replaced-pod", "tampered-secret"} {
		t.Run(mode, func(t *testing.T) {
			kube := fake.NewSimpleClientset(slotTestPod())
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
			})}).(*slotRuntimeAgentClient)
			if _, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw"}); err != nil {
				t.Fatal(err)
			}
			// Model losing the final acknowledgement write after runtime accepted.
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
			delete(cm.Data, "applied_generation")
			cm.Labels["clawmanager.io/needs-reconcile"] = "true"
			_, _ = kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{})
			if mode == "replaced-pod" {
				pod, _ := kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
				pod.UID = "replacement"
				_, _ = kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{})
			} else {
				secret, _ := kube.CoreV1().Secrets("runtime").Get(context.Background(), "lite-instance-7-g1", metav1.GetOptions{})
				secret.Labels["clawmanager.io/user-id"] = "999"
				_, _ = kube.CoreV1().Secrets("runtime").Update(context.Background(), secret, metav1.UpdateOptions{})
			}
			if err := client.ReconcileAssignments(context.Background()); err == nil {
				t.Fatal("unsafe assignment replayed")
			}
			if calls != 1 {
				t.Fatal("untrusted recovery contacted runtime")
			}
		})
	}
}

func seedPendingAssignment(t *testing.T, client *slotRuntimeAgentClient, id int) {
	t.Helper()
	req := RuntimeAgentCreateGatewayRequest{InstanceID: id, UserID: 2, Generation: 1, AgentType: "openclaw", PodUID: "pod-uid"}
	raw, _ := json.Marshal(req)
	if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: id, UserID: 2, Generation: 1, RuntimeType: "openclaw", PodUID: "pod-uid", Endpoint: "https://10.0.0.2:19090", GatewayID: fmt.Sprintf("gw-%d-1", id), Request: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestSlotReconcileContinuesPastAnInvalidAssignment(t *testing.T) {
	kube := fake.NewSimpleClientset(slotTestPod())
	calls := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-8-1"}`))}, nil
	})}).(*slotRuntimeAgentClient)
	seedPendingAssignment(t, client, 7)
	seedPendingAssignment(t, client, 8)
	if err := kube.CoreV1().Secrets("runtime").Delete(context.Background(), "lite-instance-7-g1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReconcileAssignments(context.Background()); err == nil {
		t.Fatal("missing Secret was not reported")
	}
	if calls != 1 {
		t.Fatalf("healthy pending assignment blocked by unrelated Secret: calls=%d", calls)
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-8", metav1.GetOptions{})
	if cm.Labels["clawmanager.io/needs-reconcile"] != "false" {
		t.Fatal("healthy assignment not acknowledged")
	}
}

func TestSlotReconcileResumesAfterUnreachableSources(t *testing.T) {
	kube := fake.NewSimpleClientset(slotTestPod())
	var mu sync.Mutex
	var cancelSweep context.CancelFunc
	sweepCalls, healthyCalls := 0, 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		var req RuntimeAgentCreateGatewayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		mu.Lock()
		sweepCalls++
		if req.InstanceID == 20 {
			healthyCalls++
		}
		if sweepCalls >= 8 {
			cancelSweep()
		}
		mu.Unlock()
		if req.InstanceID == 20 {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-20-1"}`))}, nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}).(*slotRuntimeAgentClient)
	for id := 1; id <= 20; id++ {
		seedPendingAssignment(t, client, id)
	}
	for pass := 0; pass < 4; pass++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		mu.Lock()
		cancelSweep = cancel
		sweepCalls = 0
		mu.Unlock()
		_ = client.ReconcileAssignments(ctx)
		cancel()
	}
	mu.Lock()
	defer mu.Unlock()
	if healthyCalls == 0 {
		t.Fatal("later healthy assignment starved behind unreachable sources")
	}
}
