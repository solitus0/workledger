package tui

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderDayTimeline(previewRecords []worklogs.LocalWorklog, selectedID string) string {
	timeline, _ := m.renderDayTimelineState(previewRecords, selectedID)
	return timeline
}

func (m model) renderDayTimelineWidth(previewRecords []worklogs.LocalWorklog, selectedID string, width int) string {
	timeline, _ := m.renderTimelineState(previewRecords, selectedID, width)
	return timeline
}

type timelineVisibility struct {
	lunch bool
	free  bool
}

func (m model) renderDayTimelineState(previewRecords []worklogs.LocalWorklog, selectedID string) (string, timelineVisibility) {
	return m.renderTimelineState(previewRecords, selectedID, calculateLayout(m.width, m.height, false).workspace.width-2)
}

// Timeline geometry is domain-specific: Lip Gloss provides borders and solid
// fills, while timestamps determine the ruler and every interval's cell range.
func (m model) renderTimelineState(previewRecords []worklogs.LocalWorklog, selectedID string, width int) (string, timelineVisibility) {
	visibility := timelineVisibility{}
	startText, endText := m.workdayBounds()
	workdayStart, startErr := m.clockOnSelectedDay(startText)
	workdayEnd, endErr := m.clockOnSelectedDay(endText)
	if startErr != nil || endErr != nil || !workdayStart.Before(workdayEnd) {
		return m.muted("Workday unavailable"), visibility
	}
	timelineStart, timelineEnd := workdayStart, workdayEnd
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
	framed := width >= 60 && calculateLayout(m.width, m.height, m.activityVisible()).workspace.height >= 23
	availableWidth := max(3, width)
	if framed {
		availableWidth = max(3, width-4)
	}
	scale := newTimelineScale(timelineStart, timelineEnd, availableWidth)
	barWidth := scale.width
	selectedStart, selectedEnd, hasSelection := m.selectedTimelineInterval(selectedID, timelineStart, timelineEnd)
	type cell struct {
		symbol   string
		color    string
		selected bool
	}
	cells := make([]cell, barWidth)
	selectedCells := 0
	for index := range cells {
		local := scale.midpoint(index)
		symbol, color := "free", m.colors.Border
		persisted := worklogAt(m.worklogs, local)
		if persisted != nil {
			if local.Before(workdayStart) || !local.Before(workdayEnd) {
				symbol, color = "▒", m.colors.Warning
			} else {
				symbol, color = "█", m.colors.Information
			}
		} else if m.inLunch(local) {
			symbol, color = "╱", m.colors.Secondary
		} else if m.dayContext.Date != "" && m.dayContext.FreeSlots != nil && !containsFreeSlot(m.dayContext.FreeSlots, local) &&
			!(m.lunchAvailableForPlacement() && m.inConfiguredLunch(local)) {
			symbol, color = "█", m.colors.Information
		}
		if preview := worklogAt(previewRecords, local); preview != nil {
			if persisted != nil && m.form != nil && m.form.previewConflict != "" {
				symbol, color = "!", m.colors.Destructive
			} else if !local.Before(workdayStart) && local.Before(workdayEnd) {
				symbol, color = "░", m.colors.Proposed
			} else {
				symbol, color = "▒", m.colors.Warning
			}
		}
		selected := hasSelection && !local.Before(selectedStart) && local.Before(selectedEnd)
		if selected {
			selectedCells++
		}
		cells[index] = cell{symbol: symbol, color: color, selected: selected}
	}
	if hasSelection && selectedCells == 0 {
		index := min(barWidth-1, max(0, scale.column(selectedStart.Add(selectedEnd.Sub(selectedStart)/2))))
		symbol := "█"
		if !selectedEnd.After(workdayStart) || !selectedStart.Before(workdayEnd) {
			symbol = "▒"
		}
		cells[index] = cell{symbol: symbol, color: m.colors.Focus, selected: true}
	}
	cellDuration := timelineQuarter
	if timelineStart.Before(workdayStart) || timelineEnd.After(workdayEnd) {
		cellDuration = time.Hour
	}
	divisions := timelineCellColumns(scale, cellDuration)
	// A single fill row, bounded by eighth-cell strokes, gives every state the
	// same visible height without requiring half-height hatch or shade glyphs.
	upper, band, lower := make([]string, barWidth), make([]string, barWidth), make([]string, barWidth)
	lunchStart, lunchEnd := -1, -1
	lunchSharesBorder := false
	for index, cell := range cells {
		symbol := cell.symbol
		switch cell.symbol {
		case "free":
			visibility.free = true
			symbol = " "
			starts := index == 0 || cells[index-1].symbol != "free"
			ends := index == barWidth-1 || cells[index+1].symbol != "free"
			switch {
			case starts && ends:
				symbol = "□"
			case ends:
				symbol = "▕"
			case starts || divisions[index] > 0:
				symbol = "▏"
			}
		case "╱":
			visibility.lunch = true
			if index >= lunchEnd {
				lunchStart, lunchEnd = index, index+1
				for lunchEnd < barWidth && cells[lunchEnd].symbol == "╱" {
					lunchEnd++
				}
				// Free cells can share their left edge even at one column.
				// Other patterns need a second column to retain their state.
				lunchSharesBorder = lunchEnd < barWidth && (cells[lunchEnd].symbol == "free" ||
					lunchEnd+1 < barWidth && cells[lunchEnd].symbol == cells[lunchEnd+1].symbol && cells[lunchEnd].selected == cells[lunchEnd+1].selected)
			}
			if index == lunchStart && (lunchEnd-lunchStart >= 3 || lunchEnd-lunchStart == 2 && lunchSharesBorder) {
				symbol = "▏"
			} else if index == lunchEnd-1 && !lunchSharesBorder && lunchEnd-lunchStart >= 2 {
				symbol = "▕"
			}
		}
		if cell.selected {
			if cell.symbol == "▒" && (m.deps.noColor || !m.colorReady) {
				symbol = "▚"
			} else if cell.symbol != "▒" && cell.symbol != "!" {
				symbol = "▓"
			}
		}
		solid := cell.symbol == "█" || cell.symbol == "▒" && !cell.selected ||
			cell.selected && cell.symbol != "▒" && cell.symbol != "!"
		if !m.deps.noColor && m.colorReady && solid {
			symbol = "█"
		}
		color := cell.color
		if cell.selected {
			color = m.colors.Focus
		}
		upper[index] = m.paint("▁", color)
		band[index] = m.renderTimelineCell(symbol, cell.color, cell.selected)
		if cellWidth := divisions[index]; cellWidth > 0 && cell.symbol != "free" && symbol != "▏" && symbol != "▕" {
			if !m.deps.noColor && m.colorReady && symbol == "█" {
				// Inverse fill gives the thin rule the terminal's background
				// color, keeping it distinct in both themes without a cell gap.
				band[index] = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Reverse(true).Render("▏")
			} else if cellWidth >= 2 && index+1 < barWidth && cells[index+1].symbol == cell.symbol && cells[index+1].selected == cell.selected {
				// Leave room for the state's pattern, especially for short
				// proposals, conflicts, lunch, and non-color selections.
				// Keep the full-height hatch when a closing side would
				// otherwise consume the last two-column lunch cell.
				if cell.symbol != "╱" || index+1 != lunchEnd-1 || lunchSharesBorder {
					band[index] = m.renderTimelineCell("▏", cell.color, cell.selected)
				}
			}
		}
		if index == lunchEnd && lunchSharesBorder {
			if !m.deps.noColor && m.colorReady && (symbol == "█" || cell.selected) {
				band[index] = lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Secondary)).Background(lipgloss.Color(color)).Render("▏")
			} else {
				band[index] = m.paint("▏", m.colors.Secondary)
			}
		}
		lower[index] = m.paint("▔", color)
	}
	lines := []string{m.muted(timelineRuler(m.selectedDate, scale)), strings.Join(upper, ""), strings.Join(band, ""), strings.Join(lower, "")}
	if visibility.lunch && width >= 60 {
		lunch := m.contextSettings.Lunch
		start, _ := m.clockOnSelectedDay(lunch.Start)
		end, _ := m.clockOnSelectedDay(lunch.End)
		label := "Lunch " + lunch.Start + "–" + lunch.End
		position := scale.column(start.Add(end.Sub(start) / 2))
		padding := min(max(0, position-lipgloss.Width(label)/2), max(0, barWidth-lipgloss.Width(label)))
		lines = append(lines, strings.Repeat(" ", padding)+m.muted(label))
	}
	content := strings.Join(lines, "\n")
	if framed {
		style := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).Width(width)
		if !m.deps.noColor && m.colorReady {
			style = style.BorderForeground(lipgloss.Color(m.colors.Border))
		}
		content = style.Render(content)
	}
	return content, visibility
}

func (m model) renderTimelineCell(symbol, color string, selected bool) string {
	if m.deps.noColor || !m.colorReady {
		return symbol
	}
	if selected {
		color = m.colors.Focus
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if symbol == "█" {
		// An opaque cell fill matches inverse divider cells even when a
		// terminal font leaves padding around its full-block glyph.
		style = style.Background(lipgloss.Color(color))
	} else if selected {
		style = style.Reverse(true)
	}
	return style.Render(symbol)
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
	// there. All states and ruler ticks share the full available width.
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
	tick := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), start.Minute()/minutes*minutes, 0, 0, start.Location()).Add(interval)
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

type timelineLegendItem struct {
	symbol string
	color  string
	label  string
}

func (m model) renderTimelineLegend(previewRecords []worklogs.LocalWorklog, selectedID string) string {
	return m.renderTimelineLegendWidth(previewRecords, selectedID, calculateLayout(m.width, m.height, false).workspace.width-2)
}

func (m model) renderTimelineLegendWidth(previewRecords []worklogs.LocalWorklog, selectedID string, width int) string {
	items := []timelineLegendItem{
		{symbol: "█", color: m.colors.Information, label: "booked"},
	}
	_, visibility := m.renderTimelineState(previewRecords, selectedID, width)
	if selectedID != "" && m.selectedWorklogExists(selectedID) {
		items = append(items, timelineLegendItem{symbol: "▓", color: m.colors.Focus, label: "selected"})
	}
	if len(previewRecords) > 0 {
		items = append(items, timelineLegendItem{symbol: "░", color: m.colors.Proposed, label: "proposed"})
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
	hasOvertime, hasSelectedOvertime := false, false
	for _, record := range appendTimelineRecords(m.worklogs, previewRecords) {
		start := record.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		overtime := start.Before(workdayStart) || end.After(workdayEnd)
		hasOvertime = hasOvertime || overtime
		hasSelectedOvertime = hasSelectedOvertime || selectedID != "" && record.ID == selectedID && overtime
	}
	if hasOvertime {
		symbol := "▒"
		if !m.deps.noColor && m.colorReady && !hasSelectedOvertime {
			symbol = "█"
		} else if hasSelectedOvertime && (m.deps.noColor || !m.colorReady) {
			symbol = "▒/▚"
		}
		items = append(items, timelineLegendItem{symbol: symbol, color: m.colors.Warning, label: "overtime"})
	}
	items = m.appendBaseTimelineLegend(items, visibility)
	if hasOvertime {
		m.compactTimelineLegend(items, calculateLayout(m.width, m.height, false).workspace.width-2)
	}
	return m.renderLegendItems(items)
}

func (m model) appendBaseTimelineLegend(items []timelineLegendItem, visibility timelineVisibility) []timelineLegendItem {
	if visibility.lunch {
		items = append(items, timelineLegendItem{symbol: "╱", color: m.colors.Secondary, label: "lunch"})
	}
	if visibility.free {
		items = append(items, timelineLegendItem{symbol: "□", color: m.colors.Muted, label: "free"})
	}
	return items
}

func (m model) compactTimelineLegend(items []timelineLegendItem, width int) {
	if timelineLegendWidth(items) <= width {
		return
	}
	for index := range items {
		switch items[index].label {
		case "selected":
			items[index].label = "sel"
		case "overtime":
			items[index].label = "OT"
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
