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

func TestIssueHistoryAppearsInlineForSelectionAcrossAllDates(t *testing.T) {
	m, service := issueFixture(t)
	m.workspace.worklogs = service
	m.issueMarks = map[string]bool{"SBAH-1": true}
	m.width, m.height = 100, 28
	m.issueHistory = nil
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	m = next.(model)
	if m.issueHistory == nil || !m.issueHistory.loading || m.overlay != noOverlay || m.focus != focusWorkspace {
		t.Fatal("selection did not start an inline preview with list focus")
	}
	m = resolveHistory(t, m, cmd)
	if len(m.issueHistory.items) != 2 || m.issueHistory.more {
		t.Fatalf("history omitted all-time worklogs: %+v", m.issueHistory.items)
	}
	view := ansi.Strip(m.render())
	for _, text := range []string{"Lifecycle checks", "Data lifecycle", "2026-10-12 09:00", "2026-10-11 09:00", "10h", "Tab Inspector"} {
		if !strings.Contains(view, text) {
			t.Fatalf("inline history missing %q:\n%s", text, view)
		}
	}
	captureBulkState(t, m, "inline-history-real-100x28")
	m = sendIssueKey(m, "enter")
	if m.overlay != noOverlay || !strings.Contains(m.renderIssueMetadata(), "Document management") {
		t.Fatal("Enter opened an overlay or lost inline metadata")
	}
	for _, action := range m.commandActions() {
		if action.key == "enter" {
			t.Fatal("removed details action remains discoverable")
		}
	}
	if m.selectedIssueKey() != "SBAH-2" || !m.issueMarks["SBAH-1"] || len(m.issueHistory.items) != 2 {
		t.Fatal("metadata details lost inline history or list context")
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

func TestInlineHistoryPagingFocusAndLayouts(t *testing.T) {
	m := historyModel(t, 63)
	m = resolveHistory(t, m, m.syncIssueHistory())
	if len(m.issueHistory.items) != issueDetailPageSize || !m.issueHistory.more {
		t.Fatal("initial query is not bounded")
	}
	m = sendIssueKey(sendIssueKey(m, "tab"), "tab")
	if m.focus != focusIssueHistory {
		t.Fatal("Tab did not focus history")
	}
	next, cmd := m.Update(actionKey("pgdown"))
	m = resolveHistory(t, next.(model), cmd)
	if m.issueHistory.history.YOffset() == 0 || m.selectedIssueKey() != "APP-1" {
		t.Fatal("history paging moved issue selection or failed to scroll")
	}
	next, cmd = m.Update(actionKey("end"))
	m = resolveHistory(t, next.(model), cmd)
	if len(m.issueHistory.items) != 63 || m.issueHistory.more || !strings.Contains(ansi.Strip(m.render()), "Entry 62") {
		t.Fatal("End did not reach all remaining history pages")
	}
	m = sendIssueKey(m, "shift+tab")
	if m.focus != focusIssueMetadata {
		t.Fatal("Shift+Tab did not focus metadata")
	}
	historyOffset := m.issueHistory.history.YOffset()
	m = sendIssueKey(m, "end")
	if m.issueHistory.history.YOffset() != historyOffset || !m.issueHistory.metadata.AtBottom() {
		t.Fatal("metadata navigation moved history")
	}
	m = sendIssueKey(m, "esc")
	if m.focus != focusWorkspace {
		t.Fatal("Esc did not return to list")
	}
	offset := m.issueHistory.history.YOffset()
	m = sendIssueKey(m, "home")
	if m.issueHistory.history.YOffset() != offset {
		t.Fatal("list navigation scrolled history")
	}
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "tab"), "tab"), "home")
	if m.issueHistory.history.YOffset() != 0 || !strings.Contains(ansi.Strip(m.render()), "Entry 00") {
		t.Fatal("history Home did not reach first worklog")
	}
	for _, size := range [][2]int{{100, 28}, {140, 40}} {
		for _, mode := range []string{"dark", "light", "no-color"} {
			m.deps.noColor = mode == "no-color"
			m.colorReady = true
			m.colors = m.deps.theme.Dark
			if mode == "light" {
				m.colors = m.deps.theme.Light
			}
			for _, drawer := range []bool{false, true} {
				m.activityOpen = drawer
				next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = next.(model)
				captureBulkState(t, m, fmt.Sprintf("inline-history-%s-%dx%d-%t", mode, size[0], size[1], drawer))
			}
		}
	}
}

func TestInlineHistoryRejectsRequestsAfterSelectionRefreshAndTabChanges(t *testing.T) {
	m := historyModel(t, 60)
	firstCmd := m.syncIssueHistory()
	firstMsg := firstCmd()
	next, unusedCmd := m.Update(actionKey("down"))
	m = next.(model)
	next, _ = m.Update(firstMsg)
	m = next.(model)
	if m.issueHistory.issueKey != "APP-2" || len(m.issueHistory.items) != 0 {
		t.Fatal("old result populated a different issue")
	}
	m = resolveHistory(t, m, unusedCmd)
	if !strings.Contains(ansi.Strip(m.render()), "No worklogs for this") {
		t.Fatal("unused issue missing empty state")
	}
	next, cmd := m.Update(actionKey("up"))
	m = next.(model)
	next, _ = m.Update(firstMsg)
	m = next.(model)
	if len(m.issueHistory.items) != 0 {
		t.Fatal("reselection accepted stale generation")
	}
	m = resolveHistory(t, m, cmd)
	m = sendIssueKey(sendIssueKey(m, "tab"), "tab")
	next, pageCmd := m.Update(actionKey("end"))
	m = next.(model)
	oldPage := pageCmd()
	reload := m.reloadIssueHistory()
	next, _ = m.Update(oldPage)
	m = next.(model)
	if len(m.issueHistory.items) != 0 || !m.issueHistory.loading {
		t.Fatal("refresh accepted old page")
	}
	m = resolveHistory(t, m, reload)
	m = sendIssueKey(m, "2")
	next, _ = m.Update(oldPage)
	m = next.(model)
	if m.issueHistory != nil {
		t.Fatal("late result restored preview in another tab")
	}
}

func TestInlineHistoryFailureRetryAndExternalChange(t *testing.T) {
	m := historyModel(t, 30)
	state := worklogState(m.workspace)
	state.issueDetailErr = errors.New("history unavailable")
	m = resolveHistory(t, m, m.syncIssueHistory())
	if m.issueHistory.err != "history unavailable" || !strings.Contains(ansi.Strip(m.render()), "Worklogs unavailable") {
		t.Fatal("history failure hidden")
	}
	state.issueDetailErr = nil
	m = resolveHistory(t, m, m.reloadIssueHistory())
	state.issueDetailErr = errors.New("next page unavailable")
	m = sendIssueKey(sendIssueKey(m, "tab"), "tab")
	next, cmd := m.Update(actionKey("end"))
	m = resolveHistory(t, next.(model), cmd)
	if len(m.issueHistory.items) != 25 || m.issueHistory.err == "" || m.issueHistory.seekEnd {
		t.Fatal("page failure lost existing history")
	}
	state.issueDetailErr = nil
	generation := m.issueHistory.generation
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	if m.issueHistory.generation <= generation || !m.issueHistory.loading || len(m.issueHistory.items) != 0 {
		t.Fatal("external change did not restart history")
	}
	m.issueHistory.loading = false
	m = resolveHistory(t, m, m.loadIssueHistoryPage())
	updated := slices.Clone(m.issues)
	updated[0].LoggedSeconds = 36000
	next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, items: updated})
	m = next.(model)
	if !strings.Contains(m.renderIssueInspector(26, 24), "Logged    10h") {
		t.Fatal("metadata budget not refreshed")
	}
}

func TestInlineHistoryHelpPreservesScrollWithActivityOpen(t *testing.T) {
	m := historyModel(t, 63)
	m.activityOpen = true
	m = resolveHistory(t, m, m.syncIssueHistory())
	m = sendIssueKey(sendIssueKey(m, "tab"), "tab")
	next, cmd := m.Update(actionKey("end"))
	m = resolveHistory(t, next.(model), cmd)
	offset, height := m.issueHistory.history.YOffset(), m.issueHistory.history.Height()
	m = sendIssueKey(m, "?")
	m.resizeIssueHistory() // An asynchronous refresh or theme update may resize behind help.
	m = sendIssueKey(m, "esc")
	if m.issueHistory.history.YOffset() != offset || m.issueHistory.history.Height() != height || m.focus != focusIssueHistory {
		t.Fatal("help changed preview scroll, geometry, or focus")
	}
	if !strings.Contains(ansi.Strip(m.render()), "Entry 62") {
		t.Fatal("help hid the last worklog")
	}
}

func TestIssueInspectorRetainsFullMetadataAndVisibleWorklogs(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {140, 40}} {
		for _, drawer := range []bool{false, true} {
			for _, theme := range []string{"dark", "light", "no-color"} {
				t.Run(fmt.Sprintf("%dx%d-activity-%t-%s", size[0], size[1], drawer, theme), func(t *testing.T) {
					m := historyModel(t, 2)
					item := &m.issues[0]
					item.Title = strings.Repeat("Full issue title ", 8) + "full-title-tail"
					item.ManualTitle = item.Title
					item.CachedTitle = strings.Repeat("Cached title ", 6) + "cached-title-tail"
					manual, cached := int64(36000), int64(28800)
					item.ManualMaxEstimateSeconds, item.CachedMaxEstimateSeconds = &manual, &cached
					item.EstimateSource = "manual"
					item.SourceAdapterFamily, item.SourceAdapterInst = "jira-cloud", "ito_jira"
					item.CreatedAt = time.Date(2026, 10, 8, 8, 26, 0, 0, time.UTC)
					item.RefreshedAt = time.Date(2026, 10, 13, 16, 28, 0, 0, time.UTC)
					m.activityOpen, m.colorReady, m.deps.noColor = drawer, true, theme == "no-color"
					if theme == "light" {
						m.colors = m.deps.theme.Light
					}
					next, cmd := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m = resolveHistory(t, next.(model), cmd)
					for _, text := range []string{"Logged", "7h30m", "10h", "2h30m", "Worklogs", "Entry 00"} {
						if !strings.Contains(ansi.Strip(m.render()), text) {
							t.Fatalf("initial inspector lost %q", text)
						}
					}
					m = sendIssueKey(m, "enter")
					if m.overlay != noOverlay {
						t.Fatal("Enter opened removed details view")
					}
					m = sendIssueKey(m, "tab")
					if m.focus != focusIssueMetadata {
						t.Fatal("Tab did not focus metadata")
					}
					rows := map[int]string{}
					for page := 0; page < 100; page++ {
						for index, line := range strings.Split(ansi.Strip(m.issueHistory.metadata.View()), "\n") {
							rows[m.issueHistory.metadata.YOffset()+index] = line
						}
						assertRedesignBounds(t, m, "complete issue inspector")
						if m.issueHistory.metadata.AtBottom() {
							break
						}
						m = sendIssueKey(m, "pgdown")
					}
					var content strings.Builder
					for row := 0; row < len(rows); row++ {
						content.WriteString(rows[row])
					}
					text := strings.Join(strings.Fields(content.String()), "")
					for _, value := range []string{"full-title-tail", "Created", "2026-10-08", "08:26", "Jira cache", "jira-cloud/ito_jira", "cached-title-tail", "8h", "manual override active", "Refreshed", "2026-10-13", "16:28", "Epic", "A local epic", "Epic effort", "2h30m left"} {
						if !strings.Contains(text, strings.Join(strings.Fields(value), "")) {
							t.Fatalf("metadata lost %q: %s", value, text)
						}
					}
					if m.issueHistory.history.YOffset() != 0 || m.selectedIssueKey() != "APP-1" {
						t.Fatal("metadata scrolling moved history or list")
					}
					captureBulkState(t, m, fmt.Sprintf("complete-inspector-%dx%d-%t-%s", size[0], size[1], drawer, theme))
					m = sendIssueKey(m, "tab")
					if m.focus != focusIssueHistory {
						t.Fatal("second Tab did not focus worklogs")
					}
					metadataOffset := m.issueHistory.metadata.YOffset()
					m = sendIssueKey(m, "down")
					if m.issueHistory.metadata.YOffset() != metadataOffset {
						t.Fatal("history scrolling moved metadata")
					}
					if m.deps.noColor && strings.Contains(m.render(), "\x1b[") {
						t.Fatal("NO_COLOR emitted ANSI")
					}
				})
			}
		}
	}
}
