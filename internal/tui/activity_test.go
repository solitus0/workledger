package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestStyledPlanAndActivityRowsPreservePlainLayout(t *testing.T) {
	ws, deps := testWorkspace(t)
	now := deps.now()
	ws.activities = &fakeActivities{}

	plain := newModel(context.Background(), deps, ws, nil)
	plain.width, plain.height = 140, 38
	plain.activeTab = plansTab
	plain.plans = []reconcile.ListEntry{
		{ID: "ready", Direction: "push", WindowFromUTC: now, WindowToUTC: now, CreatedAt: now, PlanningStatus: "ready", ExecutionState: "ready", ActionableItems: 3, OpenItems: 3},
		{ID: "partial", Direction: "pull", WindowFromUTC: now, WindowToUTC: now, CreatedAt: now, PlanningStatus: "ready", ExecutionState: "partially_applied", ActionableItems: 3, OpenItems: 1, SucceededItems: 2},
		{ID: "succeeded", Direction: "push", WindowFromUTC: now, WindowToUTC: now, CreatedAt: now, PlanningStatus: "ready", ExecutionState: "succeeded", ActionableItems: 8, SucceededItems: 8},
	}
	if rendered := plain.renderPlanListWorkspace(109, 35); !strings.Contains(rendered, "STATE") || !strings.Contains(rendered, "OPEN") || !strings.Contains(rendered, "succeeded") {
		t.Fatalf("plan list does not expose terminal execution state and open count:\n%s", rendered)
	}
	colored := plain
	colored.deps.noColor = false
	if got, want := stripANSI(colored.renderPlanListWorkspace(109, 35)), plain.renderPlanListWorkspace(109, 35); got != want {
		t.Fatalf("styled plan layout differs from plain layout:\n%s\nwant:\n%s", got, want)
	}

	duration := int64(250)
	plain.activities = []activity.Entry{
		{Source: activity.SourceTUI, Operation: "presets.update", State: activity.StateSucceeded, StartedAt: now, DurationMS: &duration, Summary: "updated the daily preset"},
		{Source: activity.SourceCLI, Operation: "reconcile.apply", State: activity.StatePartial, StartedAt: now, DurationMS: &duration, Summary: "two scopes applied and one skipped"},
	}
	colored.activities = plain.activities
	if got, want := stripANSI(colored.renderActivityDrawer(109, activityDrawerHeight)), plain.renderActivityDrawer(109, activityDrawerHeight); got != want {
		t.Fatalf("styled activity layout differs from plain layout:\n%s\nwant:\n%s", got, want)
	}
}

func TestActivityLifecycleAndDrawer(t *testing.T) {
	ws, deps := testWorkspace(t)
	activities := &fakeActivities{}
	ws.activities = activities
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	cmd := m.beginActivity("worklogs.add", "issue=APP-1 · duration=1h", func() tea.Msg {
		return mutationResultMsg{kind: "add", records: []worklogs.LocalWorklog{{ID: "one", IssueKey: "APP-1", StartedAtUTC: deps.now()}}}
	})
	if len(m.activities) != 1 || m.activities[0].State != activity.StateRunning {
		t.Fatalf("optimistic activity=%+v", m.activities)
	}
	next, _ := m.Update(cmd())
	m = next.(model)
	if len(activities.started) != 1 || len(activities.finished) != 1 || m.activities[0].State != activity.StateSucceeded {
		t.Fatalf("activity starts=%d finishes=%d entries=%+v", len(activities.started), len(activities.finished), m.activities)
	}
	if footer := m.renderFooter(m.width); strings.Contains(footer, "added worklog") || strings.Contains(footer, "succeeded") {
		t.Fatalf("completed activity leaked into action bar: %q", footer)
	}
	next, _ = m.Update(textKey("g"))
	m = next.(model)
	if !m.activityOpen || m.focus != focusActivity {
		t.Fatalf("drawer open=%t focused=%t", m.activityOpen, m.focus == focusActivity)
	}
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("activity render=%dx%d", width, height)
	}
	if !strings.Contains(rendered, "Activity") || !strings.Contains(rendered, "worklogs.add") {
		t.Fatalf("activity drawer missing: %s", rendered)
	}
	activityTitleLine := strings.Split(rendered, "\n")[minimumHeight-3-activityDrawerHeight+1]
	if offset := strings.Index(activityTitleLine, "Activity"); offset < 31 {
		t.Fatalf("activity drawer starts at column %d, want right panel column 31 or later: %q", offset, activityTitleLine)
	}
	m.openAddForm()
	if strings.Contains(m.render(), "worklogs.add") {
		t.Fatal("activity drawer remained visible during form")
	}
}

func TestActivityDrawerShowsOnlyExplicitDomainActions(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	items := []activity.Entry{
		{ID: "delete", Source: activity.SourceTUI, Operation: "worklogs.delete", State: activity.StateSucceeded},
		{ID: "cli-add", Source: activity.SourceCLI, Operation: "worklogs.add", State: activity.StateFailed},
		{ID: "retry", Source: activity.SourceTUI, Operation: "plan.retry-failed", State: activity.StateCanceled},
		{ID: "refresh", Source: activity.SourceTUI, Operation: "worklogs.refresh", State: activity.StateSucceeded},
		{ID: "launcher", Source: activity.SourceCLI, Operation: "tui", State: activity.StateRunning},
		{ID: "list", Source: activity.SourceCLI, Operation: "worklogs.list", State: activity.StateSucceeded},
	}

	next, _ := m.Update(activityListResultMsg{generation: m.activityGen, items: items})
	m = next.(model)
	if got, want := len(m.activities), 3; got != want {
		t.Fatalf("visible activity count = %d, want %d: %+v", got, want, m.activities)
	}
	for index, want := range []string{"delete", "cli-add", "retry"} {
		if got := m.activities[index].ID; got != want {
			t.Fatalf("visible activity %d = %q, want %q", index, got, want)
		}
	}

	m.upsertActivity(activity.Entry{ID: "status-refresh", Source: activity.SourceTUI, Operation: "status.refresh"})
	if got := len(m.activities); got != 3 {
		t.Fatalf("internal optimistic activity became visible: %+v", m.activities)
	}
}

func TestActivityMouseHitAreaIsLimitedToRightPanel(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.activityOpen = true
	m.activities = []activity.Entry{{ID: "one"}}
	activityTop := m.height - 3 - activityDrawerHeight

	next, _ := m.Update(mouseClick(1, activityTop+2, tea.MouseLeft))
	m = next.(model)
	if m.focus == focusActivity || m.focus == focusRail {
		t.Fatalf("left rail click activityFocused=%t railFocused=%t", m.focus == focusActivity, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(31, activityTop+2, tea.MouseLeft))
	m = next.(model)
	if m.focus != focusActivity || m.focus == focusRail {
		t.Fatalf("right drawer click activityFocused=%t railFocused=%t", m.focus == focusActivity, m.focus == focusRail)
	}
}

func TestActivityCommitDoesNotStaleOpenEditor(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	next, cmd := m.Update(pollResultMsg{activityChanged: true})
	m = next.(model)
	if m.form == nil || m.form.stale || m.reloadAfterForm {
		t.Fatalf("activity-only change affected editor: %+v", m.form)
	}
	if cmd == nil {
		t.Fatal("activity-only change did not schedule polling")
	}
}

func TestActivityShortcutAppearsInIdleActionBar(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	for _, tab := range []tab{statusTab, worklogsTab, trashTab, presetsTab} {
		m.activeTab = tab
		footer := m.renderFooter(m.width)
		if !strings.Contains(footer, "g Activity") {
			t.Fatalf("tab %d action bar missing activity shortcut: %q", tab, footer)
		}
		if strings.Index(footer, "g Activity") < strings.Index(footer, "q Quit") {
			t.Fatalf("tab %d activity shortcut is not at the end: %q", tab, footer)
		}
	}
	m.activityOpen = true
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "g Close activity") {
		t.Fatalf("open drawer action bar missing close shortcut: %q", footer)
	}
	m.focus = focusActivity
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "g Close activity") {
		t.Fatalf("focused drawer action bar missing close shortcut: %q", footer)
	}
}
