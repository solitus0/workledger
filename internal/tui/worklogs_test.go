package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestWorklogGenerationAndDateProtectAgainstLateResults(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogGen = 7
	m.worklogs = testRows()
	late := worklogResultMsg{generation: 6, date: m.selectedDate, items: []worklogs.LocalWorklog{{ID: "late"}}}
	next, _ := m.Update(late)
	m = next.(model)
	if m.worklogs[0].ID != "row-1" {
		t.Fatalf("late generation overwrote worklogs: %#v", m.worklogs)
	}

	wrongDate := worklogResultMsg{generation: 7, date: m.selectedDate.AddDate(0, 0, -1), items: []worklogs.LocalWorklog{{ID: "wrong-date"}}}
	next, _ = m.Update(wrongDate)
	m = next.(model)
	if m.worklogs[0].ID != "row-1" {
		t.Fatalf("wrong-date result overwrote worklogs: %#v", m.worklogs)
	}
}

func TestWorklogNavigationAndToday(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	start := m.selectedDate
	next, _ := m.Update(textKey("l"))
	m = next.(model)
	if !sameDay(m.selectedDate, start.AddDate(0, 0, 1)) {
		t.Fatalf("right day = %v", m.selectedDate)
	}
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	if !sameDay(m.selectedDate, beginningOfDay(deps.now(), time.UTC)) {
		t.Fatalf("today = %v", m.selectedDate)
	}
}

func TestWeekModeNavigatesDaysAndWeeksAndOpensSelectedDay(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.weekDays = testWeekDays()
	m.selectWeekDate(m.selectedDate)

	next, _ := m.Update(textKey("w"))
	m = next.(model)
	if m.worklogView != weekWorklogView {
		t.Fatalf("worklog view = %v, want week", m.worklogView)
	}
	next, _ = m.Update(textKey("j"))
	m = next.(model)
	if got := m.selectedDate.Format("2006-01-02"); got != "2026-05-22" {
		t.Fatalf("selected week day = %s, want 2026-05-22", got)
	}
	if len(m.worklogs) != 1 || m.worklogs[0].ID != "fri" {
		t.Fatalf("selected day worklogs = %#v", m.worklogs)
	}
	m.selectWeekDate(time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	next, _ = m.Update(textKey("j"))
	m = next.(model)
	if got := m.selectedDate.Format("2006-01-02"); got != "2026-05-24" {
		t.Fatalf("week day navigation crossed Sunday: %s", got)
	}

	next, cmd := m.Update(textKey("h"))
	m = next.(model)
	if got := m.selectedDate.Format("2006-01-02"); got != "2026-05-17" || cmd == nil {
		t.Fatalf("previous week selected date=%s cmd=%v", got, cmd)
	}
	if len(m.weekDays) != 0 || len(m.worklogs) != 0 {
		t.Fatalf("previous week retained stale rows")
	}

	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.worklogView != dayWorklogView {
		t.Fatalf("Enter did not open selected day")
	}

	next, _ = m.Update(textKey("w"))
	m = next.(model)
	next, _ = m.Update(textKey("d"))
	m = next.(model)
	if m.worklogView != dayWorklogView {
		t.Fatalf("d did not return from week to day")
	}
}

func TestWorklogLoadFetchesSelectedMondayThroughSundayOnce(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.context.Days = testWeekDays()
	m := newModel(context.Background(), deps, ws, nil)

	result := m.worklogCmd(3, m.selectedDate)().(worklogResultMsg)
	if result.err != nil {
		t.Fatalf("worklog load: %v", result.err)
	}
	if len(service.contextInputs) != 1 {
		t.Fatalf("context calls = %d, want 1", len(service.contextInputs))
	}
	input := service.contextInputs[0]
	if input.From != "2026-05-18" || input.To != "2026-05-24" {
		t.Fatalf("context window = %s..%s", input.From, input.To)
	}
	if len(result.items) != 1 || result.items[0].ID != "thu" || len(result.weekDays) != 7 {
		t.Fatalf("selected week result = %#v", result)
	}
}

func TestTabFocusAndRowNavigation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = append(testRows(), worklogs.LocalWorklog{ID: "row-2", Revision: 1})
	next, _ := m.Update(textKey("j"))
	m = next.(model)
	if m.worklogSelected != 1 {
		t.Fatalf("row selection = %d, want 1", m.worklogSelected)
	}
	next, _ = m.Update(textKey("1"))
	m = next.(model)
	if m.activeTab != statusTab {
		t.Fatalf("tab 1 did not select status")
	}
	next, _ = m.Update(specialKey(tea.KeyTab))
	m = next.(model)
	if m.focus != focusRail {
		t.Fatalf("tab did not focus rail")
	}
	next, _ = m.Update(textKey("j"))
	m = next.(model)
	if m.activeTab != worklogsTab || m.focus != focusRail {
		t.Fatalf("rail navigation tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.focus == focusRail {
		t.Fatal("rail activation did not focus the selected workspace")
	}
}

func TestDeleteAndHelpOverlays(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = testRows()
	next, _ := m.Update(textKey("d"))
	m = next.(model)
	if m.overlay != noOverlay || m.worklogView != dayWorklogView {
		t.Fatalf("lowercase d should select Day without deleting: view=%v overlay=%v", m.worklogView, m.overlay)
	}
	next, _ = m.Update(textKey("D"))
	m = next.(model)
	if m.overlay != deleteOverlay {
		t.Fatalf("delete overlay = %v", m.overlay)
	}
	if len(m.deleteSelection) != 1 || !strings.Contains(m.render(), "Move selected worklog to trash?") {
		t.Fatalf("single delete did not snapshot and explain trash archival")
	}
	next, _ = m.Update(specialKey(tea.KeyEscape))
	m = next.(model)
	if m.overlay != noOverlay {
		t.Fatalf("escape did not close overlay")
	}
	next, _ = m.Update(textKey("?"))
	m = next.(model)
	if m.overlay != helpOverlay {
		t.Fatalf("help overlay = %v", m.overlay)
	}
}

func TestWeekDeleteSnapshotsAndDeletesExactlySelectedDay(t *testing.T) {
	ws, deps := testWorkspace(t)
	checks := 0
	deps.checkWritable = func(string, string) error { checks++; return nil }
	service := worklogState(ws)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.worklogView = weekWorklogView
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "one", Revision: 2, DurationSeconds: 3600},
		{ID: "two", Revision: 4, DurationSeconds: 5400},
	}

	next, _ := m.Update(textKey("D"))
	m = next.(model)
	if m.overlay != deleteDayOverlay || len(m.deleteSelection) != 2 {
		t.Fatalf("week delete overlay=%v selection=%#v", m.overlay, m.deleteSelection)
	}
	rendered := m.render()
	for _, expected := range []string{"Move selected day's worklogs to trash?", "2 worklogs (2h 30m)", "Thursday, 2026-05-21", "They can be restored if conflict-free."} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("week delete confirmation missing %q:\n%s", expected, rendered)
		}
	}

	m.worklogs = nil
	next, cmd := m.Update(textKey("y"))
	m = next.(model)
	if cmd == nil || m.overlay != noOverlay || len(m.deleteSelection) != 0 {
		t.Fatalf("week delete confirmation was not submitted cleanly")
	}
	result := cmd().(mutationResultMsg)
	if result.err != nil || result.deleted != 2 || checks != 1 || len(service.batchDeletes) != 1 {
		t.Fatalf("week delete result=%#v checks=%d calls=%#v", result, checks, service.batchDeletes)
	}
	call := service.batchDeletes[0]
	if call.filters.From != "2026-05-21" || call.filters.To != "2026-05-21" || len(call.expected) != 2 || call.expected[0] != (worklogs.DeleteExpectation{ID: "one", Revision: 2}) || call.expected[1] != (worklogs.DeleteExpectation{ID: "two", Revision: 4}) {
		t.Fatalf("week delete snapshot call = %#v", call)
	}
	next, _ = m.Update(result)
	m = next.(model)
	if m.notice != "deleted 2 worklogs from 2026-05-21" {
		t.Fatalf("week delete notice = %q", m.notice)
	}
}

func TestWeekDeleteOnEmptyDayIsHarmless(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogView = weekWorklogView
	next, cmd := m.Update(textKey("D"))
	m = next.(model)
	if cmd != nil || m.overlay != noOverlay || m.notice != "no worklogs to delete on the selected day" {
		t.Fatalf("empty week delete cmd=%v overlay=%v notice=%q", cmd, m.overlay, m.notice)
	}
}

func TestFooterNoticeUsesExplicitSeverity(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.setNotice(noticeError, "save did not complete")
	if footer := m.renderFooter(120); !strings.Contains(footer, m.paint("save did not complete", m.colors.Destructive)) {
		t.Fatalf("error notice missing destructive style: %q", footer)
	}
	m.setNotice(noticeSuccess, "saved plan created")
	if footer := m.renderFooter(120); !strings.Contains(footer, m.paint("saved plan created", m.colors.Success)) {
		t.Fatalf("success notice depends on copy instead of explicit severity: %q", footer)
	}
}

func TestSelectedIssueTotalLoadsForExactIssueAndIgnoresLateResults(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.issueTotals = map[string]int{"APP-1": 3 * 3600, "APP-2": 90 * 60}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "one", IssueKey: "APP-1"},
		{ID: "two", IssueKey: "APP-2"},
	}

	cmd := m.startIssueTotalLoad()
	firstGeneration := m.issueTotalGen
	if cmd == nil || !m.issueTotalLoading {
		t.Fatal("selected issue total was not scheduled")
	}
	next, _ := m.Update(cmd())
	m = next.(model)
	if m.issueTotal != 3*3600 || !strings.Contains(m.renderWorklogDetail(), "Local issue total 3h 00m") {
		t.Fatalf("first issue total state = %#v detail=%q", m, m.renderWorklogDetail())
	}

	next, cmd = m.Update(textKey("j"))
	m = next.(model)
	if cmd == nil || m.issueTotalIssue != "APP-2" || !m.issueTotalLoading {
		t.Fatalf("second issue total was not scheduled: issue=%q loading=%v", m.issueTotalIssue, m.issueTotalLoading)
	}
	late := issueTotalResultMsg{generation: firstGeneration, issueKey: "APP-1", totalSeconds: 99 * 3600}
	next, _ = m.Update(late)
	m = next.(model)
	if m.issueTotal != 0 || m.issueTotalIssue != "APP-2" {
		t.Fatalf("late issue total overwrote selection: %#v", m)
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.issueTotal != 90*60 || strings.Join(service.issueTotalKeys, ",") != "APP-1,APP-2" {
		t.Fatalf("second issue total=%d keys=%v", m.issueTotal, service.issueTotalKeys)
	}
}

func TestSelectedIssueTotalFailureIsNonFatal(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.issueTotalErr = errors.New("aggregate unavailable")
	m := newModel(context.Background(), deps, ws, nil)
	m.worklogs = []worklogs.LocalWorklog{{ID: "one", IssueKey: "APP-1"}}

	cmd := m.startIssueTotalLoad()
	next, _ := m.Update(cmd())
	m = next.(model)
	if !strings.Contains(m.renderWorklogDetail(), "Local issue total Unavailable") || m.selectedWorklog() == nil {
		t.Fatalf("aggregate failure should preserve detail: %q", m.renderWorklogDetail())
	}
}

func TestQuitAndInterruptCommands(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	_, cmd := m.Update(textKey("q"))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("q command = %T", cmd())
	}
	_, cmd = m.Update(ctrlKey('c'))
	if _, ok := cmd().(tea.InterruptMsg); !ok {
		t.Fatalf("ctrl+c command = %T", cmd())
	}
}
