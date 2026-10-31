package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type previewTeamFixture struct {
	*managedTeamFixture
	protectErr error
	members    []models.TeamMember
	listErr    error
}

func (f *previewTeamFixture) ListMembersByTeamIDContext(ctx context.Context, team int) ([]models.TeamMember, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.members != nil {
		return append([]models.TeamMember(nil), f.members...), ctx.Err()
	}
	return []models.TeamMember{*f.member}, ctx.Err()
}
func (f *previewTeamFixture) ProtectTeamPreview(ctx context.Context, id, user int) error {
	if f.protectErr != nil {
		return f.protectErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.team.ID != id || f.team.UserID != user || f.team.Status != models.TeamStatusRunning {
		return errors.New("lost team")
	}
	if f.team.PreviewSecurityProfile != "" && f.team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
		return errors.New("unknown marker")
	}
	f.team.PreviewSecurityProfile = models.TeamPreviewSecurityProfile
	return nil
}
func previewFixture(t *testing.T) (*ManagedTeamPreview, *previewTeamFixture, runtimeauth.Identity, string) {
	t.Helper()
	env := `{"CLAWMANAGER_TEAM_ID":"12"}`
	id := 7
	identity := runtimeauth.Identity{UserID: 3, InstanceID: id, Generation: 4, PodUID: "preview-pod"}
	teams := &previewTeamFixture{managedTeamFixture: &managedTeamFixture{team: &models.Team{ID: 12, UserID: 3, Status: models.TeamStatusRunning}, member: &models.TeamMember{TeamID: 12, InstanceID: &id, UserID: 3, Status: models.TeamMemberStatusIdle, InstanceMode: InstanceModeLite, RuntimeType: RuntimeTypeOpenClaw}}}
	redis := &ManagedTeamRedis{instances: managedInstanceFixture{&models.Instance{ID: id, UserID: 3, EnvironmentOverridesJSON: &env, InstanceMode: InstanceModeLite, RuntimeType: RuntimeBackendGateway, Type: RuntimeTypeOpenClaw, RuntimeSecurityProfile: RuntimeSecurityProfile}}, teams: teams, current: func(context.Context, int) (runtimeauth.Identity, error) { return identity, nil }}
	root := t.TempDir()
	dir := filepath.Join(k8s.TeamSharedWorkspacePath(root, 3, 12), "site")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "asset.js"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewManagedTeamPreview(redis, &RuntimeSecurityAuthority{key: []byte(strings.Repeat("k", 32))}, root, "http://clawmanager-egress-proxy.ns.svc.cluster.local:3128", func(context.Context, runtimeauth.Identity) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return p, teams, identity, dir
}
func previewPath(t *testing.T, g ManagedTeamPreviewGrant) string {
	t.Helper()
	u, e := url.Parse(g.URL)
	if e != nil {
		t.Fatal(e)
	}
	return u.EscapedPath()
}
func TestManagedPreviewSignOpenScopeAndStickyProtection(t *testing.T) {
	p, teams, id, _ := previewFixture(t)
	grant, err := p.Sign(context.Background(), id, "site/index.html", "interactive")
	if err != nil {
		t.Fatal(err)
	}
	if teams.team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
		t.Fatal("grant preceded durable protection")
	}
	if grant.Mode != "interactive" || time.Until(grant.ExpiresAt) > 5*time.Minute {
		t.Fatal("invalid expiry or mode")
	}
	path := previewPath(t, grant)
	f, err := p.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !strings.HasSuffix(f.IsolatedHost, ".clawmanager-team-preview.invalid") {
		t.Fatal("missing isolated origin")
	}
	sibling := strings.TrimSuffix(path, "index.html") + "asset.js"
	asset, err := p.Open(context.Background(), sibling)
	if err != nil {
		t.Fatal(err)
	}
	asset.Close()
	for _, bad := range []string{strings.Replace(path, "/interactive/", "/static/", 1), strings.Replace(path, "/12/", "/13/", 1), strings.TrimSuffix(path, "index.html") + "../other/file", strings.TrimSuffix(path, "index.html") + "%2e%2e/file", path + "%2fother"} {
		if f, e := p.Open(context.Background(), bad); e == nil {
			f.Close()
			t.Errorf("accepted altered grant %q", bad)
		}
	}
	teams.member.Status = models.TeamMemberStatusDeleted
	if err := f.Check(context.Background()); err == nil {
		t.Fatal("deleted issuer remained authorized")
	}
	if teams.team.PreviewSecurityProfile == "" {
		t.Fatal("membership deletion reset sticky marker")
	}
}
func TestManagedPreviewFailsClosedAndPinsFiles(t *testing.T) {
	p, teams, id, dir := previewFixture(t)
	for _, bad := range []string{"../site/index.html", "/site/index.html", "site//index.html", "site/./index.html", "site\\index.html"} {
		if _, e := p.Sign(context.Background(), id, bad, "static"); e == nil {
			t.Fatalf("accepted path %q", bad)
		}
	}
	if _, e := p.Sign(context.Background(), id, "site/index.html", "unknown"); e == nil {
		t.Fatal("accepted mode")
	}
	teams.protectErr = errors.New("storage unavailable")
	if _, e := p.Sign(context.Background(), id, "site/index.html", "interactive"); e == nil {
		t.Fatal("issued without durable marker")
	}
	teams.protectErr = nil
	grant, e := p.Sign(context.Background(), id, "site/index.html", "interactive")
	if e != nil {
		t.Fatal(e)
	}
	f, e := p.Open(context.Background(), previewPath(t, grant))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = os.Rename(filepath.Join(dir, "index.html"), filepath.Join(dir, "old.html")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "index.html"), []byte("replacement"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.Check(context.Background()); e == nil {
		t.Fatal("replaced inode remained bound")
	}
	if e = os.Link(filepath.Join(dir, "asset.js"), filepath.Join(dir, "hard.js")); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Sign(context.Background(), id, "site/hard.js", "static"); e == nil {
		t.Fatal("hardlink signed")
	}
	if e = os.Symlink("asset.js", filepath.Join(dir, "soft.js")); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Sign(context.Background(), id, "site/soft.js", "static"); e == nil {
		t.Fatal("symlink signed")
	}
	p.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	if f, e := p.Open(context.Background(), previewPath(t, grant)); e == nil {
		f.Close()
		t.Fatal("expired grant opened")
	}
}

func TestManagedPreviewRejectsWrongIssuerMetadataAndPrefixEscape(t *testing.T) {
	p, teams, id, dir := previewFixture(t)
	grant, err := p.Sign(context.Background(), id, "site/index.html", "interactive")
	if err != nil {
		t.Fatal(err)
	}
	original := previewPath(t, grant)
	parts := strings.Split(original, "/")
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(parts[5], ".")[0])
	if err != nil {
		t.Fatal(err)
	}
	var claims managedPreviewClaims
	if json.Unmarshal(payload, &claims) != nil {
		t.Fatal("claims")
	}
	for name, change := range map[string]func(*managedPreviewClaims){
		"future issued":    func(c *managedPreviewClaims) { c.Issued += 60; c.Expires += 60 },
		"too long":         func(c *managedPreviewClaims) { c.Expires++ },
		"wrong generation": func(c *managedPreviewClaims) { c.Issuer.Generation++ },
		"wrong Pod":        func(c *managedPreviewClaims) { c.Issuer.PodUID = "replacement" },
		"wrong user":       func(c *managedPreviewClaims) { c.Issuer.UserID++ },
		"wrong instance":   func(c *managedPreviewClaims) { c.Issuer.InstanceID++ },
		"unknown version":  func(c *managedPreviewClaims) { c.Version = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := claims
			change(&changed)
			body, _ := json.Marshal(changed)
			pathParts := append([]string(nil), parts...)
			pathParts[5] = base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(p.signature(body))
			if f, e := p.Open(context.Background(), strings.Join(pathParts, "/")); e == nil {
				f.Close()
				t.Fatal("invalid signed claims accepted")
			}
		})
	}
	f, err := p.Open(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = os.MkdirAll(filepath.Join(filepath.Dir(dir), "outside"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(dir), "outside", "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("../outside", filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if f, e := p.Open(context.Background(), strings.TrimSuffix(original, "index.html")+"escape/secret"); e == nil {
		f.Close()
		t.Fatal("asset escaped signed prefix")
	}
	teams.team.PreviewSecurityProfile = "unknown-future"
	if err = f.Check(context.Background()); err == nil {
		t.Fatal("unknown marker accepted")
	}
	if _, err = p.Sign(context.Background(), id, "site/index.html", "interactive"); err == nil {
		t.Fatal("unknown marker overwritten")
	}
}

func TestManagedPreviewBoundedAdmission(t *testing.T) {
	p, _, id, _ := previewFixture(t)
	grant, err := p.Sign(context.Background(), id, "site/index.html", "interactive")
	if err != nil {
		t.Fatal(err)
	}
	var opened []*ManagedTeamPreviewFile
	defer func() {
		for _, file := range opened {
			file.Close()
		}
	}()
	for i := 0; i < 64; i++ {
		file, err := p.Open(context.Background(), previewPath(t, grant))
		if err != nil {
			t.Fatal(err)
		}
		opened = append(opened, file)
	}
	if file, err := p.Open(context.Background(), previewPath(t, grant)); err == nil {
		file.Close()
		t.Fatal("preview connections exceed admission bound")
	}
	opened[0].Close()
	file, err := p.Open(context.Background(), previewPath(t, grant))
	if err != nil {
		t.Fatal("admission not released", err)
	}
	file.Close()
}

func TestManagedPreviewWholeTeamMigrationGate(t *testing.T) {
	for _, scenario := range []string{"Pro member", "legacy Lite", "missing instance", "wrong SQL Team", "member list unavailable", "unsupported image", "second Pro member", "duplicate binding", "too many members"} {
		t.Run(scenario, func(t *testing.T) {
			p, teams, id, _ := previewFixture(t)
			instance := p.redis.instances.(managedInstanceFixture).instance
			switch scenario {
			case "Pro member":
				teams.member.InstanceMode = InstanceModePro
			case "legacy Lite":
				instance.RuntimeSecurityProfile = ""
			case "missing instance":
				teams.member.InstanceID = nil
			case "wrong SQL Team":
				env := `{"CLAWMANAGER_TEAM_ID":"13"}`
				instance.EnvironmentOverridesJSON = &env
			case "member list unavailable":
				teams.listErr = errors.New("SQL unavailable")
			case "unsupported image":
				p.guard = func(context.Context, runtimeauth.Identity) error { return errors.New("unapproved image") }
			case "second Pro member":
				other := *teams.member
				other.InstanceMode = InstanceModePro
				other.ID = 99
				teams.members = []models.TeamMember{*teams.member, other}
			case "duplicate binding":
				teams.members = []models.TeamMember{*teams.member, *teams.member}
			case "too many members":
				teams.members = make([]models.TeamMember, 257)
				for i := range teams.members {
					teams.members[i] = *teams.member
				}
			}
			if err := p.Protect(context.Background(), id); err == nil {
				t.Fatal("partial or unverified Team migration succeeded")
			}
			if teams.team.PreviewSecurityProfile != "" {
				t.Fatal("failed gate persisted irreversible marker")
			}
		})
	}
	p, _, _, _ := previewFixture(t)
	if _, err := NewManagedTeamPreview(p.redis, &RuntimeSecurityAuthority{key: []byte(strings.Repeat("k", 32))}, p.workspaceRoot, p.origin, nil); err == nil {
		t.Fatal("nil image guard accepted")
	}
}
func TestManagedPreviewImageRollbackRevokesMarkedTeam(t *testing.T) {
	p, teams, id, _ := previewFixture(t)
	grant, err := p.Sign(context.Background(), id, "site/index.html", "interactive")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := p.Open(context.Background(), previewPath(t, grant))
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	p.guard = func(context.Context, runtimeauth.Identity) error { return errors.New("old image returned") }
	if err = binding.Check(context.Background()); err == nil {
		t.Fatal("active preview survived image rollback")
	}
	if opened, err := p.Open(context.Background(), previewPath(t, grant)); err == nil {
		opened.Close()
		t.Fatal("old image redeemed grant")
	}
	if _, err = p.Sign(context.Background(), id, "site/index.html", "interactive"); err == nil {
		t.Fatal("old image signed grant")
	}
	if teams.team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
		t.Fatal("image rollback revived legacy grants")
	}
}

type previewInstances map[int]*models.Instance

func (m previewInstances) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	instance, ok := m[id]
	if !ok {
		return nil, errors.New("missing")
	}
	return instance, nil
}
func (f *previewTeamFixture) GetMemberByTeamInstanceContext(ctx context.Context, team, instance int) (*models.TeamMember, error) {
	if f.members != nil {
		for _, member := range f.members {
			if member.InstanceID != nil && *member.InstanceID == instance {
				copy := member
				return &copy, ctx.Err()
			}
		}
		return nil, errors.New("missing member")
	}
	return f.managedTeamFixture.GetMemberByTeamInstanceContext(ctx, team, instance)
}
func TestManagedPreviewWholeTeamGateChecksEveryReadyMember(t *testing.T) {
	p, teams, id, _ := previewFixture(t)
	instance := p.redis.instances.(managedInstanceFixture).instance
	otherInstance := *instance
	otherInstance.ID = 8
	otherInstance.Type = RuntimeTypeHermes
	otherMember := *teams.member
	otherMember.ID = 99
	otherMember.InstanceID = &otherInstance.ID
	otherMember.RuntimeType = RuntimeTypeHermes
	removed := otherMember
	removed.Status = models.TeamMemberStatusDeleted
	removed.InstanceMode = InstanceModePro
	removed.InstanceID = nil
	teams.members = []models.TeamMember{*teams.member, otherMember, removed}
	p.redis.instances = previewInstances{id.InstanceID: instance, otherInstance.ID: &otherInstance}
	p.redis.current = func(ctx context.Context, instance int) (runtimeauth.Identity, error) {
		current := id
		current.InstanceID = instance
		if instance != id.InstanceID {
			current.PodUID = "other-pod"
		}
		return current, ctx.Err()
	}
	seen := map[int]bool{}
	p.guard = func(ctx context.Context, current runtimeauth.Identity) error {
		seen[current.InstanceID] = true
		return ctx.Err()
	}
	if err := p.Protect(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !seen[7] || !seen[8] || teams.team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
		t.Fatal("whole-Team proof incomplete", seen)
	}
}
