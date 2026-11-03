//go:build integration

// Integration tests for OpenClawTransferService against a real Kubernetes
// pod. Skipped unless OPENCLAW_TEST_POD is set.
//
// Usage:
//
//	export KUBECONFIG=/path/to/kubeconfig
//	export OPENCLAW_TEST_POD=<namespace>/<pod-name>
//	go test -tags integration ./internal/services -run TestOpenClawTransfer_ -v
//
// The test pod must be a running OpenClaw/webtop instance (container name
// "desktop") with /config mounted as the persistent workspace.

package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type testExecRunner struct {
	cfg    *rest.Config
	client *kubernetes.Clientset
	ns     string
	pod    string
}

func mustTestRunner(t *testing.T) *testExecRunner {
	t.Helper()
	target := os.Getenv("OPENCLAW_TEST_POD")
	if target == "" {
		t.Skip("OPENCLAW_TEST_POD not set (format: <namespace>/<pod>); skipping integration test")
	}
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("OPENCLAW_TEST_POD must be <namespace>/<pod>, got %q", target)
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}
	return &testExecRunner{cfg: cfg, client: cs, ns: parts[0], pod: parts[1]}
}

func (r *testExecRunner) exec(ctx context.Context, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := r.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(r.pod).
		Namespace(r.ns).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "desktop",
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(r.cfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

func TestOpenClawTransfer_Export_RoundTrip(t *testing.T) {
	r := mustTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Seed .openclaw with a marker file under /config.
	markerName := fmt.Sprintf("marker-%d.txt", time.Now().UnixNano())
	seed := []string{"sh", "-lc", fmt.Sprintf(`mkdir -p /config/.openclaw && echo hello > /config/.openclaw/%s`, markerName)}
	if err := r.exec(ctx, seed, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	t.Cleanup(func() {
		_ = r.exec(context.Background(), []string{"sh", "-lc", fmt.Sprintf(`rm -f /config/.openclaw/%s`, markerName)}, nil, io.Discard, io.Discard)
	})

	// Run the real Export command.
	var stdout, stderr bytes.Buffer
	if err := r.exec(ctx, buildExportCommand(), nil, &stdout, &stderr); err != nil {
		t.Fatalf("export exec: %v (stderr=%s)", err, stderr.String())
	}
	if stdout.Len() < 100 {
		t.Fatalf("export archive too small (%d bytes), stderr=%s", stdout.Len(), stderr.String())
	}

	// Verify marker entry exists in the archive.
	gz, err := gzip.NewReader(&stdout)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	wantSuffix := ".openclaw/" + markerName
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if strings.HasSuffix(hdr.Name, wantSuffix) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("marker %q not found in archive", wantSuffix)
	}
}

func TestOpenClawTransfer_Export_MissingWorkspace(t *testing.T) {
	r := mustTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Temporarily move .openclaw out of the way.
	backup := fmt.Sprintf(".openclaw.bak-%d", time.Now().UnixNano())
	if err := r.exec(ctx, []string{"sh", "-lc", fmt.Sprintf(`if [ -d /config/.openclaw ]; then mv /config/.openclaw /config/%s; fi`, backup)}, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("hide workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = r.exec(context.Background(), []string{"sh", "-lc", fmt.Sprintf(`if [ -d /config/%s ]; then rm -rf /config/.openclaw && mv /config/%s /config/.openclaw; fi`, backup, backup)}, nil, io.Discard, io.Discard)
	})

	var stdout, stderr bytes.Buffer
	err := r.exec(ctx, buildExportCommand(), nil, &stdout, &stderr)
	if !isExportEmptyWorkspaceError(err) {
		t.Fatalf("expected exit-code-42 empty-workspace error, got err=%v stderr=%q", err, stderr.String())
	}
}

func TestOpenClawTransfer_Import_OwnershipIsAbc(t *testing.T) {
	r := mustTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Build a tiny tar.gz containing .openclaw/ownership-probe.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("probe\n")
	hdr := &tar.Header{Name: ".openclaw/ownership-probe", Mode: 0o644, Size: int64(len(content)), ModTime: time.Now()}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	tw.Close()
	gz.Close()

	var stderr bytes.Buffer
	if err := r.exec(ctx, buildImportCommand(), &buf, io.Discard, &stderr); err != nil {
		t.Fatalf("import exec: %v (stderr=%s)", err, stderr.String())
	}
	t.Cleanup(func() {
		_ = r.exec(context.Background(), []string{"sh", "-lc", `rm -f /config/.openclaw/ownership-probe`}, nil, io.Discard, io.Discard)
	})

	// stat the restored file; expect uid:gid 1000:1000.
	var out bytes.Buffer
	if err := r.exec(ctx, []string{"sh", "-lc", `stat -c '%u:%g' /config/.openclaw/ownership-probe`}, nil, &out, io.Discard); err != nil {
		t.Fatalf("stat probe: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != "1000:1000" {
		t.Fatalf("ownership = %q, want 1000:1000", got)
	}
}
