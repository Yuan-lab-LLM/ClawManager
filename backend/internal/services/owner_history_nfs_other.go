//go:build !linux

package services

import (
	"fmt"
	"io"
)

func RunOwnerHistoryHelper(args []string, _ io.Reader, _ io.Writer) (bool, error) {
	if len(args) > 0 && args[0] == ownerHistoryHelperCommand {
		return true, fmt.Errorf("private NFS history requires Linux")
	}
	return false, nil
}
