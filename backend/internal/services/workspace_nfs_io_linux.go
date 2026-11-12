//go:build linux

package services

import (
	"bytes"
	"clawreef/internal/ownerrecords"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type workspaceNFSOutput struct{ bytes.Buffer }

func (b *workspaceNFSOutput) Write(p []byte) (int, error) {
	if len(p) > workspaceNFSResponseLimit-b.Len() {
		return 0, fmt.Errorf("workspace helper response too large")
	}
	return b.Buffer.Write(p)
}

func runWorkspaceNFSHelper(ctx context.Context, request workspaceNFSRequest, input io.Reader, audit func(workspaceNFSEvent) error) (response workspaceNFSResponse, file *os.File, ended bool, result error) {
	return runWorkspaceNFSHelperCommand(ctx, WorkspaceIOHelperCommand, request, input, audit)
}

func runWorkspaceNFSHelperCommand(ctx context.Context, command string, request workspaceNFSRequest, input io.Reader, audit func(workspaceNFSEvent) error) (response workspaceNFSResponse, file *os.File, ended bool, result error) {
	data, err := json.Marshal(request)
	if err != nil || len(data) > workspaceNFSHeaderLimit {
		return response, nil, true, ErrWorkspacePathInvalid
	}
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return response, nil, true, err
	}
	left, right := os.NewFile(uintptr(pair[0]), "workspace-parent"), os.NewFile(uintptr(pair[1]), "workspace-child")
	defer left.Close()
	defer right.Close()
	connection, err := net.FileConn(left)
	if err != nil {
		return response, nil, true, err
	}
	control := connection.(*net.UnixConn)
	defer control.Close()
	executable, err := os.Executable()
	if err != nil {
		return response, nil, true, err
	}
	exe, err := os.Open(executable)
	if err != nil {
		return response, nil, true, err
	}
	defer exe.Close()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	readers := []io.Reader{bytes.NewReader(header[:]), bytes.NewReader(data)}
	if input != nil {
		readers = append(readers, io.LimitReader(input, request.Size+1))
	}
	cmd := exec.CommandContext(ctx, "/proc/self/fd/4", command)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "TMPDIR=/tmp", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdin = io.MultiReader(readers...)
	cmd.ExtraFiles = []*os.File{right, exe}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(request.UID), Gid: uint32(request.GID), Groups: []uint32{}}, Pdeathsig: syscall.SIGKILL}
	if command == ownerHistoryHelperCommand {
		cmd.SysProcAttr.Setpgid = true
	}
	cmd.WaitDelay = 5 * time.Second
	var output workspaceNFSOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return response, nil, true, err
	}
	right.Close()
	uncertain := func(err error) error {
		if workspaceNFSMutation(request.Action) != "" || command == workspaceManagementHelperCommand && workspaceManagementMutation(request.Action) {
			return errors.Join(ErrWorkspaceNFSUncertain, err)
		}
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		control.Close()
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer stop()
	var controlErr, auditFailure error
	for {
		payload, oob := make([]byte, 32<<10), make([]byte, syscall.CmsgSpace(4))
		n, anc, flags, _, readErr := control.ReadMsgUnix(payload, oob)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && ctx.Err() == nil {
				controlErr = readErr
			}
			break
		}
		if n == 0 && anc == 0 {
			break
		}
		messages, parseErr := syscall.ParseSocketControlMessage(oob[:anc])
		var rights []int
		if parseErr == nil {
			for _, m := range messages {
				fds, e := syscall.ParseUnixRights(&m)
				if e != nil {
					parseErr = e
					break
				}
				rights = append(rights, fds...)
			}
		}
		closeRights := func() {
			for _, fd := range rights {
				syscall.Close(fd)
			}
		}
		var event workspaceNFSEvent
		d := json.NewDecoder(bytes.NewReader(payload[:n]))
		d.DisallowUnknownFields()
		if parseErr != nil || flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0 || d.Decode(&event) != nil || d.Decode(new(any)) != io.EOF {
			closeRights()
			controlErr = fmt.Errorf("invalid workspace helper control receipt")
			break
		}
		if event.Kind == "fd" {
			if (request.Action != "download" && request.Action != "open_preview" && command != workspaceOperationLockHelperCommand) || file != nil || len(rights) != 1 {
				closeRights()
				controlErr = fmt.Errorf("unexpected workspace file descriptor")
				break
			}
			syscall.CloseOnExec(rights[0])
			file = os.NewFile(uintptr(rights[0]), "workspace-download")
			info, e := file.Stat()
			mode, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFL, 0)
			if e != nil || !info.Mode().IsRegular() || errno != 0 || mode&syscall.O_ACCMODE != syscall.O_RDONLY && !(command == workspaceOperationLockHelperCommand && mode&syscall.O_ACCMODE == syscall.O_RDWR) {
				controlErr = fmt.Errorf("workspace descriptor is not a regular read-only file")
				break
			}
			continue
		}
		if len(rights) != 0 || audit == nil || event.Kind != "audit" {
			closeRights()
			controlErr = fmt.Errorf("unexpected workspace audit receipt")
			break
		}
		auditErr := audit(event)
		ack, _ := json.Marshal(workspaceNFSEvent{Kind: "audit_ack", OK: auditErr == nil})
		if _, e := control.Write(ack); e != nil {
			controlErr = e
			break
		}
		if auditErr != nil {
			auditFailure = auditErr
		}
	}
	if controlErr != nil {
		_ = cmd.Process.Kill()
		control.Close()
	}
	waitErr := cmd.Wait()
	ended = cmd.ProcessState != nil && cmd.ProcessState.Exited()
	// Signaled fixed Go helpers spawn no subprocesses; Wait reaps the only
	// filesystem writer even when cancellation terminates it.
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			ended = true
		}
	}
	if command == ownerHistoryHelperCommand && !ownerrecords.HistoryReaderGroupDrained(cmd) {
		ended = false
		waitErr = errors.Join(waitErr, fmt.Errorf("native history reader descendants have not exited"))
	}
	if waitErr != nil || controlErr != nil || auditFailure != nil || ctx.Err() != nil {
		if file != nil {
			file.Close()
		}
		err := errors.Join(ctx.Err(), controlErr, auditFailure, waitErr)
		if waitErr != nil || controlErr != nil || ctx.Err() != nil {
			err = uncertain(err)
		}
		return response, nil, ended, err
	}
	d := json.NewDecoder(bytes.NewReader(output.Bytes()))
	d.DisallowUnknownFields()
	if d.Decode(&response) != nil || d.Decode(new(any)) != io.EOF {
		if file != nil {
			file.Close()
		}
		return response, nil, ended, uncertain(fmt.Errorf("invalid workspace helper response"))
	}
	if err = workspaceNFSError(response.Error); err != nil {
		if file != nil {
			file.Close()
		}
		if response.Error == "failed" {
			err = uncertain(err)
		}
		return response, nil, ended, err
	}
	wantFD := request.Action == "download" || request.Action == "open_preview" || command == workspaceOperationLockHelperCommand
	if wantFD != (file != nil) {
		if file != nil {
			file.Close()
		}
		return response, nil, ended, fmt.Errorf("workspace descriptor missing")
	}
	if file != nil {
		info, err := file.Stat()
		if err != nil || info.Size() != response.Size {
			file.Close()
			return response, nil, ended, ErrWorkspaceTextConflict
		}
	}
	return response, file, ended, nil
}

func validateWorkspaceNFSHelperPath(request workspaceNFSRequest) error {
	for current := request.Scope.WorkspacePath; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrWorkspacePathEscape
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return ErrWorkspacePathInvalid
		}
		if current == request.Scope.WorkspacePath {
			owner := int(st.Uid)
			if request.Scope.TeamID > 0 {
				if owner != 0 && owner != 10001 && owner != teamSharedUID {
					return os.ErrPermission
				}
			} else if owner != 0 && owner != 10001 && owner != request.UID {
				return os.ErrPermission
			}
		} else if (st.Uid != 0 && st.Uid != 10001) || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
			return ErrWorkspacePathEscape
		}
		if current == "/" {
			break
		}
	}
	return nil
}

func readWorkspaceNFSHelperRequest(input io.Reader) (workspaceNFSRequest, error) {
	// No service initialization, environment credentials, arbitrary executable or
	// extra descriptor is inherited. Protect the private control socket from any
	// process with a matching filesystem UID before reading the request.
	if _, _, err := syscall.Syscall6(syscall.SYS_PRCTL, 4, 0, 0, 0, 0, 0); err != 0 {
		return workspaceNFSRequest{}, err
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return workspaceNFSRequest{}, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[0] == "CapPrm:" || fields[0] == "CapEff:" || fields[0] == "CapInh:" || fields[0] == "CapAmb:") && fields[1] != "0000000000000000" {
			return workspaceNFSRequest{}, os.ErrPermission
		}
	}
	syscall.Close(4)
	var prefix [4]byte
	if _, err := io.ReadFull(input, prefix[:]); err != nil {
		return workspaceNFSRequest{}, err
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size == 0 || size > workspaceNFSHeaderLimit {
		return workspaceNFSRequest{}, ErrWorkspacePathInvalid
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(input, data); err != nil {
		return workspaceNFSRequest{}, err
	}
	var request workspaceNFSRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return workspaceNFSRequest{}, ErrWorkspacePathInvalid
	}
	return request, nil
}

func RunWorkspaceIOHelper(args []string, input io.Reader, output io.Writer) (bool, error) {
	if handled, err := RunOwnerHistoryHelper(args, input, output); handled {
		return true, err
	}
	if len(args) > 0 && args[0] == workspaceOperationLockHelperCommand {
		return true, runWorkspaceOperationLockHelper(args, input, output)
	}
	if handled, err := RunTeamWorkspaceHelper(args, input, output); handled {
		return true, err
	}
	if len(args) > 0 && args[0] == workspaceManagementHelperCommand {
		return true, runWorkspaceManagementHelper(args, input, output)
	}
	if len(args) == 0 || args[0] != WorkspaceIOHelperCommand {
		return false, nil
	}
	if len(args) != 1 {
		return true, fmt.Errorf("invalid workspace helper command")
	}
	request, err := readWorkspaceNFSHelperRequest(input)
	if err != nil {
		return true, err
	}
	uid, gid, err := validateWorkspaceNFSScope(request.Root, request.Scope)
	if err != nil {
		return true, err
	}
	if request.Scope.TeamID > 0 && (request.Action == "save_text" || request.Action == "upload") && request.UID > RuntimeLinuxIDBase && uint64(request.UID) < 1<<32-1 {
		uid = request.UID
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || request.UID != uid || request.GID != gid || os.Getuid() != uid || os.Geteuid() != uid || os.Getgid() != gid || os.Getegid() != gid {
		return true, os.ErrPermission
	}
	if err = validateWorkspaceNFSHelperPath(request); err != nil {
		return true, json.NewEncoder(output).Encode(workspaceNFSResponse{Error: workspaceNFSErrorCode(err)})
	}
	if request.UploadMax <= 0 || request.Size < 0 {
		return true, ErrWorkspaceUploadTooLarge
	}
	if err = os.Setenv(WorkspaceUploadMaxBytesEnv, strconv.FormatInt(request.UploadMax, 10)); err != nil {
		return true, err
	}
	if request.Scope.TeamID > 0 {
		syscall.Umask(0007)
	}
	f := os.NewFile(3, "workspace-control")
	connection, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return true, err
	}
	control := connection.(*net.UnixConn)
	defer control.Close()
	scope := request.Scope
	scope.credentialScoped = true
	scope.uploadIdentity = request.Version
	s := &workspaceFileService{auditHook: func(action, path string, size int64) error {
		data, err := json.Marshal(workspaceNFSEvent{Kind: "audit", Action: action, Path: path, Bytes: size})
		if err != nil || len(data) > 32<<10 {
			return fmt.Errorf("invalid workspace audit metadata")
		}
		if _, err = control.Write(data); err != nil {
			return err
		}
		reply := make([]byte, 256)
		n, err := control.Read(reply)
		if err != nil {
			return err
		}
		var ack workspaceNFSEvent
		if json.Unmarshal(reply[:n], &ack) != nil || ack.Kind != "audit_ack" || !ack.OK {
			return fmt.Errorf("workspace audit authorization failed")
		}
		return nil
	}}
	var response workspaceNFSResponse
	var download io.ReadCloser
	ctx := context.Background()
	switch request.Action {
	case "list":
		response.Entries, err = s.List(ctx, scope, request.Path)
	case "search":
		response.Entries, err = s.Search(ctx, scope, request.Query, request.Limit)
	case "preview":
		response.Preview, err = s.Preview(ctx, scope, request.Path)
	case "git_read":
		response.GitData, err = s.ReadGitFile(ctx, scope, request.Path)
	case "read_text":
		response.Text, err = s.ReadText(ctx, scope, request.Path)
	case "save_text":
		response.Text, err = s.saveText(ctx, scope, request.Path, request.Text, request.Version)
	case "upload":
		response.Entry, err = s.upload(ctx, scope, request.Path, request.Name, &exactWorkspaceUpload{reader: input, remaining: request.Size}, request.Size)
	case "mkdir":
		response.Entry, err = s.mkdir(ctx, scope, request.Path)
	case "rename":
		response.Entry, err = s.rename(ctx, scope, request.Path, request.Next)
	case "delete":
		err = s.delete(ctx, scope, request.Path)
	case "open_preview":
		var file *os.File
		file, response.Name, response.Size, err = s.OpenPreview(ctx, scope, request.Path)
		if file != nil {
			download = file
		}
	case "download":
		download, response.Name, response.Size, err = s.OpenDownload(ctx, scope, request.Path)
	case "upload_owner":
		if scope.TeamID == 0 {
			err = os.ErrPermission
			break
		}
		var root *os.Root
		var name string
		root, name, err = openWorkspaceFileParent(scope, request.Path)
		if err == nil {
			defer root.Close()
			var info os.FileInfo
			info, err = inspectWorkspaceUpload(root, name)
			if err == nil {
				response.Owner = 10001
				response.UploadIdentity = workspaceUploadIdentity(info)
				if info != nil {
					st, ok := info.Sys().(*syscall.Stat_t)
					if !ok || int(st.Gid) != teamSharedGID {
						err = os.ErrPermission
					} else {
						response.Owner = int(st.Uid)
					}
				}
			}
		}
	case "text_owner":
		if scope.TeamID == 0 {
			err = os.ErrPermission
			break
		}
		var root *os.Root
		var name string
		root, name, err = openTextParent(scope, request.Path)
		if err == nil {
			defer root.Close()
			var info os.FileInfo
			_, info, err = readTextAt(root, name)
			if err == nil {
				st, ok := info.Sys().(*syscall.Stat_t)
				if !ok || int(st.Gid) != teamSharedGID {
					err = os.ErrPermission
				} else {
					response.Owner = int(st.Uid)
				}
			}
		}
	default:
		err = ErrWorkspacePathInvalid
	}
	if download != nil {
		defer download.Close()
	}
	if err == nil && download != nil {
		fd, ok := download.(interface{ Fd() uintptr })
		if !ok {
			return true, fmt.Errorf("workspace download is not a file")
		}
		readOnly, openErr := os.Open(fmt.Sprintf("/proc/self/fd/%d", fd.Fd()))
		if openErr != nil {
			return true, openErr
		}
		defer readOnly.Close()
		data, _ := json.Marshal(workspaceNFSEvent{Kind: "fd"})
		_, _, err = control.WriteMsgUnix(data, syscall.UnixRights(int(readOnly.Fd())), nil)
	}
	response.Error = workspaceNFSErrorCode(err)
	return true, json.NewEncoder(output).Encode(response)
}

func workspaceNFSTransportUncertain(err error) bool {
	for _, transport := range []error{syscall.EIO, syscall.ESTALE, syscall.ETIMEDOUT, syscall.ENOTCONN, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EHOSTDOWN, syscall.EHOSTUNREACH, syscall.ENETDOWN, syscall.ENETUNREACH, syscall.ENOLINK} {
		if errors.Is(err, transport) {
			return true
		}
	}
	return false
}
