package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
)

var errManagedPreview = errors.New("managed Team preview unavailable")
var managedPreviewAdmission = make(chan struct{}, 64)

const managedPreviewTTL = 5 * time.Minute
const managedPreviewMaxSize = 64 << 20

type managedPreviewProtection interface {
	ProtectTeamPreview(context.Context, int, int) error
	ListMembersByTeamIDContext(context.Context, int) ([]models.TeamMember, error)
}
type ManagedTeamPreview struct {
	redis                 *ManagedTeamRedis
	protection            managedPreviewProtection
	key                   []byte
	workspaceRoot, origin string
	now                   func() time.Time
	guard                 func(context.Context, runtimeauth.Identity) error
}
type ManagedTeamPreviewGrant struct {
	URL       string    `json:"url"`
	Mode      string    `json:"mode"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type managedPreviewClaims struct {
	Version int                  `json:"v"`
	Team    int                  `json:"team"`
	Prefix  string               `json:"prefix"`
	Mode    string               `json:"mode"`
	Issuer  runtimeauth.Identity `json:"issuer"`
	Issued  int64                `json:"iat"`
	Expires int64                `json:"exp"`
}

func NewManagedTeamPreview(redis *ManagedTeamRedis, authority *RuntimeSecurityAuthority, workspaceRoot, previewOrigin string, guard func(context.Context, runtimeauth.Identity) error) (*ManagedTeamPreview, error) {
	if redis == nil || authority == nil || guard == nil || len(authority.key) < 32 || !filepath.IsAbs(workspaceRoot) {
		return nil, errManagedPreview
	}
	protection, ok := redis.teams.(managedPreviewProtection)
	if !ok {
		return nil, errManagedPreview
	}
	origin, err := url.Parse(previewOrigin)
	if err != nil || origin.Scheme != "http" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errManagedPreview
	}
	labels := strings.Split(origin.Hostname(), ".")
	if len(labels) < 3 || labels[0] != "clawmanager-egress-proxy" || labels[1] == "" || labels[2] != "svc" {
		return nil, errManagedPreview
	}
	mac := hmac.New(sha256.New, authority.key)
	mac.Write([]byte("clawmanager/managed-team-preview/v3/signing-key\x00"))
	return &ManagedTeamPreview{redis: redis, protection: protection, key: mac.Sum(nil), workspaceRoot: workspaceRoot, origin: strings.TrimSuffix(origin.String(), "/"), now: time.Now, guard: guard}, nil
}
func (p *ManagedTeamPreview) protect(ctx context.Context, id runtimeauth.Identity) (*models.Team, error) {
	team, err := p.redis.authorize(ctx, id, 0)
	if err != nil {
		return nil, errManagedPreview
	}
	if team.PreviewSecurityProfile == "" {
		if err = p.migrationReady(ctx, team, id); err != nil {
			return nil, errManagedPreview
		}
	} else if team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile || p.guard(ctx, id) != nil {
		return nil, errManagedPreview
	}
	if err = p.protection.ProtectTeamPreview(ctx, team.ID, team.UserID); err != nil {
		return nil, errManagedPreview
	}
	team, err = p.redis.authorize(ctx, id, team.ID)
	if err != nil || team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
		return nil, errManagedPreview
	}
	return team, nil
}

// Retiring legacy grants is a Team-wide transition. Every extant member must
// have a protected Lite assignment and a trusted, approved native image before
// the first durable CAS. A Pro/legacy/missing member leaves the marker empty.
func (p *ManagedTeamPreview) migrationReady(ctx context.Context, team *models.Team, issuer runtimeauth.Identity) error {
	members, err := p.protection.ListMembersByTeamIDContext(ctx, team.ID)
	if err != nil || len(members) == 0 || len(members) > 256 {
		return errManagedPreview
	}
	seen := make(map[int]bool, len(members))
	for _, member := range members {
		if member.Status == models.TeamMemberStatusDeleting || member.Status == models.TeamMemberStatusDeleted {
			continue
		}
		if member.TeamID != team.ID || member.UserID != team.UserID || member.InstanceMode != InstanceModeLite || member.InstanceID == nil || *member.InstanceID < 1 || seen[*member.InstanceID] {
			return errManagedPreview
		}
		if member.RuntimeType != RuntimeTypeOpenClaw && member.RuntimeType != RuntimeTypeHermes {
			return errManagedPreview
		}
		switch member.Status {
		case models.TeamMemberStatusCreating, models.TeamMemberStatusIdle, models.TeamMemberStatusBusy:
		default:
			return errManagedPreview
		}
		instanceID := *member.InstanceID
		seen[instanceID] = true
		instance, err := p.redis.instances.GetByIDContext(ctx, instanceID)
		if err != nil || instance == nil || instance.ID != instanceID || instance.UserID != team.UserID || instance.InstanceMode != InstanceModeLite || instance.RuntimeType != RuntimeBackendGateway || instance.Type != member.RuntimeType || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
			return errManagedPreview
		}
		current, err := p.redis.current(ctx, instanceID)
		if err != nil || current.InstanceID != instanceID || current.UserID != team.UserID {
			return errManagedPreview
		}
		if _, err = p.redis.authorize(ctx, current, team.ID); err != nil || p.guard(ctx, current) != nil {
			return errManagedPreview
		}
	}
	if !seen[issuer.InstanceID] || ctx.Err() != nil {
		return errManagedPreview
	}
	return nil
}

func (p *ManagedTeamPreview) Protect(ctx context.Context, id runtimeauth.Identity) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := p.protect(ctx, id)
	return err
}
func (p *ManagedTeamPreview) Sign(ctx context.Context, id runtimeauth.Identity, relative, mode string) (ManagedTeamPreviewGrant, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !validManagedPreviewPath(relative) || !validManagedPreviewMode(mode) {
		return ManagedTeamPreviewGrant{}, errManagedPreview
	}
	team, err := p.protect(ctx, id)
	if err != nil {
		return ManagedTeamPreviewGrant{}, err
	}
	prefix := path.Dir(relative)
	if prefix == "." {
		prefix = ""
	}
	file, _, err := p.openFile(team.UserID, team.ID, prefix, path.Base(relative))
	if err != nil {
		return ManagedTeamPreviewGrant{}, errManagedPreview
	}
	file.Close()
	if ctx.Err() != nil {
		return ManagedTeamPreviewGrant{}, errManagedPreview
	}
	now := p.now().Unix()
	claims := managedPreviewClaims{Version: 3, Team: team.ID, Prefix: prefix, Mode: mode, Issuer: id, Issued: now, Expires: now + int64(managedPreviewTTL/time.Second)}
	payload, _ := json.Marshal(claims)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(p.signature(payload))
	prefix64 := "_"
	if prefix != "" {
		prefix64 = base64.RawURLEncoding.EncodeToString([]byte(prefix))
	}
	return ManagedTeamPreviewGrant{URL: p.origin + "/v3/" + mode + "/" + strconv.Itoa(team.ID) + "/" + prefix64 + "/" + token + "/" + url.PathEscape(path.Base(relative)), Mode: mode, ExpiresAt: time.Unix(claims.Expires, 0).UTC()}, nil
}
func (p *ManagedTeamPreview) signature(payload []byte) []byte {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte("clawmanager/team-preview/v3/grant\x00"))
	mac.Write(payload)
	return mac.Sum(nil)
}
func validManagedPreviewMode(mode string) bool { return mode == "static" || mode == "interactive" }
func validManagedPreviewPath(value string) bool {
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\\\x00\r\n") || strings.TrimSpace(value) != value {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
func (p *ManagedTeamPreview) parse(escaped string) (managedPreviewClaims, string, string, error) {
	var claims managedPreviewClaims
	if len(escaped) > 8192 {
		return claims, "", "", errManagedPreview
	}
	parts := strings.Split(escaped, "/")
	if len(parts) < 7 || parts[0] != "" || parts[1] != "v3" || !validManagedPreviewMode(parts[2]) {
		return claims, "", "", errManagedPreview
	}
	tokenParts := strings.Split(parts[5], ".")
	if len(tokenParts) != 2 {
		return claims, "", "", errManagedPreview
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(tokenParts[0])
	if err != nil {
		return claims, "", "", errManagedPreview
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(tokenParts[1])
	if err != nil || !hmac.Equal(sig, p.signature(payload)) || json.Unmarshal(payload, &claims) != nil {
		return claims, "", "", errManagedPreview
	}
	prefix64 := "_"
	if claims.Prefix != "" {
		prefix64 = base64.RawURLEncoding.EncodeToString([]byte(claims.Prefix))
	}
	now := p.now().Unix()
	if claims.Version != 3 || claims.Team < 1 || parts[3] != strconv.Itoa(claims.Team) || parts[2] != claims.Mode || parts[4] != prefix64 || (claims.Prefix != "" && !validManagedPreviewPath(claims.Prefix)) || claims.Issued > now || claims.Expires <= now || claims.Expires <= claims.Issued || claims.Expires-claims.Issued > 300 || claims.Expires-now > 300 {
		return claims, "", "", errManagedPreview
	}
	segments := make([]string, len(parts)-6)
	for i, segment := range parts[6:] {
		decoded, err := url.PathUnescape(segment)
		if err != nil || strings.Contains(decoded, "/") || url.PathEscape(decoded) != segment {
			return claims, "", "", errManagedPreview
		}
		segments[i] = decoded
	}
	relative := strings.Join(segments, "/")
	if !validManagedPreviewPath(relative) {
		return claims, "", "", errManagedPreview
	}
	hash := sha256.Sum256([]byte(parts[5]))
	return claims, relative, "p-" + hex.EncodeToString(hash[:20]) + ".clawmanager-team-preview.invalid", nil
}

type ManagedTeamPreviewFile struct {
	File           *os.File
	Mode           string
	ExpiresAt      time.Time
	IsolatedHost   string
	provider       *ManagedTeamPreview
	claims         managedPreviewClaims
	relative       string
	info, rootInfo os.FileInfo
	once           sync.Once
	fileOnce       sync.Once
	closeErr       error
	legacy         bool
	legacyUser     int
}

func (p *ManagedTeamPreview) Open(ctx context.Context, escapedPath string) (binding *ManagedTeamPreviewFile, err error) {
	select {
	case managedPreviewAdmission <- struct{}{}:
	default:
		return nil, errManagedPreview
	}
	defer func() {
		if binding == nil {
			<-managedPreviewAdmission
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	claims, relative, host, err := p.parse(escapedPath)
	if err != nil {
		return nil, err
	}
	team, err := p.redis.authorize(ctx, claims.Issuer, claims.Team)
	if err != nil || team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile || p.guard(ctx, claims.Issuer) != nil {
		return nil, errManagedPreview
	}
	file, rootInfo, err := p.openFile(team.UserID, team.ID, claims.Prefix, relative)
	if err != nil {
		return nil, errManagedPreview
	}
	info, err := file.Stat()
	if err != nil || ctx.Err() != nil || !p.now().Before(time.Unix(claims.Expires, 0)) {
		file.Close()
		return nil, errManagedPreview
	}
	return &ManagedTeamPreviewFile{File: file, Mode: claims.Mode, ExpiresAt: time.Unix(claims.Expires, 0), IsolatedHost: host, provider: p, claims: claims, relative: relative, info: info, rootInfo: rootInfo}, nil
}
func (f *ManagedTeamPreviewFile) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !f.ExpiresAt.IsZero() && !f.provider.now().Before(f.ExpiresAt) {
		return errManagedPreview
	}
	var team *models.Team
	var err error
	if f.legacy {
		team, err = f.provider.legacyTeam(ctx, f.claims.Team, f.legacyUser)
		if err != nil {
			return errManagedPreview
		}
	} else {
		team, err = f.provider.redis.authorize(ctx, f.claims.Issuer, f.claims.Team)
		if err != nil || team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile || f.provider.guard(ctx, f.claims.Issuer) != nil {
			return errManagedPreview
		}
	}
	file, rootInfo, err := f.provider.openFile(team.UserID, team.ID, f.claims.Prefix, f.relative)
	if err != nil {
		return errManagedPreview
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, f.info) || !os.SameFile(rootInfo, f.rootInfo) {
		return errManagedPreview
	}
	pinned, err := f.File.Stat()
	if err != nil || workspaceHasMultipleLinks(pinned) || ctx.Err() != nil || (!f.ExpiresAt.IsZero() && !f.provider.now().Before(f.ExpiresAt)) {
		return errManagedPreview
	}
	return nil
}
func (f *ManagedTeamPreviewFile) Close() error {
	f.once.Do(func() { _ = f.Interrupt(); <-managedPreviewAdmission })
	return f.closeErr
}

// Interrupt stops file I/O, retaining admission until the handler has joined
// every authorization check and calls Close. A timed-out check may still be
// blocked in a filesystem syscall that does not respond to context cancellation.
func (f *ManagedTeamPreviewFile) Interrupt() error {
	f.fileOnce.Do(func() { f.closeErr = f.File.Close() })
	return f.closeErr
}

func (p *ManagedTeamPreview) legacyTeam(ctx context.Context, teamID, userID int) (*models.Team, error) {
	team, err := p.redis.teams.GetTeamByIDContext(ctx, teamID)
	if err != nil || team == nil || team.ID != teamID || team.UserID != userID || team.Status != models.TeamStatusRunning || team.PreviewSecurityProfile != "" || ctx.Err() != nil {
		return nil, errManagedPreview
	}
	return team, nil
}

// OpenLegacy is used only after the receiver verifies the historical signature.
// It bounds active legacy streams and revokes them when the Team is protected.
func (p *ManagedTeamPreview) OpenLegacy(ctx context.Context, teamID, userID int, prefix, relative string) (binding *ManagedTeamPreviewFile, err error) {
	select {
	case managedPreviewAdmission <- struct{}{}:
	default:
		return nil, errManagedPreview
	}
	defer func() {
		if binding == nil {
			<-managedPreviewAdmission
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if teamID < 1 || userID < 1 || (prefix != "" && !validManagedPreviewPath(prefix)) || !validManagedPreviewPath(relative) {
		return nil, errManagedPreview
	}
	if _, err := p.legacyTeam(ctx, teamID, userID); err != nil {
		return nil, errManagedPreview
	}
	file, rootInfo, err := p.openFile(userID, teamID, prefix, relative)
	if err != nil {
		return nil, errManagedPreview
	}
	info, err := file.Stat()
	if err != nil || ctx.Err() != nil {
		file.Close()
		return nil, errManagedPreview
	}
	return &ManagedTeamPreviewFile{File: file, provider: p, claims: managedPreviewClaims{Team: teamID, Prefix: prefix}, relative: relative, info: info, rootInfo: rootInfo, legacy: true, legacyUser: userID}, nil
}

// Walk each directory without symlinks and pin the signed prefix before opening
// assets beneath it. Root.OpenFile prevents escape even during replacement races.
func (p *ManagedTeamPreview) openFile(user, team int, prefix, relative string) (*os.File, os.FileInfo, error) {
	root, err := os.OpenRoot(p.workspaceRoot)
	if err != nil {
		return nil, nil, err
	}
	defer func() { root.Close() }()
	directory := k8s.TeamSharedWorkspaceRelativePath(user, team)
	if prefix != "" {
		directory += "/" + prefix
	}
	for _, segment := range strings.Split(directory, "/") {
		info, err := root.Lstat(segment)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errManagedPreview
		}
		next, err := root.OpenRoot(segment)
		if err != nil {
			return nil, nil, err
		}
		pinned, err := next.Stat(".")
		if err != nil || !os.SameFile(info, pinned) {
			next.Close()
			return nil, nil, errManagedPreview
		}
		root.Close()
		root = next
	}
	rootInfo, err := root.Stat(".")
	if err != nil {
		return nil, nil, err
	}
	// Asset subdirectories need the same symlink rejection as the signed prefix.
	components := strings.Split(relative, "/")
	for _, segment := range components[:len(components)-1] {
		info, err := root.Lstat(segment)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errManagedPreview
		}
		next, err := root.OpenRoot(segment)
		if err != nil {
			return nil, nil, err
		}
		pinned, err := next.Stat(".")
		if err != nil || !os.SameFile(info, pinned) {
			next.Close()
			return nil, nil, errManagedPreview
		}
		root.Close()
		root = next
	}
	file, err := root.OpenFile(components[len(components)-1], os.O_RDONLY|workspaceTextOpenFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || workspaceHasMultipleLinks(info) || info.Size() > managedPreviewMaxSize {
		file.Close()
		return nil, nil, errManagedPreview
	}
	return file, rootInfo, nil
}
