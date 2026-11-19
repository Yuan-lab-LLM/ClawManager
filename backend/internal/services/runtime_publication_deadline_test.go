package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

type publicationDeadlineInstances struct {
	securitySlotInstances
	binding       *models.InstanceRuntimeBinding
	reads, raceAt int
}

func (s *publicationDeadlineInstances) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	instance, err := s.securityInstances.GetByIDContext(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot := *instance
	s.reads++
	if s.raceAt > 0 && s.reads == s.raceAt {
		s.instance.Status = "running"
	}
	return &snapshot, nil
}

func (s *publicationDeadlineInstances) MarkRuntimePublicationTimeout(ctx context.Context, id, generation int, origin time.Time, message string) (bool, error) {
	code := "RUNTIME_PUBLICATION_TIMEOUT"
	if s.instance.ID != id || s.instance.RuntimeGeneration != generation {
		return false, errors.New("generation changed")
	}
	if s.instance.Status != "creating" || s.binding.CreatedAt != origin {
		return false, nil
	}
	s.instance.Status = "error"
	s.instance.RuntimeErrorCode = &code
	s.instance.RuntimeErrorMessage = &message
	return true, nil
}

func (s *publicationDeadlineInstances) RuntimePublicationOrigin(context.Context, int, int) (time.Time, error) {
	return s.binding.CreatedAt, nil
}

type publicationDeadlineSchedulerRepo struct {
	*fakeRuntimeInstanceRepo
	instance *models.Instance
	race     bool
}

func (r *publicationDeadlineSchedulerRepo) MarkRuntimePublicationTimeout(ctx context.Context, id, generation int, origin time.Time, message string) (bool, error) {
	if r.race {
		r.instance.Status = "running"
	}
	if r.instance.Status != "creating" || r.instance.RuntimeGeneration != generation || r.instance.CreatedAt != origin {
		return false, nil
	}
	return true, r.UpdateRuntimeState(ctx, id, "error", generation, &message)
}

func TestDirectG1PublishedCreateExpiresWithoutManualStartOrNorthbound(t *testing.T) {
	for _, reconciler := range []string{"scheduler", "assignment", "caught-up", "create-race", "capacity-wait"} {
		t.Run(reconciler, func(t *testing.T) {
			f := newPendingSlotReplayFixture(t)
			f.instance.RuntimeGeneration = 1
			f.binding.Generation = 1
			f.binding.GatewayID = "pending-7-1"
			f.instance.CreatedAt = time.Now().Add(-6 * time.Minute)
			original := f.instance.CreatedAt
			f.binding.CreatedAt = original
			if reconciler == "capacity-wait" {
				f.binding.CreatedAt = time.Now()
			}
			renewed := time.Now()
			f.instance.StartedAt = &renewed
			f.instance.UpdatedAt = renewed
			f.binding.UpdatedAt = renewed
			f.instance.RuntimeSecurityProfile = RuntimeSecurityProfile
			pod, err := f.kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			pod.Labels["clawmanager.io/runtime-security"] = RuntimeSecurityProfile
			pod.Status.Phase = corev1.PodRunning
			if _, err = f.kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			f.kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				cm := action.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap)
				cm.UID, cm.ResourceVersion = "assignment-uid", "10"
				return false, nil, nil
			})
			// Model direct API G1 SQL + published protected assignment, with no
			// model-activity/start_deadline row or Northbound operation.
			source := &publicationDeadlineInstances{securitySlotInstances: securitySlotInstances{securityInstances: securityInstances{instance: &f.instance}}, binding: f.binding}
			a, err := NewRuntimeSecurityAuthority(f.kube, "runtime", source, []byte(strings.Repeat("k", 32)))
			if err != nil {
				t.Fatal(err)
			}
			req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: RuntimeTypeOpenClaw, PodUID: "pod-uid", WorkspacePath: f.binding.WorkspacePath, GatewayPort: 20003, PortRange: RuntimeAgentPortRange{Start: f.scheduler.gatewayPortStart, End: f.scheduler.gatewayPortEnd}, SecurityProfile: RuntimeSecurityProfile, Environment: map[string]string{"BUSINESS_TOKEN": "original-issued-once"}}
			req, err = prepareIsolatedRuntimeEnvironment(req, a.nativeCredential(runtimeauth.Identity{InstanceID: 7, UserID: 2, Generation: 1, PodUID: "pod-uid"}))
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(req)
			if err := f.client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 1, RuntimeType: RuntimeTypeOpenClaw, Endpoint: *f.pod.AgentEndpoint, PodUID: "pod-uid", GatewayID: "gw-7-1", Request: raw, Environment: req.Environment}); err != nil {
				t.Fatal(err)
			}
			cm, err := f.kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ready := reconciler == "caught-up" || reconciler == "capacity-wait"
			reads := 0
			a.assignments = securityAssignmentFunc(func(ctx context.Context, _ int) (*corev1.ConfigMap, []corev1.Pod, error) {
				reads++
				if !ready {
					return nil, nil, unavailableInventoryError()
				}
				return cm, []corev1.Pod{*pod}, nil
			})
			if err := ConfigureSlotRuntimeSecurity(f.client, a); err != nil {
				t.Fatal(err)
			}
			secretBefore, err := f.kube.CoreV1().Secrets("runtime").Get(context.Background(), "lite-instance-7-g1", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if reconciler == "capacity-wait" {
				if err := f.client.ReconcileAssignments(context.Background()); err != nil {
					t.Fatalf("first reservation after capacity wait could not start: %v", err)
				}
				if f.instance.Status != "creating" || len(f.requests) != 1 || f.bindings.bindings[7] != f.binding {
					t.Fatal("capacity wait prevented the published reservation from reaching its agent")
				}
				if _, err := a.CurrentIdentity(context.Background(), f.instance.ID); err != nil {
					t.Fatalf("capacity wait prevented the agent from obtaining its current identity: %v", err)
				}
				return
			}
			if reconciler == "create-race" {
				source.raceAt = 3
				start := &runtimeGatewayStart{endpoint: *f.pod.AgentEndpoint, workspacePath: f.binding.WorkspacePath, environment: req.Environment, reservedBinding: f.binding}
				err = f.scheduler.createGatewayOnPod(context.Background(), f.instance, RuntimeTypeOpenClaw, *f.pod, start)
				repo := f.scheduler.instanceRepo.(*fakeRuntimeInstanceRepo)
				if !errors.Is(err, errRuntimeGatewayStartPending) || f.instance.Status != "running" || f.instance.RuntimeErrorCode != nil || len(repo.runtimeStates) != 0 || len(f.requests) != 0 || f.bindings.bindings[7] != f.binding || f.pods.releases[9] != 0 || f.bindings.deleteAndReleaseCalls[7] != 0 || f.bindings.deleteCalls[7] != 0 {
					t.Fatalf("lost timeout CAS changed running or released ownership: err=%v status=%s writes=%v", err, f.instance.Status, repo.runtimeStates)
				}
				return
			}
			if reconciler == "scheduler" {
				repo := newFakeRuntimeInstanceRepo()
				f.scheduler.instanceRepo = &publicationDeadlineSchedulerRepo{fakeRuntimeInstanceRepo: repo, instance: &f.instance}
				_ = f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
				state, ok := repo.runtimeStates[7]
				if !ok || state.status != "error" || state.message == nil || !strings.Contains(*state.message, "manual inspection") {
					t.Fatalf("expiry not visible: %+v", state)
				}
			} else {
				err = f.client.ReconcileAssignments(context.Background())
				if err == nil || runtimeFailureCode(err) != "RUNTIME_PUBLICATION_TIMEOUT" || source.instance.Status != "error" || source.instance.RuntimeErrorCode == nil || *source.instance.RuntimeErrorCode != "RUNTIME_PUBLICATION_TIMEOUT" {
					t.Fatalf("assignment expiry not visible: err=%v instance=%+v", err, source.instance)
				}
			}
			// Neither observed catch-up nor renewed mutable timestamps grants a start.
			ready = true
			_ = f.client.ReconcileAssignments(context.Background())
			if len(f.requests) != 0 || reads != 0 || f.envCalls != 0 || f.instance.CreatedAt != original || f.instance.RuntimeGeneration != 1 || f.bindings.bindings[7] != f.binding || f.pods.claims[9] != 0 || f.pods.releases[9] != 0 || f.bindings.deleteCalls[7] != 0 || f.bindings.deleteAndReleaseCalls[7] != 0 {
				t.Fatal("expired ownership or original deadline changed, or late Agent POST occurred")
			}
			secretAfter, err := f.kube.CoreV1().Secrets("runtime").Get(context.Background(), "lite-instance-7-g1", metav1.GetOptions{})
			if err != nil || !reflect.DeepEqual(secretBefore, secretAfter) {
				t.Fatal("expiry regenerated Secret")
			}
		})
	}
}

func TestPublicationDeadlinePreservesKnownRunningBinding(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.instance.CreatedAt = time.Now().Add(-time.Hour)
	f.instance.RuntimeGeneration = 1
	f.instance.RuntimeSecurityProfile = RuntimeSecurityProfile
	f.binding.Generation = 1
	f.binding.GatewayID = "gw-7-1"
	f.binding.State = "running"
	f.client.security = &RuntimeSecurityAuthority{assignments: securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		t.Fatal("running binding read inventory")
		return nil, nil, nil
	})}
	repo := newFakeRuntimeInstanceRepo()
	f.scheduler.instanceRepo = repo
	if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) != 0 {
		t.Fatal(errs)
	}
	if state := repo.runtimeStates[7]; state.status != "running" {
		t.Fatalf("known running binding expired instead of syncing: %+v", state)
	}
	if len(f.requests) != 0 || f.pods.releases[9] != 0 || f.bindings.bindings[7] != f.binding {
		t.Fatal("running ownership changed")
	}
}

func TestPublicationDeadlineExemptsTimelyRunningAndLaterStarts(t *testing.T) {
	for _, scenario := range []string{"timely", "running", "later-generation", "not-published"} {
		t.Run(scenario, func(t *testing.T) {
			instance := models.Instance{RuntimeGeneration: 1, RuntimeSecurityProfile: RuntimeSecurityProfile, Status: "creating", CreatedAt: time.Now().Add(-time.Hour)}
			switch scenario {
			case "timely":
				instance.CreatedAt = time.Now().Add(-time.Minute)
			case "running":
				instance.Status = "running"
			case "later-generation":
				instance.RuntimeGeneration = 2
			case "not-published":
				instance.RuntimeSecurityProfile = ""
			}
			if err := runtimePublicationCreateTimeout(&instance, instance.CreatedAt); err != nil {
				t.Fatalf("valid startup expired: %v", err)
			}
		})
	}
}

func TestPublicationTimeoutPreservesOwnershipAcrossCreateCallers(t *testing.T) {
	instance := models.Instance{Status: "creating", RuntimeGeneration: 1, RuntimeSecurityProfile: RuntimeSecurityProfile, CreatedAt: time.Now().Add(-time.Hour)}
	timeout := runtimePublicationCreateTimeout(&instance, instance.CreatedAt)
	if timeout == nil || runtimeFailureCode(timeout) != "RUNTIME_PUBLICATION_TIMEOUT" || !runtimeGatewayCreateOutcomeUncertain(timeout) {
		t.Fatalf("timeout could reach terminal create cleanup: %v", timeout)
	}
}

func TestPublicationSchedulerTimeoutCASDoesNotOverwriteRunning(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.instance.RuntimeGeneration = 1
	f.instance.CreatedAt = time.Now().Add(-time.Hour)
	f.instance.RuntimeSecurityProfile = RuntimeSecurityProfile
	f.binding.Generation = 1
	f.binding.GatewayID = "pending-7-1"
	f.binding.CreatedAt = f.instance.CreatedAt
	f.client.security = &RuntimeSecurityAuthority{assignments: securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
		t.Fatal("expiry read inventory")
		return nil, nil, nil
	})}
	repo := newFakeRuntimeInstanceRepo()
	f.scheduler.instanceRepo = &publicationDeadlineSchedulerRepo{fakeRuntimeInstanceRepo: repo, instance: &f.instance, race: true}
	_ = f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
	if f.instance.Status != "running" || len(repo.runtimeStates) != 0 || f.bindings.bindings[7] != f.binding || f.pods.releases[9] != 0 || len(f.requests) != 0 {
		t.Fatal("lost scheduler timeout CAS overwrote running or released ownership")
	}
}
