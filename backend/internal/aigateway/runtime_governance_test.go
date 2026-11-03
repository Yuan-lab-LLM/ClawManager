package aigateway

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

type taskTestStore struct {
	state models.GatewayTaskState
	err   error
	binds int
}

func (r *taskTestStore) Check(context.Context, models.GatewayTaskIdentity) (*models.GatewayTaskState, error) {
	return &r.state, r.err
}
func (r *taskTestStore) Bind(context.Context, models.GatewayTaskIdentity, int) error {
	r.binds++
	return r.err
}
func (r *taskTestStore) Finish(context.Context, models.GatewayTaskIdentity, bool) error { return r.err }

func TestRuntimeTaskCannotResumeThroughAutoOrAnotherModel(t *testing.T) {
	for _, finished := range []bool{false, true} {
		store := &taskTestStore{state: models.GatewayTaskState{Revoked: !finished, Finished: finished}}
		s := &service{runtimeTasks: store} // Must reject BEFORE model routing/classification.
		for _, name := range []string{"auto", "another-allowed-model"} {
			req := ChatCompletionRequest{Model: name, RuntimeTask: &models.GatewayTaskIdentity{}}
			if _, _, err := s.ChatCompletions(t.Context(), 1, req); !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("nonstream %v", err)
			}
			if _, err := s.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder()); !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("stream %v", err)
			}
		}
		if store.binds != 0 {
			t.Fatal("revoked task reached dispatch")
		}
	}
}

func TestRuntimeTaskStoreFailureFailsClosed(t *testing.T) {
	store := &taskTestStore{err: errors.New("database offline")}
	s := &service{runtimeTasks: store}
	if err := s.checkRuntimeTask(t.Context(), ChatCompletionRequest{RuntimeTask: &models.GatewayTaskIdentity{}}); err == nil {
		t.Fatal("accepted failed policy read")
	}
	if err := s.checkRuntimeTask(t.Context(), ChatCompletionRequest{}); err != nil {
		t.Fatal("legacy non-task request changed")
	}
}

func TestRuntimeCatalogFiltersDeploymentsAndContext(t *testing.T) {
	items := routingModels()
	items[0].VisibilityScope = "selected_users"
	items[0].AllowedUserIDs = []int{1}
	items[0].ContextWindowTokens = 1
	items[1].ContextWindowTokens = 4096
	s := routingGateway(items, &stubAuditEventService{})
	catalog, err := s.RuntimeCatalog(2, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range catalog {
		if m.ID == items[0].DisplayName {
			t.Fatal("hidden deployment leaked")
		}
		if m.ID == "auto" && m.ContextWindow != 4096 {
			t.Fatalf("hidden context affected auto: %+v", m)
		}
	}
	for i := range items {
		items[i].VisibilityScope = "selected_users"
		items[i].AllowedUserIDs = []int{1}
	}
	s = routingGateway(items, &stubAuditEventService{})
	catalog, err = s.RuntimeCatalog(2, nil)
	if err != nil || len(catalog) != 0 {
		t.Fatalf("empty policy: %+v %v", catalog, err)
	}
}

type runtimeFullDeploymentStore struct{ routingDeploymentStore }

func (s *runtimeFullDeploymentStore) Acquire(_ context.Context, input services.GatewayDeploymentAcquireInput) (*services.GatewayDeploymentLease, error) {
	s.acquired = append(s.acquired, input)
	return nil, services.ErrGatewayDeploymentUnavailable
}

func TestRuntimeTaskDoesNotBindCapacitySkippedDeployments(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmtBool(stream), func(t *testing.T) {
			store := &taskTestStore{}
			capacity := &runtimeFullDeploymentStore{}
			s := routingGateway(routingModels(), &stubAuditEventService{})
			s.modelRepo = &accessCheckedRepository{&stubLLMModelRepository{active: routingModels()}, &accessTestChecker{}}
			s.runtimeTasks = store
			s.deploymentStore = capacity
			req := routingRequest("hello")
			req.RuntimeTask = &models.GatewayTaskIdentity{InstanceID: 1, UserID: 1, SessionID: "s", TaskID: "t"}
			var err error
			if stream {
				_, err = s.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder())
			} else {
				_, _, err = s.ChatCompletions(t.Context(), 1, req)
			}
			if err == nil || len(capacity.acquired) == 0 {
				t.Fatalf("expected capacity rejection: calls=%d err=%v", len(capacity.acquired), err)
			}
			if store.binds != 0 {
				t.Fatalf("bound %d deployments without capacity", store.binds)
			}
		})
	}
}
