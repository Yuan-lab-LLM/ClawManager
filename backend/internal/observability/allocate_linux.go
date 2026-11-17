//go:build linux

package observability

import (
	"os"
	"syscall"
)

// Fallocate reserves physical blocks without writing the reservation through the
// page cache. Filesystems without this guarantee use the explicit zero writer.
func preallocateJournal(f *os.File, size int64) error {
	if err := syscall.Fallocate(int(f.Fd()), 0, 0, size); err == nil {
		return nil
	} else if err != syscall.EOPNOTSUPP && err != syscall.ENOSYS {
		return err
	}
	return writeJournalReservation(f, size)
}
