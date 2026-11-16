//go:build linux

package ownerrecords

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureHistoryReader(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
func killHistoryReader(cmd *exec.Cmd) {
	if cmd.Process != nil {
		if cmd.Process.Signal(syscall.Signal(0)) != nil {
			return
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func historyReaderGroupDrained(cmd *exec.Cmd) bool {
	if cmd.Process == nil {
		return true
	}
	group := cmd.Process.Pid
	if errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) {
		return true
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	found := false
	for _, entry := range entries {
		if _, err = strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		raw, e := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if e != nil {
			continue
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 3 {
			continue
		}
		pgrp, e := strconv.Atoi(fields[2])
		if e != nil || pgrp != group {
			continue
		}
		found = true
		if fields[0] != "Z" && fields[0] != "X" {
			return false
		}
	}
	// Zombies perform no I/O. Do not depend on the container's PID 1 reaping
	// adopted children promptly. Otherwise require kernel-confirmed disappearance.
	return found || errors.Is(syscall.Kill(-group, 0), syscall.ESRCH)
}
