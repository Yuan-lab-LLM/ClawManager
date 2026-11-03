package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type protectedRetryInstances struct {
	*fakeRuntimeInstanceRepo
	resets int
	reject bool
}

func (r *protectedRetryInstances) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.GetByID(id)
}
func (r *protectedRetryInstances) ProtectRuntimeGeneration(ctx context.Context, id, gen int, profile string) error {
	p, _ := r.GetByID(id)
	if p == nil || p.RuntimeGeneration != gen || p.Status != "creating" {
		return errors.New("invalid creating protection")
	}
	p.RuntimeSecurityProfile = profile
	return nil
}
func (r *protectedRetryInstances) RetryRuntimeSchedulingError(ctx context.Context, want models.Instance) (bool, error) {
	r.resets++
	if r.reject {
		return false, nil
	}
	p, _ := r.GetByID(want.ID)
	if p == nil || p.Status != "error" || p.RuntimeGeneration != want.RuntimeGeneration {
		return false, nil
	}
	p.Status = "creating"
	p.RuntimeErrorCode = nil
	p.RuntimeErrorMessage = nil
	return true, nil
}
func TestProtectedSchedulerRestoresDurableCreatingBeforeRealSlotRetry(t *testing.T) {
	for _, scenario := range []string{"recover", "cas-lost", "pending-binding", "permanent-error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			a, _, kube, cm, pod := securityFixture(t)
			if err := kube.CoreV1().ConfigMaps("test").Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			path := "/workspaces/opencode/user-3/instance-7"
			code, msg := "RUNTIME_POOL_UNAVAILABLE", "no schedulable opencode runtime pod"
			instance := models.Instance{ID: 7, UserID: 3, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", Status: "error", RuntimeGeneration: 2, WorkspacePath: &path, RuntimeErrorCode: &code, RuntimeErrorMessage: &msg, UpdatedAt: time.Now().Add(-time.Minute)}
			repo := &protectedRetryInstances{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo(), reject: scenario == "cas-lost"}
			if scenario == "permanent-error" {
				code = "USER_CONFIGURATION_INVALID"
				msg = "invalid user config"
			}
			repo.byID[7] = &instance
			repo.desiredRunning = []models.Instance{instance}
			a.instances = repo
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/gateways") {
					calls++
				}
				if repo.byID[7].Status != "creating" {
					t.Error("native launch before durable creating")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-2","gateway_port":20000,"status":"running"}`))}, nil
			})}).(*slotRuntimeAgentClient)
			client.security = a
			endpoint, uid := "https://"+pod.Status.PodIP+":19090", string(pod.UID)
			now := time.Now().UTC()
			p := models.RuntimePod{ID: 9, RuntimeType: "opencode", AgentEndpoint: &endpoint, PodUID: &uid, PodIP: &pod.Status.PodIP, Namespace: "test", PodName: pod.Name, DeploymentName: "pool", ImageRef: "registry/opencode:test", LastSeenAt: &now, State: "ready", Capacity: 2}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &p}, schedulable: []models.RuntimePod{p}}
			bindings := newFakeRuntimeBindingRepo()
			if scenario == "pending-binding" {
				bindings.bindings[7] = &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, RuntimeType: "opencode", State: RuntimeGatewayBindingCreating, GatewayID: "pending-7-2", Generation: 2, GatewayPort: 20000, WorkspacePath: path}
			}
			s := NewRuntimeScheduler(repo, pods, bindings, &fakeRuntimeRolloutRepo{}, client, &fakeRuntimeEventService{}, nil, &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{Namespace: "test", PodName: pod.Name, PodUID: uid, PodIP: p.PodIP, DeploymentName: p.DeploymentName, ImageRef: p.ImageRef, RuntimeType: "opencode", State: "ready"}}}, time.Second, WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
			s.runtimeNamespace = "test"
			if err := s.reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if scenario == "recover" {
				if calls != 1 || repo.resets != 1 {
					t.Fatalf("protected retry never reached actual slot client: resets=%d native_calls=%d", repo.resets, calls)
				}
				if _, err := kube.CoreV1().ConfigMaps("test").Get(ctx, "lite-instance-7", metav1.GetOptions{}); err != nil {
					t.Fatal("retry failed to publish current assignment")
				}
			} else if calls != 0 || repo.byID[7].Status != "error" {
				t.Fatal("lost CAS, pending placement or permanent error was revived")
			}
			if (scenario == "pending-binding" || scenario == "permanent-error") && repo.resets != 0 {
				t.Fatal("retry CAS attempted before no-binding/recoverable proof")
			}
		})
	}
}
