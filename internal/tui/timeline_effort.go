package tui

import (
	"slices"
	"time"

	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) timelineWorklogs() []worklogs.LocalWorklog {
	if m.dayContext.Date == m.selectedDate.Format("2006-01-02") && m.dayContext.TimelineWorklogs != nil {
		return m.dayContext.TimelineWorklogs
	}
	return m.worklogs
}

func timelineDayInterval(record worklogs.LocalWorklog, day time.Time) (time.Time, time.Time) {
	start := record.StartedAtUTC.In(day.Location())
	end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
	if start.Before(day) {
		start = day
	}
	if next := day.AddDate(0, 0, 1); end.After(next) {
		end = next
	}
	return start, end
}

// Count actual logged durations in chronological order, including concurrent
// records as the day balance does. Gaps and unbooked lunch consume no quota.
func timelineOvertimeStart(records []worklogs.LocalWorklog, day time.Time, quota time.Duration) (time.Time, bool) {
	type event struct {
		at    time.Time
		delta int
	}
	events := make([]event, 0, 2*len(records))
	for _, record := range records {
		start, end := timelineDayInterval(record, day)
		if start.Before(end) {
			events = append(events, event{start, 1}, event{end, -1})
		}
	}
	slices.SortFunc(events, func(a, b event) int { return a.at.Compare(b.at) })
	var previous time.Time
	var logged time.Duration
	active := 0
	for _, event := range events {
		if active > 0 {
			duration := event.at.Sub(previous) * time.Duration(active)
			if logged+duration > quota {
				return previous.Add((quota - logged) / time.Duration(active)), true
			}
			logged += duration
		}
		active += event.delta
		previous = event.at
	}
	return time.Time{}, false
}
