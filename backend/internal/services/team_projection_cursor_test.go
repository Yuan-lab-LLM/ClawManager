package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
)

type cursorProjectionRepository struct {
	*teamRepositoryStub
	cursorErr, outboxErr error
	failMember           bool
	projections          int
}

func (r *cursorProjectionRepository) UpdateTeam(team *models.Team) error {
	if r.cursorErr != nil {
		return r.cursorErr
	}
	return r.teamRepositoryStub.UpdateTeam(team)
}
func (r *cursorProjectionRepository) CreateEventOutbox(row *models.TeamEventOutbox) error {
	if r.outboxErr != nil {
		return r.outboxErr
	}
	return r.teamRepositoryStub.CreateEventOutbox(row)
}
func (r *cursorProjectionRepository) EventExistsByStreamID(teamID int, id string) (bool, error) {
	for _, event := range r.createdEvents {
		if event.TeamID == teamID && derefTeamString(event.RedisStreamID) == id {
			return true, nil
		}
	}
	return false, nil
}
func (r *cursorProjectionRepository) UpdateMember(member *models.TeamMember) error {
	r.projections++
	if r.failMember {
		return errors.New("member unavailable")
	}
	return r.teamRepositoryStub.UpdateMember(member)
}
func cursorProjectionFixture() (*cursorProjectionRepository, *models.Team, redisStreamMessage) {
	member := &models.TeamMember{ID: 1, TeamID: 12, MemberKey: "worker"}
	r := &cursorProjectionRepository{teamRepositoryStub: &teamRepositoryStub{membersByKey: map[string]*models.TeamMember{"worker": member}}}
	return r, &models.Team{ID: 12, RedisEventsLastID: "1-0", UpdatedAt: time.Unix(1, 0).UTC()}, redisStreamMessage{ID: "2-0", Fields: map[string]string{"payload": `{"event":"message","memberId":"worker"}`}}
}

func TestProjectionCursorCommitFailureKeepsOriginalMemoryAndDedup(t *testing.T) {
	r, team, message := cursorProjectionFixture()
	before := *team
	r.cursorErr = errors.New("cursor commit unavailable")
	if (&teamService{repo: r}).projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("failed cursor commit acknowledged")
	}
	if team.RedisEventsLastID != before.RedisEventsLastID || !team.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("uncommitted cursor leaked into memory")
	}
	r.cursorErr = nil
	if !(&teamService{repo: r}).projectConsumedTeamEvent(context.Background(), team, nil, message) || team.RedisEventsLastID != message.ID {
		t.Fatal("redelivery after failed cursor commit was not acknowledged")
	}
	if r.projections != 1 || len(r.createdEvents) != 1 || len(r.outboxRows) != 0 {
		t.Fatal("successful projection repeated effects or created recovery history", r.projections, len(r.createdEvents), len(r.outboxRows))
	}
}

func TestProjectionFailureWithoutDurableHandoffKeepsCursor(t *testing.T) {
	r, team, message := cursorProjectionFixture()
	r.failMember = true
	r.outboxErr = errors.New("recovery unavailable")
	if (&teamService{repo: r}).projectConsumedTeamEvent(context.Background(), team, nil, message) || team.RedisEventsLastID != "1-0" {
		t.Fatal("failed projection acknowledged without durable recovery")
	}
}

func TestProjectionDurableHandoffSurvivesCursorCommitFailure(t *testing.T) {
	r, team, message := cursorProjectionFixture()
	r.failMember = true
	r.cursorErr = errors.New("cursor unavailable")
	if (&teamService{repo: r}).projectConsumedTeamEvent(context.Background(), team, nil, message) || team.RedisEventsLastID != "1-0" {
		t.Fatal("failed cursor commit acknowledged or changed memory")
	}
	if len(r.outboxRows) != 1 {
		t.Fatal("durable recovery intent lost")
	}
	var original redisStreamMessage
	if err := json.Unmarshal([]byte(r.outboxRows[0].PayloadJSON), &original); err != nil || original.ID != message.ID || original.Fields["payload"] != message.Fields["payload"] {
		t.Fatalf("recovery transport identity lost: %+v %v", original, err)
	}
}

func TestProjectionDuplicateBusinessIdentityDoesNotRepeatWrites(t *testing.T) {
	r, team, message := cursorProjectionFixture()
	message.Fields["payload"] = `{"event":"message","memberId":"worker","eventId":"business-event"}`
	service := &teamService{repo: r}
	if !service.projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("initial projection failed")
	}
	message.ID = "3-0"
	if !service.projectConsumedTeamEvent(context.Background(), team, nil, message) || r.projections != 1 || len(r.createdEvents) != 1 {
		t.Fatal("business identity dedup bypassed", r.projections, len(r.createdEvents))
	}
}
