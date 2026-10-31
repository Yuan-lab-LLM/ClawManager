package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"k8s.io/client-go/kubernetes"
)

type ArtifactClientBootstrap struct {
	Reference       SecretRef `json:"reference"`
	Endpoint        string    `json:"endpoint"`
	Bucket          string    `json:"bucket"`
	Root            string    `json:"root"`
	Secure          bool      `json:"secure"`
	CredentialsFile string    `json:"credentials_file"`
	CAFile          string    `json:"ca_file,omitempty"`
}
type CaptureRunnerBootstrap struct {
	Capture            LocalCaptureResourceConfig `json:"source"`
	Upload             UploadJobRequest           `json:"upload"`
	Verifier           VerifyJobRequest           `json:"verifier"`
	ArtifactRead       ArtifactClientBootstrap    `json:"artifact_read"`
	ArtifactFinalWrite ArtifactClientBootstrap    `json:"artifact_final_write"`
	SigningReference   SecretRef                  `json:"signing_reference"`
	SigningKeyFile     string                     `json:"signing_key_file"`
}

func readRunnerFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("not_ready")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > max {
		return nil, errors.New("validation_failed")
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(raw)) > max {
		return nil, errors.New("validation_failed")
	}
	return raw, nil
}
func artifactClientFromBootstrap(c ArtifactClientBootstrap) (ImmutableObjectStore, func(), error) {
	var zero ImmutableObjectStore
	if c.Reference.Validate("artifact") != nil || c.Endpoint == "" || strings.Contains(c.Endpoint, "://") {
		return zero, nil, errors.New("validation_failed")
	}
	if !c.Secure {
		host, _, err := net.SplitHostPort(c.Endpoint)
		ip := net.ParseIP(host)
		if err != nil || host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return zero, nil, errors.New("isolation_invalid")
		}
	}
	var credential struct {
		AccessKey    string `json:"access_key"`
		SecretKey    string `json:"secret_key"`
		SessionToken string `json:"session_token,omitempty"`
	}
	raw, err := readRunnerFile(c.CredentialsFile, 16384)
	if err != nil {
		return zero, nil, err
	}
	defer clear(raw)
	if strictDecode(raw, &credential) != nil || credential.AccessKey == "" || credential.SecretKey == "" {
		return zero, nil, errors.New("validation_failed")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if c.CAFile != "" {
		ca, err := readRunnerFile(c.CAFile, 1<<20)
		if err != nil {
			return zero, nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(ca) {
			return zero, nil, errors.New("validation_failed")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client, err := minio.New(c.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(credential.AccessKey, credential.SecretKey, credential.SessionToken), Secure: c.Secure, Transport: transport})
	if err != nil {
		return zero, nil, errors.New("artifact_unavailable")
	}
	store := ImmutableObjectStore{Client: client, Bucket: c.Bucket, Root: c.Root}
	if _, err = store.key("bootstrap-check"); err != nil {
		transport.CloseIdleConnections()
		return zero, nil, errors.New("validation_failed")
	}
	return store, transport.CloseIdleConnections, nil
}

// LoadCaptureTaskRunner installs concrete capture/upload/core-publication
// adapters from an offline credential mount. It does not assert dependency
// health, enroll writers, or install an acceptance signer/catalog capability.
func LoadCaptureTaskRunner(ctx context.Context, path string, client kubernetes.Interface) (*CaptureTaskRunner, func(), error) {
	var cfg CaptureRunnerBootstrap
	raw, err := readRunnerFile(path, 256<<10)
	if err != nil {
		return nil, nil, err
	}
	defer clear(raw)
	if strictDecode(raw, &cfg) != nil || client == nil || cfg.SigningReference.Validate("signing") != nil || cfg.ArtifactRead.Reference != cfg.ArtifactFinalWrite.Reference {
		return nil, nil, errors.New("validation_failed")
	}
	identities := []string{cfg.Capture.Capture.ServiceAccount, cfg.Capture.Secrets.ServiceAccount, cfg.Upload.ServiceAccount, cfg.Verifier.ServiceAccount}
	seen := map[string]bool{}
	for _, identity := range identities {
		if identity == "" || seen[identity] {
			return nil, nil, errors.New("isolation_invalid")
		}
		seen[identity] = true
	}
	if cfg.Verifier.BootstrapUID == "" || cfg.Upload.BootstrapUID == "" || cfg.Capture.Capture.BootstrapUID == "" || cfg.Capture.Secrets.BootstrapUID == "" || cfg.Capture.Capture.Gate.Scope == "" {
		return nil, nil, errors.New("validation_failed")
	}
	readStore, closeRead, err := artifactClientFromBootstrap(cfg.ArtifactRead)
	if err != nil {
		return nil, nil, err
	}
	writeStore, closeWrite, err := artifactClientFromBootstrap(cfg.ArtifactFinalWrite)
	if err != nil {
		closeRead()
		return nil, nil, err
	}
	key, err := readRunnerFile(cfg.SigningKeyFile, ed25519.PrivateKeySize)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		closeRead()
		closeWrite()
		clear(key)
		return nil, nil, errors.New("signing_key_unavailable")
	}
	closeAll := func() { closeRead(); closeWrite(); clear(key) }
	jobs := CaptureJobs{Client: client}
	stages := LocalCaptureResources{Volumes: LocalStageVolumes{Client: client, MountDirectory: cfg.Capture.MountDirectory}, Config: cfg.Capture}
	publisher := CorePublisher{CaptureJobs: jobs, Uploads: UploadJobs{Client: client}, Verifier: VerifyJobs{Client: client}, VerifyTemplate: cfg.Verifier, Resources: KubernetesCoreResources{Client: client, UploadTemplate: cfg.Upload}}
	publisher.ResolveSigner = func(_ context.Context, ref SecretRef) (ed25519.PrivateKey, error) {
		if ref != cfg.SigningReference {
			return nil, errors.New("signing_key_unavailable")
		}
		return append(ed25519.PrivateKey(nil), key...), nil
	}
	publisher.ResolveReader = func(_ context.Context, ref SecretRef) (CoreArtifactReader, error) {
		if ref != cfg.ArtifactRead.Reference {
			return nil, errors.New("artifact_unavailable")
		}
		return readStore, nil
	}
	publisher.FinalWrites = FinalWriteDriver{Resolve: func(_ context.Context, ref SecretRef) (FinalWriteProvider, error) {
		if ref != cfg.ArtifactFinalWrite.Reference {
			return nil, errors.New("artifact_unavailable")
		}
		return writeStore, nil
	}}
	select {
	case <-ctx.Done():
		closeAll()
		return nil, nil, ctx.Err()
	default:
	}
	return &CaptureTaskRunner{Jobs: jobs, Resources: stages, Publisher: &publisher}, closeAll, nil
}
