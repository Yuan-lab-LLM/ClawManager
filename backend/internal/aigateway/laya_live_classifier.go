package aigateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
)

// Startup installs a dynamic resolver. The environment is a compatibility
// default only: a saved administrator choice overrides it on every new request.
func (s *ClassifierSettingsService) LiveRoutingOption(mode string) (ServiceOption, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "rules":
		s.liveMode = "manual_test_only"
	case "laya":
		s.liveMode = "laya_auto"
	case "hybrid":
		s.liveMode = "hybrid_auto"
	default:
		return nil, errors.New("CLAWMANAGER_ROUTING_CLASSIFIER must be rules, laya or hybrid")
	}
	return func(target *service) {
		adapter := &savedLayaClassifier{settings: s}
		base := &routingClassifierRunner{name: "laya", classifier: adapter, timeout: 30 * time.Second, slots: make(chan struct{}, maxRoutingClassifierInflight), rulesFirst: s.liveMode == "hybrid_auto"}
		target.routingClassifier = nil
		if s.liveMode != "manual_test_only" {
			target.routingClassifier = base
		}
		target.resolveRoutingClassifier = func(ctx context.Context) (*routingClassifierRunner, error) {
			select {
			case s.modeSlots <- struct{}{}:
				defer func() { <-s.modeSlots }()
			default:
				return nil, layaError("busy")
			}
			readCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			setting, err := s.repo.Get(readCtx)
			if err != nil || readCtx.Err() != nil || setting == nil {
				return nil, layaError("storage_error")
			}
			selectedMode, _ := s.effectiveMode(setting)
			if !validClassifierMode(selectedMode) {
				return nil, layaError("invalid_mode")
			}
			if selectedMode == "rules" {
				return nil, nil
			}
			runner := *base // shared bounded slots, immutable per-request policy
			runner.rulesFirst = selectedMode == "hybrid"
			runner.classifier = &layaConfigurationSnapshot{owner: adapter, setting: *setting}
			return &runner, nil
		}
	}, nil
}

// Mode, endpoint and credentials are one snapshot per classification. Disabling
// affects subsequent snapshots; an in-flight request may finish on its old one.
type layaConfigurationSnapshot struct {
	owner   *savedLayaClassifier
	setting models.RoutingClassifierSetting
}

func (l *layaConfigurationSnapshot) Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
	return l.owner.classifySetting(ctx, input, &l.setting)
}

// Reads saved configuration on each call: clearing/changing credentials takes
// effect for subsequent requests, including across replicas. The HTTP transport
// is reused only while the entire encrypted configuration snapshot is unchanged.
// In-flight requests may finish using their previous configuration snapshot.
type savedLayaClassifier struct {
	settings *ClassifierSettingsService
	mu       sync.Mutex
	snapshot models.RoutingClassifierSetting
	adapter  *LayaClassifier
}

func (l *savedLayaClassifier) Classify(ctx context.Context, input RoutingClassificationInput) (RoutingClassification, error) {
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	setting, err := l.settings.repo.Get(readCtx)
	if err != nil {
		return RoutingClassification{}, layaError("storage_error")
	}
	return l.classifySetting(ctx, input, setting)
}

func (l *savedLayaClassifier) classifySetting(ctx context.Context, input RoutingClassificationInput, setting *models.RoutingClassifierSetting) (RoutingClassification, error) {
	if err := ctx.Err(); err != nil {
		return RoutingClassification{}, err
	}
	// Invalid/oversized input must never be sent, even if configuration is broken.
	if _, _, err := layaPayload(input); err != nil {
		return RoutingClassification{}, err
	}
	select {
	case l.settings.slots <- struct{}{}:
		defer func() { <-l.settings.slots }()
	default:
		return RoutingClassification{}, layaError("busy")
	}
	if setting == nil || setting.BaseURL == "" || setting.EncryptedAPIKey == "" {
		return RoutingClassification{}, layaError("configuration_missing")
	}
	adapter, err := l.getAdapter(setting)
	if err != nil {
		return RoutingClassification{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(setting.TimeoutMS)*time.Millisecond)
	defer cancel()
	return adapter.Classify(callCtx, input)
}

func (l *savedLayaClassifier) getAdapter(setting *models.RoutingClassifierSetting) (*LayaClassifier, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adapter != nil && l.snapshot == *setting {
		return l.adapter, nil
	}
	key, err := l.settings.unseal(setting)
	if err != nil {
		return nil, err
	}
	adapter, err := l.settings.newClassifier(setting.BaseURL, key, time.Duration(setting.TimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if l.adapter != nil {
		l.adapter.Close() // closes idle connections, not in-flight calls
	}
	l.adapter, l.snapshot = adapter, *setting
	return adapter, nil
}

// Only fixed safe codes reach audit records; never log arbitrary error strings,
// HTTP bodies, credentials, user text, or untrusted adapter error codes.
func layaFallbackReason(err error) string {
	var safe *LayaError
	if !errors.As(err, &safe) {
		return "error"
	}
	switch safe.Code {
	case "timeout", "busy", "input_too_long", "unsupported_input", "invalid_input",
		"configuration_missing", "storage_error", "encryption_unavailable", "credential_unreadable",
		"invalid_url", "invalid_api_key", "invalid_timeout", "blocked_address", "connection_failed",
		"http_401", "http_403", "http_404", "http_422", "http_429", "http_503",
		"invalid_response", "response_too_large", "response_read_failed", "invalid_mode":
		return safe.Code
	default:
		return "error"
	}
}
