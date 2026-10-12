package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestHourlyTimelineAcrossBrowsingAndPlacementWindows(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {160, 36}, {260, 36}} {
		for _, theme := range []string{"dark", "light", "no color"} {
			for _, window := range []string{"Day", "Week", "Month", "Manual", "Fit", "Fill", "Edit", "Move"} {
				t.Run(fmt.Sprintf("%s/%s/%dx%d", window, theme, size[0], size[1]), func(t *testing.T) {
					ws, deps := testWorkspace(t)
					deps.noColor = theme == "no color"
					m := newModel(t.Context(), deps, ws, nil)
					m.width, m.height = size[0], size[1]
					m.worklogLoading = false
					if theme == "light" {
						m.colors = deps.theme.Light
					}
					m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
					record := worklogs.LocalWorklog{ID: "ot", IssueKey: "WL-101", StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 10 * 3600, Revision: 1}
					m.worklogs = []worklogs.LocalWorklog{record}
					m.dayContext = worklogs.ContextDay{Date: m.selectedDate.Format("2006-01-02"), BookedSeconds: 10 * 3600, Worklogs: m.worklogs}
					m.contextDays = []worklogs.ContextDay{m.dayContext}
					switch window {
					case "Week":
						m.worklogView = weekWorklogView
					case "Month":
						m.worklogView = monthWorklogView
					case "Manual", "Fit", "Fill":
						m.openAddForm()
						m.form.placement = map[string]placementMode{"Manual": manualPlacement, "Fit": fitPlacement, "Fill": fillPlacement}[window]
						m.form.inputs[issueInput].SetValue(record.IssueKey)
						m.form.inputs[startInput].SetValue("07:00")
						m.form.inputs[durationInput].SetValue("10h")
						m.form.previewRecords = []worklogs.LocalWorklog{record}
						m.worklogs, m.dayContext.Worklogs = nil, nil
					case "Edit":
						m.openEditForm(record)
					case "Move":
						m.move = &moveState{preview: worklogs.MovePreview{Source: record, Settings: m.contextSettings}, destination: record.StartedAtUTC}
					}
					raw := m.render()
					if deps.noColor && strings.Contains(raw, "\x1b") {
						t.Fatal("NO_COLOR window emitted ANSI styling")
					}
					rendered := stripANSI(raw)
					lines := strings.Split(rendered, "\n")
					found := false
					for row, line := range lines {
						start := strings.IndexRune(line, '▁')
						if start < 0 {
							continue
						}
						column := len([]rune(line[:start]))
						count := 0
						for _, symbol := range []rune(line)[column:] {
							if symbol != '▁' {
								break
							}
							count++
						}
						band := string([]rune(lines[row+1])[column : column+count])
						if got := strings.Count(band, "▏"); got != 9 {
							t.Fatalf("%s does not use the same hourly OT grid: %d dividers in %q", window, got, band)
						}
						found = true
						break
					}
					if !found && !(window == "Month" && m.height == minimumHeight) {
						t.Fatalf("%s lost its timeline at %dx%d", window, m.width, m.height)
					}
					if len(lines) > m.height {
						t.Fatal("window exceeded its viewport height")
					}
					for _, line := range lines {
						if lipgloss.Width(line) > m.width {
							t.Fatalf("window exceeded viewport width: %q", line)
						}
					}
				})
			}
		}
	}
}

func TestTimelineResolutionFollowsPersistedAndProposedOvertime(t *testing.T) {
	for _, tc := range []struct {
		name        string
		start       time.Duration
		duration    time.Duration
		preview     bool
		wantRules   int
		first, last string
	}{
		{"within bounds exceeding quota", 8 * time.Hour, 9 * time.Hour, false, 8, "08:00", "17:00"},
		{"persisted early overtime", 7 * time.Hour, 10 * time.Hour, false, 9, "07:00", "17:00"},
		{"persisted late overtime", 8 * time.Hour, 10*time.Hour + 15*time.Minute, false, 10, "08:00", "18:15"},
		{"proposed early overtime", 7 * time.Hour, 10 * time.Hour, true, 9, "07:00", "17:00"},
		{"proposed late overtime", 8 * time.Hour, 10*time.Hour + 15*time.Minute, true, 10, "08:00", "18:15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(t.Context(), deps, ws, nil)
			m.width, m.height = 260, 36
			m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
			records := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(tc.start), DurationSeconds: int(tc.duration / time.Second)}}
			var preview []worklogs.LocalWorklog
			if tc.preview {
				preview = records
			} else {
				m.worklogs = records
			}
			ruler, upper, band, lower := timelineParts(t, m.renderDayTimelineWidth(preview, "", 144))
			if got := strings.Count(band, "▏"); got != tc.wantRules {
				t.Fatalf("internal divisions = %d, want %d: %q", got, tc.wantRules, band)
			}
			if !strings.HasPrefix(ruler, tc.first) || !strings.HasSuffix(ruler, tc.last) || len([]rune(band)) != 140 {
				t.Fatalf("adaptive grid changed bounds or full width: ruler=%q band=%q", ruler, band)
			}
			if upper != strings.Repeat("▁", 140) || lower != strings.Repeat("▔", 140) {
				t.Fatal("adaptive grid changed the common band height")
			}
			if tc.preview {
				_, _, recovered, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 144))
				if strings.Count(recovered, "▏") != 36 {
					t.Fatalf("removing the OT proposal did not restore the quarter-hour grid: %q", recovered)
				}
			}
		})
	}
}

func TestTimelineHourlyCellsUseLocalClockAndPreservePartialBounds(t *testing.T) {
	for _, location := range []*time.Location{time.UTC, time.FixedZone("UTC+05:10", (5*60+10)*60)} {
		start := time.Date(2026, 10, 9, 7, 52, 0, 0, location)
		columns := timelineCellColumns(newTimelineScale(start, start.Add(75*time.Minute), 75), time.Hour)
		if len(columns) != 2 || columns[8] != 60 || columns[68] != 7 {
			t.Fatalf("%s: hourly cells rounded partial times or used UTC boundaries: %v", location, columns)
		}
	}
}

func TestTimelineHourlyGridPreservesSubhourSelectionLunchAndProposal(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 600, 36
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28 * 60, Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"}}
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "early", StartedAtUTC: m.selectedDate.Add(7*time.Hour + 52*time.Minute), DurationSeconds: 8 * 60},
		{ID: "selected", StartedAtUTC: m.selectedDate.Add(9*time.Hour + 10*time.Minute), DurationSeconds: 20 * 60},
	}
	preview := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(17*time.Hour + 7*time.Minute), DurationSeconds: 15 * 60}}
	// One minute per terminal column, including both partial axis tails.
	ruler, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(preview, "selected", 574))
	cells := []rune(band)
	if !strings.HasPrefix(ruler, "07:52") || !strings.HasSuffix(ruler, "17:22") || len(cells) != 570 {
		t.Fatalf("actual partial bounds changed: %q", ruler)
	}
	if string(cells[78:98]) != strings.Repeat("▓", 20) || cells[77] == '▓' || cells[98] == '▓' {
		t.Fatalf("20-minute selection snapped to an hourly cell: %q", band)
	}
	if string(cells[248:293]) != "▏"+strings.Repeat("╱", 44) || cells[293] != '▏' {
		t.Fatalf("45-minute lunch lost its full-height hatch and border: %q", string(cells[248:294]))
	}
	if string(cells[555:570]) != strings.Repeat("▒", 15) || cells[554] == '▒' {
		t.Fatalf("15-minute OT proposal snapped to an hourly cell: %q", string(cells[550:]))
	}
	if m.worklogs[1].DurationSeconds != 20*60 || preview[0].DurationSeconds != 15*60 {
		t.Fatal("adaptive rendering mutated the stored or proposed duration")
	}
}

func TestLateManualPreviewDoesNotClaimToStartBeforeWorkdayEnd(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.openAddForm()
	m.form.placement = manualPlacement
	for _, tc := range []struct {
		start time.Duration
		want  bool
	}{
		{16*time.Hour + 45*time.Minute, true},
		{17 * time.Hour, false},
		{17*time.Hour + 7*time.Minute, false},
	} {
		m.form.previewRecords = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(tc.start), DurationSeconds: 1800}}
		if got := strings.Contains(m.renderPlacementInspector(60, 20), "Starts before 17:00"); got != tc.want {
			t.Fatalf("manual start %s has misleading workday-end feedback: %s", tc.start, m.renderPlacementInspector(60, 20))
		}
	}
}
