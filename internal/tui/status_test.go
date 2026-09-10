package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/status"
)

func TestNewModelChoosesWorklogsOrDegradedStatus(t *testing.T) {
	ws, deps := testWorkspace(t)
	healthy := newModel(context.Background(), deps, ws, nil)
	if healthy.activeTab != worklogsTab {
		t.Fatalf("healthy active tab = %v, want worklogs", healthy.activeTab)
	}

	degraded := newModel(context.Background(), deps, nil, errors.New("config invalid"))
	if degraded.activeTab != statusTab || degraded.workspaceErr == nil {
		t.Fatalf("unexpected degraded model %#v", degraded)
	}
}

func TestWorkspaceOpensAsCommandAndFailureEntersDegradedMode(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, nil, nil)
	if m.activeTab != worklogsTab || !m.workspaceLoading {
		t.Fatalf("startup state tab=%v loading=%v", m.activeTab, m.workspaceLoading)
	}
	result := m.workspaceCmd(m.workspaceGen)().(workspaceResultMsg)
	next, _ := m.Update(result)
	m = next.(model)
	if m.workspace != ws || m.workspaceLoading || m.activeTab != worklogsTab {
		t.Fatalf("healthy workspace result not applied: %#v", m)
	}

	deps.openWorkspace = func(context.Context) (*workspace, error) { return nil, errors.New("storage unavailable") }
	m = newModel(context.Background(), deps, nil, nil)
	result = m.workspaceCmd(m.workspaceGen)().(workspaceResultMsg)
	next, _ = m.Update(result)
	m = next.(model)
	if m.workspace != nil || m.workspaceLoading || m.activeTab != statusTab || !strings.Contains(m.workspaceErr.Error(), "storage unavailable") {
		t.Fatalf("degraded workspace result not applied: %#v", m)
	}
}

func TestStaleWorkspaceResultIsClosed(t *testing.T) {
	ws, deps := testWorkspace(t)
	closed := false
	ws.close = func() error { closed = true; return nil }
	m := newModel(context.Background(), deps, nil, nil)
	m.workspaceGen = 2
	next, _ := m.Update(workspaceResultMsg{generation: 1, workspace: ws})
	m = next.(model)
	if !closed || m.workspace != nil {
		t.Fatalf("stale workspace was not discarded and closed")
	}
}

func TestStatusGenerationProtectsAgainstLateResults(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.statusGen = 4
	late := statusResultMsg{generation: 3, report: status.Report{Items: []status.Item{{Target: "late"}}}}
	next, _ := m.Update(late)
	m = next.(model)
	if len(m.statusItems) != 0 {
		t.Fatalf("late status result overwrote state: %#v", m.statusItems)
	}

	current := statusResultMsg{generation: 4, report: status.Report{Items: []status.Item{{Target: "current", Status: "ok"}}}}
	next, _ = m.Update(current)
	m = next.(model)
	if len(m.statusItems) != 1 || m.statusItems[0].Target != "current" {
		t.Fatalf("current result not applied: %#v", m.statusItems)
	}
}

func TestStatusRefreshCancelsPreviousRequestAndRendersFailure(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	previous := m.statusCtx
	_ = m.startStatusRefresh()
	if !errors.Is(previous.Err(), context.Canceled) {
		t.Fatalf("previous status request was not cancelled")
	}
	next, _ := m.Update(statusResultMsg{generation: m.statusGen, err: errors.New("adapter exploded"), checkedAt: deps.now()})
	m = next.(model)
	m.activeTab = statusTab
	rendered := m.render()
	for _, expected := range []string{"Check failed", "adapter exploded", "Status check failed"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("status failure render missing %q", expected)
		}
	}
}

func TestStatusSwitchesBetweenDiagnosticsAndReadOnlyConfig(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.activeTab = statusTab
	summary := config.ConfigSummary{
		ConfigPath:               "/tmp/config.yaml",
		DefaultOutput:            "table",
		SQLitePath:               "/tmp/workledger.db",
		LocalTimezone:            "UTC",
		MinimumDurationSeconds:   900,
		DailyMinimumQuotaSeconds: 28800,
		DayStart:                 "08:00",
		DayEnd:                   "17:00",
		DailyLunch:               "12:00-12:45",
	}
	next, _ := m.Update(statusResultMsg{
		generation: m.statusGen,
		report: status.Report{
			Items:         []status.Item{{Category: "local", Target: "config", Status: "ok"}},
			ConfigSummary: &summary,
		},
		checkedAt: deps.now(),
	})
	m = next.(model)

	next, _ = m.Update(textKey("c"))
	m = next.(model)
	rendered := m.render()
	for _, expected := range []string{"[Config]", "Effective configuration", "config_path", "/tmp/config.yaml", "Read-only effective configuration."} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("config view missing %q: %q", expected, rendered)
		}
	}

	next, _ = m.Update(textKey("j"))
	m = next.(model)
	if m.statusConfigSelected != 1 || !strings.Contains(m.render(), "Field default_output") {
		t.Fatalf("config selection did not move: selected=%d render=%q", m.statusConfigSelected, m.render())
	}
	next, _ = m.Update(specialKey(tea.KeyLeft))
	m = next.(model)
	if m.statusView != statusDiagnosticsView || !strings.Contains(m.render(), "[Status]") {
		t.Fatalf("left did not restore diagnostics view: view=%v", m.statusView)
	}
}

func TestStatusConfigShowsValidationIssuesInDegradedMode(t *testing.T) {
	_, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, nil, errors.New("workspace unavailable"))
	m.width, m.height = minimumWidth, minimumHeight
	next, _ := m.Update(statusResultMsg{
		generation: m.statusGen,
		report: status.Report{
			Items:        []status.Item{{Category: "local", Target: "config", Status: "error"}},
			ConfigIssues: []config.ValidationIssue{{Field: "local_timezone", Message: "unknown timezone"}},
		},
		checkedAt: deps.now(),
	})
	m = next.(model)
	next, _ = m.Update(textKey("c"))
	m = next.(model)
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("degraded config render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"Configuration validation errors", "local_timezone", "unknown timezone", "Validation issue"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("degraded config view missing %q: %q", expected, rendered)
		}
	}
}

func TestNumberedTabActivationFocusesWorkspace(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.focus = focusRail

	next, _ := m.Update(textKey("1"))
	m = next.(model)
	if m.activeTab != statusTab || m.focus == focusRail || m.focus == focusActivity {
		t.Fatalf("status shortcut tab=%v railFocused=%v activityFocused=%v", m.activeTab, m.focus == focusRail, m.focus == focusActivity)
	}

	m.activityOpen = true
	m.focus = focusActivity
	next, _ = m.Update(textKey("2"))
	m = next.(model)
	if m.activeTab != worklogsTab || m.focus == focusRail || m.focus == focusActivity {
		t.Fatalf("worklogs shortcut tab=%v railFocused=%v activityFocused=%v", m.activeTab, m.focus == focusRail, m.focus == focusActivity)
	}
}
