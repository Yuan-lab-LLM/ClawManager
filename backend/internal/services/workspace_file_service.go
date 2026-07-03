package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const (
	WorkspaceUploadMaxBytesEnv = "WORKSPACE_UPLOAD_MAX_BYTES"
	defaultWorkspaceUploadMax  = int64(524288000)
	textPreviewMaxBytes        = int64(1024 * 1024)
	imagePreviewMaxBytes       = int64(10 * 1024 * 1024)
)

var (
	ErrWorkspaceDirectoryExpected = errors.New("workspace directory is required")
	ErrWorkspaceFileExpected      = errors.New("workspace file is required")
	ErrWorkspacePreviewTooLarge   = errors.New("workspace preview exceeds maximum size")
	ErrWorkspaceUploadTooLarge    = errors.New("workspace upload exceeds maximum size")
	ErrWorkspaceRootOperation     = errors.New("workspace root cannot be modified")
	ErrWorkspaceFileNameInvalid   = errors.New("workspace filename is invalid")
	ErrWorkspaceEntryExists       = errors.New("workspace entry already exists")
)

type WorkspaceFileScope struct {
	InstanceID    int
	UserID        int
	WorkspacePath string
}

type WorkspaceEntry struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modified_at"`
	Previewable  bool      `json:"previewable"`
	Downloadable bool      `json:"downloadable"`
}

type WorkspacePreview struct {
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Text        string `json:"text,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

type WorkspaceFileService interface {
	List(ctx context.Context, scope WorkspaceFileScope, relativePath string) ([]WorkspaceEntry, error)
	Preview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspacePreview, error)
	OpenPreview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error)
	OpenDownload(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error)
	Upload(ctx context.Context, scope WorkspaceFileScope, relativeDir string, filename string, reader io.Reader, size int64) (*WorkspaceEntry, error)
	Mkdir(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspaceEntry, error)
	Rename(ctx context.Context, scope WorkspaceFileScope, oldPath, newPath string) (*WorkspaceEntry, error)
	Delete(ctx context.Context, scope WorkspaceFileScope, relativePath string) error
}

type workspaceFileService struct {
	auditRepo repository.WorkspaceFileAuditRepository
}

func NewWorkspaceFileService(auditRepo repository.WorkspaceFileAuditRepository) WorkspaceFileService {
	return &workspaceFileService{auditRepo: auditRepo}
}

func WorkspaceUploadMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv(WorkspaceUploadMaxBytesEnv))
	if raw == "" {
		return defaultWorkspaceUploadMax
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultWorkspaceUploadMax
	}
	return value
}

func (s *workspaceFileService) List(ctx context.Context, scope WorkspaceFileScope, relativePath string) ([]WorkspaceEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := ResolveWorkspacePath(scope.WorkspacePath, relativePath, false)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	dir, err := root.Open(workspaceRootName(resolved.RelativePath))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrWorkspaceDirectoryExpected
	}

	items, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	entries := make([]WorkspaceEntry, 0, len(items))
	for _, item := range items {
		if isWorkspaceTransientEntry(item.Name()) {
			continue
		}
		itemInfo, infoErr := item.Info()
		if infoErr != nil {
			return nil, infoErr
		}
		childRelative := joinWorkspaceRelative(resolved.RelativePath, item.Name())
		entries = append(entries, buildWorkspaceEntry(childRelative, itemInfo))
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

func (s *workspaceFileService) Preview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspacePreview, error) {
	resolved, file, info, err := s.openExistingFile(ctx, scope, relativePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	kind, contentType, previewable, maxBytes := workspacePreviewKind(info.Name(), info.Size())
	downloadURL := workspaceDownloadURL(scope.InstanceID, resolved.RelativePath)
	if !previewable {
		return &WorkspacePreview{
			Kind:        "binary",
			ContentType: "application/octet-stream",
			DownloadURL: downloadURL,
		}, nil
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, ErrWorkspacePreviewTooLarge
	}
	if kind == "text" {
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
		PreviewURL:  workspacePreviewURL(scope.InstanceID, resolved.RelativePath),
		DownloadURL: downloadURL,
	}, nil
}

func (s *workspaceFileService) OpenPreview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	file, info, contentType, err := s.openPreviewableFile(ctx, scope, relativePath)
	if err != nil {
		return nil, "", 0, err
	}
	return file, contentType, info.Size(), nil
}

func (s *workspaceFileService) openPreviewableFile(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, os.FileInfo, string, error) {
	_, file, info, err := s.openExistingFile(ctx, scope, relativePath)
	if err != nil {
		return nil, nil, "", err
	}
	_, contentType, previewable, maxBytes := workspacePreviewKind(info.Name(), info.Size())
	if !previewable {
		file.Close()
		return nil, nil, "", ErrWorkspaceFileExpected
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		file.Close()
		return nil, nil, "", ErrWorkspacePreviewTooLarge
	}
	return file, info, contentType, nil
}

func (s *workspaceFileService) OpenDownload(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	resolved, file, info, err := s.openExistingFile(ctx, scope, relativePath)
	if err != nil {
		return nil, "", 0, err
	}
	if err := s.recordAudit(ctx, scope, "download", resolved.RelativePath, info.Size()); err != nil {
		file.Close()
		return nil, "", 0, err
	}
	return file, info.Name(), info.Size(), nil
}

func (s *workspaceFileService) Upload(ctx context.Context, scope WorkspaceFileScope, relativeDir string, filename string, reader io.Reader, size int64) (*WorkspaceEntry, error) {
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
	dir, err := ResolveWorkspacePath(scope.WorkspacePath, relativeDir, false)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dirInfo, err := root.Stat(workspaceRootName(dir.RelativePath))
	if err != nil {
		return nil, err
	}
	if !dirInfo.IsDir() {
		return nil, ErrWorkspaceDirectoryExpected
	}

	targetRelative := joinWorkspaceRelative(dir.RelativePath, cleanName)
	target, err := ResolveWorkspacePath(scope.WorkspacePath, targetRelative, true)
	if err != nil {
		return nil, err
	}
	if err := ensureWorkspaceRuntimeOwnership(scope, dir.RelativePath); err != nil {
		return nil, err
	}
	targetName := workspaceRootName(target.RelativePath)
	if targetInfo, statErr := root.Stat(targetName); statErr == nil && targetInfo.IsDir() {
		return nil, ErrWorkspaceFileExpected
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}

	tempRelative := joinWorkspaceRelative(dir.RelativePath, fmt.Sprintf(".%s.tmp-%d", cleanName, time.Now().UnixNano()))
	tempName := workspaceRootName(tempRelative)
	file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = root.Remove(tempName)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = root.Remove(tempName)
		return nil, closeErr
	}
	if written > maxBytes {
		_ = root.Remove(tempName)
		return nil, ErrWorkspaceUploadTooLarge
	}
	if err := applyWorkspaceRuntimeOwnership(scope, tempRelative, 0640); err != nil {
		_ = root.Remove(tempName)
		return nil, err
	}
	if err := s.recordAudit(ctx, scope, "upload", target.RelativePath, written); err != nil {
		_ = root.Remove(tempName)
		return nil, err
	}
	if err := root.Rename(tempName, targetName); err != nil {
		_ = root.Remove(tempName)
		return nil, err
	}
	info, err := root.Stat(targetName)
	if err != nil {
		return nil, err
	}
	entry := buildWorkspaceEntry(target.RelativePath, info)
	return &entry, nil
}

func (s *workspaceFileService) Mkdir(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspaceEntry, error) {
	if isWorkspaceRootRelative(relativePath) {
		return nil, ErrWorkspaceRootOperation
	}
	resolved, err := ResolveWorkspacePath(scope.WorkspacePath, relativePath, true)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	resolvedName := workspaceRootName(resolved.RelativePath)
	if _, err := root.Lstat(resolvedName); err == nil {
		return nil, ErrWorkspaceEntryExists
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := s.recordAudit(ctx, scope, "mkdir", resolved.RelativePath, 0); err != nil {
		return nil, err
	}
	if err := root.MkdirAll(resolvedName, 0750); err != nil {
		return nil, err
	}
	if err := ensureWorkspaceRuntimeOwnership(scope, resolved.RelativePath); err != nil {
		_ = root.RemoveAll(resolvedName)
		return nil, err
	}
	info, err := root.Stat(resolvedName)
	if err != nil {
		return nil, err
	}
	entry := buildWorkspaceEntry(resolved.RelativePath, info)
	return &entry, nil
}

func (s *workspaceFileService) Rename(ctx context.Context, scope WorkspaceFileScope, oldPath, newPath string) (*WorkspaceEntry, error) {
	if isWorkspaceRootRelative(oldPath) || isWorkspaceRootRelative(newPath) {
		return nil, ErrWorkspaceRootOperation
	}
	oldResolved, err := ResolveWorkspacePath(scope.WorkspacePath, oldPath, false)
	if err != nil {
		return nil, err
	}
	newResolved, err := ResolveWorkspacePath(scope.WorkspacePath, newPath, true)
	if err != nil {
		return nil, err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	oldName := workspaceRootName(oldResolved.RelativePath)
	newName := workspaceRootName(newResolved.RelativePath)
	if _, err := root.Lstat(newName); err == nil {
		return nil, ErrWorkspaceEntryExists
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := root.Lstat(oldName); err != nil {
		return nil, err
	}
	auditPath := oldResolved.RelativePath + " -> " + newResolved.RelativePath
	if err := s.recordAudit(ctx, scope, "rename", auditPath, 0); err != nil {
		return nil, err
	}
	if err := root.Rename(oldName, newName); err != nil {
		return nil, err
	}
	if err := ensureWorkspaceRuntimeOwnership(scope, newResolved.RelativePath); err != nil {
		return nil, err
	}
	info, err := root.Stat(newName)
	if err != nil {
		return nil, err
	}
	entry := buildWorkspaceEntry(newResolved.RelativePath, info)
	return &entry, nil
}

func (s *workspaceFileService) Delete(ctx context.Context, scope WorkspaceFileScope, relativePath string) error {
	if isWorkspaceRootRelative(relativePath) {
		return ErrWorkspaceRootOperation
	}
	resolved, err := ResolveWorkspacePath(scope.WorkspacePath, relativePath, false)
	if err != nil {
		return err
	}
	if err := s.recordAudit(ctx, scope, "delete", resolved.RelativePath, 0); err != nil {
		return err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(workspaceRootName(resolved.RelativePath))
}

func (s *workspaceFileService) openExistingFile(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*ResolvedWorkspacePath, *os.File, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	resolved, err := ResolveWorkspacePath(scope.WorkspacePath, relativePath, false)
	if err != nil {
		return nil, nil, nil, err
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, nil, nil, err
	}
	defer root.Close()
	file, err := root.Open(workspaceRootName(resolved.RelativePath))
	if err != nil {
		return nil, nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, nil, err
	}
	if info.IsDir() {
		file.Close()
		return nil, nil, nil, ErrWorkspaceFileExpected
	}
	return resolved, file, info, nil
}

func (s *workspaceFileService) recordAudit(ctx context.Context, scope WorkspaceFileScope, action, relativePath string, bytes int64) error {
	if s.auditRepo == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.auditRepo.Create(ctx, &models.WorkspaceFileAudit{
		InstanceID:   scope.InstanceID,
		UserID:       scope.UserID,
		Action:       action,
		RelativePath: relativePath,
		Bytes:        bytes,
		CreatedAt:    time.Now().UTC(),
	})
}

func buildWorkspaceEntry(relativePath string, info os.FileInfo) WorkspaceEntry {
	isDir := info.IsDir()
	return WorkspaceEntry{
		Name:         info.Name(),
		Path:         filepath.ToSlash(relativePath),
		IsDir:        isDir,
		Size:         info.Size(),
		ModifiedAt:   info.ModTime().UTC(),
		Previewable:  workspaceEntryPreviewable(info.Name(), info.Size(), isDir),
		Downloadable: !isDir,
	}
}

func isWorkspaceTransientEntry(name string) bool {
	return name == ".tmp" || strings.HasPrefix(name, ".tmp-skill-")
}

func workspaceEntryPreviewable(name string, size int64, isDir bool) bool {
	if isDir {
		return false
	}
	kind, _, previewable, maxBytes := workspacePreviewKind(name, size)
	if !previewable {
		return false
	}
	return kind == "pdf" || maxBytes <= 0 || size <= maxBytes
}

func workspacePreviewKind(name string, size int64) (string, string, bool, int64) {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".log", ".py", ".js", ".ts", ".go", ".sh":
		return "text", "text/plain; charset=utf-8", true, textPreviewMaxBytes
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return "image", contentType, true, imagePreviewMaxBytes
	case ".pdf":
		return "pdf", "application/pdf", true, 0
	default:
		return "binary", "application/octet-stream", false, 0
	}
}

func sanitizeWorkspaceFilename(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", ErrWorkspaceFileNameInvalid
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "\x00") {
		return "", ErrWorkspaceFileNameInvalid
	}
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "", ErrWorkspaceFileNameInvalid
	}
	return cleaned, nil
}

func joinWorkspaceRelative(base, name string) string {
	base = filepath.ToSlash(strings.TrimSpace(base))
	name = filepath.ToSlash(strings.TrimSpace(name))
	if base == "" {
		return path.Clean(name)
	}
	return path.Clean(base + "/" + name)
}

func ensureWorkspaceRuntimeOwnership(scope WorkspaceFileScope, relativePath string) error {
	if _, _, ok := runtimeWorkspaceOwner(scope); !ok {
		return nil
	}
	relative, err := cleanWorkspaceRelativePath(relativePath)
	if err != nil {
		return err
	}
	if relative == "" {
		return nil
	}

	parts := strings.Split(relative, "/")
	for idx := range parts {
		current := strings.Join(parts[:idx+1], "/")
		resolved, err := ResolveWorkspacePath(scope.WorkspacePath, current, false)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolved.RealPath)
		if err != nil {
			return err
		}
		mode := os.FileMode(0640)
		if info.IsDir() {
			mode = 0750
		}
		if err := applyWorkspaceRuntimeOwnershipToPath(scope, resolved.RealPath, mode); err != nil {
			return err
		}
	}
	return nil
}

func applyWorkspaceRuntimeOwnership(scope WorkspaceFileScope, relativePath string, mode os.FileMode) error {
	if _, _, ok := runtimeWorkspaceOwner(scope); !ok {
		return nil
	}
	resolved, err := ResolveWorkspacePath(scope.WorkspacePath, relativePath, false)
	if err != nil {
		return err
	}
	return applyWorkspaceRuntimeOwnershipToPath(scope, resolved.RealPath, mode)
}

func applyWorkspaceRuntimeOwnershipToPath(scope WorkspaceFileScope, targetPath string, mode os.FileMode) error {
	uid, gid, ok := runtimeWorkspaceOwner(scope)
	if !ok {
		return nil
	}
	if err := os.Chown(targetPath, uid, gid); err != nil {
		return err
	}
	if err := os.Chmod(targetPath, mode); err != nil {
		return err
	}
	return nil
}

func runtimeWorkspaceOwner(scope WorkspaceFileScope) (int, int, bool) {
	if scope.InstanceID <= 0 || scope.UserID <= 0 {
		return 0, 0, false
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(scope.WorkspacePath)))
	parts := strings.Split(strings.Trim(cleaned, "/"), "/")
	if len(parts) < 3 {
		return 0, 0, false
	}
	instancePart := fmt.Sprintf("instance-%d", scope.InstanceID)
	userPart := fmt.Sprintf("user-%d", scope.UserID)
	runtimeType := parts[len(parts)-3]
	if parts[len(parts)-1] != instancePart || parts[len(parts)-2] != userPart {
		return 0, 0, false
	}
	if runtimeType != RuntimeTypeOpenClaw && runtimeType != RuntimeTypeHermes {
		return 0, 0, false
	}
	linuxID := RuntimeLinuxID(scope.InstanceID)
	return linuxID, linuxID, true
}

func isWorkspaceRootRelative(relativePath string) bool {
	cleaned, err := cleanWorkspaceRelativePath(relativePath)
	return err == nil && cleaned == ""
}

func openWorkspaceRoot(workspacePath string) (*os.Root, error) {
	resolved, err := ResolveWorkspacePath(workspacePath, "", false)
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(resolved.Root)
}

func workspaceRootName(relativePath string) string {
	relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
	if relativePath == "" {
		return "."
	}
	return filepath.FromSlash(relativePath)
}

func workspaceDownloadURL(instanceID int, relativePath string) string {
	return fmt.Sprintf("/api/v1/instances/%d/workspace/download?path=%s", instanceID, url.QueryEscape(filepath.ToSlash(relativePath)))
}

func workspacePreviewURL(instanceID int, relativePath string) string {
	return fmt.Sprintf("/api/v1/instances/%d/workspace/preview?path=%s&raw=1", instanceID, url.QueryEscape(filepath.ToSlash(relativePath)))
}
