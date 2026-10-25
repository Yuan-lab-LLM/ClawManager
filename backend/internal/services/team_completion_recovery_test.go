package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

type recoveryFailureRepository struct {
	*singleMemberRepositoryStub
	fail bool
}

func (r *recoveryFailureRepository) CreateEventWithOutbox(e *models.TeamEvent, o *models.TeamEventOutbox, observations ...*models.TeamEvent) error {
	if r.fail {
		return errors.New("isolated transaction failure")
	}
	return r.teamRepositoryStub.CreateEventWithOutbox(e, o, observations...)
}

func TestSingleMemberTurnEndUsesSharedAtomicRecovery(t *testing.T) {
	for _, runtimeType := range []string{"openclaw", "hermes"} {
		for _, mode := range []string{"lite", "pro"} {
			t.Run(runtimeType+"/"+mode, func(t *testing.T) {
				task := &models.TeamTask{ID: 45, TeamID: 7, TargetMemberID: 2, MessageID: "input", Status: "running", PayloadJSON: `{"executionType":"single_member","followupContext":{"text":"Keep earlier citations"}}`}
				member := &models.TeamMember{ID: 2, TeamID: 7, MemberKey: "editor", Role: "worker", RuntimeType: runtimeType, InstanceMode: mode, CurrentTaskID: intPtr(45)}
				base := &teamRepositoryStub{tasksByID: map[int]*models.TeamTask{45: task}, tasksByMessageID: map[string]*models.TeamTask{"input": task}, membersByKey: map[string]*models.TeamMember{"editor": member}, workItems: []models.TeamWorkItem{singleMemberWorkItem(task, member, time.Now())}}
				repo := &recoveryFailureRepository{singleMemberRepositoryStub: &singleMemberRepositoryStub{base}, fail: true}
				s := &teamService{repo: repo}
				team := &models.Team{ID: 7, CommunicationMode: "leader_mediated"}
				payload := map[string]interface{}{"event": "task_progress", "eventKind": "turn_finished_without_completion", "eventId": "turn-45", "memberId": "editor", "messageId": "input", "taskId": "team-7-task-45", "rootTaskId": "team-7-task-45", "assignmentId": "single-member", "revision": 1, "activeTurnFinished": true, "turnObservationOutcome": "completion_receipt_gap", "immediateRecoveryEligible": true, "resultMarkdown": "Existing complete answer with citations", "nonAuthoritative": true, "stateEffect": "none"}
				send := func() error {
					b, _ := json.Marshal(payload)
					return s.projectTeamEvent(team, nil, redisStreamMessage{ID: "1-0", Fields: map[string]string{"payload": string(b)}})
				}
				if send() == nil || len(base.createdEvents) != 0 || len(base.outboxRows) != 0 {
					t.Fatal("failed transaction must not consume source observation")
				}
				repo.fail = false
				if err := send(); err != nil {
					t.Fatal(err)
				}
				if err := send(); err != nil {
					t.Fatal(err)
				}
				if len(base.createdEvents) != 2 || len(base.outboxRows) != 1 {
					t.Fatalf("duplicate or missing recovery: %d/%d", len(base.createdEvents), len(base.outboxRows))
				}
				if task.Status != "running" || task.AcceptedCompletionID != nil || len(base.workItems) != 1 {
					t.Fatal("observation changed business outcome")
				}
				var envelope map[string]interface{}
				_ = json.Unmarshal([]byte(base.outboxRows[0].PayloadJSON), &envelope)
				if envelope["executionType"] != "single_member" || envelope["assignmentId"] != "single-member" || envelope["rootTaskId"] != "team-7-task-45" || envelope["to"] != "editor" {
					t.Fatalf("lost binding: %#v", envelope)
				}
				for _, want := range []string{"Existing complete answer with citations", "Keep earlier citations", "text-only answer", "do not repeat research"} {
					if !strings.Contains(eventString(envelope, "prompt"), want) {
						t.Fatalf("missing recovery instruction %q", want)
					}
				}
				if eventBool(envelope["turnOutcomePolicy"].(map[string]interface{}), "immediateRecoveryAllowed") {
					t.Fatal("recursive recovery permitted")
				}
				if eventBool(teamEventPayloadMap(base.createdEvents[0]), "visibleToChat") {
					t.Fatal("ordinary prose exposed as delivery")
				}
			})
		}
	}
}

func TestCompletionRecoveryRejectsNonActionableAndLateTurns(t *testing.T) {
	task := &models.TeamTask{ID: 45, TeamID: 7, TargetMemberID: 2, Status: "running"}
	member := &models.TeamMember{ID: 2, TeamID: 7}
	for _, extra := range []map[string]interface{}{
		{"completionRecoveryAttempt": 1}, {"observationConflict": true}, {"lateAfterAssignmentTerminal": true},
		{"downstreamAssignmentStarted": true}, {"turnObservationOutcome": "legitimate_wait"},
		{"turnObservationOutcome": "runtime_observation_unknown"}, {"immediateRecoveryEligible": false}, {"explicitCompletion": true},
	} {
		p := map[string]interface{}{"activeTurnFinished": true, "turnObservationOutcome": "completion_receipt_gap", "immediateRecoveryEligible": true}
		for k, v := range extra {
			p[k] = v
		}
		if shouldRequestRootCoordinationRecovery(task, member, "turn_finished_without_completion", p) {
			t.Fatalf("unsafe recovery: %#v", extra)
		}
	}
}
