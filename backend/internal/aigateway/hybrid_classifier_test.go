package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func hybridGateway(t *testing.T, settings *ClassifierSettingsService) *service {
	t.Helper()
	option, err := settings.LiveRoutingOption(" HYBRID ")
	if err != nil {
		t.Fatal(err)
	}
	s := &service{}
	option(s)
	return s
}

func TestHybridRuleCoverage(t *testing.T) {
	for _, tc := range []struct {
		text, status, reason string
		tier                 int
	}{
		{"你好！", "matched", "explicit_task_pattern", 1},
		{"23 * 4", "matched", "explicit_task_pattern", 1},
		{"帮我写一个 Python 排序函数", "matched", "explicit_task_pattern", 2},
		{"帮我写一个Python排序函数。", "matched", "explicit_task_pattern", 2},
		{"Please write a Python function to sort a list.", "matched", "explicit_task_pattern", 2},
		{"这个该怎么办", "unmatched", "no_rule_match", 1},
		{"分析一下", "ambiguous", "weak_rule_match", 2},
		{"继续", "ambiguous", "context_required", 2},
		{"帮我写一个 Python 排序函数，然后证明正确性", "ambiguous", "compound_task", 4},
		{"帮我写一个 Python 排序函数，支持分布式处理和零停机迁移", "ambiguous", "weak_rule_match", 2},
		{"翻译：帮我写一个 Python 排序函数", "ambiguous", "weak_rule_match", 1},
		{"不要帮我写一个 Python 排序函数", "ambiguous", "weak_rule_match", 2},
		{"帮我写一个 Python 排序函数\n```\nextra requirements\n```", "ambiguous", "weak_rule_match", 2},
	} {
		t.Run(tc.text, func(t *testing.T) {
			in := RoutingClassificationInput{Turns: []RoutingTaskTurn{{Text: tc.text}}}
			d := classifyRoutingTurns(in)
			a := assessRoutingRules(in, d)
			if a.Status != tc.status || a.Reason != tc.reason || d.RequiredTier != tc.tier {
				t.Fatalf("assessment=%+v tier=%d", a, d.RequiredTier)
			}
		})
	}
	for _, turn := range []RoutingTaskTurn{{Text: "你好", HasNonText: true}, {Text: "你好", Truncated: true}} {
		in := RoutingClassificationInput{Turns: []RoutingTaskTurn{turn}}
		if a := assessRoutingRules(in, classifyRoutingTurns(in)); a.Status == "matched" || a.Reason != "unsupported_input" {
			t.Fatal(a)
		}
	}
	if a := assessRoutingRules(RoutingClassificationInput{}, routingDecision{}); a.Status != "unmatched" {
		t.Fatal(a)
	}
	// New standalone tasks need not inherit the difficulty of old user turns.
	in := RoutingClassificationInput{Turns: []RoutingTaskTurn{{Text: "严格证明根号二是无理数"}, {Text: "帮我写一个Python排序函数"}}}
	if a := assessRoutingRules(in, classifyRoutingTurns(in)); a.Status != "matched" {
		t.Fatal(a)
	}
}

func TestHybridClearMatchBypassesLayaAndBusySlots(t *testing.T) {
	s, repo := classifierTestService()
	g := hybridGateway(t, s)
	for i := 0; i < cap(g.routingClassifier.slots); i++ {
		g.routingClassifier.slots <- struct{}{}
	}
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		t.Error("must not construct Laya")
		return nil, layaError("connection_failed")
	}
	d, err := g.classifyRoutingTask(t.Context(), routingRequest("帮我写一个 Python 排序函数").Messages, false)
	if err != nil || d.RequiredTier != 2 || d.Classifier != "heuristic" || d.RequestedClassifier != "hybrid" || d.ClassifierFallback != "" || d.RuleAssessment.Status != "matched" {
		t.Fatalf("%+v %v", d, err)
	}
	view := s.view(&repo.row)
	if view.Mode != "hybrid_auto" {
		t.Fatal(view)
	}
}

func TestHybridUnresolvedUsesLayaAndAuditsOrigin(t *testing.T) {
	s, _ := classifierTestService()
	if _, err := s.Save(t.Context(), classifierTestDraft()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		return testLaya(t, func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return layaResponse(200, layaValidResponse), nil
		}), nil
	}
	audit := &stubAuditEventService{}
	g := routingGateway(routingModels(), audit)
	option, _ := s.LiveRoutingOption("hybrid")
	option(g)
	prepared, err := g.prepareChatRequest(t.Context(), 1, routingRequest("陌生任务"))
	if err != nil || prepared.resolvedModel.ID != 2 || calls.Load() != 1 {
		t.Fatalf("%+v %v calls=%d", prepared, err, calls.Load())
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
		if d.RequestedClassifier != "hybrid" || d.Classifier != "laya" || d.RuleAssessment.Status != "unmatched" || d.Policy != "hybrid-classifier-v1" || d.ClassifierConfidenceKind != "normalized_entropy" {
			t.Fatal(d)
		}
		if strings.Contains(*event.Details, "陌生任务") || strings.Contains(*event.Details, "test-secret") {
			t.Fatal("audit leaked content")
		}
		found = true
	}
	if !found {
		t.Fatal("missing audit")
	}
	req := routingRequest("陌生任务")
	req.Model = "capable"
	if _, err := g.prepareChatRequest(t.Context(), 1, req); err != nil || calls.Load() != 1 {
		t.Fatal("explicit model called Laya", err)
	}
}

func TestHybridFallbackAndPreflight(t *testing.T) {
	for _, code := range []string{"configuration_missing", "storage_error", "input_too_long", "unsupported_input", "secure_policy_local_only", "busy", "http_401", "invalid_response", "timeout"} {
		t.Run(code, func(t *testing.T) {
			s, repo := classifierTestService()
			draft := classifierTestDraft()
			draft.TimeoutMS = 100
			if _, err := s.Save(t.Context(), draft); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
				return testLaya(t, func(req *http.Request) (*http.Response, error) {
					calls.Add(1)
					switch code {
					case "http_401":
						return layaResponse(401, "secret body"), nil
					case "invalid_response":
						return layaResponse(200, `{}`), nil
					case "timeout":
						<-req.Context().Done()
						return nil, req.Context().Err()
					default:
						t.Error("unexpected HTTP call")
						return layaResponse(200, layaValidResponse), nil
					}
				}), nil
			}
			g := hybridGateway(t, s)
			messages := routingRequest("陌生任务").Messages
			switch code {
			case "configuration_missing":
				repo.row.EncryptedAPIKey = ""
			case "storage_error":
				repo.err = errors.New("secret database message")
			case "input_too_long":
				messages = routingRequest(strings.Repeat("中", 500)).Messages
			case "unsupported_input":
				messages = []ChatMessage{{Role: "user", Content: []interface{}{map[string]interface{}{"type": "image_url", "image_url": "private"}}}}
			case "busy":
				for i := 0; i < cap(g.routingClassifier.slots); i++ {
					g.routingClassifier.slots <- struct{}{}
				}
			}
			d, err := g.classifyRoutingTask(t.Context(), messages, code == "secure_policy_local_only")
			requested := "hybrid"
			if code == "storage_error" {
				requested = "dynamic"
			}
			if err != nil || d.Classifier != "heuristic" || d.RequestedClassifier != requested || d.ClassifierFallback != code || (code != "storage_error" && d.RuleAssessment == nil) {
				t.Fatalf("%+v %v", d, err)
			}
			if code != "http_401" && code != "invalid_response" && code != "timeout" && calls.Load() != 0 {
				t.Fatal("preflight sent HTTP")
			}
			raw, _ := json.Marshal(d)
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private") {
				t.Fatal("unsafe audit", string(raw))
			}
		})
	}
}

func TestHybridCancellation(t *testing.T) {
	s, _ := classifierTestService()
	g := hybridGateway(t, s)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, text := range []string{"你好", "陌生任务"} {
		if _, err := g.classifyRoutingTask(ctx, routingRequest(text).Messages, false); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestHybridInFlightCancellationDoesNotFallback(t *testing.T) {
	s, _ := classifierTestService()
	if _, err := s.Save(t.Context(), classifierTestDraft()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	stopped := make(chan struct{})
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		return testLaya(t, func(req *http.Request) (*http.Response, error) {
			close(started)
			<-req.Context().Done()
			close(stopped)
			return nil, req.Context().Err()
		}), nil
	}
	g := hybridGateway(t, s)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := g.classifyRoutingTask(ctx, routingRequest("陌生任务").Messages, false)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation became fallback", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("classification ignored cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request ignored cancellation")
	}
}

func TestHybridManualPreview(t *testing.T) {
	for _, tc := range []struct {
		text, response, source, reason string
		need                           bool
		tier                           int
	}{
		{"帮我写一个Python排序函数", layaValidResponse, "rules", "", false, 2},
		{"陌生任务", layaValidResponse, "laya", "", true, 2},
		{"陌生任务", `{}`, "rules_fallback", "invalid_response", true, 1},
		{"你好", `{}`, "rules", "", false, 1},
		{strings.Repeat("中", 500), layaValidResponse, "rules_fallback", "input_too_long", false, 1},
	} {
		t.Run(tc.source+tc.reason+tc.text[:3], func(t *testing.T) {
			s, repo := classifierTestService()
			s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
				return testLaya(t, func(*http.Request) (*http.Response, error) { return layaResponse(200, tc.response), nil }), nil
			}
			draft := classifierTestDraft()
			out, err := s.Compare(t.Context(), ClassifierCompareRequest{Text: tc.text, Draft: &draft})
			if err != nil || out.Hybrid == nil {
				t.Fatalf("%+v %v", out, err)
			}
			p := out.Hybrid
			if p.Source != tc.source || p.RequiredTier != tc.tier || p.WouldCallLaya != tc.need || p.FallbackReason != tc.reason || out.ProductionRoutingChanged || repo.saves != 0 {
				t.Fatalf("%+v", p)
			}
			view, _ := s.Get(t.Context())
			if view.Mode != "manual_test_only" {
				t.Fatal("comparison enabled routing")
			}
		})
	}
}
