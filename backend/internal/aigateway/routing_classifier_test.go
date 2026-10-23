package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

type testRoutingClassifier func(context.Context, RoutingClassificationInput) (RoutingClassification, error)

func (f testRoutingClassifier) Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
	return f(ctx, input)
}

func TestRoutingClassifierDefaultAndRuleParity(t *testing.T) {
	gateway := &service{}
	for _, prompt := range []string{"你好", "写一个 Python 函数", "设计分布式架构", "证明这个定理", strings.Repeat("你好", routingTextLimit)} {
		messages := routingRequest(prompt).Messages
		want := classifyRoutingDifficulty(messages)
		got, err := gateway.classifyRoutingTask(t.Context(), messages, false)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("default changed: %+v %+v %v", got, want, err)
		}
		rule, err := (RuleRoutingClassifier{}).Classify(t.Context(), newRoutingClassificationInput(messages))
		if err != nil || rule.RequiredTier != want.RequiredTier || rule.Score == nil || *rule.Score != want.Score || !reflect.DeepEqual(rule.Signals, want.Signals) || !reflect.DeepEqual(rule.Warnings, want.Warnings) {
			t.Fatalf("rule adapter changed: %+v %+v %v", rule, want, err)
		}
	}
}

func TestRoutingClassifierInputIsBoundedUserText(t *testing.T) {
	messages := []ChatMessage{{Role: "system", Content: "SYSTEM_SECRET"}}
	for i := 0; i < 12; i++ {
		messages = append(messages, ChatMessage{Role: "user", Content: "old"})
	}
	messages = append(messages,
		ChatMessage{Role: "assistant", Content: "ASSISTANT_SECRET"},
		ChatMessage{Role: "tool", Content: "TOOL_SECRET"},
		ChatMessage{Role: "USER", Content: []any{
			map[string]any{"type": "text", "text": strings.Repeat("中", 20000) + "TAIL"},
			map[string]any{"type": "image_url", "image_url": "IMAGE_SECRET"},
		}},
	)
	input := newRoutingClassificationInput(messages)
	last := input.Turns[len(input.Turns)-1]
	if len(input.Turns) != 8 || !last.HasNonText || !last.Truncated || utf8.RuneCountInString(last.Text) != routingTextLimit+1 || !strings.HasSuffix(last.Text, "TAIL") {
		t.Fatalf("bad bounded input: %d %+v", len(input.Turns), last)
	}
	raw, _ := json.Marshal(input)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("non-user text or image leaked to classifier")
	}
	input.Turns[0].Text = "changed"
	if messages[1].Content != "old" {
		t.Fatal("classifier input aliases request")
	}
}

func TestRoutingClassifierCustomSelectionAndAudit(t *testing.T) {
	audit := &stubAuditEventService{}
	gateway := routingGateway(routingModels(), audit)
	var calls atomic.Int32
	WithRoutingClassifier("test-tier", testRoutingClassifier(func(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
		calls.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			return RoutingClassification{}, errors.New("missing deadline")
		}
		confidence := .8
		return RoutingClassification{RequiredTier: 4, Confidence: &confidence, Signals: []string{"test_signal"}}, nil
	}), time.Second)(gateway)
	p, err := gateway.prepareChatRequest(t.Context(), 1, routingRequest("你好"))
	if err != nil || p.resolvedModel.ID != 2 || calls.Load() != 1 {
		t.Fatalf("custom classifier not used: %+v %v calls=%d", p, err, calls.Load())
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
		if d.Classifier != "test-tier" || d.RequiredTier != 4 || d.ClassifierConfidence == nil || *d.ClassifierConfidence != .8 || strings.Contains(*event.Details, "你好") {
			t.Fatalf("bad classifier audit: %+v", d)
		}
		found = true
	}
	if !found {
		t.Fatal("no classifier audit")
	}
}

func TestRoutingClassifierFallback(t *testing.T) {
	badConfidence := math.NaN()
	for _, tc := range []struct {
		name       string
		classifier testRoutingClassifier
	}{
		{"error", func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
			return RoutingClassification{}, errors.New("API_KEY_SECRET")
		}},
		{"panic", func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
			panic("API_KEY_SECRET")
		}},
		{"invalid_result", func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
			return RoutingClassification{RequiredTier: 5}, nil
		}},
		{"invalid_result", func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
			return RoutingClassification{RequiredTier: 1, Confidence: &badConfidence}, nil
		}},
		{"invalid_result", func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
			return RoutingClassification{RequiredTier: 1, Signals: []string{"user prompt / secret"}}, nil
		}},
		{"timeout", func(ctx context.Context, _ RoutingClassificationInput) (RoutingClassification, error) {
			<-ctx.Done()
			return RoutingClassification{}, ctx.Err()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &service{}
			WithRoutingClassifier("custom", tc.classifier, 20*time.Millisecond)(gateway)
			d, err := gateway.classifyRoutingTask(t.Context(), routingRequest("证明这个定理").Messages, false)
			if err != nil || d.Classifier != "heuristic" || d.RequestedClassifier != "custom" || d.ClassifierFallback != tc.name || d.RequiredTier != 4 {
				t.Fatalf("bad fallback: %+v %v", d, err)
			}
			raw, _ := json.Marshal(d)
			if strings.Contains(string(raw), "SECRET") {
				t.Fatal("adapter error leaked to audit")
			}
		})
	}
}

func TestRoutingClassifierDeadlineEvenWhenAdapterIgnoresContext(t *testing.T) {
	gateway := &service{}
	release := make(chan struct{})
	defer close(release)
	WithRoutingClassifier("stuck", testRoutingClassifier(func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
		<-release
		return RoutingClassification{RequiredTier: 4}, nil
	}), 20*time.Millisecond)(gateway)
	// One slot makes the bounded-worker check deterministic without 32 waits.
	gateway.routingClassifier.slots = make(chan struct{}, 1)
	started := time.Now()
	d, err := gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if err != nil || d.ClassifierFallback != "timeout" || time.Since(started) > time.Second {
		t.Fatalf("deadline not enforced: %+v %v", d, err)
	}
	d, err = gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if err != nil || d.ClassifierFallback != "busy" {
		t.Fatalf("stuck worker did not bound concurrency: %+v %v", d, err)
	}
}

func TestRoutingClassifierCancellationStopsRouting(t *testing.T) {
	for _, preCanceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "during", true: "before"}[preCanceled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			// Authorization now loads candidates before any external classification;
			// cancellation still must stop before selecting or dispatching a model.
			gateway := &service{modelRepo: &stubLLMModelRepository{active: routingModels()}}
			WithRoutingClassifier("cancel", testRoutingClassifier(func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
				calls.Add(1)
				cancel()
				return RoutingClassification{RequiredTier: 1}, nil
			}), time.Second)(gateway)
			if preCanceled {
				cancel()
			}
			model, _, err := gateway.selectContentModel(ctx, routingRequest("你好"), false)
			if model != nil || !errors.Is(err, context.Canceled) || (preCanceled && calls.Load() != 0) {
				t.Fatalf("canceled request routed: %+v %v", model, err)
			}
		})
	}
}

func TestRoutingClassifierConcurrentLimit(t *testing.T) {
	gateway := &service{}
	started := make(chan struct{}, maxRoutingClassifierInflight)
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	WithRoutingClassifier("concurrent", testRoutingClassifier(func(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
		started <- struct{}{}
		select {
		case <-release:
			return RoutingClassification{RequiredTier: 4}, nil
		case <-ctx.Done():
			return RoutingClassification{}, ctx.Err()
		}
	}), 5*time.Second)(gateway)
	type result struct {
		decision routingDecision
		err      error
	}
	results := make(chan result, maxRoutingClassifierInflight)
	for i := 0; i < maxRoutingClassifierInflight; i++ {
		go func() {
			d, err := gateway.classifyRoutingTask(ctx, routingRequest("你好").Messages, false)
			results <- result{d, err}
		}()
	}
	for i := 0; i < maxRoutingClassifierInflight; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("classifiers did not start")
		}
	}
	d, err := gateway.classifyRoutingTask(ctx, routingRequest("你好").Messages, false)
	if err != nil || d.ClassifierFallback != "busy" {
		t.Fatalf("concurrency bound not enforced: %+v %v", d, err)
	}
	close(release)
	for i := 0; i < maxRoutingClassifierInflight; i++ {
		select {
		case got := <-results:
			if got.err != nil || got.decision.Classifier != "concurrent" || got.decision.RequiredTier != 4 {
				t.Fatalf("concurrent result: %+v", got)
			}
		case <-ctx.Done():
			t.Fatal("classifiers did not finish")
		}
	}
}

func TestRoutingClassifierCannotBypassPolicy(t *testing.T) {
	for _, mode := range []string{"explicit", "block", "require_approval", "secure", "allowlist", "tools", "context"} {
		t.Run(mode, func(t *testing.T) {
			items := routingModels()
			req := routingRequest("你好")
			gateway := routingGateway(items, &stubAuditEventService{})
			var calls atomic.Int32
			WithRoutingClassifier("test", testRoutingClassifier(func(context.Context, RoutingClassificationInput) (RoutingClassification, error) {
				calls.Add(1)
				return RoutingClassification{RequiredTier: 1}, nil
			}), time.Second)(gateway)
			switch mode {
			case "explicit":
				req.Model = "capable"
			case "block", "require_approval":
				gateway.riskDetector = routingRiskDetector{analysis: services.RiskAnalysis{IsSensitive: true, HighestAction: mode}}
			case "secure":
				items[0].IsSecure = false
				gateway.riskDetector = routingRiskDetector{analysis: services.RiskAnalysis{IsSensitive: true, HighestAction: models.RiskActionRouteSecureModel}}
			case "allowlist":
				req.AllowedModelGroups = []string{"advanced"}
			case "tools":
				no := false
				items[0].ToolsSupportOverride = &no
				req.Tools = []byte(`[{"type":"function","function":{"name":"test"}}]`)
			case "context":
				items[0].ContextWindowTokens = 1
			}
			prepared, err := gateway.prepareChatRequest(t.Context(), 1, req)
			if mode == "block" || mode == "require_approval" {
				if err == nil || calls.Load() != 0 {
					t.Fatalf("blocked request classified: %v calls=%d", err, calls.Load())
				}
				return
			}
			if err != nil || prepared.resolvedModel.ID != 2 {
				t.Fatalf("policy bypass: %+v %v", prepared, err)
			}
			if (mode == "explicit" || mode == "secure") && calls.Load() != 0 {
				t.Fatal("explicit or sensitive request sent to adapter")
			}
		})
	}
}

func TestRoutingClassifierInputMutationDoesNotChangeFallback(t *testing.T) {
	gateway := &service{}
	WithRoutingClassifier("mutator", testRoutingClassifier(func(_ context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
		input.Turns[0].Text = "你好"
		return RoutingClassification{}, errors.New("failed")
	}), time.Second)(gateway)
	request := routingRequest("证明这个定理")
	d, err := gateway.classifyRoutingTask(t.Context(), request.Messages, false)
	if err != nil || d.RequiredTier != 4 || request.Messages[0].Content != "证明这个定理" {
		t.Fatalf("mutated fallback: %+v %v", d, err)
	}
}

func TestRoutingClassifierResultValidationAndOptions(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 1.1} {
		if validRoutingClassification(RoutingClassification{RequiredTier: 1, Score: &value}) || validRoutingClassification(RoutingClassification{RequiredTier: 1, Confidence: &value}) {
			t.Fatal("invalid numeric output accepted")
		}
	}
	if validRoutingClassification(RoutingClassification{RequiredTier: 0}) || validRoutingClassification(RoutingClassification{RequiredTier: 1, Warnings: make([]string, 33)}) {
		t.Fatal("invalid result accepted")
	}
	gateway := &service{}
	WithRoutingClassifier("invalid name", RuleRoutingClassifier{}, 0)(gateway)
	if gateway.routingClassifier.name != "custom" || gateway.routingClassifier.timeout != defaultRoutingClassifierTimeout {
		t.Fatal("unsafe option defaults")
	}
	WithRoutingClassifier("", nil, 0)(gateway)
	d, err := gateway.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if err != nil || gateway.routingClassifier != nil || d.Classifier != "heuristic" || slices.Contains(d.Warnings, "classifier_fallback_error") {
		t.Fatal("nil option did not restore rules")
	}
}
