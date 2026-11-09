package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"testing"
	"time"
)

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
