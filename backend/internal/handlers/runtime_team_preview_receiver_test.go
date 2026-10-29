package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes/fake"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type previewReceiverRepo struct {
	repository.TeamRepository
	mu     sync.Mutex
	team   models.Team
	member models.TeamMember
}

func (r *previewReceiverRepo) GetTeamByID(id int) (*models.Team, error) {
	return r.GetTeamByIDContext(context.Background(), id)
}

func (r *previewReceiverRepo) GetTeamByIDContext(ctx context.Context, id int) (*models.Team, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	team := r.team
	return &team, ctx.Err()
}
func (r *previewReceiverRepo) GetMemberByTeamInstanceContext(ctx context.Context, team, id int) (*models.TeamMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	member := r.member
	return &member, ctx.Err()
}
func (r *previewReceiverRepo) ListMembersByTeamIDContext(ctx context.Context, id int) ([]models.TeamMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []models.TeamMember{r.member}, ctx.Err()
}
func (r *previewReceiverRepo) ProtectTeamPreview(ctx context.Context, id, user int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.team.PreviewSecurityProfile = models.TeamPreviewSecurityProfile
	return ctx.Err()
}

type previewReceiverInstance struct{ instance models.Instance }

func (r previewReceiverInstance) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	instance := r.instance
	return &instance, ctx.Err()
}
func managedPreviewReceiver(t *testing.T, modes ...string) (*EgressProxyHandler, services.ManagedTeamPreviewGrant, *atomic.Bool) {
	t.Helper()
	h, g, r, _ := managedPreviewReceiverWithGuard(t, func(context.Context, runtimeauth.Identity) error { return nil }, modes...)
	return h, g, r
}
func managedPreviewReceiverWithGuard(t *testing.T, guard func(context.Context, runtimeauth.Identity) error, modes ...string) (*EgressProxyHandler, services.ManagedTeamPreviewGrant, *atomic.Bool, *previewReceiverRepo) {
	t.Helper()
	id := runtimeauth.Identity{InstanceID: 8, UserID: 1, Generation: 2, PodUID: "preview-issuer"}
	env := `{"CLAWMANAGER_TEAM_ID":"42"}`
	instances := previewReceiverInstance{models.Instance{ID: 8, UserID: 1, EnvironmentOverridesJSON: &env, InstanceMode: services.InstanceModeLite, RuntimeType: services.RuntimeBackendGateway, Type: services.RuntimeTypeOpenClaw, RuntimeSecurityProfile: services.RuntimeSecurityProfile}}
	repo := &previewReceiverRepo{team: models.Team{ID: 42, UserID: 1, Status: models.TeamStatusRunning}, member: models.TeamMember{TeamID: 42, InstanceID: &id.InstanceID, UserID: 1, Status: models.TeamMemberStatusIdle, InstanceMode: services.InstanceModeLite, RuntimeType: services.RuntimeTypeOpenClaw}}
	revoked := &atomic.Bool{}
	redis, err := services.NewManagedTeamRedis(services.NewTeamService(repo, nil), instances, func(context.Context, int) (runtimeauth.Identity, error) {
		if revoked.Load() {
			return runtimeauth.Identity{}, errors.New("revoked")
		}
		return id, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := services.NewRuntimeSecurityAuthority(fake.NewClientset(), "test", instances, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := k8s.TeamSharedWorkspacePath(root, 1, 42)
	if err = os.MkdirAll(filepath.Join(dir, "site"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "site", "index.html"), []byte("preview body"), 0600); err != nil {
		t.Fatal(err)
	}
	const origin = "http://clawmanager-egress-proxy.test.svc.cluster.local:3128"
	provider, err := services.NewManagedTeamPreview(redis, authority, root, origin, guard)
	if err != nil {
		t.Fatal(err)
	}
	mode := "interactive"
	if len(modes) > 0 {
		mode = modes[0]
	}
	grant, err := provider.Sign(context.Background(), id, "site/index.html", mode)
	if err != nil {
		t.Fatal(err)
	}
	secret := "legacy-preview-secret"
	repo.team.TeamTokenSecretName = &secret
	h := NewEgressProxyHandler(nil, WithManagedTeamPreview(provider), WithTeamArtifactPreviewOrigin(origin), WithTeamArtifactPreview(repo, &stubTeamPreviewSecrets{token: secret}, root, func(int) string { return "test" }))
	return h, grant, revoked, repo
}
func TestManagedPreviewReceiverBootstrapAndOriginIsolation(t *testing.T) {
	h, grant, _ := managedPreviewReceiver(t)
	request := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", target, nil)
		h.Handle(c)
		return rec
	}
	bootstrap := request(grant.URL)
	if bootstrap.Code != 307 {
		t.Fatalf("bootstrap: %d", bootstrap.Code)
	}
	target := bootstrap.Header().Get("Location")
	origin, _ := url.Parse(target)
	originForm := httptest.NewRequest("GET", origin.RequestURI(), nil)
	originForm.Host = origin.Host
	originResponse := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(originResponse)
	c.Request = originForm
	h.Handle(c)
	if originResponse.Code != 200 {
		t.Fatal("isolated Host rejected for origin-form request", originResponse.Code)
	}
	response := request(target)
	if response.Code != 200 || response.Body.String() != "preview body" {
		t.Fatalf("isolated origin failed: %d", response.Code)
	}
	parsed, _ := url.Parse(target)
	parsed.Host = "p-wrong.clawmanager-team-preview.invalid"
	if response = request(parsed.String()); response.Code != 403 {
		t.Fatal("wrong isolated origin accepted")
	}
}
func TestManagedPreviewReceiverRevokesBlockedWriter(t *testing.T) {
	for _, reason := range []string{"issuer revoked", "grant expired"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			h, grant, revoked := managedPreviewReceiver(t)
			if reason == "grant expired" {
				parsed, _ := url.Parse(grant.URL)
				parts := strings.Split(parsed.Path, "/")
				token := strings.Split(parts[5], ".")
				payload, _ := base64.RawURLEncoding.DecodeString(token[0])
				var claims map[string]any
				_ = json.Unmarshal(payload, &claims)
				claims["exp"] = time.Now().Add(2 * time.Second).Unix()
				payload, _ = json.Marshal(claims)
				keyMAC := hmac.New(sha256.New, []byte(strings.Repeat("k", 32)))
				keyMAC.Write([]byte("clawmanager/managed-team-preview/v3/signing-key\x00"))
				mac := hmac.New(sha256.New, keyMAC.Sum(nil))
				mac.Write([]byte("clawmanager/team-preview/v3/grant\x00"))
				mac.Write(payload)
				parts[5] = base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
				parsed.Path = strings.Join(parts, "/")
				grant.URL = parsed.String()
			}
			bootstrap := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(bootstrap)
			c.Request = httptest.NewRequest("GET", grant.URL, nil)
			h.Handle(c)
			if bootstrap.Code != 307 {
				t.Fatalf("bootstrap %d", bootstrap.Code)
			}
			writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
			c, _ = gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest("GET", bootstrap.Header().Get("Location"), nil)
			done := make(chan struct{})
			go func() { defer close(done); h.Handle(c) }()
			defer func() { writer.SetWriteDeadline(time.Now()); <-done }()
			select {
			case <-writer.writing:
			case <-time.After(time.Second):
				t.Fatal("preview did not start")
			}
			if reason == "issuer revoked" {
				revoked.Store(true)
			}
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("preview writer survived issuer loss or expiry")
			}
		})
	}
}

func TestManagedPreviewStaticHTMLCannotExecuteOnSharedOrigin(t *testing.T) {
	h, grant, _ := managedPreviewReceiver(t, "static")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", grant.URL, nil)
	h.Handle(c)
	if rec.Code != 200 || strings.Contains(rec.Header().Get("Content-Security-Policy"), "allow-scripts") || strings.Contains(rec.Header().Get("Content-Security-Policy"), "allow-same-origin") {
		t.Fatal("static HTML can execute on the shared bootstrap origin", rec.Code)
	}
}

func TestManagedPreviewReceiverBlockedCheckCannotAccumulateDetachedWorkers(t *testing.T) {
	var block atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	guard := func(context.Context, runtimeauth.Identity) error {
		if block.Load() {
			close(entered)
			<-release
		}
		return nil
	}
	h, grant, _, _ := managedPreviewReceiverWithGuard(t, guard, "static")
	writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("GET", grant.URL, nil)
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(c) }()
	defer func() { close(release); writer.SetWriteDeadline(time.Now()); <-done }()
	select {
	case <-writer.writing:
	case <-time.After(time.Second):
		t.Fatal("preview did not start")
	}
	block.Store(true)
	select {
	case <-entered:
	case <-time.After(6 * time.Second):
		t.Fatal("guard check did not start")
	}
	select {
	case <-writer.release:
	case <-time.After(3 * time.Second):
		t.Fatal("blocked issuer check retained stream past its deadline")
	}
	select {
	case <-done:
		t.Fatal("receiver detached blocked check")
	default:
	}
}

func TestManagedPreviewReceiverFirstProtectionRevokesExistingLegacyStream(t *testing.T) {
	h, _, _, repo := managedPreviewReceiverWithGuard(t, func(context.Context, runtimeauth.Identity) error { return nil })
	repo.mu.Lock()
	repo.team.PreviewSecurityProfile = ""
	repo.mu.Unlock()
	const prefix = "site"
	target := "http://clawmanager-egress-proxy.test.svc.cluster.local:3128/v1/42/" + base64.RawURLEncoding.EncodeToString([]byte(prefix)) + "/" + signTeamPreviewForTest("legacy-preview-secret", 42, prefix) + "/index.html"
	writer := &blockedEgressWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("GET", target, nil)
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(c) }()
	defer func() { writer.SetWriteDeadline(time.Now()); <-done }()
	select {
	case <-writer.writing:
	case <-time.After(time.Second):
		t.Fatal("legacy preview did not start")
	}
	repo.mu.Lock()
	repo.team.PreviewSecurityProfile = models.TeamPreviewSecurityProfile
	repo.mu.Unlock()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("legacy stream retained retired Team authority")
	}
}
