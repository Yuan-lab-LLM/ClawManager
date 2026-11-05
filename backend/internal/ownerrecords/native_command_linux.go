//go:build linux

package ownerrecords

import (
	"os/exec"
	"syscall"
)

func configureNativeHistoryCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
