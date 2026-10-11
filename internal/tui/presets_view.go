package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m model) renderPresetWorkspace(width, height int) string {
	if m.workspace == nil {
		return m.pane("Presets unavailable", width, height)
	}
	if m.presetForm != nil {
		return m.pane(m.renderPresetForm(), width, height)
	}
	listWidth, _ := inspectorWidths(width)
	lines := []string{m.paintBold("Worklog presets", m.colors.Secondary), "", m.muted("  Name                 Issue        Duration")}
	start, end := visibleRange(m.presetSelected, len(m.presets), height-len(lines))
	for index := start; index < end; index++ {
		item := m.presets[index]
		prefix := "  "
		if index == m.presetSelected {
			prefix = "> "
		}
		lines = append(lines, m.selectedRow(fmt.Sprintf("%s%-20s %-12s %s", prefix, truncate(item.Name, 20), truncate(item.IssueKey, 12), formatEstimateDuration(int64(item.DurationSeconds))), listWidth-2, index == m.presetSelected))
	}
	if len(m.presets) == 0 {
		message := "No presets yet."
		if m.presetLoading {
			message = "Loading…"
		}
		lines = append(lines, message)
	}
	detail := m.renderPresetDetail()
	return m.inspector(strings.Join(lines, "\n"), detail, width, height)
}

func (m model) renderPresetDetail() string {
	detail := "No preset selected."
	if item := m.selectedPreset(); item != nil {
		lastUsed := "Never"
		if item.LastUsedAt != nil {
			lastUsed = item.LastUsedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04")
		}
		detail = fmt.Sprintf("Preset details\n\nName %s\nIssue %s\nStart time %s\nDuration %s\n\nLast used\n%s\nUpdated\n%s\n\n%s", item.Name, item.IssueKey, item.StartTime, formatDuration(item.DurationSeconds), lastUsed, item.UpdatedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), item.Description)
	}
	return detail
}

func (m model) renderPresetForm() string {
	title := "Add worklog preset"
	if m.presetForm.editing {
		title = "Edit worklog preset"
	}
	labels := []string{"Name", "Issue", "Start time", "Duration", "Description"}
	lines := []string{title, ""}
	for index, label := range labels {
		lines = append(lines, m.renderFormRow(label, m.presetForm.inputs[index].View(), index == m.presetForm.focus))
	}
	lines = append(lines, "", m.muted("Applying a preset creates an ordinary worklog; existing worklogs are never linked or changed."))
	if m.presetForm.stale {
		lines = append(lines, m.paint("Changed externally — cancel and reopen before saving.", m.colors.Warning))
	}
	if m.presetFormError != "" {
		lines = append(lines, m.paint(m.presetFormError, m.colors.Destructive))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderPresetPicker(width, height int) string {
	lines := []string{m.paint("Add worklog from preset", m.colors.Focus), ""}
	if m.presetPicker.searching {
		lines = append(lines, "Find preset", m.presetPicker.input.View(), "")
	}
	for index, item := range m.presetPicker.items {
		prefix := "  "
		if m.presetPicker.selected == index {
			prefix = m.paint("> ", m.colors.Focus)
		}
		lines = append(lines, m.selectedRow(prefix+item.Name+"  "+m.muted(presetSummary(item)), width-12, index == m.presetPicker.selected))
		if len(lines) >= min(14, height-5) {
			break
		}
	}
	if len(m.presetPicker.items) == 0 {
		lines = append(lines, m.muted("  No matching presets"))
	}
	modalWidth := min(76, width-8)
	longest := 0
	for _, line := range lines {
		longest = max(longest, lipgloss.Width(line))
	}
	modalWidth = min(modalWidth, max(48, longest+4))
	modal := m.box(strings.Join(lines, "\n"), modalWidth, min(max(10, len(lines)+2), height-4), true)
	background := m
	background.presetPicker = nil
	base := background.renderWorkspace(width, height)
	return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(modal).X((width-lipgloss.Width(modal))/2).Y((height-lipgloss.Height(modal))/2).Z(1)).Render()
}

func formatPresetTimestamp(value *time.Time, location *time.Location) string {
	if value == nil {
		return "Never"
	}
	return value.In(location).Format("2006-01-02 15:04")
}
