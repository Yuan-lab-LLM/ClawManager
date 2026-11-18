package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// TeamDataCatalog is signed coordination metadata, stored outside artifact
// prefixes. It survives loss of the application's control database. Readers
// never interpret LIST as a snapshot or a proof of absence.
type TeamDataCatalog struct {
	Schema    string                      `json:"schema"`
	Origin    string                      `json:"origin"`
	Revision  uint64                      `json:"revision"`
	Artifacts map[string]TeamDataArtifact `json:"artifacts"`
	Audit     []TeamDataAudit             `json:"audit"`
}
type TeamDataAudit struct {
	At       time.Time `json:"at"`
	Actor    int       `json:"actor"`
	Action   string    `json:"action"`
	BackupID string    `json:"backup_id"`
}
type TeamDataArtifact struct {
	BackupID        string            `json:"backup_id"`
	CommitSHA256    string            `json:"commit_sha256"`
	CapturedAt      time.Time         `json:"captured_at"`
	Bytes           int64             `json:"bytes"`
	Protected       bool              `json:"protected"`
	Health          string            `json:"health"`
	VerifiedAt      *time.Time        `json:"verified_at,omitempty"`
	CheckedAt       *time.Time        `json:"checked_at,omitempty"`
	LastError       string            `json:"last_error,omitempty"`
	Leases          map[string]string `json:"leases"`
	Deleting        bool              `json:"deleting"`
	Deleted         bool              `json:"deleted"`
	DeleteActor     int               `json:"delete_actor,omitempty"`
	DeleteVersions  []ObjectReference `json:"delete_versions,omitempty"`
	DeletedVersions int               `json:"deleted_versions"`
}
type signedTeamDataCatalog struct {
	Payload   TeamDataCatalog   `json:"payload"`
	Signature ArtifactSignature `json:"signature"`
}
type TeamDataCatalogStore struct {
	Store      ImmutableObjectStore
	Origin     string
	PublicKey  ed25519.PublicKey
	Signer     ed25519.PrivateKey
	KeyVersion string
}

func (c TeamDataCatalogStore) objectKey() (string, error) {
	if !refPattern.MatchString(c.Origin) {
		return "", errors.New("validation_failed")
	}
	return c.Store.key("management/" + c.Origin + "/state.json")
}
func (c TeamDataCatalogStore) Load(ctx context.Context) (TeamDataCatalog, string, error) {
	state := TeamDataCatalog{Schema: "team-data-catalog.v1", Origin: c.Origin, Artifacts: map[string]TeamDataArtifact{}, Audit: []TeamDataAudit{}}
	key, e := c.objectKey()
	if e != nil {
		return state, "", e
	}
	info, e := c.Store.Client.StatObject(ctx, c.Store.Bucket, key, minio.StatObjectOptions{})
	if e != nil {
		if code := minio.ToErrorResponse(e).Code; code == "NoSuchKey" || code == "NoSuchObject" {
			return state, "", nil
		}
		return state, "", errors.New("artifact_unavailable")
	}
	if info.Size < 1 || info.Size > 4<<20 || info.VersionID == "" || info.VersionID == "null" {
		return state, "", errors.New("evidence_incomplete")
	}
	ref := ObjectReference{Key: "management/" + c.Origin + "/state.json", VersionID: info.VersionID, Bytes: info.Size}
	// readCurrent authenticates bytes via the signed envelope below. Its ETag
	// is used only as a conditional-update token, never as an integrity hash.
	raw, actual, e := c.Store.readCurrent(ctx, ref.Key)
	if e != nil {
		return state, "", e
	}
	if actual.VersionID != info.VersionID {
		return state, "", errors.New("catalog_changed")
	}
	var signed signedTeamDataCatalog
	if strictDecode(raw, &signed) != nil || verifyObject("clawmanager:team-data-catalog:v1", signed.Payload, signed.Signature, c.PublicKey, c.KeyVersion) != nil {
		return state, "", errors.New("signature_invalid")
	}
	state = signed.Payload
	if state.Schema != "team-data-catalog.v1" || state.Origin != c.Origin || state.Artifacts == nil || len(state.Artifacts) > 10000 {
		return state, "", errors.New("evidence_incomplete")
	}
	return state, info.ETag, nil
}
func (c TeamDataCatalogStore) Update(ctx context.Context, change func(*TeamDataCatalog) error) error {
	if len(c.Signer) != ed25519.PrivateKeySize || !bytes.Equal(c.Signer.Public().(ed25519.PublicKey), c.PublicKey) {
		return errors.New("signing_key_unavailable")
	}
	key, e := c.objectKey()
	if e != nil {
		return e
	}
	for attempt := 0; attempt < 12; attempt++ {
		state, etag, e := c.Load(ctx)
		if e != nil {
			if e.Error() == "catalog_changed" {
				continue
			}
			return e
		}
		if e = change(&state); e != nil {
			return e
		}
		state.Revision++
		if len(state.Audit) > 100 {
			state.Audit = state.Audit[len(state.Audit)-100:]
		}
		sig, e := signObject("clawmanager:team-data-catalog:v1", state, c.Signer, c.KeyVersion)
		if e != nil {
			return e
		}
		raw, e := canonicalJSON(signedTeamDataCatalog{state, sig})
		if e != nil {
			return e
		}
		if len(raw) > 4<<20 {
			return errors.New("capacity_exceeded")
		}
		opts := minio.PutObjectOptions{ContentType: "application/json", DisableMultipart: true}
		if etag == "" {
			opts.SetMatchETagExcept("*")
		} else {
			opts.SetMatchETag(etag)
		}
		_, e = c.Store.Client.PutObject(ctx, c.Store.Bucket, key, bytes.NewReader(raw), int64(len(raw)), opts)
		if e == nil {
			return nil
		}
		if code := minio.ToErrorResponse(e).Code; code != "PreconditionFailed" && code != "ConditionalRequestConflict" {
			return errors.New("external_result_unknown")
		}
	}
	return errors.New("catalog_changed")
}
func (c TeamDataCatalogStore) Import(ctx context.Context, a PublishedArtifact, actor int, verified bool) error {
	if a.Manifest.Payload.Purpose != "team_data" || a.Manifest.Payload.OriginInstallationID != c.Origin {
		return errors.New("artifact_conflict")
	}
	return c.Update(ctx, func(s *TeamDataCatalog) error {
		id := a.Manifest.Payload.BackupID
		record, ok := s.Artifacts[id]
		if ok && (record.Deleting || record.Deleted) {
			return errors.New("backup_deleting")
		}
		if ok && record.CommitSHA256 != a.CommitReference.SHA256 {
			return errors.New("artifact_conflict")
		}
		if !ok {
			record = TeamDataArtifact{BackupID: id, CommitSHA256: a.CommitReference.SHA256, CapturedAt: a.Manifest.Payload.CapturedAt, Protected: true, Health: "unchecked", Leases: map[string]string{}}
			for _, p := range a.Manifest.Payload.Parts {
				record.Bytes += p.CiphertextBytes
			}
		}
		if verified {
			now := time.Now().UTC()
			record.Health = "verified"
			record.VerifiedAt = &now
			record.CheckedAt = &now
			record.LastError = ""
		}
		s.Artifacts[id] = record
		s.Audit = append(s.Audit, TeamDataAudit{time.Now().UTC(), actor, "import", id})
		return nil
	})
}
func (c TeamDataCatalogStore) Acquire(ctx context.Context, id, run, owner string) error {
	return c.Update(ctx, func(s *TeamDataCatalog) error {
		a, ok := s.Artifacts[id]
		if !ok || a.Deleting || a.Deleted {
			return errors.New("backup_deleting")
		}
		if a.Leases == nil {
			a.Leases = map[string]string{}
		}
		if previous, ok := a.Leases[run]; ok && previous != owner {
			return errors.New("ownership_lost")
		}
		a.Leases[run] = owner
		s.Artifacts[id] = a
		return nil
	})
}
func (c TeamDataCatalogStore) Release(ctx context.Context, id, run, owner string) error {
	return c.Update(ctx, func(s *TeamDataCatalog) error {
		a, ok := s.Artifacts[id]
		if !ok {
			return nil // Legacy restore copies predate storage-side references.
		}
		if previous, ok := a.Leases[run]; ok && previous != owner {
			return errors.New("ownership_lost")
		}
		delete(a.Leases, run)
		s.Artifacts[id] = a
		return nil
	})
}
func (s *TeamDataService) catalog() TeamDataCatalogStore {
	var pub ed25519.PublicKey
	if len(s.signer) == ed25519.PrivateKeySize {
		pub = s.signer.Public().(ed25519.PublicKey)
	}
	return TeamDataCatalogStore{s.store, s.cfg.InstallationID, pub, s.signer, s.cfg.KeyVersion}
}
func (s *TeamDataService) Catalog(ctx context.Context) ([]TeamDataArtifact, error) {
	state, _, e := s.catalog().Load(ctx)
	if e != nil {
		return nil, e
	}
	out := []TeamDataArtifact{}
	for _, a := range state.Artifacts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapturedAt.After(out[j].CapturedAt) })
	return out, nil
}

type TeamDataDiscovered struct {
	BackupID     string    `json:"backup_id"`
	State        string    `json:"state"`
	Error        string    `json:"error_code,omitempty"`
	CommitSHA256 string    `json:"commit_sha256,omitempty"`
	CapturedAt   time.Time `json:"captured_at,omitempty"`
	Bytes        int64     `json:"bytes"`
}
type TeamDataDiscovery struct {
	Items                 []TeamDataDiscovered `json:"items"`
	NextCursor            string               `json:"next_cursor"`
	Partial               bool                 `json:"partial"`
	AuthoritativeSnapshot bool                 `json:"authoritative_snapshot"`
}

func teamDataDiscoveryCandidate(prefix, key string) string {
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "/") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(key, prefix), "/")
	if len(id) != 36 || !strings.HasPrefix(id, "sbk_") || !publicIDPattern.MatchString(id) {
		return ""
	}
	return id
}
func DiscoverTeamDataArtifacts(ctx context.Context, c TeamDataCatalogStore, cursor string, limit int) (TeamDataDiscovery, error) {
	out := TeamDataDiscovery{Items: []TeamDataDiscovered{}}
	if limit < 1 || limit > 100 {
		return out, errors.New("validation_failed")
	}
	prefix, e := c.Store.key(c.Origin + "/placeholder")
	if e != nil {
		return out, e
	}
	prefix = strings.TrimSuffix(prefix, "placeholder")
	if cursor != "" && teamDataDiscoveryCandidate(prefix, cursor) == "" {
		return out, errors.New("validation_failed")
	}
	state, _, e := c.Load(ctx)
	if e != nil {
		return out, e
	}
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	count := 0
	for obj := range c.Store.Client.ListObjects(listCtx, c.Store.Bucket, minio.ListObjectsOptions{Prefix: prefix, StartAfter: cursor, Recursive: false, MaxKeys: limit + 1}) {
		if obj.Err != nil {
			out.Partial = true
			return out, errors.New("artifact_unavailable")
		}
		if count == limit {
			out.Partial = true
			return out, nil
		}
		count++
		out.NextCursor = obj.Key
		id := teamDataDiscoveryCandidate(prefix, obj.Key)
		if id == "" {
			return out, errors.New("invalid artifact identity")
		}
		if a := state.Artifacts[id]; a.Deleting || a.Deleted {
			continue
		}
		item := TeamDataDiscovered{BackupID: id, State: "metadata_verified"}
		a, e := OpenPublishedArtifact(ctx, c.Store, c.Origin, id, c.PublicKey, c.KeyVersion)
		if e != nil {
			item.State = "unavailable"
			item.Error = PublicError(e)
		} else if a.Manifest.Payload.Purpose != "team_data" {
			item.State = "unsupported"
			item.Error = "artifact_conflict"
		} else {
			item.CommitSHA256 = a.CommitReference.SHA256
			item.CapturedAt = a.Manifest.Payload.CapturedAt
			for _, p := range a.Manifest.Payload.Parts {
				item.Bytes += p.CiphertextBytes
			}
		}
		out.Items = append(out.Items, item)
	}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	out.NextCursor = ""
	return out, nil
}
func (s *TeamDataService) Discover(ctx context.Context, cursor string, limit int) (TeamDataDiscovery, error) {
	return DiscoverTeamDataArtifacts(ctx, s.catalog(), cursor, limit)
}
func (s *TeamDataService) Import(ctx context.Context, id string, actor int) error {
	if s.cfg.Disabled {
		return errors.New("feature_disabled")
	}
	a, e := OpenPublishedArtifact(ctx, s.store, s.cfg.InstallationID, id, s.signer.Public().(ed25519.PublicKey), s.cfg.KeyVersion)
	if e != nil {
		return e
	}
	return s.catalog().Import(ctx, a, actor, false)
}
