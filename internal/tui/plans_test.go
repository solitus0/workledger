package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile"
)

func TestPlansTabReviewsSavedPlanAndGuardsApply(t *testing.T) {
	ws, deps := testWorkspace(t)
	plan := reconcile.Plan{
		ID: "plan-1", Direction: "push", PlanningStatus: "ready", ExecutionState: "ready",
		WindowFromUTC: deps.now(), WindowToUTC: deps.now().Add(time.Hour),
		Items: []reconcile.PlanItem{{
			ID: "item-1", PlanID: "plan-1", PlanStatus: "ready", PlannedAction: "replace", ComparisonStatus: "remote_diff",
			ExecutionState: "not_attempted", TargetAdapterFamily: "clockify", TargetAdapterInstance: "clockify", TargetIssue: "APP-1",
			LocalRowCount: 2, RemoteRowCount: 1,
			InspectionSummary: reconcile.InspectionSummary{CreateRowCount: 2, DeleteRowCount: 1},
		}},
	}
	plans := ws.plans.(*fakePlans)
	plans.items = []reconcile.ListEntry{{ID: plan.ID, Direction: plan.Direction, PlanningStatus: plan.PlanningStatus, ExecutionState: plan.ExecutionState, WindowFromUTC: plan.WindowFromUTC, WindowToUTC: plan.WindowToUTC, ActionableItems: 1, OpenItems: 1, TotalItems: 1}}
	plans.plan = plan

	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.plans = plans.items
	next, _ := m.Update(textKey("3"))
	m = next.(model)
	if m.activeTab != plansTab || !strings.Contains(m.render(), "Saved plans") {
		t.Fatalf("plans tab did not open: tab=%v", m.activeTab)
	}
	next, cmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.plan == nil || !strings.Contains(m.render(), "clockify/APP-1") {
		t.Fatalf("saved plan was not rendered: %#v", m.plan)
	}
	if footer := m.renderFooter(m.width); strings.Contains(footer, "Retry failed") || strings.Contains(footer, "Retry uncertain") {
		t.Fatalf("plan without retryable scopes shows retry actions:\n%s", footer)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "A Apply") {
		t.Fatalf("plan with an unapplied ready scope hides apply action:\n%s", footer)
	}
	next, _ = m.Update(textKey("A"))
	m = next.(model)
	if m.overlay != applyPlanOverlay {
		t.Fatal("apply confirmation did not open")
	}
	confirmation := m.render()
	for _, expected := range []string{"Apply saved plan?", "Creates    2", "Deletes    1", "Remote worklogs may be created or deleted"} {
		if !strings.Contains(confirmation, expected) {
			t.Fatalf("confirmation missing %q:\n%s", expected, confirmation)
		}
	}
}

func TestPlansDefaultToLocalWeekAndSwitchToDay(t *testing.T) {
	ws, deps := testWorkspace(t)
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	ws.cfg.Location = location
	deps.now = func() time.Time { return time.Date(2026, 3, 29, 12, 0, 0, 0, location) }
	plans := ws.plans.(*fakePlans)
	plans.items = []reconcile.ListEntry{
		{ID: "recent", CreatedAt: time.Date(2026, 3, 29, 10, 0, 0, 0, location)},
		{ID: "week-start", CreatedAt: time.Date(2026, 3, 23, 10, 0, 0, 0, location)},
		{ID: "old", CreatedAt: time.Date(2026, 3, 20, 10, 0, 0, 0, location)},
	}

	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab
	cmd := m.startPlanLoad()
	next, _ := m.Update(cmd())
	m = next.(model)

	if m.planListView != planWeekListView || len(plans.rangeCalls) != 1 {
		t.Fatalf("default plan view=%v calls=%#v", m.planListView, plans.rangeCalls)
	}
	wantFrom := time.Date(2026, 3, 23, 0, 0, 0, 0, location).UTC()
	wantTo := time.Date(2026, 3, 30, 0, 0, 0, 0, location).UTC()
	call := plans.rangeCalls[0]
	if call.limit != planListLimit || !call.from.Equal(wantFrom) || !call.to.Equal(wantTo) || call.to.Sub(call.from) != 167*time.Hour {
		t.Fatalf("default range=%#v, want [%s, %s) across DST", call, wantFrom, wantTo)
	}
	view := m.renderPlanListWorkspace(89, 33)
	if len(m.plans) != 2 || m.plans[0].ID != "recent" || !strings.Contains(view, "2026-03-23–2026-03-29") || !strings.Contains(view, "[Week]") {
		t.Fatalf("default plans=%#v", m.plans)
	}

	next, cmd = m.Update(textKey("d"))
	m = next.(model)
	if m.planListView != planDayListView || len(m.plans) != 0 || m.planSelected != 0 {
		t.Fatalf("day switch did not clear range state: %#v", m)
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(plans.rangeCalls) != 2 || len(m.plans) != 1 || m.plans[0].ID != "recent" {
		t.Fatalf("day plans=%#v calls=%#v", m.plans, plans.rangeCalls)
	}
	dayCall := plans.rangeCalls[1]
	wantDayFrom := time.Date(2026, 3, 29, 0, 0, 0, 0, location).UTC()
	wantDayTo := time.Date(2026, 3, 30, 0, 0, 0, 0, location).UTC()
	if !dayCall.from.Equal(wantDayFrom) || !dayCall.to.Equal(wantDayTo) || dayCall.to.Sub(dayCall.from) != 23*time.Hour {
		t.Fatalf("day range=%#v, want [%s, %s) across DST", dayCall, wantDayFrom, wantDayTo)
	}
	if view = m.renderPlanListWorkspace(89, 33); !strings.Contains(view, "Sunday, 2026-03-29") || !strings.Contains(view, "[Day]") || !strings.Contains(m.renderFooter(m.width), "d/w View") {
		t.Fatalf("day labels missing:\n%s", view)
	}
}

func TestPlansSameRangeRefreshPreservesSelection(t *testing.T) {
	ws, deps := testWorkspace(t)
	plans := ws.plans.(*fakePlans)
	plans.items = []reconcile.ListEntry{
		{ID: "newest", CreatedAt: deps.now()},
		{ID: "selected", CreatedAt: deps.now().Add(-time.Hour)},
	}
	m := newModel(context.Background(), deps, ws, nil)
	m.plans = append([]reconcile.ListEntry(nil), plans.items...)
	m.planSelected = 1

	cmd := m.startPlanLoad()
	next, _ := m.Update(cmd())
	m = next.(model)
	if selected := m.selectedPlanEntry(); selected == nil || selected.ID != "selected" {
		t.Fatalf("same-range refresh lost selection: %#v", selected)
	}
}

func TestPlansRailSummarizesActionableVisibleScopes(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.planLoading = false
	m.plans = []reconcile.ListEntry{
		{NotAttemptedItems: 2, FailedItems: 1, UncertainItems: 3},
		{NotAttemptedItems: 1, FailedItems: 1},
	}

	rail := stripANSI(m.renderRail(38))
	for _, expected := range []string{"Unapplied  3", "Failed     2", "Uncertain  3"} {
		if !strings.Contains(rail, expected) {
			t.Fatalf("plans rail missing %q:\n%s", expected, rail)
		}
	}
	if strings.Contains(rail, "Recent") || strings.Contains(rail, "Selected   push") {
		t.Fatalf("plans rail retained static summary:\n%s", rail)
	}

	compact := stripANSI(m.renderRail(28))
	if !strings.Contains(compact, "3 apply · 5 retry") {
		t.Fatalf("compact plans rail missing combined counts:\n%s", compact)
	}
}

func TestEmptyPlanWindowOffersCreation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.planLoading = false
	view := m.renderPlanListWorkspace(89, 33)
	if !strings.Contains(view, "No saved plans in this week") || strings.Contains(view, "Press n") || strings.Contains(view, "press Enter") {
		t.Fatalf("week empty state should not duplicate action-bar shortcuts:\n%s", view)
	}

	m.planLoading = true
	if loading := m.renderPlanListWorkspace(89, 33); !strings.Contains(loading, "Loading…") || strings.Contains(loading, "No saved plans in this week") {
		t.Fatalf("loading state rendered as empty:\n%s", loading)
	}

	m.planLoading = false
	m.plans = []reconcile.ListEntry{{ID: "retained", CreatedAt: deps.now()}}
	ws.plans.(*fakePlans).err = errors.New("plan range failed")
	cmd := m.startPlanLoad()
	next, _ := m.Update(cmd())
	m = next.(model)
	if m.planLoading || len(m.plans) != 1 || m.plans[0].ID != "retained" || !strings.Contains(m.notice, "plan range failed") {
		t.Fatalf("failed range refresh did not retain the visible list: plans=%#v notice=%q loading=%v", m.plans, m.notice, m.planLoading)
	}
}

func TestPlanActionBarShowsOnlyAvailableRetryScopes(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab
	m.plan = &reconcile.Plan{Items: []reconcile.PlanItem{
		{PlanStatus: "ready", ExecutionState: "failed"},
		{PlanStatus: "ready", ExecutionState: "succeeded"},
	}}

	footer := m.renderFooter(m.width)
	if strings.Contains(footer, "A Apply") {
		t.Fatalf("plan without an unapplied ready scope shows apply action:\n%s", footer)
	}
	if !strings.Contains(footer, "f Retry failed") {
		t.Fatalf("failed retry action missing:\n%s", footer)
	}
	if strings.Contains(footer, "u Retry uncertain") {
		t.Fatalf("unavailable uncertain retry action shown:\n%s", footer)
	}

	m.plan.Items = append(m.plan.Items, reconcile.PlanItem{PlanStatus: "ready", ExecutionState: "uncertain"})
	if footer = m.renderFooter(m.width); !strings.Contains(footer, "f Retry failed") || !strings.Contains(footer, "u Retry uncertain") {
		t.Fatalf("available retry actions missing:\n%s", footer)
	}

	m.plan.Items = append(m.plan.Items, reconcile.PlanItem{PlanStatus: "ready", ExecutionState: "not_attempted"})
	if footer = m.renderFooter(m.width); !strings.Contains(footer, "A Apply") {
		t.Fatalf("available apply action missing:\n%s", footer)
	}
}

func TestNewPlanFormUsesExplicitDirectionWindowAndTarget(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab

	next, _ := m.Update(textKey("n"))
	m = next.(model)
	if m.planForm == nil || m.planForm.direction != "push" || len(m.planForm.targets) != 1 {
		t.Fatalf("unexpected plan form: %#v", m.planForm)
	}
	next, _ = m.Update(textKey("u"))
	m = next.(model)
	next, _ = m.Update(textKey("w"))
	m = next.(model)
	next, _ = m.Update(textKey("x"))
	m = next.(model)
	request := m.planReconcileRequest()
	if request.Direction != "pull" || len(request.Adapters) != 1 || request.Adapters[0] != "clockify" || request.WindowTo.Sub(request.WindowFrom) < 6*24*time.Hour {
		t.Fatalf("unexpected reconcile request: %#v", request)
	}
	next, _ = m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.overlay != reconcilePlanOverlay || !strings.Contains(m.render(), "It does not apply changes") {
		t.Fatal("reconcile confirmation did not explain plan-only behavior")
	}
}

func TestNewPlanFormShowsShortcutsOnlyInActionBar(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab
	m.openPlanForm()

	if body := m.renderPlanForm(); strings.Contains(body, "p Push") || strings.Contains(body, "Enter Inspect") || strings.Contains(body, "Esc Cancel") {
		t.Fatalf("plan form body duplicates action-bar shortcuts:\n%s", body)
	}
	rendered := m.render()
	for _, shortcut := range []string{"p Push", "Enter Inspect", "Esc Cancel"} {
		if strings.Count(rendered, shortcut) != 1 {
			t.Fatalf("shortcut %q should appear once in the action bar:\n%s", shortcut, rendered)
		}
	}
}

func TestNewPlanFormAcceptsInclusiveCustomWindowInEitherOrder(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab
	m.openPlanForm()
	next, _ := m.Update(textKey("c"))
	m = next.(model)
	m.planForm.dateInputs[0].SetValue("2026-05-23")
	m.planForm.dateInputs[1].SetValue("2026-05-21")
	request := m.planReconcileRequest()
	if got := request.WindowFrom.In(time.UTC).Format("2006-01-02"); got != "2026-05-21" {
		t.Fatalf("custom window from=%s", got)
	}
	if got := request.WindowTo.In(time.UTC).Format("2006-01-02 15:04:05"); got != "2026-05-23 23:59:59" {
		t.Fatalf("custom window to=%s", got)
	}
}

func TestRunningPlanOperationOnlyAcceptsCancellation(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.activeTab = plansTab
	canceled := false
	m.planOperation = &planOperationState{kind: "plan.apply", cancel: func() { canceled = true }}

	next, _ := m.Update(textKey("3"))
	m = next.(model)
	if m.activeTab != plansTab || canceled {
		t.Fatal("running operation accepted unrelated navigation")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if !canceled || !strings.Contains(m.notice, "canceling") {
		t.Fatal("Esc did not request plan operation cancellation")
	}
}

func TestNewPlanFormSelectsExplicitJiraRouteProfile(t *testing.T) {
	ws, deps := testWorkspace(t)
	ws.cfg.File.JiraCloud = &config.JiraCloudConfig{Instances: map[string]config.JiraCloudInstance{
		"company": {
			BaseURL: "https://jira.example.test",
			Auth:    config.JiraCloudAuthBlock{Email: "operator@example.test", Token: "secret"},
			Routing: &config.JiraInstanceRoutes{Profiles: map[string]config.JiraRouteProfile{
				"default":   {IssuePrefixes: []string{"APP"}},
				"reporting": {ReportingTargets: map[string]string{"APP": "REPORT-1"}},
			}},
		},
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.openPlanForm()
	for index, target := range m.planForm.targets {
		if target.AdapterFamily == "jira-cloud" {
			m.planForm.targetIndex = index
			break
		}
	}
	m.resetPlanProfiles()
	if len(m.planForm.profiles) != 2 {
		t.Fatalf("route profiles=%v targets=%v", m.planForm.profiles, m.planForm.targets)
	}
	next, _ := m.Update(textKey("v"))
	m = next.(model)
	request := m.planReconcileRequest()
	if len(request.Adapters) != 1 || request.Adapters[0] != "jira-cloud" || request.RouteProfile != "default" {
		t.Fatalf("unexpected routed request: %#v", request)
	}
}

func TestPlansViewsFitMinimumTerminal(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.activeTab = plansTab
	m.plans = []reconcile.ListEntry{{ID: "plan-1", Direction: "push", PlanningStatus: "ready", ExecutionState: "ready", CreatedAt: deps.now(), WindowFromUTC: deps.now(), WindowToUTC: deps.now()}}
	cases := []struct {
		name    string
		prepare func()
	}{
		{name: "list", prepare: func() { m.planForm, m.plan = nil, nil }},
		{name: "form", prepare: func() { m.plan = nil; m.openPlanForm() }},
		{name: "detail", prepare: func() {
			m.planForm = nil
			m.plan = &reconcile.Plan{ID: "plan-1", Direction: "push", WindowFromUTC: deps.now(), WindowToUTC: deps.now()}
		}},
	}
	for _, testCase := range cases {
		testCase.prepare()
		rendered := m.render()
		if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
			t.Fatalf("%s plans render=%dx%d, want %dx%d", testCase.name, width, height, minimumWidth, minimumHeight)
		}
	}
}
