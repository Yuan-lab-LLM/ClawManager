package aigateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type ClassifierSettingsView struct {
	BaseURL         string `json:"base_url"`
	TimeoutMS       int    `json:"timeout_ms"`
	Model           string `json:"model"`
	HasAPIKey       bool   `json:"has_api_key"`
	EncryptionReady bool   `json:"encryption_ready"`
	Mode            string `json:"mode"`
	RoutingMode     string `json:"routing_mode"`
	ModeSource      string `json:"mode_source"`
}
type ClassifierModeDraft struct {
	Mode            string `json:"mode"`
	ConfirmExternal bool   `json:"confirm_external"`
}
type ClassifierSettingsDraft struct {
	BaseURL     string  `json:"base_url"`
	TimeoutMS   int     `json:"timeout_ms"`
	APIKey      *string `json:"api_key,omitempty"`
	ClearAPIKey bool    `json:"clear_api_key,omitempty"`
}
type ClassifierCompareRequest struct {
	Text  string                   `json:"text"`
	Draft *ClassifierSettingsDraft `json:"draft,omitempty"`
}
type ClassifierTestResult struct {
	Status         string                 `json:"status"`
	Classifier     string                 `json:"classifier"`
	DurationMS     float64                `json:"duration_ms"`
	Classification *RoutingClassification `json:"classification,omitempty"`
	ErrorCode      string                 `json:"error_code,omitempty"`
}
type ClassifierComparison struct {
	Rule                     ClassifierTestResult         `json:"rule"`
	Laya                     ClassifierTestResult         `json:"laya"`
	SameTier                 *bool                        `json:"same_tier,omitempty"`
	ProductionRoutingChanged bool                         `json:"production_routing_changed"`
	Hybrid                   *HybridClassificationPreview `json:"hybrid,omitempty"`
}

// Connection saves/comparisons never change the mode. An explicit administrator
// action persists mode separately; Auto reads it for each new classification.
type ClassifierSettingsService struct {
	repo          repository.RoutingClassifierSettingRepository
	aead          cipher.AEAD
	mu            sync.Mutex
	slots         chan struct{}
	modeSlots     chan struct{}
	newClassifier func(string, string, time.Duration) (*LayaClassifier, error)
	liveMode      string // immutable after startup, before serving requests
}

func NewClassifierSettingsService(repo repository.RoutingClassifierSettingRepository, keyMaterial string) *ClassifierSettingsService {
	s := &ClassifierSettingsService{repo: repo, slots: make(chan struct{}, 4), modeSlots: make(chan struct{}, maxRoutingClassifierInflight), newClassifier: NewLayaClassifier}
	// Prefer a dedicated random environment secret; configured JWT secret is a
	// compatibility fallback. Never encrypt using the known development default.
	if len(keyMaterial) >= 32 && keyMaterial != "clawreef-secret-key-change-in-production" {
		key := sha256.Sum256([]byte("clawmanager:laya-settings:v1:" + keyMaterial))
		block, _ := aes.NewCipher(key[:])
		s.aead, _ = cipher.NewGCM(block)
	}
	return s
}

func (s *ClassifierSettingsService) view(setting *models.RoutingClassifierSetting) ClassifierSettingsView {
	routingMode, source := s.effectiveMode(setting)
	mode := "manual_test_only"
	if routingMode == "hybrid" {
		mode = "hybrid_auto"
	} else if routingMode == "laya" {
		mode = "laya_auto"
	}
	return ClassifierSettingsView{BaseURL: setting.BaseURL, TimeoutMS: setting.TimeoutMS, Model: "multilingual", HasAPIKey: setting.EncryptedAPIKey != "", EncryptionReady: s.aead != nil, Mode: mode, RoutingMode: routingMode, ModeSource: source}
}
func (s *ClassifierSettingsService) Get(ctx context.Context) (ClassifierSettingsView, error) {
	setting, err := s.repo.Get(ctx)
	if err != nil {
		return ClassifierSettingsView{}, layaError("storage_error")
	}
	if setting == nil {
		return ClassifierSettingsView{}, layaError("storage_error")
	}
	if mode, _ := s.effectiveMode(setting); !validClassifierMode(mode) {
		return ClassifierSettingsView{}, layaError("invalid_mode")
	}
	return s.view(setting), nil
}

func validClassifierMode(mode string) bool {
	return mode == "rules" || mode == "hybrid" || mode == "laya"
}

func (s *ClassifierSettingsService) effectiveMode(setting *models.RoutingClassifierSetting) (string, string) {
	if setting.RoutingMode != "" {
		return setting.RoutingMode, "saved"
	}
	switch s.liveMode {
	case "hybrid_auto":
		return "hybrid", "deployment_default"
	case "laya_auto":
		return "laya", "deployment_default"
	default:
		return "rules", "deployment_default"
	}
}

// Disabling must work even when credentials, encryption or URL are broken.
// Enabling validates saved connection settings without sending user/model data.
func (s *ClassifierSettingsService) SaveMode(ctx context.Context, draft ClassifierModeDraft) (ClassifierSettingsView, error) {
	if !validClassifierMode(draft.Mode) {
		return ClassifierSettingsView{}, layaError("invalid_mode")
	}
	if draft.Mode != "rules" && !draft.ConfirmExternal {
		return ClassifierSettingsView{}, layaError("external_confirmation_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if draft.Mode != "rules" {
		setting, err := s.repo.Get(ctx)
		if err != nil {
			return ClassifierSettingsView{}, layaError("storage_error")
		}
		if setting == nil || setting.BaseURL == "" || setting.EncryptedAPIKey == "" {
			return ClassifierSettingsView{}, layaError("configuration_missing")
		}
		key, err := s.unseal(setting)
		if err != nil {
			return ClassifierSettingsView{}, err
		}
		probe, err := NewLayaClassifier(setting.BaseURL, key, time.Duration(setting.TimeoutMS)*time.Millisecond)
		if err != nil {
			return ClassifierSettingsView{}, err
		}
		probe.Close()
	}
	if err := s.repo.SaveMode(ctx, draft.Mode); err != nil {
		return ClassifierSettingsView{}, layaError("storage_error")
	}
	return s.Get(ctx)
}

func (s *ClassifierSettingsService) seal(key, baseURL string) (string, error) {
	if s.aead == nil {
		return "", layaError("encryption_unavailable")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", layaError("encryption_failed")
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(key), []byte(baseURL))
	return base64.StdEncoding.EncodeToString(sealed), nil
}
func (s *ClassifierSettingsService) unseal(setting *models.RoutingClassifierSetting) (string, error) {
	if setting.EncryptedAPIKey == "" {
		return "", nil
	}
	if s.aead == nil {
		return "", layaError("encryption_unavailable")
	}
	raw, err := base64.StdEncoding.DecodeString(setting.EncryptedAPIKey)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", layaError("credential_unreadable")
	}
	key, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte(setting.BaseURL))
	if err != nil {
		return "", layaError("credential_unreadable")
	}
	return string(key), nil
}

func (s *ClassifierSettingsService) prepare(stored *models.RoutingClassifierSetting, draft ClassifierSettingsDraft) (*models.RoutingClassifierSetting, string, error) {
	baseURL, err := ValidateLayaURL(draft.BaseURL)
	if err != nil {
		return nil, "", err
	}
	if draft.TimeoutMS < 100 || draft.TimeoutMS > 30000 {
		return nil, "", layaError("invalid_timeout")
	}
	if draft.ClearAPIKey && draft.APIKey != nil {
		return nil, "", layaError("invalid_api_key")
	}
	key := ""
	if draft.APIKey != nil {
		key = strings.TrimSpace(*draft.APIKey)
		// Reuse constructor validation, but do not send any request.
		probe, err := NewLayaClassifier(baseURL, key, time.Duration(draft.TimeoutMS)*time.Millisecond)
		if err != nil {
			return nil, "", err
		}
		probe.Close()
	} else if !draft.ClearAPIKey && stored.EncryptedAPIKey != "" {
		if stored.BaseURL != baseURL {
			return nil, "", layaError("key_required_for_new_url")
		}
		key, err = s.unseal(stored)
		if err != nil {
			return nil, "", err
		}
	}
	return &models.RoutingClassifierSetting{ID: 1, BaseURL: baseURL, TimeoutMS: draft.TimeoutMS}, key, nil
}

func (s *ClassifierSettingsService) Save(ctx context.Context, draft ClassifierSettingsDraft) (ClassifierSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.repo.Get(ctx)
	if err != nil {
		return ClassifierSettingsView{}, layaError("storage_error")
	}
	setting, key, err := s.prepare(stored, draft)
	if err != nil {
		return ClassifierSettingsView{}, err
	}
	if key != "" {
		setting.EncryptedAPIKey, err = s.seal(key, setting.BaseURL)
		if err != nil {
			return ClassifierSettingsView{}, err
		}
	}
	if err := s.repo.Save(ctx, setting); err != nil {
		return ClassifierSettingsView{}, layaError("storage_error")
	}
	return s.Get(ctx)
}

func (s *ClassifierSettingsService) Compare(ctx context.Context, req ClassifierCompareRequest) (out ClassifierComparison, err error) {
	if strings.TrimSpace(req.Text) == "" || !utf8.ValidString(req.Text) || len(req.Text) > 16000 {
		return ClassifierComparison{}, layaError("invalid_input")
	}
	input := RoutingClassificationInput{Turns: []RoutingTaskTurn{{Text: req.Text}}}
	defer func() {
		if err == nil && out.Rule.Classification != nil {
			out.Hybrid = hybridComparisonPreview(input, out)
		}
	}()
	start := time.Now()
	rule, err := (RuleRoutingClassifier{}).Classify(ctx, input)
	if err != nil {
		return ClassifierComparison{}, err
	}
	result := ClassifierComparison{Rule: ClassifierTestResult{Status: "success", Classifier: "heuristic/content-cost-v4", DurationMS: float64(time.Since(start).Microseconds()) / 1000, Classification: &rule}, Laya: ClassifierTestResult{Status: "error", Classifier: "laya/multilingual"}}
	start = time.Now()
	failed := func(err error) (ClassifierComparison, error) {
		result.Laya.DurationMS = float64(time.Since(start).Microseconds()) / 1000
		result.Laya.ErrorCode = ClassifierErrorCode(err)
		return result, nil
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return failed(layaError("busy"))
	}
	stored, err := s.repo.Get(ctx)
	if err != nil {
		return failed(layaError("storage_error"))
	}
	setting := stored
	var key string
	if req.Draft != nil {
		setting, key, err = s.prepare(stored, *req.Draft)
	} else {
		key, err = s.unseal(stored)
	}
	if err != nil {
		return failed(err)
	}
	adapter, err := s.newClassifier(setting.BaseURL, key, time.Duration(setting.TimeoutMS)*time.Millisecond)
	if err != nil {
		return failed(err)
	}
	defer adapter.Close()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(setting.TimeoutMS)*time.Millisecond)
	defer cancel()
	laya, err := adapter.Classify(callCtx, input)
	if err != nil {
		return failed(err)
	}
	result.Laya = ClassifierTestResult{Status: "success", Classifier: "laya/multilingual", DurationMS: float64(time.Since(start).Microseconds()) / 1000, Classification: &laya}
	same := rule.RequiredTier == laya.RequiredTier
	result.SameTier = &same
	return result, nil
}

func ClassifierErrorCode(err error) string {
	var safe *LayaError
	if errors.As(err, &safe) {
		return safe.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "internal_error"
}
