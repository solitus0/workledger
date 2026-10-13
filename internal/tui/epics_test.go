package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

func saveIssueForm(t *testing.T, m model) model {
	t.Helper()
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	if cmd == nil {
		t.Fatalf("issue save did not start: %s", m.issueFormError)
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.issueForm != nil {
		t.Fatalf("issue save failed: %s", m.issueFormError)
	}
	load := m.startIssueLoad()
	next, _ = m.Update(load())
	return next.(model)
}

func TestEpicsWorkflowShowsUnknownOverrunAndExactMembers(t *testing.T) {
	m, service := issueFixture(t)
	ctx := context.Background()
	if _, err := service.Add(ctx, m.workspace.cfg, worklogs.AddInput{IssueKey: "SBAH-3", StartedUTC: "2026-10-09T09:00:00Z", Duration: "3h", Description: "Over estimate"}); err != nil {
		t.Fatal(err)
	}
	epic, estimate := "Document management", int64(3600)
	if err := service.ApplyIssueMetadata(ctx, []worklogs.IssueMetadataPatch{
		{IssueKey: "SBAH-3", MaxEstimateSeconds: &estimate, MaxEstimateSet: true, Epic: &epic, EpicSet: true},
		{IssueKey: "SBAH-4"},
	}, false); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(m.startIssueLoad()())
	m = next.(model)
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	group := m.selectedEpic()
	if group == nil || group.Epic != epic || group.IssueCount != 4 || group.RemainingSeconds != 9*3600 || group.OverrunSeconds != 2*3600 || group.UnestimatedIssueCount != 1 {
		t.Fatalf("epic totals lost unknown/overrun: %#v", group)
	}
	view := ansi.Strip(m.render())
	for _, metric := range []string{"9h known left", "1 unestimated", "2h over", "Ungrouped", "Remaining unknown"} {
		if !strings.Contains(view, metric) {
			t.Fatalf("epic view lost %s: %s", metric, view)
		}
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.issueView != issueListView || m.issueEpic == nil || *m.issueEpic != epic || len(m.visibleIssues()) != 4 {
		t.Fatal("epic did not drill into exact members")
	}
	m.restoreIssueSelection("SBAH-2")
	next, _ = m.Update(textKey("e"))
	m = next.(model)
	m.issueForm.inputs[3].SetValue("")
	m = saveIssueForm(t, m)
	if m.selectedIssueKey() == "SBAH-2" || len(m.visibleIssues()) != 3 {
		t.Fatal("membership clear retained a hidden member selection")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issueView != epicListView || m.selectedEpic().Epic != epic || m.selectedEpic().RemainingSeconds != 3*3600 {
		t.Fatal("return did not retain original epic and recalculate")
	}
	m.restoreEpicSelection("", true)
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.issueEpic == nil || *m.issueEpic != "" || len(m.visibleIssues()) != 2 {
		t.Fatal("Ungrouped scope was confused with an unfiltered issue list")
	}
	next, _ = m.Update(textKey("a"))
	m = next.(model)
	if m.issueForm.inputs[3].Value() != "" {
		t.Fatal("new unassigned issue inherited a named epic")
	}
	m.issueForm.inputs[0].SetValue("SBAH-99")
	m = saveIssueForm(t, m)
	if m.selectedIssueKey() != "SBAH-99" || len(m.visibleIssues()) != 3 {
		t.Fatal("new unassigned issue lost its member view selection")
	}
	m.restoreIssueSelection("SBAH-2")
	next, _ = m.Update(textKey("e"))
	m = next.(model)
	m.issueForm.inputs[3].SetValue("New milestone")
	m = saveIssueForm(t, m)
	if m.selectedIssueKey() == "SBAH-2" || len(m.visibleIssues()) != 2 {
		t.Fatal("reassignment left selection on a hidden issue")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	groups := m.visibleEpics()
	if len(groups) != 3 || groups[0].Epic != epic || groups[1].Epic != "New milestone" || groups[2].Epic != "" {
		t.Fatalf("groups should be sorted with Ungrouped last: %#v", groups)
	}
	m.restoreEpicSelection("New milestone", true)
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if len(m.visibleIssues()) != 1 || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("reassigned issue was not accessible through its new epic")
	}
}

func TestEpicSearchAndViewTogglePreserveContextAndTotals(t *testing.T) {
	m, _ := issueFixture(t)
	next, _ := m.Update(textKey("t"))
	m = next.(model)
	total := *m.selectedEpic()
	next, _ = m.Update(textKey("/"))
	m = next.(model)
	for _, letter := range "DOCUMENT" {
		next, _ = m.Update(textKey(string(letter)))
		m = next.(model)
	}
	if len(m.visibleEpics()) != 1 || *m.selectedEpic() != total {
		t.Fatal("epic name search changed group metrics")
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	m.restoreIssueSelection("SBAH-2")
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	if m.selectedEpic() == nil || m.selectedEpic().Epic != "Document management" || m.epicSearch == nil {
		t.Fatal("toggle lost epic search/selection context")
	}
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	if m.issueEpic == nil || *m.issueEpic != "Document management" || m.selectedIssueKey() != "SBAH-2" {
		t.Fatal("toggle lost member issue context")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.issueView != epicListView || m.issueEpic != nil {
		t.Fatal("Esc did not finish the member drilldown")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.epicSearch != nil || m.selectedEpic().Epic != "Document management" {
		t.Fatal("Esc did not clear epic filter and retain selection")
	}
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	if m.issueEpic != nil || len(m.visibleIssues()) != len(m.issues) {
		t.Fatal("all issues were inaccessible after leaving group members")
	}
}

func TestEpicAndMemberListsScrollAndDetailsExposeLongContent(t *testing.T) {
	m, service := issueFixture(t)
	patches := make([]worklogs.IssueMetadataPatch, 0, 45)
	for index := 0; index < 45; index++ {
		epic, title := fmt.Sprintf("Milestone %02d", index), strings.Repeat("Long issue title ", 25)+"Final title"
		patches = append(patches, worklogs.IssueMetadataPatch{IssueKey: fmt.Sprintf("SBAH-%d", 100+index), Title: &title, TitleSet: true, Epic: &epic, EpicSet: true})
	}
	if err := service.ApplyIssueMetadata(context.Background(), patches, false); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(m.startIssueLoad()())
	m = next.(model)
	m.width, m.height, m.activityOpen = 100, 28, true
	next, _ = m.Update(textKey("t"))
	m = next.(model)
	for range 45 {
		next, _ = m.Update(specialKey(tea.KeyDown))
		m = next.(model)
	}
	if !strings.Contains(m.render(), "Milestone 44") || m.selectedEpic().Epic != "Milestone 44" {
		t.Fatal("epic list did not scroll selected final group into view")
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyEnd))
	m = next.(model)
	if !strings.Contains(m.render(), "Final title") {
		t.Fatal("expanded details did not scroll to full title")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.selectedIssueKey() != "SBAH-144" {
		t.Fatal("closing details lost member selection")
	}
	m.issueEpic = nil
	for range 50 {
		next, _ = m.Update(specialKey(tea.KeyDown))
		m = next.(model)
	}
	if !strings.Contains(m.render(), "SBAH-144") {
		t.Fatal("issue list did not scroll selected final issue into view")
	}
}

func TestEpicAndMemberLayoutsAcrossThemesAndActivity(t *testing.T) {
	m, _ := issueFixture(t)
	for _, noColor := range []bool{true, false} {
		for _, palette := range []Palette{DefaultTheme().Light, DefaultTheme().Dark} {
			for _, size := range [][2]int{{100, 28}, {120, 36}} {
				for _, drawer := range []bool{false, true} {
					for _, view := range []issueView{issueListView, epicListView} {
						base := m
						base.deps.noColor, base.colors, base.colorReady = noColor, palette, true
						base.width, base.height, base.activityOpen, base.issueView = size[0], size[1], drawer, view
						base.restoreEpicSelection("Document management", true)
						assertRedesignBounds(t, base, "issues/epics")
						for _, content := range []string{"Logged", "Remaining", "Document management", "unestimated"} {
							if !strings.Contains(ansi.Strip(base.render()), content) {
								t.Fatalf("lost %s in view %d at %v activity=%t", content, view, size, drawer)
							}
						}
						if noColor && strings.Contains(base.render(), "\x1b[") {
							t.Fatal("NO_COLOR emitted ANSI")
						}
						if view == epicListView {
							next, _ := base.Update(specialKey(tea.KeyEnter))
							assertRedesignBounds(t, next.(model), "epic members")
						}
					}
				}
			}
		}
	}
}

func TestEpicLongBudgetsKeepSelectedGroupAndUnknownEstimateVisible(t *testing.T) {
	m, _ := issueFixture(t)
	estimate, logged := int64(9000000000000000), int64(9100000000000000)
	m.issues = []worklogs.IssueMetadata{
		{IssueKey: "SBAH-1", Epic: "First", MaxEstimateSeconds: &estimate},
		{IssueKey: "SBAH-2", Epic: "First", LoggedSeconds: logged, MaxEstimateSeconds: &estimate},
		{IssueKey: "SBAH-3", Epic: "First"},
		{IssueKey: "SBAH-4", Epic: "Last"},
	}
	m.width, m.height, m.activityOpen, m.issueView = 100, 28, true, epicListView
	m.restoreEpicSelection("Last", true)
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "> Last") || !strings.Contains(view, "Estimated   Unknown") || !strings.Contains(view, "Remaining   Unknown") {
		t.Fatalf("long preceding group hid final selection or all-unknown estimate: %s", view)
	}
	assertRedesignBounds(t, m, "large epic budgets")
}
