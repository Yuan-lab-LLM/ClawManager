package services

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type workspaceGuardRepo struct {
	repository.InstanceRepository
	instance models.Instance
	locked   bool
	releases int
	err      error
}

func (r *workspaceGuardRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	if r.err != nil {
		return nil, r.err
	}
	r.locked = true
	return func() { r.locked = false; r.releases++ }, nil
}
func (r *workspaceGuardRepo) GetByID(int) (*models.Instance, error) {
	if !r.locked {
		panic("snapshot read outside lifecycle lock")
	}
	v := r.instance
	return &v, nil
}

func TestWorkspaceUpgradeGuardRejectsAllMutationsBeforeIO(t *testing.T) {
	for _, failure := range []error{models.ErrRuntimeUpgradeInProgress, errors.New("database unavailable")} {
		// Embedded nil methods panic if any filesystem/exec operation escapes.
		base := &guardedWorkspaceFileService{}
		guard := WithWorkspaceUpgradeGuard(base, upgradeBlockedRepo{failure: failure}, false)
		scope := WorkspaceFileScope{InstanceID: 90401, UserID: 1, RuntimeGeneration: 2, WorkspacePath: "/unused"}
		for name, write := range map[string]func() error{
			"upload": func() error {
				_, err := guard.Upload(t.Context(), scope, "", "file", strings.NewReader("x"), 1)
				return err
			},
			"mkdir":  func() error { _, err := guard.Mkdir(t.Context(), scope, "new"); return err },
			"rename": func() error { _, err := guard.Rename(t.Context(), scope, "old", "new"); return err },
			"delete": func() error { return guard.Delete(t.Context(), scope, "old") },
		} {
			if err := write(); !errors.Is(err, failure) {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
}

func TestWorkspaceUpgradeGuardRevalidatesOwnerGenerationAndBackend(t *testing.T) {
	for _, change := range []string{"owner", "generation", "path", "backend", "deleted", "missing guard"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			repo := &workspaceGuardRepo{instance: models.Instance{ID: 90402, UserID: 1, RuntimeGeneration: 2, WorkspacePath: &root, Status: "running"}}
			scope := WorkspaceFileScope{InstanceID: 90402, UserID: 1, RuntimeGeneration: 2, WorkspacePath: root}
			switch change {
			case "owner":
				repo.instance.UserID++
			case "generation":
				repo.instance.RuntimeGeneration++
			case "path":
				scope.WorkspacePath = filepath.Join(root, "old")
			case "backend":
				repo.instance.InstanceMode = InstanceModePro
			case "deleted":
				repo.instance.Status = "deleting"
			}
			var instances repository.InstanceRepository = repo
			if change == "missing guard" {
				instances = nil
			}
			guard := WithWorkspaceUpgradeGuard(NewWorkspaceFileService(nil), instances, false)
			if _, err := guard.Mkdir(t.Context(), scope, "new"); err == nil {
				t.Fatal("stale scope wrote data")
			}
			if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
				t.Fatal("filesystem changed", err)
			}
			if repo.locked {
				t.Fatal("lock leaked")
			}
		})
	}
}

func TestWorkspaceUpgradeGuardPreservesOrdinaryWritesAndReads(t *testing.T) {
	root := t.TempDir()
	repo := &workspaceGuardRepo{instance: models.Instance{ID: 90403, UserID: 1, RuntimeGeneration: 2, WorkspacePath: &root}}
	scope := WorkspaceFileScope{InstanceID: 90403, UserID: 1, RuntimeGeneration: 2, WorkspacePath: root}
	guard := WithWorkspaceUpgradeGuard(NewWorkspaceFileService(nil), repo, false)
	if _, err := guard.Mkdir(t.Context(), scope, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Upload(t.Context(), scope, "docs", "old.txt", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Rename(t.Context(), scope, "docs/old.txt", "docs/new.txt"); err != nil {
		t.Fatal(err)
	}
	repo.err = models.ErrRuntimeUpgradeInProgress
	items, err := guard.List(t.Context(), scope, "docs")
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if _, err = guard.Preview(t.Context(), scope, "docs/new.txt"); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := guard.OpenDownload(t.Context(), scope, "docs/new.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = guard.Delete(t.Context(), scope, "docs/new.txt"); !errors.Is(err, repo.err) {
		t.Fatal(err)
	}
	repo.err = nil
	if err = guard.Delete(t.Context(), scope, "docs/new.txt"); err != nil {
		t.Fatal(err)
	}
	if repo.locked || repo.releases != 4 {
		t.Fatal("lock accounting", repo.releases)
	}
}

type blockingWorkspaceUpload struct {
	WorkspaceFileService
	entered, finish chan struct{}
}

func (s blockingWorkspaceUpload) Upload(ctx context.Context, scope WorkspaceFileScope, dir, name string, r io.Reader, size int64) (*WorkspaceEntry, error) {
	close(s.entered)
	<-s.finish
	return nil, errors.New("injected upload failure")
}
func TestWorkspaceUpgradeGuardHoldsLockUntilIOFinishes(t *testing.T) {
	root := t.TempDir()
	repo := &workspaceGuardRepo{instance: models.Instance{ID: 90404, UserID: 1, WorkspacePath: &root}}
	base := blockingWorkspaceUpload{entered: make(chan struct{}), finish: make(chan struct{})}
	guard := WithWorkspaceUpgradeGuard(base, repo, false)
	done := make(chan error, 1)
	go func() {
		_, err := guard.Upload(context.Background(), WorkspaceFileScope{InstanceID: 90404, UserID: 1, WorkspacePath: root}, "", "a", strings.NewReader("x"), 1)
		done <- err
	}()
	<-base.entered
	_, err := acquireInstanceLifecycleOperation(t.Context(), repo, 90404)
	close(base.finish)
	if writeErr := <-done; writeErr == nil {
		t.Fatal("lost IO error")
	}
	if !errors.Is(err, models.ErrInstanceOperationInProgress) {
		t.Fatal("lifecycle overtook upload", err)
	}
	release, err := acquireInstanceLifecycleOperation(t.Context(), repo, 90404)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestUpgradeBlocksSkillWritesBeforeDependencies(t *testing.T) {
	s := &skillService{instanceRepo: upgradeBlockedRepo{failure: models.ErrRuntimeUpgradeInProgress}}
	for name, write := range map[string]func() error{
		"attach":  func() error { _, err := s.AttachSkillToInstance(1, "admin", 90405, 2); return err },
		"hub":     func() error { _, err := s.InstallHubSkill(1, "admin", 2, 90405); return err },
		"restore": func() error { _, err := s.RestoreInstanceSkill(1, "admin", 90405, 2); return err },
		"remove":  func() error { return s.RemoveSkillFromInstance(90405, 2) },
	} {
		if err := write(); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	results := s.BatchInstallHubSkill(1, "admin", 2, []int{90405, 90406})
	if len(results) != 2 || results[0].Error == "" || results[1].Error == "" {
		t.Fatal(results)
	}
}
