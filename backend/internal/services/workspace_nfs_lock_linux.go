//go:build linux

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

func (s *RuntimeUpgradeService) lockNFSWorkspaceOperations(ids []int) (func(), error) {
	var files []*os.File
	release := func() {
		for i := len(files) - 1; i >= 0; i-- {
			_ = files[i].Close()
		}
	}
	for _, id := range normalizedPositiveIDs(ids) {
		request := workspaceNFSRequest{Root: s.workspaceRoot, Scope: WorkspaceFileScope{InstanceID: id}, Action: "operation_lock", UID: 10001, GID: 10001}
		_, file, _, err := runWorkspaceNFSHelperCommand(context.Background(), workspaceOperationLockHelperCommand, request, nil, nil)
		if err != nil {
			release()
			return nil, err
		}
		files = append(files, file)
	}
	return release, nil
}

func runWorkspaceOperationLockHelper(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 {
		return ErrWorkspacePathInvalid
	}
	request, err := readWorkspaceNFSHelperRequest(input)
	if err != nil {
		return err
	}
	if request.Action != "operation_lock" || request.Scope.InstanceID <= 0 || !filepath.IsAbs(request.Root) || filepath.Clean(request.Root) != request.Root || request.Root == "/" {
		return ErrWorkspacePathInvalid
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || request.UID != 10001 || request.GID != 10001 || os.Getuid() != 10001 || os.Geteuid() != 10001 || os.Getgid() != 10001 || os.Getegid() != 10001 {
		return os.ErrPermission
	}
	validation := request
	validation.Scope.WorkspacePath = request.Root
	if err = validateWorkspaceNFSHelperPath(validation); err != nil {
		return json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
	}
	file, err := openNFSOperationLock(request.Root, request.Scope.InstanceID)
	if err != nil {
		return json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	channel := os.NewFile(3, "workspace-operation-control")
	connection, err := net.FileConn(channel)
	channel.Close()
	if err != nil {
		return err
	}
	defer connection.Close()
	payload, _ := json.Marshal(workspaceNFSEvent{Kind: "fd"})
	// Send the original open file description. Reopening via /proc would lose
	// its flock, and explicit LOCK_UN would release the parent's transferred lock.
	if _, _, err = connection.(*net.UnixConn).WriteMsgUnix(payload, syscall.UnixRights(int(file.Fd())), nil); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(workspaceNFSResponse{Size: info.Size()})
}

func openNFSOperationLock(rootPath string, instanceID int) (*os.File, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { root.Close() }()
	for _, part := range []string{".clawmanager-upgrades", "operations"} {
		if err = root.Mkdir(part, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 10001 || info.Mode().Perm() != 0700 {
			return nil, os.ErrPermission
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, err
		}
		pinned, err := next.Stat(".")
		if err != nil || !os.SameFile(info, pinned) {
			next.Close()
			return nil, ErrWorkspacePathEscape
		}
		root.Close()
		root = next
	}
	name := fmt.Sprintf("instance-%d.lock", instanceID)
	file, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != 10001 || info.Mode().Perm() != 0600 {
		file.Close()
		return nil, os.ErrPermission
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrWorkspaceUpgradeBusy
		}
		return nil, err
	}
	return file, nil
}
