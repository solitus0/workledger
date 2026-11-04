package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile"
)

func TestPlanLastExecutionUsesSavedAttemptAndLocalTime(t *testing.T) {
	ws, deps := testWorkspace(t)
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	ws.cfg.Location = location
	m := newModel(t.Context(), deps, ws, nil)
	m.plan = &reconcile.Plan{ID: "plan", Direction: "push"}
	for _, state := range []string{"pending", "failed", "succeeded", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			item := reconcile.PlanItem{PlanStatus: "ready", ExecutionState: state, LatestAttempt: &reconcile.DeliveryAttempt{
				State: state, CreatedAt: time.Date(2026, 10, 21, 10, 15, 30, 0, time.UTC), Message: "Saved delivery result",
			}}
			view := stripANSI(m.renderPlanItemInspector(item, 32, 28))
			for _, want := range []string{"Last execution", "State " + displayState(state), "2026-10-21 13:15:30 EEST", "Saved delivery result", "Planned destination"} {
				if !strings.Contains(view, want) {
					t.Fatalf("inspector missing %q:\n%s", want, view)
				}
			}
			if strings.Contains(view, "After apply") {
				t.Fatalf("projection labelled as an actual outcome:\n%s", view)
			}
		})
	}
	item := reconcile.PlanItem{PlanStatus: "ready", ExecutionState: "not_attempted"}
	if view := m.renderPlanItemInspector(item, 32, 28); !strings.Contains(view, "Not applied") {
		t.Fatalf("missing empty execution state:\n%s", view)
	}
	item.ExecutionState = "uncertain"
	item.LatestAttempt = &reconcile.DeliveryAttempt{State: "pending", CreatedAt: deps.now(), Message: "push delivery started"}
	if view := m.renderPlanItemInspector(item, 32, 28); !strings.Contains(view, "State Uncertain") || !strings.Contains(view, "no confirmed outcome") {
		t.Fatalf("stale pending attempt did not explain uncertainty:\n%s", view)
	}

	item.LatestAttempt.Message = strings.Repeat("Long remote error message. ", 100) + "Finalexecutionnote"
	m.plan.Items = []reconcile.PlanItem{item}
	m.activeTab, m.width, m.height = plansTab, 100, 28
	next, _ := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.overlay != detailsOverlay || !strings.Contains(m.detailContent, item.LatestAttempt.Message) {
		t.Fatal("scrollable scope details lost the full execution message")
	}
}

func TestPlanRetryRefreshesExecutionAndPreservesScopeSelection(t *testing.T) {
	for _, outcome := range []string{"progress", "success", "failure", "cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			ws, deps := testWorkspace(t)
			untouched := reconcile.PlanItem{ID: "done", PlanStatus: "ready", ExecutionState: "succeeded", LatestAttempt: &reconcile.DeliveryAttempt{
				State: "succeeded", CreatedAt: deps.now().Add(-time.Hour), Message: "Earlier success",
			}}
			selected := reconcile.PlanItem{ID: "retry", PlanStatus: "ready", ExecutionState: "failed", TargetIssue: "APP-2", LatestAttempt: &reconcile.DeliveryAttempt{
				State: "failed", CreatedAt: deps.now().Add(-time.Hour), Message: "Earlier failure",
			}}
			m := newModel(t.Context(), deps, ws, nil)
			m.activeTab, m.width, m.height = plansTab, 120, 36
			m.plan = &reconcile.Plan{ID: "plan", Direction: "push", Items: []reconcile.PlanItem{untouched, selected}}
			m.planItemSelected = 0
			m.planOperation = &planOperationState{kind: "plan.retry-failed", planID: "plan"}
			if view := stripANSI(m.renderPlansWorkspace(89, 30)); !strings.Contains(view, "Last execution") || !strings.Contains(view, "Retry in progress") || !strings.Contains(view, "Earlier failure") {
				t.Fatalf("retry replaced the scope review:\n%s", view)
			}

			state, message := "succeeded", "Retry completed"
			var resultErr error
			switch outcome {
			case "failure":
				state, message = "failed", "New remote failure"
				resultErr = errors.New("remote failure")
			case "cancellation":
				state, message = "uncertain", "context canceled"
				resultErr = context.Canceled
			}
			selected.ExecutionState = state
			selected.LatestAttempt = &reconcile.DeliveryAttempt{State: state, Message: message, CreatedAt: deps.now()}
			// Reordering ensures the selection is restored by identity, not index.
			ws.plans.(*fakePlans).plan = reconcile.Plan{ID: "plan", Direction: "push", Items: []reconcile.PlanItem{selected, untouched}}
			var next tea.Model
			var cmd tea.Cmd
			if outcome == "progress" {
				ch := make(chan progress.Event)
				close(ch)
				next, cmd = m.Update(planProgressMsg{event: progress.Event{ScopeDone: 1, ScopeTotal: 2}, ch: ch})
			} else {
				next, cmd = m.Update(planOperationResultMsg{kind: "plan.retry-failed", planID: "plan", err: resultErr})
			}
			m = next.(model)
			queue := []tea.Cmd{cmd}
			for len(queue) > 0 {
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
				case planLoadResultMsg, planListResultMsg:
					next, follow := m.Update(msg)
					m = next.(model)
					queue = append(queue, follow)
				}
			}
			if item := m.selectedPlanItem(); item == nil || item.ID != "retry" || item.LatestAttempt.Message != message || item.ExecutionState != state {
				t.Fatalf("retry did not refresh the selected scope: %#v", item)
			}
			if m.plan.Items[1].LatestAttempt != untouched.LatestAttempt {
				t.Fatal("retry changed an excluded scope's execution result")
			}
			view := stripANSI(m.renderPlansWorkspace(89, 30))
			if !strings.Contains(view, message) || (outcome != "progress" && strings.Contains(view, "Retry in progress")) {
				t.Fatalf("updated execution result missing or stale progress retained:\n%s", view)
			}
		})
	}
}
