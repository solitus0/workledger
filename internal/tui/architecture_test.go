package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/activity"
)

func TestLayoutOwnsRenderAndMouseGeometry(t *testing.T) {
	layout := calculateLayout(120, 40, true)
	if layout.rail.width != 30 || layout.workspace.x != 31 {
		t.Fatalf("rail=%+v workspace=%+v", layout.rail, layout.workspace)
	}
	if !layout.activity.contains(31, layout.activity.y) || layout.workspace.contains(31, layout.activity.y) {
		t.Fatalf("activity and workspace rectangles overlap or leave the right panel: %+v", layout)
	}

	ws, deps := testWorkspace(t)
	m := newModel(t.Context(), deps, ws, nil)
	m.width, m.height = 120, 40
	_, cards := m.renderRailLayout(layout.rail.height)
	wantTabs := [...]tab{statusTab, worklogsTab, plansTab, presetsTab, trashTab}
	for index, card := range cards {
		next, _ := m.updateMouse(mouseClick(card.x+1, card.y+card.height/2, tea.MouseLeft))
		if got := next.(model).activeTab; got != wantTabs[index] {
			t.Fatalf("rendered rail card %d selected tab %v, want %v", index, got, wantTabs[index])
		}
	}
}

func TestMutationRefreshEffectsStayCapabilityScoped(t *testing.T) {
	if got := refreshForMutation("add"); got != refreshWorklogs {
		t.Fatalf("add refresh = %v", got)
	}
	if got := refreshForMutation("delete"); got != refreshWorklogs|refreshTrash {
		t.Fatalf("delete refresh = %v", got)
	}
	if got := refreshForMutation("add") | refreshPresets; got != refreshWorklogs|refreshPresets {
		t.Fatalf("preset-backed add refresh = %v", got)
	}
}

func TestInteractionStateChoosesOneActiveMode(t *testing.T) {
	state := interactionState{form: &formState{}, presetForm: &presetFormState{}, planOperation: &planOperationState{}}
	if got := state.kind(); got != interactionPlanOperation {
		t.Fatalf("active interaction = %v, want plan operation", got)
	}
	state.overlay = helpOverlay
	if got := state.kind(); got != interactionHelp {
		t.Fatalf("active interaction = %v, want help", got)
	}
	state = interactionState{}
	if got := state.kind(); got != interactionIdle {
		t.Fatalf("active interaction = %v, want idle", got)
	}
}

func TestUnknownTrackedResultFailsClosed(t *testing.T) {
	state, code, message := activityOutcome(struct{}{})
	if state != activity.StateFailed || code != "unexpected_result" || message == "" {
		t.Fatalf("unknown outcome = (%q, %q, %q)", state, code, message)
	}
}

func TestRefreshScopesOnlyStartRequestedCapabilities(t *testing.T) {
	ws, deps := testWorkspace(t)
	ws.activities = &fakeActivities{}
	assertChanged := func(scope refreshScope, worklogs, trash, presets, plans, activities bool) {
		t.Helper()
		m := newModel(t.Context(), deps, ws, nil)
		beforeWorklogs, beforeTrash, beforePresets, beforePlans, beforeActivities := m.worklogGen, m.trashGen, m.presetGen, m.planGen, m.activityGen
		if cmd := m.refreshCmd(scope); cmd == nil {
			t.Fatalf("refresh %v returned no command", scope)
		}
		if (m.worklogGen != beforeWorklogs) != worklogs || (m.trashGen != beforeTrash) != trash ||
			(m.presetGen != beforePresets) != presets || (m.planGen != beforePlans) != plans ||
			(m.activityGen != beforeActivities) != activities {
			t.Fatalf("refresh %v changed generations worklogs=%t trash=%t presets=%t plans=%t activities=%t", scope,
				m.worklogGen != beforeWorklogs, m.trashGen != beforeTrash, m.presetGen != beforePresets,
				m.planGen != beforePlans, m.activityGen != beforeActivities)
		}
	}

	assertChanged(refreshWorklogs, true, false, false, false, false)
	assertChanged(refreshDateData, true, true, false, true, false)
	assertChanged(refreshActivity, false, false, false, false, true)
	assertChanged(refreshWorkspace, true, true, true, true, true)
}
