package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestSingleMemberTaskRequiresPersistedContract(t *testing.T) {
	for _, payload := range []string{"", `{}`, `{"executionMode":"leader_control_plane_snapshot"}`, `{"executionType":"team"}`} {
		if isSingleMemberTask(&models.TeamTask{PayloadJSON: payload}) {
			t.Fatalf("legacy task reinterpreted: %s", payload)
		}
	}
	if !isSingleMemberTask(&models.TeamTask{PayloadJSON: `{"executionType":"single_member"}`}) {
		t.Fatal("explicit single-member contract missing")
	}
}

func TestSingleMemberEnvelopeKeepsSessionButRebindsBusinessIdentity(t *testing.T) {
	task := &models.TeamTask{ID: 45, TeamID: 7, MessageID: "new-input", PayloadJSON: `{"executionType":"single_member","executorMemberKey":"editor","followupContext":{"text":"old taskId=team-7-task-12; preserve sources","artifactRefs":["/team/artifacts/team-7-task-12/members/editor/a/report.md"]}}`}
	payload := map[string]interface{}{}
	_ = json.Unmarshal([]byte(task.PayloadJSON), &payload)
	envelope := buildTeamTaskEnvelope(7, "editor", task, "new-input", payload, map[string]string{"role": "worker", "communicationMode": "leader_mediated"}, time.Now())
	if envelope["executionType"] != "single_member" || envelope["rootTaskId"] != "team-7-task-45" || envelope["assignmentId"] != singleMemberWorkID {
		t.Fatalf("wrong current contract: %#v", envelope)
	}
	if _, changed := envelope["conversationId"]; changed {
		t.Fatal("native Team session route must not change")
	}
	prompt := eventString(envelope, "prompt")
	if !strings.Contains(prompt, "preserve sources") || !strings.Contains(prompt, "Read the current file") || strings.Contains(prompt, "only the Leader") {
		t.Fatalf("missing follow-up context or contradictory completion policy: %s", prompt)
	}
	check := map[string]interface{}{"requiresCompletion": false, "intent": "assignment_status_check", "prompt": "continue your exact attempt", "assignmentId": singleMemberWorkID}
	applyTeamTaskEnvelopeContext(check, task, "editor")
	if check["executionType"] != "single_member" || check["requiresCompletion"] != false || check["rootTaskId"] != "team-7-task-45" {
		t.Fatalf("recovery lost task scope: %#v", check)
	}
}

func TestSingleMemberContextIncludesOwnConfirmedWorkAndPreviousFollowup(t *testing.T) {
	old := &models.TeamTask{ID: 12, TeamID: 7, TargetMemberID: 1, PayloadJSON: `{"prompt":"Write a report with verified sources","followupContext":{"text":"Keep the original citations"}}`, Status: models.TeamTaskStatusSucceeded}
	result := `{"resultMarkdown":"Collected source A and B","artifactRefs":["/team/artifacts/team-7-task-12/members/editor/a/report.md"]}`
	other := `{"resultMarkdown":"Private work of another member"}`
	repo := &teamRepositoryStub{tasksByID: map[int]*models.TeamTask{12: old}, workItems: []models.TeamWorkItem{
		{ID: 1, TeamID: 7, RootTaskID: 12, OwnerMemberID: intPtr(2), Title: "Collect sources", Status: models.TeamTaskStatusSucceeded, ResultJSON: &result},
		{ID: 2, TeamID: 7, RootTaskID: 12, OwnerMemberID: intPtr(3), Status: models.TeamTaskStatusSucceeded, ResultJSON: &other},
	}}
	s := &teamService{repo: repo}
	payload := map[string]interface{}{"prompt": "Shorten the report"}
	contextID := 12
	if err := s.prepareSingleMemberContext(&models.Team{ID: 7}, &models.TeamMember{ID: 2, TeamID: 7, MemberKey: "editor"}, &contextID, payload); err != nil {
		t.Fatal(err)
	}
	context, _ := payload["followupContext"].(map[string]interface{})
	text := eventString(context, "text")
	for _, want := range []string{"verified sources", "Collected source A and B", "Keep the original citations"} {
		if !strings.Contains(text, want) {
			t.Fatalf("lost memory %q: %s", want, text)
		}
	}
	if strings.Contains(text, "Private work") {
		t.Fatal("another member's delivery treated as this worker's memory")
	}
	if old.Status != models.TeamTaskStatusSucceeded {
		t.Fatal("reference task reopened")
	}
	foreign := 99
	repo.tasksByID[99] = &models.TeamTask{ID: 99, TeamID: 8}
	if s.prepareSingleMemberContext(&models.Team{ID: 7}, &models.TeamMember{ID: 2}, &foreign, map[string]interface{}{}) == nil {
		t.Fatal("cross-Team reference accepted")
	}
	zero := 0
	cleared := map[string]interface{}{}
	if err := s.prepareSingleMemberContext(&models.Team{ID: 7}, &models.TeamMember{ID: 2}, &zero, cleared); err != nil || cleared["followupContext"] != nil {
		t.Fatal("reference opt-out ignored")
	}
}

func TestSingleMemberExplicitCompletionDoesNotRequireLeader(t *testing.T) {
	for _, status := range []string{"succeeded", "failed"} {
		t.Run(status, func(t *testing.T) {
			task := &models.TeamTask{ID: 45, TeamID: 7, TargetMemberID: 2, MessageID: "input-45", Status: models.TeamTaskStatusRunning, PayloadJSON: `{"executionType":"single_member","executorMemberKey":"editor"}`}
			worker := &models.TeamMember{ID: 2, TeamID: 7, MemberKey: "editor", Role: "worker", CurrentTaskID: intPtr(45)}
			base := &teamRepositoryStub{tasksByID: map[int]*models.TeamTask{45: task}, tasksByMessageID: map[string]*models.TeamTask{"input-45": task}, membersByKey: map[string]*models.TeamMember{"editor": worker}, workItems: []models.TeamWorkItem{singleMemberWorkItem(task, worker, time.Now())}}
			repo := &singleMemberRepositoryStub{teamRepositoryStub: base}
			service := &teamService{repo: repo}
			payload := map[string]interface{}{"protocolVersion": 3, "event": "completion_proposed", "eventId": "complete-45", "completionId": "complete-45", "attemptId": "try-1", "memberId": "editor", "taskId": "team-7-task-45", "rootTaskId": "team-7-task-45", "messageId": "input-45", "rootMessageId": "input-45", "assignmentId": singleMemberWorkID, "revision": 1, "status": status, "summary": "Report updated", "resultMarkdown": "Updated report with original sources", "completionSource": "team_complete_task", "explicitCompletion": true, "assignmentResultOnly": true, "rootTaskTerminal": false}
			encoded, _ := json.Marshal(payload)
			if err := service.projectTeamEvent(&models.Team{ID: 7, CommunicationMode: "leader_mediated"}, nil, redisStreamMessage{ID: "1-0", Fields: map[string]string{"payload": string(encoded)}}); err != nil {
				t.Fatal(err)
			}
			if repo.updatedTask == nil || repo.updatedTask.Status != status || repo.workItems[0].Status != status {
				t.Fatalf("root/card not atomic: task=%#v items=%#v", repo.updatedTask, repo.workItems)
			}
			if len(repo.outboxRows) != 1 || len(repo.createdEvents) != 1 {
				t.Fatalf("unexpected Leader handoff or missing ACK: %d/%d", len(repo.outboxRows), len(repo.createdEvents))
			}
		})
	}
}

type singleMemberRepositoryStub struct{ *teamRepositoryStub }

func (s *singleMemberRepositoryStub) CreateSingleMemberTask(task *models.TeamTask, item *models.TeamWorkItem) error {
	if err := s.CreateTask(task); err != nil {
		return err
	}
	item.RootTaskID = task.ID
	return s.UpsertWorkItem(item)
}
func (s *singleMemberRepositoryStub) AcceptSingleMemberCompletion(task *models.TeamTask, item *models.TeamWorkItem, version int64, event *models.TeamEvent, outbox *models.TeamEventOutbox) (bool, error) {
	accepted, err := s.AcceptRootCompletion(task, version, event, outbox)
	if accepted && err == nil {
		err = s.UpsertWorkItem(item)
	}
	return accepted, err
}

func TestSingleMemberContextBudgetAndAutomaticOwnership(t *testing.T) {
	result := `{"resultMarkdown":"Most recent own work"}`
	repo := &teamRepositoryStub{tasksByID: map[int]*models.TeamTask{
		12: {ID: 12, TeamID: 7, TargetMemberID: 1, Status: "succeeded", PayloadJSON: `{"prompt":"` + strings.Repeat("旧", 18000) + `"}`},
		13: {ID: 13, TeamID: 7, TargetMemberID: 1, Status: "succeeded", PayloadJSON: `{"prompt":"unrelated newest"}`},
	}, workItems: []models.TeamWorkItem{{ID: 1, RootTaskID: 12, TeamID: 7, OwnerMemberID: intPtr(2), Status: "succeeded", ResultJSON: &result}, {ID: 2, RootTaskID: 13, TeamID: 7, OwnerMemberID: intPtr(3), UpdatedAt: time.Now()}}}
	payload := map[string]interface{}{}
	s := &teamService{repo: repo}
	if err := s.prepareSingleMemberContext(&models.Team{ID: 7}, &models.TeamMember{ID: 2}, nil, payload); err != nil {
		t.Fatal(err)
	}
	context := payload["followupContext"].(map[string]interface{})
	if context["taskId"] != 12 || !strings.Contains(context["text"].(string), "Most recent own work") || len([]rune(context["text"].(string))) > 12200 {
		t.Fatalf("bad bounded ownership context: %#v", context)
	}
}

func TestSingleMemberDoesNotAcceptOtherAuthorProseOrOldIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		member string
		extra  map[string]interface{}
	}{
		{"other owner", "leader", nil},
		{"plain end of turn", "editor", map[string]interface{}{"event": "reply", "explicitCompletion": false, "completionSource": ""}},
		{"old root identity", "editor", map[string]interface{}{"taskId": "team-7-task-12"}},
		{"old assignment", "editor", map[string]interface{}{"assignmentId": "old-delivery"}},
		{"passive monitor", "editor", map[string]interface{}{"event": "assignment_heartbeat", "stateEffect": "none", "nonAuthoritative": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := &models.TeamTask{ID: 45, TeamID: 7, TargetMemberID: 2, MessageID: "new", Status: "running", PayloadJSON: `{"executionType":"single_member"}`}
			worker := &models.TeamMember{ID: 2, TeamID: 7, MemberKey: "editor", Role: "worker", CurrentTaskID: intPtr(45)}
			repo := &singleMemberRepositoryStub{&teamRepositoryStub{tasksByID: map[int]*models.TeamTask{45: task}, tasksByMessageID: map[string]*models.TeamTask{"new": task}, membersByKey: map[string]*models.TeamMember{"editor": worker, "leader": {ID: 1, TeamID: 7, MemberKey: "leader", Role: "leader"}}, workItems: []models.TeamWorkItem{singleMemberWorkItem(task, worker, time.Now())}}}
			payload := map[string]interface{}{"protocolVersion": 3, "event": "completion_proposed", "eventId": "proposal", "completionId": "proposal", "attemptId": "try", "memberId": test.member, "rootTaskId": "team-7-task-45", "taskId": "team-7-task-45", "messageId": "new", "rootMessageId": "new", "assignmentId": singleMemberWorkID, "revision": 1, "status": "succeeded", "summary": "完成", "resultMarkdown": "完成的实际内容", "completionSource": "team_complete_task", "explicitCompletion": true}
			for key, value := range test.extra {
				payload[key] = value
			}
			encoded, _ := json.Marshal(payload)
			if err := (&teamService{repo: repo}).projectTeamEvent(&models.Team{ID: 7, CommunicationMode: "leader_mediated"}, nil, redisStreamMessage{ID: "1-0", Fields: map[string]string{"payload": string(encoded)}}); err != nil {
				t.Fatal(err)
			}
			if task.Status == "succeeded" || task.Status == "failed" || repo.workItems[0].Status == "succeeded" {
				t.Fatal("untrusted/non-explicit event closed root")
			}
		})
	}
}
