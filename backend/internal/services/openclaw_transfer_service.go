package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	k8sexec "k8s.io/client-go/util/exec"
)

const (
	openclawConfigDirName       = ".openclaw"
	hermesConfigDirName         = ".hermes"
	openclawBaseDir             = "/config"
	openclawExportEmptyExitCode = 42
)

// ErrOpenClawWorkspaceMissing is returned by Export when the .openclaw
// workspace does not exist inside the desktop container. Handlers should
// map this to an HTTP 404 rather than returning an empty 200 body.
var ErrOpenClawWorkspaceMissing = errors.New("openclaw workspace is empty or missing")

// ErrHermesWorkspaceMissing is returned by ExportHermes when the .hermes
// workspace does not exist inside the runtime container.
var ErrHermesWorkspaceMissing = errors.New("hermes workspace is empty or missing")

type OpenClawTransferService interface {
	Export(ctx context.Context, userID, instanceID int) ([]byte, error)
	Import(ctx context.Context, userID, instanceID int, archive io.Reader) error
	ExportHermes(ctx context.Context, userID, instanceID int) ([]byte, error)
	ImportHermes(ctx context.Context, userID, instanceID int, archive io.Reader) error
}

type openClawTransferService struct {
	podService *k8s.PodService
}

type workspaceTransferSpec struct {
	dirName           string
	baseDirExpr       string
	missingErr        error
	actionLabel       string
	preserveTargetDir bool
}

func NewOpenClawTransferService() OpenClawTransferService {
	return &openClawTransferService{
		podService: k8s.NewPodService(),
	}
}

// buildBaseDirExpr returns a POSIX shell expression that resolves the
// OpenClaw persistent directory inside the desktop container. It honors the
// CLAWMANAGER_AGENT_PERSISTENT_DIR env var (injected by ClawManager at pod
// creation) and falls back to the hardcoded PVC mount path.
//
// $HOME is intentionally NOT used: `kubectl exec` spawns a fresh process as
// root with HOME=/root, which does not match the linuxserver entrypoint's
// runtime user `abc` (HOME=/config).
func buildBaseDirExpr() string {
	return fmt.Sprintf("${CLAWMANAGER_AGENT_PERSISTENT_DIR:-%s}", openclawBaseDir)
}

func openClawWorkspaceSpec() workspaceTransferSpec {
	return workspaceTransferSpec{
		dirName:     openclawConfigDirName,
		baseDirExpr: buildBaseDirExpr(),
		missingErr:  ErrOpenClawWorkspaceMissing,
		actionLabel: ".openclaw",
	}
}

func hermesWorkspaceSpec() workspaceTransferSpec {
	return workspaceTransferSpec{
		dirName:           hermesConfigDirName,
		baseDirExpr:       openclawBaseDir,
		missingErr:        ErrHermesWorkspaceMissing,
		actionLabel:       ".hermes",
		preserveTargetDir: true,
	}
}

// buildExportCommand returns the sh -lc command used to stream a gzipped
// tarball of the .openclaw workspace from the desktop container over stdout.
// When the workspace does not exist, the command exits with
// openclawExportEmptyExitCode so the service layer can map it to
// ErrOpenClawWorkspaceMissing instead of returning an empty archive.
func buildExportCommand() []string {
	return buildWorkspaceExportCommand(openClawWorkspaceSpec())
}

func buildHermesExportCommand() []string {
	return buildWorkspaceExportCommand(hermesWorkspaceSpec())
}

func buildWorkspaceExportCommand(spec workspaceTransferSpec) []string {
	script := fmt.Sprintf(
		`base_dir="%s"; target_dir="$base_dir/%s"; `+
			`if [ ! -d "$target_dir" ]; then exit %d; fi; `+
			`tar czf - -C "$base_dir" %s`,
		spec.baseDirExpr,
		spec.dirName,
		openclawExportEmptyExitCode,
		shellQuote(spec.dirName),
	)
	return []string{"sh", "-lc", script}
}

// buildImportCommand returns the sh -lc command used to restore a gzipped
// tarball of the .openclaw workspace into the desktop container from stdin.
// The extract is re-exec'd as user `abc` (uid 1000) via `su` so restored
// files are owned by the runtime user, matching how the linuxserver
// entrypoint writes /config.
func buildImportCommand() []string {
	return buildWorkspaceImportCommand(openClawWorkspaceSpec())
}

func buildHermesImportCommand() []string {
	return buildWorkspaceImportCommand(hermesWorkspaceSpec())
}

func buildWorkspaceImportCommand(spec workspaceTransferSpec) []string {
	clearTarget := `rm -rf "$target_dir" && mkdir -p "$base_dir"`
	if spec.preserveTargetDir {
		script := fmt.Sprintf(
			`base_dir="%s"; target_dir="$base_dir/%s"; `+
				`mkdir -p "$target_dir" && find "$target_dir" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && `+
				`tar xzf - -C "$base_dir" && chown -R abc:abc "$target_dir"`,
			spec.baseDirExpr,
			spec.dirName,
		)
		return []string{"sh", "-lc", script}
	}
	inner := fmt.Sprintf(
		`base_dir="%s"; target_dir="$base_dir/%s"; `+
			`%s && tar xzf - -C "$base_dir"`,
		spec.baseDirExpr,
		spec.dirName,
		clearTarget,
	)
	outer := fmt.Sprintf(`exec su abc -s /bin/sh -c %s`, shellQuote(inner))
	return []string{"sh", "-lc", outer}
}

func (s *openClawTransferService) Export(ctx context.Context, userID, instanceID int) ([]byte, error) {
	return s.exportWorkspace(ctx, userID, instanceID, openClawWorkspaceSpec(), buildExportCommand())
}

func (s *openClawTransferService) ExportHermes(ctx context.Context, userID, instanceID int) ([]byte, error) {
	return s.exportWorkspace(ctx, userID, instanceID, hermesWorkspaceSpec(), buildHermesExportCommand())
}

func (s *openClawTransferService) exportWorkspace(ctx context.Context, userID, instanceID int, spec workspaceTransferSpec, command []string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := s.exec(ctx, userID, instanceID, command, nil, &stdout, &stderr); err != nil {
		if isExportEmptyWorkspaceError(err) {
			return nil, spec.missingErr
		}
		return nil, formatExecError("export "+spec.actionLabel, err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// isExportEmptyWorkspaceError reports whether err indicates the export
// command exited with openclawExportEmptyExitCode (signalling that the
// .openclaw workspace does not exist).
func isExportEmptyWorkspaceError(err error) bool {
	if err == nil {
		return false
	}
	var codeErr k8sexec.CodeExitError
	if errors.As(err, &codeErr) {
		return codeErr.Code == openclawExportEmptyExitCode
	}
	// Fallback: remotecommand sometimes wraps the exit code in a plain
	// error whose message contains "exit code N".
	return strings.Contains(err.Error(), fmt.Sprintf("exit code %d", openclawExportEmptyExitCode))
}

func (s *openClawTransferService) Import(ctx context.Context, userID, instanceID int, archive io.Reader) error {
	return s.importWorkspace(ctx, userID, instanceID, archive, openClawWorkspaceSpec(), buildImportCommand())
}

func (s *openClawTransferService) ImportHermes(ctx context.Context, userID, instanceID int, archive io.Reader) error {
	return s.importWorkspace(ctx, userID, instanceID, archive, hermesWorkspaceSpec(), buildHermesImportCommand())
}

func (s *openClawTransferService) importWorkspace(ctx context.Context, userID, instanceID int, archive io.Reader, spec workspaceTransferSpec, command []string) error {
	var stderr bytes.Buffer
	if err := s.exec(ctx, userID, instanceID, command, archive, nil, &stderr); err != nil {
		return formatExecError("import "+spec.actionLabel, err, stderr.String())
	}

	return nil
}

func (s *openClawTransferService) exec(ctx context.Context, userID, instanceID int, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if s.podService == nil || s.podService.GetClient() == nil || s.podService.GetClient().Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	pod, err := s.podService.GetPod(ctx, userID, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	req := s.podService.GetClient().Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: "desktop",
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(s.podService.GetClient().Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to initialize exec stream: %w", err)
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func formatExecError(action string, execErr error, stderr string) error {
	if stderr != "" {
		return fmt.Errorf("failed to %s: %s", action, stderr)
	}
	return fmt.Errorf("failed to %s: %w", action, execErr)
}
