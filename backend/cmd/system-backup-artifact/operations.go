package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"clawreef/internal/systembackup"
	"github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type objectConfig struct {
	Endpoint        string `json:"endpoint"`
	Secure          bool   `json:"secure"`
	CredentialsFile string `json:"credentials_file"`
	CAFile          string `json:"ca_file,omitempty"`
}
type redisConfig struct {
	Address         string `json:"address"`
	CredentialsFile string `json:"credentials_file"`
	Database        int    `json:"database"`
	Secure          bool   `json:"secure"`
	CAFile          string `json:"ca_file,omitempty"`
	ServerName      string `json:"server_name,omitempty"`
}
type kubeConfig struct {
	Server     string `json:"server"`
	ServerName string `json:"server_name"`
	CAFile     string `json:"ca_file"`
	TokenFile  string `json:"token_file"`
}
type captureConfig struct {
	MySQLDSNFile string       `json:"mysql_dsn_file"`
	Redis        redisConfig  `json:"redis"`
	Objects      objectConfig `json:"objects"`
	Buckets      []string     `json:"buckets"`
	Workspace    []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"workspace"`
	DeploymentClient kubeConfig                         `json:"deployment_client"`
	SecretClient     kubeConfig                         `json:"secret_client"`
	Namespace        string                             `json:"namespace"`
	NamespaceUID     string                             `json:"namespace_uid"`
	Deployments      []systembackup.KubernetesSelection `json:"deployments"`
	Secrets          []systembackup.KubernetesSelection `json:"secrets"`
	StageDirectory   string                             `json:"stage_directory"`
}
type restoreConfig struct {
	RunID                 string            `json:"run_id"`
	MySQLDSNFile          string            `json:"mysql_dsn_file"`
	VerifierDSNFile       string            `json:"verifier_dsn_file"`
	ControlDSNFile        string            `json:"control_dsn_file"`
	ControlInstallationID string            `json:"control_installation_id"`
	Redis                 redisConfig       `json:"redis"`
	Objects               objectConfig      `json:"objects"`
	BucketMapping         map[string]string `json:"bucket_mapping"`
	Directory             string            `json:"directory"`
}

type restoredVerificationConfig struct {
	Intent          systembackup.RestoreVerificationIntent `json:"intent"`
	VerifierDSNFile string                                 `json:"verifier_dsn_file"`
	Redis           redisConfig                            `json:"redis"`
	Objects         objectConfig                           `json:"objects"`
	BucketMapping   map[string]string                      `json:"bucket_mapping"`
	Directory       string                                 `json:"directory"`
}

func connectDB(ctx context.Context, path string) (*sql.DB, string, error) {
	raw, e := readBounded(path, 16384)
	if e != nil {
		return nil, "", e
	}
	defer clear(raw)
	cfg, e := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if e != nil || cfg.DBName == "" || cfg.MultiStatements || cfg.AllowAllFiles {
		return nil, "", errors.New("invalid database bootstrap")
	}
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return nil, "", errors.New("database unavailable")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, "", errors.New("database unavailable")
	}
	return db, cfg.DBName, nil
}
func connectionTLS(secure bool, address, caFile, serverName string) (*tls.Config, error) {
	if !secure {
		host, _, e := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if e != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("TLS required outside local tunnel")
		}
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if caFile != "" {
		raw, e := readBounded(caFile, 1<<20)
		if e != nil {
			return nil, e
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, errors.New("invalid trust roots")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}
func connectObjects(c objectConfig) (*minio.Client, func(), error) {
	tlsConfig, e := connectionTLS(c.Secure, c.Endpoint, c.CAFile, "")
	if e != nil {
		return nil, nil, e
	}
	var cred struct {
		AccessKey    string `json:"access_key"`
		SecretKey    string `json:"secret_key"`
		SessionToken string `json:"session_token,omitempty"`
	}
	if e = decodeFile(c.CredentialsFile, &cred); e != nil || cred.AccessKey == "" || cred.SecretKey == "" {
		return nil, nil, errors.New("object credentials unavailable")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsConfig
	cx, e := minio.New(c.Endpoint, &minio.Options{Secure: c.Secure, Creds: credentials.NewStaticV4(cred.AccessKey, cred.SecretKey, cred.SessionToken), Transport: tr})
	if e != nil {
		tr.CloseIdleConnections()
		return nil, nil, errors.New("invalid object endpoint")
	}
	return cx, tr.CloseIdleConnections, nil
}
func connectRedis(ctx context.Context, c redisConfig) (*systembackup.RedisClient, error) {
	tlsConfig, e := connectionTLS(c.Secure, c.Address, c.CAFile, c.ServerName)
	if e != nil {
		return nil, e
	}
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if e = decodeFile(c.CredentialsFile, &cred); e != nil {
		return nil, e
	}
	return systembackup.ConnectRedis(ctx, systembackup.RedisOptions{Address: c.Address, Username: cred.Username, Password: cred.Password, Database: c.Database, TLS: tlsConfig, MaxReplyBytes: 64 << 20})
}
func connectKubernetes(c kubeConfig) (dynamic.Interface, error) {
	if !strings.HasPrefix(c.Server, "https://") {
		return nil, errors.New("Kubernetes TLS required")
	}
	ca, e := readBounded(c.CAFile, 1<<20)
	if e != nil {
		return nil, e
	}
	token, e := readBounded(c.TokenFile, 16384)
	if e != nil {
		return nil, e
	}
	defer clear(token)
	if len(bytes.TrimSpace(token)) == 0 {
		return nil, errors.New("Kubernetes credential unavailable")
	}
	return dynamic.NewForConfig(&rest.Config{Host: c.Server, BearerToken: strings.TrimSpace(string(token)), Timeout: 30 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: ca, ServerName: c.ServerName}})
}

func runDataOperation(ctx context.Context, operation, bootstrapPath, operationPath, origin, id string, budget systembackup.CaptureBudget, out io.Writer) error {
	return runBoundDataOperation(ctx, operation, bootstrapPath, operationPath, origin, id, budget, nil, out)
}

func runBoundDataOperation(ctx context.Context, operation, bootstrapPath, operationPath, origin, id string, budget systembackup.CaptureBudget, binding *systembackup.DataOperationBinding, out io.Writer) error {
	if binding != nil && (binding.Validate() != nil || binding.Operation != operation || binding.Origin != origin || binding.BackupID != id) {
		return errors.New("validation_failed")
	}
	return withBootstrap(ctx, bootstrapPath, func(cfg bootstrap, store systembackup.ImmutableObjectStore, pub ed25519.PublicKey, wrapper systembackup.AESKeyWrapper) error {
		switch operation {
		case "verify-restored":
			var c restoredVerificationConfig
			if e := decodeFile(operationPath, &c); e != nil {
				return e
			}
			artifact, e := systembackup.OpenPublishedArtifact(ctx, store, origin, id, pub, cfg.VerificationKeyVersion)
			if e != nil {
				return e
			}
			if binding != nil {
				intentHash, err := systembackup.RequestHash(c.Intent)
				if err != nil || binding.IntentSHA256 != intentHash || binding.RunID != c.Intent.RunID || binding.CommitSHA256 != artifact.CommitReference.SHA256 {
					return errors.New("artifact_conflict")
				}
			}
			target := systembackup.RestoredDataReader{Directory: c.Directory, BucketMapping: c.BucketMapping}
			target.MySQL, target.Database, e = connectDB(ctx, c.VerifierDSNFile)
			if e != nil {
				return e
			}
			defer target.MySQL.Close()
			target.Redis, e = connectRedis(ctx, c.Redis)
			if e != nil {
				return e
			}
			defer target.Redis.Close()
			var closeObjects func()
			target.Objects, closeObjects, e = connectObjects(c.Objects)
			if e != nil {
				return e
			}
			defer closeObjects()
			report, e := systembackup.VerifyRestoredData(ctx, store, artifact, wrapper, c.Intent, target, budget)
			if binding != nil {
				if e != nil {
					return e
				}
				receipt, err := systembackup.RestoredVerificationReceipt(*binding, c.Intent, report)
				if err != nil {
					return err
				}
				return json.NewEncoder(out).Encode(receipt)
			}
			if writeErr := json.NewEncoder(out).Encode(report); e == nil {
				e = writeErr
			}
			return e
		case "capture":
			var c captureConfig
			if e := decodeFile(operationPath, &c); e != nil {
				return e
			}
			signer, e := readBounded(cfg.SigningKeyFile, ed25519.PrivateKeySize)
			if e != nil || len(signer) != ed25519.PrivateKeySize {
				return errors.New("signing_key_unavailable")
			}
			defer clear(signer)
			if !bytes.Equal(ed25519.PrivateKey(signer).Public().(ed25519.PublicKey), pub) {
				return errors.New("signing_key_unavailable")
			}
			source := systembackup.CaptureSources{Namespace: c.Namespace, NamespaceUID: c.NamespaceUID, ObjectBuckets: c.Buckets, Deployments: c.Deployments, Secrets: c.Secrets}
			source.MySQL, _, e = connectDB(ctx, c.MySQLDSNFile)
			if e != nil {
				return e
			}
			defer source.MySQL.Close()
			source.Redis, e = connectRedis(ctx, c.Redis)
			if e != nil {
				return e
			}
			defer source.Redis.Close()
			var closeObjects func()
			source.Objects, closeObjects, e = connectObjects(c.Objects)
			if e != nil {
				return e
			}
			defer closeObjects()
			source.DeploymentClient, e = connectKubernetes(c.DeploymentClient)
			if e != nil {
				return e
			}
			source.SecretClient, e = connectKubernetes(c.SecretClient)
			if e != nil {
				return e
			}
			for _, r := range c.Workspace {
				source.Workspace = append(source.Workspace, systembackup.FileRoot{ID: r.ID, Path: r.Path})
			}
			manifest, e := systembackup.CaptureFunctionalBundle(ctx, c.StageDirectory, origin, id, source, budget, wrapper, ed25519.PrivateKey(signer), cfg.VerificationKeyVersion)
			if e != nil {
				return e
			}
			artifact, e := systembackup.PublishFunctionalArtifact(ctx, store, c.StageDirectory, manifest, ed25519.PrivateKey(signer), cfg.VerificationKeyVersion, budget.MaxBytes)
			if e != nil {
				return e
			}
			// Report the immutable publication reference. Staging cleanup is explicit
			// and only touches the six files created in this invocation's stage.
			root, e := os.OpenRoot(c.StageDirectory)
			if e != nil {
				return errors.New("staging_cleanup_failed")
			}
			defer root.Close()
			for _, name := range systembackup.RequiredParts {
				if e = root.Remove(name + ".enc"); e != nil {
					return errors.New("staging_cleanup_failed")
				}
			}
			return json.NewEncoder(out).Encode(map[string]any{"backup_id": id, "origin_installation_id": origin, "status": "published", "purpose": manifest.Payload.Purpose, "consistency": manifest.Payload.Consistency, "acceptance_eligible": false, "commit": artifact.CommitReference})
		case "restore":
			var c restoreConfig
			if e := decodeFile(operationPath, &c); e != nil {
				return e
			}
			artifact, e := systembackup.OpenPublishedArtifact(ctx, store, origin, id, pub, cfg.VerificationKeyVersion)
			if e != nil {
				return e
			}
			if binding != nil && (binding.RunID != c.RunID || binding.CommitSHA256 != artifact.CommitReference.SHA256) {
				return errors.New("artifact_conflict")
			}
			target := systembackup.DataRestoreTarget{Directory: c.Directory, BucketMapping: c.BucketMapping}
			target.MySQL, target.Database, e = connectDB(ctx, c.MySQLDSNFile)
			if e != nil {
				return e
			}
			defer target.MySQL.Close()
			target.Verifier, _, e = connectDB(ctx, c.VerifierDSNFile)
			if e != nil {
				return e
			}
			defer target.Verifier.Close()
			target.Control.DB, _, e = connectDB(ctx, c.ControlDSNFile)
			if e != nil {
				return e
			}
			defer target.Control.DB.Close()
			target.Control.InstallationID = c.ControlInstallationID
			target.Redis, e = connectRedis(ctx, c.Redis)
			if e != nil {
				return e
			}
			defer target.Redis.Close()
			var closeObjects func()
			target.Objects, closeObjects, e = connectObjects(c.Objects)
			if e != nil {
				return e
			}
			defer closeObjects()
			report, e := systembackup.RestorePublishedData(ctx, store, artifact, wrapper, c.RunID, target, budget)
			if binding != nil {
				if e != nil {
					return e
				}
				receipt, err := systembackup.RestoreReceipt(*binding, report)
				if err != nil {
					return err
				}
				return json.NewEncoder(out).Encode(receipt)
			}
			if writeErr := json.NewEncoder(out).Encode(report); e == nil {
				e = writeErr
			}
			return e
		default:
			return errors.New("validation_failed")
		}
	})
}
