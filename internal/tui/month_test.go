package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func testMonthDays(date time.Time) []worklogs.ContextDay {
	start, end := monthBounds(date)
	days := make([]worklogs.ContextDay, 0, end.Day())
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		day := worklogs.ContextDay{Date: date.Format("2006-01-02")}
		if isWeekday(date) {
			day.BookedSeconds = 8 * 3600
			day.Worklogs = []worklogs.LocalWorklog{{
				ID: day.Date, IssueKey: "WL-1234", StartedAtUTC: date.Add(8 * time.Hour),
				DurationSeconds: day.BookedSeconds, Description: "Calendar work", Revision: 1,
			}}
		}
		days = append(days, day)
	}
	return days
}

func testMonthModel(t *testing.T, date time.Time) model {
	t.Helper()
	ws, deps := testWorkspace(t)
	deps.now = func() time.Time { return date.Add(13 * time.Hour) }
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogView = monthWorklogView
	m.worklogLoading = false
	m.contextDays = testMonthDays(date)
	for _, day := range m.contextDays {
		m.contextSummary.BookedSeconds += day.BookedSeconds
		m.contextSummary.WorklogCount += len(day.Worklogs)
	}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 8 * 3600,
	}
	m.selectContextDate(date)
	return m
}

func TestMonthModeLoadsOneLocalCalendarMonthAndRejectsObsoleteModeResult(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.contextDays = testWeekDays()
	state := worklogState(ws)
	state.context.Days = testMonthDays(m.selectedDate)
	state.context.Settings = worklogs.ContextSettings{DailyMinimumQuotaSeconds: 28800}
	next, cmd := m.Update(textKey("m"))
	m = next.(model)
	if m.worklogView != monthWorklogView || cmd == nil || len(m.contextDays) != 0 {
		t.Fatalf("enter Month: view=%v cmd=%v days=%d", m.worklogView, cmd, len(m.contextDays))
	}
	result := cmd().(worklogResultMsg)
	next, _ = m.Update(result)
	m = next.(model)
	if len(state.contextInputs) != 1 || state.contextInputs[0].From != "2026-05-01" || state.contextInputs[0].To != "2026-05-31" {
		t.Fatalf("month context requests = %#v", state.contextInputs)
	}
	if len(m.contextDays) != 31 || len(m.worklogs) != 1 || m.worklogs[0].ID != "2026-05-21" || m.worklogLoading {
		t.Fatalf("month load: days=%d rows=%v loading=%v", len(m.contextDays), m.worklogs, m.worklogLoading)
	}
	next, cmd = m.Update(textKey("w"))
	m = next.(model)
	if m.worklogView != weekWorklogView || cmd == nil || len(m.contextDays) != 0 || !sameDay(m.selectedDate, result.date) {
		t.Fatal("switching to Week did not retain the date and invalidate the month context")
	}
	next, _ = m.Update(result)
	m = next.(model)
	if len(m.contextDays) != 0 {
		t.Fatal("late month result repopulated Week")
	}
	cmd()
	if input := state.contextInputs[1]; input.From != "2026-05-18" || input.To != "2026-05-24" {
		t.Fatalf("Week after Month queried %s..%s", input.From, input.To)
	}
}

func TestMonthSpatialNavigationAndDayDrillDown(t *testing.T) {
	m := testMonthModel(t, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
	for _, step := range []struct{ key, want string }{
		{"h", "2026-05-20"}, {"l", "2026-05-21"}, {"k", "2026-05-14"}, {"j", "2026-05-21"},
	} {
		next, _ := m.Update(textKey(step.key))
		m = next.(model)
		if got := m.selectedDate.Format("2006-01-02"); got != step.want || len(m.worklogs) != 1 || m.worklogs[0].ID != step.want {
			t.Fatalf("%s selected %s, rows=%v; want %s", step.key, got, m.worklogs, step.want)
		}
	}
	if calls := len(worklogState(m.workspace).contextInputs); calls != 0 {
		t.Fatalf("cached date navigation queried context %d times", calls)
	}
	for _, key := range []string{"D", "e"} {
		next, _ := m.Update(textKey(key))
		m = next.(model)
		if m.overlay != noOverlay || m.form != nil {
			t.Fatalf("Month exposed mutation through %s", key)
		}
	}
	next, cmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.worklogView != dayWorklogView || cmd == nil || m.selectedDate.Day() != 21 {
		t.Fatal("Enter did not open the selected date in Day mode")
	}
}

func TestMonthNavigationClampsMonthLengthsAndStopsAtEdges(t *testing.T) {
	for _, tc := range []struct{ date, key, want string }{
		{"2026-01-31", "pgdown", "2026-02-28"},
		{"2024-01-31", "pgdown", "2024-02-29"},
		{"2026-05-31", "pgup", "2026-04-30"},
		{"2026-12-31", "pgdown", "2027-01-31"},
		{"2026-01-01", "pgup", "2025-12-01"},
		{"2026-05-01", "h", "2026-05-01"},
		{"2026-05-01", "k", "2026-05-01"},
		{"2026-05-31", "l", "2026-05-31"},
		{"2026-05-31", "j", "2026-05-31"},
	} {
		t.Run(tc.date+"/"+tc.key, func(t *testing.T) {
			date, _ := time.Parse("2006-01-02", tc.date)
			m := testMonthModel(t, date)
			next, cmd := m.updateMonth(tc.key)
			m = next.(model)
			if got := m.selectedDate.Format("2006-01-02"); got != tc.want {
				t.Fatalf("selected %s; want %s", got, tc.want)
			}
			if strings.HasPrefix(tc.key, "pg") && (cmd == nil || len(m.contextDays) != 0 || len(m.worklogs) != 0) {
				t.Fatal("month paging retained stale data or failed to load the new range")
			}
		})
	}
}

func TestMonthNavigationDuringPendingLoadStartsNewGeneration(t *testing.T) {
	m := testMonthModel(t, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
	state := worklogState(m.workspace)
	state.context.Days = m.contextDays
	cmd := m.startWorklogLoad()
	old := cmd().(worklogResultMsg)
	next, cmd := m.Update(textKey("h"))
	m = next.(model)
	if cmd == nil || m.worklogGen <= old.generation {
		t.Fatal("moving selection left a pending result attached to the old date")
	}
	next, _ = m.Update(old)
	m = next.(model)
	if m.dayContext.Date != "2026-05-20" || !m.worklogLoading {
		t.Fatal("old date result changed the selected day or completed the new load")
	}
	next, _ = m.Update(m.worklogCmd(m.worklogGen, m.selectedDate)())
	if next.(model).worklogLoading {
		t.Fatal("new selected-date load did not complete")
	}
}

func TestMonthGapCountsExcludeTodayFutureAndWeekendsWithoutNettingExtraHours(t *testing.T) {
	m := testMonthModel(t, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
	for index := range m.contextDays {
		day := &m.contextDays[index]
		switch day.Date {
		case "2026-05-04":
			day.BookedSeconds = 16 * 3600
		case "2026-05-05", "2026-05-21", "2026-05-22":
			day.BookedSeconds = 0
		case "2026-05-06":
			day.BookedSeconds = 4 * 3600
		}
	}
	if empty, short := m.monthGaps(); empty != 1 || short != 1 {
		t.Fatalf("month gaps = %d empty, %d short; want 1 of each", empty, short)
	}
	calendar := strings.Join(m.monthCalendarRows(65, false), "\n")
	for _, expected := range []string{" 5?", " 6?", "[21]*", "22·"} {
		if !strings.Contains(calendar, expected) {
			t.Fatalf("calendar missing %q:\n%s", expected, calendar)
		}
	}
	if strings.Contains(calendar, "21?") || strings.Contains(calendar, "22?") || strings.Contains(calendar, "17?") {
		t.Fatalf("calendar flagged a non-past weekday:\n%s", calendar)
	}
}

func TestMonthCalendarFitsMinimumViewportWithActivityAndColor(t *testing.T) {
	for _, date := range []time.Time{
		time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC), // four weeks
		time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), // five weeks
		time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), // six weeks
	} {
		for _, activity := range []bool{false, true} {
			for _, noColor := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/activity=%v/noColor=%v", date.Format("2006-01"), activity, noColor), func(t *testing.T) {
					m := testMonthModel(t, date)
					m.activityOpen, m.deps.noColor, m.colorReady = activity, noColor, true
					m.dayContext.Collisions = []worklogs.ContextCollision{{}}
					for index := range m.contextDays {
						if m.contextDays[index].Date == m.dayContext.Date {
							m.contextDays[index] = m.dayContext
						}
					}
					rendered := m.render()
					if w, h := lipgloss.Width(rendered), lipgloss.Height(rendered); w != minimumWidth || h != minimumHeight {
						t.Fatalf("render = %dx%d; want 100x28:\n%s", w, h, rendered)
					}
					plain := stripANSI(rendered)
					for _, expected := range []string{"Local booked", "Day  Week  [Month]", "Mon", "Sun", "Past weekdays:", "? below quota", "! overlap", date.Format("2006-01-02"), "WL-1234", "PgUp/Dn Month", "g Activity"} {
						if activity && expected == "g Activity" {
							expected = "g Close activity"
						}
						if !strings.Contains(plain, expected) {
							t.Fatalf("render missing %q:\n%s", expected, plain)
						}
					}
					if noColor && strings.Contains(rendered, "\x1b[") {
						t.Fatal("NO_COLOR calendar includes ANSI styling")
					}
				})
			}
		}
	}
}

func TestMonthMouseSelectsCalendarCellsAndIgnoresPadding(t *testing.T) {
	for _, activity := range []bool{false, true} {
		m := testMonthModel(t, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
		m.activityOpen = activity
		rowHeight := 2
		if activity {
			rowHeight = 1
		}
		// May 20 is Wednesday in the fourth displayed week.
		next, _ := m.Update(mouseClick(33+2*9, 5+3*rowHeight, tea.MouseLeft))
		m = next.(model)
		if m.selectedDate.Day() != 20 || m.worklogs[0].ID != "2026-05-20" {
			t.Fatalf("calendar click selected %s", m.selectedDate)
		}
		next, _ = m.Update(mouseClick(33, 5, tea.MouseLeft))
		if next.(model).selectedDate.Day() != 20 {
			t.Fatal("adjacent-month padding cell was selectable")
		}
	}
}

func TestMonthAddFormFitsAndPreservesSelectedDate(t *testing.T) {
	m := testMonthModel(t, time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC))
	m.activityOpen = true
	next, _ := m.Update(textKey("a"))
	m = next.(model)
	if m.form == nil || m.worklogView != monthWorklogView || m.selectedDate.Day() != 27 {
		t.Fatal("Month Add did not open a draft for the selected date")
	}
	rendered := m.render()
	if lipgloss.Width(rendered) != minimumWidth || lipgloss.Height(rendered) != minimumHeight {
		t.Fatalf("month Add does not fit:\n%s", rendered)
	}
	for _, expected := range []string{"Add worklog", "Issue", "Duration", "Description", "Placement", "Calculated", "Ctrl+S"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("month Add missing %q:\n%s", expected, rendered)
		}
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	if next.(model).worklogView != monthWorklogView || next.(model).form != nil {
		t.Fatal("cancel did not return to Month")
	}
}
