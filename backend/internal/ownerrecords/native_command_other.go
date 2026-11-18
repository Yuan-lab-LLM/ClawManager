//go:build !linux

package ownerrecords

import "os/exec"

func configureNativeHistoryCommand(_ *exec.Cmd) {}
