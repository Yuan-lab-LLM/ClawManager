package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"strings"
	"testing"
)

type shutdownObservationStore struct {
	calls, id, owner int
	source           string
	observation      models.RuntimeShutdownObservation
	err              error
	deadline         bool
}

func (s *shutdownObservationStore) Record(ctx context.Context, id, owner int, source string, o models.RuntimeShutdownObservation) error {
	s.calls++
	s.id = id
	s.owner = owner
	s.source = source
	s.observation = o
	_, s.deadline = ctx.Deadline()
	return s.err
}
func (s *shutdownObservationStore) List(context.Context, int, int, int64) ([]models.RuntimeShutdownAudit, error) {
	return nil, nil
}
func TestRuntimeShutdownProObservationScopeAndCompatibility(t *testing.T) {
	store := &shutdownObservationStore{}
	s := &instanceRuntimeStatusService{shutdownAudits: store}
	session := &AgentSession{Instance: &models.Instance{ID: 8, UserID: 7, RuntimeGeneration: 3}, Agent: &models.InstanceAgent{InstanceID: 8}}
	o := models.RuntimeShutdownObservation{Generation: 3, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_unconfirmed"}
	if s.recordShutdownObservation(nil, nil) != nil || store.calls != 0 {
		t.Fatal("legacy observation required")
	}
	if err := s.recordShutdownObservation(session, &o); err != nil || store.calls != 1 || store.id != 8 || store.owner != 7 || store.source != "pro_agent" || !store.deadline {
		t.Fatalf("%+v %v", store, err)
	}
	for _, change := range []func(){func() { session.Agent.InstanceID = 9 }, func() { session.Instance.RuntimeGeneration = 4 }} {
		old := *session.Agent
		gen := session.Instance.RuntimeGeneration
		change()
		if s.recordShutdownObservation(session, &o) == nil {
			t.Fatal("mismatched identity accepted")
		}
		*session.Agent = old
		session.Instance.RuntimeGeneration = gen
	}
	if store.calls != 1 {
		t.Fatal("invalid observation persisted")
	}
	store.err = errors.New("password=secret")
	if err := s.recordShutdownObservation(session, &o); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsanitized storage error")
	}
	s.shutdownAudits = nil
	if s.recordShutdownObservation(session, &o) == nil {
		t.Fatal("silent loss")
	}
}
