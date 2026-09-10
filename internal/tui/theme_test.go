package tui

import (
	"context"
	"image/color"
	"math"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDefaultThemeMeetsSemanticContrastTargets(t *testing.T) {
	theme := DefaultTheme()
	for name, testCase := range map[string]struct {
		palette    Palette
		background string
	}{
		"light": {palette: theme.Light, background: "#EFF1F5"},
		"dark":  {palette: theme.Dark, background: "#1E1E2E"},
	} {
		for role, value := range map[string]string{
			"focus": testCase.palette.Focus, "information": testCase.palette.Information,
			"progress": testCase.palette.Progress, "warning": testCase.palette.Warning,
			"success": testCase.palette.Success, "destructive": testCase.palette.Destructive,
			"proposed": testCase.palette.Proposed, "secondary": testCase.palette.Secondary,
			"muted": testCase.palette.Muted, "placeholder": testCase.palette.Placeholder,
		} {
			if ratio := contrastRatio(value, testCase.background); ratio < 4.5 {
				t.Errorf("%s %s contrast = %.2f:1, want at least 4.5:1", name, role, ratio)
			}
		}
		if ratio := contrastRatio(testCase.palette.Border, testCase.background); ratio < 3 {
			t.Errorf("%s border contrast = %.2f:1, want at least 3:1", name, ratio)
		}
	}
}

func contrastRatio(foreground, background string) float64 {
	first, second := relativeLuminance(foreground), relativeLuminance(background)
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

func relativeLuminance(value string) float64 {
	rgb, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		panic(err)
	}
	channel := func(component uint64) float64 {
		value := float64(component) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(rgb>>16) + 0.7152*channel((rgb>>8)&0xff) + 0.0722*channel(rgb&0xff)
}

func TestDefaultThemeUsesContrastQualifiedCatppuccinRoles(t *testing.T) {
	want := Theme{
		Light: Palette{
			Focus: "#7E1FD4", Information: "#006F73", Progress: "#0B63CE", Warning: "#925500", Success: "#287A1D",
			Destructive: "#D20F39", Proposed: "#0969A2", Secondary: "#5C5F6F", Muted: "#686B7A", Border: "#737686", Placeholder: "#686B7A",
		},
		Dark: Palette{
			Focus: "#CBA6F7", Information: "#94E2D5", Progress: "#89B4FA", Warning: "#F9E2AF", Success: "#A6E3A1",
			Destructive: "#F38BA8", Proposed: "#74C7EC", Secondary: "#BAC2DE", Muted: "#9399B2", Border: "#6C7086", Placeholder: "#9399B2",
		},
	}
	if got := DefaultTheme(); got != want {
		t.Fatalf("default theme = %+v, want %+v", got, want)
	}
}

func TestModelUsesConfiguredAdaptiveTheme(t *testing.T) {
	ws, deps := testWorkspace(t)
	custom := Theme{
		Light: Palette{Focus: "light-focus", Information: "light-information", Progress: "light-progress", Warning: "light-warning", Success: "light-success", Destructive: "light-destructive", Proposed: "light-proposed", Secondary: "light-secondary", Muted: "light-muted", Border: "light-border", Placeholder: "light-placeholder"},
		Dark:  Palette{Focus: "dark-focus", Information: "dark-information", Progress: "dark-progress", Warning: "dark-warning", Success: "dark-success", Destructive: "dark-destructive", Proposed: "dark-proposed", Secondary: "dark-secondary", Muted: "dark-muted", Border: "dark-border", Placeholder: "dark-placeholder"},
	}
	deps.theme = custom
	m := newModel(context.Background(), deps, ws, nil)
	if m.colors != custom.Dark {
		t.Fatalf("initial palette = %+v, want dark palette", m.colors)
	}
	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	if got := next.(model).colors; got != custom.Light {
		t.Fatalf("light-background palette = %+v, want %+v", got, custom.Light)
	}
}

func TestRuntimeStartsUnstyledUntilTerminalBackgroundIsKnown(t *testing.T) {
	deps := defaultDependencies(DefaultTheme())
	deps.noColor = false
	m := newModel(context.Background(), deps, nil, nil)
	if m.colorReady || containsANSI(m.paint("pending", m.colors.Focus)) {
		t.Fatal("runtime emitted theme colors before terminal background was known")
	}
	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.Black})
	m = next.(model)
	if !m.colorReady || !containsANSI(m.paint("ready", m.colors.Focus)) {
		t.Fatal("runtime did not enable theme colors after terminal background response")
	}
}

func containsANSI(value string) bool {
	for index := 0; index+1 < len(value); index++ {
		if value[index] == 0x1b && value[index+1] == '[' {
			return true
		}
	}
	return false
}
