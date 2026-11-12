package ownerrecords

import (
	"context"
	"fmt"
	"time"
)

type PortalUser struct {
	Owner      string        `json:"owner"`
	XP         int64         `json:"xp"`
	Level      int           `json:"level"`
	Tokens     uint64        `json:"tokens"`
	Questions  *uint64       `json:"questions"`
	Sessions   uint64        `json:"sessions"`
	TasksDone  int           `json:"tasks_done"`
	TasksTotal int           `json:"tasks_total"`
	SnapshotAt string        `json:"snapshot_at"`
	Visits     VisitCalendar `json:"visits"`
	Ready      bool          `json:"ready"`
}
type PortalUsers struct {
	Users []PortalUser `json:"users"`
	Total int          `json:"total"`
	Page  int          `json:"page"`
	AsOf  time.Time    `json:"as_of"`
}

type CalendarPreview struct {
	Document        CalendarDocument `json:"document"`
	ExpectedVersion uint64           `json:"expected_version"`
	Added           int              `json:"added"`
	Changed         int              `json:"changed"`
	Removed         int              `json:"removed"`
	AffectedUsers   int              `json:"affected_users"`
	Examples        []map[string]any `json:"examples"`
}

func (s *Store) PreviewCalendar(ctx context.Context, d CalendarDocument, now time.Time) (CalendarPreview, error) {
	heads, e := s.calendarHeads(ctx)
	if e != nil {
		return CalendarPreview{}, e
	}
	old := heads[d.Year]
	merged, e := mergeCalendar(old.Document, d)
	if e != nil {
		return CalendarPreview{}, e
	}
	out := CalendarPreview{Document: merged, ExpectedVersion: old.Version, Examples: []map[string]any{}}
	prev := map[string]CalendarDay{}
	for _, x := range old.Document.Days {
		prev[x.Date] = x
	}
	for _, x := range merged.Days {
		if p, ok := prev[x.Date]; !ok {
			out.Added++
		} else if p != x {
			out.Changed++
		}
		delete(prev, x.Date)
	}
	out.Removed = len(prev)
	// One saved-facts read per preview, not one database request per user.
	users, e := s.portalUsers(ctx, heads, now)
	if e != nil {
		return out, e
	}
	nextHeads := map[int]CalendarVersion{}
	for y, v := range heads {
		nextHeads[y] = v
	}
	nextHeads[d.Year] = CalendarVersion{Document: merged, Version: old.Version + 1}
	// Reuse the bounded aggregate reader for a coherent before/after comparison.
	next, e := s.portalUsers(ctx, nextHeads, now)
	if e != nil {
		return out, e
	}
	if len(users) != len(next) {
		return out, fmt.Errorf("统计数据正在变化，请重新预览")
	}
	for i, u := range users {
		v := next[i]
		if u.Owner != v.Owner {
			return out, fmt.Errorf("统计数据正在变化，请重新预览")
		}
		if u.Visits.Streak != v.Visits.Streak || u.Visits.Longest != v.Visits.Longest {
			out.AffectedUsers++
			if len(out.Examples) < 10 {
				out.Examples = append(out.Examples, map[string]any{"owner": u.Owner, "before": u.Visits.Streak, "after": v.Visits.Streak})
			}
		}
	}
	return out, nil
}
