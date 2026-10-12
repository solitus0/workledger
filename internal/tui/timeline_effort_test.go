package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestTimelineOvertimeConsumesLoggedEffortInChronologicalOrder(t *testing.T) {
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	record := func(start, duration time.Duration) worklogs.LocalWorklog {
		return worklogs.LocalWorklog{StartedAtUTC: day.Add(start), DurationSeconds: int(duration / time.Second)}
	}
	for _, tc := range []struct {
		name     string
		records  []worklogs.LocalWorklog
		quota    time.Duration
		want     time.Duration
		overtime bool
	}{
		{"early start and worked lunch", []worklogs.LocalWorklog{record(7*time.Hour, 10*time.Hour)}, 8 * time.Hour, 15 * time.Hour, true},
		{"worked lunch within workday", []worklogs.LocalWorklog{record(8*time.Hour, 9*time.Hour)}, 8 * time.Hour, 16 * time.Hour, true},
		{"early start exactly at target", []worklogs.LocalWorklog{record(7*time.Hour, 8*time.Hour)}, 8 * time.Hour, 0, false},
		{"late work below target", []worklogs.LocalWorklog{record(17*time.Hour, 2*time.Hour)}, 8 * time.Hour, 0, false},
		{"lunch and gaps excluded", []worklogs.LocalWorklog{record(13*time.Hour, 5*time.Hour), record(8*time.Hour, 4*time.Hour)}, 8 * time.Hour, 17 * time.Hour, true},
		{"partial later worklog", []worklogs.LocalWorklog{record(8*time.Hour, 4*time.Hour), record(13*time.Hour, 3*time.Hour+30*time.Minute), record(18*time.Hour, 2*time.Hour)}, 8 * time.Hour, 18*time.Hour + 30*time.Minute, true},
		{"custom target", []worklogs.LocalWorklog{record(7*time.Hour, 10*time.Hour)}, 6 * time.Hour, 13 * time.Hour, true},
		{"concurrent logs agree with summed balance", []worklogs.LocalWorklog{record(8*time.Hour, 2*time.Hour), record(8*time.Hour, 8*time.Hour)}, 8 * time.Hour, 14 * time.Hour, true},
		{"carry in contributes only selected day", []worklogs.LocalWorklog{record(-2*time.Hour, 9*time.Hour), record(8*time.Hour, 2*time.Hour)}, 8 * time.Hour, 9 * time.Hour, true},
		{"carry out excludes next day", []worklogs.LocalWorklog{record(8*time.Hour, 6*time.Hour), record(22*time.Hour, 4*time.Hour)}, 8 * time.Hour, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, overtime := timelineOvertimeStart(tc.records, day, tc.quota)
			if overtime != tc.overtime || overtime && !start.Equal(day.Add(tc.want)) {
				t.Fatalf("OT begins %s (%t), want %s (%t)", start, overtime, day.Add(tc.want), tc.overtime)
			}
		})
	}
}

func TestTimelineEarlyAndLateWorkBelowQuotaRemainsRegular(t *testing.T) {
	for _, start := range []int{7, 18} {
		ws, deps := testWorkspace(t)
		m := newModel(t.Context(), deps, ws, nil)
		m.width, m.height = 160, 36
		m.worklogs = []worklogs.LocalWorklog{{ID: "short-day", StartedAtUTC: m.selectedDate.Add(time.Duration(start) * time.Hour), DurationSeconds: 3600}}
		_, visibility := m.renderTimelineState(nil, "short-day", 140)
		_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "short-day", 140))
		if visibility.overtime || strings.Contains(band, "▒") || strings.Contains(m.renderTimelineLegend(nil, "short-day"), "overtime") {
			t.Fatalf("%02d:00 below-quota work is classified as OT: %q", start, band)
		}
		if !strings.Contains(timelineAnnotation(t, m.renderDayTimelineWidth(nil, "short-day", 140)), "┌") {
			t.Fatalf("regular early/late selection lost its highlight: %q", band)
		}
	}
}

func TestLunchPreviewReclassifiesLaterBookingAndCancelRestoresQuota(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 600, 36
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"}}
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "morning", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 4 * 3600},
		{ID: "afternoon", StartedAtUTC: m.selectedDate.Add(13 * time.Hour), DurationSeconds: 4 * 3600},
	}
	before := m.renderDayTimelineWidth(nil, "afternoon", 544)
	m.openAddForm()
	m.form.noLunch = true
	m.form.previewRecords = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(12 * time.Hour), DurationSeconds: 3600}}
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(m.form.previewRecords, "", 544))
	cells := []rune(band)
	if !strings.Contains(string(cells[240:300]), "░") || strings.Contains(string(cells[:480]), "▒") || !strings.Contains(string(cells[480:540]), "▒") {
		t.Fatalf("working-lunch proposal did not put OT on the later 16:00–17:00 booking: %q", band)
	}
	if !strings.Contains(m.renderProjectedDayBalance(), "9h 00m total") || !strings.Contains(m.renderProjectedDayBalance(), "1h 00m over") {
		t.Fatalf("projected balance disagrees with timeline OT: %q", m.renderProjectedDayBalance())
	}
	next, _ := m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.form != nil || m.renderDayTimelineWidth(nil, "afternoon", 544) != before || m.worklogs[1].DurationSeconds != 4*3600 {
		t.Fatal("cancel failed to restore exact-target quarter grid, lunch and unchanged bookings")
	}
}

func TestTimelineIncludesCarryInAndClipsProjectedCarryOut(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 160, 36
	today := worklogs.LocalWorklog{ID: "today", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 2 * 3600}
	carry := worklogs.LocalWorklog{ID: "carry", StartedAtUTC: m.selectedDate.Add(-2 * time.Hour), DurationSeconds: 9 * 3600}
	m.worklogs = []worklogs.LocalWorklog{today}
	m.dayContext = worklogs.ContextDay{Date: m.selectedDate.Format("2006-01-02"), Worklogs: m.worklogs, TimelineWorklogs: []worklogs.LocalWorklog{carry, today}, BookedSeconds: 9 * 3600}
	ruler, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "today", 140))
	if !strings.HasPrefix(ruler, "00:00") || !strings.Contains(timelineWithoutDividers(band), "█▒") || !strings.Contains(m.renderTimelineLegend(nil, "today"), "▒ overtime") {
		t.Fatalf("carried-over hours were omitted from the quota or selected-day axis: %q", band)
	}
	m.openAddForm()
	m.form.previewRecords = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(23 * time.Hour), DurationSeconds: 2 * 3600}}
	if balance := m.renderProjectedDayBalance(); !strings.Contains(balance, "10h 00m total") {
		t.Fatalf("projected balance included next-day work: %q", balance)
	}
	ruler, _, _, _ = timelineParts(t, m.renderDayTimelineWidth(m.form.previewRecords, "", 140))
	if !strings.HasSuffix(ruler, "24:00") {
		t.Fatalf("day axis leaked into tomorrow: %q", ruler)
	}
}

func TestTimelineQuotaUsesActualElapsedTimeAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range []time.Time{time.Date(2026, 3, 29, 0, 0, 0, 0, location), time.Date(2026, 10, 25, 0, 0, 0, 0, location)} {
		end := day.AddDate(0, 0, 1)
		records := []worklogs.LocalWorklog{{StartedAtUTC: day.UTC(), DurationSeconds: int(end.Sub(day) / time.Second)}}
		start, overtime := timelineOvertimeStart(records, day, 8*time.Hour)
		if !overtime || !start.Equal(day.Add(8*time.Hour)) {
			t.Fatalf("%s: quota followed wall-clock labels instead of logged duration: %s", day, start)
		}
	}
}

func TestTimelinePaintsLastTwoHoursAfterEarlyStartAndWorkedLunch(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 600, 36
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 8 * 3600, Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"}}
	m.worklogs = []worklogs.LocalWorklog{{ID: "all", StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 10 * 3600}}
	// One minute per column makes the target boundary inspectable at 15:00.
	ruler, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 604))
	cells := []rune(band)
	if !strings.HasPrefix(ruler, "07:00") || !strings.HasSuffix(ruler, "17:00") || len(cells) != 600 {
		t.Fatalf("quota classification changed chronology: %q", ruler)
	}
	if strings.Contains(string(cells[:480]), "▒") || !strings.Contains(string(cells[480:]), "▒") || strings.Contains(band, "╱") {
		t.Fatalf("OT is not on the final two logged hours or worked lunch is excluded: %q", band)
	}
	if strings.Count(band, "▏") != 39 {
		t.Fatalf("actual OT did not retain the quarter-hour grid: %q", band)
	}
}
