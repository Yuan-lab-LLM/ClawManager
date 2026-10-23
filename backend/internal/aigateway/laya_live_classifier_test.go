package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func liveLayaGateway(t *testing.T, settings *ClassifierSettingsService) *service {
	t.Helper()
	option, err := settings.LiveRoutingOption("laya")
	if err != nil {
		t.Fatal(err)
	}
	gateway := &service{}
	option(gateway)
	return gateway
}

func TestLayaLiveGateDefaultOff(t *testing.T) {
	for _, mode := range []string{"", "rules", " RULES "} {
		s, _ := classifierTestService()
		option, err := s.LiveRoutingOption(mode)
		if err != nil {
			t.Fatal(err)
		}
		gateway := &service{}
		option(gateway)
		view, _ := s.Get(t.Context())
		if gateway.routingClassifier != nil || view.Mode != "manual_test_only" {
			t.Fatal("default must not send live requests")
		}
	}
	s, _ := classifierTestService()
	if _, err := s.LiveRoutingOption("typo"); err == nil {
		t.Fatal("unknown mode must fail startup")
	}
}

func TestLayaLiveAutoSelectionAndAudit(t *testing.T) {
	s, _ := classifierTestService()
	_, _ = s.Save(t.Context(), classifierTestDraft())
	calls := 0
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		return testLaya(t, func(*http.Request) (*http.Response, error) {
			calls++
			return layaResponse(200, layaValidResponse), nil // tier 2, unlike rule's tier 1
		}), nil
	}
	audit := &stubAuditEventService{}
	gateway := routingGateway(routingModels(), audit)
	option, _ := s.LiveRoutingOption("laya")
	option(gateway)
	prepared, err := gateway.prepareChatRequest(t.Context(), 1, routingRequest("你好"))
	if err != nil || prepared.resolvedModel.ID != 2 || calls != 1 {
		t.Fatalf("Auto did not use Laya tier to select model: %+v %v", prepared, err)
	}
	found := false
	for _, event := range audit.events {
		if event.EventType != "gateway.routing.selected" || event.Details == nil {
			continue
		}
		var d routingDecision
		if err := json.Unmarshal([]byte(*event.Details), &d); err != nil {
			t.Fatal(err)
		}
		if d.Classifier != "laya" || d.RequiredTier != 2 || d.ModelID != 2 || strings.Contains(*event.Details, "test-secret") {
			t.Fatal("wrong audit", d)
		}
		found = true
	}
	if !found {
		t.Fatal("missing live routing audit")
	}
	req := routingRequest("你好")
	req.Model = "capable"
	if _, err := gateway.prepareChatRequest(t.Context(), 1, req); err != nil || calls != 1 {
		t.Fatal("explicit selection must bypass Laya", err)
	}
}

func TestLayaLiveUsesSavedSettingsAndRefreshesCredentials(t *testing.T) {
	s, _ := classifierTestService()
	draft := classifierTestDraft()
	if _, err := s.Save(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	constructs, calls := 0, 0
	lastKey := ""
	s.newClassifier = func(url, key string, timeout time.Duration) (*LayaClassifier, error) {
		constructs++
		lastKey = key
		return testLaya(t, func(*http.Request) (*http.Response, error) {
			calls++
			return layaResponse(200, layaValidResponse), nil
		}), nil
	}
	gateway := liveLayaGateway(t, s)
	view, _ := s.Get(t.Context())
	if view.Mode != "laya_auto" {
		t.Fatal(view)
	}
	for i := 0; i < 2; i++ {
		d, err := gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
		if err != nil || d.Classifier != "laya" || d.RequestedClassifier != "laya" || d.RequiredTier != 2 || len(d.Signals) != 1 || d.Signals[0] != layaPromptVersion || d.ClassifierFallback != "" || d.ClassifierConfidenceKind != "normalized_entropy" {
			t.Fatalf("live classifier not used: %+v %v", d, err)
		}
	}
	if constructs != 1 || calls != 2 || lastKey != "test-secret" {
		t.Fatal("transport not reused or wrong saved key")
	}
	newKey := "rotated-test-key"
	draft.APIKey = &newKey
	if _, err := s.Save(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	_, _ = gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if constructs != 2 || calls != 3 || lastKey != newKey {
		t.Fatal("updated key was not applied")
	}
	draft.APIKey, draft.ClearAPIKey = nil, true
	if _, err := s.Save(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	d, err := gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if err != nil || d.Classifier != "heuristic" || d.ClassifierFallback != "configuration_missing" || calls != 3 {
		t.Fatal("cleared key still used", d, err)
	}
}

func TestLayaLiveFallbackReasonsAndSafety(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		secure     bool
		configure  func(*ClassifierSettingsService, *classifierMemoryRepo)
	}{
		{"configuration_missing", "你好", false, func(s *ClassifierSettingsService, r *classifierMemoryRepo) { r.row.EncryptedAPIKey = "" }},
		{"storage_error", "你好", false, func(s *ClassifierSettingsService, r *classifierMemoryRepo) {
			r.err = errors.New("private database credential")
		}},
		{"input_too_long", strings.Repeat("中", 500), false, nil},
		{"secure_policy_local_only", "你好", true, nil},
		{"busy", "你好", false, func(s *ClassifierSettingsService, r *classifierMemoryRepo) {
			for i := 0; i < cap(s.slots); i++ {
				s.slots <- struct{}{}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo := classifierTestService()
			_, _ = s.Save(t.Context(), classifierTestDraft())
			s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
				t.Error("must not construct/send Laya request")
				return nil, layaError("connection_failed")
			}
			if tc.configure != nil {
				tc.configure(s, repo)
			}
			gateway := liveLayaGateway(t, s)
			d, err := gateway.classifyRoutingTask(t.Context(), routingRequest(tc.text).Messages, tc.secure)
			requested := "laya"
			if tc.name == "storage_error" {
				requested = "dynamic"
			}
			if err != nil || d.Classifier != "heuristic" || d.ClassifierFallback != tc.name || d.RequestedClassifier != requested {
				t.Fatalf("%+v %v", d, err)
			}
		})
	}
}

func TestLayaLiveFailureTimeoutAndCancel(t *testing.T) {
	for _, expected := range []string{"http_401", "timeout", "canceled"} {
		t.Run(expected, func(t *testing.T) {
			s, _ := classifierTestService()
			draft := classifierTestDraft()
			draft.TimeoutMS = 100
			_, _ = s.Save(t.Context(), draft)
			s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
				return testLaya(t, func(r *http.Request) (*http.Response, error) {
					if expected == "http_401" {
						return layaResponse(401, "private upstream body"), nil
					}
					<-r.Context().Done()
					return nil, r.Context().Err()
				}), nil
			}
			gateway := liveLayaGateway(t, s)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if expected == "canceled" {
				cancel()
			}
			d, err := gateway.classifyRoutingTask(ctx, routingRequest("你好").Messages, false)
			if expected == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || d.ClassifierFallback != expected || d.Classifier != "heuristic" {
				t.Fatal(d, err)
			}
			raw, _ := json.Marshal(d)
			if strings.Contains(string(raw), "private") {
				t.Fatal("upstream body leaked")
			}
		})
	}
	if layaFallbackReason(layaError("secret_content")) != "error" {
		t.Fatal("untrusted code passed to audit")
	}
}
