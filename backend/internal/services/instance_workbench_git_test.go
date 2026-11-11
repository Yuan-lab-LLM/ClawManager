package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"clawreef/internal/workbench"
)

type gitRepairAgent struct {
	changes []map[string]any
	calls   []string
	diff    string
	deny    bool
}

func (a *gitRepairAgent) Workbench(_ context.Context, _ string, req WorkbenchRequest) (json.RawMessage, error) {
	var r struct {
		Operation string `json:"operation"`
	}
	_ = json.Unmarshal(req.Data, &r)
	a.calls = append(a.calls, r.Operation)
	if a.deny {
		return nil, errors.New("not registered")
	}
	if r.Operation == "status" {
		return json.Marshal(map[string]any{"project": map[string]any{"path": "repo"}, "status": map[string]any{"branch": "main", "changes": a.changes}, "extra": "preserved"})
	}
	return json.Marshal(map[string]any{"diff": a.diff, "extra": "preserved"})
}
func gitRepairService(t *testing.T, root string, a *gitRepairAgent) (*InstanceWorkbenchService, workbenchTarget) {
	t.Helper()
	s := &InstanceWorkbenchService{agent: a}
	setter, ok := any(s).(interface{ SetGitFiles(WorkspaceFileService) })
	if !ok {
		t.Fatal("missing guarded Git file wiring")
	}
	setter.SetGitFiles(NewWorkspaceFileService(nil))
	return s, workbenchTarget{scope: workbench.PreviewScope{UserID: 34, InstanceID: 12, Generation: 3, GatewayID: "gw"}, workspace: root}
}
func gitRepairExecute(t *testing.T, s *InstanceWorkbenchService, target workbenchTarget, request any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(request)
	r, e := s.execute(context.Background(), target, "git", b)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal(r, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestWorkbenchGitUntrackedCountsDiffBinaryAndBudget(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "repo"), 0700)
	cases := []struct {
		name, text string
		lines      int
		binary     bool
	}{{"new", "中文\nlast", 2, false}, {"empty", "", 0, false}, {"newline.txt", "one\n", 1, false}, {"photo.png", "\x89PNG\x00", 0, true}}
	a := &gitRepairAgent{}
	for _, c := range cases {
		if e := os.WriteFile(filepath.Join(root, "repo", c.name), []byte(c.text), 0600); e != nil {
			t.Fatal(e)
		}
		a.changes = append(a.changes, map[string]any{"path": c.name, "untracked": true, "additions": 0, "deletions": 0})
	}
	a.changes = append(a.changes, map[string]any{"path": "mixed.txt", "staged": true, "worktree_status": "M", "additions": 99, "deletions": 88})
	s, target := gitRepairService(t, root, a)
	out := gitRepairExecute(t, s, target, map[string]any{"operation": "status", "path": "repo"})
	changes := out["status"].(map[string]any)["changes"].([]any)
	for i, c := range cases {
		ch := changes[i].(map[string]any)
		if ch["binary"] != c.binary || ch["stats_known"] != !c.binary {
			t.Fatalf("%s flags: %+v", c.name, ch)
		}
		if !c.binary && ch["additions"] != float64(c.lines) {
			t.Fatalf("%s counts: %+v", c.name, ch)
		}
	}
	if changes[4].(map[string]any)["stats_known"] != false || out["extra"] != "preserved" {
		t.Fatal(out)
	}
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "diff", "path": "repo", "paths": []string{"new"}})
	if !strings.Contains(out["diff"].(string), "@@ -0,0 +1,2 @@\n+中文\n+last\n\\ No newline at end of file\n") {
		t.Fatal(out)
	}
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "diff", "path": "repo", "paths": []string{"photo.png"}})
	if out["binary"] != true || out["preview_path"] != "repo/photo.png" {
		t.Fatal(out)
	}
	a.diff = "Binary files a/photo.png and b/photo.png differ\n"
	a.changes[3]["untracked"] = false
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "diff", "path": "repo", "paths": []string{"photo.png"}, "staged": true})
	if out["diff"] != a.diff || out["binary"] != true || out["preview_path"] != nil {
		t.Fatal("staged preview misrepresented", out)
	}
	// Thousands of files never produce unbounded per-poll reads.
	a.changes = nil
	for i := 0; i < 1000; i++ {
		a.changes = append(a.changes, map[string]any{"path": fmt.Sprintf("missing-%d", i), "untracked": true})
	}
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "status", "path": "repo"})
	if len(out["status"].(map[string]any)["changes"].([]any)) != 1000 {
		t.Fatal("dropped status rows")
	}
}
func TestWorkbenchGitIdentitySavesRepositoryAndCommits(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("git", "init", repo)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("init %v %s", e, b)
	}
	p := filepath.Join(repo, ".git", "config")
	old, _ := os.ReadFile(p)
	old = append(old, []byte("\n# preserve bytes\n[remote \"origin\"]\n\turl = https://example.test/a.git\n")...)
	_ = os.WriteFile(p, old, 0600)
	a := &gitRepairAgent{}
	s, target := gitRepairService(t, root, a)
	req := map[string]any{"operation": "set-identity", "path": "repo", "name": "A \\\" person", "email": "a@example.test"}
	out := gitRepairExecute(t, s, target, req)
	identity := out["identity"].(map[string]any)
	if identity["name"] != req["name"] || identity["scope"] != "repository" {
		t.Fatal(out)
	}
	data, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(data), string(old)) {
		t.Fatal("unrelated config changed")
	}
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "identity", "path": "repo"})
	if out["identity"].(map[string]any)["name"] != req["name"] {
		t.Fatal(out)
	}
	c := exec.Command("git", "-C", repo, "-c", "user.useConfigOnly=true", "-c", "commit.gpgSign=false", "commit", "--allow-empty", "-m", "identity verified")
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+root)
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("saved identity cannot commit: %v %s", e, b)
	}
	for _, name := range []string{"bad\nname", "bad<name>", "bad\x00name"} {
		req["name"] = name
		b, _ := json.Marshal(req)
		if _, e := s.execute(context.Background(), target, "git", b); e == nil {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	before, _ := os.ReadFile(p)
	a.deny = true
	req["name"] = "valid"
	b, _ := json.Marshal(req)
	if _, e := s.execute(context.Background(), target, "git", b); e == nil {
		t.Fatal("identity bypassed registration")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("denied request wrote config")
	}
}
func TestWorkbenchGitIdentityRejectsUnsupportedAndSymlinks(t *testing.T) {
	for _, config := range []string{"[extensions]\n worktreeConfig = true\n", "[include]\n path = other\n", "[includeIf \"gitdir:/tmp\"]\n path=other\n"} {
		root := t.TempDir()
		_ = os.MkdirAll(filepath.Join(root, "repo/.git"), 0700)
		_ = os.WriteFile(filepath.Join(root, "repo/.git/config"), []byte(config), 0600)
		s, target := gitRepairService(t, root, &gitRepairAgent{})
		r, _ := json.Marshal(map[string]any{"operation": "set-identity", "path": "repo", "name": "Person", "email": "p@example.test"})
		if _, e := s.execute(context.Background(), target, "git", r); e == nil {
			t.Fatal("accepted unsupported config", config)
		}
	}
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "repo/.git"), 0700)
	outside := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(outside, []byte("[core]\n bare=false\n"), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "repo/.git/config"))
	s, target := gitRepairService(t, root, &gitRepairAgent{})
	r, _ := json.Marshal(map[string]any{"operation": "set-identity", "path": "repo", "name": "Person", "email": "p@example.test"})
	if _, e := s.execute(context.Background(), target, "git", r); e == nil {
		t.Fatal("accepted config symlink")
	}
}

func TestWorkbenchGitConfigValuesAndRepeatedSaves(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{` " A " # comment`, " A "}, {`"A\\B\" C"`, "A\\B\" C"}, {` Plain Person  ; note`, "Plain Person"}} {
		got, e := gitConfigValue(tc.raw)
		if e != nil || got != tc.want {
			t.Fatalf("value %q = %q, %v; want %q", tc.raw, got, e, tc.want)
		}
	}
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "repo/.git"), 0700)
	_ = os.WriteFile(filepath.Join(root, "repo/.git/config"), []byte("[core]\n bare=false\n"), 0600)
	s, target := gitRepairService(t, root, &gitRepairAgent{})
	req := map[string]any{"operation": "set-identity", "path": "repo", "name": "Person", "email": "p@example.test"}
	gitRepairExecute(t, s, target, req)
	gitRepairExecute(t, s, target, req)
	data, _ := os.ReadFile(filepath.Join(root, "repo/.git/config"))
	if strings.Count(string(data), "[user]") != 1 {
		t.Fatal("repeated saves append duplicate identity blocks")
	}
	_ = os.WriteFile(filepath.Join(root, "repo/.git/config.lock"), nil, 0600)
	b, _ := json.Marshal(req)
	if _, e := s.execute(context.Background(), target, "git", b); e == nil {
		t.Fatal("overwrote config while Git holds config.lock")
	}
}

func TestWorkbenchGitSingleSideStatsAndBinaryDiffDetection(t *testing.T) {
	a := &gitRepairAgent{changes: []map[string]any{
		{"path": "staged.txt", "staged": true, "worktree_status": " ", "additions": 8, "deletions": 2},
		{"path": "working.txt", "staged": false, "worktree_status": "M", "additions": 4, "deletions": 1},
		{"path": "mixed.txt", "staged": true, "worktree_status": "M", "additions": 12, "deletions": 3},
		{"path": "zero.dat", "staged": true, "worktree_status": " ", "additions": 0, "deletions": 0},
	}, diff: "diff --git a/source b/source\n--- a/source\n+++ b/source\n@@ -0,0 +1 @@\n+GIT binary patch\n"}
	s, target := gitRepairService(t, t.TempDir(), a)
	out := gitRepairExecute(t, s, target, map[string]any{"operation": "status", "path": "repo"})
	changes := out["status"].(map[string]any)["changes"].([]any)
	if changes[0].(map[string]any)["staged_additions"] != float64(8) || changes[1].(map[string]any)["worktree_additions"] != float64(4) || changes[2].(map[string]any)["stats_known"] != false || changes[3].(map[string]any)["stats_known"] != false {
		t.Fatal(out)
	}
	out = gitRepairExecute(t, s, target, map[string]any{"operation": "diff", "path": "repo", "paths": []string{"source"}})
	if out["binary"] != false {
		t.Fatal("ordinary patch content marked binary", out)
	}
}

type gitBudgetFiles struct {
	WorkspaceFileService
	calls int
	data  []byte
}

func (s *gitBudgetFiles) ReadGitFile(context.Context, WorkspaceFileScope, string) ([]byte, error) {
	s.calls++
	return s.data, nil
}
func TestWorkbenchGitReadBudgetAndTextGuard(t *testing.T) {
	a := &gitRepairAgent{}
	for i := 0; i < 1000; i++ {
		a.changes = append(a.changes, map[string]any{"path": fmt.Sprint(i), "untracked": true})
	}
	s, target := gitRepairService(t, t.TempDir(), a)
	files := &gitBudgetFiles{WorkspaceFileService: NewWorkspaceFileService(nil)}
	s.SetGitFiles(files)
	gitRepairExecute(t, s, target, map[string]any{"operation": "status", "path": "repo"})
	if files.calls != 16 {
		t.Fatalf("unbounded reads %d", files.calls)
	}
	files.calls = 0
	files.data = []byte(strings.Repeat("x", 700000))
	gitRepairExecute(t, s, target, map[string]any{"operation": "status", "path": "repo"})
	if files.calls != 2 {
		t.Fatalf("aggregate byte budget ignored %d", files.calls)
	}
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "config"), []byte("[core]\n bare=false\n"), 0600)
	guard := WithWorkspaceUpgradeGuard(NewWorkspaceFileService(nil), upgradeBlockedRepo{failure: ErrWorkspaceUpgradeBusy}, false).(WorkspaceTextService)
	doc, e := guard.ReadText(context.Background(), workspaceTestScope(root), "config")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = guard.SaveText(context.Background(), workspaceTestScope(root), "config", "changed", doc.Version); !errors.Is(e, ErrWorkspaceUpgradeBusy) {
		t.Fatalf("text save bypassed upgrade guard: %v", e)
	}
}

type gitConflictFiles struct {
	WorkspaceFileService
	root string
}

func (s gitConflictFiles) ReadText(ctx context.Context, scope WorkspaceFileScope, p string) (*WorkspaceText, error) {
	return s.WorkspaceFileService.(WorkspaceTextService).ReadText(ctx, scope, p)
}
func (s gitConflictFiles) SaveText(ctx context.Context, scope WorkspaceFileScope, p, text, version string) (*WorkspaceText, error) {
	existing, e := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(p)))
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(s.root, filepath.FromSlash(p)), append(existing, []byte("# concurrent writer\n")...), 0600); e != nil {
		return nil, e
	}
	return s.WorkspaceFileService.(WorkspaceTextService).SaveText(ctx, scope, p, text, version)
}
func TestWorkbenchGitIdentityVersionConflictPreservesOtherWriter(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "repo/.git"), 0700)
	_ = os.WriteFile(filepath.Join(root, "repo/.git/config"), []byte("[core]\n bare=false\n"), 0600)
	s, target := gitRepairService(t, root, &gitRepairAgent{})
	s.SetGitFiles(gitConflictFiles{WorkspaceFileService: NewWorkspaceFileService(nil), root: root})
	r, _ := json.Marshal(map[string]any{"operation": "set-identity", "path": "repo", "name": "Person", "email": "p@example.test"})
	_, e := s.execute(context.Background(), target, "git", r)
	var operation *WorkbenchOperationError
	if !errors.As(e, &operation) || operation.Code != "identity_conflict" {
		t.Fatalf("missing clear conflict: %v", e)
	}
	data, _ := os.ReadFile(filepath.Join(root, "repo/.git/config"))
	if !strings.HasSuffix(string(data), "# concurrent writer\n") || strings.Contains(string(data), "[user]") {
		t.Fatal("overwrote concurrent config", string(data))
	}
}

func TestWorkbenchGitPathNeverNormalizesToDifferentFile(t *testing.T) {
	for _, tc := range [][2]string{{" repo ", "file"}, {"repo", " file "}, {"repo", `a\b`}, {"../repo", "file"}, {"repo", "../file"}} {
		if p, e := gitWorkspacePath(tc[0], tc[1]); e == nil {
			t.Fatalf("unsafe normalization %q %q to %q", tc[0], tc[1], p)
		}
	}
}

func TestWorkbenchGitIdentityPreservesExternallyEditedManagedBlock(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if out, e := exec.Command("git", "init", repo).CombinedOutput(); e != nil {
		t.Fatalf("init %s %v", out, e)
	}
	s, target := gitRepairService(t, root, &gitRepairAgent{})
	req := map[string]any{"operation": "set-identity", "path": "repo", "name": "Person", "email": "person@example.test"}
	gitRepairExecute(t, s, target, req)
	config := filepath.Join(repo, ".git/config")
	for _, args := range [][]string{{"--unset", "user.email"}, {"--add", "user.signingkey", "KEY-ID"}} {
		cmd := exec.Command("git", append([]string{"config", "--file", config}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("external config %s %v", out, e)
		}
	}
	original, e := os.ReadFile(config)
	if e != nil {
		t.Fatal(e)
	}
	req["email"] = "new@example.test"
	gitRepairExecute(t, s, target, req)
	updated, e := os.ReadFile(config)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(string(updated), string(original)) {
		t.Fatal("removed unrelated externally modified config bytes")
	}
	if out, e := exec.Command("git", "config", "--file", config, "--get", "user.signingkey").CombinedOutput(); e != nil || strings.TrimSpace(string(out)) != "KEY-ID" {
		t.Fatalf("lost signing key %q %v", out, e)
	}
}
func TestWorkbenchGitImageFormatsMatchWorkspacePreview(t *testing.T) {
	for _, ext := range []string{"png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "svg"} {
		name := "asset." + ext
		kind, _, previewable, _ := workspacePreviewKind(name, 1)
		if gitImagePath(name) != (kind == "image" && previewable) {
			t.Fatalf("Git advertises unsupported image %s", name)
		}
	}
}
