package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrWorkspaceTextConflict = errors.New("file changed since opening; reload before saving")
var ErrWorkspaceTextUnsupported = errors.New("only regular UTF-8 text files up to 1 MiB can be edited; links are not supported")

// Optional capability: existing shared/team implementations do not gain writes.
type WorkspaceTextService interface {
	ReadText(context.Context, WorkspaceFileScope, string) (*WorkspaceText, error)
	SaveText(context.Context, WorkspaceFileScope, string, string, string) (*WorkspaceText, error)
}
type WorkspaceText struct {
	Text    string `json:"text"`
	Version string `json:"version"`
}

func workspaceTextName(name string) bool {
	name = path.Base(name)
	lower := strings.ToLower(name)
	if lower == ".env" || strings.HasPrefix(lower, ".env.") || lower == "dockerfile" || lower == "makefile" {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".log", ".py", ".js", ".ts", ".go", ".sh", ".toml", ".ini", ".conf", ".cfg":
		return true
	}
	return false
}
func validWorkspaceText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}
func textVersion(data []byte, info os.FileInfo) string {
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(fmt.Sprintf("%d:%d:", info.ModTime().UnixNano(), info.Mode())), data...)))
}

// Pin the parent directory and refuse symlinks on the entire editing path.
func openTextParent(scope WorkspaceFileScope, relative string) (*os.Root, string, error) {
	rel, err := cleanWorkspaceRelativePath(relative)
	if err != nil {
		return nil, "", err
	}
	if rel == "" || !workspaceTextName(path.Base(rel)) {
		return nil, "", ErrWorkspaceTextUnsupported
	}
	root, err := openWorkspaceRoot(scope.WorkspacePath)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		st, e := root.Lstat(part)
		if e != nil {
			root.Close()
			return nil, "", e
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", ErrWorkspaceTextUnsupported
		}
		next, e := root.OpenRoot(part)
		root.Close()
		if e != nil {
			return nil, "", e
		}
		root = next
	}
	return root, parts[len(parts)-1], nil
}
func readTextAt(root *os.Root, name string) (*WorkspaceText, os.FileInfo, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || workspaceHasMultipleLinks(st) {
		return nil, nil, ErrWorkspaceTextUnsupported
	}
	if st.Size() > textPreviewMaxBytes {
		return nil, nil, ErrWorkspacePreviewTooLarge
	}
	file, err := root.OpenFile(name, os.O_RDONLY|workspaceTextOpenFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(st, actual) {
		return nil, nil, ErrWorkspaceTextConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, textPreviewMaxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if len(data) > int(textPreviewMaxBytes) {
		return nil, nil, ErrWorkspacePreviewTooLarge
	}
	if !validWorkspaceText(string(data)) {
		return nil, nil, ErrWorkspaceTextUnsupported
	}
	if !actual.ModTime().Equal(after.ModTime()) || actual.Size() != after.Size() {
		return nil, nil, ErrWorkspaceTextConflict
	}
	return &WorkspaceText{Text: string(data), Version: textVersion(data, after)}, after, nil
}
func (s *workspaceFileService) ReadText(ctx context.Context, scope WorkspaceFileScope, relative string) (*WorkspaceText, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, name, err := openTextParent(scope, relative)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	doc, _, err := readTextAt(root, name)
	return doc, err
}
func (s *workspaceFileService) saveText(ctx context.Context, scope WorkspaceFileScope, relative, text, version string) (*WorkspaceText, error) {
	if len(text) > int(textPreviewMaxBytes) {
		return nil, ErrWorkspacePreviewTooLarge
	}
	if !validWorkspaceText(text) {
		return nil, ErrWorkspaceTextUnsupported
	}
	if version == "" {
		return nil, ErrWorkspaceTextConflict
	}
	root, name, err := openTextParent(scope, relative)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	unlock, err := lockWorkspaceEditor(ctx, root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	old, info, err := readTextAt(root, name)
	if err != nil {
		return nil, err
	}
	if old.Version != version {
		return nil, ErrWorkspaceTextConflict
	}
	temp := fmt.Sprintf(".clawmanager-edit-%d", time.Now().UnixNano())
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer root.Remove(temp)
	if err = preserveWorkspaceTextOwner(file, info); err == nil {
		err = file.Chmod(info.Mode().Perm())
	}
	if err == nil {
		_, err = file.WriteString(text)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// External writers may not honor our lock. Check again immediately before replace.
	current, _, err := readTextAt(root, name)
	if err != nil {
		return nil, err
	}
	if current.Version != version {
		return nil, ErrWorkspaceTextConflict
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = s.recordAudit(ctx, scope, "edit_attempt", relative, int64(len(text))); err != nil {
		return nil, err
	}
	if err = root.Rename(temp, name); err != nil {
		return nil, err
	}
	doc, _, err := readTextAt(root, name)
	// A non-cooperating writer may replace the file after our commit. Never acknowledge
	// its different contents as the user's successfully saved document.
	if err == nil && doc.Text != text {
		return nil, ErrWorkspaceTextConflict
	}
	return doc, err
}
