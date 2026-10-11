package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderMonthWorkspace(width, height int) string {
	heading := m.selectedDate.Format("January 2006") + " · Local booked " + formatDuration(m.contextSummary.BookedSeconds)
	lines := []string{heading, m.renderWorklogViewSelector(), ""}
	compact := monthCalendarCompact(m.selectedDate, height)
	lines = append(lines, m.monthCalendarRows(width-2, compact)...)
	empty, short := m.monthGaps()
	summary := fmt.Sprintf("Past weekdays: %d empty · %d short · %s/day quota", empty, short, formatDuration(m.dailyQuotaSeconds()))
	if len(m.contextDays) == 0 {
		summary = "No monthly context available."
		if m.worklogLoading {
			summary = "Loading month…"
		}
	}
	lines = append(lines, m.rule(width-2), m.muted(truncate(summary, width-2)), m.renderMonthLegend())
	topHeight := len(lines)
	return lipgloss.JoinVertical(lipgloss.Left, m.pane(strings.Join(lines, "\n"), width, topHeight),
		m.pane(m.renderMonthDayDetail(width-2, height-topHeight), width, height-topHeight))
}

func monthCalendarCompact(date time.Time, height int) bool {
	// Reserve room for the divider, legend, and selected-day preview.
	return height < 13+monthCalendarWeeks(date)*2
}

func monthCalendarWeeks(date time.Time) int {
	start, end := monthBounds(date)
	offset := (int(start.Weekday()) + 6) % 7
	return (offset + end.Day() + 6) / 7
}

func (m model) monthStateColor(marker string) string {
	switch marker {
	case "?":
		return m.colors.Warning
	case "!":
		return m.colors.Destructive
	case "*":
		return m.colors.Progress
	case "·":
		return m.colors.Muted
	default:
		return m.colors.Secondary
	}
}

func (m model) renderMonthLegend() string {
	items := []struct{ marker, label string }{
		{"?", "below quota"}, {"!", "overlap"}, {"*", "today"}, {"·", "pending"},
	}
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, m.paint(item.marker+" "+item.label, m.monthStateColor(item.marker)))
	}
	return strings.Join(labels, "  ")
}

func (m model) monthCalendarRows(width int, compact bool) []string {
	cellWidth := min(14, width/7)
	headers := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	for index, label := range headers {
		headers[index] = fmt.Sprintf("%-*s", cellWidth, label)
	}
	rows := []string{m.muted(strings.Join(headers, ""))}
	days := make(map[string]worklogs.ContextDay, len(m.contextDays))
	for _, day := range m.contextDays {
		days[day.Date] = day
	}
	today := beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
	start, end := monthBounds(m.selectedDate)
	offset := (int(start.Weekday()) + 6) % 7
	for week := 0; week < monthCalendarWeeks(start); week++ {
		dateCells, durationCells := make([]string, 7), make([]string, 7)
		for column := 0; column < 7; column++ {
			date := start.AddDate(0, 0, week*7+column-offset)
			dateText, durationText, color := "", "", m.colors.Secondary
			if !date.Before(start) && !date.After(end) {
				day, known := days[date.Format("2006-01-02")]
				flags := ""
				if known && date.Before(today) && isWeekday(date) && day.BookedSeconds < m.dailyQuotaSeconds() {
					flags += "?"
				}
				if len(day.Collisions) > 0 {
					flags += "!"
				}
				if sameDay(date, today) {
					flags += "*"
				} else if date.After(today) {
					flags += "·"
				}
				marker := " "
				// Conflicts keep their semantic color even on today or a pending date.
				switch {
				case len(day.Collisions) > 0:
					marker = "!"
				case strings.Contains(flags, "?"):
					marker = "?"
				case sameDay(date, today):
					marker = "*"
				case date.After(today):
					marker = "·"
				}
				color = m.monthStateColor(marker)
				selected := sameDay(date, m.selectedDate)
				durationText = "—"
				if known {
					durationText = strings.ReplaceAll(formatDuration(day.BookedSeconds), " ", "")
				}
				if compact {
					prefix := " "
					if selected {
						prefix = m.paint(">", m.colors.Focus)
					}
					clock := "—"
					if known {
						clock = fmt.Sprintf("%d:%02d", day.BookedSeconds/3600, day.BookedSeconds%3600/60)
					}
					dateText = prefix + m.paint(fmt.Sprintf("%2d%s%5s", date.Day(), marker, truncate(clock, 5)), color)
				} else {
					dateText = m.paint(fmt.Sprintf("%2d", date.Day()), color)
					if selected {
						dateText = m.paint("[", m.colors.Focus) + m.paint(fmt.Sprintf("%02d", date.Day()), color) + m.paint("]", m.colors.Focus)
					}
					for _, flag := range flags {
						dateText += m.paint(string(flag), m.monthStateColor(string(flag)))
					}
				}
			}
			dateCells[column] = lipgloss.NewStyle().Width(cellWidth).Render(dateText)
			durationCells[column] = lipgloss.NewStyle().Width(cellWidth).Render(m.paint(truncate(durationText, cellWidth), color))
		}
		rows = append(rows, strings.Join(dateCells, ""))
		if !compact {
			rows = append(rows, strings.Join(durationCells, ""))
		}
	}
	return rows
}

func (m model) monthGaps() (empty, short int) {
	today := beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
	for _, day := range m.contextDays {
		date, err := time.ParseInLocation("2006-01-02", day.Date, m.workspace.cfg.Location)
		if err != nil || !date.Before(today) || !isWeekday(date) {
			continue
		}
		if day.BookedSeconds == 0 {
			empty++
		} else if day.BookedSeconds < m.dailyQuotaSeconds() {
			short++
		}
	}
	return empty, short
}

func isWeekday(date time.Time) bool {
	return date.Weekday() != time.Saturday && date.Weekday() != time.Sunday
}

func (m model) renderMonthDayDetail(width, height int) string {
	booked := m.dayBookedSeconds()
	heading := m.selectedDate.Format("Mon 2006-01-02") + " · " + formatDuration(booked) + " booked"
	today := beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
	if count := len(m.dayContext.Collisions); count > 0 {
		heading += fmt.Sprintf(" · ! %d overlap", count)
	}
	switch {
	case sameDay(m.selectedDate, today):
		heading += " · today in progress"
	case m.selectedDate.After(today):
		heading += " · pending"
	case isWeekday(m.selectedDate) && booked < m.dailyQuotaSeconds():
		heading += " · " + formatDuration(m.dailyQuotaSeconds()-booked) + " short"
	}
	lines := []string{truncate(heading, width)}
	timeline := m.renderDayTimelineWidth(nil, "", width)
	if height >= lipgloss.Height(timeline)+3 {
		lines = append(lines, timeline)
		legend := wrapIssueDescription(m.renderTimelineLegendWidth(nil, "", width), width)
		if height >= lipgloss.Height(strings.Join(lines, "\n"))+len(legend)+2 {
			lines = append(lines, "")
			lines = append(lines, legend...)
		}
	}
	available := height - lipgloss.Height(strings.Join(lines, "\n"))
	lines = append(lines, m.unselectedWorklogRows(width, available)...)
	if len(m.worklogs) == 0 && available > 0 {
		message := "No worklogs yet."
		if booked > 0 {
			message = "Booked time carries over; no worklogs start here."
		}
		lines = append(lines, message)
	}
	return strings.Join(lines, "\n")
}
