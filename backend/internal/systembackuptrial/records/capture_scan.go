package records

import (
	"context"
	"database/sql"
)

// Scratch space belongs to one table reader. Returned values are consumed by
// rawCell before the next scan; they must never be placed directly in Snapshot.
type captureScanSlots struct {
	values   []any
	pointers []any
}

func newCaptureScanSlots(columns int) captureScanSlots {
	s := captureScanSlots{values: make([]any, columns), pointers: make([]any, columns)}
	for i := range s.values {
		s.pointers[i] = &s.values[i]
	}
	return s
}

func (s captureScanSlots) scan(ctx context.Context, rows *sql.Rows) ([]any, error) {
	clear(s.values)
	finish := BeginCaptureTiming(ctx, CaptureRowScan)
	err := rows.Scan(s.pointers...)
	finish.Finish(err)
	if err != nil {
		clear(s.values) // a driver may have filled only part of the row
		return nil, err
	}
	return s.values, nil
}
