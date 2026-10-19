package ownerrecords

import "time"

// WorkdayStreak counts confirmed question-active weekdays. Saturday/Sunday are
// skipped, not treated as holidays or makeup workdays. An unfinished inactive
// today does not break the streak until the next workday. This helper does not
// turn incomplete collection into a real dashboard value.
func WorkdayStreak(dates []string, now time.Time) int {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return 0
	}
	now = now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	active := map[string]bool{}
	for _, raw := range dates {
		if d, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil && !d.After(day) {
			active[raw] = true
		}
	}
	weekend := func(d time.Time) bool { return d.Weekday() == time.Saturday || d.Weekday() == time.Sunday }
	if !weekend(day) && !active[day.Format("2006-01-02")] {
		day = day.AddDate(0, 0, -1)
	}
	count := 0
	for {
		if !weekend(day) {
			if !active[day.Format("2006-01-02")] {
				return count
			}
			count++
		}
		day = day.AddDate(0, 0, -1)
	}
}
