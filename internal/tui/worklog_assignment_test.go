package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

func settleAssignment(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("assignment commands did not settle")
		}
		pending := queue[0]
		queue = queue[1:]
		if pending == nil {
			continue
		}
		msg := pending()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		switch msg.(type) {
		case worklogResultMsg, worklogAssignmentLoadedMsg, worklogAssignmentSuggestionsMsg, worklogAssignmentResultMsg, worklogMarksCheckedMsg, issuePickerReadyMsg, issuePickerResultMsg, trackerConsumedMsg, issueListResultMsg, issueTotalResultMsg:
			next, follow := m.Update(msg)
			m = next.(model)
			queue = append(queue, follow)
		}
	}
	return m
}

func assignmentFixture(t *testing.T) (model, *worklogs.Service) {
	m, service := issueFixture(t)
	m.workspace.worklogs = service
	m.workspace.mutations = service
	m.workspace.cfg.DayEnd = "17:00"
	m.workspace.cfg.DailyLunch = "12:00-12:45"
	m.activeTab = worklogsTab
	m.worklogView = unassignedWorklogView
	m.worklogLoading = false
	m.width, m.height = 100, 28
	for i := 0; i < 3; i++ {
		_, err := service.Add(t.Context(), m.workspace.cfg, worklogs.AddInput{StartedUTC: fmt.Sprintf("2026-10-%02dT09:00:00Z", 14+i), Duration: "15m", Description: fmt.Sprintf("Pending activity %d", i+1)})
		if err != nil {
			t.Fatal(err)
		}
	}
	load := m.startWorklogLoad()
	message := load().(worklogResultMsg)
	if message.err != nil || len(message.items) != 3 {
		t.Fatalf("backlog load: view=%v count=%d err=%v", m.worklogView, len(message.items), message.err)
	}
	next, follow := m.Update(message)
	m = settleAssignment(t, next.(model), follow)
	return m, service
}

func TestWorklogAssignmentSearchAndSave(t *testing.T) {
	m, service := assignmentFixture(t)
	m = sendIssueKey(m, "v")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "v")
	if len(m.worklogMarks) != 3 {
		t.Fatalf("range marks=%v rows=%d loading=%v tab=%v focus=%v interaction=%v selected=%d", m.worklogMarks, len(m.worklogs), m.worklogLoading, m.activeTab, m.focus, m.interactionState.kind(), m.worklogSelected)
	}
	m = sendIssueKey(m, "up")
	if len(m.worklogMarks) != 3 {
		t.Fatal("focus changed marks")
	}
	next, cmd := m.Update(actionKey("b"))
	m = settleAssignment(t, next.(model), cmd)
	if m.worklogAssignment == nil || m.worklogAssignment.loading {
		t.Fatal("assignment not loaded")
	}
	for _, noColor := range []bool{true, false} {
		m.deps.noColor = noColor
		for _, size := range [][2]int{{100, 28}, {140, 40}} {
			m.width, m.height = size[0], size[1]
			assertRedesignBounds(t, m, "assignment dialog")
		}
	}
	modal := ansi.Strip(m.renderWorklogAssignment(64, 20))
	if !strings.Contains(modal, "3 worklogs selected") || strings.Contains(modal, "Pending activity") {
		t.Fatalf("modal repeats selected worklogs: %s", modal)
	}
	next, cmd = m.Update(ctrlKey('k'))
	m = settleAssignment(t, next.(model), cmd)
	if m.issuePicker == nil || len(m.issuePicker.items) == 0 || len(m.issuePicker.details) != 0 {
		t.Fatal("issue-only picker missing or loaded history")
	}
	for _, key := range []string{"ctrl+t", "ctrl+t"} {
		next, cmd = m.Update(actionKey(key))
		m = settleAssignment(t, next.(model), cmd)
	}
	if m.issuePicker.mode != worklogs.IssueFindByTitle {
		t.Fatal("title search mode missing")
	}
	m.issuePicker.input.SetValue("lifecycle")
	m = settleAssignment(t, m, m.loadIssuePickerPage(0, false))
	if len(m.issuePicker.items) != 1 || m.issuePicker.items[0].IssueKey != "SBAH-2" {
		t.Fatal("title search did not select matching issue")
	}
	assertRedesignBounds(t, m, "issue finder")
	m = sendIssueKey(m, "enter")
	if m.issuePicker != nil || m.worklogAssignment == nil || m.worklogAssignment.input.Value() != "SBAH-2" {
		t.Fatal("choice did not return to assignment")
	}
	before, _, _ := service.List(t.Context(), m.workspace.cfg, worklogs.ListFilters{Unassigned: true})
	if len(before) != 3 {
		t.Fatal("choosing issue saved assignment early")
	}
	m.workspace.cfg.MinimumDurationSeconds = 3600
	next, cmd = m.Update(actionKey("enter"))
	m = next.(model)
	if !m.worklogAssignment.saving || cmd == nil {
		t.Fatal("save did not start")
	}
	m = settleAssignment(t, m, cmd)
	if m.worklogAssignment != nil || len(m.worklogMarks) != 0 || len(m.worklogs) != 0 {
		t.Fatalf("save did not clear submitted selection: %#v", m.worklogAssignment)
	}
}

func TestWorklogMarksRangeHiddenSelectionAndScale(t *testing.T) {
	ws, deps := testWorkspace(t)
	state := worklogState(ws)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 10000; i++ {
		state.items = append(state.items, worklogs.LocalWorklog{ID: fmt.Sprintf("row-%05d", i), StartedAtUTC: start.Add(time.Duration(i) * 30 * time.Minute), DurationSeconds: 900, Description: "Pending activity", Revision: 1})
	}
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 100, 28
	m.worklogView = unassignedWorklogView
	m.worklogs = state.items
	m.worklogLoading = false
	m = sendIssueKey(m, "v")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "up")
	if len(m.worklogMarks) != 2 {
		t.Fatalf("shrinking range retained trail marks=%v loading=%v focus=%v selected=%v", m.worklogMarks, m.worklogLoading, m.focus, m.worklogSelected)
	}
	m = sendIssueKey(m, "esc")
	if m.worklogRange != nil || len(m.worklogMarks) != 2 {
		t.Fatal("Esc did not finish range first")
	}
	m = sendIssueKey(m, "ctrl+a")
	if len(m.worklogMarks) != 10000 {
		t.Fatal("Ctrl+A did not select all matches")
	}
	m = sendIssueKey(m, "end")
	if m.worklogSelected != 9999 {
		t.Fatal("End did not select final row")
	}
	assertRedesignBounds(t, m, "large backlog")
	if strings.Count(ansi.Strip(m.render()), "Pending activity") > 30 {
		t.Fatal("rendered offscreen backlog rows")
	}
	// Only a subset is loaded in Day; all other marks still exist in storage.
	m.worklogView = dayWorklogView
	m.worklogs = state.items[:1]
	m.worklogSelected = 0
	m = settleAssignment(t, m, m.checkWorklogMarks())
	if len(m.worklogMarks) != 10000 || !strings.Contains(m.worklogSelectionLabel(), "9999 outside current view") {
		t.Fatal("date scope pruned hidden marks")
	}
	state.items = state.items[1:]
	m = settleAssignment(t, m, m.checkWorklogMarks())
	if len(m.worklogMarks) != 9999 {
		t.Fatal("confirmed deletion did not prune mark")
	}
}

func TestWorklogAssignmentFailureCancelAndStale(t *testing.T) {
	m, _ := assignmentFixture(t)
	m = sendIssueKey(m, "space")
	next, cmd := m.Update(actionKey("b"))
	m = settleAssignment(t, next.(model), cmd)
	m.worklogAssignment.input.SetValue("SBAH-2")
	m = sendIssueKey(m, "esc")
	if len(m.worklogMarks) != 1 || m.overlay != noOverlay {
		t.Fatal("cancel lost explicit selection")
	}
	next, cmd = m.Update(actionKey("b"))
	m = settleAssignment(t, next.(model), cmd)
	m.worklogAssignment.input.SetValue("SBAH-2")
	m.deps.checkWritable = func(string, string) error { return errors.New("read only storage") }
	next, cmd = m.Update(actionKey("enter"))
	m = settleAssignment(t, next.(model), cmd)
	if m.worklogAssignment == nil || m.worklogAssignment.input.Value() != "SBAH-2" || len(m.worklogMarks) != 1 || m.worklogAssignment.err != "read only storage" {
		t.Fatal("failed save lost selection or destination")
	}
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if !m.worklogAssignment.stale {
		t.Fatal("external change did not block stale assignment")
	}
	_, cmd = m.Update(actionKey("enter"))
	if cmd != nil {
		t.Fatal("stale assignment submitted")
	}
}
