package services

import (
	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Optional interface keeps existing adapters/stubs compatible. The production
// engine implements it for all northbound member read paths.
type TeamMemberObserver interface {
	ObserveTeamMembers(context.Context, []models.Team, []models.TeamMember) []models.TeamMember
}

type teamObservationPods struct {
	at   time.Time
	pods []k8s.RuntimeDeploymentPod
}
type teamRuntimeFactsReader interface {
	TeamMemberRuntimeFacts(context.Context, []int) (map[int]models.TeamMemberRuntimeFacts, error)
}

func observationFresh(t *time.Time, now time.Time, max time.Duration) bool {
	return t != nil && !t.IsZero() && now.Sub(*t) <= max && t.Sub(now) <= time.Minute
}

// Presence describes liveness only. Assignment IDs, availability and outcome
// fields from old status records must never overwrite the current work ledger.
func projectTeamMemberObservation(member models.TeamMember, instance models.Instance, gateway models.TeamMemberRuntimeObservation, raw string, now time.Time) models.TeamMember {
	obs := gateway
	if gateway.State == "online" {
		obs.State, obs.Reason, obs.ObservedAt = "unknown", "collaboration_unconfirmed", nil
		var presence struct {
			TeamID            json.RawMessage `json:"teamId"`
			MemberID          string          `json:"memberId"`
			Liveness          string          `json:"liveness"`
			LastSeenAt        time.Time       `json:"lastSeenAt"`
			Generation        *int            `json:"generation"`
			RuntimeGeneration *int            `json:"runtimeGeneration"`
		}
		if len(raw) <= 65536 && json.Unmarshal([]byte(raw), &presence) == nil {
			teamID := strings.Trim(string(presence.TeamID), `"`)
			start := instance.CreatedAt
			if instance.StartedAt != nil && instance.StartedAt.After(start) {
				start = *instance.StartedAt
			}
			gen := presence.Generation
			if gen == nil {
				gen = presence.RuntimeGeneration
			}
			if teamID == strconv.Itoa(member.TeamID) && presence.MemberID == member.MemberKey && observationFresh(&presence.LastSeenAt, now, 2*time.Minute) && !presence.LastSeenAt.Before(start) && (gen == nil || *gen == instance.RuntimeGeneration) {
				if presence.Liveness == "online" {
					obs.State, obs.Reason, obs.ObservedAt = "online", "", &presence.LastSeenAt
				} else if presence.Liveness == "offline" {
					obs.State, obs.Reason, obs.ObservedAt = "offline", "collaboration_offline", &presence.LastSeenAt
				}
			}
		}
		// Missing/invalid collaboration proof is a bounded confirmation state,
		// not an infinite startup spinner and never proof a model task failed.
		if obs.State == "unknown" && !instance.CreatedAt.IsZero() {
			start := instance.CreatedAt
			if instance.StartedAt != nil && instance.StartedAt.After(start) {
				start = *instance.StartedAt
			}
			if now.Sub(start) > 3*time.Minute {
				obs.Reason = "collaboration_heartbeat_missing"
			}
		}
	}
	member.RuntimeObservation = &obs
	businessBlock := member.BlockedReason != nil && strings.TrimSpace(*member.BlockedReason) != "" && !strings.HasPrefix(strings.TrimSpace(*member.BlockedReason), teamRuntimeUnavailableReasonPrefix)
	if businessBlock || member.Status == models.TeamMemberStatusFailed {
		member.Availability = "blocked"
		return member
	}
	switch obs.State {
	case "online":
		member.Availability = "idle"
		if member.CurrentTaskID != nil || member.Status == models.TeamMemberStatusBusy {
			member.Availability = "busy"
		}
		if member.BlockedReason != nil && strings.HasPrefix(strings.TrimSpace(*member.BlockedReason), teamRuntimeUnavailableReasonPrefix) {
			member.BlockedReason = nil
		}
	case "offline":
		member.Availability = "offline"
	default:
		member.Availability = "unknown"
	}
	return member
}

func (s *teamService) observationLivePods(ctx context.Context, pod *models.RuntimePod) ([]k8s.RuntimeDeploymentPod, error) {
	key := pod.Namespace + "/" + pod.DeploymentName
	s.observationMu.Lock()
	cached, ok := s.observationPods[key]
	s.observationMu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Second {
		return cached.pods, nil
	}
	if s.pvcService == nil || s.pvcService.GetClient() == nil {
		return nil, fmt.Errorf("runtime inventory unavailable")
	}
	service := k8s.NewRuntimeDeploymentService(s.pvcService.GetClient().Clientset)
	pods, err := service.ListDeploymentPods(ctx, []k8s.RuntimeDeploymentRef{{Namespace: pod.Namespace, Name: pod.DeploymentName}})
	if err != nil {
		return nil, err
	}
	s.observationMu.Lock()
	if s.observationPods == nil {
		s.observationPods = map[string]teamObservationPods{}
	}
	for k, v := range s.observationPods {
		if time.Since(v.at) > time.Minute {
			delete(s.observationPods, k)
		}
	}
	s.observationPods[key] = teamObservationPods{time.Now(), pods}
	s.observationMu.Unlock()
	return pods, nil
}

func (s *teamService) observeMemberGateway(ctx context.Context, f models.TeamMemberRuntimeFacts, now time.Time) models.TeamMemberRuntimeObservation {
	i := f.Instance
	result := models.TeamMemberRuntimeObservation{State: "unknown", Reason: "runtime_unconfirmed"}
	if teamMemberInstanceStarting(i.Status) {
		result.State, result.Reason = "starting", "runtime_starting"
		return result
	}
	if i.Status != "running" {
		result.State, result.Reason = "offline", "runtime_unavailable"
		return result
	}
	if _, lite := v2RuntimeTypeForInstance(&i); !lite {
		// Pro has one owned instance Pod; the existing service already resolves
		// its namespace/name without trusting a member's supplied metadata.
		if s.podService == nil {
			return result
		}
		p, err := s.podService.GetPod(ctx, i.UserID, i.ID)
		if err == nil && p != nil && p.DeletionTimestamp == nil && isPodReady(p) {
			result.State, result.Reason = "online", ""
		}
		return result
	}
	b, p := f.Binding, f.Runtime
	runtimeType, _ := v2RuntimeTypeForInstance(&i)
	if b == nil || b.RuntimeType != runtimeType || b.Generation != i.RuntimeGeneration || b.State != "running" || b.GatewayID == "" || b.GatewayPort <= 0 || p == nil || p.RuntimeType != runtimeType || p.State != "ready" || !observationFresh(p.LastSeenAt, now, heartbeat.Timeout) || !observationFresh(b.LastHealthAt, now, heartbeat.Timeout) {
		return result
	}
	live, err := s.observationLivePods(ctx, p)
	if err != nil {
		return result
	}
	for _, actual := range live {
		if actual.State == "ready" && runtimeRolloutAgentMatchesPod(*p, actual) && runtimePodMatchesImage(*p, actual.ImageRef) {
			result.State, result.Reason, result.ObservedAt = "online", "", b.LastHealthAt
			return result
		}
		if actual.PodName == p.PodName && actual.State == "failed" && (p.PodUID == nil || *p.PodUID == actual.PodUID) {
			result.State, result.Reason = "offline", "runtime_unavailable"
			return result
		}
	}
	return result
}

func (s *teamService) ObserveTeamMembers(ctx context.Context, teams []models.Team, members []models.TeamMember) []models.TeamMember {
	result := append([]models.TeamMember(nil), members...)
	if len(result) == 0 {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	byTeam := map[int]models.Team{}
	var ids []int
	for _, team := range teams {
		byTeam[team.ID] = team
	}
	for _, m := range members {
		if m.InstanceID != nil {
			ids = append(ids, *m.InstanceID)
		}
	}
	facts := map[int]models.TeamMemberRuntimeFacts{}
	if reader, ok := s.repo.(teamRuntimeFactsReader); ok {
		if loaded, err := reader.TeamMemberRuntimeFacts(ctx, ids); err == nil {
			facts = loaded
		}
	}
	// HMGET reads only the authorized members and never scans Redis or returns
	// internal status text. Runtime/model execution cannot block this path.
	presence := map[int]string{}
	presenceUnavailable := map[int]bool{}
	for _, team := range teams {
		args := []string{"HMGET", teamPresenceKey(team.ID)}
		var indexes []int
		for idx, m := range members {
			if m.TeamID == team.ID && m.UserID == team.UserID {
				args = append(args, m.MemberKey)
				indexes = append(indexes, idx)
			}
		}
		if len(indexes) == 0 || ctx.Err() != nil {
			continue
		}
		bus, err := s.redisBusForTeam(ctx, &team)
		if err != nil {
			presenceUnavailable[team.ID] = true
			continue
		}
		reply, err := bus.do(ctx, args...)
		if err != nil {
			presenceUnavailable[team.ID] = true
			continue
		}
		values, ok := reply.([]interface{})
		if !ok || len(values) != len(indexes) {
			continue
		}
		for n, v := range values {
			if raw, ok := v.(string); ok {
				presence[indexes[n]] = raw
			}
		}
	}
	now := time.Now().UTC()
	for idx, m := range result {
		gateway := models.TeamMemberRuntimeObservation{State: "unknown", Reason: "runtime_unconfirmed"}
		var instance models.Instance
		if team, ok := byTeam[m.TeamID]; ok && team.UserID == m.UserID && m.InstanceID != nil {
			if f, ok := facts[*m.InstanceID]; ok && f.Instance.UserID == team.UserID && (team.IsCM() || stringValue(f.Instance.Owner) == stringValue(team.Owner)) {
				instance = f.Instance
				gateway = s.observeMemberGateway(ctx, f, now)
				// Older plugins omit a generation. Their heartbeat must still be
				// newer than the current binding and Runtime registration.
				start := instance.CreatedAt
				if instance.StartedAt != nil && instance.StartedAt.After(start) {
					start = *instance.StartedAt
				}
				if f.Binding != nil && f.Binding.CreatedAt.After(start) {
					start = f.Binding.CreatedAt
				}
				if f.Runtime != nil && f.Runtime.CreatedAt.After(start) {
					start = f.Runtime.CreatedAt
				}
				instance.StartedAt = &start
				if gateway.State == "online" && presenceUnavailable[team.ID] {
					gateway.State, gateway.Reason = "unknown", "presence_unavailable"
				}
			}
		}
		result[idx] = projectTeamMemberObservation(m, instance, gateway, presence[idx], now)
	}
	return result
}
