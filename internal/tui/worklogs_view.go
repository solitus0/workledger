package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderWorklogWorkspace(width, height int) string {
	if m.workspace == nil {
		message := "Loading local workspace…"
		if !m.workspaceLoading {
			message = "Worklogs unavailable\n\nFix configuration or local storage in Status."
		}
		return m.pane(message, width, height)
	}
	if m.form != nil {
		return m.renderWorklogFormWorkspace(width, height)
	}
	if m.worklogView == weekWorklogView {
		return m.renderWeekWorkspace(width, height)
	}
	if m.worklogView == monthWorklogView {
		return m.renderMonthWorkspace(width, height)
	}
	contentWidth := width - 2
	selector := m.renderWorklogViewSelector()
	balance := m.renderDayQuota()
	heading := balance + strings.Repeat(" ", max(2, contentWidth-lipgloss.Width(balance)-lipgloss.Width(selector))) + selector
	lines := []string{heading, ""}
	lines = append(lines, strings.Split(m.renderDayTimelineWidth(nil, m.selectedWorklogID(), contentWidth), "\n")...)
	lines = append(lines, "", m.renderTimelineLegend(nil, m.selectedWorklogID()), m.rule(contentWidth))
	chrome := m.pane(strings.Join(lines, "\n"), width, len(lines))
	remaining := height - len(lines)
	listWidth, detailWidth := inspectorWidths(width)
	availableHeight := min(8, max(4, remaining/2))
	rowHeight := max(2, remaining-availableHeight)
	list := m.muted(m.worklogTableHeader(listWidth-2)) + "\n" + strings.Join(m.worklogRows(listWidth-2, rowHeight-1), "\n")
	if m.worklogFilter != nil {
		list = m.paint("Find ", m.colors.Focus) + m.worklogFilter.input.View() + "\n" + m.muted(m.worklogTableHeader(listWidth-2)) + "\n" + strings.Join(m.worklogRows(listWidth-2, rowHeight-2), "\n")
	}
	if len(m.visibleWorklogIndices()) == 0 {
		message := "No worklogs yet."
		if m.worklogFilter != nil {
			message = "No matching worklogs."
		}
		if m.worklogLoading {
			message = "Loading…"
		}
		list += "\n" + message
	}
	list = lipgloss.NewStyle().Height(rowHeight).MaxHeight(rowHeight).Render(list) + "\n" + m.renderAvailableWindows(listWidth-2, availableHeight)
	detail := m.renderWorklogInspector(detailWidth-2, remaining)
	body := m.inspector(list, detail, width, remaining)
	return lipgloss.JoinVertical(lipgloss.Left, chrome, body)
}

func (m model) renderDayQuota() string {
	booked, quota := m.dayBookedSeconds(), m.dailyQuotaSeconds()
	label := m.paint(formatEstimateDuration(int64(booked)), m.colors.Information) + " logged / " + formatEstimateDuration(int64(quota)) + " target"
	switch remaining := quota - booked; {
	case remaining > 0:
		return label + "  " + m.paint(formatEstimateDuration(int64(remaining))+" remaining", m.colors.Warning)
	case remaining < 0:
		return label + "  " + m.paint(formatEstimateDuration(int64(-remaining))+" over target", m.colors.Warning)
	default:
		return label + "  " + m.paint("target met", m.colors.Success)
	}
}

func (m model) worklogTableHeader(width int) string {
	return truncate(fmt.Sprintf("  %-3s %-5s %-8s %-12s %s", "#", "Start", "Duration", "Issue", "Summary"), width)
}

func (m model) renderAvailableWindows(width, height int) string {
	lines := []string{m.rule(width), m.paintBold("Available windows", m.colors.Secondary)}
	slots := m.dayContext.FreeSlots
	if m.dayContext.Date == "" {
		return strings.Join(append(lines, m.muted("Availability loading…")), "\n")
	}
	total := 0
	for _, slot := range slots {
		total += slot.DurationSeconds
	}
	lunch := m.contextSettings.Lunch
	lunchRows := 0
	if lunch != nil && height >= 5 {
		lunchRows = 1
	}
	count := min(len(slots), max(0, height-3-lunchRows))
	for _, slot := range slots[:count] {
		lines = append(lines, fmt.Sprintf("%s–%s  %s", slot.Start.In(m.workspace.cfg.Location).Format("15:04"), slot.End.In(m.workspace.cfg.Location).Format("15:04"), formatEstimateDuration(int64(slot.DurationSeconds))))
	}
	summary := formatEstimateDuration(int64(total)) + " available"
	if count < len(slots) {
		label := "windows"
		if len(slots)-count == 1 {
			label = "window"
		}
		summary += fmt.Sprintf(" · %d more %s", len(slots)-count, label)
	}
	if len(slots) == 0 {
		summary = "No available windows."
	}
	lines = append(lines, m.muted(truncate(summary, width)))
	if lunchRows > 0 {
		lines = append(lines, m.muted("Lunch "+lunch.Start+"–"+lunch.End+" excluded"))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWorklogInspector(width, height int) string {
	item := m.selectedWorklog()
	if item == nil {
		return m.paintBold("Worklog details", m.colors.Secondary) + "\n\nNo worklog selected."
	}
	metadata := m.issueMetadata[item.IssueKey]
	lines := []string{m.paintBold("Worklog details", m.colors.Secondary), "Issue " + item.IssueKey}
	if metadata.Title != "" {
		lines = append(lines, m.secondary(truncate(metadata.Title, width)))
	}
	lines = append(lines, "Local issue total", m.renderIssueTotal(item.IssueKey))
	if metadata.MaxEstimateSeconds != nil && *metadata.MaxEstimateSeconds > 0 {
		usage := "Unavailable"
		if m.issueTotalLoading {
			usage = "Loading…"
		} else if m.issueTotalIssue == item.IssueKey && m.issueTotalErr == "" {
			usage = fmt.Sprintf("%.1f%%", 100*float64(m.issueTotal)/float64(*metadata.MaxEstimateSeconds))
		}
		lines = append(lines, "Estimate used: "+usage)
		if usage != "Unavailable" && usage != "Loading…" {
			lines = append(lines, formatEstimateDuration(int64(m.issueTotal))+" / "+formatEstimateDuration(*metadata.MaxEstimateSeconds))
			if int64(m.issueTotal) > *metadata.MaxEstimateSeconds {
				lines = append(lines, m.paint(formatEstimateDuration(int64(m.issueTotal)-*metadata.MaxEstimateSeconds)+" over estimate", m.colors.Warning))
			}
		}
	}
	if len(lines) > height {
		// Keep estimate warnings visible when Activity reduces the height.
		index := slices.Index(lines, "Local issue total")
		lines[index] += " " + lines[index+1]
		if lipgloss.Width(lines[index]) > width {
			lines[index] = "Total " + lines[index+1]
		}
		lines = slices.Delete(lines, index+1, index+2)
	}
	local := item.StartedAtUTC.In(m.workspace.cfg.Location)
	end := local.Add(time.Duration(item.DurationSeconds) * time.Second)
	if height >= len(lines)+4 {
		lines = append(lines, "", m.rule(width), "Time "+local.Format("15:04")+"–"+end.Format("15:04"), "Duration "+formatEstimateDuration(int64(item.DurationSeconds)))
	}
	budget := height - len(lines) - 2
	if budget > 0 {
		description := wrapIssueDescription(item.Description, width)
		lines = append(lines, "", m.secondary("Description"))
		lines = append(lines, description[:min(len(description), budget)]...)
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWorklogFormWorkspace(width, height int) string {
	contentWidth := width - 2
	lines := []string{m.renderDayQuota(), "", m.renderDayTimelineWidth(m.form.previewRecords, "", contentWidth), "", m.renderTimelineLegend(m.form.previewRecords, ""), m.rule(contentWidth)}
	chromeHeight := lipgloss.Height(strings.Join(lines, "\n"))
	_, detailWidth := inspectorWidths(width)
	return lipgloss.JoinVertical(lipgloss.Left, m.pane(strings.Join(lines, "\n"), width, chromeHeight),
		m.inspector(m.renderForm(height-chromeHeight), m.renderPlacementInspector(detailWidth-2, height-chromeHeight), width, height-chromeHeight))
}

func (m model) renderPlacementInspector(width, height int) string {
	lines := []string{m.renderFormSection("Calculated", m.colors.Information), ""}
	if context := m.renderIssueContext(); context != "" {
		lines = append(lines, wrapIssueDescription(context, width)...)
		lines = append(lines, "")
	}
	if m.form.editing {
		lines = append(lines, "Update one worklog", "", "ID", m.form.id)
		return strings.Join(lines, "\n")
	}
	switch {
	case m.form.previewLoading:
		lines = append(lines, m.paint("Calculating…", m.colors.Progress))
	case m.form.previewError != "" && len(m.form.previewRecords) == 0:
		color := m.colors.Destructive
		if m.form.previewOvertimeAvailable {
			color = m.colors.Warning
		}
		lines = append(lines, wrapIssueDescription(m.paint(m.form.previewError, color), width)...)
	case len(m.form.previewRecords) == 0:
		lines = append(lines, wrapIssueDescription(m.renderPlacementPreview(), width)...)
	default:
		lines = append(lines, m.paint(pluralizeWorklogs(len(m.form.previewRecords))+" · "+m.form.placement.String(), m.colors.Proposed))
		for _, record := range m.form.previewRecords {
			if len(lines) >= height-5 {
				lines = append(lines, m.muted("Additional slots on timeline"))
				break
			}
			start := record.StartedAtUTC.In(m.workspace.cfg.Location)
			end := start.Add(time.Duration(record.DurationSeconds) * time.Second)
			lines = append(lines, m.paint(start.Format("15:04")+"–"+end.Format("15:04")+"  "+formatEstimateDuration(int64(record.DurationSeconds)), m.colors.Proposed))
		}
	}
	lines = append(lines, "", m.secondary("After saving"))
	lines = append(lines, wrapIssueDescription(m.renderProjectedDayBalance(), width)...)
	if m.form.previewConflict != "" {
		lines = append(lines, "", m.paint(m.form.previewConflict, m.colors.Destructive))
	} else if !m.form.overtime {
		_, endText := m.workdayBounds()
		boundary, err := m.clockOnSelectedDay(endText)
		if err == nil {
			for _, record := range m.form.previewRecords {
				if record.StartedAtUTC.Before(boundary) && record.StartedAtUTC.Add(time.Duration(record.DurationSeconds)*time.Second).After(boundary) {
					lines = append(lines, "")
					lines = append(lines, wrapIssueDescription("Starts before "+endText+"; may finish later.", width)...)
					break
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderWeekWorkspace(width, height int) string {
	weekStart, weekEnd := weekBounds(m.selectedDate)
	heading := fmt.Sprintf("Week · %s–%s · %s logged", weekStart.Format("2006-01-02"), weekEnd.Format("2006-01-02"), formatDuration(m.contextSummary.BookedSeconds))
	chrome := m.pane(heading+"\n"+m.renderWorklogViewSelector()+"\n", width, 3)
	listWidth, detailWidth := inspectorWidths(width)
	lines := []string{m.muted("  DAY DATE       Booked Logs Delta    !")}
	for _, day := range m.contextDays {
		date, err := time.ParseInLocation("2006-01-02", day.Date, m.workspace.cfg.Location)
		if err != nil {
			continue
		}
		selected := sameDay(date, m.selectedDate)
		prefix := "  "
		if selected {
			prefix = "> "
		}
		delta := "—"
		if isWeekday(date) {
			delta = formatDelta(day.BookedSeconds - m.dailyQuotaSeconds())
		}
		row := fmt.Sprintf("%s%-3s %-10s %-6s %4d %-8s %d", prefix, date.Format("Mon"), day.Date, formatEstimateDuration(int64(day.BookedSeconds)), len(day.Worklogs), delta, len(day.Collisions))
		lines = append(lines, m.selectedRow(row, listWidth-2, selected))
	}
	if len(m.contextDays) == 0 {
		lines = append(lines, "No weekly context available.")
	}
	lines = append(lines, "", m.rule(listWidth-2), m.paintBold("Selected day", m.colors.Secondary), m.renderStyledDayBalance())
	lines = append(lines, m.unselectedWorklogRows(listWidth-2, max(0, height-3-len(lines)))...)
	detail := m.renderWeekDayDetail(detailWidth-2, height-3)
	return lipgloss.JoinVertical(lipgloss.Left, chrome, m.inspector(strings.Join(lines, "\n"), detail, width, height-3))
}

func (m model) renderWorklogViewSelector() string {
	labels := []string{"Day", "Week", "Month"}
	for index, label := range labels {
		if worklogView(index) == m.worklogView {
			if m.deps.noColor || !m.colorReady {
				labels[index] = "[" + label + "]"
			} else {
				labels[index] = lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Focus)).Reverse(true).Bold(true).Render(" " + label + " ")
			}
		} else {
			labels[index] = m.secondary(label)
		}
	}
	return strings.Join(labels, "  ")
}

func (m model) renderWeekDayDetail(width, height int) string {
	lines := []string{m.selectedDate.Format("Monday, 2006-01-02"), "", m.renderDayTimelineWidth(nil, "", width), ""}
	lines = append(lines, wrapIssueDescription(m.renderTimelineLegendWidth(nil, "", width), width)...)
	lines = append(lines, m.renderAvailableWindows(width, max(4, height-len(lines))))
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
	indices := m.visibleWorklogIndices()
	if len(indices) == 0 {
		return nil
	}
	position := slices.Index(indices, m.worklogSelected)
	start, end := visibleRange(position, len(indices), height)
	rows := make([]string, 0, end-start)
	for _, index := range indices[start:end] {
		item := m.worklogs[index]
		prefix := "  "
		if index == m.worklogSelected {
			prefix = "> "
		}
		local := item.StartedAtUTC.In(m.workspace.cfg.Location)
		row := fmt.Sprintf("%s%-3d %-5s %-8s %-12s %s", prefix, index+1, local.Format("15:04"), formatEstimateDuration(int64(item.DurationSeconds)), truncate(item.IssueKey, 12), item.Description)
		rows = append(rows, m.selectedRow(row, width, index == m.worklogSelected))
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
	metadata := m.issueMetadata[item.IssueKey]
	extra := ""
	if metadata.Title != "" {
		extra += "\nTitle             " + metadata.Title
	}
	if metadata.MaxEstimateSeconds != nil && *metadata.MaxEstimateSeconds > 0 {
		usage := "Unavailable"
		if m.issueTotalIssue == item.IssueKey && !m.issueTotalLoading && m.issueTotalErr == "" {
			maximum := *metadata.MaxEstimateSeconds
			usage = fmt.Sprintf("%.1f%% (%s / %s)", 100*float64(m.issueTotal)/float64(maximum), formatEstimateDuration(int64(m.issueTotal)), formatEstimateDuration(maximum))
			if int64(m.issueTotal) > maximum {
				usage += " · " + formatEstimateDuration(int64(m.issueTotal)-maximum) + " over estimate"
			}
		} else if m.issueTotalLoading {
			usage = "Loading…"
		}
		extra += "\nEstimate used:    " + usage
	}
	return fmt.Sprintf("Issue             %s%s\nLocal issue total %s\nStart             %s\nDuration          %s\nCreated           %s\nUpdated           %s\nID                %s\n\n%s",
		item.IssueKey, extra, m.renderIssueTotal(item.IssueKey), local.Format("2006-01-02 15:04"), formatDuration(item.DurationSeconds),
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

func (m model) renderForm(height int) string {
	title := "Add worklog"
	if m.form.editing {
		title = "Edit worklog"
	}
	details := []string{m.renderFormSection("Worklog details", m.colors.Focus)}
	warnings := []string{}
	if m.form.stale {
		warnings = append(warnings, m.paint("Changed externally — reload before saving.", m.colors.Warning))
	}
	if m.formError != "" {
		warnings = append(warnings, wrapIssueDescription(m.paint(m.formError, m.colors.Destructive), m.formContentWidth())...)
	}
	for _, field := range m.formFields() {
		index, ok := inputIndexForField(field)
		if !ok {
			continue
		}
		label := map[formField]string{issueField: "Issue", startField: "Start", durationField: "Duration", descriptionField: "Description"}[field]
		if field == startField && !m.form.editing {
			label = "Start time"
		}
		details = append(details, m.renderFormRow(label, m.form.inputs[index].View(), field == m.form.focus))
	}
	essential := len(details) + len(warnings)
	options := 0
	if !m.form.editing {
		essential += 2 // Blank row and Placement cue.
		if m.form.placement != manualPlacement {
			options = 2
			essential += options
		}
	}
	lines := []string{}
	if height >= essential+2 {
		lines = append(lines, m.paintBold(title, m.colors.Focus), "")
	} else if height >= essential+1 {
		lines = append(lines, m.paintBold(title, m.colors.Focus))
	}
	lines = append(lines, warnings...)
	lines = append(lines, details...)
	if !m.form.editing {
		lines = append(lines, "", m.renderFormSectionRow("Placement", m.renderPlacementSelector(), m.form.focus == placementField, m.colors.Focus))
		if height > len(lines)+options {
			lines = append(lines, m.muted(m.form.placement.Description()))
		}
		if m.form.placement != manualPlacement {
			overtime, lunch := "[ ]", "[ ]"
			if m.form.overtime {
				overtime = "[x]"
			}
			if m.form.noLunch {
				lunch = "[x]"
			}
			_, dayEnd := m.workdayBounds()
			if height > len(lines)+options {
				lines = append(lines, "")
			}
			lines = append(lines, overtime+" Allow starts after "+dayEnd, lunch+" Use lunch time")
		}
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
	parts := make([]string, 0, 3)
	if metadata.Title != "" {
		parts = append(parts, metadata.Title)
	}
	if metadata.SourceAdapterFamily != "" {
		parts = append(parts, strings.ReplaceAll(metadata.SourceAdapterFamily, "_", " "))
	}
	if metadata.MaxEstimateSeconds != nil {
		estimate := "estimate " + formatEstimateDuration(*metadata.MaxEstimateSeconds)
		if metadata.EstimateSource == "manual" {
			estimate += " (manual)"
		}
		parts = append(parts, estimate)
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
	width := calculateLayout(m.width, m.height, m.activityVisible()).workspace.width
	if m.form != nil {
		width, _ = inspectorWidths(width)
	}
	return max(24, width-2)
}

func pluralizeWorklogs(count int) string {
	if count == 1 {
		return "1 worklog"
	}
	return fmt.Sprintf("%d worklogs", count)
}

// Estimates retain whole seconds; unlike worklogs, they need not be minute-aligned.
func formatEstimateDuration(seconds int64) string {
	parts := make([]string, 0, 3)
	if hours := seconds / 3600; hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes := (seconds % 3600) / 60; minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if remainder := seconds % 60; remainder > 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainder))
	}
	if len(parts) == 0 {
		return "0h"
	}
	return strings.Join(parts, "")
}
