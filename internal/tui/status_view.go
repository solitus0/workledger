package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m model) renderStatusWorkspace(width, height int) string {
	if m.statusView == statusConfigView {
		return m.renderStatusConfigWorkspace(width, height)
	}
	return m.renderStatusDiagnosticsWorkspace(width, height)
}

func (m model) renderStatusDiagnosticsWorkspace(width, height int) string {
	topHeight := max(10, height/2)
	detailHeight := height - topHeight
	rows := make([]string, 0)
	if m.statusLoading && len(m.statusItems) == 0 {
		for _, category := range []string{"local", "env", "routing", "connectivity"} {
			rows = append(rows, fmt.Sprintf("  %-14s %-26s %s", category, "loading", m.paint("…", m.colors.Progress)))
		}
	} else {
		rows = append(rows, m.statusRows(width-6, topHeight-7)...)
	}
	header := fmt.Sprintf("  %-14s %-26s %-10s %s", "CATEGORY", "TARGET", "STATE", "MESSAGE")
	table := "Status diagnostics\n" + m.renderStatusSelectors() + "\n\n" + m.muted(header) + "\n" + strings.Join(rows, "\n")
	if len(rows) == 0 {
		if m.statusErr != "" {
			table += "\n  " + m.paint("Status check failed", m.colors.Destructive)
		} else {
			table += "\n  No diagnostics"
		}
	}
	detail := "Select a diagnostic to inspect its full message."
	if m.statusErr != "" {
		detail = m.paint("Status check failed", m.colors.Destructive) + "\n\n" + m.paint(m.statusErr, m.colors.Destructive)
	} else if len(m.statusItems) > 0 {
		item := m.statusItems[clampIndex(m.statusSelected, len(m.statusItems))]
		detail = fmt.Sprintf("Target       %s\nCategory     %s\nState        %s\nFailure kind %s\n\n%s",
			item.Target, item.Category, m.paintState(item.Status), emptyDash(string(item.FailureKind)), m.paintStateMessage(item.Status, item.Message))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.box(table, width, topHeight, m.focus != focusRail),
		m.box(detail, width, detailHeight, false),
	)
}

func (m model) renderStatusConfigWorkspace(width, height int) string {
	topHeight := max(10, height/2)
	detailHeight := height - topHeight
	rows := m.statusConfigRows()
	visibleRows := make([]string, 0)
	if m.statusLoading && len(rows) == 0 {
		visibleRows = append(visibleRows, "  "+m.paint("Loading configuration…", m.colors.Progress))
	} else {
		start, end := visibleRange(m.statusConfigSelected, len(rows), topHeight-8)
		visibleRows = make([]string, 0, end-start)
		for index := start; index < end; index++ {
			row := rows[index]
			prefix := "  "
			if index == m.statusConfigSelected {
				prefix = m.paint("> ", m.colors.Focus)
			}
			visibleRows = append(visibleRows, fmt.Sprintf("%s%-30s %s", prefix, truncate(row.field, 30), truncate(row.value, max(8, width-39))))
		}
	}

	tableLines := []string{
		"Effective configuration",
		m.renderStatusSelectors(),
		"",
	}
	if len(m.statusConfigIssues) > 0 {
		tableLines = append(tableLines, m.paint("Configuration validation errors", m.colors.Destructive))
	}
	tableLines = append(tableLines, m.muted(fmt.Sprintf("  %-30s %s", "FIELD", "VALUE")), strings.Join(visibleRows, "\n"))
	table := strings.Join(tableLines, "\n")
	if len(rows) == 0 && !m.statusLoading {
		table += "\n  No configuration details"
	}

	detail := "Select a configuration field to inspect its full value."
	if m.statusErr != "" {
		detail = m.paint("Configuration check failed", m.colors.Destructive) + "\n\n" + m.paint(m.statusErr, m.colors.Destructive)
	} else if len(rows) > 0 {
		row := rows[clampIndex(m.statusConfigSelected, len(rows))]
		if len(m.statusConfigIssues) > 0 {
			detail = fmt.Sprintf("%s\n\nField   %s\nMessage %s", m.paint("Validation issue", m.colors.Destructive), row.field, m.paint(row.value, m.colors.Destructive))
		} else {
			detail = fmt.Sprintf("Field %s\nValue %s\n\nRead-only effective configuration.", row.field, row.value)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.box(table, width, topHeight, m.focus != focusRail),
		m.box(detail, width, detailHeight, false),
	)
}

func (m model) renderStatusSelectors() string {
	diagnostics, configuration := m.secondary("Status"), m.secondary("Config")
	if m.statusView == statusConfigView {
		configuration = m.paint("[Config]", m.colors.Focus)
	} else {
		diagnostics = m.paint("[Status]", m.colors.Focus)
	}
	return diagnostics + "  " + configuration
}

type statusConfigRow struct {
	field string
	value string
}

func (m model) statusConfigRows() []statusConfigRow {
	if len(m.statusConfigIssues) > 0 {
		rows := make([]statusConfigRow, 0, len(m.statusConfigIssues))
		for _, issue := range m.statusConfigIssues {
			rows = append(rows, statusConfigRow{field: emptyDash(issue.Field), value: issue.Message})
		}
		return rows
	}
	if m.statusConfig == nil {
		return nil
	}
	summary := m.statusConfig
	return []statusConfigRow{
		{field: "config_path", value: summary.ConfigPath},
		{field: "default_output", value: summary.DefaultOutput},
		{field: "sqlite_path", value: summary.SQLitePath},
		{field: "local_timezone", value: emptyDash(summary.LocalTimezone)},
		{field: "minimum_duration_seconds", value: fmt.Sprint(summary.MinimumDurationSeconds)},
		{field: "daily_minimum_quota_seconds", value: fmt.Sprint(summary.DailyMinimumQuotaSeconds)},
		{field: "day_start", value: summary.DayStart},
		{field: "day_end", value: summary.DayEnd},
		{field: "daily_lunch", value: summary.DailyLunch},
		{field: "jira_instance_count", value: fmt.Sprint(summary.JiraInstanceCount)},
		{field: "unique_env_var_count", value: fmt.Sprint(summary.UniqueEnvVarCount)},
		{field: "missing_env_var_count", value: fmt.Sprint(summary.MissingEnvVarCount)},
		{field: "unique_routed_prefix_count", value: fmt.Sprint(summary.UniqueRoutedPrefixCount)},
		{field: "reporting_target_count", value: fmt.Sprint(summary.ReportingTargetCount)},
		{field: "clockify_mapping_count", value: fmt.Sprint(summary.ClockifyMappingCount)},
	}
}

func (m model) statusRows(width, height int) []string {
	if len(m.statusItems) == 0 {
		return nil
	}
	start, end := visibleRange(m.statusSelected, len(m.statusItems), height)
	rows := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		item := m.statusItems[index]
		prefix := "  "
		if index == m.statusSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		state := m.statusPaint(item.Status)
		messageWidth := max(8, width-56)
		rows = append(rows, fmt.Sprintf("%s%-14s %-26s %-10s %s", prefix, truncate(item.Category, 14), truncate(item.Target, 26), state, truncate(item.Message, messageWidth)))
	}
	return rows
}
