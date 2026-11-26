//go:build !linux

package systembackuptrial

import "os"

func publishWorkspaceFiles(_ *os.Root, _, _ string) error { return errWorkspaceFiles }

func inheritWorkspaceDirectory(_, _ *os.Root, _ string) error { return errWorkspaceFiles }
