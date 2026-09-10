package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m model) renderPresetWorkspace(width, height int) string {
	if m.workspace == nil {
		return m.box("Presets unavailable", width, height, false)
	}
	if m.presetForm != nil {
		return m.box(m.renderPresetForm(), width, height, true)
	}
	topHeight := max(10, height/2)
	detailHeight := height - topHeight
	lines := []string{"Worklog presets", "", m.muted(fmt.Sprintf("  %-22s %-14s %-7s %-10s %s", "NAME", "ISSUE", "START", "DURATION", "DESCRIPTION"))}
	start, end := visibleRange(m.presetSelected, len(m.presets), max(1, topHeight-5))
	for index := start; index < end; index++ {
		item := m.presets[index]
		prefix := "  "
		if index == m.presetSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		lines = append(lines, fmt.Sprintf("%s%-22s %-14s %-7s %-10s %s", prefix, truncate(item.Name, 22), item.IssueKey, item.StartTime, formatDuration(item.DurationSeconds), truncate(item.Description, max(8, width-65))))
	}
	if len(m.presets) == 0 {
		if m.presetLoading {
			lines = append(lines, "  "+m.paint("Loading…", m.colors.Progress))
		} else {
			lines = append(lines, "  No presets yet.")
		}
	}
	detail := "No preset selected."
	if item := m.selectedPreset(); item != nil {
		lastUsed := "Never"
		if item.LastUsedAt != nil {
			lastUsed = item.LastUsedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04")
		}
		detail = fmt.Sprintf("Name        %s\nIssue       %s\nStart time  %s\nDuration    %s\nLast used   %s\nUpdated     %s\n\n%s", item.Name, item.IssueKey, item.StartTime, formatDuration(item.DurationSeconds), lastUsed, item.UpdatedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), item.Description)
	}
	if m.overlay == deletePresetOverlay {
		detail = m.renderConfirmation(width-2, detailHeight-2)
	}
	confirmationOpen := m.overlay == deletePresetOverlay
	return lipgloss.JoinVertical(lipgloss.Left, m.box(strings.Join(lines, "\n"), width, topHeight, m.focus != focusRail && !confirmationOpen), m.box(detail, width, detailHeight, false))
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
		lines = append(lines, prefix+item.Name+"  "+m.muted(presetSummary(item)))
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
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func formatPresetTimestamp(value *time.Time, location *time.Location) string {
	if value == nil {
		return "Never"
	}
	return value.In(location).Format("2006-01-02 15:04")
}
