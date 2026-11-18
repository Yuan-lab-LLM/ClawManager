package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/systembackup"
)

type phaseBootstrap struct {
	EncryptionKeyFile   string                 `json:"encryption_key_file"`
	EncryptionReference systembackup.SecretRef `json:"encryption_reference"`
}
type phaseCaptureConfig struct {
	Source             captureConfig `json:"source"`
	ControlDSNFile     string        `json:"gate_read_dsn_file"`
	AbortMarginSeconds int           `json:"abort_margin_seconds"`
}

// The capture bootstrap cannot decode artifact signing or final-write
// credentials. Ordinary and Secret collectors receive disjoint source files.
func runCapturePhase(ctx context.Context, bootstrapPath, operationPath, origin, id, readyFile string, request systembackup.CapturePhaseRequest, budget systembackup.CaptureBudget, out io.Writer) (resultErr error) {
	stage := "bootstrap_config"
	defer func() {
		if resultErr != nil {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"event": "capture_error", "step": stage, "code": systembackup.PublicError(resultErr)})
			resultErr = &operationStageError{stage: stage, cause: resultErr}
		}
	}()
	var bootstrap phaseBootstrap
	var c phaseCaptureConfig
	if err := decodeFile(bootstrapPath, &bootstrap); err != nil {
		return err
	}
	stage = "operation_config"
	if err := decodeFile(operationPath, &c); err != nil {
		return err
	}
	stage = "validate_request"
	if request.Validate() != nil || request.Origin != origin || request.BackupID != id || c.AbortMarginSeconds < 1 || c.AbortMarginSeconds > 60 || readyFile != "/run/backup-status/ready" || c.Source.StageDirectory != "/run/backup-stage/data" {
		return errors.New("validation_failed")
	}
	stage = "encryption_key"
	key, err := readBounded(bootstrap.EncryptionKeyFile, 32)
	if err != nil || len(key) != 32 {
		return errors.New("encryption_key_unavailable")
	}
	defer clear(key)
	wrapper := systembackup.AESKeyWrapper{Reference: bootstrap.EncryptionReference, Key: key}
	stage = "gate_connection"
	gate, err := systembackup.OpenControlDatabase(ctx, c.ControlDSNFile)
	if err != nil {
		return err
	}
	defer gate.Close()
	stage = "source_connections"
	source, closeSource, err := connectPhaseSources(ctx, c.Source, request.Phase)
	if err != nil {
		return err
	}
	defer closeSource()
	if request.Phase == "capture" {
		stage = "source_gate_identity"
		if err = backupgate.RequireSameDatabase(ctx, source.MySQL, gate); err != nil {
			return errors.New("writer_registry_drift")
		}
	}
	stage = "validate_sources"
	if err = source.ValidateForPhase(request.Phase); err != nil {
		return err
	}
	stage = "staging_prepare"
	if err = os.Mkdir(c.Source.StageDirectory, 0750); err != nil {
		return errors.New("fresh encrypted staging required")
	}
	// Readiness is published only after credential files and connections have
	// been prepared. No business rows, Secret contents or file data are read yet.
	stage = "readiness_write"
	if err = os.WriteFile(readyFile, []byte("ready\n"), 0600); err != nil {
		return errors.New("evidence_incomplete")
	}
	defer os.Remove(readyFile)
	guard := systembackup.SQLCaptureGuard{Gate: systembackup.Gate{DB: gate}, Ownership: request.Gate, AbortMargin: time.Duration(c.AbortMarginSeconds) * time.Second}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	stage = "gate_wait"
	for {
		var phase, owner string
		var generation, token uint64
		if err = gate.QueryRowContext(ctx, "SELECT phase,COALESCE(owner,''),generation,fencing_token FROM system_backup_gate WHERE scope=?", request.Gate.Scope).Scan(&phase, &owner, &generation, &token); err != nil {
			return err
		}
		if phase == "held" && owner == request.Gate.Owner && generation == request.Gate.Generation && token == request.Gate.Token {
			break
		}
		before := phase == "idle" && generation+1 == request.Gate.Generation && token+1 == request.Gate.Token
		acquiring := phase == "acquiring" && owner == request.Gate.Owner && generation == request.Gate.Generation && token+1 == request.Gate.Token
		if !before && !acquiring {
			return systembackup.ErrFence
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	stage = "capture"
	candidate, err := systembackup.CapturePhase(ctx, c.Source.StageDirectory, request, source, budget, wrapper, guard)
	if err != nil {
		return err
	}
	stage = "report"
	return json.NewEncoder(out).Encode(candidate)
}

func connectPhaseSources(ctx context.Context, c captureConfig, phase string) (systembackup.CaptureSources, func(), error) {
	s := systembackup.CaptureSources{Namespace: c.Namespace, NamespaceUID: c.NamespaceUID, Deployments: c.Deployments, Secrets: c.Secrets, ObjectBuckets: c.Buckets}
	var cleanup []func()
	closeAll := func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
	fail := func(err error) (systembackup.CaptureSources, func(), error) {
		closeAll()
		return systembackup.CaptureSources{}, nil, err
	}
	if phase == "secret_capture" {
		if c.MySQLDSNFile != "" || c.Redis != (redisConfig{}) || c.Objects != (objectConfig{}) || len(c.Buckets) > 0 || len(c.Workspace) > 0 || c.DeploymentClient != (kubeConfig{}) || len(c.Deployments) > 0 {
			return fail(errors.New("validation_failed"))
		}
		client, err := connectKubernetes(c.SecretClient)
		if err != nil {
			return fail(err)
		}
		s.SecretClient = client
		return s, closeAll, nil
	}
	if phase != "capture" || c.SecretClient != (kubeConfig{}) || len(c.Secrets) != 0 {
		return fail(errors.New("validation_failed"))
	}
	var err error
	s.MySQL, _, err = connectDB(ctx, c.MySQLDSNFile)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, func() { s.MySQL.Close() })
	s.Redis, err = connectRedis(ctx, c.Redis)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, func() { s.Redis.Close() })
	var closeObjects func()
	s.Objects, closeObjects, err = connectObjects(c.Objects)
	if err != nil {
		return fail(err)
	}
	cleanup = append(cleanup, closeObjects)
	s.DeploymentClient, err = connectKubernetes(c.DeploymentClient)
	if err != nil {
		return fail(err)
	}
	for _, root := range c.Workspace {
		s.Workspace = append(s.Workspace, systembackup.FileRoot{ID: root.ID, Path: root.Path})
	}
	return s, closeAll, nil
}
