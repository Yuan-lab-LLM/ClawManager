package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// WorkspaceGitFileReader is read-only and independent of the text editor's filename allowlist.
// The NFS implementation keeps credential checks and the byte limit inside the helper.
type WorkspaceGitFileReader interface {
	ReadGitFile(context.Context, WorkspaceFileScope, string) ([]byte, error)
}

func (s *workspaceFileService) ReadGitFile(ctx context.Context, scope WorkspaceFileScope, relative string) ([]byte, error) {
	if s.useNFSIO(scope) {
		r, _, e := s.nfsIO(ctx, scope, workspaceNFSRequest{Action: "git_read", Path: relative}, nil)
		return r.GitData, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	root, name, e := openWorkspaceFileParent(scope, relative)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	st, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || workspaceHasMultipleLinks(st) {
		return nil, ErrWorkspaceTextUnsupported
	}
	if st.Size() > textPreviewMaxBytes {
		return nil, ErrWorkspacePreviewTooLarge
	}
	f, e := root.OpenFile(name, os.O_RDONLY|workspaceTextOpenFlags, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if e = validateWorkspacePrivateInode(scope, actual); e != nil {
		return nil, e
	}
	if !os.SameFile(st, actual) {
		return nil, ErrWorkspaceTextConflict
	}
	data, e := io.ReadAll(io.LimitReader(f, textPreviewMaxBytes+1))
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if len(data) > int(textPreviewMaxBytes) {
		return nil, ErrWorkspacePreviewTooLarge
	}
	if actual.Size() != after.Size() || !actual.ModTime().Equal(after.ModTime()) {
		return nil, ErrWorkspaceTextConflict
	}
	return data, nil
}
func (s *guardedWorkspaceFileService) ReadGitFile(ctx context.Context, scope WorkspaceFileScope, relative string) ([]byte, error) {
	reader, ok := s.WorkspaceFileService.(WorkspaceGitFileReader)
	if !ok {
		return nil, ErrWorkspaceTextUnsupported
	}
	return reader.ReadGitFile(ctx, scope, relative)
}
func (s *guardedWorkspaceFileService) ReadText(ctx context.Context, scope WorkspaceFileScope, relative string) (*WorkspaceText, error) {
	reader, ok := s.WorkspaceFileService.(WorkspaceTextService)
	if !ok {
		return nil, ErrWorkspaceTextUnsupported
	}
	return reader.ReadText(ctx, scope, relative)
}
func (s *guardedWorkspaceFileService) SaveText(ctx context.Context, scope WorkspaceFileScope, relative, text, version string) (*WorkspaceText, error) {
	release, e := s.acquire(ctx, scope)
	if e != nil {
		return nil, e
	}
	defer release()
	writer, ok := s.WorkspaceFileService.(WorkspaceTextService)
	if !ok {
		return nil, ErrWorkspaceTextUnsupported
	}
	return writer.SaveText(ctx, scope, relative, text, version)
}
func (s *InstanceWorkbenchService) SetGitFiles(files WorkspaceFileService) { s.gitFiles = files }
func gitWorkbenchError(code, message string) error {
	return &WorkbenchOperationError{Code: code, Message: message}
}
func gitWorkspacePath(project, file string) (string, error) {
	if len(project) > 4096 || len(file) > 4096 || strings.ContainsAny(project+file, "\x00\r\n\\") || strings.TrimSpace(project) != project || strings.TrimSpace(file) != file {
		return "", ErrWorkspacePathInvalid
	}
	// Validate independently before joining: a file cannot escape its selected repository.
	p, e := cleanWorkspaceRelativePath(project)
	if e != nil {
		return "", e
	}
	f, e := cleanWorkspaceRelativePath(file)
	if e != nil {
		return "", e
	}
	return path.Join(p, f), nil
}

type gitWorkbenchRequest struct {
	Operation string   `json:"operation"`
	Path      string   `json:"path"`
	Paths     []string `json:"paths"`
	Staged    bool     `json:"staged"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
}

func (s *InstanceWorkbenchService) gitAgent(ctx context.Context, target workbenchTarget, req any) (map[string]any, error) {
	data, e := json.Marshal(req)
	if e != nil {
		return nil, e
	}
	raw, e := s.executeAgent(ctx, target, "git", data)
	if e != nil {
		return nil, e
	}
	var result map[string]any
	if e = json.Unmarshal(raw, &result); e != nil || result == nil {
		return nil, ErrWorkbenchUnavailable
	}
	return result, nil
}
func (s *InstanceWorkbenchService) executeGit(ctx context.Context, target workbenchTarget, data json.RawMessage) (json.RawMessage, error) {
	var req gitWorkbenchRequest
	if len(data) > 1<<20 || json.Unmarshal(data, &req) != nil {
		return nil, gitWorkbenchError("invalid_request", "Invalid Git request.")
	}
	ctx, cancel := context.WithTimeout(ctx, 125*time.Second)
	defer cancel()
	if req.Operation == "identity" || req.Operation == "set-identity" {
		// Validate the registered project in the running sandbox before touching its metadata.
		if _, e := s.gitAgent(ctx, target, map[string]any{"operation": "status", "path": req.Path}); e != nil {
			return nil, e
		}
		result, e := s.gitIdentity(ctx, target, req)
		if e != nil {
			return nil, e
		}
		return json.Marshal(result)
	}
	if req.Operation == "diff" && s.gitFiles != nil && len(req.Paths) == 1 {
		status, e := s.gitAgent(ctx, target, map[string]any{"operation": "status", "path": req.Path})
		if e != nil {
			return nil, e
		}
		if !req.Staged {
			for _, change := range gitChanges(status) {
				if change["path"] == req.Paths[0] && change["untracked"] == true {
					return s.gitUntrackedDiff(ctx, target, req)
				}
			}
		}
	}
	result, e := s.gitAgent(ctx, target, json.RawMessage(data))
	if e != nil {
		return nil, e
	}
	if result["status"] != nil {
		s.gitStatus(ctx, target, req.Path, result)
	}
	if req.Operation == "diff" {
		diff, _ := result["diff"].(string)
		binary := strings.HasPrefix(diff, "Binary files ") || strings.Contains(diff, "\nBinary files ") || strings.HasPrefix(diff, "GIT binary patch\n") || strings.Contains(diff, "\nGIT binary patch\n")
		result["binary"] = binary
		if binary && !req.Staged && len(req.Paths) == 1 {
			if p, e := gitWorkspacePath(req.Path, req.Paths[0]); e == nil && gitImagePath(p) {
				result["preview_path"] = p
			}
		}
	}
	return json.Marshal(result)
}
func gitChanges(response map[string]any) []map[string]any {
	status, _ := response["status"].(map[string]any)
	raw, _ := status["changes"].([]any)
	changes := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if c, ok := r.(map[string]any); ok {
			changes = append(changes, c)
		}
	}
	return changes
}
func gitScope(target workbenchTarget) WorkspaceFileScope {
	return WorkspaceFileScope{InstanceID: target.scope.InstanceID, UserID: target.scope.UserID, RuntimeGeneration: target.scope.Generation, WorkspacePath: target.workspace}
}
func gitLineCount(text string) int {
	n := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}
func gitImagePath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}
func (s *InstanceWorkbenchService) gitStatus(ctx context.Context, target workbenchTarget, project string, result map[string]any) {
	reader, ok := s.gitFiles.(WorkspaceGitFileReader)
	// Bounded polling: at most 16 file opens and 1 MiB accepted input per response.
	// Runtime sums are exact for a single nonzero side; mixed and zero/binary counts stay unknown.
	remaining, reads := int(textPreviewMaxBytes), 0
	for _, c := range gitChanges(result) {
		c["stats_known"] = false
		c["binary"] = false
		if c["untracked"] != true {
			additions, _ := c["additions"].(float64)
			deletions, _ := c["deletions"].(float64)
			staged := c["staged"] == true
			worktree, _ := c["worktree_status"].(string)
			if (additions > 0 || deletions > 0) && !(staged && strings.TrimSpace(worktree) != "") {
				side := "worktree"
				if staged {
					side = "staged"
				}
				c[side+"_additions"] = additions
				c[side+"_deletions"] = deletions
				c["stats_known"] = true
			}
		}
		if !ok || c["untracked"] != true || reads >= 16 || remaining <= 0 {
			continue
		}
		reads++
		file, _ := c["path"].(string)
		p, e := gitWorkspacePath(project, file)
		if e != nil {
			continue
		}
		b, e := reader.ReadGitFile(ctx, gitScope(target), p)
		if e != nil {
			continue
		}
		remaining -= len(b)
		if remaining < 0 {
			continue
		}
		binary := !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0
		c["binary"] = binary
		if binary {
			continue
		}
		n := gitLineCount(string(b))
		c["additions"] = n
		c["deletions"] = 0
		c["worktree_additions"] = n
		c["worktree_deletions"] = 0
		c["stats_known"] = true
	}
}
func (s *InstanceWorkbenchService) gitUntrackedDiff(ctx context.Context, target workbenchTarget, req gitWorkbenchRequest) (json.RawMessage, error) {
	reader, ok := s.gitFiles.(WorkspaceGitFileReader)
	if !ok {
		return nil, gitWorkbenchError("unsupported", "Bounded Git file previews are unavailable.")
	}
	p, e := gitWorkspacePath(req.Path, req.Paths[0])
	if e != nil {
		return nil, e
	}
	b, e := reader.ReadGitFile(ctx, gitScope(target), p)
	if e != nil {
		if errors.Is(e, ErrWorkspacePreviewTooLarge) {
			return json.Marshal(map[string]any{"truncated": true, "diff": ""})
		}
		return nil, e
	}
	result := map[string]any{"binary": !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0}
	if result["binary"] == true {
		if gitImagePath(p) {
			result["preview_path"] = p
		}
		return json.Marshal(result)
	}
	// Quoted headers preserve paths containing tabs, quotes, or other unusual bytes.
	quote := func(s string) string {
		if strings.ContainsAny(s, "\t\r\n\"\\") {
			return strconv.Quote(s)
		}
		return s
	}
	file := req.Paths[0]
	var out strings.Builder
	fmt.Fprintf(&out, "diff --git %s %s\nnew file mode 100644\n--- /dev/null\n+++ %s\n", quote("a/"+file), quote("b/"+file), quote("b/"+file))
	text := string(b)
	n := gitLineCount(text)
	if n > 0 {
		fmt.Fprintf(&out, "@@ -0,0 +1,%d @@\n", n)
		for _, line := range strings.SplitAfter(text, "\n") {
			if line == "" {
				continue
			}
			out.WriteByte('+')
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	diff := out.String()
	if len(diff) > 256<<10 {
		diff = diff[:256<<10]
		for !utf8.ValidString(diff) {
			diff = diff[:len(diff)-1]
		}
		result["truncated"] = true
	}
	result["diff"] = diff
	return json.Marshal(result)
}

type gitRepositoryIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Scope string `json:"scope"`
}

func validGitAuthor(value string, email bool) bool {
	if value == "" || len(value) > 254 || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "<>") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
		if email && unicode.IsSpace(r) {
			return false
		}
	}
	if email && (!strings.Contains(value, "@") || strings.HasPrefix(value, "@") || strings.HasSuffix(value, "@")) {
		return false
	}
	return true
}
func gitConfigValue(raw string) (string, error) {
	var out strings.Builder
	quoted, escaped, end := false, false, 0
	for _, r := range strings.TrimSpace(raw) {
		if escaped {
			switch r {
			case '\\', '"':
				out.WriteRune(r)
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'b':
				out.WriteByte('\b')
			default:
				return "", ErrWorkspaceTextUnsupported
			}
			escaped = false
			end = out.Len()
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && (r == '#' || r == ';') {
			break
		}
		whitespace := r == ' ' || r == '\t'
		if !quoted && whitespace && out.Len() == 0 {
			continue
		}
		out.WriteRune(r)
		if quoted || !whitespace {
			end = out.Len()
		}
	}
	if escaped || quoted {
		return "", ErrWorkspaceTextUnsupported
	}
	return out.String()[:end], nil
}
func gitConfigIdentity(config string) (gitRepositoryIdentity, error) {
	identity := gitRepositoryIdentity{Scope: "repository"}
	section := ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				return identity, ErrWorkspaceTextUnsupported
			}
			trailing := strings.TrimSpace(line[end+1:])
			if trailing != "" && !strings.HasPrefix(trailing, "#") && !strings.HasPrefix(trailing, ";") {
				return identity, ErrWorkspaceTextUnsupported
			}
			header := strings.TrimSpace(strings.ToLower(line[1:end]))
			section = header
			if strings.HasPrefix(header, "include") {
				return identity, gitWorkbenchError("unsupported_config", "Repository config includes are not supported by the identity editor.")
			}
			continue
		}
		if strings.HasSuffix(line, "\\") {
			return identity, ErrWorkspaceTextUnsupported
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !found {
			key = strings.ToLower(strings.Fields(line)[0])
			value = "true"
		}
		if section == "extensions" && key == "worktreeconfig" {
			v, e := gitConfigValue(value)
			if e != nil {
				return identity, e
			}
			switch strings.ToLower(v) {
			case "false", "no", "off", "0":
			default:
				return identity, gitWorkbenchError("unsupported_config", "Repository worktree config is not supported by the identity editor.")
			}
		}
		if section == "user" && (key == "name" || key == "email") {
			v, e := gitConfigValue(value)
			if e != nil {
				return identity, e
			}
			if key == "name" {
				identity.Name = v
			} else {
				identity.Email = v
			}
		}
	}
	return identity, nil
}
func (s *InstanceWorkbenchService) gitIdentity(ctx context.Context, target workbenchTarget, req gitWorkbenchRequest) (map[string]any, error) {
	writer, ok := s.gitFiles.(WorkspaceTextService)
	if !ok {
		return nil, gitWorkbenchError("unsupported", "Repository identity editing is unavailable.")
	}
	project, e := gitWorkspacePath(req.Path, "")
	if e != nil {
		return nil, e
	}
	if project == "." {
		project = ""
	}
	entries, e := s.gitFiles.List(ctx, gitScope(target), project)
	if e != nil {
		return nil, e
	}
	normal := false
	for _, entry := range entries {
		if entry.Name == ".git" && entry.IsDir {
			normal = true
			break
		}
	}
	if !normal {
		return nil, gitWorkbenchError("unsupported_config", "Identity editing requires a normal .git directory; linked worktrees are not supported.")
	}
	metadata := path.Join(project, ".git")
	entries, e = s.gitFiles.List(ctx, gitScope(target), metadata)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if entry.Name == "config.lock" && req.Operation == "set-identity" {
			return nil, gitWorkbenchError("identity_conflict", "Git is updating repository config; retry after it finishes.")
		}
		if entry.Name == "commondir" {
			return nil, gitWorkbenchError("unsupported_config", "Shared Git metadata is not supported by the identity editor.")
		}
	}
	config := path.Join(metadata, "config")
	doc, e := writer.ReadText(ctx, gitScope(target), config)
	if e != nil {
		return nil, e
	}
	identity, e := gitConfigIdentity(doc.Text)
	if e != nil {
		if req.Operation == "identity" {
			return map[string]any{"identity": gitRepositoryIdentity{Scope: "repository"}}, nil
		}
		var operation *WorkbenchOperationError
		if errors.As(e, &operation) {
			return nil, e
		}
		return nil, gitWorkbenchError("unsupported_config", "Repository config syntax is not supported by the identity editor.")
	}
	if req.Operation == "set-identity" {
		if !validGitAuthor(req.Name, false) || !validGitAuthor(req.Email, true) {
			return nil, gitWorkbenchError("invalid_identity", "Provide a name and email without control characters or angle brackets (up to 254 bytes).")
		}
		// Appending overrides prior local values without rewriting unrelated config bytes.
		quote := func(v string) string { return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(v) + "\"" }
		updated := doc.Text
		const marker = "\n# ClawManager repository identity\n"
		if i := strings.LastIndex(updated, marker); i >= 0 {
			suffix := updated[i+len(marker):]
			canonical := "[user]\n\tname = " + quote(identity.Name) + "\n\temail = " + quote(identity.Email) + "\n"
			if suffix == canonical {
				updated = updated[:i]
			}
		}
		if updated != "" && !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		updated += marker + "[user]\n\tname = " + quote(req.Name) + "\n\temail = " + quote(req.Email) + "\n"
		if _, e = writer.SaveText(ctx, gitScope(target), config, updated, doc.Version); e != nil {
			if errors.Is(e, ErrWorkspaceTextConflict) {
				return nil, gitWorkbenchError("identity_conflict", "Repository config changed; reload identity and retry.")
			}
			return nil, e
		}
		identity = gitRepositoryIdentity{Name: req.Name, Email: req.Email, Scope: "repository"}
	}
	return map[string]any{"identity": identity}, nil
}
