package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type capabilityInstanceRepo struct {
	repository.InstanceRepository
	instance *models.Instance
	rawCalls int
	getErr   error
}

func (r *capabilityInstanceRepo) GetByID(id int) (*models.Instance, error) {
	return r.instance, r.getErr
}
func (r *capabilityInstanceRepo) GetByAccessToken(token string) (*models.Instance, error) {
	r.rawCalls++
	return r.instance, r.getErr
}

type capabilityBindingRepo struct {
	repository.InstanceRuntimeBindingRepository
	binding *models.InstanceRuntimeBinding
	err     error
}

func (r *capabilityBindingRepo) GetRunningByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error) {
	return r.binding, r.err
}

func gatewayCapabilityFixture(t *testing.T) (*capabilityInstanceRepo, *runtimeauth.Service, string, string) {
	t.Helper()
	raw := "raw-instance-token"
	repo := &capabilityInstanceRepo{instance: &models.Instance{ID: 8, UserID: 1, RuntimeGeneration: 3, Status: "running", Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", AccessToken: &raw}}
	id := runtimeauth.Identity{InstanceID: 8, UserID: 1, Generation: 3, PodUID: "pod-test"}
	s, err := runtimeauth.New([]byte(strings.Repeat("k", 32)), func(context.Context, int) (runtimeauth.Identity, error) { return id, nil })
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.Issue(context.Background(), id, runtimeauth.AudienceModel)
	if err != nil {
		t.Fatal(err)
	}
	egress, err := s.Issue(context.Background(), id, runtimeauth.AudienceEgress)
	if err != nil {
		t.Fatal(err)
	}
	return repo, s, model, egress
}
func gatewayCapabilityRequest(auth gin.HandlerFunc, token string) (*httptest.ResponseRecorder, map[any]any) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var metadata map[any]any
	r.Use(auth)
	r.GET("/", func(c *gin.Context) { metadata = c.Keys; c.Status(204) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-ClawManager-Instance-Id", "999")
	request.Header.Set("X-ClawManager-User-Id", "999")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, request)
	return rec, metadata
}
func TestGatewayRuntimeCapabilityMetadata(t *testing.T) {
	repo, s, model, _ := gatewayCapabilityFixture(t)
	binding := &capabilityBindingRepo{binding: &models.InstanceRuntimeBinding{InstanceID: 8, Generation: 3, State: "running", RuntimePodID: 42, GatewayID: "gw-8"}}
	rec, got := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilities(repo, s, func(context.Context, int) (bool, error) { return true, nil }, binding), model)
	if rec.Code != 204 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for key, want := range map[string]any{"userID": 1, "instanceID": 8, "instanceType": "openclaw", "instanceMode": "lite", "runtimeType": "gateway", "gatewayID": "gw-8", "runtimePodID": int64(42), "gatewayAuthType": "instance"} {
		if got[key] != want {
			t.Errorf("%s=%v want=%v", key, got[key], want)
		}
	}
	if repo.rawCalls != 0 {
		t.Fatal("capability attempted legacy authentication")
	}
}
func TestGatewayRuntimeCapabilityRejectsWrongPurposeAndDBMismatch(t *testing.T) {
	for _, kind := range []string{"egress", "invalid", "user", "generation", "stopped", "pro", "binding", "db-error", "predicate-error"} {
		t.Run(kind, func(t *testing.T) {
			repo, s, model, egress := gatewayCapabilityFixture(t)
			token := model
			predicate := func(context.Context, int) (bool, error) { return true, nil }
			bindings := &capabilityBindingRepo{}
			switch kind {
			case "egress":
				token = egress
			case "invalid":
				token = "v1.invalid"
			case "user":
				repo.instance.UserID++
			case "generation":
				repo.instance.RuntimeGeneration++
			case "stopped":
				repo.instance.Status = "stopped"
			case "pro":
				repo.instance.InstanceMode = "pro"
			case "binding":
				bindings.binding = &models.InstanceRuntimeBinding{InstanceID: 8, Generation: 2, State: "running"}
			case "db-error":
				repo.getErr = errors.New("sentinel-secret")
			case "predicate-error":
				predicate = func(context.Context, int) (bool, error) { return false, errors.New("sentinel-secret") }
			}
			rec, _ := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilities(repo, s, predicate, bindings), token)
			if rec.Code != 401 || repo.rawCalls != 0 {
				t.Fatalf("status=%d rawCalls=%d body=%s", rec.Code, repo.rawCalls, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "sentinel-secret") || strings.Contains(rec.Body.String(), token) {
				t.Fatal("credential/error exposed")
			}
		})
	}
}
func TestGatewayRuntimeCapabilitiesLegacyProtection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protected bool
		err       error
		want      int
	}{{"protected", true, nil, 401}, {"unprotected", false, nil, 204}, {"error", false, errors.New("lookup failed"), 401}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, s, _, _ := gatewayCapabilityFixture(t)
			rec, _ := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilities(repo, s, func(ctx context.Context, id int) (bool, error) {
				if id != 8 {
					t.Errorf("wrong instance: %d", id)
				}
				return tc.protected, tc.err
			}), *repo.instance.AccessToken)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	repo, _, _, _ := gatewayCapabilityFixture(t)
	rec, _ := gatewayCapabilityRequest(GatewayAuth(repo), *repo.instance.AccessToken)
	if rec.Code != 204 {
		t.Fatalf("existing caller broken: %d", rec.Code)
	}
}
func TestGatewayRuntimeCapabilitiesPreservesUserJWT(t *testing.T) {
	repo, s, _, _ := gatewayCapabilityFixture(t)
	token, err := utils.GenerateToken(utils.TokenClaims{UserID: 55, TokenType: "access"}, getJWTSecret(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rec, got := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilities(repo, s, func(context.Context, int) (bool, error) {
		t.Error("user JWT reached runtime predicate")
		return true, errors.New("unavailable")
	}), token)
	if rec.Code != 204 || got["userID"] != 55 || got["gatewayAuthType"] != "user" || got["instanceID"] != nil || repo.rawCalls != 0 {
		t.Fatalf("unexpected user metadata: status=%d metadata=%v rawCalls=%d", rec.Code, got, repo.rawCalls)
	}
}

func TestGatewayRuntimeCapabilityDuringActiveStartup(t *testing.T) {
	repo, s, model, _ := gatewayCapabilityFixture(t)
	repo.instance.Status = "creating"
	rec, metadata := gatewayCapabilityRequest(GatewayAuthWithRuntimeCapabilities(repo, s, func(context.Context, int) (bool, error) { return true, nil }), model)
	if rec.Code != 204 || metadata["instanceID"] != 8 {
		t.Fatalf("active startup capability rejected: status=%d metadata=%v body=%s", rec.Code, metadata, rec.Body.String())
	}
}
