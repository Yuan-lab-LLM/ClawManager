package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"clawreef/internal/models"
	"clawreef/internal/repository"
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
	podService     *k8s.PodService
	instanceRepo   repository.InstanceRepository
	bindingRepo    repository.InstanceRuntimeBindingRepository
	runtimePodRepo repository.RuntimePodRepository
}

type openClawTransferRuntimeRepositories struct {
	instanceRepo   repository.InstanceRepository
	bindingRepo    repository.InstanceRuntimeBindingRepository
	runtimePodRepo repository.RuntimePodRepository
}

type transferExecTarget struct {
	namespace string
	podName   string
	container string
}

var (
	openClawTransferReposMu sync.RWMutex
	openClawTransferRepos   openClawTransferRuntimeRepositories
)

type workspaceTransferSpec struct {
	dirName           string
	baseDirExpr       string
	missingErr        error
	actionLabel       string
	preserveTargetDir bool
}

func NewOpenClawTransferService() OpenClawTransferService {
	openClawTransferReposMu.RLock()
	repos := openClawTransferRepos
	openClawTransferReposMu.RUnlock()
	return &openClawTransferService{
		podService:     k8s.NewPodService(),
		instanceRepo:   repos.instanceRepo,
		bindingRepo:    repos.bindingRepo,
		runtimePodRepo: repos.runtimePodRepo,
	}
}

func SetOpenClawTransferRuntimeRepositories(instanceRepo repository.InstanceRepository, bindingRepo repository.InstanceRuntimeBindingRepository, runtimePodRepo repository.RuntimePodRepository) {
	openClawTransferReposMu.Lock()
	openClawTransferRepos = openClawTransferRuntimeRepositories{
		instanceRepo:   instanceRepo,
		bindingRepo:    bindingRepo,
		runtimePodRepo: runtimePodRepo,
	}
	openClawTransferReposMu.Unlock()
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
	inner := fmt.Sprintf(
		`base_dir="%s"; target_dir="$base_dir/%s"; `+
			`import_root="$base_dir/.clawmanager-imports"; mkdir -p "$import_root"; `+
			`staging_dir="$(mktemp -d "$import_root/import.XXXXXX")" || exit 1; `+
			`archive="$staging_dir/archive.tar.gz"; cat > "$archive"; `+
			`tar tzf "$archive" | awk 'BEGIN { ok=1 } /^\// { ok=0 } /(^|\/)\.\.($|\/)/ { ok=0 } { if ($0 !~ /^%s(\/|$)/) ok=0 } END { exit ok ? 0 : 1 }' || exit 65; `+
			`tar xzf "$archive" -C "$staging_dir" --no-same-owner; `+
			`new_target="$staging_dir/%s"; test -d "$new_target" || exit 66; `+
			`preserved="$base_dir/%s.preserved.$(date +%%s%%N)"; `+
			`if [ -e "$target_dir" ]; then mv "$target_dir" "$preserved"; fi; `+
			`if ! mv "$new_target" "$target_dir"; then if [ -e "$preserved" ]; then mv "$preserved" "$target_dir"; fi; exit 67; fi; `+
			`chown -R abc:abc "$target_dir"`,
		spec.baseDirExpr,
		spec.dirName,
		spec.dirName,
		spec.dirName,
		spec.dirName,
	)
	if spec.preserveTargetDir {
		return []string{"sh", "-lc", inner}
	}
	return []string{"sh", "-lc", fmt.Sprintf(`exec su abc -s /bin/sh -c %s`, shellQuote(inner))}
}

func (s *openClawTransferService) Export(ctx context.Context, userID, instanceID int) ([]byte, error) {
	return s.exportWorkspace(ctx, userID, instanceID, openClawWorkspaceSpec())
}

func (s *openClawTransferService) ExportHermes(ctx context.Context, userID, instanceID int) ([]byte, error) {
	return s.exportWorkspace(ctx, userID, instanceID, hermesWorkspaceSpec())
}

func (s *openClawTransferService) exportWorkspace(ctx context.Context, userID, instanceID int, spec workspaceTransferSpec) ([]byte, error) {
	resolvedSpec, err := s.workspaceSpecForInstance(ctx, userID, instanceID, spec)
	if err != nil {
		return nil, err
	}
	command := buildWorkspaceExportCommand(resolvedSpec)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := s.exec(ctx, userID, instanceID, command, nil, &stdout, &stderr); err != nil {
		if isExportEmptyWorkspaceError(err) {
			return nil, resolvedSpec.missingErr
		}
		return nil, formatExecError("export "+resolvedSpec.actionLabel, err, stderr.String())
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
	return s.importWorkspace(ctx, userID, instanceID, archive, openClawWorkspaceSpec())
}

func (s *openClawTransferService) ImportHermes(ctx context.Context, userID, instanceID int, archive io.Reader) error {
	return s.importWorkspace(ctx, userID, instanceID, archive, hermesWorkspaceSpec())
}

func (s *openClawTransferService) importWorkspace(ctx context.Context, userID, instanceID int, archive io.Reader, spec workspaceTransferSpec) error {
	resolvedSpec, err := s.workspaceSpecForInstance(ctx, userID, instanceID, spec)
	if err != nil {
		return err
	}
	command := buildWorkspaceImportCommand(resolvedSpec)
	var stderr bytes.Buffer
	if err := s.exec(ctx, userID, instanceID, command, archive, nil, &stderr); err != nil {
		return formatExecError("import "+resolvedSpec.actionLabel, err, stderr.String())
	}

	return nil
}

func (s *openClawTransferService) workspaceSpecForInstance(ctx context.Context, userID, instanceID int, spec workspaceTransferSpec) (workspaceTransferSpec, error) {
	instance, err := s.runtimeManagedInstance(ctx, userID, instanceID)
	if err != nil || instance == nil {
		return spec, err
	}
	baseDir := ""
	if instance.WorkspacePath != nil {
		baseDir = strings.TrimSpace(*instance.WorkspacePath)
	}
	if baseDir == "" {
		runtimeType, ok := NormalizeV2RuntimeType(instance.Type)
		if !ok {
			return spec, nil
		}
		baseDir = RuntimeWorkspacePath(runtimeType, instance.UserID, instance.ID)
	}
	spec.baseDirExpr = baseDir
	return spec, nil
}

func (s *openClawTransferService) exec(ctx context.Context, userID, instanceID int, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if s.podService == nil || s.podService.GetClient() == nil || s.podService.GetClient().Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	target, err := s.runtimeExecTarget(ctx, userID, instanceID)
	if err != nil {
		return err
	}
	if target == nil {
		pod, podErr := s.podService.GetPod(ctx, userID, instanceID)
		if podErr != nil {
			return fmt.Errorf("failed to get pod: %w", podErr)
		}
		target = &transferExecTarget{
			namespace: pod.Namespace,
			podName:   pod.Name,
			container: "desktop",
		}
	}

	req := s.podService.GetClient().Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(target.podName).
		Namespace(target.namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: target.container,
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

func (s *openClawTransferService) runtimeManagedInstance(ctx context.Context, userID, instanceID int) (*models.Instance, error) {
	if s == nil || s.instanceRepo == nil {
		return nil, nil
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return nil, nil
	}
	if instance.UserID != userID {
		return nil, fmt.Errorf("instance does not belong to user")
	}
	if !isLiteRuntimeInstance(instance) {
		return nil, nil
	}
	return instance, nil
}

func (s *openClawTransferService) runtimeExecTarget(ctx context.Context, userID, instanceID int) (*transferExecTarget, error) {
	instance, err := s.runtimeManagedInstance(ctx, userID, instanceID)
	if err != nil || instance == nil {
		return nil, err
	}
	if s.bindingRepo == nil || s.runtimePodRepo == nil {
		return nil, fmt.Errorf("runtime repositories are not configured for lite workspace transfer")
	}
	binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime binding: %w", err)
	}
	if binding == nil {
		return nil, fmt.Errorf("runtime binding not found for instance %d", instanceID)
	}
	if binding.Generation != instance.RuntimeGeneration {
		return nil, fmt.Errorf("runtime binding generation does not match instance")
	}
	runtimePod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime pod: %w", err)
	}
	if runtimePod == nil {
		return nil, fmt.Errorf("runtime pod not found for instance %d", instanceID)
	}
	return &transferExecTarget{
		namespace: runtimePod.Namespace,
		podName:   runtimePod.PodName,
		container: "runtime",
	}, nil
}

func isLiteRuntimeInstance(instance *models.Instance) bool {
	if instance == nil {
		return false
	}
	if mode, ok := NormalizeInstanceMode(instance.InstanceMode); ok {
		return mode == InstanceModeLite
	}
	return strings.EqualFold(strings.TrimSpace(instance.RuntimeType), RuntimeBackendGateway)
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
