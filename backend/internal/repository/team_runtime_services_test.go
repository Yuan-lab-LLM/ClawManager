package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// A SQL boundary spy is used because the local environment has no disposable
// MySQL. It makes detached/legacy context-free access fail immediately.
type teamContextSession struct {
	db.Session
	got       context.Context
	table     string
	condition []interface{}
	limit     int
	members   []models.TeamMember
}

func (s *teamContextSession) WithContext(ctx context.Context) db.Session { s.got = ctx; return s }
func (s *teamContextSession) Collection(name string) db.Collection {
	s.table = name
	return teamContextCollection{s: s}
}

type teamContextCollection struct {
	db.Collection
	s *teamContextSession
}

func (c teamContextCollection) Find(args ...interface{}) db.Result {
	c.s.condition = args
	return teamContextResult{s: c.s}
}

type teamContextResult struct {
	db.Result
	s *teamContextSession
}

func (r teamContextResult) Limit(limit int) db.Result { r.s.limit = limit; return r }
func (r teamContextResult) All(target any) error {
	if r.s.got == nil {
		return errors.New("lost SQL context")
	}
	if err := r.s.got.Err(); err != nil {
		return err
	}
	*target.(*[]models.TeamMember) = append([]models.TeamMember(nil), r.s.members...)
	return nil
}

func TestRuntimeTeamAssociationQueryIsContextualBoundedAndUnambiguous(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		session := &teamContextSession{members: make([]models.TeamMember, count)}
		if count > 0 {
			session.members[0].ID = 17
		}
		repo := &teamRepository{sess: session}
		member, err := repo.GetRuntimeMemberByInstanceContext(context.Background(), 7)
		if count == 2 {
			if err == nil {
				t.Fatal("ambiguous association accepted")
			}
		} else if err != nil || (count == 0 && member != nil) || (count == 1 && (member == nil || member.ID != 17)) {
			t.Fatalf("rows=%d member=%v err=%v", count, member, err)
		}
		if session.limit != 2 || session.table != "team_members" || !reflect.DeepEqual(session.condition, []interface{}{db.Cond{"instance_id": 7}}) {
			t.Fatal("association lost authoritative index query or bound")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &teamContextSession{}
	if _, err := (&teamRepository{sess: session}).GetRuntimeMemberByInstanceContext(ctx, 7); !errors.Is(err, context.Canceled) {
		t.Fatal("query lost cancellation", err)
	}
}

func (r teamContextResult) One(any) error {
	if r.s.got == nil {
		return errors.New("lost SQL context")
	}
	return r.s.got.Err()
}
func TestTeamRuntimeRepositoryUsesContextAndAuthoritativeTuple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &teamContextSession{}
	repo := &teamRepository{sess: session}
	if _, err := repo.GetTeamByIDContext(ctx, 12); !errors.Is(err, context.Canceled) {
		t.Fatal("Team SQL did not receive cancellation", err)
	}
	if session.table != "teams" || !reflect.DeepEqual(session.condition, []interface{}{db.Cond{"id": 12}}) {
		t.Fatal("wrong Team lookup scope", session.table, session.condition)
	}
	session.got = nil
	if _, err := repo.GetMemberByTeamInstanceContext(ctx, 12, 7); !errors.Is(err, context.Canceled) {
		t.Fatal("member SQL did not receive cancellation", err)
	}
	if session.table != "team_members" || !reflect.DeepEqual(session.condition, []interface{}{db.Cond{"team_id": 12, "instance_id": 7}}) {
		t.Fatal("wrong member lookup tuple", session.table, session.condition)
	}
}
