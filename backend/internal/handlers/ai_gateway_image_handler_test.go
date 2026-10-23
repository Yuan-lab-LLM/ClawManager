package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type imageHandlerService struct {
	aigateway.Service
	called         bool
	owner          services.ImageArtifactOwner
	groups         []string
	err            error
	idempotencyKey string
	result         *aigateway.ImageExecutionResponse
}

func (s *imageHandlerService) ImageCatalog(_ context.Context, userID int, groups []string) ([]aigateway.ImageCatalogModel, error) {
	s.owner.UserID, s.groups = userID, groups
	return []aigateway.ImageCatalogModel{{ID: "visible-image"}}, s.err
}

func (s *imageHandlerService) ExecuteImage(_ context.Context, userID int, req *aigateway.ImageOperationRequest, attr aigateway.ChatCompletionRequest) (*aigateway.ImageExecutionResponse, error) {
	s.called = true
	s.idempotencyKey = attr.ImageIdempotencyKey
	s.owner = services.ImageArtifactOwner{UserID: userID, InstanceID: attr.InstanceID, GatewayKeyID: attr.GatewayKeyID}
	s.groups = attr.AllowedModelGroups
	if attr.UsageFinalizer != nil {
		_ = attr.UsageFinalizer(123)
	}
	if s.result != nil {
		return s.result, s.err
	}
	return &aigateway.ImageExecutionResponse{TraceID: "image-test"}, s.err
}

func TestImageCatalogHandlerScope(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		svc := &imageHandlerService{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/images/models", nil)
		if authenticated {
			c.Set("userID", 7)
			c.Set("gatewayAllowedModelGroups", []string{"images"})
		}
		NewAIGatewayHandler(svc, nil, nil, nil, nil).ImageModels(c)
		if !authenticated {
			if w.Code != 401 {
				t.Fatal(w.Code)
			}
			continue
		}
		if w.Code != 200 || svc.owner.UserID != 7 || len(svc.groups) != 1 || svc.groups[0] != "images" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, svc, w.Header())
		}
	}
}

func TestImageReferenceModePreservesNativeAPIAndGovernance(t *testing.T) {
	for _, mode := range []string{"", "references", "invalid"} {
		for _, authenticated := range []bool{false, true} {
			result := &aigateway.ImageExecutionResponse{TraceID: "image-test"}
			if err := json.Unmarshal([]byte(`{"created":1,"data":[{"b64_json":"pixels-canary","revised_prompt":"prompt-canary","artifact_id":"img_test"}]}`), result); err != nil {
				t.Fatal(err)
			}
			svc := &imageHandlerService{result: result}
			store := &fakeGatewayRateLimitStore{}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/images/generations", strings.NewReader(`{"model":"image-only","prompt":"draw"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("X-ClawManager-Image-Result", mode)
			if authenticated {
				c.Set("userID", 7)
				c.Set("gatewayKeyID", "key")
				c.Set("gatewayKeyConcurrencyLimit", 2)
			}
			NewAIGatewayHandler(svc, nil, nil, nil, store).ImageGenerations(c)
			if mode == "invalid" || !authenticated {
				if w.Code == 200 || svc.called {
					t.Fatal("invalid or unauthenticated execution")
				}
				continue
			}
			if w.Code != 200 || !svc.called || store.releaseCount != 1 {
				t.Fatal(w.Code, w.Body.String(), store)
			}
			if mode == "references" {
				if strings.Contains(w.Body.String(), "canary") || !strings.Contains(w.Body.String(), "img_test") || !strings.Contains(w.Body.String(), "request_id") {
					t.Fatal(w.Body.String())
				}
			} else if !strings.Contains(w.Body.String(), "pixels-canary") {
				t.Fatal("native image response changed", w.Body.String())
			}
		}
	}
}

func (s *imageHandlerService) ImageRequestStatus(_ context.Context, owner services.ImageArtifactOwner, id string, groups []string) (*aigateway.ImageJobStatus, error) {
	s.owner, s.groups = owner, groups
	return &aigateway.ImageJobStatus{RequestID: id, State: "unknown"}, s.err
}

func TestImageStatusHandlerAuthorizationAndNoStore(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, denied := range []bool{false, true} {
			svc := &imageHandlerService{}
			if denied {
				svc.err = errors.New("private database detail")
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/images/requests/image_test", nil)
			c.Params = gin.Params{{Key: "requestID", Value: "image_test"}}
			if authenticated {
				c.Set("userID", 7)
				c.Set("instanceID", 8)
				c.Set("gatewayKeyDBID", 9)
				c.Set("gatewayAllowedModelGroups", []string{"images"})
			}
			NewAIGatewayHandler(svc, nil, nil, nil, nil).ImageRequestStatus(c)
			want := 200
			if !authenticated {
				want = 401
			} else if denied {
				want = 404
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
			if authenticated && (svc.owner.UserID != 7 || *svc.owner.InstanceID != 8 || *svc.owner.GatewayKeyID != 9 || len(svc.groups) != 1) {
				t.Fatal("credential scope lost", svc)
			}
		}
	}
}
func (s *imageHandlerService) LoadImageArtifact(_ context.Context, owner services.ImageArtifactOwner, _ string, groups []string) (*services.ImageArtifact, error) {
	s.owner = owner
	s.groups = groups
	return &services.ImageArtifact{MediaType: "image/png", Data: []byte("test")}, s.err
}

func TestImageHandlerAttributionLimitsAndRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeGatewayRateLimitStore{}
	svc := &imageHandlerService{}
	h := NewAIGatewayHandler(svc, nil, nil, nil, store)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/images/generations", strings.NewReader(`{"model":"image-only","prompt":"draw"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", "client-image-request")
	c.Set("userID", 7)
	c.Set("instanceID", 8)
	c.Set("gatewayKeyDBID", 9)
	c.Set("gatewayKeyID", "key")
	c.Set("gatewayAllowedModelGroups", []string{"images"})
	c.Set("gatewayKeyRPMLimit", 10)
	c.Set("gatewayKeyTPMLimit", 100000)
	c.Set("gatewayKeyConcurrencyLimit", 2)
	h.ImageGenerations(c)
	if w.Code != 200 || !svc.called || svc.owner.UserID != 7 || *svc.owner.InstanceID != 8 || *svc.owner.GatewayKeyID != 9 || len(svc.groups) != 1 {
		t.Fatal(w.Code, w.Body.String(), svc)
	}
	if store.input.EstimatedTokens < 32768 || store.input.RPMLimit != 10 || store.input.TPMLimit != 100000 || store.input.ConcurrentRequestsLimit != 2 || store.finalizedTokens != 123 || store.releaseCount != 1 {
		t.Fatal(store)
	}
	if w.Header().Get("x-should-retry") != "false" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Trace-ID") != "image-test" {
		t.Fatal(w.Header())
	}
	if svc.idempotencyKey != "client-image-request" {
		t.Fatal("idempotency header lost")
	}
}

func TestImageHandlerErrorSanitizationAndStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{models.ErrModelUnavailable, 403, "model_unavailable"},
		{services.ErrGatewayKeyBudgetExceeded, 429, "budget_exceeded"},
		{services.ErrGatewayDeploymentUnavailable, 429, "image_deployment_unavailable"},
		{&aigateway.ImageExecutionError{Status: 400, Code: "image_price_required", Message: "Set image price"}, 400, "image_price_required"},
		{errors.New("provider-secret-key https://secret-url"), 503, "image_request_failed"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		(&AIGatewayHandler{}).writeImageError(c, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestImageErrorTracePreservesRevocationStatus(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	(&AIGatewayHandler{}).writeImageError(c, &aigateway.ImageRequestError{RequestID: "image-safe-id", Err: models.ErrModelUnavailable})
	if w.Code != 403 || w.Header().Get("X-Trace-ID") != "image-safe-id" || !strings.Contains(w.Body.String(), "model_unavailable") {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
}

func TestImageArtifactHandlerCarriesNarrowCredentialScope(t *testing.T) {
	svc := &imageHandlerService{}
	h := NewAIGatewayHandler(svc, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/artifacts/img_a", nil)
	c.Set("userID", 7)
	c.Set("instanceID", 8)
	c.Set("gatewayKeyDBID", 9)
	c.Set("gatewayAllowedModelGroups", []string{"images"})
	c.Params = gin.Params{{Key: "artifactID", Value: "img_a"}}
	h.ImageArtifact(c)
	if w.Code != 200 || svc.owner.UserID != 7 || *svc.owner.InstanceID != 8 || *svc.owner.GatewayKeyID != 9 || len(svc.groups) != 1 {
		t.Fatal(w.Code, svc)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(w.Header())
	}
}
