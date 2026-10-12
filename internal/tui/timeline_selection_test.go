package tui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestTimelineRangeBracketGeometry(t *testing.T) {
	for _, theme := range []string{"dark", "light", "no color", "pending color"} {
		for _, tc := range []struct {
			name     string
			start    time.Duration
			duration time.Duration
			first    int
			marker   string
		}{
			{"left edge", 7 * time.Hour, 2 * time.Hour, 0, "┌────┐"},
			{"right edge", 15 * time.Hour, 2 * time.Hour, 24, "┌────┐"},
			{"crossing quota", 14*time.Hour + 30*time.Minute, 2 * time.Hour, 22, "┌────┐"},
			{"two columns", 15 * time.Hour, 40 * time.Minute, 24, "┌┐"},
			{"one sampled column", 15 * time.Hour, 20 * time.Minute, 24, "▼"},
			{"between samples", 15*time.Hour + 31*time.Minute, time.Minute, 25, "▼"},
			{"carry in", -time.Hour, 8*time.Hour + 20*time.Minute, 0, "┌───────────┐"},
			{"carry out", 23 * time.Hour, 2 * time.Hour, 28, "┌┐"},
			{"zero duration", 15 * time.Hour, 0, 0, ""},
			{"outside day", 25 * time.Hour, time.Hour, 0, ""},
		} {
			t.Run(theme+"/"+tc.name, func(t *testing.T) {
				ws, deps := testWorkspace(t)
				deps.noColor = theme == "no color"
				deps.waitForBackground = theme == "pending color"
				m := newModel(t.Context(), deps, ws, nil)
				m.width, m.height = 100, 28
				if theme == "light" {
					m.colors = deps.theme.Light
				}
				m.contextSettings = worklogs.ContextSettings{DayStart: "07:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 3600}
				m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(tc.start), DurationSeconds: int(tc.duration / time.Second)}}
				selected := m.renderDayTimelineWidth(nil, "selected", 30)
				unselected := m.renderDayTimelineWidth(nil, "", 30)
				ruler, upper, band, lower := timelineParts(t, selected)
				beforeRuler, beforeUpper, beforeBand, beforeLower := timelineParts(t, unselected)
				if ruler != beforeRuler || upper != beforeUpper || band != beforeBand || lower != beforeLower || len(strings.Split(selected, "\n")) != len(strings.Split(unselected, "\n")) {
					t.Fatal("selection changed ruler, occupancy, bottom edge, or height")
				}
				want := strings.Repeat(" ", tc.first) + tc.marker + strings.Repeat(" ", len([]rune(band))-tc.first-len([]rune(tc.marker)))
				if annotation := timelineAnnotation(t, selected); annotation != want {
					t.Fatalf("annotation=%q, want %q", annotation, want)
				}
				if (theme == "no color" || theme == "pending color") && strings.Contains(selected, "\x1b") {
					t.Fatal("monochrome emitted ANSI styling")
				}
				legend := m.renderTimelineLegendWidth(nil, "selected", 30)
				if strings.Contains(stripANSI(legend), "selected") != (tc.marker != "") {
					t.Fatalf("legend claimed an invisible selection: %q", legend)
				}
			})
		}
	}
}

// Uses real SQLite and domain services; only unrelated remote/status services
// remain fake. Keyboard input, asynchronous loads, and rendering use the model.
func selectionServiceModel(t *testing.T, width, height int, theme string) model {
	t.Helper()
	ws, deps := testWorkspace(t)
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := worklogs.NewService(store)
	ws.cfg.DayStart, ws.cfg.DayEnd = "07:00", "17:00"
	ws.cfg.DailyLunch = "12:00-13:00"
	ws.cfg.MinimumDurationSeconds = 60
	ws.cfg.DailyMinimumQuotaSeconds = 8 * 3600
	ws.worklogs, ws.mutations = service, service
	ws.activities = &fakeActivities{}
	for i, slot := range [][2]string{{"07:00", "8h"}, {"15:00", "2h"}, {"17:01", "1m"}} {
		_, err = service.Add(t.Context(), ws.cfg, worklogs.AddInput{IssueKey: fmt.Sprintf("WL-%d", i+1), Started: "2026-05-21T" + slot[0], Duration: slot[1], Description: "Selection parity"})
		if err != nil {
			t.Fatal(err)
		}
	}
	deps.noColor = theme == "no color"
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = width, height
	if theme == "light" {
		m.colors = deps.theme.Light
	}
	cmd := m.startWorklogLoad()
	msg := cmd().(worklogResultMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.items) != 3 {
		t.Fatalf("loaded %d records on %s (settings=%+v)", len(msg.items), msg.date, msg.settings)
	}
	next, _ := m.Update(msg)
	return next.(model)
}

func TestTimelineSelectionKeyboardServiceParity(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {160, 36}, {260, 36}} {
		for _, theme := range []string{"dark", "light", "no color"} {
			t.Run(fmt.Sprintf("%s/%dx%d", theme, size[0], size[1]), func(t *testing.T) {
				m := selectionServiceModel(t, size[0], size[1], theme)
				records := append([]worklogs.LocalWorklog(nil), m.worklogs...)
				step := func(key string) {
					t.Helper()
					next, cmd := m.Update(actionKey(key))
					m = next.(model)
					// Calendar and Move load commands are direct, bounded service requests.
					if cmd != nil && (m.worklogLoading || m.move != nil && m.move.loading) {
						next, _ = m.Update(cmd())
						m = next.(model)
					}
				}
				check := func(caret bool) {
					t.Helper()
					timeline := m.renderDayTimeline(nil, m.selectedWorklogID())
					_, upper, band, lower := timelineParts(t, timeline)
					_, beforeUpper, beforeBand, beforeLower := timelineParts(t, m.renderDayTimeline(nil, ""))
					if upper != beforeUpper || band != beforeBand || lower != beforeLower {
						t.Fatal("keyboard selection replaced underlying state")
					}
					if caret != strings.Contains(timelineAnnotation(t, timeline), "▼") || !caret && (!strings.Contains(timelineAnnotation(t, timeline), "┌") || !strings.Contains(timelineAnnotation(t, timeline), "┐")) {
						t.Fatalf("wrong selection shape: %q", upper)
					}
					if !strings.Contains(stripANSI(m.render()), strings.TrimSpace(timelineAnnotation(t, timeline))) {
						t.Fatal("workspace did not render the selected interval marker")
					}
					assertRedesignBounds(t, m, "range bracket parity")
					if theme == "no color" && strings.Contains(m.render(), "\x1b") {
						t.Fatal("monochrome window contains ANSI")
					}
				}
				check(false)
				step("j")
				check(false)
				step("j")
				check(true)
				id := m.selectedWorklogID()
				step("enter")
				step("esc")
				if m.selectedWorklogID() != id {
					t.Fatal("details changed selected row")
				}
				check(true)
				for _, key := range []string{"e", "M", "a"} {
					step(key)
					if strings.Contains(stripANSI(m.render()), "┌─┐/▼ selected") {
						t.Fatalf("%s preview leaked persisted selection", key)
					}
					step("esc")
					if m.selectedWorklogID() != id {
						t.Fatalf("cancel %s lost selection", key)
					}
					check(true)
				}
				for _, key := range []string{"w", "m"} {
					step(key)
					if strings.Contains(stripANSI(m.render()), "┌─┐/▼ selected") {
						t.Fatalf("%s date preview leaked worklog selection", key)
					}
					assertRedesignBounds(t, m, "date preview parity")
					step("d")
					check(m.selectedWorklog().DurationSeconds == 60)
				}
				step("/")
				m.worklogFilter.input.SetValue("no matching worklog")
				m.restoreWorklogSelection("")
				step("enter")
				if strings.Contains(stripANSI(m.render()), "┌─┐/▼ selected") {
					t.Fatal("empty filter retained selection")
				}
				step("esc")
				check(m.selectedWorklog().DurationSeconds == 60)
				cmd := m.startWorklogLoad()
				next, _ := m.Update(cmd())
				m = next.(model)
				if !reflect.DeepEqual(records, m.worklogs) {
					t.Fatal("selection/navigation changed persisted worklogs")
				}
			})
		}
	}
}

func TestTimelineCanvasSelectionNeverChangesBarCells(t *testing.T) {
	for _, theme := range []string{"dark", "light", "no color", "pending color"} {
		for _, width := range []int{36, 79, 144} {
			for _, duration := range []int{1, 20 * 60, 4 * 3600} {
				t.Run(fmt.Sprintf("%s/%d/%d", theme, width, duration), func(t *testing.T) {
					ws, deps := testWorkspace(t)
					deps.noColor = theme == "no color"
					deps.waitForBackground = theme == "pending color"
					m := newModel(t.Context(), deps, ws, nil)
					m.width, m.height = 100, 28
					if theme == "light" {
						m.colors = deps.theme.Light
					}
					m.contextSettings = worklogs.ContextSettings{DayStart: "07:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 3600, Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"}}
					m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(10 * time.Hour), DurationSeconds: duration}}
					preview := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 1800}}
					selectedPlan, _ := m.buildTimelinePlan(preview, "selected", width)
					basePlan, _ := m.buildTimelinePlan(preview, "", width)
					selected, base := m.timelineCanvas(selectedPlan), m.timelineCanvas(basePlan)
					if selected.Width() != base.Width() || selected.Height() != base.Height() {
						t.Fatal("selection changed canvas bounds")
					}
					for y := timelineBarRow; y < timelineBarRow+timelineBarHeight; y++ {
						for x := 0; x < selected.Width(); x++ {
							if !selected.CellAt(x, y).Equal(base.CellAt(x, y)) {
								t.Fatalf("selection changed bar cell (%d,%d)", x, y)
							}
						}
					}
					if !reflect.DeepEqual(selectedPlan.cells, basePlan.cells) || !reflect.DeepEqual(selectedPlan.divisions, basePlan.divisions) {
						t.Fatal("selection changed render plan occupancy or grid")
					}
				})
			}
		}
	}
}
