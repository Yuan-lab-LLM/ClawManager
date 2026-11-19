//go:build !linux

package observability

import "os"

func preallocateJournal(f *os.File, start, end int64) error {
	return writeJournalReservation(f, start, end)
}
