package tui

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderDayTimeline(previewRecords []worklogs.LocalWorklog, selectedID string) string {
	timeline, _ := m.renderDayTimelineState(previewRecords, selectedID)
	return timeline
}

type timelineVisibility struct {
	lunch bool
	free  bool
}

func (m model) renderDayTimelineState(previewRecords []worklogs.LocalWorklog, selectedID string) (string, timelineVisibility) {
	visibility := timelineVisibility{}
	startText, endText := m.workdayBounds()
	workdayStart, startErr := m.clockOnSelectedDay(startText)
	workdayEnd, endErr := m.clockOnSelectedDay(endText)
	if startErr != nil || endErr != nil || !workdayStart.Before(workdayEnd) {
		return m.muted("Workday unavailable"), visibility
	}
	timelineStart := workdayStart
	timelineEnd := workdayEnd
	for _, record := range appendTimelineRecords(m.worklogs, previewRecords) {
		start := record.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		if start.Before(timelineStart) {
			timelineStart = start
		}
		if end.After(timelineEnd) {
			timelineEnd = end
		}
	}
	hasEarlyOvertime := timelineStart.Before(workdayStart)
	hasLateOvertime := timelineEnd.After(workdayEnd)
	selectedStart, selectedEnd, hasSelection := m.selectedTimelineInterval(selectedID, timelineStart, timelineEnd)
	barWidth := min(52, max(18, m.formContentWidth()-26))
	boundaryCount := 0
	if hasEarlyOvertime {
		boundaryCount++
	}
	if hasLateOvertime {
		boundaryCount++
	}
	cellCount := barWidth - boundaryCount
	span := timelineEnd.Sub(timelineStart)
	startBoundaryIndex, endBoundaryIndex := -1, -1
	if hasEarlyOvertime {
		startBoundaryIndex = int(math.Ceil(float64(cellCount)*float64(workdayStart.Sub(timelineStart))/float64(span) - 0.5))
		startBoundaryIndex = min(max(1, startBoundaryIndex), cellCount-1)
	}
	if hasLateOvertime {
		endBoundaryIndex = int(math.Ceil(float64(cellCount)*float64(workdayEnd.Sub(timelineStart))/float64(span) - 0.5))
		endBoundaryIndex = min(max(1, endBoundaryIndex), cellCount-1)
	}
	segments := make([]string, 0, barWidth)
	topMargin := make([]string, 0, barWidth)
	bottomMargin := make([]string, 0, barWidth)
	hasVisibleSelection := false
	for index := 0; index < cellCount; index++ {
		if index == startBoundaryIndex || index == endBoundaryIndex {
			marginTop, marginBottom := " ", " "
			boundaryTime := workdayStart
			if index == endBoundaryIndex {
				boundaryTime = workdayEnd
			}
			if hasSelection && selectedStart.Before(boundaryTime) && selectedEnd.After(boundaryTime) {
				marginTop, marginBottom = "▔", "▁"
			}
			segments = append(segments, m.paint("│", m.colors.Secondary))
			topMargin = append(topMargin, marginTop)
			bottomMargin = append(bottomMargin, marginBottom)
		}
		local := timelineStart.Add(time.Duration(float64(span) * (float64(index) + 0.5) / float64(cellCount)))
		symbol, color := "─", m.colors.Muted
		persisted := worklogAt(m.worklogs, local)
		if persisted != nil {
			switch {
			case local.Before(workdayStart) || !local.Before(workdayEnd):
				symbol, color = "▒", m.colors.Warning
			default:
				symbol, color = "█", m.colors.Information
			}
		} else if m.inLunch(local) {
			symbol, color = "·", m.colors.Secondary
		} else if m.dayContext.Date != "" && m.dayContext.FreeSlots != nil && !containsFreeSlot(m.dayContext.FreeSlots, local) &&
			!(m.lunchAvailableForPlacement() && m.inConfiguredLunch(local)) {
			symbol, color = "█", m.colors.Information
		}
		preview := worklogAt(previewRecords, local)
		if preview != nil {
			if persisted != nil && m.form != nil && m.form.previewConflict != "" {
				symbol, color = "!", m.colors.Destructive
			} else if !local.Before(workdayStart) && local.Before(workdayEnd) {
				symbol, color = "▓", m.colors.Proposed
			} else {
				symbol, color = "▒", m.colors.Warning
			}
		}
		switch symbol {
		case "·":
			visibility.lunch = true
		case "─":
			visibility.free = true
		}
		marginTop, marginBottom := " ", " "
		if hasSelection && !local.Before(selectedStart) && local.Before(selectedEnd) {
			hasVisibleSelection = true
			marginTop, marginBottom = "▔", "▁"
		}
		topMargin = append(topMargin, marginTop)
		segments = append(segments, m.paint(symbol, color))
		bottomMargin = append(bottomMargin, marginBottom)
	}
	startLabel := timelineEndLabel(m.selectedDate, timelineStart)
	endLabel := timelineEndLabel(m.selectedDate, timelineEnd)
	middle := startLabel + " " + strings.Join(segments, "") + " " + endLabel
	if !hasSelection || !hasVisibleSelection {
		return middle, visibility
	}
	paintSelectionMargin := func(cells []string) string {
		var line strings.Builder
		for _, cell := range cells {
			if cell == " " {
				line.WriteString(cell)
				continue
			}
			line.WriteString(m.paint(cell, m.colors.Focus))
		}
		return line.String()
	}
	leftMarginWidth := utf8.RuneCountInString(startLabel) + 1
	rightMarginWidth := utf8.RuneCountInString(endLabel) + 1
	leftMargin := strings.Repeat(" ", leftMarginWidth)
	rightMargin := strings.Repeat(" ", rightMarginWidth)
	return leftMargin + paintSelectionMargin(topMargin) + rightMargin + "\n" +
		middle + "\n" +
		leftMargin + paintSelectionMargin(bottomMargin) + rightMargin, visibility
}

func appendTimelineRecords(persisted, preview []worklogs.LocalWorklog) []worklogs.LocalWorklog {
	records := make([]worklogs.LocalWorklog, 0, len(persisted)+len(preview))
	records = append(records, persisted...)
	return append(records, preview...)
}

type timelineLegendItem struct {
	symbol string
	color  string
	label  string
}

func (m model) renderTimelineLegend(previewRecords []worklogs.LocalWorklog, selectedID string) string {
	items := []timelineLegendItem{
		{symbol: "█", color: m.colors.Information, label: "booked"},
	}
	_, visibility := m.renderDayTimelineState(previewRecords, selectedID)
	if selectedID != "" && m.selectedWorklogExists(selectedID) {
		items = append(items, timelineLegendItem{symbol: "─", color: m.colors.Focus, label: "selected"})
	}
	if len(previewRecords) > 0 {
		items = append(items, timelineLegendItem{symbol: "▓", color: m.colors.Proposed, label: "proposed"})
		if m.form != nil && m.form.previewConflict != "" {
			items = append(items, timelineLegendItem{symbol: "!", color: m.colors.Destructive, label: "conflict"})
		}
	}
	startText, endText := m.workdayBounds()
	workdayStart, startErr := m.clockOnSelectedDay(startText)
	workdayEnd, err := m.clockOnSelectedDay(endText)
	if startErr != nil || err != nil {
		return m.renderLegendItems(m.appendBaseTimelineLegend(items, visibility))
	}
	hasEarlyOvertime, hasLateOvertime := false, false
	for _, record := range appendTimelineRecords(m.worklogs, previewRecords) {
		start := record.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		hasEarlyOvertime = hasEarlyOvertime || start.Before(workdayStart)
		hasLateOvertime = hasLateOvertime || end.After(workdayEnd)
	}
	if hasEarlyOvertime || hasLateOvertime {
		items = append(items, timelineLegendItem{symbol: "▒", color: m.colors.Warning, label: "overtime"})
	}
	items = m.appendBaseTimelineLegend(items, visibility)
	if hasEarlyOvertime {
		items = append(items, timelineLegendItem{symbol: "│", color: m.colors.Secondary, label: "day start"})
	}
	if hasLateOvertime {
		items = append(items, timelineLegendItem{symbol: "│", color: m.colors.Secondary, label: "day end"})
	}
	if hasEarlyOvertime || hasLateOvertime {
		m.compactTimelineLegend(items)
	}
	return m.renderLegendItems(items)
}

func (m model) appendBaseTimelineLegend(items []timelineLegendItem, visibility timelineVisibility) []timelineLegendItem {
	if visibility.lunch {
		items = append(items, timelineLegendItem{symbol: "·", color: m.colors.Secondary, label: "lunch"})
	}
	if visibility.free {
		items = append(items, timelineLegendItem{symbol: "─", color: m.colors.Muted, label: "free"})
	}
	return items
}

func (m model) compactTimelineLegend(items []timelineLegendItem) {
	if timelineLegendWidth(items) <= m.formContentWidth()-13 {
		return
	}
	for index := range items {
		switch items[index].label {
		case "selected":
			items[index].label = "sel"
		case "overtime":
			items[index].label = "OT"
		case "day end":
			items[index].label = "end"
		case "day start":
			items[index].label = "start"
		}
	}
}

func timelineLegendWidth(items []timelineLegendItem) int {
	width := 0
	for index, item := range items {
		if index > 0 {
			width += 2
		}
		width += utf8.RuneCountInString(item.symbol) + 1 + utf8.RuneCountInString(item.label)
	}
	return width
}

func (m model) selectedWorklogExists(id string) bool {
	for _, record := range m.worklogs {
		if record.ID == id && record.DurationSeconds > 0 {
			return true
		}
	}
	return false
}

func (m model) selectedTimelineInterval(id string, workdayStart, workdayEnd time.Time) (time.Time, time.Time, bool) {
	if id == "" {
		return time.Time{}, time.Time{}, false
	}
	for _, record := range m.worklogs {
		if record.ID != id {
			continue
		}
		start := record.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		if start.Before(workdayStart) {
			start = workdayStart
		}
		if end.After(workdayEnd) {
			end = workdayEnd
		}
		return start, end, start.Before(end)
	}
	return time.Time{}, time.Time{}, false
}

func (m model) renderLegendItems(items []timelineLegendItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, m.paint(item.symbol, item.color)+" "+m.muted(item.label))
	}
	return strings.Join(parts, "  ")
}

func (m model) clockOnSelectedDay(value string) (time.Time, error) {
	clock, err := time.ParseInLocation("15:04", value, m.workspace.cfg.Location)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(m.selectedDate.Year(), m.selectedDate.Month(), m.selectedDate.Day(), clock.Hour(), clock.Minute(), 0, 0, m.workspace.cfg.Location), nil
}

func timelineEndLabel(selectedDate, value time.Time) string {
	if value.Hour() == 0 && value.Minute() == 0 && !sameDay(selectedDate, value) {
		return "24:00"
	}
	return value.Format("15:04")
}

func (m model) workdayBounds() (string, string) {
	start, end := m.contextSettings.DayStart, m.contextSettings.DayEnd
	if start == "" {
		start = m.workspace.cfg.DayStart
	}
	if end == "" {
		end = m.workspace.cfg.DayEnd
	}
	if start == "" {
		start = "08:00"
	}
	if end == "" {
		end = "17:00"
	}
	return start, end
}

func (m model) inLunch(value time.Time) bool {
	return !m.lunchAvailableForPlacement() && m.inConfiguredLunch(value)
}

func (m model) lunchAvailableForPlacement() bool {
	return m.form != nil && !m.form.editing && m.form.placement != manualPlacement && m.form.noLunch
}

func (m model) inConfiguredLunch(value time.Time) bool {
	if m.contextSettings.Lunch == nil {
		return false
	}
	start, startErr := time.ParseInLocation("15:04", m.contextSettings.Lunch.Start, value.Location())
	end, endErr := time.ParseInLocation("15:04", m.contextSettings.Lunch.End, value.Location())
	if startErr != nil || endErr != nil {
		return false
	}
	start = time.Date(value.Year(), value.Month(), value.Day(), start.Hour(), start.Minute(), 0, 0, value.Location())
	end = time.Date(value.Year(), value.Month(), value.Day(), end.Hour(), end.Minute(), 0, 0, value.Location())
	return !value.Before(start) && value.Before(end)
}

func worklogAt(records []worklogs.LocalWorklog, value time.Time) *worklogs.LocalWorklog {
	for index := range records {
		record := &records[index]
		start := record.StartedAtUTC.In(value.Location())
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		if !value.Before(start) && value.Before(end) {
			return record
		}
	}
	return nil
}

func containsFreeSlot(slots []worklogs.ContextFreeSlot, value time.Time) bool {
	for _, slot := range slots {
		start := slot.Start.In(value.Location())
		end := slot.End.In(value.Location())
		if !value.Before(start) && value.Before(end) {
			return true
		}
	}
	return false
}
