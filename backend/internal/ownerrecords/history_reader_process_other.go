//go:build !linux

package ownerrecords

import "os/exec"

func configureHistoryReader(cmd *exec.Cmd)         {}
func historyReaderGroupDrained(cmd *exec.Cmd) bool { return true }
func killHistoryReader(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
