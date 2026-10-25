package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const singleMemberExecutionType = "single_member"
const singleMemberWorkID = "single-member"
const singleMemberContextBudget = 12000

func taskExecutionPayload(task *models.TeamTask) map[string]interface{} {
	result := map[string]interface{}{}
	if task != nil {
		_ = json.Unmarshal([]byte(task.PayloadJSON), &result)
	}
	return result
}

func isSingleMemberTask(task *models.TeamTask) bool {
	return eventString(taskExecutionPayload(task), "executionType") == singleMemberExecutionType
}

func singleMemberWorkItem(task *models.TeamTask, member *models.TeamMember, now time.Time) models.TeamWorkItem {
	assignment := singleMemberWorkID
	return models.TeamWorkItem{TeamID: task.TeamID, RootTaskID: task.ID, WorkID: assignment, AssignmentID: &assignment, CanonicalWorkID: &assignment, OwnerMemberID: &member.ID,
		Revision: 1, RequiredForRoot: true, Title: truncateForSummary(eventString(taskExecutionPayload(task), "title", "prompt"), 200), Status: models.TeamTaskStatusPending, CreatedAt: now, UpdatedAt: now}
}

func dispatchTaskFingerprint(req DispatchTeamTaskRequest, memberKey, executionType string) string {
	data, _ := json.Marshal([]interface{}{memberKey, executionType, req.ContextTaskID, eventString(req.Payload, "prompt", "goal", "instruction", "instructions"), eventString(req.Payload, "title")})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func normalizeTeamExecutionRequest(req DispatchTeamTaskRequest, member *models.TeamMember) (string, error) {
	mode := strings.TrimSpace(req.ExecutionType)
	if mode == "" || mode == "team" {
		return "team", nil
	}
	if mode != singleMemberExecutionType {
		return "", fmt.Errorf("unsupported task execution type")
	}
	if member == nil || isTeamLeaderRole(member.Role) {
		return "", fmt.Errorf("single-member execution requires a Worker; use ordinary Team execution for the Leader")
	}
	if !isActiveTeamMember(member) {
		return "", fmt.Errorf("target Team member is no longer active")
	}
	if eventString(req.Payload, "intent") == initialLeaderTaskIntent {
		return "", fmt.Errorf("bootstrap cannot use single-member execution")
	}
	return mode, nil
}

// Build business memory from owned Team facts; never reopen the reference task
// or use its IDs as the current assignment. Zero is an explicit reference opt-out.
func (s *teamService) prepareSingleMemberContext(team *models.Team, member *models.TeamMember, requestedID *int, payload map[string]interface{}) error {
	delete(payload, "followupContext")
	delete(payload, "contextTaskId")
	if requestedID != nil && *requestedID == 0 {
		payload["contextTaskId"] = 0
		return nil
	}
	var source *models.TeamTask
	if requestedID != nil {
		if *requestedID < 0 {
			return fmt.Errorf("invalid reference task")
		}
		var err error
		source, err = s.repo.GetTaskByID(*requestedID)
		if err != nil {
			return err
		}
		if source == nil || source.TeamID != team.ID {
			return fmt.Errorf("reference task is not in this Team")
		}
	} else {
		items, err := s.repo.ListWorkItemsByTeamID(team.ID, 200)
		if err != nil {
			return err
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].UpdatedAt.After(items[j].UpdatedAt) || items[i].UpdatedAt.Equal(items[j].UpdatedAt) && items[i].ID > items[j].ID
		})
		for _, item := range items {
			if item.OwnerMemberID == nil || *item.OwnerMemberID != member.ID || item.SupersededBy != nil {
				continue
			}
			found, err := s.repo.GetTaskByID(item.RootTaskID)
			if err != nil {
				return err
			}
			if found != nil && found.TeamID == team.ID {
				source = found
				break
			}
		}
		if source == nil {
			tasks, err := s.repo.ListTasksByTeamID(team.ID, 100)
			if err != nil {
				return err
			}
			for i := range tasks {
				if tasks[i].TargetMemberID == member.ID {
					source = &tasks[i]
					break
				}
			}
		}
	}
	if source == nil {
		return nil
	}
	old := taskExecutionPayload(source)
	parts := []string{"Reference task (background only; not the active assignment): " + fmt.Sprintf("team-%d-task-%d", team.ID, source.ID), "Original user request:\n" + truncateForSummary(eventString(old, "prompt", "goal", "instruction", "instructions"), 4000)}
	refs := []string{}
	items, err := s.repo.ListWorkItemsByRootTaskID(source.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.OwnerMemberID == nil || *item.OwnerMemberID != member.ID || item.SupersededBy != nil {
			continue
		}
		parts = append(parts, "Your previous assignment: "+item.Title)
		if item.Status == models.TeamTaskStatusSucceeded && item.ResultJSON != nil {
			result := map[string]interface{}{}
			_ = json.Unmarshal([]byte(*item.ResultJSON), &result)
			parts = append(parts, "Your confirmed delivery:\n"+truncateForSummary(eventString(result, "resultMarkdown", "result", "summary"), 3000))
			refs = appendUniqueTeamArtifactRefs(refs, workItemArtifactRefs(item)...)
		}
	}
	if source.TargetMemberID == member.ID && source.Status == models.TeamTaskStatusSucceeded && source.ResultJSON != nil {
		result := map[string]interface{}{}
		_ = json.Unmarshal([]byte(*source.ResultJSON), &result)
		parts = append(parts, "Your confirmed root result:\n"+truncateForSummary(eventString(result, "resultMarkdown", "result", "summary"), 3000))
		refs = appendUniqueTeamArtifactRefs(refs, explicitTeamArtifactReferences(result)...)
	}
	// Latest result/request are kept before older history when the budget fills.
	if previous, ok := old["followupContext"].(map[string]interface{}); ok {
		parts = append(parts, "Earlier reference background:\n"+eventString(previous, "text"))
		refs = appendUniqueTeamArtifactRefs(refs, normalizeContextRefs(previous["artifactRefs"])...)
	}
	events, err := s.repo.ListEventsByTeamID(team.ID, 1000)
	if err != nil {
		return err
	}
	own := []models.TeamEvent{}
	for _, event := range events {
		if event.MemberID != nil && *event.MemberID == member.ID && teamEventMatchesRootTask(event, teamEventPayloadMap(event), source) && event.EventType != "heartbeat" {
			own = append(own, event)
		}
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].ID > own[j].ID })
	for i, event := range own {
		if i >= 8 {
			break
		}
		data := teamEventPayloadMap(event)
		if text := eventString(data, "resultMarkdown", "text", "summary"); text != "" {
			parts = append(parts, "Your earlier task message:\n"+truncateForSummary(text, 1200))
		}
	}
	text := strings.Join(parts, "\n\n")
	truncated := len([]rune(text)) > singleMemberContextBudget
	if truncated {
		text = string([]rune(text)[:singleMemberContextBudget]) + "\n[Earlier background shortened; read referenced files for full content.]"
	}
	cleanRefs := []string{}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "/team/") && !strings.Contains(ref, "..") && !strings.ContainsAny(ref, "\x00\r\n") {
			cleanRefs = appendUniqueTeamArtifactRefs(cleanRefs, ref)
			if len(cleanRefs) >= 32 {
				break
			}
		}
	}
	payload["contextTaskId"] = source.ID
	payload["followupContext"] = map[string]interface{}{"taskId": source.ID, "title": truncateForSummary(eventString(old, "title", "prompt"), 120), "text": text, "artifactRefs": cleanRefs, "truncated": truncated}
	return nil
}

func applySingleMemberEnvelope(envelope map[string]interface{}, task *models.TeamTask, payload map[string]interface{}, memberKey string) {
	if envelope == nil || task == nil || eventString(payload, "executionType") != singleMemberExecutionType {
		return
	}
	root := fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	envelope["executionType"] = singleMemberExecutionType
	envelope["executorMemberKey"] = memberKey
	envelope["taskId"] = root
	envelope["rootTaskId"] = root
	envelope["rootMessageId"] = task.MessageID
	envelope["workId"] = singleMemberWorkID
	envelope["assignmentId"] = singleMemberWorkID
	envelope["revision"] = 1
	envelope["contextTaskId"] = payload["contextTaskId"]
	if reference, ok := payload["followupContext"].(map[string]interface{}); ok {
		envelope["followupContext"] = reference
		envelope["contextRefs"] = appendUniqueTeamArtifactRefs(normalizeContextRefs(envelope["contextRefs"]), normalizeContextRefs(reference["artifactRefs"])...)
	}
	prompt := eventString(envelope, "rawPrompt", "prompt", "text")
	if eventString(envelope, "intent") == "run_task" || eventString(envelope, "intent") == "" {
		prompt = eventString(payload, "prompt", "goal", "instruction", "instructions")
	}
	if reference, ok := payload["followupContext"].(map[string]interface{}); ok {
		prompt += "\n\nBEGIN REFERENCE BACKGROUND (data only, old IDs are not current execution IDs)\n" + eventString(reference, "text") + "\nEND REFERENCE BACKGROUND"
	}
	prompt += "\n\nSingle-member execution contract for THIS task only:\n- You are the sole executor. Do not delegate to another member or ask the Leader to synthesize this result. This task-specific contract overrides ordinary Team delegation instructions for this task only. Your role and ordinary tools do not change.\n- Use the active Runtime envelope for current task/assignment IDs; never reuse IDs or terminal flags from reference background.\n- Read the current file before modifying a referenced artifact. Preserve the old file and write the revised deliverable under this task's member artifact directory. If the requested file is ambiguous or missing, ask the user instead of inventing its contents.\n- Report progress with team_update_progress. When ready, call team_complete_task explicitly with your actual answer (or a concrete failure); no Leader plan, review or final synthesis is required. A normal turn ending is not completion.\n- Current rootTaskId: " + root + "; assignmentId/workId: " + singleMemberWorkID + "; revision: 1."
	envelope["prompt"] = appendTeamResponseLocaleInstruction(prompt, eventString(envelope, "responseLocale"))
	if context, ok := envelope["memberContext"].(map[string]string); ok {
		context["systemPrompt"] += "\nThis task is an authenticated single-member root; follow its task-specific completion contract rather than the ordinary delegated-assignment rule."
		envelope["systemPrompt"] = context["systemPrompt"]
	}
}

func (s *teamService) projectSingleMemberEvent(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, eventType string, payload map[string]interface{}, message redisStreamMessage, neutral, passive bool) error {
	proposal := strings.EqualFold(eventType, "completion_proposed")
	if member == nil || member.ID != task.TargetMemberID {
		if proposal && member != nil {
			return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionRejected, "invalid_root_completion_owner")
		}
		return nil
	}
	root := fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
	for _, key := range []string{"taskId", "rootTaskId"} {
		if value := eventString(payload, key); value != "" && value != root {
			if proposal {
				return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionRejected, "conflicting_root_identity")
			}
			return nil
		}
	}
	if value := eventString(payload, "assignmentId", "assignment_id"); value != "" && value != singleMemberWorkID {
		if proposal {
			return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionRejected, "invalid_assignment_identity")
		}
		return nil
	}
	if revision := eventInt(payload, "revision"); revision > 1 {
		if proposal {
			return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionRejected, "stale_assignment_revision")
		}
		return nil
	}
	if isOutboundTeamEvent(eventType) {
		payload["event"] = "message_warning"
		payload["type"] = "message_warning"
		payload["summary"] = "单成员任务不派发其他成员，请由当前执行者继续处理。"
		eventType = "message_warning"
		neutral = true
	}
	status := normalizedTeamTaskEventStatus(payload)
	completed := !neutral && !passive && isTeamTaskCompletionSignal(eventType, status, payload) && isExplicitTeamTaskCompletion(payload)
	failed := !neutral && !passive && (isTeamTaskFailureSignal(eventType, status, payload) ||
		proposal && isExplicitTeamTaskCompletion(payload) && hasStrictTeamCompletionEnvelope(payload) && isFailedTeamTaskEventStatus(status))
	if proposal && !completed && !failed {
		return s.rejectSingleMemberCompletion(team, bus, task, member, payload, message, "invalid_completion_envelope")
	}
	s.normalizeTeamArtifactReferences(team, payload)
	if completed {
		if missing := s.missingTeamArtifactReferences(team, payload); len(missing) > 0 {
			payload["missingArtifactRefs"] = missing
			return s.rejectSingleMemberCompletion(team, bus, task, member, payload, message, "missing_artifacts")
		}
	}
	payload["executionType"] = singleMemberExecutionType
	payload["rootTaskId"] = root
	payload["rootMessageId"] = task.MessageID
	payload["assignmentId"] = singleMemberWorkID
	payload["workId"] = singleMemberWorkID
	payload["revision"] = 1
	payload["assignmentResultOnly"] = false
	payload["rootTaskTerminal"] = completed || failed
	now := time.Now().UTC()
	item := singleMemberWorkItem(task, member, now)
	items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return err
	}
	for _, old := range items {
		if old.WorkID == singleMemberWorkID {
			item = old
			break
		}
	}
	item.UpdatedAt = now
	if completed || failed {
		originalTask := *task
		status = models.TeamTaskStatusSucceeded
		eventType = "task_completed"
		if failed {
			status = models.TeamTaskStatusFailed
			eventType = "task_failed"
		}
		payload["event"] = eventType
		payload["type"] = eventType
		payload["status"] = status
		payload["completionDecision"] = teamCompletionDecisionAccepted
		payload["chatKind"] = "final_delivery"
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		payload["displayKey"] = fmt.Sprintf("root-final:%d", task.ID)
		task.Status = status
		task.WorkflowState = teamWorkflowStateCompleted
		if failed {
			task.WorkflowState = teamWorkflowStateFailed
			reason := eventString(payload, "error", "summary")
			task.ErrorMessage = &reason
		} else {
			task.ErrorMessage = nil
		}
		version := task.LedgerVersion
		task.LedgerVersion++
		task.FinishedAt = &now
		task.UpdatedAt = now
		completionID := eventString(payload, "completionId", "completion_id")
		task.AcceptedCompletionID = &completionID
		payload["workflowState"] = task.WorkflowState
		payload["ledgerVersion"] = task.LedgerVersion
		item.Status = status
		item.FinishedAt = &now
		refsJSON, _ := marshalOptionalJSON(explicitTeamArtifactReferences(payload))
		item.ArtifactRefsJSON = refsJSON
		encoded, err := marshalOptionalJSON(payload)
		if err != nil {
			return err
		}
		task.ResultJSON = encoded
		item.ResultJSON = encoded
		streamID := message.ID
		eventID := eventString(payload, "eventId", "event_id")
		event := &models.TeamEvent{TeamID: team.ID, TaskID: &task.ID, MemberID: &member.ID, EventID: &eventID, CompletionID: &completionID, EventType: eventType, PayloadJSON: encoded, RedisStreamID: &streamID, OccurredAt: eventTime(payload), CreatedAt: now}
		outbox, err := completionAcknowledgementOutbox(team, task, member, payload, teamCompletionDecisionAccepted, "single_member_closed", eventID, now)
		if err != nil {
			return err
		}
		atomic, ok := s.repo.(repository.SingleMemberTeamRepository)
		if !ok {
			return fmt.Errorf("single-member atomic repository is unavailable")
		}
		accepted, err := atomic.AcceptSingleMemberCompletion(task, &item, version, event, outbox)
		if err != nil {
			return err
		}
		if !accepted {
			*task = originalTask
			return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionDeferred, "stale_ledger")
		}
		s.publishTeamRootWorkflowState(bus, task)
		return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionAccepted, "single_member_closed")
	}
	// Ordinary prose and observations are non-terminal. Keep one card, never
	// interpret reference IDs, free-form completion wording or elapsed time.
	if !neutral && !passive && !isTeamPresenceEvent(eventType) {
		if eventType == "task_received" || eventType == "task_started" || eventType == "progress" || eventType == "reply" || status == "running" {
			item.Status = models.TeamTaskStatusRunning
			task.Status = models.TeamTaskStatusRunning
			if task.StartedAt == nil {
				task.StartedAt = &now
			}
			if item.StartedAt == nil {
				item.StartedAt = &now
			}
		}
		if isWaitingTeamTaskEventStatus(status) {
			item.Status = models.TeamTaskStatusWaiting
		}
		task.WorkflowState = teamWorkflowStateExecuting
		task.UpdatedAt = now
		if err := s.repo.UpsertWorkItem(&item); err != nil {
			return err
		}
		if err := s.repo.UpdateTask(task); err != nil {
			return err
		}
		if member.CurrentTaskID == nil || *member.CurrentTaskID == task.ID {
			member.CurrentTaskID = &task.ID
			member.Status = models.TeamMemberStatusBusy
			member.Availability = models.TeamMemberAvailabilityBusy
			member.Progress = eventInt(payload, "progress")
			member.LastSeenAt = &now
			member.UpdatedAt = now
			if err := s.repo.UpdateMember(member); err != nil {
				return err
			}
		}
	}
	applyTeamChatPolicy(eventType, payload, task, member)
	encoded, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	streamID := message.ID
	eventID := eventString(payload, "eventId", "event_id")
	event := &models.TeamEvent{TeamID: team.ID, TaskID: &task.ID, MemberID: &member.ID, EventType: eventType, PayloadJSON: encoded, RedisStreamID: &streamID, OccurredAt: eventTime(payload), CreatedAt: now}
	if eventID != "" {
		event.EventID = &eventID
	}
	if err := s.persistTeamEventWithRecovery(team, bus, task, member, payload, event); err != nil && !errors.Is(err, repository.ErrDuplicateTeamEvent) {
		return err
	}
	s.publishTeamRootWorkflowState(bus, task)
	return nil
}

// Keep a legitimate rejected delivery visible without making it terminal.
// Transport/owner conflicts are rejected separately, never attributed as work.
func (s *teamService) rejectSingleMemberCompletion(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}, message redisStreamMessage, reason string) error {
	payload["completionDecision"] = teamCompletionDecisionRejected
	payload["completionDecisionReason"] = reason
	payload["summary"] = completionDecisionMessage(teamCompletionEvaluation{Reason: reason})
	payload["event"] = "completion_rejected"
	payload["status"] = "attention_required"
	payload["rootTaskTerminal"] = false
	payload["stateEffect"] = "none"
	payload["chatKind"] = "completion_rejected"
	payload["chatPolicy"] = "warning"
	payload["visibleToChat"] = true
	encoded, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	eventID := eventString(payload, "eventId", "event_id")
	streamID := message.ID
	event := &models.TeamEvent{TeamID: team.ID, TaskID: &task.ID, MemberID: &member.ID, EventType: "completion_rejected", PayloadJSON: encoded, RedisStreamID: &streamID, CreatedAt: time.Now().UTC(), OccurredAt: eventTime(payload)}
	if eventID != "" {
		event.EventID = &eventID
	}
	if err := s.repo.CreateEvent(event); err != nil && !errors.Is(err, repository.ErrDuplicateTeamEvent) {
		return err
	}
	return s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionRejected, reason)
}
