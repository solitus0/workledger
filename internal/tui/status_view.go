package tui

import (
	"fmt"
	"strings"
)

func (m model) renderStatusWorkspace(width, height int) string {
	if m.statusView == statusConfigView {
		return m.renderStatusConfigWorkspace(width, height)
	}
	return m.renderStatusDiagnosticsWorkspace(width, height)
}

func (m model) renderStatusDiagnosticsWorkspace(width, height int) string {
	listWidth, _ := inspectorWidths(width)
	ok, warnings, failures := statusCounts(m.statusItems)
	checked := "Not checked"
	if !m.statusChecked.IsZero() {
		checked = "Checked " + m.statusChecked.In(m.selectedDate.Location()).Format("02 Jan 15:04")
	}
	lines := []string{m.paintBold("Status diagnostics", m.colors.Secondary), m.renderStatusSelectors(),
		m.paint(fmt.Sprintf("OK %d", ok), m.colors.Success) + " · " + m.paint(fmt.Sprintf("Warnings %d", warnings), m.colors.Warning) + " · " + m.paint(fmt.Sprintf("Errors %d", failures), m.colors.Destructive), m.muted(checked), "", m.muted("  Category   Target                 State")}
	lines = append(lines, m.statusRows(listWidth-2, height-len(lines))...)
	if len(m.statusItems) == 0 {
		message := "No diagnostics"
		if m.statusLoading {
			message = "Loading diagnostics…"
		} else if m.statusErr != "" {
			message = "Status check failed"
		}
		lines = append(lines, message)
	}
	detail := m.renderStatusDetail()
	return m.inspector(strings.Join(lines, "\n"), detail, width, height)
}

func (m model) renderStatusDetail() string {
	detail := "Select a diagnostic to inspect its full message."
	if m.statusErr != "" {
		detail = m.paint("Status check failed", m.colors.Destructive) + "\n\n" + m.statusErr
	} else if len(m.statusItems) > 0 {
		item := m.statusItems[clampIndex(m.statusSelected, len(m.statusItems))]
		detail = fmt.Sprintf("Diagnostic details\n\nState %s\nCategory %s\nTarget %s\nFailure kind %s\n\n%s", m.paintState(item.Status), item.Category, item.Target, emptyDash(string(item.FailureKind)), m.paintStateMessage(item.Status, item.Message))
	}
	return detail
}

func (m model) renderStatusConfigWorkspace(width, height int) string {
	listWidth, _ := inspectorWidths(width)
	rows := m.statusConfigRows()
	lines := []string{m.paintBold("Effective configuration", m.colors.Secondary), m.renderStatusSelectors(), ""}
	if len(m.statusConfigIssues) > 0 {
		lines = append(lines, m.paint("Configuration validation errors", m.colors.Destructive))
	}
	lines = append(lines, m.muted("  Field                    Value"))
	start, end := visibleRange(m.statusConfigSelected, len(rows), height-len(lines))
	for index := start; index < end; index++ {
		row := rows[index]
		prefix := "  "
		if index == m.statusConfigSelected {
			prefix = "> "
		}
		lines = append(lines, m.selectedRow(fmt.Sprintf("%s%-24s %s", prefix, truncate(row.field, 24), row.value), listWidth-2, index == m.statusConfigSelected))
	}
	detail := m.renderConfigDetail()
	if len(rows) == 0 {
		message := "No configuration details"
		if m.statusLoading {
			message = "Loading configuration…"
		}
		lines = append(lines, message)
	}
	return m.inspector(strings.Join(lines, "\n"), detail, width, height)
}

func (m model) renderConfigDetail() string {
	rows := m.statusConfigRows()
	detail := "Select a configuration field to inspect its full value."
	if m.statusErr != "" {
		detail = "Configuration check failed\n\n" + m.statusErr
	} else if len(rows) > 0 {
		row := rows[clampIndex(m.statusConfigSelected, len(rows))]
		detail = "Configuration details\n\nField " + row.field + "\n\nValue\n" + row.value + "\n\nRead-only effective configuration."
		if len(m.statusConfigIssues) > 0 {
			detail = m.paint("Validation issue", m.colors.Destructive) + "\n\nField " + row.field + "\n\nMessage\n" + m.paint(row.value, m.colors.Destructive)
		}
	}
	return detail
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
	start, end := visibleRange(m.statusSelected, len(m.statusItems), height)
	rows := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		item := m.statusItems[index]
		prefix := "  "
		if index == m.statusSelected {
			prefix = "> "
		}
		row := fmt.Sprintf("%s%-10s %-22s %s", prefix, truncate(item.Category, 10), truncate(item.Target, 22), m.paintState(item.Status))
		rows = append(rows, m.selectedRow(row, width, index == m.statusSelected))
	}
	return rows
}
