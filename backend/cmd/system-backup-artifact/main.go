// system-backup-artifact captures, verifies or restores data without starting
// business services. Verify and restore use disaster-side dependencies only.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"clawreef/internal/systembackup"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type bootstrap struct {
	Endpoint               string                 `json:"endpoint"`
	Bucket                 string                 `json:"bucket"`
	Root                   string                 `json:"root"`
	Secure                 bool                   `json:"secure"`
	CredentialsFile        string                 `json:"credentials_file"`
	CAFile                 string                 `json:"ca_file,omitempty"`
	SigningKeyFile         string                 `json:"signing_key_file,omitempty"`
	VerificationKeyFile    string                 `json:"verification_key_file"`
	VerificationKeyVersion string                 `json:"verification_key_version"`
	EncryptionKeyFile      string                 `json:"encryption_key_file"`
	EncryptionReference    systembackup.SecretRef `json:"encryption_reference"`
}

func readBounded(path string, max int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("bootstrap file unavailable")
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil || !stat.Mode().IsRegular() || stat.Size() > max {
		return nil, errors.New("invalid bootstrap file")
	}
	raw, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(raw)) > max {
		return nil, errors.New("invalid bootstrap file")
	}
	return raw, nil
}
func decodeFile(path string, out any) error {
	raw, e := readBounded(path, 16384)
	if e != nil {
		return e
	}
	defer clear(raw)
	if e = systembackup.DecodeStrictJSON(raw, out); e != nil {
		return errors.New("invalid bootstrap configuration")
	}
	return nil
}

type uploadBootstrap struct {
	Endpoint               string `json:"endpoint"`
	Bucket                 string `json:"bucket"`
	Root                   string `json:"root"`
	Secure                 bool   `json:"secure"`
	CredentialsFile        string `json:"credentials_file"`
	CAFile                 string `json:"ca_file,omitempty"`
	VerificationKeyFile    string `json:"verification_key_file"`
	VerificationKeyVersion string `json:"verification_key_version"`
}

func connectArtifactStore(ctx context.Context, cfg uploadBootstrap) (systembackup.ImmutableObjectStore, func(), error) {
	var zero systembackup.ImmutableObjectStore
	if cfg.Endpoint == "" || strings.Contains(cfg.Endpoint, "://") || cfg.Bucket == "" {
		return zero, nil, errors.New("validation_failed")
	}
	if !cfg.Secure {
		host, _, e := net.SplitHostPort(cfg.Endpoint)
		if e != nil {
			return zero, nil, errors.New("validation_failed")
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return zero, nil, errors.New("TLS is required outside a local verification tunnel")
		}
	}
	var cred struct {
		AccessKey    string `json:"access_key"`
		SecretKey    string `json:"secret_key"`
		SessionToken string `json:"session_token,omitempty"`
	}
	if e := decodeFile(cfg.CredentialsFile, &cred); e != nil {
		return zero, nil, errors.New("artifact_unavailable")
	}
	if cred.AccessKey == "" || cred.SecretKey == "" {
		return zero, nil, errors.New("artifact_unavailable")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		raw, e := readBounded(cfg.CAFile, 1<<20)
		if e != nil {
			return zero, nil, e
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return zero, nil, errors.New("invalid trust roots")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	client, e := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cred.AccessKey, cred.SecretKey, cred.SessionToken), Secure: cfg.Secure, Transport: transport})
	if e != nil {
		return zero, nil, errors.New("artifact_unavailable")
	}

	return systembackup.ImmutableObjectStore{Client: client, Bucket: cfg.Bucket, Root: cfg.Root}, transport.CloseIdleConnections, nil
}

func withBootstrap(ctx context.Context, path string, action func(bootstrap, systembackup.ImmutableObjectStore, ed25519.PublicKey, systembackup.AESKeyWrapper) error) error {
	var cfg bootstrap
	if e := decodeFile(path, &cfg); e != nil {
		return e
	}
	store, closeStore, e := connectArtifactStore(ctx, uploadBootstrap{Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, Root: cfg.Root, Secure: cfg.Secure, CredentialsFile: cfg.CredentialsFile, CAFile: cfg.CAFile})
	if e != nil {
		return e
	}
	defer closeStore()
	pub, e := readBounded(cfg.VerificationKeyFile, ed25519.PublicKeySize)
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("signing_key_unavailable")
	}
	key, e := readBounded(cfg.EncryptionKeyFile, 32)
	if e != nil || len(key) != 32 {
		return errors.New("encryption_key_unavailable")
	}
	defer clear(key)
	wrapper := systembackup.AESKeyWrapper{Reference: cfg.EncryptionReference, Key: key}
	return action(cfg, store, ed25519.PublicKey(pub), wrapper)
}
func verify(ctx context.Context, path, origin, id string, max int64, out io.Writer) error {
	return withBootstrap(ctx, path, func(cfg bootstrap, store systembackup.ImmutableObjectStore, pub ed25519.PublicKey, wrapper systembackup.AESKeyWrapper) error {
		artifact, e := systembackup.OpenPublishedArtifact(ctx, store, origin, id, pub, cfg.VerificationKeyVersion)
		if e != nil {
			return e
		}
		if e = systembackup.VerifyPublishedArtifact(ctx, store, artifact, wrapper, max); e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(map[string]any{"backup_id": id, "origin_installation_id": origin, "purpose": artifact.Manifest.Payload.Purpose, "consistency": artifact.Manifest.Payload.Consistency, "integrity_verified": true, "acceptance_eligible": false, "parts_verified": len(artifact.Manifest.Payload.Parts)})
	})
}

func executionDeadline(now time.Time, timeout time.Duration, absolute string) (time.Time, error) {
	deadline := now.Add(timeout)
	if absolute == "" {
		return deadline, nil
	}
	frozen, e := time.Parse(time.RFC3339Nano, absolute)
	if e != nil {
		return time.Time{}, errors.New("validation_failed")
	}
	if !frozen.After(now) {
		return time.Time{}, errors.New("deadline_exceeded")
	}
	if frozen.Before(deadline) {
		deadline = frozen
	}
	return deadline, nil
}

func main() {
	reportFile := flag.String("report-file", "", "bounded operation result file")
	readyFile := flag.String("ready-file", "", "capture-phase readiness file")
	captureRequest := flag.String("capture-request", "", "frozen non-secret capture request (base64url)")
	uploadRequest := flag.String("upload-request", "", "frozen non-secret upload request (base64url)")
	dataRequest := flag.String("data-request", "", "frozen isolated data execution identity (base64url)")
	cursor := flag.String("cursor", "", "bounded Team data discovery cursor")
	pageSize := flag.Int("page-size", 20, "Team data discovery page size (1..100)")
	operation := flag.String("operation", "verify", "verify, capture, restore or verify-restored (data-only)")
	operationConfig := flag.String("operation-config", "", "source or isolated target configuration file")
	maxItems := flag.Int("max-items", 1000000, "maximum records/files per data source")
	config := flag.String("bootstrap", "", "disaster-side bootstrap JSON file")
	origin := flag.String("origin", "", "origin installation identity")
	id := flag.String("backup", "", "public backup identity")
	max := flag.Int64("max-bytes", 16<<30, "maximum verified ciphertext bytes")
	deadline := flag.Duration("timeout", 30*time.Minute, "verification deadline")
	absoluteDeadline := flag.String("deadline-at", "", "frozen absolute task deadline (RFC3339)")
	flag.Parse()
	if *operation == "readiness" {
		if *readyFile != "/run/backup-status/ready" {
			os.Exit(2)
		}
		info, err := os.Lstat(*readyFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() != 6 {
			os.Exit(1)
		}
		raw, err := os.ReadFile(*readyFile)
		if err != nil || string(raw) != "ready\n" {
			os.Exit(1)
		}
		return
	}
	if *config == "" || *origin == "" || (*id == "" && *operation != "team-data-discover") || *max < 1<<20 || *max > 900<<30 || *deadline < time.Second || *deadline > 12*time.Hour {
		fmt.Fprintln(os.Stderr, "invalid verification arguments")
		os.Exit(2)
	}
	end, deadlineErr := executionDeadline(time.Now(), *deadline, *absoluteDeadline)
	if deadlineErr != nil {
		fmt.Fprintln(os.Stderr, "backup operation failed:", systembackup.PublicError(deadlineErr))
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithDeadline(ctx, end)
	defer cancel()
	var e error
	var report bytes.Buffer
	var output io.Writer = os.Stdout
	if *reportFile != "" {
		if *operation != "verify" && *operation != "verify-snapshot" && *operation != "capture-phase" && *operation != "upload-parts" && !((*operation == "restore" || *operation == "verify-restored") && *dataRequest != "") {
			fmt.Fprintln(os.Stderr, "report file is unsupported for this operation")
			os.Exit(2)
		}
		output = io.MultiWriter(os.Stdout, &report)
	}
	switch *operation {
	case "team-data-discover":
		e = discoverTeamData(ctx, *config, *origin, *cursor, *pageSize, output)
	case "team-data-recover", "team-data-cleanup", "team-data-verify":
		e = recoverTeamData(ctx, *operation, *config, *operationConfig, *origin, *id, output)
	case "verify":
		e = verify(ctx, *config, *origin, *id, *max, output)
	case "verify-snapshot":
		e = verifySnapshot(ctx, *config, *origin, *id, systembackup.CaptureBudget{MaxBytes: *max, MaxItems: *maxItems}, output)
	case "upload-parts":
		var request systembackup.PartUploadRequest
		raw, decodeErr := base64.RawURLEncoding.Strict().DecodeString(*uploadRequest)
		if decodeErr != nil || len(raw) > 8192 || systembackup.DecodeStrictJSON(raw, &request) != nil {
			e = errors.New("validation_failed")
		} else {
			e = runUploadParts(ctx, *config, *operationConfig, *origin, *id, request, *max, output)
		}
	case "capture-phase":
		var request systembackup.CapturePhaseRequest
		raw, decodeErr := base64.RawURLEncoding.Strict().DecodeString(*captureRequest)
		if *operationConfig == "" || *maxItems < 1 || *maxItems > 100000000 {
			e = errors.New("validation_failed")
		} else if decodeErr != nil || len(raw) > 8192 || systembackup.DecodeStrictJSON(raw, &request) != nil {
			e = errors.New("validation_failed")
		} else {
			e = runCapturePhase(ctx, *config, *operationConfig, *origin, *id, *readyFile, request, systembackup.CaptureBudget{MaxBytes: *max, MaxItems: *maxItems}, output)
		}
	case "capture", "restore", "verify-restored":
		if *operationConfig == "" || *maxItems < 1 || *maxItems > 100000000 {
			e = errors.New("validation_failed")
		} else {
			var binding *systembackup.DataOperationBinding
			if *dataRequest != "" {
				binding = &systembackup.DataOperationBinding{}
				raw, err := base64.RawURLEncoding.Strict().DecodeString(*dataRequest)
				if err != nil || len(raw) > 4096 || systembackup.DecodeStrictJSON(raw, binding) != nil || binding.Validate() != nil || binding.Operation != *operation {
					e = errors.New("validation_failed")
				}
			}
			if e == nil {
				e = runBoundDataOperation(ctx, *operation, *config, *operationConfig, *origin, *id, systembackup.CaptureBudget{MaxBytes: *max, MaxItems: *maxItems}, binding, output)
			}
		}
	default:
		e = errors.New("validation_failed")
	}
	if e == nil && *reportFile != "" {
		if report.Len() > 4096 {
			e = errors.New("evidence_incomplete")
		} else {
			e = os.WriteFile(*reportFile, report.Bytes(), 0600)
		}
	}
	if e != nil {
		if *reportFile != "" {
			_ = writeOperationFailure(*reportFile, *operation, *origin, *id, e)
		}
		fmt.Fprintln(os.Stderr, "backup operation failed:", systembackup.PublicError(e))
		os.Exit(1)
	}
}
