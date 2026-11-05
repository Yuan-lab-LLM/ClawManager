//go:build linux

package services

import (
	"archive/zip"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if handled, err := RunWorkspaceIOHelper(os.Args[1:], os.Stdin, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "workspace helper failed")
			os.Exit(2)
		}
		return
	}
	os.Exit(m.Run())
}

func TestNFSWorkspaceIOPrivateTextAndTransferredDownload(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-workspace-io-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err = os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "opencode", "user-3", "instance-12")
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(root, 200012, 200012); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "private.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(filepath.Join(root, "private.txt"), 200012, 200012); err != nil {
		t.Fatal(err)
	}
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: root}
	writes := &workspaceWriteFake{}
	s := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{}).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	doc, err := s.ReadText(context.Background(), scope, "private.txt")
	if err != nil || doc.Text != "secret" {
		t.Fatalf("read: %#v %v", doc, err)
	}
	doc, err = s.SaveText(context.Background(), scope, "./private.txt", "updated", doc.Version)
	if err != nil || doc.Text != "updated" {
		t.Fatalf("save: %#v %v", doc, err)
	}
	if writes.begins != 1 || writes.completes != 1 || writes.active {
		t.Fatalf("write lifetime not settled: %+v", writes)
	}
	if _, err = s.SaveText(context.Background(), scope, "private.txt", "stale", "old-version"); !errors.Is(err, ErrWorkspaceTextConflict) {
		t.Fatalf("lost conflict enum: %v", err)
	}
	file, name, size, err := s.OpenDownload(context.Background(), scope, "private.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || name != "private.txt" || size != 7 || string(data) != "updated" {
		t.Fatalf("download: %s %d %q %v", name, size, data, err)
	}
	if _, err = s.Upload(context.Background(), scope, "", "new.txt", strings.NewReader("stream"), 6); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upload(context.Background(), scope, "", "short.txt", strings.NewReader("x"), 6); err == nil {
		t.Fatal("accepted truncated upload")
	}
	if _, err = os.Stat(filepath.Join(root, "short.txt")); !os.IsNotExist(err) {
		t.Fatal("truncated upload committed")
	}
	if _, err = s.ReadText(context.Background(), scope, "../private.txt"); !errors.Is(err, ErrWorkspacePathEscape) {
		t.Fatalf("traversal: %v", err)
	}
	bad := scope
	bad.WorkspacePath = filepath.Join(base, "opencode", "user-3", "instance-13")
	if _, err = s.List(context.Background(), bad, ""); err == nil {
		t.Fatal("mismatched instance scope accepted")
	}
	if _, err = s.Mkdir(context.Background(), scope, "folder"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upload(context.Background(), scope, "folder", "entry.txt", strings.NewReader("zip me"), -1); err != nil {
		t.Fatal(err)
	}
	archive, name, _, err := s.OpenDownload(context.Background(), scope, "folder")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile, ok := archive.(*os.File)
	if !ok {
		t.Fatal("archive was not passed as descriptor")
	}
	defer archiveFile.Close()
	info, err := archiveFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(archiveFile, info.Size())
	if err != nil || name != "folder.zip" || len(z.File) != 2 {
		t.Fatalf("archive FD: %s %#v %v", name, z, err)
	}
	mode, _, errno := syscall.Syscall(syscall.SYS_FCNTL, archiveFile.Fd(), syscall.F_GETFL, 0)
	if errno != 0 || mode&syscall.O_ACCMODE != syscall.O_RDONLY {
		t.Fatal("download inherited write authority")
	}
	if _, err = s.Rename(context.Background(), scope, "new.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), scope, "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), scope, ""); !errors.Is(err, ErrWorkspaceRootOperation) {
		t.Fatalf("root delete: %v", err)
	}
	auditFailure := errors.New("audit unavailable")
	s.auditRepo = &recordingWorkspaceFileAuditRepo{err: auditFailure}
	if _, err = s.Upload(context.Background(), scope, "", "denied.txt", strings.NewReader("data"), 4); !errors.Is(err, auditFailure) {
		t.Fatalf("audit error lost: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "denied.txt")); !os.IsNotExist(err) {
		t.Fatal("mutated before audit permission")
	}
	if writes.active {
		t.Fatal("confirmed failed operation retained live intent")
	}
}

type nfsTeamGuard struct{ member WorkspaceFileScope }

func (g nfsTeamGuard) WithWorkspaceMutation(_ context.Context, _ WorkspaceFileScope, run func() error) error {
	return run()
}
func (g nfsTeamGuard) WorkspaceTeamWriteScopes(context.Context, WorkspaceFileScope) ([]WorkspaceFileScope, error) {
	return []WorkspaceFileScope{g.member}, nil
}

func TestNFSWorkspaceIOTeamPreservesMemberOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-team-io-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	root := filepath.Join(base, "teams", "user-3", "team-7-shared")
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(root, 10001, 1000); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0770|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{"shared.txt": 0660, "private.txt": 0600} {
		p := filepath.Join(root, name)
		if err = os.WriteFile(p, []byte("original"), mode); err != nil {
			t.Fatal(err)
		}
		if err = os.Chown(p, 200012, 1000); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	scope := WorkspaceFileScope{TeamID: 7, UserID: 3, WorkspacePath: root}
	writes := &workspaceWriteFake{}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	ConfigureWorkspaceUpgradeGuard(s, nfsTeamGuard{member: WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4}})
	doc, err := s.ReadText(t.Context(), scope, "shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveText(t.Context(), scope, "shared.txt", "updated", doc.Version); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != 200012 || st.Gid != 1000 || info.Mode().Perm() != 0660 {
		t.Fatalf("member metadata changed: %+v %v", st, info.Mode())
	}
	if _, err = s.ReadText(t.Context(), scope, "private.txt"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("private member file was exposed: %v", err)
	}
	other := filepath.Join(root, "other-owner.txt")
	if err = os.WriteFile(other, []byte("other"), 0660); err != nil {
		t.Fatal(err)
	}
	os.Chmod(other, 0660)
	os.Chown(other, 200013, 1000)
	doc, err = s.ReadText(t.Context(), scope, "other-owner.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveText(t.Context(), scope, "other-owner.txt", "other-saved", doc.Version); err != nil {
		t.Fatal("second Team owner cannot share editor lock:", err)
	}
	if _, err = s.Mkdir(t.Context(), scope, "shared-dir"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upload(t.Context(), scope, "shared-dir", "new.txt", strings.NewReader("shared"), 6); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(root, "shared-dir"))
	if err != nil || info.Mode().Perm() != 0770 || info.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("new Team directory mode: %v %v", info, err)
	}
	info, err = os.Stat(filepath.Join(root, "shared-dir", "new.txt"))
	if err != nil || info.Mode().Perm() != 0660 {
		t.Fatalf("new Team file mode: %v %v", info, err)
	}
}

type cancelNFSUpload struct{ ctx context.Context }

func (r cancelNFSUpload) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, r.ctx.Err() }
func TestNFSWorkspaceIOCanceledHelperRetainsUncertainIntent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-cancel-io-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	root := filepath.Join(base, "opencode", "user-3", "instance-12")
	os.MkdirAll(root, 0755)
	os.Chown(root, 200012, 200012)
	writes := &workspaceWriteFake{}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = s.Upload(ctx, WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: root}, "", "canceled.txt", cancelNFSUpload{ctx}, 100)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if !errors.Is(err, ErrWorkspaceNFSUncertain) || !writes.active || writes.begins != 1 || writes.completes != 0 {
		t.Fatalf("uncertain dispatched NFS mutation released intent: %+v %v", writes, err)
	}
}

func TestNFSWorkspaceIORealRootSquash(t *testing.T) {
	base := os.Getenv("CM_NFS_FILEIO_ROOT")
	if base == "" {
		t.Skip("requires isolated preprovisioned root-squash NFS directory")
	}
	root := filepath.Join(base, "opencode", "user-3", "instance-12")
	if _, err := os.ReadFile(filepath.Join(root, "private.txt")); !os.IsPermission(err) {
		t.Fatalf("root-squash negative control failed: %v", err)
	}
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: root}
	writes := &workspaceWriteFake{}
	audit := &recordingWorkspaceFileAuditRepo{}
	s := NewWorkspaceFileService(audit).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	doc, err := s.ReadText(t.Context(), scope, "private.txt")
	if err != nil || doc.Text != "private-nfs" {
		t.Fatalf("private NFS read: %#v %v", doc, err)
	}
	if _, err = s.SaveText(t.Context(), scope, "private.txt", "saved-nfs", doc.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mkdir(t.Context(), scope, "files"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upload(t.Context(), scope, "files", "stream.txt", strings.NewReader("streamed-nfs"), 12); err != nil {
		t.Fatal(err)
	}
	file, _, _, err := s.OpenDownload(t.Context(), scope, "private.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(data) != "saved-nfs" {
		t.Fatalf("NFS transferredFD: %q %v", data, err)
	}
	if _, err = s.Rename(t.Context(), scope, "files/stream.txt", "files/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(t.Context(), scope, "files"); err != nil {
		t.Fatal(err)
	}
	if writes.active || writes.begins != writes.completes || len(audit.items) < 5 {
		t.Fatalf("audit/lifetime missing: %+v %+v", writes, audit.items)
	}
	t.Log("root-squash denied backend root; exact-instance helper read/write0600, streaming upload, transferredFD download, mkdir/rename/delete and audit/write-intent fencing passed")
}

func TestNFSWorkspaceIOTransportUncertaintyReceipt(t *testing.T) {
	for _, err := range []error{syscall.EIO, syscall.ESTALE, syscall.ETIMEDOUT, syscall.ECONNRESET} {
		if !errors.Is(workspaceNFSError(workspaceNFSErrorCode(err)), ErrWorkspaceNFSUncertain) {
			t.Fatalf("lost transport uncertainty: %v", err)
		}
	}
	if errors.Is(workspaceNFSError(workspaceNFSErrorCode(os.ErrPermission)), ErrWorkspaceNFSUncertain) {
		t.Fatal("ordinary denied operation marked uncertain")
	}
}

func TestNFSUploadOverwritePreservesPrivateMetadata(t *testing.T) {
	base := t.TempDir()
	scope := WorkspaceFileScope{WorkspacePath: base, credentialScoped: true}
	path := filepath.Join(base, "private")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Stat(path)
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	if _, err := s.Upload(t.Context(), scope, "", "private", strings.NewReader("new"), 3); err != nil {
		t.Fatal(err)
	}
	now, _ := os.Stat(path)
	if old.Mode() != now.Mode() {
		t.Fatalf("upload changed private mode: %v => %v", old.Mode(), now.Mode())
	}
}

type emptyNFSTeamGuard struct{}

func (emptyNFSTeamGuard) WithWorkspaceMutation(_ context.Context, _ WorkspaceFileScope, run func() error) error {
	return run()
}
func (emptyNFSTeamGuard) WorkspaceTeamWriteScopes(context.Context, WorkspaceFileScope) ([]WorkspaceFileScope, error) {
	return nil, nil
}
func TestNFSTeamStoppedMutationStillTracksIntent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-team-empty-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err = os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "teams", "user-3", "team-7-shared")
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(root, 10001, 1000); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0770|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	writes := &workspaceWriteFake{}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	ConfigureWorkspaceUpgradeGuard(s, emptyNFSTeamGuard{})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = s.Upload(ctx, WorkspaceFileScope{TeamID: 7, UserID: 3, WorkspacePath: root}, "", "pending", cancelNFSUpload{ctx}, 100)
	if !errors.Is(err, ErrWorkspaceNFSUncertain) || !writes.active || writes.begins != 1 {
		t.Fatalf("Team dispatch escaped durable tracking: %+v %v", writes, err)
	}
}

func TestNFSUploadOverwriteMetadataRealRootSquash(t *testing.T) {
	base := os.Getenv("CM_NFS_OVERWRITE_ROOT")
	if base == "" {
		t.Skip("requires root-squash overwrite fixture")
	}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	writes := &workspaceWriteFake{}
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	instance := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: filepath.Join(base, "opencode", "user-3", "instance-12")}
	team := WorkspaceFileScope{TeamID: 7, UserID: 3, WorkspacePath: filepath.Join(base, "teams", "user-3", "team-7-shared")}
	for _, tc := range []struct {
		scope  WorkspaceFileScope
		name   string
		denied bool
	}{{instance, "private.txt", false}, {team, "shared.txt", false}, {team, "private.txt", true}} {
		target := filepath.Join(tc.scope.WorkspacePath, tc.name)
		before, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Upload(t.Context(), tc.scope, "", tc.name, strings.NewReader("replacement"), 11)
		if tc.denied {
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("private Team overwrite allowed: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		old, next := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if before.Mode() != after.Mode() || old.Uid != next.Uid || old.Gid != next.Gid {
			t.Fatalf("metadata changed %v %v %+v %+v", before.Mode(), after.Mode(), old, next)
		}
		if tc.denied && !os.SameFile(before, after) {
			t.Fatal("denied overwrite replaced private inode")
		}
	}
}

func TestNFSUploadRejectsTargetReplacementAndHardlinks(t *testing.T) {
	for _, kind := range []string{"replace", "hardlink", "initial-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			target := filepath.Join(base, "target.txt")
			os.WriteFile(target, []byte("old"), 0600)
			if kind == "initial-hardlink" {
				if err := os.Link(target, filepath.Join(base, "alias")); err != nil {
					t.Fatal(err)
				}
			}
			s := NewWorkspaceFileService(nil).(*workspaceFileService)
			s.auditHook = func(string, string, int64) error {
				if kind == "replace" {
					other := filepath.Join(base, "other")
					os.WriteFile(other, []byte("replacement"), 0600)
					return os.Rename(other, target)
				}
				return os.Link(target, filepath.Join(base, "alias"))
			}
			if _, err := s.Upload(t.Context(), WorkspaceFileScope{WorkspacePath: base, credentialScoped: true}, "", "target.txt", strings.NewReader("new"), 3); err == nil {
				t.Fatal("raced or multiply linked target replaced")
			}
			body, _ := os.ReadFile(target)
			if string(body) == "new" {
				t.Fatal("upload committed after target changed")
			}
		})
	}
}

func TestNFSTeamUncertainUploadSurvivesSQLRestart(t *testing.T) {
	for _, membership := range []string{"empty", "stopped", "mixed"} {
		t.Run(membership, func(t *testing.T) {
			sess := upgradeFlowDB(t)
			run := func(q string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			run(`ALTER TABLE teams ADD user_id INT,ADD status VARCHAR(32)`)
			run(`ALTER TABLE team_members ADD team_id INT,ADD instance_id INT,ADD status VARCHAR(32)`)
			migration, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range strings.Split(string(migration), ";") {
				if strings.TrimSpace(q) != "" {
					run(q)
				}
			}
			run(`INSERT INTO teams(id,user_id,status)VALUES(7,3,'active')`)
			if membership != "empty" {
				run(`INSERT INTO instances(id,user_id,status,runtime_generation)VALUES(12,3,'stopped',4)`)
				run(`INSERT INTO team_members(id,team_id,instance_id,status)VALUES(12,7,12,'idle')`)
			}
			if membership == "mixed" {
				run(`INSERT INTO instances(id,user_id,status,runtime_generation)VALUES(13,3,'running',4)`)
				run(`INSERT INTO team_members(id,team_id,instance_id,status)VALUES(13,7,13,'idle')`)
			}
			base, err := os.MkdirTemp("/tmp", "cm-team-sql-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(base)
			os.Chmod(base, 0755)
			root := filepath.Join(base, "teams", "user-3", "team-7-shared")
			os.MkdirAll(root, 0755)
			os.Chown(root, 10001, 1000)
			os.Chmod(root, 0770|os.ModeSetgid)
			s := NewWorkspaceFileService(nil).(*workspaceFileService)
			ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, repository.NewWorkspaceWriteRepository(sess))
			ConfigureWorkspaceUpgradeGuard(s, emptyNFSTeamGuard{})
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			_, err = s.Upload(ctx, WorkspaceFileScope{TeamID: 7, UserID: 3, WorkspacePath: root}, "", "pending", cancelNFSUpload{ctx}, 100)
			if !errors.Is(err, ErrWorkspaceNFSUncertain) {
				t.Fatal("expected uncertain dispatch", err)
			}
			restarted := repository.NewWorkspaceWriteRepository(sess)
			err = restarted.Begin(t.Context(), models.WorkspaceWriteIntent{Operation: strings.Repeat("c", 32), TeamID: 7, OwnerID: 3, Action: "upload"})
			if !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
				t.Fatal("backend reconstruction lost Team writer", err)
			}
		})
	}
}
