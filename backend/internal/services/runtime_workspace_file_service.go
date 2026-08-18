package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	runtimeWorkspaceErrPrefix = "CLAW_WORKSPACE_ERR:"
	runtimeWorkspaceBaseDir   = "/config"
)

type runtimeWorkspaceExecutor interface {
	exec(ctx context.Context, userID, instanceID int, command []string, stdin io.Reader, stdout, stderr io.Writer) error
}

type runtimeWorkspaceFileService struct {
	auditRepo repository.WorkspaceFileAuditRepository
	executor  runtimeWorkspaceExecutor
}

type k8sRuntimeWorkspaceExecutor struct {
	deploymentService *k8s.InstanceDeploymentService
}

type runtimeWorkspaceEntryPayload struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func NewRuntimeWorkspaceFileService(auditRepo repository.WorkspaceFileAuditRepository) WorkspaceFileService {
	return &runtimeWorkspaceFileService{
		auditRepo: auditRepo,
		executor: &k8sRuntimeWorkspaceExecutor{
			deploymentService: k8s.NewInstanceDeploymentService(),
		},
	}
}

func (s *runtimeWorkspaceFileService) List(ctx context.Context, scope WorkspaceFileScope, relativePath string) ([]WorkspaceEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relative, err := cleanWorkspaceRelativePath(relativePath)
	if err != nil {
		return nil, err
	}
	var response struct {
		Entries []runtimeWorkspaceEntryPayload `json:"entries"`
	}
	if err := s.runJSON(ctx, scope, []string{"python3", "-c", runtimeWorkspaceListScript, runtimeWorkspaceBase(scope), relative}, nil, &response); err != nil {
		return nil, err
	}
	entries := make([]WorkspaceEntry, 0, len(response.Entries))
	for _, payload := range response.Entries {
		entry := runtimeWorkspaceEntryFromPayload(payload)
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].Name < entries[j].Name
		}
		return left < right
	})
	return entries, nil
}

func (s *runtimeWorkspaceFileService) Preview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspacePreview, error) {
	relative, entry, err := s.statFile(ctx, scope, relativePath)
	if err != nil {
		return nil, err
	}
	kind, contentType, previewable, maxBytes := workspacePreviewKind(entry.Name, entry.Size)
	downloadURL := workspaceDownloadURL(scope.InstanceID, relative)
	if !previewable {
		return &WorkspacePreview{
			Kind:        "binary",
			ContentType: "application/octet-stream",
			DownloadURL: downloadURL,
		}, nil
	}
	if maxBytes > 0 && entry.Size > maxBytes {
		return nil, ErrWorkspacePreviewTooLarge
	}
	if kind == "text" {
		file, _, _, err := s.openRemoteFile(ctx, scope, relative, false)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, textPreviewMaxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > textPreviewMaxBytes {
			return nil, ErrWorkspacePreviewTooLarge
		}
		return &WorkspacePreview{
			Kind:        "text",
			ContentType: contentType,
			Text:        string(data),
			DownloadURL: downloadURL,
		}, nil
	}

	return &WorkspacePreview{
		Kind:        kind,
		ContentType: contentType,
		PreviewURL:  workspacePreviewURL(scope.InstanceID, relative),
		DownloadURL: downloadURL,
	}, nil
}

func (s *runtimeWorkspaceFileService) OpenPreview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	_, entry, err := s.statFile(ctx, scope, relativePath)
	if err != nil {
		return nil, "", 0, err
	}
	_, contentType, previewable, maxBytes := workspacePreviewKind(entry.Name, entry.Size)
	if !previewable {
		return nil, "", 0, ErrWorkspaceFileExpected
	}
	if maxBytes > 0 && entry.Size > maxBytes {
		return nil, "", 0, ErrWorkspacePreviewTooLarge
	}
	file, _, size, err := s.openRemoteFile(ctx, scope, relativePath, false)
	if err != nil {
		return nil, "", 0, err
	}
	return file, contentType, size, nil
}

func (s *runtimeWorkspaceFileService) OpenDownload(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	relative, entry, err := s.statFile(ctx, scope, relativePath)
	if err != nil {
		return nil, "", 0, err
	}
	file, filename, size, err := s.openRemoteFile(ctx, scope, relative, true)
	if err != nil {
		return nil, "", 0, err
	}
	if filename == "" {
		filename = entry.Name
	}
	return file, filename, size, nil
}

func (s *runtimeWorkspaceFileService) Upload(ctx context.Context, scope WorkspaceFileScope, relativeDir string, filename string, reader io.Reader, size int64) (*WorkspaceEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maxBytes := WorkspaceUploadMaxBytes()
	if size > maxBytes {
		return nil, ErrWorkspaceUploadTooLarge
	}
	cleanName, err := sanitizeWorkspaceFilename(filename)
	if err != nil {
		return nil, err
	}
	dir, err := cleanWorkspaceRelativePath(relativeDir)
	if err != nil {
		return nil, err
	}
	targetRelative := joinWorkspaceRelative(dir, cleanName)
	limited := io.LimitReader(reader, maxBytes+1)
	var payload runtimeWorkspaceEntryPayload
	if err := s.runJSON(ctx, scope, []string{
		"python3",
		"-c",
		runtimeWorkspaceUploadScript,
		runtimeWorkspaceBase(scope),
		dir,
		cleanName,
		fmt.Sprintf("%d", maxBytes),
	}, limited, &payload); err != nil {
		return nil, err
	}
	if err := s.recordAudit(ctx, scope, "upload", targetRelative, payload.Size); err != nil {
		return nil, err
	}
	entry := runtimeWorkspaceEntryFromPayload(payload)
	return &entry, nil
}

func (s *runtimeWorkspaceFileService) Mkdir(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspaceEntry, error) {
	if isWorkspaceRootRelative(relativePath) {
		return nil, ErrWorkspaceRootOperation
	}
	relative, err := cleanWorkspaceRelativePath(relativePath)
	if err != nil {
		return nil, err
	}
	var payload runtimeWorkspaceEntryPayload
	if err := s.runJSON(ctx, scope, []string{"python3", "-c", runtimeWorkspaceMkdirScript, runtimeWorkspaceBase(scope), relative}, nil, &payload); err != nil {
		return nil, err
	}
	if err := s.recordAudit(ctx, scope, "mkdir", relative, 0); err != nil {
		return nil, err
	}
	entry := runtimeWorkspaceEntryFromPayload(payload)
	return &entry, nil
}

func (s *runtimeWorkspaceFileService) Rename(ctx context.Context, scope WorkspaceFileScope, oldPath, newPath string) (*WorkspaceEntry, error) {
	if isWorkspaceRootRelative(oldPath) || isWorkspaceRootRelative(newPath) {
		return nil, ErrWorkspaceRootOperation
	}
	oldRelative, err := cleanWorkspaceRelativePath(oldPath)
	if err != nil {
		return nil, err
	}
	newRelative, err := cleanWorkspaceRelativePath(newPath)
	if err != nil {
		return nil, err
	}
	var payload runtimeWorkspaceEntryPayload
	if err := s.runJSON(ctx, scope, []string{"python3", "-c", runtimeWorkspaceRenameScript, runtimeWorkspaceBase(scope), oldRelative, newRelative}, nil, &payload); err != nil {
		return nil, err
	}
	if err := s.recordAudit(ctx, scope, "rename", oldRelative+" -> "+newRelative, 0); err != nil {
		return nil, err
	}
	entry := runtimeWorkspaceEntryFromPayload(payload)
	return &entry, nil
}

func (s *runtimeWorkspaceFileService) Delete(ctx context.Context, scope WorkspaceFileScope, relativePath string) error {
	if isWorkspaceRootRelative(relativePath) {
		return ErrWorkspaceRootOperation
	}
	relative, err := cleanWorkspaceRelativePath(relativePath)
	if err != nil {
		return err
	}
	if err := s.run(ctx, scope, []string{"python3", "-c", runtimeWorkspaceDeleteScript, runtimeWorkspaceBase(scope), relative}, nil, io.Discard); err != nil {
		return err
	}
	return s.recordAudit(ctx, scope, "delete", relative, 0)
}

func (s *runtimeWorkspaceFileService) statFile(ctx context.Context, scope WorkspaceFileScope, relativePath string) (string, WorkspaceEntry, error) {
	relative, err := cleanWorkspaceRelativePath(relativePath)
	if err != nil {
		return "", WorkspaceEntry{}, err
	}
	var payload runtimeWorkspaceEntryPayload
	if err := s.runJSON(ctx, scope, []string{"python3", "-c", runtimeWorkspaceStatFileScript, runtimeWorkspaceBase(scope), relative}, nil, &payload); err != nil {
		return "", WorkspaceEntry{}, err
	}
	entry := runtimeWorkspaceEntryFromPayload(payload)
	return relative, entry, nil
}

func (s *runtimeWorkspaceFileService) openRemoteFile(ctx context.Context, scope WorkspaceFileScope, relativePath string, audit bool) (*os.File, string, int64, error) {
	relative, entry, err := s.statFile(ctx, scope, relativePath)
	if err != nil {
		return nil, "", 0, err
	}
	file, err := os.CreateTemp("", "clawmanager-runtime-workspace-*")
	if err != nil {
		return nil, "", 0, err
	}
	ok := false
	defer func() {
		if !ok {
			name := file.Name()
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if err := s.run(ctx, scope, []string{"python3", "-c", runtimeWorkspaceStreamFileScript, runtimeWorkspaceBase(scope), relative}, nil, file); err != nil {
		return nil, "", 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", 0, err
	}
	if audit {
		action := "download"
		if strings.HasPrefix(strings.TrimSpace(scope.AuditActionPrefix), "llm_") {
			action = "input_file"
		}
		if err := s.recordAudit(ctx, scope, action, relative, entry.Size); err != nil {
			return nil, "", 0, err
		}
	}
	ok = true
	return file, entry.Name, entry.Size, nil
}

func (s *runtimeWorkspaceFileService) runJSON(ctx context.Context, scope WorkspaceFileScope, command []string, stdin io.Reader, target any) error {
	var stdout bytes.Buffer
	if err := s.run(ctx, scope, command, stdin, &stdout); err != nil {
		return err
	}
	if err := json.Unmarshal(stdout.Bytes(), target); err != nil {
		return fmt.Errorf("failed to parse runtime workspace response: %w", err)
	}
	return nil
}

func (s *runtimeWorkspaceFileService) run(ctx context.Context, scope WorkspaceFileScope, command []string, stdin io.Reader, stdout io.Writer) error {
	if s == nil || s.executor == nil {
		return fmt.Errorf("runtime workspace executor is not configured")
	}
	var stderr bytes.Buffer
	if err := s.executor.exec(ctx, scope.UserID, scope.InstanceID, command, stdin, stdout, &stderr); err != nil {
		return mapRuntimeWorkspaceError(err, stderr.String())
	}
	return nil
}

func (s *runtimeWorkspaceFileService) recordAudit(ctx context.Context, scope WorkspaceFileScope, action, relativePath string, bytes int64) error {
	if s.auditRepo == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	action = strings.TrimSpace(scope.AuditActionPrefix) + action
	return s.auditRepo.Create(ctx, &models.WorkspaceFileAudit{
		InstanceID:   scope.InstanceID,
		UserID:       scope.UserID,
		Action:       action,
		RelativePath: relativePath,
		Bytes:        bytes,
		CreatedAt:    time.Now().UTC(),
	})
}

func (e *k8sRuntimeWorkspaceExecutor) exec(ctx context.Context, userID, instanceID int, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	client := k8s.GetClient()
	if client == nil || client.Clientset == nil || client.Config == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	deploymentService := e.deploymentService
	if deploymentService == nil {
		deploymentService = k8s.NewInstanceDeploymentService()
	}
	pod, err := deploymentService.GetActivePod(ctx, userID, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get active instance pod: %w", err)
	}
	req := client.Clientset.CoreV1().RESTClient().Post().
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
	exec, err := remotecommand.NewSPDYExecutor(client.Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to initialize runtime workspace exec stream: %w", err)
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}

func runtimeWorkspaceBase(scope WorkspaceFileScope) string {
	if base := strings.TrimSpace(scope.WorkspacePath); base != "" {
		return base
	}
	return runtimeWorkspaceBaseDir
}

func runtimeWorkspaceEntryFromPayload(payload runtimeWorkspaceEntryPayload) WorkspaceEntry {
	isDir := payload.IsDir
	return WorkspaceEntry{
		Name:         payload.Name,
		Path:         filepath.ToSlash(payload.Path),
		IsDir:        isDir,
		Size:         payload.Size,
		ModifiedAt:   payload.ModifiedAt.UTC(),
		Previewable:  workspaceEntryPreviewable(payload.Name, payload.Size, isDir),
		Downloadable: !isDir,
	}
}

func mapRuntimeWorkspaceError(execErr error, stderr string) error {
	code := runtimeWorkspaceErrorCode(stderr)
	switch code {
	case "not_found":
		return ErrWorkspacePathNotFound
	case "escape":
		return ErrWorkspacePathEscape
	case "dir_required":
		return ErrWorkspaceDirectoryExpected
	case "file_required":
		return ErrWorkspaceFileExpected
	case "exists":
		return ErrWorkspaceEntryExists
	case "upload_too_large":
		return ErrWorkspaceUploadTooLarge
	case "filename_invalid", "invalid":
		return ErrWorkspacePathInvalid
	}
	if strings.TrimSpace(stderr) != "" {
		return fmt.Errorf("runtime workspace operation failed: %s", strings.TrimSpace(stderr))
	}
	if execErr != nil {
		return execErr
	}
	return errors.New("runtime workspace operation failed")
}

func runtimeWorkspaceErrorCode(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, runtimeWorkspaceErrPrefix) {
			remainder := strings.TrimPrefix(line, runtimeWorkspaceErrPrefix)
			if index := strings.Index(remainder, ":"); index >= 0 {
				return remainder[:index]
			}
			return remainder
		}
	}
	return ""
}

const runtimeWorkspacePythonPrelude = `
import datetime
import json
import os
import shutil
import stat
import sys
import tempfile

ERR_PREFIX = "CLAW_WORKSPACE_ERR:"

def fail(code, message=""):
    print(ERR_PREFIX + code + ":" + str(message), file=sys.stderr)
    sys.exit(1)

def clean_rel(value):
    raw = (value or "").strip().replace("\\", "/")
    if raw in ("", "."):
        return ""
    if "\x00" in raw:
        fail("invalid", "path contains null byte")
    if raw.startswith("/"):
        fail("escape", "absolute paths are not allowed")
    parts = []
    for part in raw.split("/"):
        if part in ("", "."):
            continue
        if part == "..":
            fail("escape", "traversal is not allowed")
        parts.append(part)
    return "/".join(parts)

def safe_base(value):
    base = os.path.realpath(value or "/config")
    if not os.path.isdir(base):
        fail("not_found", "workspace root")
    return base

def is_subpath(base, target):
    try:
        return os.path.commonpath([base, target]) == base
    except ValueError:
        return False

def joined_path(base, rel):
    target = os.path.abspath(os.path.join(base, rel)) if rel else base
    if not is_subpath(base, target):
        fail("escape", rel)
    return target

def target_for(base, rel, allow_missing=False):
    rel = clean_rel(rel)
    target = joined_path(base, rel)
    if os.path.lexists(target):
        real = os.path.realpath(target)
        if not is_subpath(base, real):
            fail("escape", rel)
        return rel, target
    if allow_missing:
        parent = os.path.realpath(os.path.dirname(target))
        if not os.path.isdir(parent):
            fail("invalid", "parent is not a directory")
        if not is_subpath(base, parent):
            fail("escape", rel)
        return rel, target
    fail("not_found", rel)

def entry_payload(base, rel, target):
    try:
        st = os.lstat(target)
    except FileNotFoundError:
        fail("not_found", rel)
    name = os.path.basename(target) if rel else os.path.basename(base)
    modified = datetime.datetime.fromtimestamp(st.st_mtime, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    return {
        "name": name,
        "path": rel,
        "is_dir": stat.S_ISDIR(st.st_mode),
        "size": int(st.st_size),
        "modified_at": modified,
    }

def json_out(value):
    print(json.dumps(value, separators=(",", ":")))

def chown_abc(path):
    try:
        import grp
        import pwd
        os.chown(path, pwd.getpwnam("abc").pw_uid, grp.getgrnam("abc").gr_gid)
    except Exception:
        pass
`

var runtimeWorkspaceListScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
rel, target = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
if not os.path.isdir(target):
    fail("dir_required", rel)
entries = []
for entry in os.scandir(target):
    child_rel = (rel + "/" + entry.name) if rel else entry.name
    child_path = os.path.join(target, entry.name)
    try:
        real = os.path.realpath(child_path)
        if not is_subpath(base, real):
            continue
    except Exception:
        continue
    entries.append(entry_payload(base, child_rel, child_path))
json_out({"entries": entries})
`

var runtimeWorkspaceStatFileScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
rel, target = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
if not os.path.isfile(target):
    fail("file_required", rel)
json_out(entry_payload(base, rel, target))
`

var runtimeWorkspaceStreamFileScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
rel, target = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
if not os.path.isfile(target):
    fail("file_required", rel)
with open(target, "rb") as source:
    shutil.copyfileobj(source, sys.stdout.buffer)
`

var runtimeWorkspaceUploadScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
dir_rel, dir_path = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
name = sys.argv[3] if len(sys.argv) > 3 else ""
max_bytes = int(sys.argv[4]) if len(sys.argv) > 4 else 524288000
if not name or name in (".", "..") or "/" in name or "\\" in name or "\x00" in name:
    fail("filename_invalid", name)
if not os.path.isdir(dir_path):
    fail("dir_required", dir_rel)
target_rel = (dir_rel + "/" + name) if dir_rel else name
target_rel, target = target_for(base, target_rel, allow_missing=True)
if os.path.isdir(target):
    fail("file_required", target_rel)
tmp_fd, tmp_name = tempfile.mkstemp(prefix="." + name + ".tmp-", dir=dir_path)
written = 0
try:
    with os.fdopen(tmp_fd, "wb") as out:
        while True:
            chunk = sys.stdin.buffer.read(1024 * 1024)
            if not chunk:
                break
            written += len(chunk)
            if written > max_bytes:
                fail("upload_too_large", target_rel)
            out.write(chunk)
    chown_abc(tmp_name)
    os.replace(tmp_name, target)
    chown_abc(target)
    json_out(entry_payload(base, target_rel, target))
finally:
    try:
        if os.path.exists(tmp_name):
            os.unlink(tmp_name)
    except Exception:
        pass
`

var runtimeWorkspaceMkdirScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
rel = clean_rel(sys.argv[2] if len(sys.argv) > 2 else "")
if not rel:
    fail("invalid", "workspace root cannot be modified")
rel, target = target_for(base, rel, allow_missing=True)
if os.path.lexists(target):
    fail("exists", rel)
os.makedirs(target, mode=0o750, exist_ok=False)
chown_abc(target)
json_out(entry_payload(base, rel, target))
`

var runtimeWorkspaceRenameScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
old_rel, old_target = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
new_rel, new_target = target_for(base, sys.argv[3] if len(sys.argv) > 3 else "", allow_missing=True)
if not old_rel or not new_rel:
    fail("invalid", "workspace root cannot be modified")
if os.path.lexists(new_target):
    fail("exists", new_rel)
os.rename(old_target, new_target)
json_out(entry_payload(base, new_rel, new_target))
`

var runtimeWorkspaceDeleteScript = runtimeWorkspacePythonPrelude + `
base = safe_base(sys.argv[1])
rel, target = target_for(base, sys.argv[2] if len(sys.argv) > 2 else "")
if not rel:
    fail("invalid", "workspace root cannot be modified")
if os.path.islink(target) or os.path.isfile(target):
    os.unlink(target)
elif os.path.isdir(target):
    shutil.rmtree(target)
else:
    fail("not_found", rel)
`

func runtimeWorkspaceContentType(name string) string {
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}
