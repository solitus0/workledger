package tui

import (
	"math"
	"strings"
	"time"

	"github.com/solitus0/workledger/internal/worklogs"
)

type timelineState uint8

const (
	timelineFree timelineState = iota
	timelineBooked
	timelineOvertime
	timelineLunch
	timelineProposed
	timelineConflict
)

type timelineSpan struct{ first, end int }
type timelinePlan struct {
	scale       timelineScale
	cells       []timelineState
	divisions   map[int]int
	selected    timelineSpan
	framed      bool
	visibility  timelineVisibility
	ruler       string
	lunchLabel  string
	lunchColumn int
}

// buildTimelinePlan reads domain state and computes geometry without styling
// or drawing. Selection never participates in the occupancy calculation.
func (m model) buildTimelinePlan(previewRecords []worklogs.LocalWorklog, selectedID string, width int) (timelinePlan, bool) {
	visibility := timelineVisibility{}
	startText, endText := m.workdayBounds()
	workdayStart, startErr := m.clockOnSelectedDay(startText)
	workdayEnd, endErr := m.clockOnSelectedDay(endText)
	if startErr != nil || endErr != nil || !workdayStart.Before(workdayEnd) {
		return timelinePlan{}, false
	}
	bookings := m.timelineWorklogs()
	records := appendTimelineRecords(bookings, previewRecords)
	day := beginningOfDay(m.selectedDate, m.workspace.cfg.Location)
	overtimeStart, hasOvertime := timelineOvertimeStart(records, day, time.Duration(m.dailyQuotaSeconds())*time.Second)
	visibility.overtime = hasOvertime
	timelineStart, timelineEnd := workdayStart, workdayEnd
	for _, record := range records {
		start, end := timelineDayInterval(record, day)
		if !start.Before(end) {
			continue
		}
		if start.Before(timelineStart) {
			timelineStart = start
		}
		if end.After(timelineEnd) {
			timelineEnd = end
		}
	}
	framed := width >= 60 && calculateLayout(m.width, m.height, m.activityVisible()).workspace.height >= 23
	availableWidth := max(3, width)
	if framed {
		availableWidth = max(3, width-4)
	}
	scale := newTimelineScale(timelineStart, timelineEnd, availableWidth)
	barWidth := scale.width
	selectedStart, selectedEnd, hasSelection := m.selectedTimelineInterval(selectedID, timelineStart, timelineEnd)
	visibility.selected = hasSelection
	cells := make([]timelineState, barWidth)
	for index := range cells {
		local := scale.midpoint(index)
		state := timelineFree
		persisted := worklogAt(bookings, local)
		if persisted != nil {
			if hasOvertime && !local.Before(overtimeStart) {
				state = timelineOvertime
			} else {
				state = timelineBooked
			}
		} else if m.inLunch(local) {
			state = timelineLunch
		} else if m.dayContext.Date != "" && m.dayContext.FreeSlots != nil && !containsFreeSlot(m.dayContext.FreeSlots, local) &&
			!(m.lunchAvailableForPlacement() && m.inConfiguredLunch(local)) {
			state = timelineBooked
		}
		if preview := worklogAt(previewRecords, local); preview != nil {
			if persisted != nil && m.form != nil && m.form.previewConflict != "" {
				state = timelineConflict
			} else if hasOvertime && !local.Before(overtimeStart) {
				state = timelineOvertime
			} else {
				state = timelineProposed
			}
		}
		cells[index] = state
	}
	divisions := timelineCellColumns(scale, timelineQuarter)
	for _, state := range cells {
		visibility.free = visibility.free || state == timelineFree
		visibility.lunch = visibility.lunch || state == timelineLunch
	}
	selected := timelineSpan{}
	if hasSelection {
		// Match the bar's midpoint sampling. A sub-column interval gets a
		// caret without changing occupancy, the ruler, or timeline height.
		first, end := max(0, scale.column(selectedStart)), min(barWidth, scale.column(selectedEnd))
		if first >= end {
			midpoint := selectedStart.Add(selectedEnd.Sub(selectedStart) / 2)
			first = min(barWidth-1, max(0, int(float64(midpoint.Sub(timelineStart))*float64(barWidth)/float64(timelineEnd.Sub(timelineStart)))))
			end = first + 1
		}
		selected = timelineSpan{first: first, end: end}
	}
	plan := timelinePlan{scale: scale, cells: cells, divisions: divisions, selected: selected, framed: framed, visibility: visibility, ruler: timelineRuler(m.selectedDate, scale)}
	if visibility.lunch && width >= 60 {
		lunch := m.contextSettings.Lunch
		start, _ := m.clockOnSelectedDay(lunch.Start)
		end, _ := m.clockOnSelectedDay(lunch.End)
		plan.lunchLabel = "Lunch " + lunch.Start + "–" + lunch.End
		position := scale.column(start.Add(end.Sub(start) / 2))
		plan.lunchColumn = min(max(0, position-len([]rune(plan.lunchLabel))/2), max(0, barWidth-len([]rune(plan.lunchLabel))))
	}
	return plan, true
}

const timelineQuarter = 15 * time.Minute

type timelineScale struct {
	start, end time.Time
	width      int
}

func newTimelineScale(start, end time.Time, availableWidth int) timelineScale {
	return timelineScale{start: start, end: end, width: availableWidth}
}

func (s timelineScale) column(value time.Time) int {
	// A boundary at a sampled midpoint belongs to the interval starting
	// there. Equal durations differ by at most one rounded terminal column.
	return int(math.Ceil(float64(s.width)*float64(value.Sub(s.start))/float64(s.end.Sub(s.start)) - 0.5))
}

func (s timelineScale) midpoint(column int) time.Time {
	return s.start.Add(time.Duration(float64(s.end.Sub(s.start)) * (float64(column) + 0.5) / float64(s.width)))
}

func timelineHourAfter(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, value.Location()).Add(time.Hour)
}

// Divisions use local clock boundaries, including when the axis begins inside
// a cell. Their widths remain proportional to actual time.
func timelineCellColumns(scale timelineScale, interval time.Duration) map[int]int {
	columns := map[int]int{}
	start, end := scale.start, scale.end
	minutes := int(interval / time.Minute)
	clockMinutes := start.Hour()*60 + start.Minute()
	tick := time.Date(start.Year(), start.Month(), start.Day(), 0, clockMinutes/minutes*minutes, 0, 0, start.Location()).Add(interval)
	for ; tick.Before(end); tick = tick.Add(interval) {
		column := scale.column(tick)
		nextColumn := scale.width
		if next := tick.Add(interval); next.Before(end) {
			nextColumn = min(scale.width, scale.column(next))
		}
		if column > 0 && column < scale.width {
			columns[column] = nextColumn - column
		}
	}
	return columns
}

func timelineRuler(selectedDate time.Time, scale timelineScale) string {
	start, end, width := scale.start, scale.end, scale.width
	ruler := []rune(strings.Repeat(" ", width))
	first, last := timelineEndLabel(selectedDate, start), timelineEndLabel(selectedDate, end)
	copy(ruler, []rune(first))
	lastColumn := max(0, width-len(last))
	copy(ruler[lastColumn:], []rune(last))
	previousEnd := len(first)
	for tick := timelineHourAfter(start); tick.Before(end); tick = tick.Add(time.Hour) {
		label := tick.Format("15:04")
		column := max(0, scale.column(tick)-len(label)/2)
		// Keep one cell between labels at either end of the ruler; shifting
		// an edge label by one cell is preferable to losing a whole hour.
		if previousEnd == len(first) && column == previousEnd {
			column++
		}
		if column == lastColumn-len(label) {
			column--
		}
		if column <= previousEnd || column+len(label) >= lastColumn {
			continue
		}
		copy(ruler[column:], []rune(label))
		previousEnd = column + len(label)
	}
	return string(ruler)
}

func appendTimelineRecords(persisted, preview []worklogs.LocalWorklog) []worklogs.LocalWorklog {
	records := make([]worklogs.LocalWorklog, 0, len(persisted)+len(preview))
	records = append(records, persisted...)
	return append(records, preview...)
}
