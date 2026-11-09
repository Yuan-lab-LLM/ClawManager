//go:build linux

package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestNFSOwnerHistoryLandlockBlocksForeignReadsAndWrites(t *testing.T) {
	if root := os.Getenv("CM_OWNER_LANDLOCK_CHILD"); root != "" {
		runtime.LockOSThread()
		workspace := filepath.Join(root, "instance")
		if err := restrictOwnerHistoryReads(root, workspace, filepath.Join(workspace, "own")); err != nil {
			os.Exit(10)
		}
		if data, err := os.ReadFile(filepath.Join(workspace, "own")); err != nil || string(data) != "own" {
			os.Exit(11)
		}
		if _, err := os.ReadFile(filepath.Join(workspace, "escape")); !os.IsPermission(err) {
			os.Exit(12)
		}
		if err := os.WriteFile(filepath.Join(workspace, "own"), []byte("changed"), 0600); !os.IsPermission(err) {
			os.Exit(13)
		}
		ready, proceed := os.NewFile(4, "ready"), os.NewFile(5, "proceed")
		if _, err := ready.Write([]byte{1}); err != nil {
			os.Exit(14)
		}
		var signal [1]byte
		if _, err := proceed.Read(signal[:]); err != nil {
			os.Exit(15)
		}
		if _, err := os.ReadFile(filepath.Join(workspace, "own")); !os.IsPermission(err) {
			os.Exit(16)
		}
		os.Exit(0)
	}
	if os.Geteuid() != 0 {
		t.Skip("trusted credential helper requires root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-owner-landlock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := filepath.Join(base, "instance")
	os.Mkdir(workspace, 0755)
	os.Chown(workspace, 200012, 200012)
	own := filepath.Join(workspace, "own")
	os.WriteFile(own, []byte("own"), 0600)
	os.Chown(own, 200012, 200012)
	foreign := filepath.Join(base, "foreign")
	os.WriteFile(foreign, []byte("world-readable foreign data"), 0644)
	os.Symlink(foreign, filepath.Join(workspace, "escape"))
	executable, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyRead.Close()
	defer readyWrite.Close()
	proceedRead, proceedWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer proceedRead.Close()
	defer proceedWrite.Close()
	cmd := exec.Command("/proc/self/fd/3", "-test.run=^TestNFSOwnerHistoryLandlockBlocksForeignReadsAndWrites$")
	cmd.ExtraFiles = []*os.File{executable, readyWrite, proceedRead}
	cmd.Env = append(os.Environ(), "CM_OWNER_LANDLOCK_CHILD="+base)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 200012, Gid: 200012, Groups: []uint32{}}}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	readyWrite.Close()
	proceedRead.Close()
	var signal [1]byte
	if _, err := readyRead.Read(signal[:]); err != nil {
		cmd.Wait()
		t.Fatalf("helper failed before inode replacement: %v", err)
	}
	// Replace the checked pathname after Landlock has captured the original
	// inode. The parser's later open must never inherit its authority by name.
	if err := os.Rename(foreign, own); err != nil {
		proceedWrite.Close()
		cmd.Wait()
		t.Fatal(err)
	}
	if _, err := proceedWrite.Write([]byte{1}); err != nil {
		cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("read-only inode fence failed: %v", err)
	}
}
