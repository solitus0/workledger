package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func issueFixture(t *testing.T) (model, *worklogs.Service) {
	t.Helper()
	ws, deps := testWorkspace(t)
	path := filepath.Join(t.TempDir(), "issues.db")
	store, _, err := sqlitestore.Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := worklogs.NewService(store)
	ws.issues = service
	ws.sqlitePath = path
	deps.now = func() time.Time { return time.Date(2026, 10, 13, 13, 42, 0, 0, time.UTC) }
	ctx := context.Background()
	for _, input := range []worklogs.AddInput{
		{IssueKey: "SBAH-1", StartedUTC: "2026-10-10T09:00:00Z", Duration: "1h", Description: "Manual ordering"},
		{IssueKey: "SBAH-2", StartedUTC: "2026-10-11T09:00:00Z", Duration: "6h", Description: "Data lifecycle implementation"},
		{IssueKey: "SBAH-2", StartedUTC: "2026-10-12T09:00:00Z", Duration: "4h", Description: "Lifecycle checks"},
		{IssueKey: "SBAH-11", StartedUTC: "2026-10-13T09:00:00Z", Duration: "1h", Description: "New task"},
	} {
		if _, err := service.Add(ctx, ws.cfg, input); err != nil {
			t.Fatal(err)
		}
	}
	estimate1, estimate2 := int64(4*3600), int64(16*3600)
	title1, title2, epic := "Manual document ordering", "Product data lifecycle", "Document management"
	if err := service.ApplyIssueMetadata(ctx, []worklogs.IssueMetadataPatch{
		{IssueKey: "SBAH-1", Title: &title1, TitleSet: true, MaxEstimateSeconds: &estimate1, MaxEstimateSet: true, Epic: &epic, EpicSet: true},
		{IssueKey: "SBAH-2", Title: &title2, TitleSet: true, MaxEstimateSeconds: &estimate2, MaxEstimateSet: true, Epic: &epic, EpicSet: true},
		{IssueKey: "SBAH-11", Epic: &epic, EpicSet: true},
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE issues SET created_at=?`, deps.now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	m := newModel(ctx, deps, ws, nil)
	m.width, m.height, m.activeTab = 120, 36, issuesTab
	next, _ := m.Update(m.issueListCmd(m.issuesGen)())
	m = next.(model)
	m.restoreIssueSelection("SBAH-2")
	return m, service
}

func TestIssuesEstimateSaveAndCancelPreserveSelection(t *testing.T) {
	m, service := issueFixture(t)
	before := m.render()
	if !strings.Contains(before, "9h known left") || !strings.Contains(before, "1 unestimated") || m.selectedIssueKey() != "SBAH-2" {
		t.Fatalf("initial issue view: %s", before)
	}
	next, _ := m.Update(textKey("e"))
	m = next.(model)
	if m.issueForm == nil || !m.issueForm.editing || m.issueForm.focus != 1 {
		t.Fatal("edit did not focus optional title with immutable issue")
	}
	m.issueForm.inputs[2].SetValue("20h")
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	if cmd == nil {
		t.Fatalf("save did not start: %s", m.issueFormError)
	}
	next, consume := m.Update(cmd())
	m = next.(model)
	if consume == nil || m.issueForm != nil {
		t.Fatal("save did not return to the list and schedule refresh")
	}
	next, _ = m.Update(consume())
	m = next.(model)
	next, _ = m.Update(m.issueListCmd(m.issuesGen)())
	m = next.(model)
	item := m.selectedIssue()
	if item == nil || item.IssueKey != "SBAH-2" || *item.MaxEstimateSeconds != 20*3600 || *item.RemainingSeconds != 10*3600 {
		t.Fatalf("save did not retain key and recalculate: %#v", item)
	}
	if !strings.Contains(m.render(), "13h known left") {
		t.Fatalf("list/group summary did not update: %s", m.render())
	}
	next, _ = m.Update(textKey("e"))
	m = next.(model)
	m.issueForm.inputs[1].SetValue("Unsaved title")
	m.issueForm.inputs[2].SetValue("40h")
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	items, err := service.SearchIssues(context.Background(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.selectedIssueKey() != "SBAH-2" || items[1].Title != "Product data lifecycle" || *items[1].MaxEstimateSeconds != 20*3600 {
		t.Fatalf("cancel changed stored values or selection: %#v", items)
	}
}

func TestIssuesSearchEmptyResultsAndDetails(t *testing.T) {
	m, _ := issueFixture(t)
	next, _ := m.Update(textKey("/"))
	m = next.(model)
	for _, letter := range "lifecycle" {
		next, _ = m.Update(textKey(string(letter)))
		m = next.(model)
	}
	if len(m.visibleIssues()) != 1 || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("title search failed")
	}
	m.issueSearch.input.SetValue("missing")
	m.restoreIssueSelection("")
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	for _, key := range []string{"e", "enter"} {
		next, cmd := m.Update(actionKey(key))
		m = next.(model)
		if cmd != nil || m.issueForm != nil || m.overlay != noOverlay {
			t.Fatal("empty search acted on hidden issue")
		}
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issueSearch != nil || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("clearing search lost earlier selection")
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.overlay != detailsOverlay || !strings.Contains(m.render(), "Document management") {
		t.Fatal("details did not open")
	}
	for _, size := range [][2]int{{100, 28}, {120, 36}} {
		m.width, m.height = size[0], size[1]
		m.refreshDetailViewport()
		assertRedesignBounds(t, m, "issue details")
	}
}

func TestIssuesFormValidationFailureRecoveryAndExternalChanges(t *testing.T) {
	m, _ := issueFixture(t)
	next, _ := m.Update(textKey("e"))
	m = next.(model)
	m.issueForm.inputs[2].SetValue("bad")
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	if cmd != nil || m.issueFormError == "" {
		t.Fatal("invalid estimate was submitted")
	}
	m.issueForm.inputs[2].SetValue("20h")
	m.workspace.issues = &fakeIssues{err: errors.New("write unavailable")}
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.issueForm == nil || m.issueForm.saving || m.issueFormError != "write unavailable" || m.issueForm.inputs[2].Value() != "20h" {
		t.Fatal("save failure did not retain editable form")
	}
	next, _ = m.Update(pollResultMsg{changed: true})
	m = next.(model)
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	if !m.issueForm.stale || cmd != nil || !strings.Contains(m.issueFormError, "cancel and reopen") {
		t.Fatal("external issue change did not block stale save")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issueForm != nil || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("failure/stale cancel did not preserve selection")
	}
}

func TestIssuesLayoutsPreserveMetricsAndFormAtMinimumSize(t *testing.T) {
	m, _ := issueFixture(t)
	for _, noColor := range []bool{true, false} {
		for _, size := range [][2]int{{100, 28}, {120, 36}} {
			for _, drawer := range []bool{false, true} {
				base := m
				base.width, base.height, base.activityOpen, base.deps.noColor = size[0], size[1], drawer, noColor
				assertRedesignBounds(t, base, "issues")
				view := ansi.Strip(base.render())
				for _, content := range []string{"6 Issues", "Logged", "Estimate", "Remaining", "Document management", "Unknown", "1 unestimated"} {
					if !strings.Contains(view, content) {
						t.Fatalf("lost %s at %v activity=%t: %s", content, size, drawer, view)
					}
				}
				base.openIssueForm(base.selectedIssue())
				assertRedesignBounds(t, base, "issue form")
				for _, content := range []string{"Title", "Max estimate", "Epic", "Ctrl+S", "Esc"} {
					if !strings.Contains(ansi.Strip(base.render()), content) {
						t.Fatalf("form lost %s at %v activity=%t", content, size, drawer)
					}
				}
			}
		}
	}
}

func TestIssuesKeyOnlyCreateAndOptionalFieldsClear(t *testing.T) {
	m, service := issueFixture(t)
	next, _ := m.Update(textKey("a"))
	m = next.(model)
	m.issueForm.inputs[0].SetValue("sbah-12")
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(m.startIssueLoad()())
	m = next.(model)
	if issue := m.selectedIssue(); issue == nil || issue.IssueKey != "SBAH-12" || issue.Title != "" || issue.Epic != "" || issue.MaxEstimateSeconds != nil {
		t.Fatalf("key-only create did not retain optional metadata: %#v", issue)
	}
	m.restoreIssueSelection("SBAH-2")
	next, _ = m.Update(textKey("e"))
	m = next.(model)
	for index := 1; index < 4; index++ {
		m.issueForm.inputs[index].SetValue("")
	}
	next, cmd = m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	items, err := service.SearchIssues(context.Background(), "SBAH", "SBAH-2", "")
	if err != nil || len(items) != 1 || items[0].Title != "" || items[0].Epic != "" || items[0].MaxEstimateSeconds != nil || items[0].LoggedSeconds != 10*3600 {
		t.Fatalf("clear removed identity or retained optional fields: %#v %v", items, err)
	}
}

func TestIssuesLongMetadataIsPreservedByUnrelatedEdit(t *testing.T) {
	m, service := issueFixture(t)
	title, epic := strings.Repeat("Long title ", 80), strings.Repeat("Long epic ", 80)
	if err := service.ApplyIssueMetadata(context.Background(), []worklogs.IssueMetadataPatch{{IssueKey: "SBAH-2", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(m.startIssueLoad()())
	m = next.(model)
	next, _ = m.Update(textKey("e"))
	m = next.(model)
	if m.issueForm.inputs[1].Value() != strings.TrimSpace(title) || m.issueForm.inputs[3].Value() != strings.TrimSpace(epic) {
		t.Fatal("opening form truncated existing metadata")
	}
	m.issueForm.inputs[2].SetValue("20h")
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	next, _ = m.Update(m.startIssueLoad()())
	m = next.(model)
	if item := m.selectedIssue(); item.Title != strings.TrimSpace(title) || item.Epic != strings.TrimSpace(epic) {
		t.Fatal("unrelated estimate edit changed long metadata")
	}
	m.width, m.height, m.activityOpen = 100, 28, true
	view := ansi.Strip(m.render())
	for _, metric := range []string{"Logged", "Estimate", "Remaining"} {
		if !strings.Contains(view, metric) {
			t.Fatalf("long metadata hid %s: %s", metric, view)
		}
	}
}

func TestIssuesRefreshRetainsSelectionAndRejectsStaleResults(t *testing.T) {
	m, _ := issueFixture(t)
	oldGeneration := m.issuesGen
	next, cmd := m.Update(textKey("r"))
	m = next.(model)
	if cmd == nil || m.issuesGen <= oldGeneration {
		t.Fatal("refresh did not start a new generation")
	}
	next, _ = m.Update(issueListResultMsg{generation: oldGeneration, items: nil})
	m = next.(model)
	if m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("stale result replaced issue selection")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("refresh lost issue selection")
	}
}

func TestIssuesNewPatchOmitsBlankMetadataAndEditClears(t *testing.T) {
	m, service := issueFixture(t)
	m.openIssueForm(nil)
	m.issueForm.inputs[0].SetValue("SBAH-99")
	patch, err := m.issueFormPatch()
	if err != nil || patch.TitleSet || patch.MaxEstimateSet || patch.EpicSet {
		t.Fatalf("blank Add metadata was not omitted: %#v %v", patch, err)
	}
	// A concurrent writer can introduce this issue after the form opens.
	title, epic, estimate := "Concurrent title", "Concurrent epic", int64(8*3600)
	if err := service.ApplyIssueMetadata(context.Background(), []worklogs.IssueMetadataPatch{{IssueKey: "SBAH-99", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true, MaxEstimateSeconds: &estimate, MaxEstimateSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyIssueMetadata(context.Background(), []worklogs.IssueMetadataPatch{patch}, false); err != nil {
		t.Fatal(err)
	}
	items, err := service.SearchIssues(context.Background(), "", "SBAH-99", "")
	if err != nil || len(items) != 1 || items[0].Title != title || items[0].Epic != epic || *items[0].MaxEstimateSeconds != estimate {
		t.Fatalf("key-only Add cleared concurrent metadata: %#v %v", items, err)
	}
	m.openIssueForm(&items[0])
	for index := 1; index < 4; index++ {
		m.issueForm.inputs[index].SetValue("")
	}
	patch, err = m.issueFormPatch()
	if err != nil || !patch.TitleSet || !patch.MaxEstimateSet || !patch.EpicSet || patch.Title != nil || patch.MaxEstimateSeconds != nil || patch.Epic != nil {
		t.Fatalf("blank Edit metadata did not clear: %#v %v", patch, err)
	}
}

func TestIssuesCatalogLoadingFailureEmptyAndRetryAreDistinct(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, destination := range []issueView{issueListView, epicListView} {
			ws, deps := testWorkspace(t)
			deps.noColor = noColor
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height, m.activityOpen, m.activeTab, m.issueView = 100, 28, true, issuesTab, destination
			view := ansi.Strip(m.render())
			if !strings.Contains(view, "Loading issues…") || strings.Contains(view, "0 issues") || strings.Contains(view, "0h left") || strings.Contains(view, "No issues yet") || strings.Contains(view, "No epics yet") {
				t.Fatalf("initial loading implied known empty budget: %s", view)
			}
			assertRedesignBounds(t, m, "initial issue loading")
			next, _ := m.Update(issueListResultMsg{generation: m.issuesGen, err: errors.New("catalog unavailable")})
			m = next.(model)
			m.clearNotice()
			view = ansi.Strip(m.render())
			if !strings.Contains(view, "Unable to load issues.") || !strings.Contains(view, "r Retry") || strings.Contains(view, "0 issues") || strings.Contains(view, "0h left") || strings.Contains(view, "No issues yet") || strings.Contains(view, "No epics yet") {
				t.Fatalf("failed load implied known empty budget: %s", view)
			}
			assertRedesignBounds(t, m, "failed issue loading")
			m.issueView = issueListView
			next, cmd := m.Update(textKey("a"))
			m = next.(model)
			if cmd != nil || m.issueForm != nil {
				t.Fatal("catalog unavailable allowed Add")
			}
			m.issueView = destination
			next, cmd = m.Update(textKey("r"))
			m = next.(model)
			if cmd == nil || m.issuesErr != "" || !m.issuesLoading {
				t.Fatal("retry did not clear persistent failure and load again")
			}
			next, _ = m.Update(cmd())
			m = next.(model)
			view = ansi.Strip(m.render())
			empty := "No issues yet"
			if destination == epicListView {
				empty = "No epics yet"
			}
			if m.issuesErr != "" || m.issuesLoading || !strings.Contains(view, empty) || strings.Contains(view, "Unable to load") || strings.Contains(view, "Loading issues") {
				t.Fatalf("successful empty catalog was not distinct: %s", view)
			}
			assertRedesignBounds(t, m, "successful empty issue catalog")
		}
	}
}

func TestIssuesRefreshFailureRetainsDataWithPersistentLabel(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, destination := range []issueView{issueListView, epicListView} {
			m, _ := issueFixture(t)
			m.deps.noColor, m.width, m.height, m.activityOpen, m.issueView = noColor, 100, 28, true, destination
			m.restoreEpicSelection("Document management", true)
			next, cmd := m.Update(textKey("r"))
			m = next.(model)
			view := ansi.Strip(m.render())
			if !strings.Contains(view, "Refreshing issues…") || !strings.Contains(view, "9h known left") {
				t.Fatalf("refresh hid prior data or its freshness: %s", view)
			}
			next, _ = m.Update(issueListResultMsg{generation: m.issuesGen, err: errors.New("refresh unavailable")})
			m = next.(model)
			m.clearNotice()
			view = ansi.Strip(m.render())
			if !strings.Contains(view, "Refresh failed; showing previous data.") || !strings.Contains(view, "9h known left") || !strings.Contains(view, "r Retry") {
				t.Fatalf("failure removed prior data or its persistent label: %s", view)
			}
			assertRedesignBounds(t, m, "issue refresh failure")
			next, retry := m.Update(textKey("r"))
			m = next.(model)
			if cmd == nil || retry == nil {
				t.Fatal("refresh/retry command missing")
			}
			next, _ = m.Update(retry())
			m = next.(model)
			if m.issuesErr != "" || m.issuesLoading || m.selectedIssueKey() != "SBAH-2" || strings.Contains(ansi.Strip(m.render()), "Refresh failed") {
				t.Fatal("successful retry did not recover and preserve selection")
			}
		}
	}
}
