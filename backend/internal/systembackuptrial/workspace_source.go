package systembackuptrial

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	domain "clawreef/internal/systembackuptrial/records"

	"github.com/gin-gonic/gin"
)

const workspaceDirectoryPageSize = 100

type workspaceSource struct {
	UserID       int    `json:"user_id"`
	InstanceID   int    `json:"instance_id"`
	Runtime      string `json:"runtime"`
	RelativeRoot string `json:"relative_root"`
}

func (s workspaceSource) valid(placement trialPlacement) bool {
	return placement.WorkspaceClaim != "" && placement.WorkspaceClaim != placement.ArtifactClaim &&
		s.UserID > 0 && s.InstanceID > 0 && supportedWorkspaceRuntime(s.Runtime) && validWorkspaceSelection(s.RelativeRoot)
}

// The application's mount already starts at the registered directory. Paths
// in requests and archive manifests are relative to the original instance.
func (s workspaceSource) mountedPath(relativeDir string) (string, bool) {
	if !validWorkspaceSelection(relativeDir) {
		return "", false
	}
	if relativeDir == s.RelativeRoot {
		return ".", true
	}
	return strings.CutPrefix(relativeDir, s.RelativeRoot+"/")
}

func (s *Service) workspaceSourceConfig(profile *trialEnvironment) (*workspaceSource, string, error) {
	root := s.value("WORKSPACE_FILES_SELECTION_ROOT")
	if profile == nil || profile.Side != "source" || profile.Source.WorkspaceFiles == nil ||
		!filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return nil, "", errWorkspaceFiles
	}
	return profile.Source.WorkspaceFiles, root, nil
}

func openRegisteredWorkspace(root string) (*os.Root, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil, errWorkspaceFiles
	}
	mount, err := os.OpenRoot(root)
	if err != nil {
		return nil, errWorkspaceFiles
	}
	if readOnly, err := restoreMountReadOnly(mount); err != nil || !readOnly {
		mount.Close()
		return nil, errWorkspaceFiles
	}
	return mount, nil
}

type workspaceDirectoryPage struct {
	RelativeDir string   `json:"relative_dir"`
	Directories []string `json:"directories"`
	Next        string   `json:"next,omitempty"`
}

func validWorkspaceDirectoryName(name string) bool {
	return !strings.Contains(name, "/") && validWorkspaceSelection("a/"+name)
}

func readWorkspaceDirectories(ctx context.Context, mount *os.Root, mountedDir, relativeDir, after string) (workspaceDirectoryPage, error) {
	root, err := openWorkspaceDirectory(mount, mountedDir)
	if err != nil {
		return workspaceDirectoryPage{}, errWorkspaceFiles
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return workspaceDirectoryPage{}, errWorkspaceFiles
	}
	defer f.Close()
	page := workspaceDirectoryPage{RelativeDir: relativeDir, Directories: []string{}}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return workspaceDirectoryPage{}, err
		}
		entries, err := f.ReadDir(128)
		total += int64(len(entries))
		if total > workspaceFileLimits.MaxFiles || (err != nil && !errors.Is(err, io.EOF)) {
			return workspaceDirectoryPage{}, errWorkspaceFiles
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() && name > after && validWorkspaceDirectoryName(name) && validWorkspaceSelection(relativeDir+"/"+name) {
				page.Directories = append(page.Directories, name)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	slices.Sort(page.Directories)
	if len(page.Directories) > workspaceDirectoryPageSize {
		page.Directories = page.Directories[:workspaceDirectoryPageSize]
		page.Next = page.Directories[len(page.Directories)-1]
	}
	return page, nil
}

func (s *Service) handleBackupWorkspaces(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) > 2 || len(query["dir"]) > 1 || len(query["after"]) > 1 ||
		len(query) != len(query["dir"])+len(query["after"]) ||
		(len(query["after"]) == 1 && !validWorkspaceDirectoryName(query.Get("after"))) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_backup_scope"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	environment := s.environment(ctx, false)
	if environment.Profile == nil || environment.missing("source") != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "trial_environment_source"})
		return
	}
	registered, mountPath, err := s.workspaceSourceConfig(environment.Profile)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_files"})
		return
	}
	relativeDir := query.Get("dir")
	if len(query["dir"]) == 0 {
		relativeDir = registered.RelativeRoot
	}
	mountedDir, ok := registered.mountedPath(relativeDir)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_backup_scope"})
		return
	}
	source, database, err := s.database("SOURCE")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "source_credential"})
		return
	}
	defer source.Close()
	choice := domain.TrialInstanceChoice{ID: int64(registered.InstanceID), UserID: int64(registered.UserID)}
	if err := domain.CheckTrialLiteOwner(ctx, source, database, s.value("SOURCE_UUID"), choice, registered.Runtime); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_files"})
		return
	}
	mount, err := openRegisteredWorkspace(mountPath)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_files"})
		return
	}
	defer mount.Close()
	page, err := readWorkspaceDirectories(ctx, mount, mountedDir, relativeDir, query.Get("after"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_files"})
		return
	}
	c.JSON(http.StatusOK, struct {
		workspaceSource
		workspaceDirectoryPage
	}{*registered, page})
}

func (s *Service) captureRegisteredWorkspace(ctx context.Context, snapshot domain.Snapshot, selection workspaceSelection) ([]byte, *workspaceManifest, error) {
	profile, err := s.environmentProfile()
	if err != nil {
		return nil, nil, domain.ErrTrialScope
	}
	registered, mountPath, err := s.workspaceSourceConfig(&profile)
	if err != nil || registered.UserID != selection.UserID || registered.InstanceID != selection.InstanceID ||
		registered.Runtime != selectedRuntime(snapshot, selection) {
		return nil, nil, domain.ErrTrialScope
	}
	relative, ok := registered.mountedPath(selection.RelativeDir)
	if !ok {
		return nil, nil, domain.ErrTrialScope
	}
	mount, err := openRegisteredWorkspace(mountPath)
	if err != nil {
		return nil, nil, err
	}
	defer mount.Close()
	return archiveWorkspaceDirectory(ctx, mount, relative, workspaceManifest{workspaceSelection: selection, Runtime: registered.Runtime})
}
