package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestDayAvailabilityFollowsVisibleRows(t *testing.T) {
	for _, size := range []struct{ width, height int }{{100, 28}, {140, 60}, {260, 60}} {
		for _, count := range []int{0, 1, 40} {
			t.Run(fmt.Sprintf("%dx%d/%d", size.width, size.height, count), func(t *testing.T) {
				ws, deps := testWorkspace(t)
				m := newModel(t.Context(), deps, ws, nil)
				m.width, m.height = size.width, size.height
				m.worklogLoading = false
				m.dayContext.Date = m.selectedDate.Format("2006-01-02")
				for i := range count {
					m.worklogs = append(m.worklogs, worklogs.LocalWorklog{ID: fmt.Sprint(i), IssueKey: "APP-1", StartedAtUTC: m.selectedDate.Add(8*time.Hour + time.Duration(i)*15*time.Minute), DurationSeconds: 900, Description: fmt.Sprintf("Entry %02d", i)})
				}
				m.worklogSelected = max(0, count-1)
				m.dayContext.Worklogs = m.worklogs
				m.dayContext.BookedSeconds = count * 900
				view := stripANSI(m.render())
				lastRow, availability := -1, -1
				for row, line := range strings.Split(view, "\n") {
					if strings.Contains(line, "[ ]") || strings.Contains(line, "No worklogs on this day") {
						lastRow = row
					}
					if strings.Contains(line, "0m free · No available windows") {
						availability = row
					}
				}
				if lastRow < 0 || availability != lastRow+2 {
					t.Fatalf("availability detached from rows: last=%d availability=%d\n%s", lastRow, availability, view)
				}
				if count > 0 && !strings.Contains(view, fmt.Sprintf("Entry %02d", count-1)) {
					t.Fatal("last selected entry is outside the visible table")
				}
				assertRedesignBounds(t, m, "Day availability")
			})
		}
	}
}

func TestCompactAvailabilityRetainsAWindowAndLunchExclusion(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.dayContext.Date = m.selectedDate.Format("2006-01-02")
	m.contextSettings.Lunch = &worklogs.ContextLunch{Start: "12:00", End: "12:45"}
	for _, hour := range []int{8, 14, 16} {
		start := m.selectedDate.Add(time.Duration(hour) * time.Hour)
		m.dayContext.FreeSlots = append(m.dayContext.FreeSlots, worklogs.ContextFreeSlot{Start: start, End: start.Add(30 * time.Minute), DurationSeconds: 1800})
	}
	view := stripANSI(m.renderAvailableWindows(50, 4))
	for _, cue := range []string{"1h30m free", "08:00–08:30", "2 more windows", "Lunch 12:00–12:45 excluded"} {
		if !strings.Contains(view, cue) {
			t.Fatalf("compact availability hides %q:\n%s", cue, view)
		}
	}
	if lipgloss.Height(view) != 4 || lipgloss.Width(view) > 50 {
		t.Fatalf("compact availability exceeds its bounds:\n%s", view)
	}
}

func TestWorklogInspectorPrioritizesSelectionAndKeepsOneEffortSummary(t *testing.T) {
	for _, size := range []struct{ width, height int }{{48, 24}, {26, 8}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(t.Context(), deps, ws, nil)
			m.worklogs = testRows()
			m.worklogs[0].Description = strings.Repeat("Audit API performance and query counts. ", 8)
			key := m.worklogs[0].IssueKey
			estimate := int64(16 * 3600)
			m.issueMetadata = map[string]worklogs.IssueMetadata{key: {Title: "Upgrade PHP and Symfony", MaxEstimateSeconds: &estimate}}
			m.issueTotalIssue, m.issueTotal, m.issueTotalLoading = key, 18*3600, false
			view := stripANSI(m.renderWorklogInspector(size.width, size.height))
			clock, description, effort := strings.Index(view, "09:00–11:00"), strings.Index(view, "Audit API"), strings.Index(view, "Issue effort · all dates")
			if clock < 0 || description < clock || effort < description {
				t.Fatalf("selected worklog does not precede effort:\n%s", view)
			}
			for _, value := range []string{"18h", "16h", "112.5%", "2h over estimate"} {
				if strings.Count(view, value) != 1 {
					t.Fatalf("expected one visible %q:\n%s", value, view)
				}
			}
			if lipgloss.Width(view) > size.width || lipgloss.Height(view) > size.height {
				t.Fatalf("inspector overflows %dx%d:\n%s", size.width, size.height, view)
			}
		})
	}
}
