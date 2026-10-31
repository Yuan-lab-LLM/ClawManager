package northbound

import (
	"encoding/json"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

func ownerTeamView(team models.Team) map[string]interface{} {
	return map[string]interface{}{"id": team.ID, "name": team.VisibleName(), "description": team.Description, "status": team.Status, "communication_mode": team.CommunicationMode, "resource_scope": models.TeamScopeNorthbound, "shared_mount_path": "/team", "created_at": team.CreatedAt, "updated_at": team.UpdatedAt}
}

func ownerTeamMemberViews(members []models.TeamMember) []map[string]interface{} {
	views := make([]map[string]interface{}, 0, len(members))
	for _, m := range members {
		if m.Status == models.TeamMemberStatusDeleted {
			continue
		}
		if m.RuntimeObservation == nil && (m.Availability == models.TeamMemberAvailabilityIdle || m.Availability == models.TeamMemberAvailabilityBusy) && (m.LastSeenAt == nil || time.Since(*m.LastSeenAt) > 2*time.Minute || time.Until(*m.LastSeenAt) > time.Minute) {
			m.Availability = models.TeamMemberAvailabilityUnknown
		}
		view := map[string]interface{}{"id": m.ID, "team_id": m.TeamID, "instance_id": m.InstanceID, "member_key": m.MemberKey, "display_name": m.DisplayName, "icon_key": m.IconKey, "role": m.Role, "runtime_type": m.RuntimeType, "instance_mode": m.InstanceMode, "description": m.Description, "status": m.Status, "current_task_id": m.CurrentTaskID, "progress": m.Progress, "last_seen_at": m.LastSeenAt, "availability": m.Availability, "runtime_status": m.RuntimeStatus, "runtime_task_id": m.RuntimeTaskID, "runtime_intent": m.RuntimeIntent, "last_summary": m.LastSummary, "created_at": m.CreatedAt, "updated_at": m.UpdatedAt}
		if m.RuntimeObservation != nil {
			view["runtime_observation"] = m.RuntimeObservation
		}
		if m.BlockedReason != nil && strings.TrimSpace(*m.BlockedReason) != "" && !(m.RuntimeObservation != nil && m.RuntimeObservation.State != "offline" && strings.HasPrefix(strings.TrimSpace(*m.BlockedReason), "Runtime unavailable: ")) {
			view["blocked_reason"] = "成员暂不可用，请查看实例状态"
		}
		views = append(views, view)
	}
	return views
}

func ownerTeamSummary(team models.Team, members []models.TeamMember) map[string]interface{} {
	view := ownerTeamView(team)
	view["members"] = ownerTeamMemberViews(members)
	available, total, confirming, faults := 0, 0, 0, 0
	for _, m := range members {
		if m.Status == models.TeamMemberStatusDeleted {
			continue
		}
		total++
		if m.RuntimeObservation != nil {
			if m.RuntimeObservation.State == "online" && (m.Availability == "idle" || m.Availability == "busy") && m.Status != "failed" && m.Status != "deleting" {
				available++
			} else if m.RuntimeObservation.State == "offline" || m.RuntimeObservation.Reason == "collaboration_heartbeat_missing" || m.Availability == "blocked" || m.Status == "failed" {
				faults++
			} else {
				confirming++
			}
		} else if (m.Availability == models.TeamMemberAvailabilityIdle || m.Availability == models.TeamMemberAvailabilityBusy) && m.LastSeenAt != nil && time.Since(*m.LastSeenAt) <= 2*time.Minute && time.Until(*m.LastSeenAt) <= time.Minute && m.Status != models.TeamMemberStatusFailed && m.Status != models.TeamMemberStatusCreating {
			available++
		} else if m.Availability == "blocked" || m.Availability == "offline" || m.Status == "failed" {
			faults++
		} else {
			confirming++
		}
	}
	view["member_count"], view["available_count"] = total, available
	view["confirming_count"], view["fault_count"] = confirming, faults
	return view
}

func ownerTeamDetails(details *services.TeamDetailsPayload) map[string]interface{} {
	var before *int
	for _, e := range details.Events {
		if before == nil || e.ID < *before {
			id := e.ID
			before = &id
		}
	}
	if details.EventsNextBeforeID != nil {
		before = details.EventsNextBeforeID
	}
	return map[string]interface{}{"team": ownerTeamView(*details.Team), "leader_member_id": details.LeaderMemberID, "members": ownerTeamMemberViews(details.Members), "tasks": safeOwnerTeamValue(details.Tasks), "events": ownerTeamVisibleEvents(details.Events), "events_has_more": details.EventsHasMore || len(details.Events) >= 200, "events_next_before_id": before, "work_items": safeOwnerTeamValue(details.WorkItems)}
}

func ownerTeamVisibleEvents(events []services.TeamEventPayload) []interface{} {
	result := []interface{}{}
	for _, event := range events {
		policy, _ := event.Payload["chatPolicy"].(string)
		if policy == "" {
			policy, _ = event.Payload["chat_policy"].(string)
		}
		if policy == "hidden" || policy == "digest" {
			continue
		}
		privateChannel := false
		for _, key := range []string{"channel", "content_type", "contentType"} {
			value, _ := event.Payload[key].(string)
			switch strings.ToLower(value) {
			case "analysis", "thinking", "reasoning", "tool_log", "control":
				privateChannel = true
			}
		}
		if privateChannel {
			continue
		}
		if visible, ok := event.Payload["visibleToChat"].(bool); ok && !visible {
			continue
		}
		if visible, ok := event.Payload["visible_to_chat"].(bool); ok && !visible {
			continue
		}
		kind, _ := event.Payload["eventKind"].(string)
		if kind == "" {
			kind, _ = event.Payload["event_kind"].(string)
		}
		hidden := false
		for _, value := range []string{event.EventType, kind} {
			switch strings.ToLower(value) {
			case "thinking", "reasoning", "tool_log", "runtime_log", "assignment_heartbeat", "assignment_check_requested", "assignment_check_result", "heartbeat", "presence", "member_presence", "execution_control", "monitor_control":
				hidden = true
			}
		}
		if hidden {
			continue
		}
		// Unknown Runtime diagnostics are not a public event contract. Preserve
		// established business events and explicitly projected visible updates.
		known := false
		for _, value := range []string{event.EventType, kind} {
			switch value {
			case "inbound", "outbound", "reply", "peer_reply", "task_assigned", "task_received", "task_started", "task_progress", "progress", "completion", "completion_proposed", "completion_accepted", "completion_rejected", "task_completed", "task_failed", "message_failed", "message_warning", "task_stale", "member_result_confirmed", "member_result_updated", "leader_plan", "leader_progress", "worker_plan", "worker_progress", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder", "completion_candidate", "completion_deferred", "completion_validation_warning", "assignment_recovery_started", "assignment_reissued", "assignment_recovery_exhausted", "artifact_changed", "workflow_plan", "workflow_updated", "phase_changed", "bootstrap_dispatch_failed", "runtime_instance_unavailable":
				known = true
			}
		}
		if !known && policy != "visible" && policy != "replaceable" && policy != "warning" {
			continue
		}
		result = append(result, safeOwnerTeamValue(event))
	}
	return result
}

// Only fields already used for business presentation cross the northbound
// boundary. Raw metadata, diagnostic payloads and control envelopes do not.
var ownerTeamBusinessFields = func() map[string]bool {
	fields := strings.Fields(`id team_id task_id member_id root_task_id rootTaskId message_id messageId target_member_id targetMemberId created_by status workflow_state plan_version ledger_version active_phase_id completion_policy plan_sealed_at accepted_completion_event_id accepted_completion_id title name summary text prompt goal instruction resultMarkdown result_markdown markdown content result payload event_type eventKind event_kind event_id eventId stream_id occurred_at created_at updated_at dispatched_at started_at finished_at progress intent execution_type context_task_id error_code role member_key display_name runtime_type instance_mode work_id workId assignment_id assignmentId revision phase_id phaseId phase_index phase_status phase_title owner_member_id ownerMemberId required review_required reviewed_revision review_status review_decision depends_on dependsOn dependencies latest_member_result_event_id result_ref resultRef result_event_id artifact_refs artifactRefs artifacts path relativePath filename size mime_type kind type answer finalAnswer final_answer delivery_mode workflowState planVersion ledgerVersion activePhaseId phaseIndex phaseTitle phaseStatus work_items workItems phases tasks events has_more next_before_id accepted final user_visible visible visibleToChat visible_to_chat chatPolicy chat_policy target user from to event state decision reason reviewer_member_id reviewerMemberId validation_assignment validationAssignment validation_target_assignment_id validationTargetAssignmentId validation_target_revision validationTargetRevision remaining_actions remainingActions workflow_sealed final_answer_ready finalAnswerReady`)
	result := map[string]bool{}
	result["user_key"], result["user_label"] = true, true
	for _, field := range strings.Fields("executionType executorMemberKey contextTaskId") {
		result[field] = true
	}
	for _, field := range strings.Fields("teamId taskId memberId displayName rootMessageId root_message_id inReplyTo in_reply_to collaborationStep collaboration_step actor recipient sourceMemberId source_member_id eventType detail memberTerminalOnly member_terminal_only assignmentResultOnly assignment_result_only leaderMediatedRouteViolation leader_mediated_route_violation rootTaskTerminal root_task_terminal explicitCompletion completionId completionSource backendGenerated acceptedResultRef source_event_id review_target_assignment_id review_target_revision total current_phase_id output_refs") {
		result[field] = true
	}
	for _, field := range strings.Fields("required_for_root superseded_by canonical_work_id validated_revision completion_id sequence_no redis_stream_id artifactChanged artifact_changed responseLocale response_locale workflow_mode leader_member_id") {
		result[field] = true
	}
	for _, field := range fields {
		result[field] = true
	}
	return result
}()

func safeOwnerTeamValue(value interface{}) interface{} {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded interface{}
	if json.Unmarshal(raw, &decoded) != nil {
		return nil
	}
	var clean func(interface{}, int) interface{}
	clean = func(v interface{}, depth int) interface{} {
		if depth > 16 {
			return nil
		}
		switch value := v.(type) {
		case map[string]interface{}:
			result := map[string]interface{}{}
			for k, item := range value {
				if ownerTeamBusinessFields[k] {
					result[k] = clean(item, depth+1)
				}
			}
			return result
		case []interface{}:
			result := make([]interface{}, 0, len(value))
			for _, item := range value {
				result = append(result, clean(item, depth+1))
			}
			return result
		default:
			return v
		}
	}
	return clean(decoded, 0)
}
