package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func auditTestPod() *corev1.Pod {
	p := slotTestPod()
	p.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
	return p
}
func auditFreeSnapshot() map[string]any {
	return map[string]any{"pod_uid": "pod-uid", "worker_epoch": strings.Repeat("a", 32), "idle_confirmed": true, "quarantined": false, "gateways": []any{}}
}
func auditOccupiedSnapshot(state string) map[string]any {
	s := auditFreeSnapshot()
	s["idle_confirmed"] = false
	s["assignment"] = map[string]any{"pod_uid": "pod-uid", "worker_epoch": strings.Repeat("a", 32), "gateway_id": "gw-7-1", "instance_id": 7, "user_id": 2, "generation": 1, "state": state}
	return s
}
func TestSlotAllocationRequiresFreshRuntimeExitProof(t *testing.T) {
	for _, scenario := range []string{"sql-and-cm-lost", "stopped-cleanup-pending", "unreachable", "missing-idle-proof", "wrong-pod-response", "pod-replaced-during-read", "free"} {
		t.Run(scenario, func(t *testing.T) {
			p := auditTestPod()
			kube := fake.NewSimpleClientset(p)
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/v1/slots/slot-0/state" || r.Header.Get("X-ClawManager-Control-Token") != "token" {
					t.Fatalf("unexpected runtime request: %s %s", r.Method, r.URL.Path)
				}
				s := auditFreeSnapshot()
				switch scenario {
				case "sql-and-cm-lost":
					s = auditOccupiedSnapshot("running")
				case "stopped-cleanup-pending":
					s = auditOccupiedSnapshot("stopped")
					s["quarantined"] = true
				case "unreachable":
					return nil, errors.New("worker unreachable")
				case "missing-idle-proof":
					delete(s, "idle_confirmed")
				case "wrong-pod-response":
					s["pod_uid"] = "other-pod"
				case "pod-replaced-during-read":
					p.UID = "other-pod"
					_, _ = kube.CoreV1().Pods("runtime").Update(r.Context(), p, metav1.UpdateOptions{})
				}
				b, _ := json.Marshal(s)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			ports, err := client.reservedGatewayPorts(context.Background(), models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid})
			_, blocked := ports[20000]
			if scenario == "free" {
				if err != nil || blocked || calls != 1 {
					t.Fatalf("confirmed free slot unavailable: ports=%v err=%v calls=%d", ports, err, calls)
				}
			} else if err == nil && !blocked {
				t.Fatalf("unsafe slot admitted: ports=%v calls=%d", ports, calls)
			}
		})
	}
}

// Only the authority methods exercised by the auditor are supplied. Any
// accidental lifecycle mutation through a different repository method panics.
type auditBindingRepository struct {
	repository.InstanceRuntimeBindingRepository
	bindings []models.InstanceRuntimeBinding
}

func (r *auditBindingRepository) ListByRuntimePodID(context.Context, int64) ([]models.InstanceRuntimeBinding, error) {
	return r.bindings, nil
}
func (r *auditBindingRepository) GetByInstanceID(_ context.Context, id int) (*models.InstanceRuntimeBinding, error) {
	for i := range r.bindings {
		if r.bindings[i].InstanceID == id {
			return &r.bindings[i], nil
		}
	}
	return nil, nil
}

type auditInstanceRepository struct {
	repository.InstanceRepository
	instance *models.Instance
	onLock   func()
}

func (r *auditInstanceRepository) GetByID(int) (*models.Instance, error) { return r.instance, nil }
func (r *auditInstanceRepository) AcquireLifecycleLock(context.Context, int) (func(), error) {
	if r.onLock != nil {
		r.onLock()
	}
	return func() {}, nil
}

type auditPodRepository struct {
	repository.RuntimePodRepository
	pods []models.RuntimePod
}

func (r *auditPodRepository) List(context.Context, string) ([]models.RuntimePod, error) {
	return r.pods, nil
}

func TestRuntimeSlotAuditPersistsUnknownOwnerAcrossManagerRestart(t *testing.T) {
	for _, scenario := range []string{"sql-and-cm-lost", "stopped-cleanup-pending", "unreachable", "wrong-pod", "free"} {
		t.Run(scenario, func(t *testing.T) {
			kube := fake.NewSimpleClientset(auditTestPod())
			snapshot := auditOccupiedSnapshot("running")
			if scenario == "stopped-cleanup-pending" {
				snapshot = auditOccupiedSnapshot("stopped")
				snapshot["quarantined"] = true
			}
			if scenario == "free" {
				snapshot = auditFreeSnapshot()
			}
			calls := 0
			transport := slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet {
					t.Fatalf("unknown owner was mutated: %s", r.Method)
				}
				if scenario == "unreachable" {
					return nil, errors.New("worker unreachable")
				}
				if scenario == "wrong-pod" {
					snapshot["pod_uid"] = "other-pod"
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: transport}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			s := &RuntimeScheduler{agentClient: client, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}}
			_ = s.auditRuntimePodSlots(context.Background(), client, pod)
			list, err := kube.CoreV1().ConfigMaps("runtime").List(context.Background(), metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=runtime-slot-audit"})
			if err != nil || len(list.Items) != 1 {
				t.Fatalf("audit result was not durable: %v %v", list, err)
			}
			var record runtimeSlotAuditRecord
			if err := json.Unmarshal([]byte(list.Items[0].Data["record"]), &record); err != nil {
				t.Fatal(err)
			}
			if scenario != "free" && record.PodError == "" && record.Ports[20000] == "" {
				t.Fatalf("inconsistent slot has no persisted reason: %+v", record)
			}
			if scenario == "free" && (record.PodError != "" || len(record.Ports) != 0) {
				t.Fatalf("free slot quarantined: %+v", record)
			}
			// Simulate a new manager and a worker that now claims idle. Persisted
			// quarantine survives until a fresh periodic audit explicitly resolves it.
			snapshot = auditFreeSnapshot()
			scenario = "after-restart"
			restarted := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: transport}).(*slotRuntimeAgentClient)
			ports, err := restarted.reservedGatewayPorts(context.Background(), pod)
			if len(record.Ports) > 0 && err == nil {
				if _, ok := ports[20000]; !ok {
					t.Fatal("restart silently cleared slot quarantine")
				}
			}
			if record.PodError != "" && err == nil {
				t.Fatal("restart silently cleared Pod quarantine")
			}
			if calls == 0 {
				t.Fatal("periodic audit never contacted runtime")
			}
		})
	}
}

func TestRuntimeSlotAuditLeavesLiveOwnerAndFreshConcurrentStartIntact(t *testing.T) {
	for _, scenario := range []string{"live-owner", "known-quarantined-owner", "concurrent-user-start", "concurrent-worker-owner"} {
		t.Run(scenario, func(t *testing.T) {
			kube := fake.NewSimpleClientset(auditTestPod())
			snapshot := auditOccupiedSnapshot("running")
			writes := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					writes++
					return nil, errors.New("must not mutate a live owner")
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			req, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", PodUID: uid, GatewayPort: 20000})
			if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: "openclaw", PodUID: uid, Endpoint: endpoint, GatewayID: "gw-7-1", Request: req}); err != nil {
				t.Fatal(err)
			}
			instances := &auditInstanceRepository{instance: &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 1, Status: "running"}}
			bindings := &auditBindingRepository{bindings: []models.InstanceRuntimeBinding{{InstanceID: 7, RuntimePodID: 1, Generation: 1, GatewayID: "gw-7-1", GatewayPort: 20000, State: "running"}}}
			if scenario == "known-quarantined-owner" {
				snapshot["quarantined"] = true
			}
			if strings.HasPrefix(scenario, "concurrent-") {
				cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
				cm.Data["desired_state"] = "draining"
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{})
				instances.instance.Status = "stopped"
				instances.onLock = func() {
					if scenario == "concurrent-user-start" {
						instances.instance.Status = "running"
						instances.instance.RuntimeGeneration = 2
					} else {
						snapshot = auditOccupiedSnapshot("running")
						snapshot["assignment"].(map[string]any)["instance_id"] = 8
						snapshot["assignment"].(map[string]any)["gateway_id"] = "gw-8-1"
					}
				}
			}
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			s := &RuntimeScheduler{agentClient: client, bindingRepo: bindings, instanceRepo: instances}
			_ = s.auditRuntimePodSlots(context.Background(), client, pod)
			if writes != 0 {
				t.Fatalf("audit attempted %d mutations after owner became live or changed", writes)
			}
			if scenario == "known-quarantined-owner" {
				_, record, err := client.readSlotAudit(context.Background(), pod)
				if err != nil || record.Ports[20000] == "" {
					t.Fatalf("quarantined live owner lacks durable exclusion: %+v %v", record, err)
				}
			}
		})
	}
}

func TestRuntimeSlotAuditCleansStoppedOwnerOnlyThroughConfirmedStop(t *testing.T) {
	kube := fake.NewSimpleClientset(auditTestPod())
	snapshot := auditOccupiedSnapshot("stopped")
	snapshot["quarantined"] = true
	deletes := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			b, _ := json.Marshal(snapshot)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/gateways/gw-7-1" {
			t.Fatalf("unexpected cleanup: %s %s", r.Method, r.URL.Path)
		}
		deletes++
		snapshot = auditFreeSnapshot()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`))}, nil
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	req, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", PodUID: uid, GatewayPort: 20000})
	if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: "openclaw", PodUID: uid, Endpoint: endpoint, GatewayID: "gw-7-1", Request: req}); err != nil {
		t.Fatal(err)
	}
	cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	cm.Data["desired_state"] = "stopped"
	_, _ = kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{})
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	s := &RuntimeScheduler{agentClient: client, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{instance: &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 1, Status: "stopped"}}}
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err != nil {
		t.Fatal(err)
	}
	_, record, err := client.readSlotAudit(context.Background(), pod)
	if err != nil || deletes != 1 || record.Ports[20000] == "" {
		t.Fatalf("stop did not retain quarantine until fresh exit proof: deletes=%d record=%+v err=%v", deletes, record, err)
	}
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err != nil {
		t.Fatal(err)
	}
	_, record, err = client.readSlotAudit(context.Background(), pod)
	if err != nil || deletes != 1 || len(record.Ports) != 0 {
		t.Fatalf("fresh proof did not resolve stopped owner: deletes=%d record=%+v err=%v", deletes, record, err)
	}
}

func TestSlotAllocationNeverCachesAnIdleGrant(t *testing.T) {
	kube := fake.NewSimpleClientset(auditTestPod())
	snapshot := auditFreeSnapshot()
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		b, _ := json.Marshal(snapshot)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	first, err := client.reservedGatewayPorts(context.Background(), pod)
	if err != nil || len(first) != 0 {
		t.Fatalf("initial free proof: %v %v", first, err)
	}
	snapshot = auditOccupiedSnapshot("running")
	next, err := client.reservedGatewayPorts(context.Background(), pod)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next[20000]; !ok {
		t.Fatal("a concurrent start was hidden by a cached free grant")
	}
}

func TestRuntimeSlotAuditDoesNotOverwriteNewerQuarantine(t *testing.T) {
	kube := fake.NewSimpleClientset(auditTestPod())
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{}).(*slotRuntimeAgentClient)
	record := runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: "openclaw", Ports: map[int]string{20000: "unknown owner"}}
	if err := client.writeSlotAudit(context.Background(), pod, nil, record); err != nil {
		t.Fatal(err)
	}
	prior, _, err := client.readSlotAudit(context.Background(), pod)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	kube.PrependReactor("update", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		updates++
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, prior.Name, errors.New("newer quarantine"))
	})
	record.Ports = map[int]string{}
	if err := client.writeSlotAudit(context.Background(), pod, prior, record); !apierrors.IsConflict(err) {
		t.Fatalf("stale free scan ignored conflict: %v", err)
	}
	if updates != 1 {
		t.Fatal("stale inspection was retried without fresh evidence")
	}
	_, persisted, err := client.readSlotAudit(context.Background(), pod)
	if err != nil || persisted.Ports[20000] == "" {
		t.Fatalf("newer quarantine lost: %+v %v", persisted, err)
	}
}

func TestRuntimeSlotAuditCadenceBatchAndConcurrency(t *testing.T) {
	kube := fake.NewSimpleClientset()
	pods := make([]models.RuntimePod, 0, 33)
	uidByIP := map[string]string{}
	for i := 1; i <= 33; i++ {
		p := auditTestPod()
		p.Name = fmt.Sprintf("pool-%d", i)
		p.UID = types.UID(fmt.Sprintf("uid-%d", i))
		p.Status.PodIP = fmt.Sprintf("10.0.0.%d", i)
		if _, err := kube.CoreV1().Pods("runtime").Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		uid, endpoint := string(p.UID), "https://"+p.Status.PodIP+":19090"
		uidByIP[p.Status.PodIP] = uid
		pods = append(pods, models.RuntimePod{ID: int64(i), RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid})
	}
	var calls, inflight, peak atomic.Int32
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		calls.Add(1)
		snapshot := auditFreeSnapshot()
		snapshot["pod_uid"] = uidByIP[r.URL.Hostname()]
		b, _ := json.Marshal(snapshot)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	s := &RuntimeScheduler{agentClient: client, podRepo: &auditPodRepository{pods: pods}, instanceRepo: &auditInstanceRepository{}, bindingRepo: &auditBindingRepository{}}
	var auditor runtimeSlotAuditor
	if err := auditor.Reconcile(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 32 || peak.Load() > 8 {
		t.Fatalf("audit bounds exceeded: calls=%d peak=%d", calls.Load(), peak.Load())
	}
	if err := auditor.Reconcile(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 32 {
		t.Fatal("audit ran again before 60 seconds")
	}
	auditor.lastRun = time.Now().Add(-runtimeSlotAuditInterval)
	if err := auditor.Reconcile(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 64 {
		t.Fatalf("next bounded pass did not run: %d", calls.Load())
	}
	pod33 := pods[32]
	if _, _, err := client.readSlotAudit(context.Background(), pod33); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), runtimeSlotAuditName(*pod33.PodUID), metav1.GetOptions{}); err != nil {
		t.Fatalf("rotation starved the last Pod: %v", err)
	}
}

func TestRuntimeSlotAuditRejectsAmbiguousPhysicalInventory(t *testing.T) {
	for _, scenario := range []string{"missing-slots", "duplicate-env", "value-from", "missing-index", "duplicate-name"} {
		t.Run(scenario, func(t *testing.T) {
			p := auditTestPod()
			capacity := 1
			switch scenario {
			case "missing-slots":
				p.Spec.Containers = nil
			case "duplicate-env":
				p.Spec.Containers[0].Env = append([]corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "0"}}, p.Spec.Containers[0].Env...)
			case "value-from":
				p.Spec.Containers[0].Env[0].ValueFrom = &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}
			case "missing-index":
				p.Spec.Containers[0].Name = "slot-1"
			case "duplicate-name":
				capacity = 2
				p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20001"}}})
			}
			if slots, err := runtimeAuditPhysicalSlots(p, capacity); err == nil {
				t.Fatalf("ambiguous physical inventory was accepted: %+v", slots)
			}
		})
	}
}
