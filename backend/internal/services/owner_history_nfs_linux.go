//go:build linux

package services

import (
	"bytes"
	"clawreef/internal/ownerrecords"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

func RunOwnerHistoryHelper(args []string, input io.Reader, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != ownerHistoryHelperCommand {
		return false, nil
	}
	if len(args) != 1 {
		return true, ErrWorkspacePathInvalid
	}
	request, err := readWorkspaceNFSHelperRequest(input)
	if err != nil {
		return true, err
	}
	expected, err := ownerHistoryRequest(request.Root, request.Scope.WorkspacePath, request.HistoryIdentity)
	if err != nil || expected.UID != request.UID || expected.GID != request.GID || request.Scope != expected.Scope || len(request.HistoryCutoff) > 1 {
		return true, ErrWorkspacePathInvalid
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || os.Getuid() != request.UID || os.Geteuid() != request.UID || os.Getgid() != request.GID || os.Getegid() != request.GID {
		return true, os.ErrPermission
	}
	syscall.CloseOnExec(3)
	if err = validateWorkspaceNFSHelperPath(request); err != nil {
		return true, json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
	}
	// Landlock is irreversible and thread-local on the supported Linux ABI. Keep
	// all parser opens and the fixed SQLite fork on this locked thread; never put
	// this thread back into the general Go pool. The helper exits after the reply.
	localTemp := request.HistoryTempPath
	if filepath.Dir(localTemp) != "/tmp" || !strings.HasPrefix(filepath.Base(localTemp), "cm-owner-history-") {
		return true, ErrWorkspacePathInvalid
	}
	nonce, decodeErr := hex.DecodeString(strings.TrimPrefix(filepath.Base(localTemp), "cm-owner-history-"))
	if decodeErr != nil || len(nonce) != 16 {
		return true, ErrWorkspacePathInvalid
	}
	if err = os.Mkdir(localTemp, 0700); err != nil {
		return true, err
	}
	// The child removes temporary query contents; parent removes its preselected
	// empty directory after confirmed helper exit (no broad /tmp removal grant).
	defer os.RemoveAll(localTemp)
	// SQLite may spill a bounded read-only query's sort state locally. Grant only
	// this helper-owned temporary directory; persistent instance files stay read-only.
	if err = os.Setenv("SQLITE_TMPDIR", localTemp); err != nil {
		return true, err
	}
	runtime.LockOSThread()
	if err = restrictOwnerHistoryReadsWithTemp(request.Root, request.Scope.WorkspacePath, localTemp, ownerHistoryInputPaths(request)...); err != nil {
		return true, err
	}
	response := workspaceNFSResponse{}
	switch request.Action {
	case "snapshot":
		var identity struct {
			Root, Workspace string
			Identity        ownerrecords.Identity
		}
		if json.Unmarshal(request.HistorySnapshot, &identity) != nil || identity.Root != request.Root || identity.Workspace != request.Scope.WorkspacePath || identity.Identity != request.HistoryIdentity {
			return true, ErrWorkspacePathInvalid
		}
		var result bytes.Buffer
		if ownerrecords.RunHistoryReaderContext(context.Background(), bytes.NewReader(request.HistorySnapshot), &result) != 0 {
			return true, ErrWorkspacePathInvalid
		}
		response.HistorySnapshot = result.Bytes()
	case "history":
		response.Questions, err = ownerrecords.ReadNativeHistorySnapshot(context.Background(), request.Root, request.Scope.WorkspacePath, request.HistoryIdentity, request.HistoryRecent, request.HistoryCutoff...)
	case "fingerprint":
		response.Fingerprint, err = ownerrecords.NativeHistoryFingerprint(context.Background(), request.Root, request.Scope.WorkspacePath, request.HistoryIdentity)
	default:
		err = ErrWorkspacePathInvalid
	}
	response.Error = workspaceNFSErrorCode(err)
	return true, json.NewEncoder(output).Encode(response)
}

// Deny writes and reads outside the exact private instance, even if a user
// races a parser path check with a symlink to a world-readable foreign file.
// See https://docs.kernel.org/userspace-api/landlock.html .
func restrictOwnerHistoryReads(workspaceRoot, workspace string, inputs ...string) error {
	return restrictOwnerHistoryReadsWithTemp(workspaceRoot, workspace, "", inputs...)
}
func restrictOwnerHistoryReadsWithTemp(workspaceRoot, workspace, localTemp string, inputs ...string) error {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 3 {
		return fmt.Errorf("private NFS history requires Landlock ABI 3 or newer")
	}
	attr := unix.LandlockRulesetAttr{Access_fs: (1 << 15) - 1}
	ruleset, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), 8, 0)
	if errno != 0 {
		return errno
	}
	defer unix.Close(int(ruleset))
	addFD := func(fd int, access uint64) error {
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, ruleset, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if errno != 0 {
			return errno
		}
		return nil
	}
	add := func(name string, access uint64, required bool) error {
		resolved, err := filepath.EvalSymlinks(name)
		if os.IsNotExist(err) && !required {
			return nil
		}
		if err != nil {
			return err
		}
		if name != workspace && (pathWithin(workspaceRoot, resolved) || pathWithin(resolved, workspaceRoot)) {
			return ErrWorkspacePathEscape
		}
		fd, err := unix.Open(resolved, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		return addFD(fd, access)
	}
	read := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	if err := add(workspace, unix.LANDLOCK_ACCESS_FS_READ_DIR, true); err != nil {
		return err
	}
	// READ_FILE is granted per validated inode, never to the complete tree.
	// A pre-existing foreign hardlink or a path swapped after validation must
	// not turn an authorized directory name into authority over a foreign inode.
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	if len(inputs) == 0 {
		inputs = []string{workspace}
	}
	count := 0
	for _, input := range inputs {
		if !pathWithin(workspace, input) {
			return ErrWorkspacePathEscape
		}
		if _, err := os.Lstat(input); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := filepath.WalkDir(input, func(name string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			count++
			if count > 2200 {
				return fmt.Errorf("history directory limit exceeded")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrWorkspacePathEscape
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(workspace, name)
			if err != nil {
				return err
			}
			file, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				return err
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
				return os.ErrPermission
			}
			return addFD(int(file.Fd()), unix.LANDLOCK_ACCESS_FS_READ_FILE)
		}); err != nil {
			return err
		}
	}
	for _, name := range []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64"} {
		if err := add(name, read|unix.LANDLOCK_ACCESS_FS_EXECUTE, false); err != nil {
			return err
		}
	}
	for _, name := range []string{"/usr/bin/sqlite3", "/etc/ld.so.cache", "/etc/ld.so.preload", "/etc/ld-musl-x86_64.path"} {
		access := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE)
		if name == "/usr/bin/sqlite3" {
			access |= unix.LANDLOCK_ACCESS_FS_EXECUTE
		}
		if err := add(name, access, false); err != nil {
			return err
		}
	}
	if localTemp != "" {
		info, err := os.Lstat(localTemp)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
			return os.ErrPermission
		}
		access := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE)
		if err := add(localTemp, access, true); err != nil {
			return err
		}
	}
	if err := add("/dev/null", unix.LANDLOCK_ACCESS_FS_READ_FILE|unix.LANDLOCK_ACCESS_FS_WRITE_FILE, true); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, ruleset, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func ownerHistoryInputPaths(request workspaceNFSRequest) []string {
	root := request.Scope.WorkspacePath
	switch request.HistoryIdentity.Runtime {
	case RuntimeTypeOpenClaw:
		return []string{filepath.Join(root, "home/.openclaw/agents")}
	case RuntimeTypeDeepSeekHarness:
		return []string{filepath.Join(root, "home/.dsh/sessions")}
	case RuntimeTypeHermes:
		path := filepath.Join(root, "home/.hermes/state.db")
		return []string{path, path + "-wal", path + "-shm"}
	case RuntimeTypeOpenCode:
		path := filepath.Join(root, "home/.local/share/opencode/opencode.db")
		return []string{path, path + "-wal", path + "-shm"}
	}
	return nil
}
