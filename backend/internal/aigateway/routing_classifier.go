package aigateway

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// RoutingTaskTurn contains only user text, not raw image data, system prompts,
// tool definitions/results, credentials, model records or caller permissions.
type RoutingTaskTurn struct {
	Text       string `json:"text"`
	HasNonText bool   `json:"has_non_text"`
	Truncated  bool   `json:"truncated"`
}

// RoutingClassificationInput holds at most eight user turns, oldest first.
// Each turn is bounded to 16,000 runes plus a head/tail separator. Adapters for
// smaller models must additionally enforce their own tokenizer-based budget.
type RoutingClassificationInput struct {
	Turns []RoutingTaskTurn `json:"turns"`
}

// RoutingClassification is a recommendation, never an authorization decision.
// RequiredTier must be 1..4. Score and Confidence, when supplied, must be finite
// values in [0,1]; heuristic scores are NOT model confidence probabilities.
// Signals/Warnings are short machine-readable codes, not explanations/prompts.
type RoutingClassification struct {
	RequiredTier int      `json:"required_tier"`
	Score        *float64 `json:"score,omitempty"`
	Confidence   *float64 `json:"confidence,omitempty"`
	Signals      []string `json:"signals,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	// Optional diagnostics, not routing thresholds. The probability map is only
	// returned to the comparison UI; the kind also disambiguates audit confidence.
	ConfidenceKind string             `json:"confidence_kind,omitempty"`
	Probabilities  map[string]float64 `json:"probabilities,omitempty"`
	RuleAssessment *RuleAssessment    `json:"rule_assessment,omitempty"`
}

// RoutingClassifier implementations must be safe for concurrent calls, respect
// ctx cancellation, and relinquish ownership of returned slices/pointers.
// Register trusted adapters at service construction; this is not a dynamic code
// loader or an HTTP endpoint. Network adapters need explicit operator setup.
type RoutingClassifier interface {
	Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error)
}

// RuleRoutingClassifier runs the local, versioned content-cost heuristics.
type RuleRoutingClassifier struct{}

func (RuleRoutingClassifier) Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
	if err := ctx.Err(); err != nil {
		return RoutingClassification{}, err
	}
	d := classifyRoutingTurns(input)
	return RoutingClassification{RequiredTier: d.RequiredTier, Score: &d.Score, Signals: d.Signals, Warnings: d.Warnings, RuleAssessment: assessRoutingRules(input, d)}, nil
}

const defaultRoutingClassifierTimeout = 2 * time.Second
const maxRoutingClassifierInflight = 32

var classifierCode = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,95}$`)
var errRoutingClassifierPanic = errors.New("routing classifier panicked")

type routingClassifierRunner struct {
	name       string
	classifier RoutingClassifier
	timeout    time.Duration
	slots      chan struct{}
	rulesFirst bool
}

// WithRoutingClassifier replaces classification for eligible Auto requests.
// Nil restores the local heuristic. Non-positive timeout uses two seconds.
// Configure only at construction, never concurrently with serving requests.
// Failed/invalid/busy classifiers fall back locally; caller cancellation does
// not fall back and never dispatches an upstream completion. Security-forced
// requests stay on local rules so sensitive text is not sent to an adapter.
func WithRoutingClassifier(name string, classifier RoutingClassifier, timeout time.Duration) ServiceOption {
	name = strings.TrimSpace(name)
	if !classifierCode.MatchString(name) || name == "heuristic" {
		name = "custom"
	}
	if timeout <= 0 {
		timeout = defaultRoutingClassifierTimeout
	}
	return func(target *service) {
		target.resolveRoutingClassifier = nil
		if classifier == nil {
			target.routingClassifier = nil
			return
		}
		target.routingClassifier = &routingClassifierRunner{
			name: name, classifier: classifier, timeout: timeout,
			slots: make(chan struct{}, maxRoutingClassifierInflight),
		}
	}
}

func newRoutingClassificationInput(messages []ChatMessage) RoutingClassificationInput {
	turns := make([]RoutingTaskTurn, 0, routingHistoryLimit)
	for i := len(messages) - 1; i >= 0 && len(turns) < routingHistoryLimit; i-- {
		if !strings.EqualFold(messages[i].Role, "user") {
			continue
		}
		text, nonText := routingUserText(messages[i].Content)
		turn := RoutingTaskTurn{Text: text, HasNonText: nonText}
		if utf8.RuneCountInString(text) > routingTextLimit {
			runes := []rune(text)
			turn.Text = string(runes[:12000]) + "\n" + string(runes[len(runes)-4000:])
			turn.Truncated = true
		}
		turns = append(turns, turn)
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return RoutingClassificationInput{Turns: turns}
}

func (s *service) classifyRoutingTask(ctx context.Context, messages []ChatMessage, secureRequired bool) (decision routingDecision, err error) {
	start := time.Now()
	defer func() {
		if decision.RequestedClassifier == "hybrid" {
			decision.ClassificationDurationMS = float64(time.Since(start).Microseconds()) / 1000
		}
	}()
	if err := ctx.Err(); err != nil {
		return routingDecision{}, err
	}
	input := newRoutingClassificationInput(messages)
	inputWarnings := []string{}
	for _, turn := range input.Turns {
		if turn.Truncated {
			appendRoutingUnique(&inputWarnings, "difficulty_text_truncated")
		}
		if turn.HasNonText {
			appendRoutingUnique(&inputWarnings, "non_text_content_not_classified")
		}
	}
	// Compute fallback before handing the detached input to a custom adapter.
	// The adapter cannot mutate the request or change fallback classification.
	fallback := classifyRoutingTurns(input)
	runner := s.routingClassifier
	if s.resolveRoutingClassifier != nil {
		var err error
		runner, err = s.resolveRoutingClassifier(ctx)
		if ctx.Err() != nil {
			return fallback, ctx.Err()
		}
		if err != nil {
			fallback.RequestedClassifier = "dynamic"
			fallback.ClassifierFallback = layaFallbackReason(err)
			appendRoutingUnique(&fallback.Warnings, "classifier_fallback_"+fallback.ClassifierFallback)
			return fallback, nil
		}
	}
	if runner == nil {
		return fallback, ctx.Err()
	}
	fallback.RequestedClassifier = runner.name
	if runner.rulesFirst {
		fallback.Policy = "hybrid-classifier-v1"
		fallback.RequestedClassifier = "hybrid"
		fallback.RuleAssessment = assessRoutingRules(input, fallback)
	}
	useFallback := func(reason string) (routingDecision, error) {
		fallback.ClassifierFallback = reason
		appendRoutingUnique(&fallback.Warnings, "classifier_fallback_"+reason)
		return fallback, ctx.Err()
	}
	if secureRequired {
		return useFallback("secure_policy_local_only")
	}
	if runner.rulesFirst {
		if fallback.RuleAssessment.Status == "matched" {
			return fallback, ctx.Err()
		}
		// No slot, database lookup or external request for unsupported input.
		if _, _, err := layaPayload(input); err != nil {
			return useFallback(layaFallbackReason(err))
		}
	}
	select {
	case runner.slots <- struct{}{}:
	default:
		return useFallback("busy")
	}
	callCtx, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()
	type outcome struct {
		result RoutingClassification
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		var out outcome
		defer func() {
			if recover() != nil {
				out = outcome{err: errRoutingClassifierPanic}
			}
			done <- out
			<-runner.slots
		}()
		out.result, out.err = runner.classifier.Classify(callCtx, input)
	}()
	var out outcome
	select {
	case <-callCtx.Done():
	case out = <-done:
	}
	if err := ctx.Err(); err != nil {
		return fallback, err
	}
	if callCtx.Err() != nil || errors.Is(out.err, context.DeadlineExceeded) {
		return useFallback("timeout")
	}
	if errors.Is(out.err, errRoutingClassifierPanic) {
		return useFallback("panic")
	}
	if out.err != nil {
		if runner.name == "laya" {
			return useFallback(layaFallbackReason(out.err))
		}
		// Never persist adapter errors: they can include credentials or prompts.
		return useFallback("error")
	}
	if !validRoutingClassification(out.result) {
		return useFallback("invalid_result")
	}
	d := routingDecision{
		Policy: "pluggable-classifier-v1", Classifier: runner.name, RequestedClassifier: runner.name,
		RequiredTier: out.result.RequiredTier,
		Difficulty:   []string{"", "simple", "medium", "complex", "reasoning"}[out.result.RequiredTier],
		Signals:      append([]string{}, out.result.Signals...), Warnings: append([]string{}, out.result.Warnings...),
		Rejections: []routingRejection{},
	}
	if runner.rulesFirst {
		d.Policy, d.RequestedClassifier = "hybrid-classifier-v1", "hybrid"
		d.RuleAssessment = fallback.RuleAssessment
	}
	if out.result.Score != nil {
		d.Score = *out.result.Score
	}
	if out.result.Confidence != nil {
		confidence := *out.result.Confidence
		d.ClassifierConfidence = &confidence
		d.ClassifierConfidenceKind = out.result.ConfidenceKind
	}
	for _, warning := range inputWarnings {
		appendRoutingUnique(&d.Warnings, warning)
	}
	return d, nil
}

func validRoutingClassification(result RoutingClassification) bool {
	if result.RequiredTier < 1 || result.RequiredTier > 4 {
		return false
	}
	for _, value := range []*float64{result.Score, result.Confidence} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1) {
			return false
		}
	}
	if result.ConfidenceKind != "" && !classifierCode.MatchString(result.ConfidenceKind) {
		return false
	}
	if len(result.Probabilities) > 32 {
		return false
	}
	for key, value := range result.Probabilities {
		if !classifierCode.MatchString(key) || !finiteProbability(value) {
			return false
		}
	}
	for _, codes := range [][]string{result.Signals, result.Warnings} {
		if len(codes) > 32 {
			return false
		}
		for _, code := range codes {
			if !classifierCode.MatchString(code) {
				return false
			}
		}
	}
	return true
}
