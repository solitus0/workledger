package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderWorklogWorkspace(width, height int) string {
	if m.workspace == nil {
		message := "Loading local workspace…"
		if !m.workspaceLoading {
			message = "Worklogs unavailable\n\nFix configuration or local storage in Status."
		}
		return m.box(message, width, height, false)
	}
	if m.worklogView == weekWorklogView {
		return m.renderWeekWorkspace(width, height)
	}
	topHeight := max(10, height/2)
	detailHeight := height - topHeight
	header := fmt.Sprintf("  %-7s %-10s %-14s %s", "TIME", "DURATION", "ISSUE", "DESCRIPTION")
	showLegend := topHeight >= 14
	timelineLines := strings.Split(m.renderDayTimeline(nil, m.selectedWorklogID()), "\n")
	chromeLines := 4 + len(timelineLines)
	if showLegend {
		chromeLines += 2
	}
	rows := m.worklogRows(width-6, max(1, topHeight-2-chromeLines))
	tableLines := []string{
		m.renderDayHeading(m.selectedDate.Format("Monday, 2006-01-02"), width-4),
		m.renderWorklogViewSelector(),
		"",
	}
	tableLines = append(tableLines, timelineLines...)
	if showLegend {
		tableLines = append(tableLines, "", strings.Repeat(" ", 6)+m.renderTimelineLegend(nil, m.selectedWorklogID()))
	}
	tableLines = append(tableLines, m.muted(header))
	tableLines = append(tableLines, rows...)
	if len(rows) == 0 {
		if m.worklogLoading {
			tableLines = append(tableLines, "  "+m.paint("Loading…", m.colors.Progress))
		} else {
			tableLines = append(tableLines, "  No worklogs yet.")
		}
	}
	table := strings.Join(tableLines, "\n")
	detail := m.renderWorklogDetail()
	if m.form != nil {
		detail = m.renderForm()
	}
	confirmationOpen := m.overlay == deleteOverlay || m.overlay == deleteDayOverlay || m.overlay == forceOverlay || m.overlay == reloadOverlay
	if confirmationOpen {
		detail = m.renderConfirmation(width-2, detailHeight-2)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.box(table, width, topHeight, m.focus != focusRail && m.form == nil && !confirmationOpen),
		m.box(detail, width, detailHeight, m.form != nil),
	)
}

func (m model) renderWeekWorkspace(width, height int) string {
	topHeight := max(13, height*3/5)
	detailHeight := height - topHeight
	weekStart, weekEnd := weekBounds(m.selectedDate)
	title := fmt.Sprintf("Week · %s–%s", weekStart.Format("2006-01-02"), weekEnd.Format("2006-01-02"))
	lines := []string{
		m.renderLoggedHeading(title, m.weekSummary.BookedSeconds, width-4),
		m.renderWorklogViewSelector(),
		"",
		m.muted(fmt.Sprintf("  %-5s %-11s %-10s %-9s %-10s %s", "DAY", "DATE", "BOOKED", "WORKLOGS", "DELTA", "COLLISIONS")),
	}
	lines = append(lines, m.weekRows()...)
	if len(m.weekDays) == 0 {
		if m.worklogLoading {
			lines = append(lines, "  "+m.paint("Loading…", m.colors.Progress))
		} else {
			lines = append(lines, "  No weekly context available.")
		}
	}

	detail := m.renderWeekDayDetail(detailHeight - 2)
	if m.form != nil {
		detail = m.renderForm()
	}
	confirmationOpen := m.overlay == deleteOverlay || m.overlay == deleteDayOverlay || m.overlay == forceOverlay || m.overlay == reloadOverlay
	if confirmationOpen {
		detail = m.renderConfirmation(width-2, detailHeight-2)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.box(strings.Join(lines, "\n"), width, topHeight, m.focus != focusRail && m.form == nil && !confirmationOpen),
		m.box(detail, width, detailHeight, m.form != nil),
	)
}

func (m model) renderWorklogViewSelector() string {
	if m.worklogView == weekWorklogView {
		return m.secondary("Day") + "  " + m.paint("[Week]", m.colors.Focus)
	}
	return m.paint("[Day]", m.colors.Focus) + "  " + m.secondary("Week")
}

func (m model) weekRows() []string {
	rows := make([]string, 0, len(m.weekDays))
	for _, day := range m.weekDays {
		date, err := time.ParseInLocation("2006-01-02", day.Date, m.workspace.cfg.Location)
		if err != nil {
			continue
		}
		prefix := "  "
		if sameDay(date, m.selectedDate) {
			prefix = m.paint("> ", m.colors.Focus)
		}
		delta := "—"
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			delta = formatDelta(day.BookedSeconds - m.dailyQuotaSeconds())
			if day.BookedSeconds < m.dailyQuotaSeconds() {
				delta = m.paint(delta, m.colors.Warning)
			} else if day.BookedSeconds > m.dailyQuotaSeconds() {
				delta = m.paint(delta, m.colors.Warning)
			}
		}
		collisions := fmt.Sprint(len(day.Collisions))
		if len(day.Collisions) > 0 {
			collisions = m.paint(collisions, m.colors.Warning)
		}
		rows = append(rows, fmt.Sprintf("%s%-5s %-11s %-10s %-9d %-10s %s",
			prefix, date.Format("Mon"), day.Date, formatDuration(day.BookedSeconds), len(day.Worklogs), delta, collisions))
	}
	return rows
}

func (m model) renderWeekDayDetail(height int) string {
	lines := []string{
		m.renderDayHeading(m.selectedDate.Format("Monday, 2006-01-02"), m.width-35),
		m.renderDayTimeline(nil, ""),
	}
	if height >= 7 {
		lines = append(lines, "")
		lines = append(lines, strings.Repeat(" ", 6)+m.renderTimelineLegend(nil, ""))
	}
	lines = append(lines, m.muted(fmt.Sprintf("  %-7s %-10s %-14s %s", "TIME", "DURATION", "ISSUE", "DESCRIPTION")))
	availableRows := max(0, height-len(lines))
	rows := m.unselectedWorklogRows(m.width-37, availableRows)
	lines = append(lines, rows...)
	if len(m.worklogs) == 0 && availableRows > 0 {
		lines = append(lines, "  No worklogs yet.")
	}
	return strings.Join(lines, "\n")
}

func (m model) unselectedWorklogRows(width, height int) []string {
	if len(m.worklogs) == 0 || height <= 0 {
		return nil
	}
	end := min(len(m.worklogs), height)
	rows := make([]string, 0, end)
	descriptionWidth := max(10, width-38)
	for _, item := range m.worklogs[:end] {
		local := item.StartedAtUTC.In(m.workspace.cfg.Location)
		rows = append(rows, fmt.Sprintf("  %-7s %-10s %-14s %s", local.Format("15:04"), formatDuration(item.DurationSeconds), item.IssueKey, truncate(item.Description, descriptionWidth)))
	}
	return rows
}

func formatDelta(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return sign + formatDuration(seconds)
}

func (m model) worklogRows(width, height int) []string {
	if len(m.worklogs) == 0 {
		return nil
	}
	start, end := visibleRange(m.worklogSelected, len(m.worklogs), height)
	rows := make([]string, 0, end-start)
	descriptionWidth := max(10, width-38)
	for index := start; index < end; index++ {
		item := m.worklogs[index]
		prefix := "  "
		if index == m.worklogSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		local := item.StartedAtUTC.In(m.workspace.cfg.Location)
		rows = append(rows, fmt.Sprintf("%s%-7s %-10s %-14s %s", prefix, local.Format("15:04"), formatDuration(item.DurationSeconds), item.IssueKey, truncate(item.Description, descriptionWidth)))
	}
	return rows
}

func (m model) renderWorklogDetail() string {
	item := m.selectedWorklog()
	if item == nil {
		return "No worklog selected."
	}
	local := item.StartedAtUTC.In(m.workspace.cfg.Location)
	created := item.CreatedAt.In(m.workspace.cfg.Location)
	updated := item.UpdatedAt.In(m.workspace.cfg.Location)
	return fmt.Sprintf("Issue             %s\nLocal issue total %s\nStart             %s\nDuration          %s\nCreated           %s\nUpdated           %s\nID                %s\n\n%s",
		item.IssueKey, m.renderIssueTotal(item.IssueKey), local.Format("2006-01-02 15:04"), formatDuration(item.DurationSeconds),
		created.Format("2006-01-02 15:04"), updated.Format("2006-01-02 15:04"), item.ID, item.Description)
}

func (m model) renderIssueTotal(issueKey string) string {
	if m.issueTotalIssue != issueKey {
		return "Unavailable"
	}
	if m.issueTotalLoading {
		return m.paint("Loading…", m.colors.Progress)
	}
	if m.issueTotalErr != "" {
		return "Unavailable"
	}
	return formatDuration(m.issueTotal)
}

func (m model) renderForm() string {
	title := "Add worklog"
	if m.form.editing {
		title = "Edit worklog"
	}
	if m.form.editing {
		title = truncate(title, m.formContentWidth())
	} else {
		title = m.renderDayHeading(title+" · "+m.selectedDate.Format("Mon, Jan 2"), m.formContentWidth())
	}
	lines := []string{title}
	lines = append(lines, m.renderFormSection("Worklog details", m.colors.Focus))
	for _, field := range m.formFields() {
		index, ok := inputIndexForField(field)
		if !ok {
			continue
		}
		label := map[formField]string{
			issueField: "Issue", startField: "Start", durationField: "Duration", descriptionField: "Description",
		}[field]
		if field == startField && !m.form.editing {
			label = "Start time"
		}
		lines = append(lines, m.renderFormRow(label, m.form.inputs[index].View(), field == m.form.focus))
		if field == issueField {
			if context := m.renderIssueContext(); context != "" && m.height >= 32 {
				lines = append(lines, fmt.Sprintf("%-12s %s", "Issue info", m.muted(context)))
			}
		}
	}
	if !m.form.editing {
		lines = append(lines, "")
		expanded := m.formLineBudget() >= 15 && m.formContentWidth() >= 78
		if expanded {
			lines = append(lines, m.renderFormSection("Placement", m.colors.Focus))
			lines = append(lines, fmt.Sprintf("%-12s %s · %s", "", m.renderPlacementSelector(), m.muted(m.form.placement.Description())))
		} else {
			lines = append(lines, m.renderFormSectionRow("Placement", m.renderPlacementSelector(), m.form.focus == placementField, m.colors.Focus))
			lines = append(lines, fmt.Sprintf("%-12s %s", "", m.muted(m.form.placement.Description())))
		}
		if expanded {
			lines = append(lines, m.renderFormSection("Calculated", m.colors.Information))
			lines = append(lines, fmt.Sprintf("%-12s %s", "", m.renderDayTimeline(m.form.previewRecords, "")))
		} else {
			lines = append(lines, m.renderFormSectionRow("Calculated", m.renderDayTimeline(m.form.previewRecords, ""), false, m.colors.Information))
		}
		if m.height >= 34 {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%-12s %s", "", m.renderTimelineLegend(m.form.previewRecords, "")))
		}
		lines = append(lines, m.renderCalculatedRow("Result", m.renderPlacementPreview()))
		lines = append(lines, m.renderCalculatedRow("After", m.renderProjectedDayBalance()))
	}
	reservedLines := 0
	if m.form.stale {
		reservedLines++
	}
	if m.formError != "" {
		reservedLines++
	}
	lines = compactFormLines(lines, m.formLineBudget()-reservedLines, []string{
		"booked", "Issue info", "After        ", m.form.placement.Description(),
	})
	if m.form.stale {
		lines = append(lines, m.paint("Changed externally — reload before saving.", m.colors.Warning))
	}
	if m.formError != "" {
		lines = append(lines, m.paint(m.formError, m.colors.Destructive))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderFormRow(label, value string, focused bool) string {
	marker := "│"
	if focused {
		marker = "›"
	}
	label = fmt.Sprintf("%-12s", marker+" "+label)
	if focused {
		label = m.paintBold(label, m.colors.Focus)
	} else {
		label = m.secondary(label)
	}
	return label + " " + value
}

func (m model) renderFormSection(label, color string) string {
	return m.paint("─ "+label, color)
}

func (m model) renderFormSectionRow(label, value string, focused bool, color string) string {
	marker := "─"
	if focused {
		marker = "›"
	}
	label = fmt.Sprintf("%-12s", marker+" "+label)
	return m.paint(label, color) + " " + value
}

func (m model) renderCalculatedRow(label, value string) string {
	return m.paint(fmt.Sprintf("%-12s", label), m.colors.Information) + " " + value
}

func (p placementMode) Description() string {
	switch p {
	case fitPlacement:
		return "Find one continuous slot"
	case fillPlacement:
		return "Split work across the earliest available slots"
	default:
		return "Choose the start time"
	}
}

func (m model) renderPlacementSelector() string {
	parts := make([]string, 0, 3)
	for _, placement := range []placementMode{manualPlacement, fitPlacement, fillPlacement} {
		label := placement.String()
		if placement == m.form.placement {
			label = m.paint("["+label+"]", m.colors.Focus)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

func (m model) renderPlacementPreview() string {
	switch {
	case m.form.previewLoading:
		return m.paint("Calculating…", m.colors.Progress)
	case m.form.previewConflict != "" && len(m.form.previewRecords) > 0:
		return m.paint(formatPreviewWindows(m.form.previewRecords, m.workspace.cfg.Location)+" · "+m.form.previewConflict, m.colors.Destructive)
	case m.form.previewError != "":
		color := m.colors.Destructive
		if m.form.previewOvertimeAvailable {
			color = m.colors.Warning
		}
		return m.paint(m.form.previewError, color)
	case len(m.form.previewRecords) == 0:
		if m.form.placement == manualPlacement {
			return m.muted("Enter issue, start time, and duration")
		}
		return m.muted("Enter issue and duration")
	}
	return m.paint(formatPreviewWindows(m.form.previewRecords, m.workspace.cfg.Location), m.colors.Proposed)
}

func formatPreviewWindows(records []worklogs.LocalWorklog, location *time.Location) string {
	parts := make([]string, 0, min(2, len(records)))
	for index, record := range records {
		if index == 2 {
			break
		}
		start := record.StartedAtUTC.In(location)
		end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
		parts = append(parts, start.Format("15:04")+"–"+end.Format("15:04"))
	}
	preview := strings.Join(parts, " + ")
	if len(records) > 2 {
		preview += fmt.Sprintf(" + %d more", len(records)-2)
	}
	return fmt.Sprintf("%s · %s", pluralizeWorklogs(len(records)), preview)
}

func (m model) renderDayBalance() string {
	booked := m.dayBookedSeconds()
	remaining := m.dailyQuotaSeconds() - booked
	if remaining > 0 {
		return fmt.Sprintf("%s logged · %s remaining", formatDuration(booked), formatDuration(remaining))
	}
	if remaining < 0 {
		return fmt.Sprintf("%s logged · %s over target", formatDuration(booked), formatDuration(-remaining))
	}
	return fmt.Sprintf("%s logged · target met", formatDuration(booked))
}

func (m model) renderDayHeading(label string, width int) string {
	balance := m.renderDayBalance()
	if utf8.RuneCountInString(label)+3+utf8.RuneCountInString(balance) > width {
		return truncate(label+" · "+balance, width)
	}
	return label + " · " + m.renderStyledDayBalance()
}

func (m model) renderLoggedHeading(label string, seconds, width int) string {
	logged := formatDuration(seconds) + " logged"
	if utf8.RuneCountInString(label)+3+utf8.RuneCountInString(logged) > width {
		return truncate(label+" · "+logged, width)
	}
	return label + " · " + m.paint(logged, m.colors.Information)
}

func (m model) renderStyledDayBalance() string {
	booked := m.dayBookedSeconds()
	remaining := m.dailyQuotaSeconds() - booked
	logged := m.paint(formatDuration(booked)+" logged", m.colors.Information)
	switch {
	case remaining > 0:
		return logged + " · " + m.paint(formatDuration(remaining)+" remaining", m.colors.Warning)
	case remaining < 0:
		return logged + " · " + m.paint(formatDuration(-remaining)+" over target", m.colors.Warning)
	default:
		return logged + " · " + m.paint("target met", m.colors.Success)
	}
}

func (m model) renderProjectedDayBalance() string {
	if m.form.previewLoading {
		return m.muted("Waiting for placement preview")
	}
	if len(m.form.previewRecords) == 0 {
		return m.muted("No projected change")
	}
	projected := m.dayBookedSeconds()
	for _, record := range m.form.previewRecords {
		projected += record.DurationSeconds
	}
	remaining := m.dailyQuotaSeconds() - projected
	result := formatDuration(projected) + " total"
	if remaining > 0 {
		return m.muted(result + " · " + formatDuration(remaining) + " below daily target")
	}
	if remaining < 0 {
		return m.paint(result+" · "+formatDuration(-remaining)+" over daily target", m.colors.Warning)
	}
	return m.paint(result+" · daily target met", m.colors.Proposed)
}

func (m model) renderIssueContext() string {
	if m.issueMetadata == nil || m.form == nil {
		return ""
	}
	key := strings.ToUpper(strings.TrimSpace(m.form.inputs[issueInput].Value()))
	metadata, ok := m.issueMetadata[key]
	if !ok {
		return ""
	}
	parts := make([]string, 0, 2)
	if metadata.SourceAdapterFamily != "" {
		parts = append(parts, strings.ReplaceAll(metadata.SourceAdapterFamily, "_", " "))
	}
	if metadata.MaxEstimateSeconds != nil {
		parts = append(parts, "estimate "+formatDuration(int(*metadata.MaxEstimateSeconds)))
	}
	return strings.Join(parts, " · ")
}

func (m model) dayBookedSeconds() int {
	if m.dayContext.Date != "" {
		return m.dayContext.BookedSeconds
	}
	total := 0
	for _, item := range m.worklogs {
		total += item.DurationSeconds
	}
	return total
}

func (m model) dailyQuotaSeconds() int {
	if m.contextSettings.DailyMinimumQuotaSeconds > 0 {
		return m.contextSettings.DailyMinimumQuotaSeconds
	}
	if m.workspace.cfg.DailyMinimumQuotaSeconds > 0 {
		return m.workspace.cfg.DailyMinimumQuotaSeconds
	}
	return 8 * 60 * 60
}

func (m model) formContentWidth() int {
	return max(24, m.width-35)
}

func (m model) formLineBudget() int {
	bodyHeight := m.height - 3
	topHeight := max(10, bodyHeight/2)
	return max(1, bodyHeight-topHeight-2)
}

func compactFormLines(lines []string, maximum int, optionalPatterns []string) []string {
	for _, pattern := range optionalPatterns {
		if len(lines) <= maximum {
			break
		}
		for index, line := range lines {
			if strings.Contains(line, pattern) {
				lines = append(lines[:index], lines[index+1:]...)
				break
			}
		}
	}
	return lines
}

func pluralizeWorklogs(count int) string {
	if count == 1 {
		return "1 worklog"
	}
	return fmt.Sprintf("%d worklogs", count)
}
