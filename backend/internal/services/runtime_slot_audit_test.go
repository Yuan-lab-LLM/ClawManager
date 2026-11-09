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
	"sync"
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

func auditRoutingPod(count, block int) *corev1.Pod {
	p := slotTestPod()
	env := func(name, value string) corev1.EnvVar { return corev1.EnvVar{Name: name, Value: value} }
	p.Spec.Containers = []corev1.Container{{Name: "runtime", Env: []corev1.EnvVar{env("RUNTIME_AGENT_ROLE", "pool"), env("RUNTIME_SLOT_COUNT", fmt.Sprint(count)), env("RUNTIME_SLOT_SOCKET_DIR", "/run/clawmanager-slots"), env("RUNTIME_GATEWAY_PORT_START", "20000"), env("RUNTIME_GATEWAY_PORT_END", "20299"), env("RUNTIME_GATEWAY_PORT_BLOCK_SIZE", fmt.Sprint(block))}}}
	for i := 0; i < count; i++ {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: fmt.Sprintf("slot-%d", i), Env: []corev1.EnvVar{env("RUNTIME_AGENT_ROLE", "slot"), env("RUNTIME_SLOT_INDEX", fmt.Sprint(i)), env("RUNTIME_SLOT_SOCKET", fmt.Sprintf("/run/clawmanager-slots/slot-%d.sock", i)), env("RUNTIME_GATEWAY_PORT_START", fmt.Sprint(20000+i*block)), env("RUNTIME_GATEWAY_PORT_END", fmt.Sprint(20000+(i+1)*block-1)), env("RUNTIME_GATEWAY_PORT_BLOCK_SIZE", fmt.Sprint(block))}})
	}
	return p
}
func auditTestPod() *corev1.Pod { return auditRoutingPod(1, 3) }
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
	releases int
	listErr  error
}

func (r *auditBindingRepository) ListByRuntimePodID(context.Context, int64) ([]models.InstanceRuntimeBinding, error) {
	return r.bindings, r.listErr
}
func (r *auditBindingRepository) GetByInstanceID(_ context.Context, id int) (*models.InstanceRuntimeBinding, error) {
	for i := range r.bindings {
		if r.bindings[i].InstanceID == id {
			return &r.bindings[i], nil
		}
	}
	return nil, nil
}

func (r *auditBindingRepository) DeleteConfirmedStoppedSlotBinding(_ context.Context, expected models.Instance, b models.InstanceRuntimeBinding) (bool, error) {
	for i, current := range r.bindings {
		if current.InstanceID == b.InstanceID && current.RuntimePodID == b.RuntimePodID && current.Generation == b.Generation && current.GatewayID == b.GatewayID && current.GatewayPort == b.GatewayPort {
			r.bindings = append(r.bindings[:i], r.bindings[i+1:]...)
			r.releases++
			return true, nil
		}
	}
	return false, nil
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
			// A restart with unchanged uncertain runtime evidence must retain the
			// durable quarantine; a fresh positive proof is covered separately.
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
		now := time.Now()
		pods = append(pods, models.RuntimePod{ID: int64(i), RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid, State: "ready", LastSeenAt: &now})
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
				p.Spec.Containers[1].Env = append([]corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "0"}}, p.Spec.Containers[1].Env...)
			case "value-from":
				p.Spec.Containers[1].Env[0].ValueFrom = &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}
			case "missing-index":
				p.Spec.Containers[1].Name = "slot-1"
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

func TestRuntimeSlotAuditRejectsMisdirectedWorkerRouting(t *testing.T) {
	for _, scenario := range []string{"worker-role", "worker-index", "worker-socket", "worker-port-start", "worker-port-end", "worker-block", "coordinator-role", "coordinator-count", "coordinator-socket-dir", "coordinator-start", "coordinator-end", "coordinator-block", "duplicate-coordinator-env", "indirect-worker-index"} {
		t.Run(scenario, func(t *testing.T) {
			p := auditRoutingPod(2, 3)
			change := func(container int, name, value string) {
				for i := range p.Spec.Containers[container].Env {
					if p.Spec.Containers[container].Env[i].Name == name {
						p.Spec.Containers[container].Env[i].Value = value
						return
					}
				}
				t.Fatalf("missing fixture env %s", name)
			}
			switch scenario {
			case "worker-role":
				change(1, "RUNTIME_AGENT_ROLE", "pool")
			case "worker-index":
				change(1, "RUNTIME_SLOT_INDEX", "1")
			case "worker-socket":
				change(1, "RUNTIME_SLOT_SOCKET", "/run/clawmanager-slots/slot-1.sock")
			case "worker-port-start":
				change(1, "RUNTIME_GATEWAY_PORT_START", "20003")
			case "worker-port-end":
				change(1, "RUNTIME_GATEWAY_PORT_END", "20003")
			case "worker-block":
				change(1, "RUNTIME_GATEWAY_PORT_BLOCK_SIZE", "1")
			case "coordinator-role":
				change(0, "RUNTIME_AGENT_ROLE", "slot")
			case "coordinator-count":
				change(0, "RUNTIME_SLOT_COUNT", "3")
			case "coordinator-socket-dir":
				change(0, "RUNTIME_SLOT_SOCKET_DIR", "/other")
			case "coordinator-start":
				change(0, "RUNTIME_GATEWAY_PORT_START", "21000")
			case "coordinator-end":
				change(0, "RUNTIME_GATEWAY_PORT_END", "20000")
			case "coordinator-block":
				change(0, "RUNTIME_GATEWAY_PORT_BLOCK_SIZE", "1")
			case "duplicate-coordinator-env":
				p.Spec.Containers[0].Env = append(p.Spec.Containers[0].Env, corev1.EnvVar{Name: "RUNTIME_SLOT_COUNT", Value: "2"})
			case "indirect-worker-index":
				for i := range p.Spec.Containers[1].Env {
					if p.Spec.Containers[1].Env[i].Name == "RUNTIME_SLOT_INDEX" {
						p.Spec.Containers[1].Env[i].ValueFrom = &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}
					}
				}
			}
			if _, err := runtimeAuditPhysicalSlots(p, 2); err == nil {
				t.Fatal("misdirected routing granted slot proof")
			}
		})
	}
}

func TestRuntimeSlotAuditReleasesOnlyExactStoppedSQLBinding(t *testing.T) {
	for _, scenario := range []string{"physically-free", "cleanup-pending", "concurrent-binding-change"} {
		t.Run(scenario, func(t *testing.T) {
			kube := fake.NewSimpleClientset(auditTestPod())
			snapshot := auditOccupiedSnapshot("stopped")
			if scenario == "physically-free" {
				snapshot["idle_confirmed"] = true
			} else {
				snapshot["quarantined"] = true
			}
			deletes := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					b, _ := json.Marshal(snapshot)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
				}
				deletes++
				snapshot["idle_confirmed"] = true
				snapshot["quarantined"] = false
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
			bindings := &auditBindingRepository{bindings: []models.InstanceRuntimeBinding{{InstanceID: 7, RuntimePodID: 1, Generation: 1, GatewayID: "gw-7-1", GatewayPort: 20000, State: "running"}}}
			instances := &auditInstanceRepository{instance: &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 1, Status: "stopped"}}
			if scenario == "concurrent-binding-change" {
				instances.onLock = func() { bindings.bindings[0].GatewayID = "gw-7-2"; bindings.bindings[0].Generation = 2 }
			}
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			s := &RuntimeScheduler{agentClient: client, bindingRepo: bindings, instanceRepo: instances}
			if err := s.auditRuntimePodSlots(context.Background(), client, pod); err != nil {
				t.Fatal(err)
			}
			if scenario == "concurrent-binding-change" {
				if deletes != 0 || bindings.releases != 0 || len(bindings.bindings) != 1 {
					t.Fatal("changed binding was stopped or released")
				}
				return
			}
			wantDeletes := 1
			if scenario == "physically-free" {
				wantDeletes = 0
			}
			if deletes != wantDeletes || bindings.releases != 1 || len(bindings.bindings) != 0 {
				t.Fatalf("confirmed stopped binding leaked: deletes=%d releases=%d bindings=%+v", deletes, bindings.releases, bindings.bindings)
			}
			if err := s.auditRuntimePodSlots(context.Background(), client, pod); err != nil {
				t.Fatal(err)
			}
			_, record, err := client.readSlotAudit(context.Background(), pod)
			if err != nil || len(record.Ports) != 0 {
				t.Fatalf("quarantine never cleared: %+v %v", record, err)
			}
		})
	}
}

func TestSlotPhysicalGatewayPortsUsesVerifiedSpecWithoutRuntimeHTTP(t *testing.T) {
	kube := fake.NewSimpleClientset(auditRoutingPod(2, 3))
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("physical routing inventory contacted runtime HTTP")
		return nil, errors.New("unexpected HTTP")
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{RuntimeType: "openclaw", Capacity: 2, AgentEndpoint: &endpoint, PodUID: &uid}
	ports, err := client.physicalGatewayPorts(context.Background(), pod)
	if err != nil || len(ports) != 2 {
		t.Fatalf("physical inventory failed: %v %v", ports, err)
	}
	for _, port := range []int{20000, 20003} {
		if _, ok := ports[port]; !ok {
			t.Fatalf("missing physical port %d", port)
		}
	}
	pod.RuntimeType = "hermes"
	if _, err := client.physicalGatewayPorts(context.Background(), pod); err == nil {
		t.Fatal("registered runtime block mismatch admitted")
	}
}

func TestRuntimeSlotAuditFiltersHistoryBeforeCursorAndBudget(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	stale := now.Add(-4 * time.Minute)
	future := now.Add(2 * time.Minute)
	endpoint, uid := "https://10.0.0.200:19090", "historical-uid"
	disabled := false
	historical := models.RuntimePod{ID: 1, RuntimeType: "openclaw", State: "ready", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid, LastSeenAt: &now}
	mutations := []func(*models.RuntimePod){
		func(p *models.RuntimePod) { p.State = "unknown" }, func(p *models.RuntimePod) { p.SchedulingPaused = true }, func(p *models.RuntimePod) { p.Draining = true },
		func(p *models.RuntimePod) { p.SchedulingEnabled = &disabled }, func(p *models.RuntimePod) { p.LastSeenAt = &stale }, func(p *models.RuntimePod) { p.LastSeenAt = &future },
		func(p *models.RuntimePod) { p.LastSeenAt = nil }, func(p *models.RuntimePod) { p.PodUID = nil }, func(p *models.RuntimePod) { p.AgentEndpoint = nil },
		func(p *models.RuntimePod) { p.Capacity = 0 }, func(p *models.RuntimePod) { p.PodUID = new(string) }, func(p *models.RuntimePod) { p.AgentEndpoint = new(string) },
	}
	pods := make([]models.RuntimePod, 0, 50)
	for i := 0; i < 48; i++ {
		p := historical
		p.ID = int64(i + 1)
		mutations[i%len(mutations)](&p)
		pods = append(pods, p)
	}
	kube := fake.NewSimpleClientset()
	allowed := map[string]string{}
	for i := 1; i <= 2; i++ {
		p := auditTestPod()
		p.Name = fmt.Sprintf("active-%d", i)
		p.UID = types.UID(fmt.Sprintf("active-uid-%d", i))
		p.Status.PodIP = fmt.Sprintf("10.0.0.%d", i)
		if _, err := kube.CoreV1().Pods("runtime").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		currentUID, currentEndpoint := string(p.UID), "https://"+p.Status.PodIP+":19090"
		allowed[p.Status.PodIP] = currentUID
		pods = append(pods, models.RuntimePod{ID: int64(100 + i), RuntimeType: "openclaw", State: "ready", Capacity: 1, AgentEndpoint: &currentEndpoint, PodUID: &currentUID, LastSeenAt: &now})
	}
	var calls atomic.Int32
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		snapshot := auditFreeSnapshot()
		snapshot["pod_uid"] = allowed[r.URL.Hostname()]
		b, _ := json.Marshal(snapshot)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	frozen := runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: "openclaw", PodError: "historical unknown owner", Ports: map[int]string{20000: "frozen"}}
	if err := client.writeSlotAudit(ctx, historical, nil, frozen); err != nil {
		t.Fatal(err)
	}
	before, _, err := client.readSlotAudit(ctx, historical)
	if err != nil {
		t.Fatal(err)
	}
	kube.ClearActions()
	scheduler := &RuntimeScheduler{agentClient: client, podRepo: &auditPodRepository{pods: pods}, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}, heartbeatTimeout: time.Minute}
	auditor := runtimeSlotAuditor{cursor: 20}
	if err := auditor.Reconcile(ctx, scheduler); err != nil {
		t.Fatalf("excluded history consumed audit requests: %v", err)
	}
	if calls.Load() != 2 || auditor.cursor != 102 {
		t.Fatalf("active pools starved by history: calls=%d cursor=%d", calls.Load(), auditor.cursor)
	}
	after, _, err := client.readSlotAudit(ctx, historical)
	if err != nil || after.Data["record"] != before.Data["record"] {
		t.Fatalf("skipping history changed its durable quarantine: %v", err)
	}
}

func TestRuntimeSlotAuditReleasesExactStoppedBindingAfterFreshFreeProof(t *testing.T) {
	for _, scenario := range []string{"stopped", "desired-running", "generation-changed", "binding-changed", "became-occupied", "pod-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			p := auditTestPod()
			kube := fake.NewSimpleClientset(p)
			reads := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatalf("free slot caused runtime mutation: %s %s", r.Method, r.URL.Path)
				}
				reads++
				snapshot := auditFreeSnapshot()
				if reads > 1 && scenario == "became-occupied" {
					snapshot = auditOccupiedSnapshot("running")
				}
				if reads > 1 && scenario == "pod-replaced" {
					p.UID = "replacement"
					_, _ = kube.CoreV1().Pods("runtime").Update(r.Context(), p, metav1.UpdateOptions{})
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			bindings := &auditBindingRepository{bindings: []models.InstanceRuntimeBinding{{InstanceID: 7, RuntimePodID: 1, Generation: 1, GatewayID: "gw-7-1", GatewayPort: 20000, State: "running"}}}
			instances := &auditInstanceRepository{instance: &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 1, Status: "stopped"}}
			switch scenario {
			case "desired-running":
				instances.instance.Status = "running"
			case "generation-changed":
				instances.instance.RuntimeGeneration = 2
			case "binding-changed":
				instances.onLock = func() { bindings.bindings[0].Generation = 2; bindings.bindings[0].GatewayID = "gw-7-2" }
			}
			s := &RuntimeScheduler{agentClient: client, bindingRepo: bindings, instanceRepo: instances}
			_ = s.auditRuntimePodSlots(context.Background(), client, pod)
			wantRelease := 0
			if scenario == "stopped" {
				wantRelease = 1
			}
			if bindings.releases != wantRelease {
				t.Fatalf("releases=%d, want %d (reads=%d)", bindings.releases, wantRelease, reads)
			}
		})
	}
}

type auditObservationRepository struct {
	auditPodRepository
	observations []models.RuntimePodObservation
	accept       bool
}

func (r *auditObservationRepository) SaveRuntimeObservation(_ context.Context, id int64, uid string, observation models.RuntimePodObservation) (bool, error) {
	if id != 1 || uid != "pod-uid" || observation.PodUID != uid {
		return false, errors.New("unexpected observation target")
	}
	r.observations = append(r.observations, observation)
	return r.accept, nil
}

func TestRuntimeSlotAuditPersistsSanitizedFactsWithoutUpgradingLegacyProof(t *testing.T) {
	for _, scenario := range []string{"free", "legacy-owner", "old-worker-owner", "malformed-owner", "missing-idle-proof", "read-failure", "wrong-response-uid", "stopped-tombstone", "legacy-stopped-tombstone", "conflicting-gateways", "matching-gateway", "binding-read-failure"} {
		t.Run(scenario, func(t *testing.T) {
			kube := fake.NewSimpleClientset(auditTestPod())
			snapshot := auditOccupiedSnapshot("starting")
			snapshot["secret"] = "DO-NOT-PERSIST"
			snapshot["gateways"] = []any{map[string]any{"gateway_id": "gw-7-1", "state": "error", "credential": "DO-NOT-PERSIST"}}
			wantState, wantVerified, wantErr := "occupied", true, false
			switch scenario {
			case "free":
				snapshot = auditFreeSnapshot()
				wantState, wantVerified = "free", false
			case "legacy-owner":
				delete(snapshot["assignment"].(map[string]any), "worker_epoch")
				wantVerified, wantErr = false, true
			case "old-worker-owner":
				snapshot["assignment"].(map[string]any)["worker_epoch"] = strings.Repeat("b", 32)
				wantVerified, wantErr = false, true
			case "malformed-owner":
				snapshot["assignment"].(map[string]any)["generation"] = 0
				wantVerified, wantErr = false, true
			case "missing-idle-proof":
				delete(snapshot, "idle_confirmed")
				wantVerified, wantErr = false, true
			case "read-failure", "wrong-response-uid":
				wantState, wantVerified, wantErr = "unknown", false, true
				if scenario == "wrong-response-uid" {
					snapshot["pod_uid"] = "other-pod"
				}
			case "stopped-tombstone":
				snapshot = auditOccupiedSnapshot("stopped")
				snapshot["idle_confirmed"] = true
				wantState, wantVerified = "free", false
			case "legacy-stopped-tombstone":
				snapshot = auditOccupiedSnapshot("stopped")
				delete(snapshot["assignment"].(map[string]any), "worker_epoch")
				wantState, wantVerified, wantErr = "unknown", false, true
			case "conflicting-gateways":
				snapshot["gateways"] = []any{map[string]any{"gateway_id": "gw-7-1", "state": "running"}, map[string]any{"gateway_id": "gw-8-2", "state": "running"}}
				wantState, wantVerified, wantErr = "unknown", false, true
			case "matching-gateway":
				snapshot["gateways"] = []any{map[string]any{"gateway_id": "gw-8-2", "state": "stopped"}, map[string]any{"gateway_id": "gw-7-1", "state": "error"}}
				wantErr = true // The reported owner has no persisted assignment.
			case "binding-read-failure":
				wantErr = true
			}
			mutations := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					mutations++
					return nil, errors.New("unexpected mutation")
				}
				if scenario == "read-failure" {
					return nil, errors.New("credential=DO-NOT-PERSIST")
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			store := &auditObservationRepository{accept: true}
			bindings := &auditBindingRepository{}
			if scenario == "binding-read-failure" {
				bindings.listErr = errors.New("SQL unavailable")
			}
			s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: bindings, instanceRepo: &auditInstanceRepository{}}
			_ = s.auditRuntimePodSlots(context.Background(), client, pod)
			if len(store.observations) != 1 || len(store.observations[0].Slots) != 1 {
				t.Fatalf("fresh facts were not persisted: %+v", store.observations)
			}
			o := store.observations[0]
			slot := o.Slots[0]
			if o.ObservedAt.IsZero() || slot.State != wantState || slot.IdentityVerified != wantVerified || (slot.Error != "") != wantErr {
				t.Fatalf("unexpected sanitized facts: %+v", o)
			}
			if scenario == "legacy-owner" && (slot.InstanceID != 7 || slot.UserID != 2 || slot.Generation != 1 || slot.GatewayID != "gw-7-1" || slot.InstanceState != "starting" || slot.GatewayState != "error") {
				t.Fatalf("legacy reported owner disappeared: %+v", slot)
			}
			if scenario == "matching-gateway" && slot.GatewayState != "error" {
				t.Fatalf("wrong gateway state: %+v", slot)
			}
			b, _ := json.Marshal(o)
			if strings.Contains(string(b), "DO-NOT-PERSIST") || strings.Contains(string(b), "worker_epoch") || mutations != 0 {
				t.Fatalf("unsafe observation or mutation: %s (mutations=%d)", b, mutations)
			}
		})
	}
}

func TestRuntimeSlotAuditDoesNotPersistUnverifiedSourceUID(t *testing.T) {
	for _, scenario := range []string{"before-success", "after-success", "after-failure"} {
		t.Run(scenario, func(t *testing.T) {
			p := auditTestPod()
			if scenario == "before-success" {
				p.UID = "replacement"
			}
			kube := fake.NewSimpleClientset(p)
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("changed source was mutated")
				}
				p.UID = "replacement"
				_, _ = kube.CoreV1().Pods("runtime").Update(r.Context(), p, metav1.UpdateOptions{})
				if scenario == "after-failure" {
					return nil, errors.New("connection interrupted")
				}
				b, _ := json.Marshal(auditFreeSnapshot())
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			store := &auditObservationRepository{accept: true}
			s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}}
			if err := s.auditRuntimePodSlots(context.Background(), client, pod); err == nil {
				t.Fatal("changed source UID was accepted")
			}
			if len(store.observations) != 0 {
				t.Fatalf("changed source persisted stale evidence: %+v", store.observations)
			}
		})
	}
}

type auditOrderedObservationRepository struct {
	auditPodRepository
	mu       sync.Mutex
	latest   models.RuntimePodObservation
	attempts []time.Time
}

func (r *auditOrderedObservationRepository) SaveRuntimeObservation(_ context.Context, _ int64, _ string, observation models.RuntimePodObservation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = append(r.attempts, observation.ObservedAt)
	if !observation.ObservedAt.After(r.latest.ObservedAt) {
		return false, nil
	}
	r.latest = observation
	return true, nil
}

func TestRuntimeSlotAuditRejectsOlderScanCompletingAfterNewerFacts(t *testing.T) {
	kube := fake.NewSimpleClientset(auditTestPod())
	started, resume := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("superseded observation mutated runtime: %s", r.Method)
		}
		snapshot := auditOccupiedSnapshot("running")
		if reads.Add(1) == 1 {
			close(started)
			select {
			case <-resume:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			snapshot = auditFreeSnapshot()
		}
		b, _ := json.Marshal(snapshot)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	store := &auditOrderedObservationRepository{}
	s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}}
	olderResult := make(chan error, 1)
	go func() { olderResult <- s.auditRuntimePodSlots(context.Background(), client, pod) }()
	<-started
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-olderResult; err == nil {
		t.Fatal("superseded inspection accepted")
	}
	if len(store.attempts) != 2 || !store.attempts[1].Before(store.attempts[0]) || store.latest.Slots[0].State != "occupied" {
		t.Fatalf("older free snapshot hid newer owner: attempts=%v latest=%+v", store.attempts, store.latest)
	}
	_, record, err := client.readSlotAudit(context.Background(), pod)
	if err != nil || record.Ports[20000] == "" {
		t.Fatalf("older result cleared newer quarantine: %+v %v", record, err)
	}
}

func TestRuntimeSlotAuditObservationRejectionPreventsCleanup(t *testing.T) {
	kube := fake.NewSimpleClientset(auditTestPod())
	reads := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("rejected observation mutated runtime")
		}
		reads++
		b, _ := json.Marshal(auditFreeSnapshot())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	bindings := &auditBindingRepository{bindings: []models.InstanceRuntimeBinding{{InstanceID: 7, RuntimePodID: 1, Generation: 1, GatewayID: "gw-7-1", GatewayPort: 20000, State: "running"}}}
	store := &auditObservationRepository{accept: false}
	s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: bindings, instanceRepo: &auditInstanceRepository{instance: &models.Instance{ID: 7, UserID: 2, RuntimeGeneration: 1, Status: "stopped"}}}
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err == nil {
		t.Fatal("observation rejection ignored")
	}
	if reads != 1 || bindings.releases != 0 {
		t.Fatalf("superseded scan continued cleanup: reads=%d releases=%d", reads, bindings.releases)
	}
	for _, action := range kube.Actions() {
		if action.GetResource().Resource == "configmaps" && (action.GetVerb() == "create" || action.GetVerb() == "update") {
			t.Fatal("superseded scan changed quarantine after observation rejection")
		}
	}
}

func TestRuntimeSlotAuditPersistsPhysicalInventoryFailureForCurrentUID(t *testing.T) {
	p := auditTestPod()
	p.Spec.Containers = nil
	kube := fake.NewSimpleClientset(p)
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid physical inventory contacted Runtime")
		return nil, errors.New("unexpected Runtime request")
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
	store := &auditObservationRepository{accept: true}
	s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}}
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err == nil {
		t.Fatal("invalid inventory accepted")
	}
	if len(store.observations) != 1 || store.observations[0].Error == "" || len(store.observations[0].Slots) != 0 {
		t.Fatalf("invalid inventory lost diagnostic: %+v", store.observations)
	}
}

func TestRuntimeSlotAuditPreservesHermesLegacyReportedOwnerWithoutSQLBinding(t *testing.T) {
	kube := fake.NewSimpleClientset(auditRoutingPod(2, 1))
	mutations := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			mutations++
			return nil, errors.New("unexpected Runtime mutation")
		}
		snapshot := auditFreeSnapshot()
		if r.URL.Path == "/v1/slots/slot-0/state" {
			snapshot["idle_confirmed"] = false
			snapshot["assignment"] = map[string]any{"pod_uid": "pod-uid", "instance_id": 25, "user_id": 1, "generation": 14, "gateway_id": "gw-25-14", "state": "starting"}
			snapshot["gateways"] = []any{map[string]any{"gateway_id": "gw-25-14", "state": "error"}}
		} else {
			snapshot["assignment"] = map[string]any{"pod_uid": "pod-uid", "instance_id": 161, "user_id": 1, "generation": 2, "gateway_id": "gw-161-2", "state": "stopped"}
		}
		b, _ := json.Marshal(snapshot)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}).(*slotRuntimeAgentClient)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	pod := models.RuntimePod{ID: 132, RuntimeType: "hermes", Capacity: 2, AgentEndpoint: &endpoint, PodUID: &uid}
	store := &auditOrderedObservationRepository{}
	bindings := &auditBindingRepository{}
	s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: bindings, instanceRepo: &auditInstanceRepository{}}
	if err := s.auditRuntimePodSlots(context.Background(), client, pod); err == nil {
		t.Fatal("legacy owner gained strict exit proof")
	}
	if len(store.latest.Slots) != 2 {
		t.Fatalf("Hermes facts lost: %+v", store.latest)
	}
	owner, tombstone := store.latest.Slots[0], store.latest.Slots[1]
	if owner.State != "occupied" || owner.InstanceID != 25 || owner.UserID != 1 || owner.Generation != 14 || owner.GatewayID != "gw-25-14" || owner.InstanceState != "starting" || owner.GatewayState != "error" || owner.IdentityVerified || owner.Error == "" {
		t.Fatalf("Hermes residual owner hidden or trusted: %+v", owner)
	}
	if tombstone.State == "occupied" || tombstone.IdentityVerified || mutations != 0 || bindings.releases != 0 || len(bindings.bindings) != 0 {
		t.Fatalf("legacy facts granted mutation authority: tombstone=%+v mutations=%d bindings=%+v", tombstone, mutations, bindings)
	}
}

func TestRuntimeSlotAuditPersistsReservationAndQuarantineDecisions(t *testing.T) {
	for _, scenario := range []string{"free", "sql-binding", "running-assignment", "stopped-assignment", "malformed-assignment", "assignment-read-failure", "binding-read-failure", "prior-quarantine", "prior-quarantine-stopped-owner", "prior-pod-quarantine", "quarantine-clear-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(auditTestPod())
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("reservation classification mutated Runtime")
				}
				snapshot := auditFreeSnapshot()
				if scenario == "prior-quarantine-stopped-owner" {
					snapshot = auditOccupiedSnapshot("stopped")
					snapshot["idle_confirmed"] = true
				}
				b, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			pod := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
			if scenario == "running-assignment" || scenario == "stopped-assignment" || scenario == "malformed-assignment" {
				req, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: pod.RuntimeType, PodUID: uid, GatewayPort: 20000})
				if err := client.assignments.EnsureDesired(ctx, k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: pod.RuntimeType, PodUID: uid, Endpoint: endpoint, GatewayID: "gw-7-1", Request: req}); err != nil {
					t.Fatal(err)
				}
				if scenario == "stopped-assignment" {
					cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
					cm.Data["desired_state"] = "stopped"
					_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
				}
				if scenario == "malformed-assignment" {
					cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
					delete(cm.Data, "generation")
					_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
				}
			}
			if scenario == "prior-quarantine" || scenario == "prior-quarantine-stopped-owner" || scenario == "quarantine-clear-conflict" {
				if err := client.writeSlotAudit(ctx, pod, nil, runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: pod.RuntimeType, Ports: map[int]string{20000: "old cleanup pending"}}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "prior-pod-quarantine" {
				if err := client.writeSlotAudit(ctx, pod, nil, runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: pod.RuntimeType, Ports: map[int]string{}, PodError: "old inventory unavailable"}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "quarantine-clear-conflict" {
				kube.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, runtimeSlotAuditName(uid), errors.New("newer quarantine"))
				})
			}
			if scenario == "assignment-read-failure" {
				kube.PrependReactor("list", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("assignment inventory unavailable")
				})
			}
			bindings := &auditBindingRepository{}
			if scenario == "sql-binding" {
				bindings.bindings = []models.InstanceRuntimeBinding{{InstanceID: 7, RuntimePodID: 1, Generation: 1, GatewayID: "gw-7-1", GatewayPort: 20000}}
			}
			if scenario == "binding-read-failure" {
				bindings.listErr = errors.New("SQL unavailable")
			}
			store := &auditObservationRepository{accept: true}
			s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: bindings, instanceRepo: &auditInstanceRepository{}}
			_ = s.auditRuntimePodSlots(ctx, client, pod)
			if len(store.observations) != 1 || len(store.observations[0].Slots) != 1 {
				t.Fatalf("missing classified observation: %+v", store.observations)
			}
			slot := store.observations[0].Slots[0]
			wantState := "unknown"
			if scenario == "free" || scenario == "stopped-assignment" {
				wantState = "free"
			}
			if slot.State != wantState || (wantState == "unknown" && slot.Error == "") {
				t.Fatalf("saved facts disagree with reservations/quarantine: %+v", store.observations[0])
			}
			if scenario == "prior-quarantine-stopped-owner" && (slot.InstanceID != 7 || slot.GatewayID != "gw-7-1" || slot.InstanceState != "stopped" || slot.IdentityVerified) {
				t.Fatalf("unknown slot lost its reported stopped owner or gained authority: %+v", slot)
			}
			if scenario == "prior-quarantine" || scenario == "prior-pod-quarantine" {
				if err := s.auditRuntimePodSlots(ctx, client, pod); err != nil {
					t.Fatal(err)
				}
				if store.observations[1].Slots[0].State != "free" {
					t.Fatalf("persisted quarantine release never became free: %+v", store.observations)
				}
			}
			if scenario == "running-assignment" {
				ports, err := client.reservedGatewayPorts(ctx, pod)
				if _, reserved := ports[20000]; err != nil || !reserved {
					t.Fatalf("running CM reservation lost: ports=%v err=%v", ports, err)
				}
			}
		})
	}
}

func TestRuntimeSlotAuditDisplaysOnlyCurrentUIDLegacyEnvelope(t *testing.T) {
	for _, runtime := range []struct {
		name                   string
		podID                  int64
		instanceID, generation int
		uid                    string
	}{
		{"hermes", 132, 25, 14, "7ab50b09-bfcc-4068-89cf-7671fcffd385"},
		{"opencode", 135, 118, 9, "opencode-current-uid"},
	} {
		for _, scenario := range []string{"current nested UID", "foreign top UID", "missing nested UID", "foreign nested UID", "missing assignment", "modern foreign nested UID", "modern missing nested UID"} {
			t.Run(runtime.name+"/"+scenario, func(t *testing.T) {
				physical := auditRoutingPod(1, RuntimeGatewayPortBlockSize(runtime.name))
				physical.UID = types.UID(runtime.uid)
				kube := fake.NewSimpleClientset(physical)
				gatewayID := fmt.Sprintf("gw-%d-%d", runtime.instanceID, runtime.generation)
				// The authenticated live legacy envelope has exactly these top-level
				// keys: no pod_uid, worker_epoch, or idle_confirmed proof exists.
				envelope := map[string]any{
					"digest":      "legacy-digest-DO-NOT-PERSIST",
					"gateways":    []any{map[string]any{"gateway_id": gatewayID, "state": "error"}},
					"assignment":  map[string]any{"pod_uid": runtime.uid, "instance_id": runtime.instanceID, "user_id": 1, "generation": runtime.generation, "gateway_id": gatewayID, "state": "starting"},
					"skills":      []any{},
					"quarantined": false,
				}
				switch scenario {
				case "foreign top UID":
					envelope["pod_uid"] = "another-pod"
				case "missing nested UID":
					delete(envelope["assignment"].(map[string]any), "pod_uid")
				case "foreign nested UID":
					envelope["assignment"].(map[string]any)["pod_uid"] = "another-pod"
				case "missing assignment":
					envelope["assignment"] = nil
				case "modern foreign nested UID":
					envelope["pod_uid"] = runtime.uid
					envelope["assignment"].(map[string]any)["pod_uid"] = "another-pod"
				case "modern missing nested UID":
					envelope["pod_uid"] = runtime.uid
					delete(envelope["assignment"].(map[string]any), "pod_uid")
				}
				client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodGet {
						t.Fatalf("legacy proof caused mutation: %s %s", r.Method, r.URL.Path)
					}
					b, _ := json.Marshal(envelope)
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
				})}).(*slotRuntimeAgentClient)
				endpoint, uid := "https://10.0.0.2:19090", runtime.uid
				pod := models.RuntimePod{ID: runtime.podID, RuntimeType: runtime.name, Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid}
				store := &auditOrderedObservationRepository{}
				bindings := &auditBindingRepository{}
				s := &RuntimeScheduler{agentClient: client, podRepo: store, bindingRepo: bindings, instanceRepo: &auditInstanceRepository{}}
				if err := s.auditRuntimePodSlots(context.Background(), client, pod); err == nil {
					t.Fatal("legacy envelope gained strict exit proof")
				}
				if len(store.latest.Slots) != 1 {
					t.Fatalf("legacy facts were not persisted: %+v", store.latest)
				}
				slot := store.latest.Slots[0]
				if scenario == "current nested UID" {
					if slot.State != "occupied" || slot.InstanceID != runtime.instanceID || slot.UserID != 1 || slot.Generation != runtime.generation || slot.GatewayID != gatewayID || slot.InstanceState != "starting" || slot.GatewayState != "error" {
						t.Fatalf("authenticated legacy owner disappeared: %+v", slot)
					}
				} else if slot.State != "unknown" || slot.InstanceID != 0 || slot.UserID != 0 || slot.Generation != 0 || slot.GatewayID != "" {
					t.Fatalf("unscoped legacy response exposed current owner: %+v", slot)
				}
				if slot.IdentityVerified || slot.Error == "" || bindings.releases != 0 {
					t.Fatalf("legacy envelope gained cleanup authority: %+v", slot)
				}
				ports, err := client.reservedGatewayPorts(context.Background(), pod)
				if _, blocked := ports[20000]; err == nil && !blocked {
					t.Fatalf("legacy envelope gained allocation authority: %+v", slot)
				}
				b, _ := json.Marshal(store.latest)
				if strings.Contains(string(b), "DO-NOT-PERSIST") {
					t.Fatalf("raw legacy envelope leaked: %s", b)
				}
			})
		}
	}
}
