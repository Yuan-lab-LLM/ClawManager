package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"github.com/gin-gonic/gin"
)

type observedPreviewRepository struct {
	team  *models.Team
	calls atomic.Int32
}

func (s *observedPreviewRepository) GetTeamByID(int) (*models.Team, error) {
	s.calls.Add(1)
	return s.team, nil
}

func protectedPreviewFixture(t *testing.T, verifier RuntimeCapabilityVerifier) (*EgressProxyHandler, string, *observedPreviewRepository) {
	t.Helper()
	root := t.TempDir()
	secretName := "team-secret"
	team := &models.Team{ID: 94, UserID: 1, TeamTokenSecretName: &secretName}
	repo := &observedPreviewRepository{team: team}
	dir := filepath.Join(k8s.TeamSharedWorkspacePath(root, 1, 94), "results")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("preview sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	const origin = "http://preview.test:3129"
	const signingSecret = "preview-signing-secret"
	target := origin + "/v1/94/" + base64.RawURLEncoding.EncodeToString([]byte("results")) + "/" + signTeamPreviewForTest(signingSecret, 94, "results") + "/artifact.txt"
	h := NewEgressProxyHandler(nil, WithTeamArtifactPreview(repo, &stubTeamPreviewSecrets{token: signingSecret}, root, func(int) string { return "test" }), WithTeamArtifactPreviewOrigin(origin))
	if verifier != nil {
		WithEgressRuntimeCapabilities(verifier)(h)
	}
	return h, target, repo
}

func TestProtectedTeamPreviewRequiresCurrentEgressCapabilityBeforeFileAccess(t *testing.T) {
	s, good, model := newEgressCapabilityService(t)
	for _, scenario := range []string{"public", "missing", "wrong-audience", "stale", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			var verifier RuntimeCapabilityVerifier = s
			if scenario == "public" {
				verifier = nil
			}
			if scenario == "stale" {
				verifier = egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
					return runtimeauth.Identity{}, errors.New("stale")
				})
			}
			h, target, repo := protectedPreviewFixture(t, verifier)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("GET", target, nil)
			if scenario != "missing" {
				token := good
				if scenario == "wrong-audience" {
					token = model
				}
				c.Request.Header.Set("Proxy-Authorization", "Bearer "+token)
			}
			h.Handle(c)
			if scenario == "public" || scenario == "valid" {
				if rec.Code != 200 || rec.Body.String() != "preview sentinel" {
					t.Fatalf("preview unavailable: %d %s", rec.Code, rec.Body.String())
				}
			} else if rec.Code == 200 || repo.calls.Load() != 0 {
				t.Fatalf("preview accessed before current capability: status=%d lookups=%d", rec.Code, repo.calls.Load())
			}
		})
	}
}

func TestProtectedTeamPreviewRevocationUnblocksStreamingClient(t *testing.T) {
	var revoked atomic.Bool
	verifier := egressVerifierFunc(func(context.Context, string, string) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errors.New("revoked")
		}
		return egressCapabilityIdentity, nil
	})
	h, target, _ := protectedPreviewFixture(t, verifier)
	writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Request.Header.Set("Proxy-Authorization", "Bearer test")
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(c) }()
	defer func() { writer.SetWriteDeadline(time.Now()); <-done }()
	select {
	case <-writer.writing:
	case <-time.After(time.Second):
		t.Fatal("preview did not start streaming")
	}
	revoked.Store(true)
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("revoked preview remains blocked writing")
	}
}
