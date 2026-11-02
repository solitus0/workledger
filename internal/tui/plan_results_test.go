package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/reconcile"
)

func TestTrackedPlanResultsRemainVisibleAfterActivityAndReload(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		applied     int
		failed      int
		err         error
		state       activity.State
	}{
		{"partial", "Plan sync partially completed", 1, 1, nil, activity.StatePartial},
		{"failed", "Plan sync failed", 0, 1, nil, activity.StateFailed},
		{"fatal after progress", "Plan sync partially completed", 1, 0, errors.New("storage failure"), activity.StatePartial},
		{"canceled", "Plan sync canceled", 1, 0, context.Canceled, activity.StateCanceled},
		{"complete", "Plan sync complete", 1, 0, nil, activity.StateSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			ws.activities = &fakeActivities{}
			m := newModel(t.Context(), deps, ws, nil)
			m.activeTab, m.width, m.height = plansTab, minimumWidth, minimumHeight
			plan := reconcile.Plan{ID: "plan", Direction: "push", Items: []reconcile.PlanItem{{ID: "scope", PlanStatus: "ready", ExecutionState: "failed"}}}
			m.plan = &plan
			canceled := false
			m.planOperation = &planOperationState{kind: "plan.apply", planID: plan.ID, cancel: func() { canceled = true }}
			r := reconcile.ApplyResult{AppliedCount: tc.applied, FailedCount: tc.failed, PlanID: plan.ID}
			if tc.applied > 0 {
				r.ScopeResults = append(r.ScopeResults, reconcile.ApplyScopeResult{AdapterFamily: "jira-cloud", AdapterInstance: "company", ScopeLabel: "APP-1", ExecutionState: "succeeded"})
			}
			if tc.failed > 0 {
				r.ScopeResults = append(r.ScopeResults, reconcile.ApplyScopeResult{AdapterFamily: "clockify", AdapterInstance: "clockify", ScopeLabel: "APP-1", ExecutionState: "failed", Message: "list projects: clockify request failed: 429 Too Many Requests"})
			}
			cmd := m.beginActivity("plan.apply", plan.ID, func() tea.Msg {
				return planOperationResultMsg{kind: "plan.apply", planID: plan.ID, apply: r, err: tc.err}
			})
			next, follow := m.Update(cmd())
			m = next.(model)
			if m.overlay != detailsOverlay || m.detailTitle != tc.title || m.notice != "" || !canceled || m.planOperation != nil || follow == nil {
				t.Fatalf("result disappeared after Activity: title=%q overlay=%v notice=%q", m.detailTitle, m.overlay, m.notice)
			}
			if m.activities[0].State != tc.state {
				t.Fatalf("activity = %v, want %v", m.activities[0].State, tc.state)
			}
			if tc.failed > 0 && (!strings.Contains(m.detailContent, "429 Too Many Requests") || !strings.Contains(m.detailContent, "clockify/clockify") || !strings.Contains(m.detailContent, "Failed or uncertain 1")) {
				t.Fatalf("partial results lost errors: %s", m.detailContent)
			}
			if tc.err != nil && !errors.Is(tc.err, context.Canceled) && !strings.Contains(m.detailContent, tc.err.Error()) {
				t.Fatal("actual fatal error missing")
			}
			next, _ = m.Update(planLoadResultMsg{generation: m.planGen, plan: plan})
			m = next.(model)
			if m.detailTitle != tc.title || m.overlay != detailsOverlay {
				t.Fatal("reload dismissed the result")
			}
			if view := stripANSI(m.render()); !strings.Contains(view, tc.title) || !strings.Contains(view, "Applied ") {
				t.Fatalf("result not visible at minimum viewport:\n%s", view)
			}
			next, _ = m.Update(specialKey(tea.KeyEsc))
			m = next.(model)
			if m.overlay != noOverlay || m.plan == nil || m.plan.ID != plan.ID {
				t.Fatal("dismissing result lost plan review")
			}
		})
	}
}

func TestPlanFailureDetailsRetainFullScrollableMessages(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	message := strings.Repeat("remote error details ", 100) + "EndOfRemoteError"
	r := reconcile.ApplyResult{FailedCount: 1, ScopeResults: []reconcile.ApplyScopeResult{{AdapterFamily: "clockify", AdapterInstance: "clockify", ScopeLabel: "APP-1", ExecutionState: "uncertain", Message: message}}}
	title, content := planExecutionResultDetails(planOperationResultMsg{apply: r})
	m.openDetails(title, content)
	m.detailViewport.GotoBottom()
	if !strings.Contains(m.detailContent, message) || !strings.Contains(stripANSI(m.render()), "EndOfRemoteError") {
		t.Fatal("full error is inaccessible")
	}
}

func TestTrackedReconcileFailureAndPartialInspectionExposeErrors(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ws, deps := testWorkspace(t)
		ws.activities = &fakeActivities{}
		m := newModel(t.Context(), deps, ws, nil)
		failure := errors.New("Clockify preparation failed: 429 Too Many Requests")
		title := "Plan operation failed"
		wantError := failure.Error()
		if canceled {
			failure, title = context.Canceled, "Plan operation canceled"
			wantError = "canceled"
		}
		cmd := m.beginActivity("plan.reconcile", "push", func() tea.Msg {
			return planOperationResultMsg{kind: "plan.reconcile", err: failure}
		})
		next, _ := m.Update(cmd())
		m = next.(model)
		if m.overlay != detailsOverlay || m.detailTitle != title || !strings.Contains(m.detailContent, wantError) {
			t.Fatalf("reconcile failure disappeared: %s", m.detailContent)
		}
	}
	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	plan := &reconcile.Plan{ID: "plan", Items: []reconcile.PlanItem{{PlanStatus: "check_failed", TargetAdapterFamily: "clockify", TargetAdapterInstance: "clockify", ReasonDetail: "429 Too Many Requests"}}}
	next, _ := m.Update(planOperationResultMsg{kind: "plan.reconcile", reconcile: reconcile.ReconcileResult{Plan: plan}})
	m = next.(model)
	if m.plan != plan || m.overlay != detailsOverlay || !strings.Contains(m.detailContent, "429") {
		t.Fatal("saved partial inspection errors disappeared")
	}
}
