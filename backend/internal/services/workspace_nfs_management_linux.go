//go:build linux

package services

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runWorkspaceManagementHelper(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 {
		return ErrWorkspacePathInvalid
	}
	request, err := readWorkspaceNFSHelperRequest(input)
	if err != nil {
		return err
	}
	uid, gid, err := validateWorkspaceNFSScope(request.Root, request.Scope)
	if err != nil || request.Scope.TeamID != 0 {
		return ErrWorkspacePathInvalid
	}
	if strings.HasPrefix(request.Action, "browser_") && filepath.Base(filepath.Dir(filepath.Dir(request.Scope.WorkspacePath))) != RuntimeTypeOpenClaw {
		return ErrWorkspacePathInvalid
	}
	businessUID := uid
	switch request.Action {
	case "probe", "clear", "browser_config", "browser_migrate", "browser_clear":
	case "browser_parents":
		uid, gid = 10001, 10001
	case "remove", "browser_grant", "browser_remove":
		uid = 10001
	default:
		return ErrWorkspacePathInvalid
	}
	groups, err := os.Getgroups()
	groupsOK := len(groups) == 0

	if err != nil || !groupsOK || request.UID != uid || request.GID != gid || os.Getuid() != uid || os.Geteuid() != uid || os.Getgid() != gid || os.Getegid() != gid {
		return os.ErrPermission
	}
	if request.Action == "browser_grant" || request.Action == "browser_parents" {
		err = grantNFSBrowserParent(request)
		return json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
	}
	// The empty-leaf removal runs as management, but the permitted leaf owner
	// remains the persisted instance identity, never an arbitrary request UID.
	validation := request
	validation.UID = businessUID
	if request.Action == "browser_clear" || request.Action == "browser_remove" {
		validation.Scope.WorkspacePath = nfsBrowserParent(request.Root, request.Scope)
	}
	err = validateWorkspaceNFSHelperPath(validation)
	missing := os.IsNotExist(err)
	if missing && (request.Action == "clear" || request.Action == "remove" || request.Action == "browser_clear" || request.Action == "browser_remove") {
		validation.Scope.WorkspacePath = filepath.Dir(validation.Scope.WorkspacePath)
		validation.UID = 10001
		for {
			_, probeErr := os.Lstat(validation.Scope.WorkspacePath)
			if os.IsNotExist(probeErr) && validation.Scope.WorkspacePath != "/" {
				validation.Scope.WorkspacePath = filepath.Dir(validation.Scope.WorkspacePath)
				continue
			}
			err = validateWorkspaceNFSHelperPath(validation)
			break
		}
		if err == nil {
			return json.NewEncoder(output).Encode(workspaceNFSResponse{})
		}
	}
	if err == nil {
		if request.Action == "browser_migrate" {
			err = migrateNFSBrowserProfile(request)
		} else {
			if request.Action == "browser_clear" || request.Action == "browser_remove" {
				parent, openErr := openNFSBrowserParent(request)
				if openErr != nil {
					err = openErr
				} else {
					parent.Close()
					request.Scope.WorkspacePath = validation.Scope.WorkspacePath
					if request.Action == "browser_clear" {
						request.Action = "clear"
					} else {
						request.Action = "remove"
					}
				}
			}
			if err == nil {
				err = performNFSInstanceManagement(request)
			}
		}
	}
	return json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
}

func performNFSInstanceManagement(request workspaceNFSRequest) error {
	if request.Action == "remove" {
		parent, err := os.OpenRoot(filepath.Dir(request.Scope.WorkspacePath))
		if err != nil {
			return err
		}
		defer parent.Close()
		// Remove (not RemoveAll) cannot discard residual data if clearing failed or
		// an unexpected writer populated the leaf after its contents were inspected.
		return parent.Remove(filepath.Base(request.Scope.WorkspacePath))
	}
	root, err := os.OpenRoot(request.Scope.WorkspacePath)
	if err != nil {
		return err
	}
	defer root.Close()
	switch request.Action {
	case "probe":
		home, err := root.Lstat("home")
		if err != nil {
			return err
		}
		if !home.IsDir() || home.Mode()&os.ModeSymlink != 0 {
			return ErrWorkspacePathEscape
		}
		return nil
	case "clear":
		directory, err := root.Open(".")
		if err != nil {
			return err
		}
		defer directory.Close()
		// A single pinned root confines every recursive removal, including hostile
		// symlinks. Pagination bounds memory without trusting business filenames.
		for {
			entries, err := directory.ReadDir(128)
			if err != nil && err != io.EOF {
				return err
			}
			for _, entry := range entries {
				if err := root.RemoveAll(entry.Name()); err != nil {
					return err
				}
			}
			if err == io.EOF {
				return nil
			}
		}
	case "browser_config":
		if filepath.Base(filepath.Dir(filepath.Dir(request.Scope.WorkspacePath))) != RuntimeTypeOpenClaw || !validNFSBrowserHost(request.Scope.InstanceID, request.BrowserHost) {
			return ErrWorkspacePathInvalid
		}
		return patchProtectedBrowserConfig(request.Scope.WorkspacePath, request.BrowserHost, request.BrowserEnabled)
	default:
		return ErrWorkspacePathInvalid
	}
}
