package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	reconcilemodel "github.com/solitus0/workledger/internal/reconcile/model"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestLocalDayFilterPreservesTimelineAndGuardsHiddenRows(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogs = testRows()
	m.worklogs = append(m.worklogs,
		worklogs.LocalWorklog{ID: "ranking", IssueKey: "WL-2", StartedAtUTC: m.selectedDate.Add(12 * time.Hour), DurationSeconds: 900, Description: "Search ranking", Revision: 1},
		worklogs.LocalWorklog{ID: "title", IssueKey: "WL-3", StartedAtUTC: m.selectedDate.Add(13 * time.Hour), DurationSeconds: 900, Description: "Testing", Revision: 1})
	m.issueMetadata = map[string]worklogs.IssueMetadata{"WL-3": {Title: "Search ranking improvements"}}
	m.worklogSelected = 1
	timeline, booked := m.renderDayTimeline(nil, ""), m.dayBookedSeconds()
	next, _ := m.Update(textKey("/"))
	m = next.(model)
	for _, char := range "RANKING" {
		next, _ = m.Update(textKey(string(char)))
		m = next.(model)
	}
	if len(m.visibleWorklogIndices()) != 2 || m.selectedWorklogID() != "ranking" {
		t.Fatalf("filter selection = %v / %s", m.visibleWorklogIndices(), m.selectedWorklogID())
	}
	generation := m.issueTotalGen
	next, _ = m.Update(struct{}{})
	m = next.(model)
	if m.issueTotalGen != generation {
		t.Fatal("unchanged filter input scheduled another issue aggregate query")
	}
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	if m.selectedWorklogID() != "title" {
		t.Fatal("filter arrows did not traverse matching rows")
	}
	if m.renderDayTimeline(nil, "") != timeline || m.dayBookedSeconds() != booked || len(m.worklogs) != 3 {
		t.Fatal("filter changed day occupancy or ledger records")
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	next, _ = m.Update(textKey("D"))
	m = next.(model)
	if m.overlay != deleteOverlay || len(m.deleteSelection) != 1 || m.deleteSelection[0].ID != "title" {
		t.Fatal("filtered delete did not confirm the selected matching row")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	next, _ = m.Update(textKey("/"))
	m = next.(model)
	m.worklogFilter.input.SetValue("no matching record")
	m.restoreWorklogSelection("")
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	for _, key := range []string{"e", "D", "M", "enter"} {
		next, cmd := m.Update(actionKey(key))
		m = next.(model)
		if cmd != nil || m.form != nil || m.move != nil || m.overlay != noOverlay || m.selectedWorklog() != nil {
			t.Fatalf("empty filter allowed %q to act on a hidden row", key)
		}
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.worklogFilter != nil || m.selectedWorklogID() != "ranking" {
		t.Fatal("clearing filter did not restore the earlier selection")
	}
}

func TestFilteredRefreshRetainsMatchingIDAndModeReturnIsSafe(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogs = append(testRows(), worklogs.LocalWorklog{ID: "other", IssueKey: "OTHER-1", Description: "Unrelated"})
	m.openWorklogFilter()
	m.worklogFilter.input.SetValue("WL-1234")
	m.worklogFilter.editing = false
	next, _ := m.Update(worklogResultMsg{generation: m.worklogGen, date: m.selectedDate, items: []worklogs.LocalWorklog{m.worklogs[1], m.worklogs[0]}})
	m = next.(model)
	if m.worklogSelected != 1 || m.selectedWorklogID() != "row-1" {
		t.Fatal("filtered refresh lost the matching ID")
	}
	m.worklogView, m.worklogSelected = weekWorklogView, 0
	next, _ = m.switchWorklogView(dayWorklogView)
	m = next.(model)
	if m.selectedWorklogID() != "row-1" {
		t.Fatal("returning to filtered Day retained a hidden selection")
	}
}

func TestActionsSearchDispatchesThroughExistingConfirmation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogs = testRows()
	next, _ := m.Update(textKey("?"))
	m = next.(model)
	next, _ = m.Update(specialKey(tea.KeyDown))
	m = next.(model)
	next, _ = m.Update(struct{}{})
	m = next.(model)
	if m.actions.selected != 1 {
		t.Fatal("unrelated input events reset the selected action")
	}
	for _, char := range "move worklog to trash" {
		next, _ = m.Update(textKey(string(char)))
		m = next.(model)
	}
	if matches := m.matchingActions(); len(matches) != 1 || matches[0].key != "D" {
		t.Fatalf("matching actions = %#v", matches)
	}
	next, cmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if cmd != nil || m.overlay != deleteOverlay || len(m.deleteSelection) != 1 {
		t.Fatal("Actions bypassed delete confirmation")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	next, _ = m.Update(textKey("?"))
	m = next.(model)
	next, cmd = m.Update(textKey("q"))
	m = next.(model)
	if cmd != nil || m.actions == nil || m.actions.input.Value() != "q" {
		t.Fatal("typing q in Actions quit instead of filtering")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.selectedWorklogID() != "row-1" || m.actions != nil {
		t.Fatal("closing Actions changed row context")
	}
}

func TestActionsOpenAndCloseFromEveryDestination(t *testing.T) {
	for _, destination := range []struct{ key, label string }{
		{"1", "Status"}, {"2", "Worklogs"}, {"3", "Plans"},
		{"4", "Presets"}, {"5", "Trash"}, {"6", "Issues"},
	} {
		t.Run(destination.label, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(t.Context(), deps, ws, nil)
			m.width, m.height = minimumWidth, minimumHeight
			next, _ := m.Update(textKey(destination.key))
			m = next.(model)
			selectedTab := m.activeTab
			next, _ = m.Update(textKey("?"))
			m = next.(model)
			if m.actions == nil || !strings.Contains(stripANSI(m.View().Content), "Actions · "+destination.label) {
				t.Fatal("Actions did not render for the selected destination")
			}
			assertRedesignBounds(t, m, "Actions on "+destination.label)
			next, _ = m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if m.overlay != noOverlay || m.actions != nil || m.activeTab != selectedTab {
				t.Fatal("closing Actions changed the selected destination")
			}
		})
	}
}

func TestWorklogDetailsScrollAndReturnToSelection(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogs = testRows()
	m.worklogs[0].Description = strings.Repeat("Detailed worklog explanation\n", 40) + "Final note"
	next, _ := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.overlay != detailsOverlay || !strings.Contains(m.render(), "Created") {
		t.Fatal("details did not expose lifecycle fields")
	}
	next, _ = m.Update(specialKey(tea.KeyEnd))
	m = next.(model)
	if !strings.Contains(m.render(), "Final note") {
		t.Fatal("long description could not be scrolled to its end")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.overlay != noOverlay || m.selectedWorklogID() != "row-1" {
		t.Fatal("details close changed selection")
	}
}

func TestExpandedInspectorsKeepLongContentAccessible(t *testing.T) {
	for _, destination := range []string{"diagnostic", "configuration", "preset", "trash", "scope"} {
		t.Run(destination, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = minimumWidth, minimumHeight
			content := strings.Repeat("Full inspector content with a long explanation.\n", 40) + "Final inspector note"
			switch destination {
			case "diagnostic":
				m.activeTab = statusTab
				m.statusItems = []status.Item{{Status: "ok", Message: content}}
			case "configuration":
				m.activeTab, m.statusView = statusTab, statusConfigView
				m.statusConfig = &config.ConfigSummary{ConfigPath: content}
			case "preset":
				m.activeTab = presetsTab
				m.presets = []presets.Preset{{ID: "preset", Description: content}}
			case "trash":
				m.activeTab = trashTab
				m.trashItems = []worklogs.TrashRecord{{ID: "trash", Description: content}}
			case "scope":
				m.activeTab = plansTab
				m.plan = &reconcile.Plan{Direction: "push", Items: []reconcile.PlanItem{{PlanStatus: "ready", Payload: []reconcilemodel.Row{{Description: content}}}}}
			}
			date, tab := m.selectedDate, m.activeTab
			next, _ := m.Update(specialKey(tea.KeyEnter))
			m = next.(model)
			if m.overlay != detailsOverlay {
				t.Fatal("selected inspector did not expand")
			}
			assertRedesignBounds(t, m, destination+" details")
			next, _ = m.Update(specialKey(tea.KeyEnd))
			m = next.(model)
			if !strings.Contains(m.render(), "Final inspector note") {
				t.Fatal("expanded inspector lost content beyond the visible pane")
			}
			for _, size := range [][2]int{{20, 8}, {120, 36}} {
				next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = next.(model)
			}
			next, _ = m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if m.overlay != noOverlay || m.activeTab != tab || !m.selectedDate.Equal(date) {
				t.Fatal("expanded inspector changed the underlying context")
			}
		})
	}
}

func TestPlanProjectionUsesSavedDesiredRowsAndPreservesUnknowns(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.plan = &reconcile.Plan{Direction: "push"}
	item := reconcile.PlanItem{PlanStatus: "ready", ComparisonStatus: "remote_diff", LocalRowCount: 2, LocalTotal: 9000, RemoteRowCount: 1,
		Payload: []reconcilemodel.Row{{DurationSeconds: 1800}, {DurationSeconds: 900}}}
	if view := m.renderPlanItemInspector(item, 32, 22); !strings.Contains(view, "Remote: 2 rows · 45m") {
		t.Fatalf("projection did not use saved desired rows:\n%s", view)
	}
	m.plan.Direction = "pull"
	if view := m.renderPlanItemInspector(item, 32, 22); !strings.Contains(view, "Local: 2 rows · 45m") {
		t.Fatalf("pull projection targeted the wrong destination:\n%s", view)
	}
	item.Payload, item.ComparisonStatus = nil, "check_failed"
	if view := m.renderPlanItemInspector(item, 32, 22); !strings.Contains(view, "Remote at planning\nUnavailable") || !strings.Contains(view, "Desired state unavailable") || !strings.Contains(view, "Create - · Delete -") {
		t.Fatalf("failed inspection invented a projection or diff:\n%s", view)
	}
}

func TestRedesignFitsEveryDestinationAndInteraction(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, size := range [][2]int{{100, 28}, {120, 36}} {
			ws, deps := testWorkspace(t)
			deps.noColor = noColor
			base := newModel(context.Background(), deps, ws, nil)
			base.width, base.height = size[0], size[1]
			base.worklogs = testRows()
			base.contextDays = testWeekDays()
			base.statusItems = []status.Item{{Category: "local", Target: "storage", Status: "ok", Message: strings.Repeat("Healthy local store. ", 8)}}
			base.presets = []presets.Preset{{ID: "preset", Name: "Daily standup", IssueKey: "WL-1", StartTime: "10:30", DurationSeconds: 900, Description: "Standup"}}
			base.plans = []reconcile.ListEntry{{ID: "plan", Direction: "push", ExecutionState: "partially_applied", CreatedAt: deps.now()}}
			for _, drawer := range []bool{false, true} {
				for _, destination := range []tab{statusTab, worklogsTab, plansTab, presetsTab, trashTab, issuesTab} {
					m := base
					m.activeTab, m.activityOpen = destination, drawer
					assertRedesignBounds(t, m, fmt.Sprintf("destination %d drawer=%t", destination, drawer))
				}
			}
			for _, mode := range []worklogView{dayWorklogView, weekWorklogView, monthWorklogView} {
				m := base
				m.worklogView = mode
				m.openAddForm()
				for _, placement := range []placementMode{fillPlacement, fitPlacement, manualPlacement} {
					m.form.placement = placement
					m.form.previewRecords = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(18 * time.Hour), DurationSeconds: 900}}
					assertRedesignBounds(t, m, fmt.Sprintf("form %d/%d", mode, placement))
					for _, hint := range []string{"Ctrl+S", "Esc", "Ctrl+P"} {
						if !strings.Contains(stripANSI(m.renderFooter(m.width)), hint) {
							t.Fatalf("form lost %s", hint)
						}
					}
				}
			}
			m := base
			m.openActions()
			assertRedesignBounds(t, m, "Actions")
			m = base
			m.openWorklogFilter()
			assertRedesignBounds(t, m, "filter")
			m = base
			m.overlay = deleteOverlay
			m.deleteSelection = m.worklogs
			assertRedesignBounds(t, m, "confirmation")
		}
	}
}

func assertRedesignBounds(t *testing.T, m model, scenario string) {
	t.Helper()
	view := m.render()
	if width, height := lipgloss.Width(view), lipgloss.Height(view); width != m.width || height != m.height {
		t.Fatalf("%s: got %dx%d, want %dx%d\n%s", scenario, width, height, m.width, m.height, view)
	}
	if m.deps.noColor && strings.Contains(view, "\x1b[") {
		t.Fatalf("%s emitted color with NO_COLOR", scenario)
	}
}

func TestTerminalTruncationUsesDisplayWidth(t *testing.T) {
	for _, value := range []string{"日々の記録", "👩‍💻 ledger", "\x1b[31m日々の記録\x1b[m"} {
		if result := truncate(value, 6); lipgloss.Width(result) > 6 {
			t.Fatalf("wide text exceeds its cell budget: %q", result)
		}
	}
}
