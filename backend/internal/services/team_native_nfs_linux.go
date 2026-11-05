//go:build linux

package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func runTeamNativeHelper(ctx context.Context, request teamNativeRequest) (bool, error) {
	uid, gid, _, err := validateTeamNativeRequest(request)
	if err != nil {
		return true, err
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > 8<<20 {
		return true, ErrWorkspacePathInvalid
	}
	executable, err := os.Executable()
	if err != nil {
		return true, err
	}
	file, err := os.Open(executable)
	if err != nil {
		return true, err
	}
	defer file.Close()
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", teamWorkspaceHelperCommand)
	cmd.ExtraFiles = []*os.File{file}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(data)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout = &output
	if err = cmd.Start(); err != nil {
		return true, err
	}
	waitErr := cmd.Wait()
	ended := cmd.ProcessState != nil
	if waitErr != nil {
		return ended, errors.Join(ErrWorkspaceNFSUncertain, ctx.Err(), fmt.Errorf("credential-scoped Team operation failed: %w", waitErr))
	}
	var response struct {
		OK    bool
		Error string
	}
	if json.Unmarshal(output.Bytes(), &response) != nil {
		return ended, errors.Join(ErrWorkspaceNFSUncertain, fmt.Errorf("Team helper completion is invalid"))
	}
	if response.Error != "" {
		return ended, workspaceNFSError(response.Error)
	}
	if !response.OK {
		return ended, ErrWorkspaceNFSUncertain
	}
	return ended, nil
}

func RunTeamWorkspaceHelper(args []string, in io.Reader, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != teamWorkspaceHelperCommand {
		return false, nil
	}
	if len(args) != 1 {
		return true, ErrWorkspacePathInvalid
	}
	syscall.Close(3)
	if _, _, err := syscall.Syscall6(syscall.SYS_PRCTL, 4, 0, 0, 0, 0, 0); err != 0 {
		return true, err
	}
	var request teamNativeRequest
	decoder := json.NewDecoder(io.LimitReader(in, (8<<20)+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return true, ErrWorkspacePathInvalid
	}
	uid, gid, path, err := validateTeamNativeRequest(request)
	if err != nil {
		return true, err
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || os.Getuid() != uid || os.Geteuid() != uid || os.Getgid() != gid || os.Getegid() != gid {
		return true, os.ErrPermission
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return true, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[0] == "CapPrm:" || fields[0] == "CapEff:" || fields[0] == "CapInh:" || fields[0] == "CapAmb:") && fields[1] != "0000000000000000" {
			return true, os.ErrPermission
		}
	}
	syscall.Umask(0007)
	if err = checkTeamNativeMounts(request.Root, path); err != nil {
		return true, err
	}
	if request.Action == "shared" || request.Action == "grant_instance" {
		if err = grantNativeTeamPath(request, path, gid); err != nil {
			return true, json.NewEncoder(out).Encode(struct {
				OK    bool
				Error string
			}{Error: workspaceNFSErrorCode(err)})
		}
	}
	if request.Action != "grant_instance" {
		err = applyNativeTeamFiles(request, path)
	}
	return true, json.NewEncoder(out).Encode(struct {
		OK    bool
		Error string
	}{OK: err == nil, Error: workspaceNFSErrorCode(err)})
}

func openTeamNativeRoot(path string) (*os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, ErrWorkspacePathEscape
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, ErrWorkspacePathEscape
	}
	return root, nil
}
func nativeTeamChild(parent *os.Root, name string, mode os.FileMode) (*os.Root, bool, error) {
	err := parent.Mkdir(name, mode.Perm())
	created := err == nil
	if err != nil && !os.IsExist(err) {
		return nil, false, err
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, false, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, false, ErrWorkspacePathEscape
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, false, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		child.Close()
		return nil, false, ErrWorkspacePathEscape
	}
	return child, created, nil
}
func nativeTeamDirectory(root *os.Root, relative string) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	if relative == "." || relative == "" {
		return current, nil
	}
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if part == "." || part == ".." || part == "" {
			current.Close()
			return nil, ErrWorkspacePathEscape
		}
		next, _, err := nativeTeamChild(current, part, 0750)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}
func grantNativeTeamPath(request teamNativeRequest, path string, gid int) error {
	resolved, err := filepath.EvalSymlinks(request.Root)
	if err != nil {
		return err
	}
	if resolved != request.Root {
		return ErrWorkspacePathEscape
	}
	for current := request.Root; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || (st.Uid != 0 && st.Uid != 10001) || info.Mode().Perm()&0022 != 0 {
			return os.ErrPermission
		}
		if current == "/" {
			break
		}
	}
	current, err := openTeamNativeRoot(request.Root)
	if err != nil {
		return err
	}
	defer func() { current.Close() }()
	info, err := current.Stat(".")
	if err != nil {
		return err
	}
	if info.Sys().(*syscall.Stat_t).Uid != 10001 {
		return fmt.Errorf("NFS root must be provisioned for management UID 10001")
	}
	if info.Mode().Perm()&0111 != 0111 {
		file, err := current.Open(".")
		if err != nil {
			return err
		}
		err = file.Chmod(info.Mode().Perm() | 0111)
		file.Close()
		if err != nil {
			return err
		}
	}
	relative, err := filepath.Rel(request.Root, path)
	if err != nil {
		return err
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		next, _, err := nativeTeamChild(current, part, 0711)
		if err != nil {
			return err
		}
		current.Close()
		current = next
		info, err := current.Stat(".")
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 10001 || info.Mode().Perm()&0022 != 0 {
			return os.ErrPermission
		}
		if info.Mode().Perm()&0111 != 0111 {
			file, err := current.Open(".")
			if err != nil {
				return err
			}
			err = file.Chmod(info.Mode().Perm() | 0111)
			file.Close()
			if err != nil {
				return err
			}
		}
	}
	name := parts[len(parts)-1]
	err = current.Mkdir(name, 0770)
	created := err == nil
	if err != nil && !os.IsExist(err) {
		return err
	}
	info, err = current.Lstat(name)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0002 != 0 {
		return os.ErrPermission
	}
	if request.Action == "grant_instance" {
		if st.Uid == uint32(RuntimeLinuxID(request.InstanceID)) {
			return nil
		}
		if st.Uid != 10001 || int(st.Gid) != gid {
			return os.ErrPermission
		}
		if !created && info.Mode().Perm() != 0770 {
			return os.ErrPermission
		}
	} else if (st.Uid != 0 && st.Uid != 10001 && st.Uid != teamSharedUID) || int(st.Gid) != gid {
		return os.ErrPermission
	}
	if created {
		leaf, err := current.Open(name)
		if err != nil {
			return err
		}
		defer leaf.Close()
		mode := os.FileMode(0770)
		if request.Action == "shared" {
			mode |= os.ModeSetgid
		}
		return leaf.Chmod(mode)
	}
	if request.Action == "shared" && (info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm()&0070 != 0070) {
		return fmt.Errorf("Team root requires a preprovisioned shared setgid grant")
	}
	return nil
}

func applyNativeTeamFiles(request teamNativeRequest, path string) error {
	root, err := openTeamNativeRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	if request.Action == "shared" {
		for _, relative := range request.Directories {
			if relative == "" {
				continue
			}
			if err = root.MkdirAll(relative, 0770); err != nil {
				return err
			}
		}
		for name, text := range request.Context {
			if err = writeNativeTeamFile(root, name, text, false, 0660, false); err != nil {
				return err
			}
		}
		return nil
	}
	promptRoots := []string{"."}
	if request.RuntimeType == RuntimeTypeOpenClaw {
		promptRoots = []string{filepath.Join("home", ".openclaw", "workspace")}
	} else if request.RuntimeType == RuntimeTypeHermes && request.Action == "identity" {
		promptRoots = append(promptRoots, filepath.Join("home", ".hermes"), filepath.Join("home", ".clawmanager-team-worker", ".hermes"))
	}
	for _, relative := range promptRoots {
		prompt, err := nativeTeamDirectory(root, relative)
		if err != nil {
			return err
		}
		for name, text := range request.Identity {
			if err = writeNativeTeamFile(prompt, name, text, true, 0600, true); err != nil {
				prompt.Close()
				return err
			}
		}
		for name, text := range request.Context {
			if err = writeNativeTeamFile(prompt, name, text, false, 0600, true); err != nil {
				prompt.Close()
				return err
			}
		}
		prompt.Close()
	}
	if request.RuntimeType == RuntimeTypeOpenClaw && request.Action == "identity" {
		if text := request.Context[teamConfigFileName]; text != "" {
			return writeNativeTeamFile(root, teamConfigFileName, text, false, 0600, true)
		}
	}
	return nil
}
func writeNativeTeamFile(root *os.Root, name, text string, overlay bool, mode os.FileMode, private bool) error {
	info, err := root.Lstat(name)
	if err == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || st.Nlink != 1 || private && st.Uid != uint32(os.Geteuid()) {
			return ErrWorkspacePathEscape
		}
		if overlay {
			mode = info.Mode().Perm()
			file, err := root.Open(name)
			if err != nil {
				return err
			}
			actual, err := file.Stat()
			if err != nil {
				file.Close()
				return err
			}
			st, ok := actual.Sys().(*syscall.Stat_t)
			if !ok || !actual.Mode().IsRegular() || st.Nlink != 1 || private && st.Uid != uint32(os.Geteuid()) || !os.SameFile(info, actual) {
				file.Close()
				return ErrWorkspaceTextConflict
			}
			data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
			after, statErr := file.Stat()
			file.Close()
			if err != nil {
				return err
			}
			if statErr != nil {
				return statErr
			}
			if actual.Size() != after.Size() || !actual.ModTime().Equal(after.ModTime()) {
				return ErrWorkspaceTextConflict
			}
			if len(data) > 1<<20 {
				return ErrWorkspacePreviewTooLarge
			}
			text = mergeNativeTeamOverlay(string(data), text)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if overlay {
		text = mergeNativeTeamOverlay("", text)
	}
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".cm-team-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	defer root.Remove(temporary)
	if err = file.Chmod(mode); err == nil {
		_, err = file.WriteString(text)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	current, currentErr := root.Lstat(name)
	if info == nil {
		if !os.IsNotExist(currentErr) {
			if currentErr != nil {
				return currentErr
			}
			return ErrWorkspaceTextConflict
		}
	} else {
		if currentErr != nil {
			return currentErr
		}
		if !os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) {
			return ErrWorkspaceTextConflict
		}
	}
	return root.Rename(temporary, name)
}
func mergeNativeTeamOverlay(base, content string) string {
	if start := strings.Index(base, teamManagedOverlayStart); start >= 0 {
		if endOffset := strings.Index(base[start:], teamManagedOverlayEnd); endOffset >= 0 {
			end := start + endOffset + len(teamManagedOverlayEnd)
			base = strings.TrimRight(base[:start]+base[end:], " \t\r\n")
		}
	}
	if strings.TrimSpace(base) != "" {
		base = strings.TrimRight(base, " \t\r\n") + "\n\n"
	}
	return base + teamManagedOverlayStart + "\n" + strings.TrimSpace(content) + "\n" + teamManagedOverlayEnd + "\n"
}

func checkTeamNativeMounts(base, target string) error {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	within := func(parent, child string) bool {
		return parent == child || strings.HasPrefix(child, parent+string(filepath.Separator))
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := unescape.Replace(fields[4])
		if mount != base && within(base, mount) && (within(mount, target) || within(target, mount)) {
			return ErrWorkspacePathEscape
		}
	}
	return nil
}
