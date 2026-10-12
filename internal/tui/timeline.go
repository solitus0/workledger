package tui

import (
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
	lunch    bool
	free     bool
	overtime bool
	selected bool
}

func (m model) renderDayTimelineState(previewRecords []worklogs.LocalWorklog, selectedID string) (string, timelineVisibility) {
	return m.renderTimelineState(previewRecords, selectedID, calculateLayout(m.width, m.height, false).workspace.width-2)
}

// Timeline row ownership is explicit. The annotation layer cannot overwrite
// the bar's top, fill, or bottom rows, including in monochrome terminals.
const (
	timelineRulerRow      = 0
	timelineSelectionRow  = 1
	timelineBarRow        = 2
	timelineBarHeight     = 3
	timelineContentHeight = timelineBarRow + timelineBarHeight
)

func (m model) renderTimelineState(previewRecords []worklogs.LocalWorklog, selectedID string, width int) (string, timelineVisibility) {
	plan, ok := m.buildTimelinePlan(previewRecords, selectedID, width)
	if !ok {
		return m.muted("Workday unavailable"), timelineVisibility{}
	}
	return m.renderTimelinePlan(plan, width), plan.visibility
}

func (m model) renderTimelinePlan(plan timelinePlan, width int) string {
	content := m.timelineCanvas(plan).Render()
	if plan.framed {
		style := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).Width(width)
		if !m.deps.noColor && m.colorReady {
			style = style.BorderForeground(lipgloss.Color(m.colors.Border))
		}
		content = style.Render(content)
	}
	return content
}

func (m model) timelineCanvas(plan timelinePlan) *lipgloss.Canvas {
	height := timelineContentHeight
	if plan.lunchLabel != "" {
		height++
	}
	canvas := lipgloss.NewCanvas(plan.scale.width, height)
	layers := []*lipgloss.Layer{lipgloss.NewLayer(m.muted(plan.ruler)).Y(timelineRulerRow), lipgloss.NewLayer(m.renderTimelineBar(plan)).Y(timelineBarRow)}
	if count := plan.selected.end - plan.selected.first; count > 0 {
		marker := "▼"
		if count > 1 {
			marker = "┌" + strings.Repeat("─", count-2) + "┐"
		}
		layers = append(layers, lipgloss.NewLayer(m.paint(marker, m.colors.Focus)).X(plan.selected.first).Y(timelineSelectionRow))
	}
	if plan.lunchLabel != "" {
		layers = append(layers, lipgloss.NewLayer(m.muted(plan.lunchLabel)).X(plan.lunchColumn).Y(timelineContentHeight))
	}
	canvas.Compose(lipgloss.NewCompositor(layers...))
	return canvas
}

func (m model) renderTimelineBar(plan timelinePlan) string {
	barWidth := plan.scale.width
	divisions := plan.divisions
	type paintedCell struct{ symbol, color string }
	cells := make([]paintedCell, barWidth)
	for index, state := range plan.cells {
		switch state {
		case timelineFree:
			cells[index] = paintedCell{"free", m.colors.Border}
		case timelineBooked:
			cells[index] = paintedCell{"█", m.colors.Information}
		case timelineOvertime:
			cells[index] = paintedCell{"▒", m.colors.Warning}
		case timelineLunch:
			cells[index] = paintedCell{"╱", m.colors.Secondary}
		case timelineProposed:
			cells[index] = paintedCell{"░", m.colors.Proposed}
		case timelineConflict:
			cells[index] = paintedCell{"!", m.colors.Destructive}
		}
	}
	upper, band, lower := make([]string, barWidth), make([]string, barWidth), make([]string, barWidth)
	lunchStart, lunchEnd := -1, -1
	lunchSharesBorder := false
	for index, cell := range cells {
		symbol := cell.symbol
		switch cell.symbol {
		case "free":
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
			if index >= lunchEnd {
				lunchStart, lunchEnd = index, index+1
				for lunchEnd < barWidth && cells[lunchEnd].symbol == "╱" {
					lunchEnd++
				}
				// Free cells can share their left edge even at one column.
				// Other patterns need a second column to retain their state.
				lunchSharesBorder = lunchEnd < barWidth && (cells[lunchEnd].symbol == "free" ||
					lunchEnd+1 < barWidth && cells[lunchEnd].symbol == cells[lunchEnd+1].symbol)
			}
			if index == lunchStart && (lunchEnd-lunchStart >= 3 || lunchEnd-lunchStart == 2 && lunchSharesBorder) {
				symbol = "▏"
			} else if index == lunchEnd-1 && !lunchSharesBorder && lunchEnd-lunchStart >= 2 {
				symbol = "▕"
			}
		}
		solid := cell.symbol == "█" || cell.symbol == "▒"
		if !m.deps.noColor && m.colorReady && solid {
			symbol = "█"
		}
		color := cell.color
		upper[index] = m.paint("▁", color)
		band[index] = m.renderTimelineCell(symbol, color)
		if cellWidth := divisions[index]; cellWidth > 0 && cell.symbol != "free" && symbol != "▏" && symbol != "▕" {
			if !m.deps.noColor && m.colorReady && symbol == "█" {
				// Inverse fill gives the thin rule the terminal's background
				// color, keeping it distinct in both themes without a cell gap.
				band[index] = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Reverse(true).Render("▏")
			} else if cellWidth >= 2 && index+1 < barWidth && cells[index+1].symbol == cell.symbol {
				// Leave room for the state's pattern, especially for short
				// proposals, conflicts, and lunch.
				// Keep the full-height hatch when a closing side would
				// otherwise consume the last two-column lunch cell.
				if cell.symbol != "╱" || index+1 != lunchEnd-1 || lunchSharesBorder {
					band[index] = m.renderTimelineCell("▏", color)
				}
			}
		}
		if index == lunchEnd && lunchSharesBorder {
			if !m.deps.noColor && m.colorReady && symbol == "█" {
				band[index] = lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Secondary)).Background(lipgloss.Color(color)).Render("▏")
			} else {
				band[index] = m.paint("▏", m.colors.Secondary)
			}
		}
		lower[index] = m.paint("▔", color)
	}
	return strings.Join([]string{strings.Join(upper, ""), strings.Join(band, ""), strings.Join(lower, "")}, "\n")
}

func (m model) renderTimelineCell(symbol, color string) string {
	if m.deps.noColor || !m.colorReady {
		return symbol
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if symbol == "█" {
		// An opaque cell fill matches inverse divider cells even when a
		// terminal font leaves padding around its full-block glyph.
		style = style.Background(lipgloss.Color(color))
	}
	return style.Render(symbol)
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
	plan, _ := m.buildTimelinePlan(previewRecords, selectedID, width)
	visibility := plan.visibility
	if visibility.selected {
		items = append(items, timelineLegendItem{symbol: "┌─┐/▼", color: m.colors.Focus, label: "selected"})
	}
	if len(previewRecords) > 0 {
		items = append(items, timelineLegendItem{symbol: "░", color: m.colors.Proposed, label: "proposed"})
		if m.form != nil && m.form.previewConflict != "" {
			items = append(items, timelineLegendItem{symbol: "!", color: m.colors.Destructive, label: "conflict"})
		}
	}
	if visibility.overtime {
		symbol := "▒"
		if !m.deps.noColor && m.colorReady {
			symbol = "█"
		}
		items = append(items, timelineLegendItem{symbol: symbol, color: m.colors.Warning, label: "overtime"})
	}
	items = m.appendBaseTimelineLegend(items, visibility)
	if visibility.overtime {
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

func (m model) selectedTimelineInterval(id string, timelineStart, timelineEnd time.Time) (time.Time, time.Time, bool) {
	if id == "" {
		return time.Time{}, time.Time{}, false
	}
	for _, record := range m.worklogs {
		if record.ID != id {
			continue
		}
		start := record.StartedAtUTC.In(m.workspace.cfg.Location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		if start.Before(timelineStart) {
			start = timelineStart
		}
		if end.After(timelineEnd) {
			end = timelineEnd
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
