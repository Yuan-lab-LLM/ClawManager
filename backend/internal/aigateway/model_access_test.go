package aigateway

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelAccessScopedRoutingAndFallback(t *testing.T) {
	items := routingModels()
	items[0].VisibilityScope, items[0].AllowedUserIDs = "selected_users", []int{1}
	// Same upstream name is deliberately irrelevant: grants belong to deployments.
	items[0].ProviderModelName, items[1].ProviderModelName = "same-model", "same-model"
	items[0].BaseURL, items[1].BaseURL = "https://one.invalid", "https://two.invalid"
	s := routingGateway(items, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(s.modelRepo, 2)
	if _, err := s.resolveRequestedModel(items[0].DisplayName); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("explicit bypass: %v", err)
	}
	p, err := s.prepareChatRequest(t.Context(), 2, routingRequest("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if p.resolvedModel.ID != items[1].ID {
		t.Fatalf("unauthorized primary: %+v", p.resolvedModel)
	}
	for _, candidate := range p.deploymentCandidates {
		if candidate.ID == items[0].ID {
			t.Fatal("fallback leaked hidden deployment")
		}
	}
	items[0].IsSecure = true
	items[1].IsSecure = false
	s = routingGateway(items, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(s.modelRepo, 2)
	_, _, err = s.resolveTargetModelForRequest(&items[1], services.RiskAnalysis{IsSensitive: true, HighestAction: models.RiskActionRouteSecureModel}, ChatCompletionRequest{})
	if err == nil {
		t.Fatal("secure reroute bypassed authorization")
	}
}

func TestModelAccessBothPublicEntryPointsReject(t *testing.T) {
	items := routingModels()
	for i := range items {
		items[i].VisibilityScope, items[i].AllowedUserIDs = "selected_users", []int{1}
	}
	for _, name := range []string{"auto", items[0].DisplayName, items[0].ModelGroup} {
		for _, stream := range []bool{false, true} {
			s := routingGateway(items, &stubAuditEventService{})
			r := routingRequest("hello")
			r.Model = name
			r.Stream = stream
			var err error
			if stream {
				_, err = s.StreamChatCompletions(t.Context(), 2, r, httptest.NewRecorder())
			} else {
				_, _, err = s.ChatCompletions(t.Context(), 2, r)
			}
			if !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("%s stream=%v: %v", name, stream, err)
			}
		}
	}
}

type accessTestChecker struct {
	revoked atomic.Bool
	failure error
}

func (c *accessTestChecker) CheckModelAccess(context.Context, int, int) error {
	if c.failure != nil {
		return c.failure
	}
	if c.revoked.Load() {
		return models.ErrModelUnavailable
	}
	return nil
}
func TestModelAccessGuardCancelsAndFailsClosed(t *testing.T) {
	checker := &accessTestChecker{}
	ctx, stop, err := guardModelCall(t.Context(), checker, 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	checker.revoked.Store(true)
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("revocation did not cancel active call")
	}
	if !errors.Is(context.Cause(ctx), models.ErrModelUnavailable) {
		t.Fatal(context.Cause(ctx))
	}
	_, stop2, err := guardModelCall(t.Context(), checker, 1, 7)
	stop2()
	if !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("revoked dispatch accepted")
	}
	checker = &accessTestChecker{failure: errors.New("database unavailable")}
	_, stop3, err := guardModelCall(t.Context(), checker, 1, 7)
	stop3()
	if err == nil {
		t.Fatal("database failure opened access")
	}
}

type accessCheckedRepository struct {
	*stubLLMModelRepository
	*accessTestChecker
}

func TestModelAccessRevocationStopsUpstreamWithoutFailover(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmtBool(stream), func(t *testing.T) {
			checker := &accessTestChecker{}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"id\":\"test\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
					w.(http.Flusher).Flush()
				}
				checker.revoked.Store(true)
				select {
				case <-r.Context().Done():
				case <-time.After(4 * time.Second):
				}
			}))
			defer upstream.Close()
			items := routingModels()
			for i := range items {
				items[i].BaseURL = upstream.URL
				items[i].ProviderModelName = items[i].DisplayName
			}
			s := routingGateway(items, &stubAuditEventService{})
			s.modelRepo = &accessCheckedRepository{&stubLLMModelRepository{active: items}, checker}
			s.httpClient, s.streamHTTPClient = upstream.Client(), upstream.Client()
			req := routingRequest("hello")
			req.Stream = stream
			w := httptest.NewRecorder()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var err error
			if stream {
				_, err = s.StreamChatCompletions(ctx, 1, req, w)
			} else {
				_, _, err = s.ChatCompletions(ctx, 1, req)
			}
			if !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("revocation result: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("revocation retried %d upstream calls", calls.Load())
			}
			if stream && !strings.Contains(w.Body.String(), "model_unavailable") {
				t.Fatalf("missing stream notice: %s", w.Body.String())
			}
		})
	}
}

func fmtBool(value bool) string {
	if value {
		return "stream"
	}
	return "nonstream"
}
