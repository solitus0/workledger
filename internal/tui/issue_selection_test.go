package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestCopyMarkedIssueKeys(t *testing.T) {
	for _, test := range []struct {
		name   string
		epic   bool
		notice string
	}{
		{"shortcut with all marks filtered out", false, "Copied 2 issue keys (2 outside current view)"},
		{"Actions from epic members", true, "Copied 2 issue keys (1 outside current view)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := navigationModel(t)
			m.issues = []worklogs.IssueMetadata{
				{IssueKey: "APP-10", Epic: "Other"},
				{IssueKey: "APP-2", Epic: "Onboarding"},
				{IssueKey: "APP-3", Epic: "Onboarding"},
			}
			m.issueMarks = map[string]bool{"APP-10": true, "APP-2": true}
			m.restoreIssueSelection("APP-2")
			key := "y"
			if test.epic {
				m = sendIssueKey(m, "t")
				m.restoreEpicSelection("Onboarding", true)
				m = sendIssueKey(m, "enter")
				m = sendIssueKey(m, "v")
			} else {
				m = sendIssueKey(m, "/")
				m = sendIssueKey(m, "no matching issue")
				m = sendIssueKey(m, "enter")
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "y Copy keys") {
				t.Fatal("copy hint missing at 100x28")
			}
			if test.epic && (!strings.Contains(view, "Tab Inspector") || !strings.Contains(view, "s Sort")) {
				t.Fatal("copy hint displaced inspector or sort shortcuts at 100x28")
			}
			if test.epic {
				m = sendIssueKey(m, "?")
				m = sendIssueKey(m, "Copy selected issue keys")
				key = "enter"
			}
			var copied string
			m.deps.writeClipboard = func(_ context.Context, text string) error {
				copied = text
				return nil
			}
			focusedKey, activeRange, filter := m.selectedIssueKey(), m.issueRange, m.issueSearch
			next, cmd := m.Update(actionKey(key))
			if cmd == nil || copied != "" {
				t.Fatal("copy should schedule an asynchronous write")
			}
			next, _ = next.Update(cmd())
			m = next.(model)
			if copied != "APP-2\nAPP-10" {
				t.Fatalf("clipboard = %q", copied)
			}
			if m.overlay != noOverlay || m.actions != nil || !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-2", "APP-10"}) ||
				m.selectedIssueKey() != focusedKey || m.issueRange != activeRange || m.issueSearch != filter {
				t.Fatal("copy changed the selection, range, filter, cursor, or overlay")
			}
			if m.noticeKind != noticeSuccess || !strings.Contains(ansi.Strip(m.View().Content), test.notice) {
				t.Fatalf("missing success notice: %q", m.notice)
			}
			assertRedesignBounds(t, m, test.name)
			m = sendIssueKey(m, "down")
			if m.notice != "" || !strings.Contains(ansi.Strip(m.View().Content), "y Copy keys") {
				t.Fatal("copy notice masked the action bar after navigation")
			}
		})
	}
}

func TestCopyIssueKeysFailureAllowsRetry(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{{IssueKey: "APP-2"}}
	m.restoreIssueSelection("APP-2")
	m = sendIssueKey(m, "v")
	activeRange := m.issueRange
	writes := 0
	m.deps.writeClipboard = func(ctx context.Context, _ string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("clipboard write has no deadline")
		}
		writes++
		if writes == 1 {
			return errors.New("clipboard unavailable")
		}
		return nil
	}
	next, cmd := m.Update(actionKey("y"))
	if cmd == nil {
		t.Fatal("copy did not start")
	}
	next, duplicate := next.Update(actionKey("y"))
	if duplicate != nil {
		t.Fatal("a second copy started while a write was pending")
	}
	next, _ = next.Update(cmd())
	m = next.(model)
	if m.noticeKind != noticeError || !strings.Contains(ansi.Strip(m.View().Content), "Could not copy issue keys. clipboard unavailable") ||
		!m.issueMarks["APP-2"] || m.selectedIssueKey() != "APP-2" || m.issueRange != activeRange {
		t.Fatalf("copy failure changed selection or did not show the error: %q", m.notice)
	}
	next, cmd = m.Update(actionKey("y"))
	if cmd == nil {
		t.Fatal("copy retry did not start")
	}
	next, _ = next.Update(cmd())
	m = next.(model)
	if writes != 2 || m.noticeKind != noticeSuccess || m.notice != "Copied 1 issue key" {
		t.Fatalf("retry wrote %d times and reported %q", writes, m.notice)
	}
}

func TestCopyIssueKeysIgnoresResultAfterNavigation(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{{IssueKey: "APP-2"}}
	m.restoreIssueSelection("APP-2")
	m = sendIssueKey(m, "space")
	m.deps.writeClipboard = func(context.Context, string) error { return nil }
	next, cmd := m.Update(actionKey("y"))
	if cmd == nil {
		t.Fatal("copy did not start")
	}
	m = sendIssueKey(next.(model), "1")
	m = sendIssueKey(m, "5")
	m.setNotice(noticeWarning, "New workspace feedback")
	next, _ = m.Update(cmd())
	m = next.(model)
	if !m.canCopyIssueKeys() || m.notice != "New workspace feedback" {
		t.Fatalf("late result left the copy busy or changed the current notice to %q", m.notice)
	}
	m = sendIssueKey(m, "down")
	if m.notice != "New workspace feedback" {
		t.Fatal("clipboard notice cleanup cleared unrelated feedback")
	}
}

func TestIgnoredMouseClicksPreservePendingAndVisibleCopyFeedback(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{{IssueKey: "APP-2"}}
	m.restoreIssueSelection("APP-2")
	m = sendIssueKey(m, "space")
	m.deps.writeClipboard = func(context.Context, string) error { return nil }
	next, cmd := m.Update(actionKey("y"))
	if cmd == nil {
		t.Fatal("copy did not start")
	}
	next, _ = next.Update(mouseClick(25, 8, tea.MouseRight))
	next, _ = next.Update(cmd())
	m = next.(model)
	if m.notice != "Copied 1 issue key" {
		t.Fatalf("right-click suppressed the pending result: %q", m.notice)
	}
	next, _ = m.Update(mouseClick(25, 8, tea.MouseRight))
	next, _ = next.Update(mouseClick(-1, -1, tea.MouseLeft))
	m = next.(model)
	if m.notice != "Copied 1 issue key" {
		t.Fatalf("ignored click cleared visible feedback: %q", m.notice)
	}
	if m = sendIssueKey(m, "down"); m.notice != "" {
		t.Fatalf("navigation retained copy feedback: %q", m.notice)
	}
}

func TestCopyIssueKeysOnlyAvailableInMarkedList(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{{IssueKey: "APP-2", Epic: "Onboarding"}}
	m.restoreIssueSelection("APP-2")
	assertUnavailable := func(m model, context string) {
		t.Helper()
		if slices.ContainsFunc(m.commandActions(), func(action commandAction) bool { return action.key == "y" }) {
			t.Fatalf("copy action available in %s", context)
		}
		if m.issueCopying {
			t.Fatalf("copy started in %s", context)
		}
	}
	m = sendIssueKey(m, "y")
	assertUnavailable(m, "unmarked issue list")
	m.issueMarks = map[string]bool{"APP-2": true}
	m = sendIssueKey(m, "t")
	m = sendIssueKey(m, "y")
	assertUnavailable(m, "epic overview")
	m = sendIssueKey(m, "t")
	m = sendIssueKey(m, "/")
	m = sendIssueKey(m, "y")
	assertUnavailable(m, "search editing")
	if m.issueSearch.input.Value() != "y" {
		t.Fatal("copy shortcut swallowed search text")
	}
}

func captureBulkState(t *testing.T, m model, name string) {
	t.Helper()
	assertRedesignBounds(t, m, name)
	if path := os.Getenv("WORKLEDGER_BULK_RENDER_DIR"); path != "" {
		if err := os.WriteFile(filepath.Join(path, name+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBulkEpicRepresentativeSlice(t *testing.T) {
	m, service := issueFixture(t)
	epic := "SBA-october-work"
	if err := service.ApplyIssueMetadata(t.Context(), []worklogs.IssueMetadataPatch{{IssueKey: "SBAH-11", Epic: &epic, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	items, err := service.SearchIssues(t.Context(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.issues = items
	m.width, m.height = 100, 28
	m.deps.noColor = false
	m.restoreIssueSelection("SBAH-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "space")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"SBAH-1", "SBAH-2"}) || m.selectedIssueKey() != "SBAH-11" {
		t.Fatal("space did not mark and advance")
	}
	captureBulkState(t, m, "06-selected")
	m = sendIssueKey(m, "b")
	m = sendIssueKey(m, "down")
	if m.overlay != epicAssignmentOverlay || m.epicAssignment == nil || !strings.Contains(ansi.Strip(m.render()), "Assign epic to 2 issues") {
		t.Fatal("picker not opened")
	}
	captureBulkState(t, m, "06-picker")
	m = sendIssueKey(m, "esc")
	if m.overlay != noOverlay || len(m.issueMarks) != 2 || m.selectedIssueKey() != "SBAH-11" {
		t.Fatal("cancel lost batch or focus")
	}
	captureBulkState(t, m, "06-cancel")
	m = sendIssueKey(m, "b")
	m = sendIssueKey(m, "down")
	// Existing destination is highlighted; commit the batch through the real SQLite service.
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	if cmd == nil || !m.epicAssignment.saving {
		t.Fatal("assignment not submitted")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.epicAssignment != nil || len(m.issueMarks) != 0 {
		t.Fatal("success retained submitted selection")
	}
	items, err = service.SearchIssues(t.Context(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Title != "Manual document ordering" || items[0].LoggedSeconds != 3600 || *items[0].MaxEstimateSeconds != 14400 || items[0].Epic != "SBA-october-work" || items[1].Epic != "SBA-october-work" {
		t.Fatalf("unrelated metadata changed: %+v", items[0])
	}
	captureBulkState(t, m, "06-applied")
}

func TestIssueVisualRangeShrinksAndUnselectsWithoutLosingPriorMarks(t *testing.T) {
	m := navigationModel(t)
	for n := 1; n <= 8; n++ {
		m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n), "", "", int64(n), 0, true))
	}
	m.issueMarks = map[string]bool{"APP-8": true}
	m.restoreIssueSelection("APP-2")
	m = sendIssueKey(m, "v")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "down")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-2", "APP-3", "APP-4", "APP-8"}) {
		t.Fatalf("range marks: %v", m.markedIssueKeys())
	}
	m = sendIssueKey(m, "up")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-2", "APP-3", "APP-8"}) {
		t.Fatal("shrinking left trail or removed prior mark")
	}
	m = sendIssueKey(m, "home")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-1", "APP-2", "APP-8"}) {
		t.Fatal("reversing range did not reset baseline")
	}
	m = sendIssueKey(m, "esc")
	if m.issueRange != nil || len(m.issueMarks) != 3 {
		t.Fatal("Esc should finish range first")
	}
	m = sendIssueKey(m, "V")
	m = sendIssueKey(m, "down")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-8"}) {
		t.Fatal("unset range did not remove marks")
	}
	m = sendIssueKey(m, "up")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"APP-2", "APP-8"}) {
		t.Fatal("shrinking unset range did not restore marks")
	}
	m = sendIssueKey(m, "V")
	m = sendIssueKey(m, "esc")
	if len(m.issueMarks) != 0 || m.issueRange != nil {
		t.Fatal("Esc should clear completed batch")
	}
}

func TestBulkSelectionSurvivesFilterSortViewResizeAndRefresh(t *testing.T) {
	m, _ := issueFixture(t)
	m.restoreIssueSelection("SBAH-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "/")
	m = sendIssueKey(m, "SBAH-2")
	m = sendIssueKey(m, "enter")
	m = sendIssueKey(m, "ctrl+a")
	if !reflect.DeepEqual(m.markedIssueKeys(), []string{"SBAH-1", "SBAH-2"}) || m.hiddenIssueMarks() != 1 {
		t.Fatalf("filtered select-all: %v", m.markedIssueKeys())
	}
	if !strings.Contains(ansi.Strip(m.render()), "1 outside current view") {
		t.Fatal("hidden count missing")
	}
	m = sendIssueKey(m, "s")
	m = sendIssueKey(m, "down")
	m = sendIssueKey(m, "enter")
	if m.selectedIssueKey() != "SBAH-2" || len(m.issueMarks) != 2 {
		t.Fatal("sort lost key/marks")
	}
	m = sendIssueKey(m, "t")
	m = sendIssueKey(m, "t")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	m = next.(model)
	items := slices.Clone(m.issues)
	slices.Reverse(items)
	next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, items: items})
	m = next.(model)
	if m.selectedIssueKey() != "SBAH-2" || len(m.issueMarks) != 2 || m.hiddenIssueMarks() != 1 {
		t.Fatal("view/resize/refresh lost identities")
	}
	m = sendIssueKey(m, "b")
	if m.epicAssignment.hidden != 1 || len(m.epicAssignment.keys) != 2 {
		t.Fatal("picker omitted hidden issue")
	}
	captureBulkState(t, m, "07-hidden-picker")
	m = sendIssueKey(m, "esc")
	m = sendIssueKey(m, "v")
	next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, items: []worklogs.IssueMetadata{items[1]}})
	m = next.(model)
	if m.issueRange != nil || len(m.issueMarks) != 1 || !strings.Contains(m.notice, "no longer available") {
		t.Fatal("refresh did not finish range/prune absent key with feedback")
	}
}

func TestVisualRangePagingUsesRenderedCapacity(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {120, 36}} {
		for _, drawer := range []bool{false, true} {
			m := navigationModel(t)
			m.width, m.height = size[0], size[1]
			m.activityOpen = drawer
			for n := 1; n <= 100; n++ {
				m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n), "Some issue", "Documents", 10000, 0, true))
			}
			m.restoreIssueSelection("APP-1")
			m = sendIssueKey(m, "v")
			visible := renderedListNames(m, `APP-\d+`)
			m = sendIssueKey(m, "pgdown")
			if m.selectedIssueKey() != visible[len(visible)-1] || len(m.issueMarks) != len(visible) {
				t.Fatalf("size %v activity %t: page range %s marks %d visible %v", size, drawer, m.selectedIssueKey(), len(m.issueMarks), visible)
			}
			m = sendIssueKey(m, "home")
			if len(m.issueMarks) != 1 {
				t.Fatal("Home did not shrink range")
			}
			m = sendIssueKey(m, "end")
			m = sendIssueKey(m, "v")
			if len(m.issueMarks) != 100 || m.issueRange != nil {
				t.Fatal("End range not complete")
			}
			captureBulkState(t, m, fmt.Sprintf("07-range-%dx%d-%t", size[0], size[1], drawer))
		}
	}
}

func TestBulkEpicNewGroupRemovalFailureAndStaleRecovery(t *testing.T) {
	m, service := issueFixture(t)
	m.restoreIssueSelection("SBAH-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "b")
	m = sendIssueKey(m, "October sprint")
	if options := m.epicDestinations(); len(options) != 1 || options[0].name != "October sprint" {
		t.Fatalf("new destination: %v", options)
	}
	originalCheck := m.deps.checkWritable
	m.deps.checkWritable = func(string, string) error { return errors.New("read only storage") }
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	next, duplicate := m.Update(actionKey("enter"))
	m = next.(model)
	if duplicate != nil {
		t.Fatal("duplicate save allowed")
	}
	m = sendIssueKey(m, "esc")
	if m.epicAssignment == nil {
		t.Fatal("cancelled in-flight write")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.epicAssignment.saving || m.epicAssignment.err != "read only storage" || m.epicAssignment.input.Value() != "October sprint" || len(m.issueMarks) != 2 {
		t.Fatal("failure lost destination/batch")
	}
	captureBulkState(t, m, "07-failed")
	items, _ := service.SearchIssues(t.Context(), "", "", "")
	if items[0].Epic != "Document management" {
		t.Fatal("failed save mutated ledger")
	}
	m.deps.checkWritable = originalCheck
	next, cmd = m.Update(actionKey("enter"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	items, _ = service.SearchIssues(t.Context(), "", "", "")
	if items[0].Epic != "October sprint" || items[1].Epic != "October sprint" || items[2].Epic != "Document management" {
		t.Fatal("retry changed wrong issues")
	}
	m.restoreIssueSelection("SBAH-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "b")
	// Empty query chooses existing epic options. Removal is a distinct final destination.
	if m.epicDestinations()[0].name == "" {
		t.Fatal("blank query defaults to removal")
	}
	for n := 0; n < 3; n++ {
		m = sendIssueKey(m, "down")
	}
	if m.epicDestinations()[m.epicAssignment.selected].name != "" {
		t.Fatal("No epic option missing")
	}
	next, cmd = m.Update(actionKey("enter"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	items, _ = service.SearchIssues(t.Context(), "", "", "")
	if items[0].Epic != "" || items[1].Epic != "October sprint" {
		t.Fatal("explicit removal changed wrong issues")
	}
	m.restoreIssueSelection("SBAH-2")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "b")
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	next, cmd = m.Update(actionKey("enter"))
	m = next.(model)
	if cmd != nil || !m.epicAssignment.stale || !strings.Contains(m.epicAssignment.err, "cancel and reopen") {
		t.Fatal("stale batch submitted")
	}
	captureBulkState(t, m, "07-stale")
	m = sendIssueKey(m, "esc")
	next, _ = m.Update(m.issueListCmd(m.issuesGen)())
	m = next.(model)
	m = sendIssueKey(m, "b")
	if m.epicAssignment == nil || m.epicAssignment.stale || len(m.issueMarks) != 1 {
		t.Fatal("stale recovery lost batch")
	}
}

func TestBulkEpicUnicodeValidationAndDestinationsScroll(t *testing.T) {
	m := navigationModel(t)
	m.issues = []worklogs.IssueMetadata{budgetIssue("APP-1", "", "Original", 1, 0, true)}
	m.restoreIssueSelection("APP-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "b")
	m.epicAssignment.input.SetValue("Invalid\u2028epic")
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	if cmd != nil || m.epicAssignment.err == "" || len(m.issueMarks) != 1 {
		t.Fatal("invalid new epic submitted")
	}
	m = sendIssueKey(m, "esc")
	for n := 2; n <= 60; n++ {
		m.issues = append(m.issues, budgetIssue(fmt.Sprintf("APP-%d", n), "", fmt.Sprintf("Epic %02d", n), 1, 0, true))
	}
	for _, noColor := range []bool{true, false} {
		m.deps.noColor = noColor
		m = sendIssueKey(m, "b")
		for n := 0; n < 100; n++ {
			m = sendIssueKey(m, "down")
		}
		if !strings.Contains(ansi.Strip(m.render()), "> No epic (remove assignment)") {
			t.Fatal("last option not visible")
		}
		captureBulkState(t, m, fmt.Sprintf("07-scrolled-picker-%t", noColor))
		m = sendIssueKey(m, "esc")
	}
}

func TestBulkEpicPolishedFeedbackAndViewportBounds(t *testing.T) {
	for _, size := range [][2]int{{100, 28}, {120, 36}} {
		for _, mode := range []string{"dark", "light", "no-color"} {
			m := navigationModel(t)
			m.width, m.height = size[0], size[1]
			m.deps.noColor = mode == "no-color"
			if mode == "light" {
				m.colors = m.deps.theme.Light
			} else {
				m.colors = m.deps.theme.Dark
			}
			m.issues = []worklogs.IssueMetadata{budgetIssue("APP-1", "A long representative title", "October sprint", 3600, 0, true)}
			m.restoreIssueSelection("APP-1")
			m = sendIssueKey(m, "space")
			m = sendIssueKey(m, "b")
			if !strings.Contains(ansi.Strip(m.render()), "Assign epic to 1 issue") || strings.Contains(ansi.Strip(m.render()), "1 issues") {
				t.Fatal("singular copy wrong")
			}
			m.epicAssignment.saving = true
			footer := ansi.Strip(m.renderFooter(m.width))
			if !strings.Contains(footer, "Saving epic assignments") || strings.Contains(footer, "Apply") || strings.Contains(footer, "Cancel") {
				t.Fatal("saving advertises blocked actions")
			}
			captureBulkState(t, m, fmt.Sprintf("09-saving-%s-%dx%d", mode, size[0], size[1]))
			m.epicAssignment.saving = false
			m.epicAssignment.stale = true
			m.epicAssignment.err = "Issues changed externally; cancel and reopen before applying."
			footer = ansi.Strip(m.renderFooter(m.width))
			if !strings.Contains(footer, "Cancel and reopen") || strings.Contains(footer, "Apply") {
				t.Fatal("stale footer offers save")
			}
			captureBulkState(t, m, fmt.Sprintf("09-stale-%s-%dx%d", mode, size[0], size[1]))
			m.epicAssignment.stale = false
			m.epicAssignment.err = strings.Repeat("Storage write failed: ", 100)
			view := ansi.Strip(m.render())
			if !strings.Contains(view, "Apply to all selected: October sprint") || !strings.Contains(view, "Storage write failed") || !strings.Contains(view, ">") {
				t.Fatal("long error obscured destination or consequence")
			}
			captureBulkState(t, m, fmt.Sprintf("09-long-error-%s-%dx%d", mode, size[0], size[1]))
		}
	}
}

func TestBulkEpicTrackedActivityRemainsVisible(t *testing.T) {
	m, _ := issueFixture(t)
	m.workspace.activities = &fakeActivities{}
	m.restoreIssueSelection("SBAH-1")
	m = sendIssueKey(m, "space")
	m = sendIssueKey(m, "b")
	next, cmd := m.Update(actionKey("enter"))
	m = next.(model)
	next, refresh := m.Update(cmd())
	m = next.(model)
	if m.epicAssignment != nil || len(m.issueMarks) != 0 || refresh == nil {
		t.Fatal("tracked save did not complete")
	}
	entries := drawerActivities(m.activities)
	if len(entries) != 1 || entries[0].Operation != "issues.apply" || entries[0].State != activity.StateSucceeded {
		t.Fatalf("bulk assignment outcome missing from Activity: %+v", entries)
	}
}
