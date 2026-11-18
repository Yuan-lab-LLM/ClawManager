package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"

	"clawreef/internal/systembackup"
)

func verifySnapshot(ctx context.Context, path, origin, id string, b systembackup.CaptureBudget, out io.Writer) error {
	return withBootstrap(ctx, path, func(cfg bootstrap, store systembackup.ImmutableObjectStore, pub ed25519.PublicKey, wrapper systembackup.AESKeyWrapper) error {
		artifact, err := systembackup.OpenPublishedArtifact(ctx, store, origin, id, pub, cfg.VerificationKeyVersion)
		if err != nil {
			return err
		}
		if err = systembackup.ValidatePublishedSnapshot(ctx, store, artifact, wrapper, b); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(systembackup.IntegrityReport{BackupID: id, Origin: origin, Purpose: artifact.Manifest.Payload.Purpose, Consistency: artifact.Manifest.Payload.Consistency, IntegrityVerified: true, SnapshotValidated: true, PartsVerified: len(artifact.Manifest.Payload.Parts), ManifestSHA256: artifact.Commit.Payload.Manifest.SHA256, IndexSHA256: artifact.Commit.Payload.Index.SHA256, CommitSHA256: artifact.CommitReference.SHA256})
	})
}
