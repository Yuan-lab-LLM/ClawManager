package ownerrecords

import (
	"io"
	"os"
)

func checkOpenClawSQLiteNetworkMode(path string) error {
	network, err := openClawSQLiteNetworkFilesystem(path)
	if err != nil {
		return err
	}
	if !network {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 100)
	if _, err := io.ReadFull(f, header); err != nil {
		return errHistoryRestricted
	}
	if string(header[:16]) != "SQLite format 3\x00" || header[18] != 1 || header[19] != 1 {
		return errHistoryRestricted
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() > 0 {
		return errHistoryRestricted
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
