package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
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
		lines = append(lines, m.selectedRow(fmt.Sprintf("%s%-20s %-12s %s", prefix, truncate(item.Name, 20), truncate(worklogs.DisplayIssueKey(item.IssueKey), 12), formatEstimateDuration(int64(item.DurationSeconds))), listWidth-2, index == m.presetSelected))
	}
	if len(m.presets) == 0 {
		message := "No presets yet. Add a preset for work you log regularly."
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
		detail = fmt.Sprintf("Preset details\n\nName %s\nIssue %s\nStart time %s\nDuration %s\n\nLast used\n%s\nUpdated\n%s\n\n%s", item.Name, worklogs.DisplayIssueKey(item.IssueKey), item.StartTime, formatDuration(item.DurationSeconds), lastUsed, item.UpdatedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), item.Description)
	}
	return detail
}

func (m model) renderPresetForm() string {
	title := "Add worklog preset"
	if m.presetForm.editing {
		title = "Edit worklog preset"
	}
	labels := []string{"Name", "Issue", "Start time", "Duration", "Description"}
	lines := []string{m.paintBold(title, m.colors.Focus), ""}
	for index, label := range labels {
		lines = append(lines, m.renderFormRow(label, m.inputView(m.presetForm.inputs[index], index == m.presetForm.focus), index == m.presetForm.focus))
		if m.presetFormError != "" && index == m.presetForm.errorIndex {
			lines = append(lines, m.fieldError(m.presetFormError, m.width-navigationWidth-3))
		}
	}
	lines = append(lines, "", m.muted("Applying a preset creates an ordinary worklog; existing worklogs are never linked or changed."))
	if m.presetForm.stale {
		lines = append(lines, m.paint("Changed externally — save to review reloading the latest record.", m.colors.Warning))
	}

	return strings.Join(lines, "\n")
}

func (m model) presetPickerRows() int {
	rows := min(12, m.height-appHeaderHeight-appFooterHeight-9)
	if m.presetPicker != nil && m.presetPicker.searching {
		rows -= 3
	}
	return max(1, rows)
}

func (m model) renderPresetPicker(width, height int) string {
	modalWidth := min(76, width-8)
	contentWidth := modalWidth - 4
	lines := []string{m.paintBold("Add worklog from preset", m.colors.Focus), ""}
	if m.presetPicker.searching {
		lines = append(lines, "Find preset", m.presetPicker.input.View(), "")
	}
	start, end := visibleRange(m.presetPicker.selected, len(m.presetPicker.items), m.presetPickerRows())
	for i := start; i < end; i++ {
		item := m.presetPicker.items[i]
		prefix := "  "
		if i == m.presetPicker.selected {
			prefix = "> "
		}
		lines = append(lines, m.selectedRow(prefix+item.Name+"  "+presetSummary(item), contentWidth, i == m.presetPicker.selected))
	}
	if len(m.presetPicker.items) == 0 {
		lines = append(lines, m.muted("No matching presets. Clear the filter to browse."))
	} else {
		lines = append(lines, "", m.muted(fmt.Sprintf("%d of %d presets", m.presetPicker.selected+1, len(m.presetPicker.items))))
	}
	modal := m.box(strings.Join(lines, "\n"), modalWidth, min(height-2, len(lines)+2), true)
	background := m
	background.presetPicker = nil
	base := m.inactive(background.renderWorkspace(width, height))
	return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(modal).X((width-lipgloss.Width(modal))/2).Y((height-lipgloss.Height(modal))/2).Z(1)).Render()
}

func formatPresetTimestamp(value *time.Time, location *time.Location) string {
	if value == nil {
		return "Never"
	}
	return value.In(location).Format("2006-01-02 15:04")
}
