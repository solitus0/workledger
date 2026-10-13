package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

func navigationModel(t *testing.T) model {
	t.Helper()
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height, m.activeTab, m.issuesLoading = 100, 28, issuesTab, false
	return m
}

func sendIssueKey(m model, key string) model {
	next, _ := m.Update(actionKey(key))
	return next.(model)
}

func budgetIssue(key, title, epic string, estimate, logged int64, known bool) worklogs.IssueMetadata {
	item := worklogs.IssueMetadata{IssueKey: key, Title: title, Epic: epic, LoggedSeconds: logged}
	if known {
		remaining := max(0, estimate-logged)
		item.MaxEstimateSeconds, item.RemainingSeconds = &estimate, &remaining
		item.OverrunSeconds = max(0, logged-estimate)
	}
	return item
}

func orderedIssueKeys(m model) []string {
	keys := []string{}
	for _, index := range m.visibleIssues() {
		keys = append(keys, m.issues[index].IssueKey)
	}
	return keys
}

func TestIssueSortMenuOrderingSelectionAndCancel(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{
		budgetIssue("APP-10", "Zebra", "alpha", 100, 0, true),
		budgetIssue("APP-2", "alpha", "alpha", 100, 0, true),
		budgetIssue("APP-3", "", "Beta", 50, 0, true),
		budgetIssue("APP-4", "Beta", "Gamma", 1, 2, true),
		budgetIssue("APP-5", "ALPHA", "", 0, 0, false),
		budgetIssue("APP-1", "", "Unknown", 0, 0, false),
	}
	m.restoreIssueSelection("APP-3")
	original := slices.Clone(m.issues)
	summary := worklogs.SummarizeIssues(m.issues)
	for _, test := range []struct {
		mode issueSortMode
		want []string
	}{
		{sortIssueKey, []string{"APP-1", "APP-2", "APP-3", "APP-4", "APP-5", "APP-10"}},
		{sortRemaining, []string{"APP-2", "APP-10", "APP-3", "APP-4", "APP-1", "APP-5"}},
		{sortOverrun, []string{"APP-4", "APP-2", "APP-3", "APP-10", "APP-1", "APP-5"}},
		{sortTitle, []string{"APP-2", "APP-5", "APP-4", "APP-10", "APP-1", "APP-3"}},
	} {
		m = sendIssueKey(m, "s")
		if m.overlay != issueSortOverlay || !strings.Contains(ansi.Strip(m.View().Content), "Sort issues") {
			t.Fatal("sort menu did not render")
		}
		assertRedesignBounds(t, m, "issue sort menu")
		m = sendIssueKey(m, "home")
		for range slices.Index(m.issueSortOptions(), test.mode) {
			m = sendIssueKey(m, "down")
		}
		m = sendIssueKey(m, "enter")
		if got := orderedIssueKeys(m); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("sort %v: %v, want %v", test.mode, got, test.want)
		}
		if m.selectedIssueKey() != "APP-3" || m.overlay != noOverlay || !strings.Contains(ansi.Strip(m.render()), "Sort: "+test.mode.label(issueListView)) {
			t.Fatal("sort lost selection or its visible label")
		}
	}
	if !reflect.DeepEqual(m.issues, original) || worklogs.SummarizeIssues(m.issues) != summary {
		t.Fatal("sorting changed catalog data or budget totals")
	}
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "home"), "esc")
	if m.issueSort != sortTitle || m.selectedIssueKey() != "APP-3" {
		t.Fatal("cancel applied a pending sort or changed selection")
	}
	updated := slices.Clone(m.issues)
	updated[2] = budgetIssue("APP-3", "Updated", "Beta", 500, 0, true)
	slices.Reverse(updated)
	next, _ := m.Update(issueListResultMsg{generation: m.issuesGen, items: updated})
	m = next.(model)
	if m.selectedIssueKey() != "APP-3" || m.issueSort != sortTitle {
		t.Fatal("refresh lost selected identity or sort preference")
	}
	m = sendIssueKey(sendIssueKey(sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "home"), "down"), "down"), "enter")
	m = sendIssueKey(m, "home")
	if m.selectedIssueKey() != "APP-3" {
		t.Fatal("Home did not follow the updated remaining sort")
	}
	m = sendIssueKey(m, "end")
	if m.selectedIssueKey() != "APP-5" {
		t.Fatal("End did not follow the sorted missing-value tail")
	}
	for _, key := range []string{"s", "pgup", "pgdown", "home", "end"} {
		found := false
		for _, action := range m.commandActions() {
			found = found || action.key == key
		}
		if !found {
			t.Fatalf("Actions omits %s", key)
		}
	}
}

func TestEpicSortRetainsNameAndIndependentMemberSort(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{
		budgetIssue("APP-1", "one", "Alpha", 20, 0, true),
		budgetIssue("APP-2", "two", "Beta", 30, 0, true),
		budgetIssue("APP-3", "three", "Gamma", 10, 20, true),
		budgetIssue("APP-4", "four", "Unknown", 0, 0, false),
		budgetIssue("APP-5", "five", "", 0, 0, false),
	}
	m.issueSort = sortTitle
	m.restoreIssueSelection("APP-2")
	m = sendIssueKey(m, "t")
	m.restoreEpicSelection("Beta", true)
	for _, test := range []struct {
		mode issueSortMode
		want []string
	}{
		{sortIssueKey, []string{"Alpha", "Beta", "Gamma", "Unknown", ""}},
		{sortRemaining, []string{"Beta", "Alpha", "Gamma", "Unknown", ""}},
		{sortOverrun, []string{"Gamma", "Alpha", "Beta", "Unknown", ""}},
	} {
		m = sendIssueKey(sendIssueKey(m, "s"), "home")
		for range slices.Index(m.issueSortOptions(), test.mode) {
			m = sendIssueKey(m, "down")
		}
		m = sendIssueKey(m, "enter")
		groups := m.visibleEpics()
		names := []string{}
		for _, group := range groups {
			names = append(names, group.Epic)
		}
		if !reflect.DeepEqual(names, test.want) || m.selectedEpic().Epic != "Beta" || m.issueSort != sortTitle {
			t.Fatalf("epic sort %v lost ordering/name/member preference: %v", test.mode, names)
		}
	}
	m = sendIssueKey(m, "enter")
	if m.issueView != issueListView || m.issueSort != sortTitle || m.selectedIssueKey() != "APP-2" {
		t.Fatal("drilldown lost member sort or selection")
	}
	m = sendIssueKey(m, "esc")
	if m.epicSort != sortOverrun || m.selectedEpic().Epic != "Beta" {
		t.Fatal("return lost epic sort or selection")
	}
	updated := slices.Clone(m.issues)
	slices.Reverse(updated)
	next, _ := m.Update(issueListResultMsg{generation: m.issuesGen, items: updated})
	m = next.(model)
	if m.selectedEpic().Epic != "Beta" || m.epicSort != sortOverrun {
		t.Fatal("refresh lost selected epic name or sort")
	}
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "home"), "esc")
	if m.epicSort != sortOverrun || m.selectedEpic().Epic != "Beta" {
		t.Fatal("cancel applied a pending epic sort")
	}
}

// Inspect rendered list cells rather than duplicating the page-capacity formula.
func renderedListNames(m model, pattern string) []string {
	re := regexp.MustCompile(pattern)
	names := []string{}
	for _, line := range strings.Split(ansi.Strip(m.render()), "\n") {
		panes := strings.Split(line, "│")
		if len(panes) > 1 {
			if name := re.FindString(panes[1]); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func TestIssuePagingFollowsRenderedFilteredViewport(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {120, 36}} {
		for _, drawer := range []bool{false, true} {
			for _, filtered := range []bool{false, true} {
				m := navigationModel(t)
				m.width, m.height, m.activityOpen = size[0], size[1], drawer
				for n := 1; n <= 100; n++ {
					title := "Other"
					if n%2 == 1 {
						title = "Match"
					}
					logged := int64(0)
					if n%2 == 0 {
						logged = 9100000000
					}
					m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n), title, "Documents", 9000000000, logged, n%5 != 0))
				}
				m.restoreIssueSelection("APP-1")
				if filtered {
					m = sendIssueKey(m, "/")
					m.issueSearch.input.SetValue("Match")
					epic := "Documents"
					m.issueEpic = &epic
					m.restoreIssueSelection("APP-1")
				}
				visible := renderedListNames(m, `APP-\d+`)
				if len(visible) < 2 {
					t.Fatal("test viewport needs multiple visible issues")
				}
				m = sendIssueKey(m, "pgdown")
				if m.selectedIssueKey() != visible[len(visible)-1] {
					t.Fatalf("size=%v activity=%t filter=%t: page selected %s, wanted last visible %s", size, drawer, filtered, m.selectedIssueKey(), visible[len(visible)-1])
				}
				if filtered && m.issueSearch.input.Value() != "Match" {
					t.Fatal("page key changed search input")
				}
				m = sendIssueKey(m, "pgup")
				if m.selectedIssueKey() != "APP-1" {
					t.Fatal("previous page did not return to the first issue")
				}
				if filtered {
					m = sendIssueKey(m, "enter")
				}
				last := "APP-100"
				if filtered {
					last = "APP-99"
				}
				m = sendIssueKey(sendIssueKey(m, "end"), "pgdown")
				if m.selectedIssueKey() != last || !slices.Contains(renderedListNames(m, `APP-\d+`), last) {
					t.Fatal("End/page clamp did not keep the last matching issue visible")
				}
				m = sendIssueKey(sendIssueKey(m, "home"), "pgup")
				if m.selectedIssueKey() != "APP-1" {
					t.Fatal("Home/page clamp did not retain the first matching issue")
				}
				assertRedesignBounds(t, m, "paged issues")
			}
		}
	}
}

func TestEpicPagingAccountsForWrappedGroupRows(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {120, 36}} {
		for _, drawer := range []bool{false, true} {
			m := navigationModel(t)
			m.width, m.height, m.activityOpen, m.issueView = size[0], size[1], drawer, epicListView
			for n := 0; n < 40; n++ {
				name := fmt.Sprintf("Milestone %02d", n)
				m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n*3+1), "Task", name, 3600, 0, true))
				if n%2 == 1 {
					m.issues = append(m.issues,
						budgetIssue(fmt.Sprintf("APP-%d", n*3+2), "Overrun", name, 9000000000000000, 9100000000000000, true),
						budgetIssue(fmt.Sprintf("APP-%d", n*3+3), "Unknown", name, 0, 0, false))
				}
			}
			m.restoreEpicSelection("Milestone 00", true)
			for _, filtered := range []bool{false, true} {
				if filtered {
					m = sendIssueKey(m, "/")
					m.epicSearch.input.SetValue("Milestone 0")
				}
				visible := renderedListNames(m, `Milestone \d+`)
				m = sendIssueKey(m, "pgdown")
				if m.selectedEpic().Epic != visible[len(visible)-1] {
					t.Fatalf("size=%v activity=%t filter=%t: group page %s != %s", size, drawer, filtered, m.selectedEpic().Epic, visible[len(visible)-1])
				}
				m = sendIssueKey(m, "pgup")
				if m.selectedEpic().Epic != "Milestone 00" {
					t.Fatal("group page up did not account for the same row heights")
				}
				if filtered {
					m = sendIssueKey(m, "enter")
				}
				last := "Milestone 39"
				if filtered {
					last = "Milestone 09"
				}
				m = sendIssueKey(sendIssueKey(m, "end"), "pgdown")
				if m.selectedEpic().Epic != last || !slices.Contains(renderedListNames(m, `Milestone \d+`), last) {
					t.Fatal("last filtered group was not selected and visible")
				}
				m = sendIssueKey(sendIssueKey(m, "home"), "pgup")
				assertRedesignBounds(t, m, "paged epics")
			}
		}
	}
}

func TestIssueNavigationEmptyListsAndInputFocus(t *testing.T) {
	m := navigationModel(t)
	for _, view := range []issueView{issueListView, epicListView} {
		m.issueView = view
		for _, key := range []string{"pgup", "pgdown", "home", "end", "s", "enter", "esc"} {
			m = sendIssueKey(m, key)
			assertRedesignBounds(t, m, "empty navigation")
		}
	}
	m, _ = issueFixture(t)
	m = sendIssueKey(m, "/")
	m.issueSearch.input.SetValue("lifecycle")
	m = sendIssueKey(m, "home")
	if m.issueSearch.input.Position() != 0 {
		t.Fatal("Home should move the search cursor while editing text")
	}
	m = sendIssueKey(m, "end")
	if m.issueSearch.input.Position() != len("lifecycle") {
		t.Fatal("End should move the search cursor while editing text")
	}
	m = sendIssueKey(sendIssueKey(m, "esc"), "enter")
	selected := m.selectedIssueKey()
	if m.overlay != detailsOverlay {
		t.Fatal("details did not open")
	}
	m = sendIssueKey(sendIssueKey(m, "end"), "pgup")
	if m.selectedIssueKey() != selected || m.overlay != detailsOverlay {
		t.Fatal("detail paging changed the list selection")
	}
}

func TestSortMenusAndPagingAfterResizeAcrossThemes(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, palette := range []Palette{DefaultTheme().Light, DefaultTheme().Dark} {
			m := navigationModel(t)
			m.deps.noColor, m.colors, m.colorReady = noColor, palette, true
			for n := 1; n <= 80; n++ {
				m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n), "Task", fmt.Sprintf("Epic %02d", n), int64(n*60), 0, true))
			}
			for _, view := range []issueView{issueListView, epicListView} {
				m.issueView = view
				m.restoreIssueSelection("APP-1")
				m.restoreEpicSelection("Epic 01", true)
				for _, size := range [][2]int{{120, 36}, {100, 28}} {
					next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m = next.(model)
					for _, drawer := range []bool{false, true} {
						m.activityOpen = drawer
						m = sendIssueKey(m, "home")
						pattern := `APP-\d+`
						if view == epicListView {
							pattern = `Epic \d+`
						}
						visible := renderedListNames(m, pattern)
						m = sendIssueKey(m, "pgdown")
						selected := m.selectedIssueKey()
						if view == epicListView {
							selected = m.selectedEpic().Epic
						}
						if selected != visible[len(visible)-1] {
							t.Fatalf("resize changed rendered page size: %s != %s", selected, visible[len(visible)-1])
						}
						m = sendIssueKey(m, "s")
						assertRedesignBounds(t, m, "sort menu themes/resize/Activity")
						if m.confirmationOverlayOpen() || m.activityVisible() || !strings.Contains(ansi.Strip(m.renderFooter(m.width)), "Apply") {
							t.Fatal("sort menu inherited destructive confirmation or Activity behavior")
						}
						m = sendIssueKey(m, "esc")
						footer := ansi.Strip(m.renderFooter(m.width))
						if !strings.Contains(footer, "PgUp/Dn") || !strings.Contains(footer, "s Sort") {
							t.Fatalf("navigation hints missing: %s", footer)
						}
						if noColor && strings.Contains(m.render(), "\x1b[") {
							t.Fatal("NO_COLOR emitted ANSI")
						}
					}
				}
			}
		}
	}
}

func TestCreatedAtIsDefaultAndPreservesSelectionAcrossSortAndRefresh(t *testing.T) {
	m := navigationModel(t)
	if m.issueSort != sortCreatedAt || m.epicSort != sortIssueKey {
		t.Fatal("created-at issue default changed epic-name default")
	}
	older := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	items := []worklogs.IssueMetadata{
		budgetIssue("APP-1", "Oldest", "October", 100, 0, true),
		budgetIssue("APP-10", "Same creation", "October", 100, 0, true),
		budgetIssue("APP-2", "Newest", "October", 100, 0, true),
		budgetIssue("APP-3", "Unknown", "October", 100, 0, true),
	}
	items[0].CreatedAt = older
	items[1].CreatedAt = newer
	items[2].CreatedAt = newer
	// A later metadata edit must never substitute for creation or move APP-1 first.
	items[0].UpdatedAt = newer.Add(48 * time.Hour)
	next, _ := m.Update(issueListResultMsg{generation: m.issuesGen, items: items})
	m = next.(model)
	want := []string{"APP-2", "APP-10", "APP-1", "APP-3"}
	if !reflect.DeepEqual(orderedIssueKeys(m), want) || m.selectedIssueKey() != "APP-2" {
		t.Fatalf("initial default order/focus: %v / %s", orderedIssueKeys(m), m.selectedIssueKey())
	}
	if !strings.Contains(ansi.Strip(m.render()), "Sort: Created at ↓") {
		t.Fatal("default creation sort not visible")
	}
	m.restoreIssueSelection("APP-10")
	m.issueMarks = map[string]bool{"APP-1": true}
	m = sendIssueKey(m, "s")
	if m.sortSelected != 0 || !strings.Contains(ansi.Strip(m.render()), "Created at ↓ (current)") {
		t.Fatal("default missing from top of sort menu")
	}
	m = sendIssueKey(sendIssueKey(m, "down"), "enter")
	if m.issueSort != sortIssueKey || m.selectedIssueKey() != "APP-10" || !m.issueMarks["APP-1"] {
		t.Fatal("explicit key sort lost focus/marks")
	}
	m = sendIssueKey(sendIssueKey(sendIssueKey(m, "s"), "home"), "enter")
	items = slices.Clone(items)
	slices.Reverse(items)
	next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, items: items})
	m = next.(model)
	if m.issueSort != sortCreatedAt || !reflect.DeepEqual(orderedIssueKeys(m), want) || m.selectedIssueKey() != "APP-10" || !m.issueMarks["APP-1"] {
		t.Fatal("creation sort refresh changed order/focus/marks")
	}
	m = sendIssueKey(m, "t")
	m.restoreEpicSelection("October", true)
	m = sendIssueKey(m, "enter")
	if m.issueSort != sortCreatedAt || !reflect.DeepEqual(orderedIssueKeys(m), want) || m.selectedIssueKey() != "APP-10" {
		t.Fatal("group drilldown discarded creation order or selected issue")
	}
	captureBulkState(t, m, "created-default-issues")
}
