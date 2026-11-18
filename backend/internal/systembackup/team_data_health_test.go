package systembackup

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestTeamDataVerifyMissingCommitUpdatesHealthAndReleasesReference(t *testing.T) {
	s, a, _ := teamDataManagementFixture(t)
	jobs := teamDataJobFixture(t)
	s.db, s.configHash, s.owner = jobs.db, jobs.configHash, jobs.owner
	s.cfg.MaxBytes, s.cfg.MaxItems = 16<<20, 10000
	ctx := context.Background()
	j, e := s.Create(ctx, 1, "missing-commit-verify", "verify", a.Manifest.Payload.BackupID)
	if e != nil {
		t.Fatal(e)
	}
	key, _ := s.store.key(a.CommitReference.Key)
	if e = s.store.Client.RemoveObject(ctx, s.store.Bucket, key, minio.RemoveObjectOptions{VersionID: a.CommitReference.VersionID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.verifyBackup(ctx, j); e == nil {
		t.Fatal("missing commit verified")
	}
	state, _, e := s.catalog().Load(ctx)
	v := state.Artifacts[j.SourceID]
	if e != nil || v.Health != "unavailable" || v.CheckedAt == nil || v.LastError == "" || len(v.Leases) != 0 {
		t.Fatal("missing commit retained verified state or leaked queued reference", e, v.Health, v.CheckedAt, len(v.Leases))
	}
	if v.Deleted {
		t.Fatal("unavailable artifact recorded as deleted")
	}
}

func TestTeamDataVerifyDistinguishesKeyAndSignatureFailures(t *testing.T) {
	for _, kind := range []string{"key_unavailable", "damaged"} {
		t.Run(kind, func(t *testing.T) {
			s, a, _ := teamDataManagementFixture(t)
			jobs := teamDataJobFixture(t)
			s.db, s.configHash, s.owner = jobs.db, jobs.configHash, jobs.owner
			s.cfg.MaxBytes, s.cfg.MaxItems = 16<<20, 10000
			ctx := context.Background()
			j, e := s.Create(ctx, 1, "health-failure-check", "verify", a.Manifest.Payload.BackupID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.db.Exec("UPDATE system_team_data_backup_jobs SET status='running',owner_id=? WHERE public_id=?", s.owner, j.ID); e != nil {
				t.Fatal(e)
			}
			if kind == "key_unavailable" {
				s.wrapper = AESKeyWrapper{Reference: SecretRef{"kubernetes", "unavailable-key", "unknown-version", "encryption"}, Key: make([]byte, 32)}
			} else {
				commit := a.Commit
				commit.Signature.Value[0] ^= 1
				raw, _ := json.Marshal(commit)
				key, _ := s.store.key(a.CommitReference.Key)
				if _, e = s.store.Client.PutObject(ctx, s.store.Bucket, key, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{}); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.verifyBackup(ctx, j); e == nil {
				t.Fatal("invalid artifact verified")
			}
			state, _, e := s.catalog().Load(ctx)
			v := state.Artifacts[j.SourceID]
			if e != nil || v.Health != kind || v.CheckedAt == nil || len(v.Leases) != 0 || v.Deleted {
				t.Fatal("failure category or reference is wrong", e, v.Health, len(v.Leases))
			}
		})
	}
}
