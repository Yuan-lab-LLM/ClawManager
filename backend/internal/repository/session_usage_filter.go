package repository

import (
	"fmt"
	"time"
)

// SessionUsageFilter optionally bounds session usage aggregates by timestamp.
type SessionUsageFilter struct {
	Since *time.Time
	Until *time.Time
}

func appendTimeFilter(query string, args []interface{}, filter SessionUsageFilter, column string) (string, []interface{}) {
	if filter.Since != nil {
		query += fmt.Sprintf(" AND %s >= ?", column)
		args = append(args, *filter.Since)
	}
	if filter.Until != nil {
		query += fmt.Sprintf(" AND %s < ?", column)
		args = append(args, *filter.Until)
	}
	return query, args
}
