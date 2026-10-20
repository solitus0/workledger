package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Run the feature's asynchronous effects, including native Bubble Tea batches.
func resolveIssueCommands(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			t.Fatal("issue commands did not settle")
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
		case issueTextSearchReadyMsg, issueTextSearchResultMsg, issueHistoryResultMsg, issueListResultMsg, activityResultMsg, activityListResultMsg:
			next, follow := m.Update(msg)
			m = next.(model)
			queue = append(queue, follow)
		}
	}
	return m
}

func TestIssueSearchModesRetainQueriesMarksSortAndLifetimeTotals(t *testing.T) {
	m, service := issueFixture(t)
	m.workspace.worklogs = service
	m.issueMarks = map[string]bool{"SBAH-11": true}
	m.issueSort = sortLogged
	m = sendIssueKey(m, "/")
	next, cmd := m.Update(tea.PasteMsg{Content: "lifecycle"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if !reflect.DeepEqual(orderedIssueKeys(m), []string{"SBAH-2"}) {
		t.Fatal("Issues mode did not search title")
	}
	m = sendIssueKey(m, "ctrl+t")
	if m.issueSearch.mode != searchWorklogText || m.issueSearch.input.Value() != "" {
		t.Fatal("modes share a query")
	}
	next, cmd = m.Update(tea.PasteMsg{Content: "MANUAL ORDERING"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if !reflect.DeepEqual(orderedIssueKeys(m), []string{"SBAH-1"}) || m.selectedIssue().LoggedSeconds != 3600 || m.issueSort != sortLogged || !m.issueMarks["SBAH-11"] {
		t.Fatal("description search changed sort, marks, budgets, or result identity")
	}
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "Find · Worklog text") || !strings.Contains(view, "[match]") || !strings.Contains(view, "Manual ordering") {
		t.Fatalf("mode or match unexplained: %s", view)
	}
	m = sendIssueKey(m, "ctrl+t")
	if m.issueSearch.input.Value() != "lifecycle" || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("Issues query was not restored")
	}
	next, cmd = m.Update(actionKey("ctrl+t"))
	m = resolveIssueCommands(t, next.(model), cmd)
	if m.issueSearch.input.Value() != "MANUAL ORDERING" || m.selectedIssueKey() != "SBAH-1" {
		t.Fatal("worklog query was not restored")
	}
	m = sendIssueKey(m, "tab")
	if m.issueSearch.editing || m.focus != focusIssueMetadata {
		t.Fatal("Tab did not transfer search focus to metadata")
	}
	m = sendIssueKey(m, "tab")
	if m.focus != focusIssueHistory {
		t.Fatal("second Tab did not focus history")
	}
	m = sendIssueKey(m, "esc")
	if m.focus != focusWorkspace || !m.issueMarks["SBAH-11"] {
		t.Fatal("history focus lost list context")
	}
}

func TestWorklogSearchRevealsOldMatchAndRetainsFullHistoryWithoutColor(t *testing.T) {
	m := historyModel(t, 63)
	m.deps.noColor = true
	m = resolveHistory(t, m, m.syncIssueHistory())
	m = sendIssueKey(sendIssueKey(m, "/"), "ctrl+t")
	next, cmd := m.Update(tea.PasteMsg{Content: "ENTRY 62"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if len(m.issueHistory.items) != 63 || !strings.Contains(ansi.Strip(m.render()), "Entry 62") || !strings.Contains(m.render(), "[match]") {
		t.Fatal("first matching entry beyond preview page was not revealed")
	}
	if strings.Contains(m.render(), "\x1b[") {
		t.Fatal("NO_COLOR emitted ANSI")
	}
	captureBulkState(t, m, "worklog-search-old-match-100x28")
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "tab"), "tab"), "home")
	if !strings.Contains(m.render(), "Entry 00") || m.issueHistory.history.YOffset() != 0 {
		t.Fatal("search hid unrelated history")
	}
}

func TestWorklogTextModeExcludesMetadataOnlyMatches(t *testing.T) {
	m, service := issueFixture(t)
	m.workspace.worklogs = service
	m = sendIssueKey(m, "/")
	next, cmd := m.Update(tea.PasteMsg{Content: "Document"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if len(m.visibleIssues()) != 3 {
		t.Fatal("Issues mode omitted matching epic metadata")
	}
	m = sendIssueKey(m, "ctrl+t")
	next, cmd = m.Update(tea.PasteMsg{Content: "Document"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if len(m.visibleIssues()) != 0 || !strings.Contains(ansi.Strip(m.render()), "No matching issues.") {
		t.Fatal("Worklog text mode included metadata matches")
	}
	m = sendIssueKey(m, "ctrl+t")
	if len(m.visibleIssues()) != 3 || m.issueSearch.input.Value() != "Document" {
		t.Fatal("switching back lost metadata query")
	}
}

func TestWorklogSearchRejectsObsoleteResultsAndRetriesFailures(t *testing.T) {
	m := historyModel(t, 30)
	m = resolveHistory(t, m, m.syncIssueHistory())
	m = sendIssueKey(sendIssueKey(m, "/"), "ctrl+t")
	m.issueSearch.input.SetValue("Entry 00")
	first := m.loadIssueTextSearch(false)()
	m.issueSearch.input.SetValue("Entry 29")
	latest := m.loadIssueTextSearch(false)
	next, cmd := m.Update(first)
	m = resolveIssueCommands(t, next.(model), cmd)
	if !m.issueSearch.loading || len(m.visibleIssues()) != 0 {
		t.Fatal("stale query result applied")
	}
	m = resolveIssueCommands(t, m, latest)
	if m.selectedIssueKey() != "APP-1" || !strings.Contains(ansi.Strip(m.render()), "Entry 29") {
		t.Fatal("latest query did not win")
	}
	old := m.loadIssueTextSearch(false)()
	m = sendIssueKey(m, "esc")
	m = sendIssueKey(sendIssueKey(m, "/"), "ctrl+t")
	m.issueSearch.input.SetValue("Entry 29")
	worklogState(m.workspace).usedIssueErr = errors.New("search unavailable")
	current := m.loadIssueTextSearch(false)
	next, _ = m.Update(old)
	m = next.(model)
	if !m.issueSearch.loading {
		t.Fatal("reopened search accepted a stale generation")
	}
	m = resolveIssueCommands(t, m, current)
	if !strings.Contains(ansi.Strip(m.render()), "Worklog search failed") || len(m.visibleIssues()) != 0 {
		t.Fatal("search error appeared as empty success")
	}
	worklogState(m.workspace).usedIssueErr = nil
	m = sendIssueKey(m, "enter")
	m.workspace.issues = &fakeIssues{items: m.issues}
	next, cmd = m.Update(actionKey("r"))
	m = resolveIssueCommands(t, next.(model), cmd)
	if m.issueSearch.err != "" || m.selectedIssueKey() != "APP-1" {
		t.Fatal("refresh did not retry the current query")
	}
}

func TestWorklogSearchKeepsEpicScopeAndRefreshSelection(t *testing.T) {
	m := historyModel(t, 30)
	ep := "A local epic"
	m.issues[1].Epic = ep
	m.issues = append(m.issues, budgetIssue("APP-3", "Outside epic", "Other epic", 0, 0, false))
	state := worklogState(m.workspace)
	state.issueDetails["APP-2"] = state.issueDetails["APP-1"][:1]
	state.issueDetails["APP-3"] = state.issueDetails["APP-1"][:1]
	m.issueEpic = &ep
	m.workspace.issues = &fakeIssues{items: m.issues}
	m = sendIssueKey(sendIssueKey(m, "/"), "ctrl+t")
	next, cmd := m.Update(tea.PasteMsg{Content: "Entry"})
	m = resolveIssueCommands(t, next.(model), cmd)
	if !reflect.DeepEqual(orderedIssueKeys(m), []string{"APP-1", "APP-2"}) {
		t.Fatal("description search escaped epic scope")
	}
	next, cmd = m.Update(actionKey("down"))
	m = resolveIssueCommands(t, next.(model), cmd)
	m = sendIssueKey(m, "enter")
	next, cmd = m.Update(actionKey("r"))
	m = resolveIssueCommands(t, next.(model), cmd)
	if m.selectedIssueKey() != "APP-2" || m.issueEpic == nil || *m.issueEpic != ep {
		t.Fatal("refresh lost scoped search selection")
	}
}

func TestWorklogSearchDebounceDiscardsObsoleteReadyMessages(t *testing.T) {
	m := historyModel(t, 30)
	m = resolveHistory(t, m, m.syncIssueHistory())
	m = sendIssueKey(sendIssueKey(m, "/"), "ctrl+t")
	next, first := m.Update(tea.PasteMsg{Content: "Entry"})
	m = next.(model)
	ready := first()
	next, latest := m.Update(tea.PasteMsg{Content: " 29"})
	m = next.(model)
	next, cmd := m.Update(ready)
	m = next.(model)
	if cmd != nil || !m.issueSearch.loading {
		t.Fatal("obsolete debounce started a query")
	}
	m = resolveIssueCommands(t, m, latest)
	if m.selectedIssueKey() != "APP-1" || !strings.Contains(ansi.Strip(m.render()), "Entry 29") {
		t.Fatal("latest debounced query did not finish")
	}
}
