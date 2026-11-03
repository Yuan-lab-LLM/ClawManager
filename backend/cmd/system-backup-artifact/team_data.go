package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"

	appdb "clawreef/internal/db"
	"clawreef/internal/systembackup"
)

func discoverTeamData(ctx context.Context, path, origin, cursor string, limit int, out io.Writer) error {
	return withBootstrap(ctx, path, func(c bootstrap, store systembackup.ImmutableObjectStore, pub ed25519.PublicKey, _ systembackup.AESKeyWrapper) error {
		result, e := systembackup.DiscoverTeamDataArtifacts(ctx, systembackup.TeamDataCatalogStore{Store: store, Origin: origin, PublicKey: pub, KeyVersion: c.VerificationKeyVersion}, cursor, limit)
		if writeErr := json.NewEncoder(out).Encode(result); e == nil {
			e = writeErr
		}
		return e
	})
}

type teamDataRecoveryConfig struct {
	ControlDSNFile    string `json:"control_dsn_file"`
	ServiceConfigFile string `json:"service_config_file"`
	PrepareControl    bool   `json:"prepare_empty_control"`
}

func recoverTeamData(ctx context.Context, operation, bootstrapPath, operationPath, origin, id string, out io.Writer) error {
	var c teamDataRecoveryConfig
	if e := decodeFile(operationPath, &c); e != nil {
		return e
	}
	control, _, e := connectDB(ctx, c.ControlDSNFile)
	if e != nil {
		return e
	}
	defer control.Close()
	if c.PrepareControl {
		if operation != "team-data-recover" && operation != "team-data-verify" {
			return errors.New("validation_failed")
		}
		if e = appdb.PrepareIsolatedRestoreSchema(ctx, control); e != nil {
			return e
		}
	}
	s, e := systembackup.NewTeamDataRecoveryService(ctx, control, c.ServiceConfigFile)
	if e != nil {
		return e
	}
	defer s.Close()
	if operation == "team-data-cleanup" {
		if e = s.CleanupRecovery(ctx, id); e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(map[string]any{"restore_id": id, "cleaned": true})
	}
	return withBootstrap(ctx, bootstrapPath, func(b bootstrap, store systembackup.ImmutableObjectStore, pub ed25519.PublicKey, _ systembackup.AESKeyWrapper) error {
		a, e := systembackup.OpenPublishedArtifact(ctx, store, origin, id, pub, b.VerificationKeyVersion)
		if e != nil {
			return e
		}
		var result systembackup.TeamDataJob
		if operation == "team-data-verify" {
			result, e = s.VerifyRecovery(ctx, origin, id, a.CommitReference.SHA256)
		} else {
			result, e = s.Recover(ctx, origin, id, a.CommitReference.SHA256)
		}
		if writeErr := json.NewEncoder(out).Encode(result); e == nil {
			e = writeErr
		}
		return e
	})
}
