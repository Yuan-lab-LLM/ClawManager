package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	appdb "clawreef/internal/db"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"
)

func teamDataDatabase(id string) string { return "cm_restore_" + id[4:] }
func teamDataBucket(id string) string   { return "cm-restore-" + id[4:] }
func teamDataWriter(id string) string   { return "tdw_" + id[4:28] }
func teamDataReader(id string) string   { return "tdr_" + id[4:28] }

func (s *TeamDataService) restoreDatabase(ctx context.Context, id string) (*sql.DB, error) {
	c := s.adminConfig.Clone()
	c.DBName = teamDataDatabase(id)
	return sql.Open("mysql", c.FormatDSN())
}

func (s *TeamDataService) createRestoreSQL(ctx context.Context, j TeamDataJob, artifact PublishedArtifact) (writer, reader *sql.DB, err error) {
	database := teamDataDatabase(j.ID)
	admin, err := s.prepareRestoreDatabase(ctx, j, func(ctx context.Context, admin *sql.DB) error {
		if e := appdb.PrepareIsolatedRestoreSchema(ctx, admin); e != nil {
			return e
		}
		if e := ConsumePublishedPart(ctx, s.store, artifact, "mysql", s.wrapper, s.cfg.MaxBytes, func(r io.Reader) error { return alignTeamDataRestoreSchema(ctx, admin, r) }); e != nil {
			return e
		}
		// Retain the target marker for older copies and independent inspection.
		_, e := admin.ExecContext(ctx, "INSERT INTO system_team_data_backup_jobs(public_id,operation,source_backup_id,actor_id,idempotency_key,request_hash,config_hash,status) VALUES(?,'restore',?,1,'resource-owner',?,?,'cleaned')", j.ID, j.SourceID, s.configHash, s.configHash)
		return e
	})
	if err != nil {
		return nil, nil, err
	}
	defer admin.Close()
	writePassword, e := RandomID("")
	if e != nil {
		return nil, nil, e
	}
	readPassword, e := RandomID("")
	if e != nil {
		return nil, nil, e
	}
	writeUser, readUser := teamDataWriter(j.ID), teamDataReader(j.ID)
	for _, account := range []struct{ name, password, permissions string }{{writeUser, writePassword, "SELECT,INSERT,UPDATE,DELETE"}, {readUser, readPassword, "SELECT"}} {
		// Both strings are generated lowercase hexadecimal, not request input.
		if _, err = s.admin.ExecContext(ctx, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", account.name, account.password)); err != nil {
			return nil, nil, err
		}
		if _, err = s.admin.ExecContext(ctx, fmt.Sprintf("GRANT %s ON `%s`.* TO '%s'@'%%'", account.permissions, database, account.name)); err != nil {
			return nil, nil, err
		}
	}
	open := func(user, password string) (*sql.DB, error) {
		c := s.adminConfig.Clone()
		c.DBName = database
		c.User = user
		c.Passwd = password
		d, e := sql.Open("mysql", c.FormatDSN())
		if e == nil {
			d.SetMaxOpenConns(2)
			e = d.PingContext(ctx)
		}
		if e != nil && d != nil {
			d.Close()
		}
		return d, e
	}
	writer, err = open(writeUser, writePassword)
	if err != nil {
		return nil, nil, err
	}
	reader, err = open(readUser, readPassword)
	if err != nil {
		writer.Close()
		return nil, nil, err
	}
	return writer, reader, nil
}

func (s *TeamDataService) revokeSQLWriter(ctx context.Context, id string) error {
	user := teamDataWriter(id)
	if _, e := s.admin.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user)); e != nil {
		return e
	}
	rows, e := s.admin.QueryContext(ctx, "SELECT ID FROM information_schema.PROCESSLIST WHERE USER=?", user)
	if e != nil {
		return e
	}
	var ids []uint64
	for rows.Next() {
		var id uint64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = s.admin.ExecContext(ctx, fmt.Sprintf("KILL CONNECTION %d", id)); e != nil {
			return e
		}
	}
	var accounts, sessions int
	if e = s.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User=?", user).Scan(&accounts); e != nil {
		return e
	}
	if e = s.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER=?", user).Scan(&sessions); e != nil {
		return e
	}
	if accounts != 0 || sessions != 0 {
		return errors.New("restore writer still active")
	}
	return nil
}

func (s *TeamDataService) probeObjectReader(ctx context.Context, bucket string) error {
	key := ".team-data-permission-probe"
	if _, e := s.objects.PutObject(ctx, bucket, key, bytes.NewReader([]byte("probe")), 5, minio.PutObjectOptions{}); e != nil {
		return e
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.objects.RemoveObject(clean, bucket, key, minio.RemoveObjectOptions{})
	}()
	obj, e := s.verifierObjects.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if e != nil {
		return e
	}
	var value [5]byte
	_, e = io.ReadFull(obj, value[:])
	obj.Close()
	if e != nil {
		return e
	}
	if string(value[:]) != "probe" {
		return errors.New("read-only object probe failed")
	}
	_, e = s.verifierObjects.PutObject(ctx, bucket, key, bytes.NewReader([]byte("write")), 5, minio.PutObjectOptions{})
	code := minio.ToErrorResponse(e).Code
	if e == nil || code != "AccessDenied" && code != "AllAccessDisabled" {
		return errors.New("object verifier is not read only")
	}
	return nil
}

func (s *TeamDataService) restore(ctx context.Context, j TeamDataJob) (report TeamDataReport, err error) {
	report = TeamDataReport{BackupID: j.SourceID, Purpose: "team_data", Consistency: "best_effort", SourceUnaffected: true}
	if err = s.phase(ctx, j, "verifying_backup"); err != nil {
		return report, err
	}
	artifact, err := OpenPublishedArtifact(ctx, s.store, s.cfg.InstallationID, j.SourceID, s.signer.Public().(ed25519.PublicKey), s.cfg.KeyVersion)
	if err != nil {
		return report, err
	}
	if artifact.Manifest.Payload.Purpose != "team_data" {
		return report, errors.New("artifact_conflict")
	}
	if err = s.catalog().Import(ctx, artifact, 0, false); err != nil {
		return report, err
	}
	if err = s.catalog().Acquire(ctx, j.SourceID, j.ID, "job:"+j.ID); err != nil {
		return report, err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e := s.catalog().Release(c, j.SourceID, j.ID, "job:"+j.ID); err == nil && e != nil {
			err = e
		}
	}()
	if err = s.preflightRestore(ctx, artifact); err != nil {
		return report, err
	}
	if err = s.phase(ctx, j, "provisioning_copy"); err != nil {
		return report, err
	}
	var writer, reader *sql.DB
	var redisAdmin *RedisClient
	var redisIdentity teamDataRedis
	redisAttempted := false
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if writer != nil {
			writer.Close()
		}
		sqlErr := s.revokeSQLWriter(c, j.ID)
		var redisErr error
		if redisAttempted {
			if redisAdmin == nil && redisIdentity.address != "" {
				redisAdmin, redisErr = connectTeamRedis(c, redisIdentity, "admin")
			}
			if redisAdmin == nil && redisErr == nil {
				redisErr = errors.New("restore Redis revocation unconfirmed")
			}
			if redisAdmin != nil {
				defer redisAdmin.Close()
				redisErr = revokeTeamRedisWriter(c, redisAdmin)
			}
		}
		report.WriteCredentialRevoked = sqlErr == nil && redisErr == nil
		if err == nil {
			err = errors.Join(sqlErr, redisErr)
		}
	}()
	writer, reader, err = s.createRestoreSQL(ctx, j, artifact)
	if err != nil {
		return report, err
	}
	defer reader.Close()
	directory := filepath.Join(s.cfg.RestoreRoot, j.ID)
	if err = os.Mkdir(directory, 0700); err != nil {
		return report, err
	}
	bucket := teamDataBucket(j.ID)
	if err = s.createRestoreBucket(ctx, j); err != nil {
		return report, err
	}
	ownerTag, e := tags.NewTags(map[string]string{"clawmanager-team-data-run": j.ID}, false)
	if e != nil {
		return report, e
	}
	if err = s.objects.SetBucketTagging(ctx, bucket, ownerTag); err != nil {
		return report, err
	}
	if err = s.probeObjectReader(ctx, bucket); err != nil {
		return report, err
	}
	redisAttempted = true
	r, e := s.createTeamRedis(ctx, j)
	redisIdentity = r
	if e != nil {
		return report, e
	}
	redisWriter, e := connectTeamRedis(ctx, r, "writer")
	if e != nil {
		return report, e
	}
	defer redisWriter.Close()
	redisAdmin, e = connectTeamRedis(ctx, r, "admin")
	if e != nil {
		return report, e
	}
	redisReader, e := connectTeamRedis(ctx, r, "reader")
	if e != nil {
		return report, e
	}
	defer redisReader.Close()
	if _, e = redisReader.command(ctx, []byte("SET"), []byte("cm:restore-history:"+j.ID+":probe"), []byte("x")); e == nil || !strings.Contains(e.Error(), "NOPERM") {
		return report, errors.New("Redis verifier is not read only")
	}
	mapping := map[string]string{s.cfg.SourceBucket: bucket}
	if err = s.phase(ctx, j, "restoring_data"); err != nil {
		return report, err
	}
	restored, e := RestorePublishedData(ctx, s.store, artifact, s.wrapper, j.ID, DataRestoreTarget{MySQL: writer, Verifier: reader, Database: teamDataDatabase(j.ID), Redis: redisWriter, RedisVerifier: redisReader, Objects: s.objects, BucketMapping: mapping, Directory: directory, Control: Store{DB: s.db, InstallationID: s.cfg.InstallationID}}, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems})
	report.Restore = &restored
	if e != nil {
		return report, e
	}
	if err = s.phase(ctx, j, "revoking_restore_writer"); err != nil {
		return report, err
	}
	writer.Close()
	redisWriter.Close()
	if err = s.revokeSQLWriter(ctx, j.ID); err != nil {
		return report, err
	}
	if err = revokeTeamRedisWriter(ctx, redisAdmin); err != nil {
		return report, err
	}
	if probe, e := connectTeamRedis(ctx, r, "writer"); e == nil {
		probe.Close()
		return report, errors.New("Redis restore writer still authenticates")
	}
	report.WriteCredentialRevoked = true
	if err = s.phase(ctx, j, "verifying_restored_data"); err != nil {
		return report, err
	}
	verified, e := VerifyRestoredData(ctx, s.store, artifact, s.wrapper, RestoreVerificationIntent{RunID: j.ID, NormalizedAt: restored.StartedAt, HistoryParts: restored.HistoryParts}, RestoredDataReader{MySQL: reader, Database: teamDataDatabase(j.ID), Redis: redisReader, Objects: s.verifierObjects, BucketMapping: mapping, Directory: directory}, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems})
	if e != nil {
		return report, e
	}
	report.Verification = &verified
	report.IntegrityVerified = verified.IntegrityVerified
	if verified.IntegrityVerified {
		if e = s.catalog().Import(ctx, artifact, 0, true); e != nil {
			return report, e
		}
	}
	return report, nil
}

func (s *TeamDataService) cleanup(ctx context.Context, id string) error {
	j, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if (j.Operation != "restore" && j.Operation != "verify") || j.ConfigHash != s.configHash {
		return errors.New("state_conflict")
	}
	if j.Status == "cleaned" {
		return nil
	}
	if j.Status != "succeeded" && j.Status != "failed" && j.Status != "canceled" {
		return errors.New("state_conflict")
	}
	if j.Operation == "verify" {
		if e = s.catalog().Release(ctx, j.SourceID, id, "job:"+id); e != nil {
			return e
		}
		_, e = s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='cleaned',phase='cleaned',updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status IN ('succeeded','failed','canceled')", id)
		return e
	}
	if e = s.deleteTeamRedis(ctx, id); e != nil {
		return e
	}
	database := teamDataDatabase(id)
	if !restoreDatabasePattern.MatchString(database) {
		return errors.New("validation_failed")
	}
	var exists int
	if e = s.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", database).Scan(&exists); e != nil {
		return e
	}
	if exists != 0 {
		d, e := s.restoreDatabase(ctx, id)
		if e != nil {
			return e
		}
		var owned int
		e = d.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_team_data_backup_jobs WHERE public_id=? AND config_hash=?", id, s.configHash).Scan(&owned)
		d.Close()
		if e != nil || owned != 1 {
			var server string
			if e = s.admin.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&server); e != nil {
				return e
			}
			proven, e := s.provisionOwned(ctx, j, "database", database, server)
			if e != nil {
				return e
			}
			if !proven {
				return errors.New("restore_ownership_unproven")
			}
		}
		if e = s.revokeSQLWriter(ctx, id); e != nil {
			return e
		}
		if _, e = s.admin.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", teamDataReader(id))); e != nil {
			return e
		}
		if _, e = s.admin.ExecContext(ctx, "DROP DATABASE `"+database+"`"); e != nil {
			return e
		}
	}
	bucket := teamDataBucket(id)
	if ok, e := s.objects.BucketExists(ctx, bucket); e != nil {
		return e
	} else if ok {
		t, e := s.objects.GetBucketTagging(ctx, bucket)
		if e != nil || t.ToMap()["clawmanager-team-data-run"] != id {
			proven, e := s.provisionOwned(ctx, j, "bucket", bucket, "")
			if e != nil {
				return e
			}
			if !proven {
				return errors.New("restore_ownership_unproven")
			}
		}
		for object := range s.objects.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				return object.Err
			}
			if e = s.objects.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); e != nil {
				return e
			}
		}
		if e = s.objects.RemoveBucket(ctx, bucket); e != nil {
			return e
		}
	}
	root, e := os.OpenRoot(s.cfg.RestoreRoot)
	if e != nil {
		return e
	}
	defer root.Close()
	owner, e := root.ReadFile(filepath.Join(id, ".restore-owner"))
	if e == nil {
		if string(owner) != id {
			return errors.New("restore directory ownership not proven")
		}
		if e = root.RemoveAll(id); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	} else {
		d, e := root.Open(id)
		if e == nil {
			entries, readErr := d.Readdirnames(1)
			d.Close()
			if readErr == nil || len(entries) > 0 {
				return errors.New("restore directory ownership not proven")
			}
			if e = root.Remove(id); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if e = s.catalog().Release(ctx, j.SourceID, id, "job:"+id); e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='cleaned',phase='cleaned',updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status IN ('succeeded','failed','canceled')", id)
	return e
}
