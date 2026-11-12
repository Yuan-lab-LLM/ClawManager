//go:build linux

package ownerrecords

import "syscall"

func openClawSQLiteNetworkFilesystem(path string) (bool, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return false, err
	}
	// Match the pinned OpenClaw sqlite-wal contract: NFS, SMB/CIFS and 9p
	// select rollback journaling. A cross-host WAL read is never certified.
	switch uint64(fs.Type) {
	case 0x6969, 0x517b, 0xff534d42, 0xfe534d42, 0x01021997:
		return true, nil
	}
	return false, nil
}
