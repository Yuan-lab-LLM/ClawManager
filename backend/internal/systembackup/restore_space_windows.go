package systembackup

import "golang.org/x/sys/windows"

func availableRestoreBytes(path string) (uint64, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var available uint64
	e = windows.GetDiskFreeSpaceEx(p, &available, nil, nil)
	return available, e
}
