package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8sservice "clawreef/internal/services/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type recheckPodRepository struct {
	repository.RuntimePodRepository
	pod          models.RuntimePod
	observations chan models.RuntimePodObservation
	onSave       func()
}

func (r *recheckPodRepository) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	return &r.pod, nil
}
func (r *recheckPodRepository) SaveRuntimeObservation(_ context.Context, _ int64, _ string, o models.RuntimePodObservation) (bool, error) {
	r.observations <- o
	if r.onSave != nil {
		r.onSave()
	}
	return true, nil
}

type recheckLeader func(context.Context) bool

func (f recheckLeader) IsLeader(ctx context.Context) bool { return f(ctx) }

func TestRuntimeSlotRecheckPromptFreshProofAfterQuarantineCAS(t *testing.T) {
	for _, scenario := range []string{"free", "late-assignment", "changed-proof", "changed-uid", "read-error", "cas-conflict", "leadership-lost"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			kube := fake.NewSimpleClientset(auditTestPod())
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			seen := time.Now()
			store := &recheckPodRepository{pod: models.RuntimePod{ID: 1, State: "ready", RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid, LastSeenAt: &seen}, observations: make(chan models.RuntimePodObservation, 8)}
			var reads atomic.Int32
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				read := reads.Add(1)
				if r.Method != http.MethodGet || r.Header.Get("X-ClawManager-Control-Token") != "token" {
					t.Error("recheck bypassed authenticated read")
				}
				snapshot := auditFreeSnapshot()
				if read > 1 {
					switch scenario {
					case "changed-proof":
						snapshot["idle_confirmed"] = false
					case "read-error":
						return nil, errors.New("fresh proof unavailable")
					case "changed-uid":
						p, _ := kube.CoreV1().Pods("runtime").Get(ctx, "pool", metav1.GetOptions{})
						if p == nil {
							p = auditTestPod()
						}
						p.UID = "replacement"
						_, _ = kube.CoreV1().Pods("runtime").Update(ctx, p, metav1.UpdateOptions{})
					}
				}
				data, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})}).(*slotRuntimeAgentClient)
			if err := client.writeSlotAudit(ctx, store.pod, nil, runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: "openclaw", Ports: map[int]string{20000: "old cleanup"}}); err != nil {
				t.Fatal(err)
			}
			kube.PrependReactor("update", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
				cm := action.(k8stesting.UpdateAction).GetObject()
				if cm.(metav1.Object).GetName() != runtimeSlotAuditName(uid) {
					return false, nil, nil
				}
				if scenario == "cas-conflict" {
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, runtimeSlotAuditName(uid), errors.New("newer exclusion"))
				}
				return false, nil, nil
			})
			// A reservation appearing after the first save must be read afresh.
			if scenario == "late-assignment" {
				store.onSave = func() {
					store.onSave = nil
					req, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 2, AgentType: "openclaw", PodUID: uid, GatewayPort: 20000})
					if err := client.assignments.EnsureDesired(ctx, k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 2, RuntimeType: "openclaw", PodUID: uid, Endpoint: endpoint, GatewayID: "gw-7-2", Request: req}); err != nil {
						t.Error(err)
					}
				}
			}
			var elected atomic.Int32
			s := &RuntimeScheduler{podRepo: store, bindingRepo: &auditBindingRepository{}, instanceRepo: &auditInstanceRepository{}, agentClient: client, leader: recheckLeader(func(context.Context) bool {
				n := elected.Add(1)
				return scenario != "leadership-lost" || n == 1
			})}
			s.slotAudit.lastRun = time.Now() // The periodic minute timer must not delay this request.
			s.RequestSlotAudit(1)
			done := make(chan struct{})
			go func() { s.runSlotAuditRequests(ctx); close(done) }()
			first := waitRecheckObservation(t, store.observations)
			if first.Slots[0].State != "unknown" {
				t.Fatalf("CAS-pending scan granted free: %+v", first)
			}
			if scenario == "cas-conflict" || scenario == "leadership-lost" {
				time.Sleep(40 * time.Millisecond)
				if reads.Load() != 1 {
					t.Fatalf("failed CAS/leadership caused follow-up: %d", reads.Load())
				}
			} else if scenario == "changed-uid" {
				deadline := time.After(time.Second)
				for reads.Load() < 2 {
					select {
					case <-deadline:
						t.Fatal("no fresh UID check")
					default:
						time.Sleep(time.Millisecond)
					}
				}
			} else {
				second := waitRecheckObservation(t, store.observations)
				want := "unknown"
				if scenario == "free" {
					want = "free"
				}
				if second.Slots[0].State != want {
					t.Fatalf("fresh scan state=%+v want=%s", second, want)
				}
			}
			cancel()
			<-done
			if reads.Load() > 2 {
				t.Fatalf("recheck spun without new progress: %d", reads.Load())
			}
		})
	}
}

func waitRecheckObservation(t *testing.T, observations <-chan models.RuntimePodObservation) models.RuntimePodObservation {
	t.Helper()
	select {
	case o := <-observations:
		return o
	case <-time.After(2 * time.Second):
		t.Fatal("slot release waited for periodic timer")
		return models.RuntimePodObservation{}
	}
}

func TestRuntimeSlotRecheckCoalescesAndBoundsRequests(t *testing.T) {
	for _, manyPods := range []bool{false, true} {
		t.Run(fmt.Sprint(manyPods), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var checks atomic.Int32
			s := &RuntimeScheduler{leader: recheckLeader(func(context.Context) bool { checks.Add(1); return false })}
			for i := 0; i < 1000; i++ {
				s.RequestSlotAudit(1)
			}
			if manyPods {
				for i := int64(2); i < 1000; i++ {
					s.RequestSlotAudit(i)
				}
			}
			done := make(chan struct{})
			go func() { s.runSlotAuditRequests(ctx); close(done) }()
			time.Sleep(80 * time.Millisecond)
			cancel()
			<-done
			want := int32(1)
			if manyPods {
				want = 64
			}
			if n := checks.Load(); n != want {
				t.Fatalf("requests not bounded/coalesced: got=%d want=%d", n, want)
			}
		})
	}
}
