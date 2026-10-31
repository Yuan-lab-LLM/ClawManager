package systembackup

import (
	"errors"
	"os"
)

func restoreFileOwner(_ *os.Root, r FileRecord) error {
	if r.UID != 0 || r.GID != 0 {
		return errors.New("Unix file ownership requires a Unix restore target")
	}
	return nil
}
