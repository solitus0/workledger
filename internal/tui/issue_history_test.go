package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

func resolveHistory(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for steps := 0; cmd != nil; steps++ {
		if steps > 20 {
			t.Fatal("history failed to finish bounded page requests")
		}
		msg := cmd()
		if _, ok := msg.(issueHistoryResultMsg); !ok {
			t.Fatalf("unexpected history message %T", msg)
		}
		next, pending := m.Update(msg)
		m = next.(model)
		cmd = pending
	}
	return m
}

func TestIssueLoggedColumnAndSortIncludesUnestimatedIssues(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{
		budgetIssue("APP-10", "Ten", "Alpha", 0, 3600, false),
		budgetIssue("APP-2", "Two", "Beta", 36000, 3600, true),
		budgetIssue("APP-3", "Three", "Gamma", 0, 7200, false),
		budgetIssue("APP-1", "Unused", "", 10000, 0, true),
	}
	m.restoreIssueSelection("APP-2")
	m.issueMarks = map[string]bool{"APP-10": true}
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "end"), "enter")
	if m.issueSort != sortLogged || !reflect.DeepEqual(orderedIssueKeys(m), []string{"APP-3", "APP-2", "APP-10", "APP-1"}) || m.selectedIssueKey() != "APP-2" || !m.issueMarks["APP-10"] {
		t.Fatalf("logged sort lost ordering/focus/marks: %v", orderedIssueKeys(m))
	}
	for _, size := range [][2]int{{100, 28}, {140, 40}} {
		m.width, m.height = size[0], size[1]
		view := ansi.Strip(m.render())
		if !strings.Contains(view, "Logged") || !strings.Contains(view, "Sort: Logged ↓") {
			t.Fatal("logged column/sort label absent")
		}
		found := false
		for _, line := range strings.Split(view, "\n") {
			panes := strings.Split(line, "│")
			if len(panes) > 1 && strings.Contains(panes[1], "APP-3") && strings.Contains(panes[1], "2h") && strings.Contains(panes[1], "Unknown") {
				found = true
			}
		}
		if !found {
			t.Fatalf("list omitted logged time for an unestimated issue: %s", view)
		}
		captureBulkState(t, m, fmt.Sprintf("logged-issues-%dx%d", size[0], size[1]))
	}
	m = sendIssueKey(m, "t")
	m.restoreEpicSelection("Beta", true)
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "end"), "enter")
	if m.epicSort != sortLogged || m.selectedEpic().Epic != "Beta" || m.visibleEpics()[0].Epic != "Gamma" || !strings.Contains(ansi.Strip(m.render()), "2h logged") {
		t.Fatal("epic logged sort/budget missing")
	}
}

func TestIssueHistoryUsesAllTimeActiveSQLiteWorklogsAndPreservesBatch(t *testing.T) {
	m, service := issueFixture(t)
	m.workspace.worklogs = service
	m.issueMarks = map[string]bool{"SBAH-1": true}
	m.width, m.height = 100, 28
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	if m.issueHistory == nil || m.issueHistory.issueKey != "SBAH-2" || !m.issueHistory.loading || !strings.Contains(ansi.Strip(m.render()), "Loading worklogs") {
		t.Fatal("issue details did not load history")
	}
	m = resolveHistory(t, m, cmd)
	if len(m.issueHistory.items) != 2 || m.issueHistory.more {
		t.Fatalf("history omitted all-time worklogs: %+v", m.issueHistory.items)
	}
	view := ansi.Strip(m.render())
	for _, text := range []string{"Lifecycle checks", "Data lifecycle implementation", "2026-10-12 09:00", "2026-10-11 09:00", "Logged", "10h", "Tab Pane"} {
		if !strings.Contains(view, text) {
			t.Fatalf("issue detail missing %q:\n%s", text, view)
		}
	}
	if strings.Contains(view, "Manual ordering") {
		t.Fatal("history includes a different issue")
	}
	captureBulkState(t, m, "history-real-100x28")
	m = sendIssueKey(m, "esc")
	if m.issueHistory != nil || m.overlay != noOverlay || m.selectedIssueKey() != "SBAH-2" || !m.issueMarks["SBAH-1"] {
		t.Fatal("closing history lost list focus/batch")
	}
}

func historyModel(t *testing.T, count int) model {
	t.Helper()
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{budgetIssue("APP-1", "A long issue title which wraps across several metadata rows", "A local epic", 36000, 27000, true), budgetIssue("APP-2", "Unused", "", 0, 0, false)}
	m.restoreIssueSelection("APP-1")
	state := worklogState(m.workspace)
	state.issueDetails = map[string][]worklogs.IssueWorklogDescription{}
	for n := 0; n < count; n++ {
		state.issueDetails["APP-1"] = append(state.issueDetails["APP-1"], worklogs.IssueWorklogDescription{StartedAtUTC: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC).AddDate(0, 0, -n), DurationSeconds: 900, Description: fmt.Sprintf("Entry %02d — repeated description\nSecond paragraph", n)})
	}
	return m
}

func TestIssueHistoryPagesToLastWorklogAndNavigatesIndependentPanes(t *testing.T) {
	m := historyModel(t, 63)
	next, cmd := m.Update(actionKey("enter"))
	m = resolveHistory(t, next.(model), cmd)
	if len(m.issueHistory.items) != issueDetailPageSize || !m.issueHistory.more {
		t.Fatal("initial query is not a bounded page")
	}
	next, cmd = m.Update(actionKey("pgdown"))
	m = resolveHistory(t, next.(model), cmd)
	if m.issueHistory.history.YOffset() == 0 {
		t.Fatal("page did not scroll history")
	}
	next, cmd = m.Update(actionKey("end"))
	m = resolveHistory(t, next.(model), cmd)
	if len(m.issueHistory.items) != 63 || m.issueHistory.more || !strings.Contains(ansi.Strip(m.render()), "Entry 62") {
		t.Fatal("End did not reach all remaining pages")
	}
	m = sendIssueKey(m, "tab")
	if m.issueHistory.historyFocus {
		t.Fatal("Tab did not focus metadata")
	}
	historyOffset := m.issueHistory.history.YOffset()
	m = sendIssueKey(m, "end")
	if m.issueHistory.history.YOffset() != historyOffset {
		t.Fatal("metadata navigation moved history")
	}
	m = sendIssueKey(m, "right")
	m = sendIssueKey(m, "home")
	if !m.issueHistory.historyFocus || m.issueHistory.history.YOffset() != 0 || !strings.Contains(ansi.Strip(m.render()), "Entry 00") {
		t.Fatal("history Home did not return to the first worklog")
	}
	for _, size := range [][2]int{{100, 28}, {140, 40}} {
		for _, mode := range []string{"dark", "light", "no-color"} {
			m.deps.noColor = mode == "no-color"
			m.colorReady = true
			if mode == "light" {
				m.colors = m.deps.theme.Light
			} else {
				m.colors = m.deps.theme.Dark
			}
			m.refreshIssueHistoryMetadata()
			next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = next.(model)
			captureBulkState(t, m, fmt.Sprintf("history-%s-%dx%d", mode, size[0], size[1]))
		}
	}
}

func TestIssueHistoryIgnoresClosedReopenedAndRefreshedRequests(t *testing.T) {
	m := historyModel(t, 60)
	next, firstCmd := m.Update(actionKey("enter"))
	m = next.(model)
	firstMsg := firstCmd()
	m = sendIssueKey(m, "esc")
	m.restoreIssueSelection("APP-2")
	next, unusedCmd := m.Update(actionKey("enter"))
	m = next.(model)
	next, _ = m.Update(firstMsg)
	m = next.(model)
	if m.issueHistory.issueKey != "APP-2" || len(m.issueHistory.items) != 0 {
		t.Fatal("closed issue result populated a different issue")
	}
	m = resolveHistory(t, m, unusedCmd)
	if !strings.Contains(ansi.Strip(m.render()), "No worklogs for this issue") {
		t.Fatal("unused issue missing empty state")
	}
	m = sendIssueKey(m, "esc")
	m.restoreIssueSelection("APP-1")
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	next, _ = m.Update(firstMsg)
	m = next.(model)
	if len(m.issueHistory.items) != 0 {
		t.Fatal("same issue reopen accepted stale generation")
	}
	m = resolveHistory(t, m, cmd)
	next, pageCmd := m.Update(actionKey("end"))
	m = next.(model)
	oldPage := pageCmd()
	reload := m.reloadIssueHistory()
	next, _ = m.Update(oldPage)
	m = next.(model)
	if len(m.issueHistory.items) != 0 || !m.issueHistory.loading {
		t.Fatal("refresh accepted an old page")
	}
	m = resolveHistory(t, m, reload)
	if len(m.issueHistory.items) != issueDetailPageSize || m.issueHistory.seekEnd {
		t.Fatal("refresh did not reset pagination")
	}
	m = sendIssueKey(m, "esc")
	next, _ = m.Update(oldPage)
	m = next.(model)
	if m.issueHistory != nil {
		t.Fatal("late page reopened closed details")
	}
}

func TestIssueHistoryFailureRetryAndExternalChange(t *testing.T) {
	m := historyModel(t, 30)
	state := worklogState(m.workspace)
	state.issueDetailErr = errors.New("history unavailable")
	next, cmd := m.Update(actionKey("enter"))
	m = resolveHistory(t, next.(model), cmd)
	if m.issueHistory.err != "history unavailable" || m.issueHistory.loading || !strings.Contains(ansi.Strip(m.render()), "Worklogs unavailable") {
		t.Fatal("query failure hidden")
	}
	captureBulkState(t, m, "history-failed")
	state.issueDetailErr = nil
	cmd = m.reloadIssueHistory()
	m = resolveHistory(t, m, cmd)
	if len(m.issueHistory.items) != 25 || m.issueHistory.err != "" {
		t.Fatal("retry did not recover")
	}
	state.issueDetailErr = errors.New("next page unavailable")
	next, cmd = m.Update(actionKey("end"))
	m = resolveHistory(t, next.(model), cmd)
	if len(m.issueHistory.items) != 25 || m.issueHistory.err == "" || m.issueHistory.seekEnd {
		t.Fatal("page failure lost existing history or continued loading")
	}
	state.issueDetailErr = nil
	previousGeneration := m.issueHistory.generation
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if m.issueHistory.generation <= previousGeneration || !m.issueHistory.loading || len(m.issueHistory.items) != 0 {
		t.Fatal("external mutation did not restart history")
	}
	// The scheduled request is bounded and generation protected; run its equivalent query.
	m.issueHistory.loading = false
	m = resolveHistory(t, m, m.loadIssueHistoryPage())
	if len(m.issueHistory.items) != 25 {
		t.Fatal("external refresh failed")
	}
	updated := slices.Clone(m.issues)
	updated[0].LoggedSeconds = 36000
	next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, items: updated})
	m = next.(model)
	if !strings.Contains(m.issueHistory.metadataText, "Logged    10h") {
		t.Fatal("metadata budget not refreshed with history")
	}
}
