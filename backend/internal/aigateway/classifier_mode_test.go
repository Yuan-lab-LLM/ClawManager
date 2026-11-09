package aigateway

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifierModeRequiresExplicitConsentAndSavedConnection(t *testing.T) {
	s, repo := classifierTestService()
	for _, tc := range []struct {
		draft ClassifierModeDraft
		code  string
	}{
		{ClassifierModeDraft{Mode: "typo", ConfirmExternal: true}, "invalid_mode"},
		{ClassifierModeDraft{Mode: "hybrid"}, "external_confirmation_required"},
		{ClassifierModeDraft{Mode: "laya"}, "external_confirmation_required"},
		{ClassifierModeDraft{Mode: "hybrid", ConfirmExternal: true}, "configuration_missing"},
	} {
		if _, err := s.SaveMode(t.Context(), tc.draft); ClassifierErrorCode(err) != tc.code {
			t.Fatal(tc, err)
		}
	}
	if repo.saves != 0 {
		t.Fatal("invalid mode update persisted")
	}
	if _, err := s.Save(t.Context(), classifierTestDraft()); err != nil {
		t.Fatal(err)
	}
	if repo.row.RoutingMode != "" {
		t.Fatal("connection save activated routing")
	}
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		t.Error("mode update must not send a model request")
		return nil, layaError("connection_failed")
	}
	view, err := s.SaveMode(t.Context(), ClassifierModeDraft{Mode: "hybrid", ConfirmExternal: true})
	if err != nil || view.RoutingMode != "hybrid" || view.Mode != "hybrid_auto" || view.ModeSource != "saved" {
		t.Fatalf("%+v %v", view, err)
	}
	draft := classifierTestDraft()
	draft.TimeoutMS = 6000
	view, err = s.Save(t.Context(), draft)
	if err != nil || view.RoutingMode != "hybrid" || repo.row.RoutingMode != "hybrid" {
		t.Fatal("connection save overwrote mode", view, err)
	}
	// Disable remains available even if saved credentials cannot be decrypted.
	repo.row.EncryptedAPIKey = "corrupt"
	if _, err := s.SaveMode(t.Context(), ClassifierModeDraft{Mode: "laya", ConfirmExternal: true}); ClassifierErrorCode(err) != "credential_unreadable" {
		t.Fatal(err)
	}
	view, err = s.SaveMode(t.Context(), ClassifierModeDraft{Mode: "rules"})
	if err != nil || view.RoutingMode != "rules" || view.Mode != "manual_test_only" || repo.row.EncryptedAPIKey != "corrupt" {
		t.Fatal(view, err)
	}
}

func TestClassifierModeDisableWithoutConnectionOrEncryption(t *testing.T) {
	s := NewClassifierSettingsService(&classifierMemoryRepo{}, "")
	view, err := s.SaveMode(t.Context(), ClassifierModeDraft{Mode: "rules"})
	if err != nil || view.RoutingMode != "rules" || view.ModeSource != "saved" {
		t.Fatal(view, err)
	}
}

func TestClassifierModeLookupSaturationStaysLocal(t *testing.T) {
	s, _ := classifierTestService()
	g := hybridGateway(t, s)
	for i := 0; i < cap(s.modeSlots); i++ {
		s.modeSlots <- struct{}{}
	}
	d, err := g.classifyRoutingTask(t.Context(), routingRequest("陌生任务").Messages, false)
	if err != nil || d.Classifier != "heuristic" || d.ClassifierFallback != "busy" || d.RequestedClassifier != "dynamic" {
		t.Fatal(d, err)
	}
}

func TestClassifierModeHotSwitchAcrossReplicas(t *testing.T) {
	a, repo := classifierTestService()
	b := NewClassifierSettingsService(repo, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	var calls atomic.Int32
	for _, s := range []*ClassifierSettingsService{a, b} {
		s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
			return testLaya(t, func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return layaResponse(200, layaValidResponse), nil
			}), nil
		}
	}
	ga, gb := &service{}, &service{}
	oa, _ := a.LiveRoutingOption("rules")
	oa(ga)
	ob, _ := b.LiveRoutingOption("laya")
	ob(gb)
	if _, err := a.Save(t.Context(), classifierTestDraft()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveMode(t.Context(), ClassifierModeDraft{Mode: "hybrid", ConfirmExternal: true}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*service{ga, gb} {
		d, err := g.classifyRoutingTask(t.Context(), routingRequest("帮我写一个Python排序函数").Messages, false)
		if err != nil || d.Classifier != "heuristic" || d.RequestedClassifier != "hybrid" || d.RequiredTier != 2 {
			t.Fatal(d, err)
		}
		d, err = g.classifyRoutingTask(t.Context(), routingRequest("陌生任务").Messages, false)
		if err != nil || d.Classifier != "laya" || d.RequestedClassifier != "hybrid" {
			t.Fatal(d, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("clear match called Laya", calls.Load())
	}
	if _, err := b.SaveMode(t.Context(), ClassifierModeDraft{Mode: "laya", ConfirmExternal: true}); err != nil {
		t.Fatal(err)
	}
	d, err := ga.classifyRoutingTask(t.Context(), routingRequest("你好").Messages, false)
	if err != nil || d.Classifier != "laya" || d.RequestedClassifier != "laya" || calls.Load() != 3 {
		t.Fatal(d, err)
	}
	if _, err := b.SaveMode(t.Context(), ClassifierModeDraft{Mode: "rules"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*service{ga, gb} {
		d, err := g.classifyRoutingTask(t.Context(), routingRequest("陌生任务").Messages, false)
		if err != nil || d.Classifier != "heuristic" || d.RequestedClassifier != "" {
			t.Fatal(d, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("saved rules did not override deployment default")
	}
	view, _ := a.Get(t.Context())
	if view.RoutingMode != "rules" || view.ModeSource != "saved" {
		t.Fatal(view)
	}
}

func TestClassifierModeReadFailureIsLocalOnly(t *testing.T) {
	s, repo := classifierTestService()
	g := hybridGateway(t, s)
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		t.Error("must stay local")
		return nil, layaError("connection_failed")
	}
	for _, invalid := range []bool{false, true} {
		repo.err = errors.New("secret db failure")
		want := "storage_error"
		if invalid {
			repo.err = nil
			repo.row.RoutingMode = "unexpected"
			want = "invalid_mode"
		}
		d, err := g.classifyRoutingTask(t.Context(), routingRequest("陌生任务").Messages, false)
		if err != nil || d.Classifier != "heuristic" || d.ClassifierFallback != want || d.RequestedClassifier != "dynamic" {
			t.Fatal(d, err)
		}
	}
}

func TestClassifierModeInFlightSnapshotAndSubsequentDisable(t *testing.T) {
	s, _ := classifierTestService()
	if _, err := s.Save(t.Context(), classifierTestDraft()); err != nil {
		t.Fatal(err)
	}
	g := hybridGateway(t, s)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		return testLaya(t, func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			close(started)
			select {
			case <-release:
				return layaResponse(200, layaValidResponse), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}), nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		d, err := g.classifyRoutingTask(ctx, routingRequest("陌生任务").Messages, false)
		if err == nil && d.Classifier != "laya" {
			err = errors.New("in-flight snapshot changed")
		}
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := s.SaveMode(t.Context(), ClassifierModeDraft{Mode: "rules"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	d, err := g.classifyRoutingTask(t.Context(), routingRequest("陌生任务").Messages, false)
	if err != nil || d.Classifier != "heuristic" || calls.Load() != 1 {
		t.Fatal(d, err, calls.Load())
	}
}
