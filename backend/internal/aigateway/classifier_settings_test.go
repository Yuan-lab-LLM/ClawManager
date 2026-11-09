package aigateway

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type classifierMemoryRepo struct {
	row   models.RoutingClassifierSetting
	saves int
	err   error
}

func (r *classifierMemoryRepo) Get(context.Context) (*models.RoutingClassifierSetting, error) {
	row := r.row
	if row.TimeoutMS == 0 {
		row.TimeoutMS = 5000
	}
	return &row, r.err
}
func (r *classifierMemoryRepo) Save(_ context.Context, row *models.RoutingClassifierSetting) error {
	if r.err != nil {
		return r.err
	}
	mode := r.row.RoutingMode
	r.row = *row
	r.row.RoutingMode = mode
	r.saves++
	return nil
}
func (r *classifierMemoryRepo) SaveMode(_ context.Context, mode string) error {
	if r.err != nil {
		return r.err
	}
	r.row.RoutingMode = mode
	r.saves++
	return nil
}
func classifierTestService() (*ClassifierSettingsService, *classifierMemoryRepo) {
	repo := &classifierMemoryRepo{}
	return NewClassifierSettingsService(repo, strings.Repeat("k", 32)), repo
}
func classifierTestDraft() ClassifierSettingsDraft {
	key := "test-secret"
	return ClassifierSettingsDraft{BaseURL: "http://laya.example:8000", TimeoutMS: 5000, APIKey: &key}
}

func TestClassifierSettingsCredentialLifecycle(t *testing.T) {
	s, repo := classifierTestService()
	draft := classifierTestDraft()
	view, err := s.Save(t.Context(), draft)
	if err != nil || !view.HasAPIKey || view.Mode != "manual_test_only" {
		t.Fatalf("%+v %v", view, err)
	}
	encoded, _ := json.Marshal(view)
	rowJSON, _ := json.Marshal(repo.row)
	if strings.Contains(string(encoded), "test-secret") || strings.Contains(string(rowJSON), repo.row.EncryptedAPIKey) || repo.row.EncryptedAPIKey == "test-secret" {
		t.Fatal("credential leaked")
	}
	key, err := s.unseal(&repo.row)
	if err != nil || key != "test-secret" {
		t.Fatal(err)
	}
	draft.APIKey = nil
	draft.TimeoutMS = 6000
	if _, err := s.Save(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	draft.BaseURL = "http://different.example"
	if _, err := s.Save(t.Context(), draft); ClassifierErrorCode(err) != "key_required_for_new_url" {
		t.Fatal(err)
	}
	copy := repo.row
	copy.BaseURL = draft.BaseURL
	if _, err := s.unseal(&copy); ClassifierErrorCode(err) != "credential_unreadable" {
		t.Fatal("ciphertext not bound to URL")
	}
	draft.ClearAPIKey = true
	view, err = s.Save(t.Context(), draft)
	if err != nil || view.HasAPIKey || repo.row.EncryptedAPIKey != "" {
		t.Fatal("clear failed", err)
	}
}
func TestClassifierSettingsEncryptionRequired(t *testing.T) {
	for _, material := range []string{"", "short", "clawreef-secret-key-change-in-production"} {
		s := NewClassifierSettingsService(&classifierMemoryRepo{}, material)
		if _, err := s.Save(t.Context(), classifierTestDraft()); ClassifierErrorCode(err) != "encryption_unavailable" {
			t.Fatal(err)
		}
	}
	s, repo := classifierTestService()
	_, _ = s.Save(t.Context(), classifierTestDraft())
	rotated := NewClassifierSettingsService(repo, strings.Repeat("x", 32))
	if _, err := rotated.unseal(&repo.row); ClassifierErrorCode(err) != "credential_unreadable" {
		t.Fatal("rotation must not use unreadable key")
	}
}
func TestClassifierComparisonDoesNotSaveOrEnable(t *testing.T) {
	s, repo := classifierTestService()
	s.newClassifier = func(url, key string, timeout time.Duration) (*LayaClassifier, error) {
		if url != "http://laya.example:8000" || key != "test-secret" || timeout != 5*time.Second {
			t.Fatal("bad config")
		}
		return testLaya(t, func(*http.Request) (*http.Response, error) { return layaResponse(200, layaValidResponse), nil }), nil
	}
	draft := classifierTestDraft()
	out, err := s.Compare(t.Context(), ClassifierCompareRequest{Text: "帮我写一个Python排序函数", Draft: &draft})
	if err != nil || out.Laya.Status != "success" || out.Rule.Status != "success" || out.SameTier == nil || out.ProductionRoutingChanged || repo.saves != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	s.newClassifier = func(string, string, time.Duration) (*LayaClassifier, error) {
		return nil, layaError("connection_failed")
	}
	out, err = s.Compare(t.Context(), ClassifierCompareRequest{Text: "你好", Draft: &draft})
	if err != nil || out.Rule.Status != "success" || out.Laya.ErrorCode != "connection_failed" || out.SameTier != nil {
		t.Fatalf("%+v %v", out, err)
	}
}
func TestClassifierComparisonBusyAndStorage(t *testing.T) {
	s, repo := classifierTestService()
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	out, err := s.Compare(t.Context(), ClassifierCompareRequest{Text: "你好"})
	if err != nil || out.Laya.ErrorCode != "busy" || out.Rule.Classification == nil {
		t.Fatal(out, err)
	}
	<-s.slots
	repo.err = errors.New("database secret details")
	if _, err := s.Get(t.Context()); err.Error() != "storage_error" {
		t.Fatal(err)
	}
}
