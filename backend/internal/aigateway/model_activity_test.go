package aigateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"clawreef/internal/models"
)

type activityRecorder struct {
	begins      atomic.Int32
	finishes    atomic.Int32
	beginError  error
	finishError error
}

func (a *activityRecorder) BeginModelActivity(context.Context, int, int, int, string) (string, error) {
	a.begins.Add(1)
	return "server-scope", a.beginError
}
func (a *activityRecorder) FinishModelActivity(context.Context, string) error {
	a.finishes.Add(1)
	return a.finishError
}

func TestModelActivityPublicEntryPointsFailClosed(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmtBool(stream), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Write([]byte(`{"choices":[],"usage":{}}`))
			}))
			defer upstream.Close()
			items := routingModels()
			for i := range items {
				items[i].BaseURL = upstream.URL
			}
			s := routingGateway(items, &stubAuditEventService{})
			s.httpClient = upstream.Client()
			s.streamHTTPClient = upstream.Client()
			activity := &activityRecorder{beginError: errors.New("database unavailable")}
			WithModelActivity(activity)(s)
			req := routingRequest("hello")
			req.Model = "cheap"
			id := 1
			req.InstanceID = &id
			var err error
			if stream {
				_, err = s.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder())
			} else {
				_, _, err = s.ChatCompletions(t.Context(), 1, req)
			}
			if !errors.Is(err, models.ErrActivityUnavailable) || calls.Load() != 0 || activity.begins.Load() != 1 || activity.finishes.Load() != 0 {
				t.Fatalf("err=%v provider=%d admission=%d finish=%d", err, calls.Load(), activity.begins.Load(), activity.finishes.Load())
			}
		})
	}
}

func TestModelActivityCountsProviderFailuresAndFinishes(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmtBool(stream), func(t *testing.T) {
			activity := &activityRecorder{}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if activity.begins.Load() != 1 || activity.finishes.Load() != 0 {
					t.Error("provider dispatched outside activity scope")
				}
				calls.Add(1)
				http.Error(w, "upstream unavailable", 503)
			}))
			defer upstream.Close()
			items := routingModels()[:1]
			items[0].BaseURL = upstream.URL
			s := routingGateway(items, &stubAuditEventService{})
			s.httpClient = upstream.Client()
			s.streamHTTPClient = upstream.Client()
			WithModelActivity(activity)(s)
			req := routingRequest("hello")
			req.Model = "cheap"
			id := 1
			req.InstanceID = &id
			if stream {
				s.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder())
			} else {
				s.ChatCompletions(t.Context(), 1, req)
			}
			if calls.Load() != 1 || activity.begins.Load() != 1 || activity.finishes.Load() != 1 {
				t.Fatalf("provider=%d admission=%d finish=%d", calls.Load(), activity.begins.Load(), activity.finishes.Load())
			}
		})
	}
}

func TestModelActivityInvalidChatDoesNotRecord(t *testing.T) {
	activity := &activityRecorder{}
	s := routingGateway(routingModels(), &stubAuditEventService{})
	WithModelActivity(activity)(s)
	id := 1
	_, _, err := s.ChatCompletions(t.Context(), 1, ChatCompletionRequest{Model: "cheap", InstanceID: &id})
	if err == nil || activity.begins.Load() != 0 {
		t.Fatalf("invalid chat recorded: %v / %d", err, activity.begins.Load())
	}
}

func TestModelActivityImageFailsBeforeProvider(t *testing.T) {
	var calls atomic.Int32
	s, _, _, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	activity := &activityRecorder{beginError: models.ErrActivityUnavailable}
	WithModelActivity(activity)(s)
	_, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr)
	if !errors.Is(err, models.ErrActivityUnavailable) || calls.Load() != 0 {
		t.Fatalf("image fail-open: %v calls=%d", err, calls.Load())
	}
}

func TestModelActivityHTTPScopedRequestRecordsOnlyOnce(t *testing.T) {
	activity := &activityRecorder{}
	s := routingGateway(routingModels(), &stubAuditEventService{})
	WithModelActivity(activity)(s)
	req := routingRequest("hello")
	id := 1
	req.InstanceID = &id
	ctx, finish, err := s.BeginModelActivityScope(t.Context(), 1, req)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := s.beginChatModelActivity(ctx, 1, &req)
	if err != nil {
		t.Fatal(err)
	}
	inner()
	if activity.begins.Load() != 1 || activity.finishes.Load() != 0 {
		t.Fatal("inference duplicated or prematurely finished HTTP scope")
	}
	if _, err = s.beginModelActivity(ctx, 2, req); !errors.Is(err, models.ErrInstanceStopping) {
		t.Fatal("scope reused for another identity")
	}
	finish()
	if activity.finishes.Load() != 1 {
		t.Fatal("outer HTTP scope was not finished")
	}
}

func TestModelActivityCancellationKeepsScopeUntilUpstreamCleanup(t *testing.T) {
	activity := &activityRecorder{}
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		close(entered)
		<-release
	}))
	defer upstream.Close()
	items := routingModels()[:1]
	items[0].BaseURL = upstream.URL
	s := routingGateway(items, &stubAuditEventService{})
	s.streamHTTPClient = upstream.Client()
	WithModelActivity(activity)(s)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := routingRequest("hello")
	req.Model = "cheap"
	req.Stream = true
	id := 1
	req.InstanceID = &id
	scoped, finish, err := s.BeginModelActivityScope(ctx, 1, req)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.StreamChatCompletions(scoped, 1, req, httptest.NewRecorder()); close(done) }()
	<-entered
	cancel()
	<-done
	if activity.finishes.Load() != 0 {
		t.Fatal("inference prematurely closed HTTP-owned activity")
	}
	close(release)
	finish()
	if activity.begins.Load() != 1 || activity.finishes.Load() != 1 {
		t.Fatal("cancelled scope not settled after cleanup")
	}
}
