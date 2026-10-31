package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"

	"clawreef/internal/systembackup"
)

type uploadPartsConfig struct {
	Manifest systembackup.SignedManifest `json:"manifest"`
}

func runUploadParts(ctx context.Context, bootstrapPath, operationPath, origin, id string, request systembackup.PartUploadRequest, maxBytes int64, out io.Writer) error {
	var cfg uploadBootstrap
	var operation uploadPartsConfig
	if err := decodeFile(bootstrapPath, &cfg); err != nil {
		return err
	}
	if err := decodeFile(operationPath, &operation); err != nil {
		return err
	}
	if request.Validate() != nil || request.Origin != origin || request.BackupID != id || operation.Manifest.Payload.BackupID != id || operation.Manifest.Payload.OriginInstallationID != origin {
		return errors.New("validation_failed")
	}
	pub, err := readBounded(cfg.VerificationKeyFile, ed25519.PublicKeySize)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("signing_key_unavailable")
	}
	store, closeStore, err := connectArtifactStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeStore()
	index, err := systembackup.UploadCapturedParts(ctx, store, systembackup.CaptureStageDirectories{Ordinary: "/run/backup-ordinary/data", Secrets: "/run/backup-secrets/data"}, operation.Manifest, ed25519.PublicKey(pub), cfg.VerificationKeyVersion, maxBytes)
	if err != nil {
		return err
	}
	result, err := systembackup.SealUploadedIndex(ctx, store, request, index)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
