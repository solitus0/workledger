package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Palette defines every color the TUI applies explicitly.
type Palette struct {
	Focus       string
	Information string
	Progress    string
	Warning     string
	Success     string
	Destructive string
	Proposed    string
	Secondary   string
	Muted       string
	Border      string
	Placeholder string
}

// Theme provides palettes for light and dark terminal backgrounds.
type Theme struct {
	Light Palette
	Dark  Palette
}

// DefaultTheme returns contrast-qualified Catppuccin-derived colors for light
// terminals and Catppuccin Mocha with the Mauve accent for dark terminals.
func DefaultTheme() Theme {
	return Theme{
		Light: Palette{
			Focus:       "#7E1FD4",
			Information: "#006F73",
			Progress:    "#0B63CE",
			Warning:     "#925500",
			Success:     "#287A1D",
			Destructive: "#D20F39",
			Proposed:    "#0969A2",
			Secondary:   "#5C5F6F",
			Muted:       "#686B7A",
			Border:      "#737686",
			Placeholder: "#686B7A",
		},
		Dark: Palette{
			Focus:       "#CBA6F7",
			Information: "#94E2D5",
			Progress:    "#89B4FA",
			Warning:     "#F9E2AF",
			Success:     "#A6E3A1",
			Destructive: "#F38BA8",
			Proposed:    "#74C7EC",
			Secondary:   "#BAC2DE",
			Muted:       "#9399B2",
			Border:      "#6C7086",
			Placeholder: "#9399B2",
		},
	}
}

func (t Theme) palette(dark bool) Palette {
	if dark {
		return t.Dark
	}
	return t.Light
}

func (p Palette) textInputStyles() textinput.Styles {
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Placeholder))
	return textinput.Styles{
		Focused: textinput.StyleState{
			Placeholder: placeholder,
			Suggestion:  placeholder,
			Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Focus)),
		},
		Blurred: textinput.StyleState{
			Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(p.Secondary)),
			Placeholder: placeholder,
			Suggestion:  placeholder,
			Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.Secondary)),
		},
		Cursor: textinput.CursorStyle{
			Color: lipgloss.Color(p.Focus),
			Shape: tea.CursorBlock,
			Blink: true,
		},
	}
}
