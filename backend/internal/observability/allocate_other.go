//go:build !linux

package observability

import "os"

func preallocateJournal(f *os.File, size int64) error { return writeJournalReservation(f, size) }
