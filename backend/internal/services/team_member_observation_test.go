package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTeamObservationInventoriesDoNotStarveLaterMembers(t *testing.T) {
	var calls atomic.Int32
	allStarted := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/deployments/") {
			if calls.Add(1) == 4 {
				once.Do(func() { close(allStarted) })
			}
			select {
			case <-allStarted:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"pool","namespace":"tenant"},"spec":{"selector":{"matchLabels":{"app":"pool"}}}}`)
		} else {
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[]}`)
		}
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := NewTeamService(observedFactsRepo{}, nil, WithTeamKubernetesClient(&k8s.Client{Namespace: "tenant", Clientset: client})).(*teamService)
	facts := map[int]models.TeamMemberRuntimeFacts{}
	for i := 0; i < 6; i++ {
		facts[i] = models.TeamMemberRuntimeFacts{Runtime: &models.RuntimePod{Namespace: "tenant", DeploymentName: fmt.Sprintf("pool-%d", i%4)}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := s.observeInventories(ctx, facts)
	if len(result) != 4 || calls.Load() != 4 {
		t.Fatalf("inventory not deduplicated: %d %d", len(result), calls.Load())
	}
	for key, read := range result {
		if read.err != nil {
			t.Fatalf("later member starved at %s: %v", key, read.err)
		}
	}
}

func TestTeamObservationRecentInventoryFailureDoesNotInventAnOutage(t *testing.T) {
	client := &k8s.Client{Namespace: "tenant", Clientset: fake.NewSimpleClientset()}
	client.Clientset.(*fake.Clientset).PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("temporary API failure")
	})
	s := NewTeamService(observedFactsRepo{}, nil, WithTeamKubernetesClient(client)).(*teamService)
	report, live := ordinaryPlacementFixture()
	key := report.Namespace + "/" + report.DeploymentName
	s.observationPods = map[string]teamObservationPods{key: {at: time.Now().Add(-11 * time.Second), pods: []k8s.RuntimeDeploymentPod{live}}}
	snapshot, err := s.observationLivePods(context.Background(), &report)
	if err != nil || !snapshot.delayed || len(snapshot.pods) != 1 {
		t.Fatalf("recent confirmed inventory lost: %+v %v", snapshot, err)
	}
	s.observationPods[key] = teamObservationPods{at: time.Now().Add(-61 * time.Second), pods: []k8s.RuntimeDeploymentPod{live}}
	if _, err = s.observationLivePods(context.Background(), &report); err == nil {
		t.Fatal("expired inventory hid an observation failure")
	}
}

func TestTeamObservationDelayedInventoryStillRequiresIdentityAndCollaboration(t *testing.T) {
	now := time.Now().UTC()
	report, live := ordinaryPlacementFixture()
	instance := models.Instance{ID: 9, UserID: 1, Status: "running", Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", RuntimeGeneration: 1, CreatedAt: now.Add(-time.Hour)}
	binding := &models.InstanceRuntimeBinding{InstanceID: 9, RuntimePodID: report.ID, RuntimeType: "openclaw", GatewayID: "gw-9-1", GatewayPort: 20000, State: "running", Generation: 1, LastHealthAt: &now}
	f := models.TeamMemberRuntimeFacts{Instance: instance, Binding: binding, Runtime: &report}
	s := NewTeamService(observedFactsRepo{}, nil).(*teamService)
	key := report.Namespace + "/" + report.DeploymentName
	at := now.Add(-15 * time.Second)
	inventory := map[string]teamInventoryRead{key: {snapshot: teamObservationPods{at: at, pods: []k8s.RuntimeDeploymentPod{live}, delayed: true}}}
	obs := s.observeMemberGateway(context.Background(), f, now, inventory)
	if obs.State != "online" || obs.Reason != "runtime_inventory_delayed" {
		t.Fatalf("%+v", obs)
	}
	member := models.TeamMember{ID: 2, TeamID: 7, UserID: 1, MemberKey: "worker", Status: "idle"}
	raw := fmt.Sprintf(`{"teamId":"7","memberId":"worker","liveness":"online","lastSeenAt":%q}`, now.Format(time.RFC3339Nano))
	got := projectTeamMemberObservation(member, instance, obs, raw, now)
	if got.Availability != "idle" || got.RuntimeObservation.Reason != "runtime_inventory_delayed" {
		t.Fatalf("%+v", got)
	}
	if got = projectTeamMemberObservation(member, instance, obs, "", now); got.Availability != "unknown" {
		t.Fatal("cached Pod invented collaboration liveness")
	}
	binding.Generation++
	if got := s.observeMemberGateway(context.Background(), f, now, inventory); got.State == "online" {
		t.Fatal("cached inventory crossed generation")
	}
	binding.Generation--
	report.PodUID = optionalString("replacement-uid")
	if got := s.observeMemberGateway(context.Background(), f, now, inventory); got.State == "online" {
		t.Fatal("cached inventory crossed Pod identity")
	}
}

func TestTeamObservationDoesNotNeedFirstTask(t *testing.T) {
	now := time.Now().UTC()
	for _, name := range []string{"fresh", "quiet", "busy", "business-block", "old-time", "future-time", "old-generation", "wrong-member", "wrong-team", "before-start", "offline", "bad-json", "stale-task"} {
		t.Run(name, func(t *testing.T) {
			member := models.TeamMember{ID: 2, TeamID: 7, UserID: 1, InstanceID: intPtr(9), MemberKey: "worker", Status: "idle", Availability: "unknown"}
			instance := models.Instance{ID: 9, UserID: 1, Status: "running", RuntimeGeneration: 2, CreatedAt: now.Add(-time.Hour), StartedAt: timePtr(now.Add(-time.Minute))}
			stamp := now
			team, key, gen, liveness := "7", "worker", 2, "online"
			if name == "quiet" {
				member.LastSeenAt = timePtr(now.Add(-time.Hour))
				member.Availability = "idle"
			}
			if name == "busy" {
				member.Status = "busy"
				member.CurrentTaskID = intPtr(42)
			}
			if name == "business-block" {
				member.BlockedReason = optionalString("missing approved source")
				member.Availability = "blocked"
			}
			if name == "old-time" {
				stamp = now.Add(-3 * time.Minute)
			}
			if name == "future-time" {
				stamp = now.Add(2 * time.Minute)
			}
			if name == "old-generation" {
				gen = 1
			}
			if name == "wrong-member" {
				key = "other"
			}
			if name == "wrong-team" {
				team = "8"
			}
			if name == "before-start" {
				stamp = now.Add(-2 * time.Minute)
			}
			if name == "offline" {
				liveness = "offline"
			}
			raw := fmt.Sprintf(`{"teamId":%q,"memberId":%q,"generation":%d,"liveness":%q,"lastSeenAt":%q,"availability":"idle","currentTaskId":"old-task-999"}`, team, key, gen, liveness, stamp.Format(time.RFC3339Nano))
			if name == "bad-json" {
				raw = "{"
			}
			before := member
			got := projectTeamMemberObservation(member, instance, models.TeamMemberRuntimeObservation{State: "online", ObservedAt: &now}, raw, now)
			wantOnline := name == "fresh" || name == "quiet" || name == "busy" || name == "business-block" || name == "stale-task"
			if (got.RuntimeObservation.State == "online") != wantOnline {
				t.Fatalf("%s: %+v", name, got)
			}
			if got.LastSeenAt != before.LastSeenAt || got.CurrentTaskID != before.CurrentTaskID || got.Status != before.Status || got.Progress != before.Progress {
				t.Fatal("observation changed ledger/heartbeat")
			}
			if wantOnline && name != "business-block" && got.Availability != map[bool]string{true: "busy", false: "idle"}[name == "busy"] {
				t.Fatalf("bad availability %+v", got)
			}
			if name == "business-block" && (got.Availability != "blocked" || got.BlockedReason != before.BlockedReason) {
				t.Fatal("business blocker cleared")
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestTeamObservationBoundsFirstPresenceConfirmation(t *testing.T) {
	now := time.Now().UTC()
	m := models.TeamMember{ID: 1, TeamID: 7, MemberKey: "worker", Status: "idle", Availability: "unknown"}
	i := models.Instance{ID: 9, Status: "running", CreatedAt: now.Add(-4 * time.Minute)}
	got := projectTeamMemberObservation(m, i, models.TeamMemberRuntimeObservation{State: "online"}, "", now)
	if got.RuntimeObservation.State != "unknown" || got.RuntimeObservation.Reason != "collaboration_heartbeat_missing" || got.Status != "idle" || got.LastSeenAt != nil {
		t.Fatalf("unbounded or fabricated state: %+v", got)
	}
	got = projectTeamMemberObservation(m, i, models.TeamMemberRuntimeObservation{State: "unknown", Reason: "presence_unavailable"}, "", now)
	if got.RuntimeObservation.Reason != "presence_unavailable" || got.Availability != "unknown" {
		t.Fatal("Redis outage became member failure")
	}
}

type observedFactsRepo struct {
	repository.TeamRepository
	facts map[int]models.TeamMemberRuntimeFacts
}

func (r observedFactsRepo) TeamMemberRuntimeFacts(context.Context, []int) (map[int]models.TeamMemberRuntimeFacts, error) {
	return r.facts, nil
}

func TestTeamObservationRedisBeforeAnyTaskAndIsolation(t *testing.T) {
	port := os.Getenv("TEST_OWNER_TEAM_REDIS_PORT")
	if port == "" {
		t.Skip("isolated Redis required")
	}
	t.Setenv("CLAWMANAGER_TEAM_REDIS_URL", "redis://127.0.0.1:"+port+"/14")
	ctx := context.Background()
	bus, _ := newRedisBus(defaultTeamRedisURL())
	teamID := int(time.Now().UnixNano()%1000000000) + 1000
	key := teamPresenceKey(teamID)
	defer bus.Del(ctx, key)
	now := time.Now().UTC()
	report, live := ordinaryPlacementFixture()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: live.PodName, Namespace: live.Namespace, UID: types.UID(live.PodUID), Labels: map[string]string{"app": "pool"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: live.ImageRef}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: *live.PodIP, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "tenant", Labels: map[string]string{"clawmanager.io/runtime-type": "openclaw"}}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pool"}}}}
	client := &k8s.Client{Namespace: "tenant", Clientset: fake.NewSimpleClientset(dep, pod)}
	owner := "a@example.com"
	team := models.Team{ID: teamID, UserID: 1, Owner: &owner, ResourceScope: models.TeamScopeNorthbound}
	instance := models.Instance{ID: 9, UserID: 1, Owner: &owner, Status: "running", Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", RuntimeGeneration: 1, CreatedAt: now.Add(-time.Minute)}
	binding := &models.InstanceRuntimeBinding{InstanceID: 9, RuntimePodID: report.ID, RuntimeType: "openclaw", GatewayID: "gw-9-1", GatewayPort: 20000, State: "running", Generation: 1, LastHealthAt: &now}
	repo := observedFactsRepo{facts: map[int]models.TeamMemberRuntimeFacts{9: {Instance: instance, Binding: binding, Runtime: &report}}}
	s := NewTeamService(repo, nil, WithTeamKubernetesClient(client)).(*teamService)
	member := models.TeamMember{ID: 2, TeamID: team.ID, UserID: 1, InstanceID: intPtr(9), MemberKey: "worker", Status: "idle", Availability: "unknown"}
	write := func(stamp time.Time) {
		raw, _ := json.Marshal(map[string]any{"teamId": fmt.Sprint(team.ID), "memberId": "worker", "liveness": "online", "lastSeenAt": stamp, "currentTaskId": "stale-task"})
		if _, err := bus.do(ctx, "HSET", key, "worker", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	write(now)
	got := s.ObserveTeamMembers(ctx, []models.Team{team}, []models.TeamMember{member})[0]
	if got.RuntimeObservation.State != "online" || got.Availability != "idle" || got.LastSeenAt != nil || got.CurrentTaskID != nil {
		t.Fatalf("first-task dependency remains: %+v", got)
	}
	write(now.Add(-3 * time.Minute))
	got = s.ObserveTeamMembers(ctx, []models.Team{team}, []models.TeamMember{member})[0]
	if got.RuntimeObservation.State != "unknown" {
		t.Fatal("stale Redis status trusted")
	}
	write(time.Now().UTC())
	other := "other@example.com"
	team.Owner = &other
	got = s.ObserveTeamMembers(ctx, []models.Team{team}, []models.TeamMember{member})[0]
	if got.RuntimeObservation.State != "unknown" {
		t.Fatal("cross-owner instance accepted")
	}
}

func TestStoppedTeamMemberWithoutLegacyReasonCannotReuseOldSuccessPresence(t *testing.T) {
	member := models.TeamMember{RuntimeStatus: optionalString("succeeded"), Availability: "idle", BlockedReason: optionalString("runtime_unavailable")}
	result := projectTeamMemberObservation(member, models.Instance{Status: "stopped"}, models.TeamMemberRuntimeObservation{State: "offline", Reason: "runtime_unavailable"}, `{"liveness":"online"}`, time.Now())
	if result.RuntimeStatus == nil || *result.RuntimeStatus != "stopped" || result.Availability != "offline" || result.BlockedReason != nil || result.RuntimeObservation.Reason != "runtime_stopped" {
		t.Fatalf("stopped state not preserved: %+v", result)
	}
}
