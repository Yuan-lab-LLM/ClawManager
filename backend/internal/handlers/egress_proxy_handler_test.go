package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"

	"github.com/gin-gonic/gin"
)

type stubEgressAuditService struct {
	events []*models.AuditEvent
}

type stubTeamPreviewRepository struct {
	team *models.Team
}

func (s *stubTeamPreviewRepository) GetTeamByID(id int) (*models.Team, error) {
	if s.team != nil && s.team.ID == id {
		return s.team, nil
	}
	return nil, nil
}

type stubTeamPreviewSecrets struct {
	token string
}

func (s *stubTeamPreviewSecrets) GetSecretValue(context.Context, string, string, string) (string, error) {
	return s.token, nil
}

func (s *stubEgressAuditService) RecordEvent(event *models.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *stubEgressAuditService) ListEventsByTraceID(string) ([]models.AuditEvent, error) {
	return nil, nil
}

func TestEgressProxyHandlerBlocksDeniedConnectHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &stubEgressAuditService{}
	handler := &EgressProxyHandler{
		policy: egresspolicy.Policy{
			Mode:               egresspolicy.ModeDenylist,
			DeniedHostSuffixes: []string{"api.openai.com"},
		},
		audit: audit,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodConnect, "https://api.openai.com:443", nil)
	ctx.Request.Host = "api.openai.com:443"
	ctx.Request.Header.Set("X-ClawManager-Instance-Id", "42")

	handler.handleConnect(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != "egress.llm.blocked" {
		t.Fatalf("expected egress audit event, got %+v", audit.events)
	}
	if audit.events[0].InstanceID == nil || *audit.events[0].InstanceID != 42 {
		t.Fatalf("expected instance id 42 on egress audit event, got %+v", audit.events[0].InstanceID)
	}
}

func TestEgressProxyHandlerAcceptsEgressInstanceHeaderAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &stubEgressAuditService{}
	handler := &EgressProxyHandler{
		policy: egresspolicy.Policy{
			Mode:               egresspolicy.ModeDenylist,
			DeniedHostSuffixes: []string{"api.openai.com"},
		},
		audit: audit,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodConnect, "https://api.openai.com:443", nil)
	ctx.Request.Host = "api.openai.com:443"
	ctx.Request.Header.Set("X-ClawManager-Egress-Instance-Id", "77")

	handler.handleConnect(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if audit.events[0].InstanceID == nil || *audit.events[0].InstanceID != 77 {
		t.Fatalf("expected instance id 77 on egress audit event, got %+v", audit.events[0].InstanceID)
	}
}

func TestEgressProxyHandlerRejectsUnsafeConnectResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &stubEgressAuditService{}
	handler := &EgressProxyHandler{
		policy: egresspolicy.Policy{Mode: egresspolicy.ModeOpen},
		audit:  audit,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, egresspolicy.ErrUnsafeTarget
		},
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodConnect, "https://127.0.0.1:8443", nil)
	ctx.Request.Host = "127.0.0.1:8443"
	handler.handleConnect(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected unsafe target audit event, got %+v", audit.events)
	}
}

func TestEgressProxyHandlerServesSignedTeamArtifactPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceRoot := t.TempDir()
	secretName := "team-94-token"
	team := &models.Team{
		ID:                  94,
		UserID:              7,
		TeamTokenSecretName: &secretName,
	}
	artifactRoot := filepath.Join(k8s.TeamSharedWorkspacePath(workspaceRoot, team.UserID, team.ID), "results", "task-193")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatalf("mkdir artifact root: %v", err)
	}
	const artifact = "<!doctype html><script src=\"app.js\"></script>"
	if err := os.WriteFile(filepath.Join(artifactRoot, "kanban.html"), []byte(artifact), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	token := "preview-secret"
	prefix := "results/task-193"
	encodedPrefix := base64.RawURLEncoding.EncodeToString([]byte(prefix))
	signature := signTeamPreviewForTest(token, team.ID, prefix)
	const previewOrigin = "http://clawmanager-egress-proxy.clawmanager-hxc-peer-system.svc.cluster.local:3128"
	target := previewOrigin + "/v1/94/" + encodedPrefix + "/" + signature + "/kanban.html"

	handler := NewEgressProxyHandler(
		nil,
		WithTeamArtifactPreview(
			&stubTeamPreviewRepository{team: team},
			&stubTeamPreviewSecrets{token: token},
			workspaceRoot,
			func(int) string { return "clawmanager-user-7" },
		),
		WithTeamArtifactPreviewOrigin(previewOrigin),
	)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	handler.Handle(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != artifact {
		t.Fatalf("preview body = %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Fatalf("missing preview sandbox CSP: %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("preview cache policy = %q", got)
	}

	directRecorder := httptest.NewRecorder()
	directContext, _ := gin.CreateTestContext(directRecorder)
	directContext.Request = httptest.NewRequest(
		http.MethodHead,
		"/v1/94/"+encodedPrefix+"/"+signature+"/kanban.html",
		nil,
	)
	directContext.Request.Host = "clawmanager-egress-proxy.clawmanager-hxc-peer-system.svc.cluster.local:3128"
	handler.Handle(directContext)
	if directRecorder.Code != http.StatusOK {
		t.Fatalf("direct managed-proxy origin preview expected 200, got %d: %s", directRecorder.Code, directRecorder.Body.String())
	}
	if directRecorder.Body.Len() != 0 {
		t.Fatalf("HEAD preview must not return a body")
	}
}

func TestEgressProxyHandlerDoesNotInterceptArbitraryPreviewLikeHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewEgressProxyHandler(
		nil,
		WithTeamArtifactPreviewOrigin("http://clawmanager-egress-proxy.clawmanager-hxc-peer-system.svc.cluster.local:3128"),
	)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "http://attacker.example/v1/94/_/invalid/index.html", nil)
	handler.Handle(ctx)
	if recorder.Code == http.StatusBadRequest || recorder.Code == http.StatusForbidden {
		t.Fatalf("arbitrary host was incorrectly treated as a Team preview: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestEgressProxyHandlerServesInteractivePreviewOnIsolatedSignedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceRoot := t.TempDir()
	secretName := "team-94-token"
	team := &models.Team{ID: 94, UserID: 7, TeamTokenSecretName: &secretName}
	artifactRoot := filepath.Join(k8s.TeamSharedWorkspacePath(workspaceRoot, team.UserID, team.ID), "results", "task-193")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatalf("mkdir artifact root: %v", err)
	}
	const artifact = `<!doctype html><style>body{color:red}</style><script>localStorage.setItem("ready","1")</script>`
	if err := os.WriteFile(filepath.Join(artifactRoot, "kanban.html"), []byte(artifact), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	const token = "preview-secret"
	const prefix = "results/task-193"
	encodedPrefix := base64.RawURLEncoding.EncodeToString([]byte(prefix))
	signature := signTeamPreviewModeForTest(token, team.ID, prefix, teamPreviewInteractiveMode)
	target := "http://" + isolatedInteractivePreviewHost(signature) + "/v2/interactive/94/" + encodedPrefix + "/" + signature + "/kanban.html"
	handler := NewEgressProxyHandler(nil, WithTeamArtifactPreview(
		&stubTeamPreviewRepository{team: team},
		&stubTeamPreviewSecrets{token: token},
		workspaceRoot,
		func(int) string { return "clawmanager-user-7" },
	))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	handler.Handle(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	csp := recorder.Header().Get("Content-Security-Policy")
	for _, expected := range []string{"'unsafe-inline'", "connect-src 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, expected) {
			t.Fatalf("interactive CSP %q missing %q", csp, expected)
		}
	}

	wrongOrigin := httptest.NewRecorder()
	wrongCtx, _ := gin.CreateTestContext(wrongOrigin)
	wrongCtx.Request = httptest.NewRequest(http.MethodGet, "http://p-wrong."+teamPreviewHost+"/v2/interactive/94/"+encodedPrefix+"/"+signature+"/kanban.html", nil)
	handler.Handle(wrongCtx)
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("interactive preview on a non-isolated origin returned %d", wrongOrigin.Code)
	}
}

func TestEgressProxyHandlerRejectsInvalidTeamArtifactPreviewSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secretName := "team-94-token"
	team := &models.Team{ID: 94, UserID: 7, TeamTokenSecretName: &secretName}
	handler := NewEgressProxyHandler(
		nil,
		WithTeamArtifactPreview(
			&stubTeamPreviewRepository{team: team},
			&stubTeamPreviewSecrets{token: "preview-secret"},
			t.TempDir(),
			func(int) string { return "clawmanager-user-7" },
		),
	)

	target := "http://" + teamPreviewHost + "/v1/94/_/invalid/index.html"
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	handler.Handle(ctx)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}

func signTeamPreviewForTest(token string, teamID int, prefix string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(teamPreviewSignaturePayload(teamID, prefix)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func signTeamPreviewModeForTest(token string, teamID int, prefix, mode string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(teamPreviewSignaturePayloadForMode(teamID, prefix, mode)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
