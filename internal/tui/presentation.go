package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Selectors keep an explicit marker when terminal colors are unavailable.
func (m model) selector(labels []string, selected int) string {
	parts := make([]string, len(labels))
	for i, label := range labels {
		if i == selected {
			label = "[" + label + "]"
			if !m.deps.noColor && m.colorReady {
				label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.colors.Focus)).Reverse(true).Render(label)
			}
		} else {
			label = m.secondary(label)
		}
		parts[i] = label
	}
	return strings.Join(parts, "  ")
}

func displayState(value string) string {
	switch value {
	case "not_attempted":
		return "Not applied"
	case "partially_applied":
		return "Partly applied"
	case "remote_diff":
		return "Different entries"
	case "check_failed":
		return "Inspection failed"
	case "not_checked":
		return "Not inspected"
	case "not_applicable":
		return "Not applicable"
	case "succeeded", "applied":
		return "Completed"
	}
	value = strings.ReplaceAll(value, "_", " ")
	if value == "" {
		return "—"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func countLabel(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

// Inactive content retains context without competing with the live window.
func (m model) inactive(content string) string { return m.muted(ansi.Strip(content)) }

func (m model) fieldError(message string, width int) string {
	lines := wrapIssueDescription("! "+message, max(1, width))
	if len(lines) > 3 {
		lines = lines[:3]
		lines[2] = truncate(lines[2], max(1, width-1)) + "…"
	}
	return m.paint(strings.Join(lines, "\n"), m.colors.Destructive)
}

func (m model) inputView(input textinput.Model, focused bool) string {
	if focused {
		return input.View()
	}
	if input.Value() == "" {
		return m.paint(truncate(input.Placeholder, input.Width()), m.colors.Placeholder)
	}
	return m.secondary(truncate(input.Value(), input.Width()))
}
