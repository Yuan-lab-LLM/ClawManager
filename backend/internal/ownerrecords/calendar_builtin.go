package ownerrecords

import _ "embed"

// Confirmed official dates travel with the binary; reads never seed or overwrite
// the database. Published administrator revisions always take precedence.
//
//go:embed calendars/cn-2026.json
var calendarCN2026 []byte

func builtinCalendar(year int) CalendarVersion {
	if year != 2026 {
		return CalendarVersion{}
	}
	// Parse into request-owned slices so uploads cannot mutate the default.
	d, err := ParseCalendar(calendarCN2026)
	if err != nil {
		// Invalid bundled data is caught by TestBuiltinCalendar2026. Keep the
		// existing provisional weekend fallback if the asset is ever corrupted.
		return CalendarVersion{}
	}
	return CalendarVersion{Document: d, Builtin: true}
}

func builtinCalendars() map[int]CalendarVersion {
	return map[int]CalendarVersion{2026: builtinCalendar(2026)}
}
