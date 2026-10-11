package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestMouseSwitchesTabsAndPanelFocus(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	layout := calculateLayout(m.width, m.height, false)
	_, railRows := m.renderRailLayout(layout.rail.height)

	next, _ := m.Update(mouseClick(1, railRows[0].y, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != statusTab || m.focus == focusRail {
		t.Fatalf("status click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(layout.workspace.x, layout.workspace.y, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != statusTab || m.focus == focusRail {
		t.Fatalf("workspace click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(1, railRows[1].y, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != worklogsTab || m.focus == focusRail {
		t.Fatalf("worklogs click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	m.focus = focusRail
	next, _ = m.Update(mouseClick(layout.rail.width, layout.workspace.y, tea.MouseLeft))
	m = next.(model)
	if m.focus != focusRail {
		t.Fatal("separator click changed focus")
	}

	next, _ = m.Update(mouseClick(layout.workspace.x, layout.footer.y, tea.MouseLeft))
	m = next.(model)
	if m.focus != focusRail {
		t.Fatal("footer click changed focus")
	}

	next, _ = m.Update(mouseClick(layout.workspace.x, layout.footer.y-1, tea.MouseLeft))
	m = next.(model)
	if m.focus == focusRail {
		t.Fatal("bottom workspace cell did not receive focus")
	}

	next, _ = m.Update(mouseClick(1, railRows[2].y, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != plansTab || m.focus == focusRail {
		t.Fatalf("plans click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(1, railRows[4].y, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != trashTab || m.focus == focusRail {
		t.Fatalf("trash click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}
}

func TestIdleActionBarOmitsTabFocus(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32

	for _, destination := range []tab{statusTab, worklogsTab, plansTab, presetsTab, trashTab} {
		m.activeTab = destination
		if footer := m.renderFooter(m.width); strings.Contains(footer, "Tab Focus") {
			t.Fatalf("tab %d action bar contains Tab Focus: %q", destination, footer)
		}
	}
}

func TestMouseNavigationRespectsBlockingStatesAndUnavailableTabs(t *testing.T) {
	ws, deps := testWorkspace(t)
	tests := []struct {
		name  string
		setup func(*model)
		click tea.MouseClickMsg
	}{
		{name: "form", setup: func(m *model) { m.openAddForm() }, click: mouseClick(1, 1, tea.MouseLeft)},
		{name: "overlay", setup: func(m *model) { m.overlay = helpOverlay }, click: mouseClick(1, 1, tea.MouseLeft)},
		{name: "undersized", setup: func(m *model) { m.width = minimumWidth - 1 }, click: mouseClick(1, 1, tea.MouseLeft)},
		{name: "non-left", setup: func(*model) {}, click: mouseClick(1, 1, tea.MouseRight)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = 120, 32
			tt.setup(&m)
			next, _ := m.Update(tt.click)
			m = next.(model)
			if m.activeTab != worklogsTab || m.focus == focusRail {
				t.Fatalf("blocked click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
			}
		})
	}

	degraded := newModel(context.Background(), deps, nil, errors.New("storage unavailable"))
	degraded.width, degraded.height = 120, 32
	next, _ := degraded.Update(mouseClick(1, 10, tea.MouseLeft))
	degraded = next.(model)
	if degraded.activeTab != statusTab {
		t.Fatalf("unavailable worklogs click selected tab %v", degraded.activeTab)
	}
}

func TestViewEnablesClickMouseEvents(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion", got)
	}
}

func TestManualPreviewUsesSelectedDayAndRendersCalculatedInterval(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.previewRecords = []worklogs.LocalWorklog{{
		IssueKey: "WL-1234", StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.openAddForm()
	m.form.placement = manualPlacement
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[startInput].SetValue("12:00")
	m.form.inputs[durationInput].SetValue("1h")

	m = resolvePreview(t, m, m.schedulePreview())
	if len(service.previewInputs) != 1 {
		t.Fatalf("manual preview calls = %d, want 1", len(service.previewInputs))
	}
	input := service.previewInputs[0]
	if input.Fit || input.Fill || input.Started != "2026-05-21T12:00" || input.Duration != "1h" {
		t.Fatalf("manual preview input = %#v", input)
	}
	rendered := m.render()
	for _, expected := range []string{"░ proposed", "1 worklog · Manual", "12:00–13:00", "After"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("manual preview render missing %q:\n%s", expected, rendered)
		}
	}
	if timeline := m.renderDayTimeline(m.form.previewRecords, ""); !strings.Contains(timeline, "░") {
		t.Fatalf("manual interval is not visible in timeline: %s", timeline)
	}
}

func TestAutomaticPlacementShortcutsToggleOptionsAndRecalculate(t *testing.T) {
	ws, deps := testWorkspace(t)
	service := worklogState(ws)
	service.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.contextSettings.Lunch = &worklogs.ContextLunch{Start: "12:00", End: "12:45"}
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h")
	if rendered := m.render(); strings.Contains(rendered, "Options") {
		t.Fatalf("automatic options are duplicated in the detail view:\n%s", rendered)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+P Manual") || !strings.Contains(footer, "Ctrl+O OT on") || !strings.Contains(footer, "Ctrl+L Use lunch") {
		t.Fatalf("automatic option actions are missing from the footer: %s", footer)
	}

	next, cmd := m.Update(ctrlKey('o'))
	m = next.(model)
	if !m.form.overtime || m.overlay != noOverlay {
		t.Fatalf("overtime toggle state=%v overlay=%v", m.form.overtime, m.overlay)
	}
	m = resolvePreview(t, m, cmd)
	if input := service.previewInputs[len(service.previewInputs)-1]; !input.Overtime || input.NoLunch {
		t.Fatalf("overtime preview input = %#v", input)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+O OT off") || !strings.Contains(footer, "Ctrl+L Use lunch") {
		t.Fatalf("overtime footer action did not follow state: %s", footer)
	}

	next, cmd = m.Update(ctrlKey('l'))
	m = next.(model)
	if !m.form.overtime || !m.form.noLunch {
		t.Fatalf("automatic options were not preserved and toggled: %#v", m.form)
	}
	m = resolvePreview(t, m, cmd)
	if input := service.previewInputs[len(service.previewInputs)-1]; !input.Overtime || !input.NoLunch {
		t.Fatalf("combined preview input = %#v", input)
	}
	rendered := m.render()
	if strings.Contains(rendered, "Options") || strings.Contains(rendered, "╱ lunch") {
		t.Fatalf("enabled automatic options are duplicated or not reflected in the legend:\n%s", rendered)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+O OT off") || !strings.Contains(footer, "Ctrl+L Reserve lunch") {
		t.Fatalf("enabled footer actions did not follow state: %s", footer)
	}
	compactFooter := m.renderFooter(minimumWidth)
	if lipgloss.Width(compactFooter) != minimumWidth || !strings.Contains(compactFooter, "Ctrl+P Manual") || !strings.Contains(compactFooter, "Ctrl+O OT off") || !strings.Contains(compactFooter, "Ctrl+L Reserve lunch") {
		t.Fatalf("compact footer does not preserve state-dependent actions: %s", compactFooter)
	}

	next, cmd = m.changePlacement(-1)
	m = next.(model)
	if m.form.placement != fitPlacement || !m.form.overtime || !m.form.noLunch || cmd == nil {
		t.Fatalf("Fit/Fill switch did not preserve options: %#v", m.form)
	}
	next, cmd = m.Update(ctrlKey('o'))
	m = next.(model)
	m = resolvePreview(t, m, cmd)
	next, cmd = m.Update(ctrlKey('l'))
	m = next.(model)
	m = resolvePreview(t, m, cmd)
	if m.form.overtime || m.form.noLunch {
		t.Fatalf("automatic options did not toggle off: %#v", m.form)
	}
	next, _ = m.changePlacement(-1)
	m = next.(model)
	if m.form.placement != manualPlacement || strings.Contains(m.render(), "Ctrl+O") || strings.Contains(m.render(), "Ctrl+L") {
		t.Fatalf("manual placement exposed automatic options:\n%s", m.render())
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+P Fit") || strings.Contains(footer, "Ctrl+O") || strings.Contains(footer, "Ctrl+L") {
		t.Fatalf("manual placement footer has wrong actions: %s", footer)
	}
	next, cmd = m.Update(ctrlKey('o'))
	m = next.(model)
	if m.form.overtime || cmd != nil {
		t.Fatalf("manual placement accepted overtime toggle: state=%v cmd=%v", m.form.overtime, cmd)
	}
}

func TestOvertimeShortcutDoesNotCaptureTypedO(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.openAddForm()
	m.form.previewOvertimeAvailable = true
	m.focusForm(descriptionField)
	next, _ := m.Update(textKey("o"))
	m = next.(model)
	if got := m.form.inputs[descriptionInput].Value(); got != "o" {
		t.Fatalf("description = %q, want typed o", got)
	}
	if m.overlay != noOverlay {
		t.Fatalf("typed o opened overlay %v", m.overlay)
	}
}

func TestOnlyNavigationRailAndActionBarDefineNavigationShortcuts(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.worklogLoading = false
	m.presetLoading = false
	m.planLoading = false

	surfaces := map[string]string{
		"worklogs":          m.renderWorklogWorkspace(89, 33),
		"presets":           m.renderPresetWorkspace(89, 33),
		"plans":             m.renderPlanListWorkspace(89, 33),
		"undersized screen": func() string { m.width, m.height = 80, 20; return m.render() }(),
	}
	m.width, m.height = 120, 36
	m.openPlanForm()
	m.plan = &reconcile.Plan{ID: "plan-1", Direction: "push", Items: []reconcile.PlanItem{
		{PlanStatus: "ready", ExecutionState: "not_attempted"},
		{PlanStatus: "ready", ExecutionState: "failed"},
		{PlanStatus: "ready", ExecutionState: "uncertain"},
	}}
	for _, state := range []overlay{
		deleteOverlay, deleteDayOverlay, forceOverlay, reloadOverlay,
		restoreOverlay, restoreDayOverlay, deletePresetOverlay, reconcilePlanOverlay,
		applyPlanOverlay, retryFailedPlanOverlay, retryUncertainPlanOverlay,
	} {
		m.overlay = state
		content, _ := m.overlayContent()
		surfaces[fmt.Sprintf("overlay %d", state)] = m.renderOverlayCopy(content, m.colors.Focus)
	}
	m.overlay = noOverlay
	m.planOperation = &planOperationState{kind: "plan.apply"}
	surfaces["plan operation"] = m.renderPlanOperation()

	for name, surface := range surfaces {
		for _, shortcut := range []string{
			"1 Status", "2 Worklogs", "3 Plans", "4 Presets", "5 Trash",
			"Press a", "Press n", "press Enter", "q quit", "y / Enter", "n / Esc", "Esc Cancel",
			"Tab/Shift+Tab", "j/k row", "a add", "R restore",
		} {
			if strings.Contains(surface, shortcut) {
				t.Fatalf("%s defines shortcut %q outside the action bar:\n%s", name, shortcut, surface)
			}
		}
	}

	rail := m.renderRail(33)
	for _, shortcut := range []string{"1 Status", "2 Worklogs", "3 Plans", "4 Presets", "5 Trash"} {
		if strings.Count(rail, shortcut) != 1 {
			t.Fatalf("destination rail should define navigation shortcut %q once:\n%s", shortcut, rail)
		}
	}
}

func TestViewHandlesMinimumSizeAndNoColor(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.width, m.height = 80, 20
	if rendered := m.render(); !strings.Contains(rendered, "Terminal too small") {
		t.Fatalf("missing resize screen: %q", rendered)
	}
	m.width, m.height = 120, 32
	m.worklogs = testRows()
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != 120 || height != 32 {
		t.Fatalf("rendered size = %dx%d, want 120x32 (rail=%d workspace=%d footer=%d)", width, height,
			lipgloss.Height(m.renderRail(29)), lipgloss.Height(m.renderWorkspace(89, 29)), lipgloss.Height(m.renderFooter(120)))
	}
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("NO_COLOR view contains ANSI: %q", rendered)
	}
	for _, removed := range []string{"WORKLEDGER", "Local only"} {
		if strings.Contains(rendered, removed) {
			t.Fatalf("render retained removed bottom status content %q", removed)
		}
	}
	for _, expected := range []string{"Status", "Worklogs", "WL-1234"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("render missing %q", expected)
		}
	}
	m.notice = "added 1 worklog"
	if footer := m.renderFooter(m.width); !strings.Contains(footer, m.notice) {
		t.Fatalf("action bar missing transient notice %q", m.notice)
	}
	m.notice = ""
	m.openAddForm()
	rendered = m.render()
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("NO_COLOR add form contains ANSI: %q", rendered)
	}
	for _, expected := range []string{"─ Worklog details", "› Issue", "│ Duration", "│ Description", "─ Placement", "─ Calculated"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("NO_COLOR add form missing structural cue %q:\n%s", expected, rendered)
		}
	}
	if body := m.renderForm(25); !strings.Contains(body, "\n\n─ Placement") {
		t.Fatalf("add form does not separate worklog fields from placement:\n%s", body)
	}
}

func TestRailUsesFullAvailableHeight(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)

	for _, height := range []int{minimumHeight - 3, 36, 80} {
		if got := lipgloss.Height(m.renderRail(height)); got != height {
			t.Fatalf("rail height = %d, want %d", got, height)
		}
	}
}

func TestNoColorEnvironmentSelectsPlainRendering(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if deps := defaultDependencies(DefaultTheme()); !deps.noColor {
		t.Fatalf("NO_COLOR was not honored")
	}
}

func TestAutomaticAddFormFitsMinimumViewport(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[durationInput].SetValue("1h30m")
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC), DurationSeconds: 5400,
	}}
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("automatic add render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"Worklog details", "› Issue", "│ Duration", "│ Description", "─ Placement", "[Fill]", "─ Calculated", "Ctrl+P Manual", "Ctrl+O OT on", "Ctrl+L Use lunch", "09:30–11:00", "Ctrl+S Create 1 worklog"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("automatic add render missing %q", expected)
		}
	}
	if body := m.renderForm(25); strings.Contains(body, "Ctrl+S") || strings.Contains(body, "Esc") {
		t.Fatalf("form body duplicates action-bar shortcuts:\n%s", body)
	}
	if strings.Count(rendered, "Ctrl+S") != 1 || strings.Count(rendered, "Esc") != 1 {
		t.Fatalf("form actions should appear once in the action bar:\n%s", rendered)
	}

	m.formError = "review required"
	rendered = m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("automatic add error render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"Worklog details", "─ Placement", "─ Calculated", "review required", "Esc"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("compact automatic add render missing %q", expected)
		}
	}
}

func TestManualAddPreviewFitsMinimumViewport(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.openAddForm()
	m.form.placement = manualPlacement
	m.form.inputs[issueInput].SetValue("WL-1234")
	m.form.inputs[startInput].SetValue("12:00")
	m.form.inputs[durationInput].SetValue("1h")
	m.form.previewRecords = []worklogs.LocalWorklog{{
		IssueKey: "WL-1234", StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}

	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("manual add render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"Start time", "[Manual]", "─ Calculated", "░"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("manual add render missing %q:\n%s", expected, rendered)
		}
	}
}

func TestDayTimelineExtendsAndMarksOvertime(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.openAddForm()
	m.form.previewRecords = []worklogs.LocalWorklog{{
		IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), DurationSeconds: 7 * 3600,
	}}

	timeline := m.renderDayTimeline(m.form.previewRecords, "")
	ruler, upper, bar, _ := timelineParts(t, timeline)
	regularIndex := strings.Index(bar, "░")
	overtimeIndex := strings.Index(bar, "▒")
	if !strings.HasPrefix(ruler, "08:00 ") || !strings.HasSuffix(ruler, "20:00") {
		t.Fatalf("overtime timeline did not extend through preview: %q", timeline)
	}
	if regularIndex < 0 || overtimeIndex <= regularIndex || !strings.Contains(timelineWithoutDividers(bar), "░▒") || strings.Contains(ruler, "│") || strings.Contains(upper, "│") {
		t.Fatalf("overtime timeline lost its continuous fill or plain ruler: %q", timeline)
	}
	if legend := m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, "▒ overtime") || strings.Contains(legend, "│") {
		t.Fatalf("overtime legend = %q", legend)
	}

	m.width, m.height = minimumWidth, 34
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != 34 {
		t.Fatalf("narrow overtime add render = %dx%d, want %dx%d", width, height, minimumWidth, 34)
	}
	if !strings.Contains(rendered, "▒ overtime") || strings.Contains(rendered, "│ day end") {
		t.Fatalf("narrow overtime render lost its legend:\n%s", rendered)
	}

	m.width, m.height = minimumWidth, minimumHeight
	rendered = m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("overtime add render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"▒", "20:00"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("minimum overtime render missing %q", expected)
		}
	}
}

func TestDayTimelineDoesNotMarkPreviewEndingAtDayEndAsOvertime(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.openAddForm()
	m.form.previewRecords = []worklogs.LocalWorklog{{
		IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}

	timeline := m.renderDayTimeline(m.form.previewRecords, "")
	ruler, _, bar, _ := timelineParts(t, timeline)
	if !strings.HasSuffix(ruler, "17:00") || !strings.HasSuffix(bar, "░") || strings.Contains(bar, "▒") || strings.Contains(m.renderTimelineLegend(m.form.previewRecords, ""), "overtime") {
		t.Fatalf("day-end preview was marked as overtime: %q", timeline)
	}
}

func TestTimelineLegendShowsOnlyRenderedAvailability(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00",
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"},
	}
	m.dayContext = worklogs.ContextDay{Date: "2026-05-21", FreeSlots: []worklogs.ContextFreeSlot{}}
	m.worklogs = []worklogs.LocalWorklog{
		{StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 4 * 3600},
		{StartedAtUTC: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), DurationSeconds: 4 * 3600},
	}

	legend := m.renderTimelineLegend(nil, "")
	if !strings.Contains(legend, "╱ lunch") || strings.Contains(legend, "□ free") {
		t.Fatalf("fully booked legend = %q, want booked and lunch without free", legend)
	}

	freeStart := time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC)
	freeEnd := time.Date(2026, 5, 21, 17, 0, 0, 0, time.UTC)
	m.worklogs[1].DurationSeconds = 3 * 3600
	m.dayContext.FreeSlots = []worklogs.ContextFreeSlot{{Start: freeStart, End: freeEnd, DurationSeconds: 3600}}
	if legend = m.renderTimelineLegend(nil, ""); !strings.Contains(legend, "□ free") {
		t.Fatalf("legend with free interval = %q, want free", legend)
	}

	preview := []worklogs.LocalWorklog{{StartedAtUTC: freeStart, DurationSeconds: 3600}}
	legend = m.renderTimelineLegend(preview, "")
	if !strings.Contains(legend, "░ proposed") || strings.Contains(legend, "□ free") {
		t.Fatalf("legend after preview fills final free interval = %q", legend)
	}

	m.worklogs[1].DurationSeconds = 4 * 3600
	preview = []worklogs.LocalWorklog{{StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600}}
	legend = m.renderTimelineLegend(preview, "")
	if !strings.Contains(legend, "░ proposed") || strings.Contains(legend, "╱ lunch") {
		t.Fatalf("legend retained lunch after preview replaced every lunch cell: %q", legend)
	}
}

func TestDayTimelinePersistedWorklogTakesPrecedenceOverLunch(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00",
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"},
	}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "selected", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC), DurationSeconds: 3 * 3600,
	}}

	for _, width := range []int{minimumWidth, 140} {
		m.width = width
		m.worklogs[0].DurationSeconds = 3 * 3600
		unselected := m.renderDayTimeline(nil, "")
		if strings.Contains(unselected, "╱") {
			t.Fatalf("width %d rendered lunch over persisted worklog: %q", width, unselected)
		}
		legend := m.renderTimelineLegend(nil, "selected")
		if strings.Contains(legend, "╱ lunch") || !strings.Contains(legend, "□ free") || !strings.Contains(legend, "▓ selected") {
			t.Fatalf("width %d legend does not match fully booked lunch timeline: %q", width, legend)
		}

		ruler, upper, bar, lower := timelineParts(t, m.renderDayTimeline(nil, "selected"))
		unselectedRuler, unselectedUpper, unselectedBar, unselectedLower := timelineParts(t, unselected)
		if ruler != unselectedRuler || strings.ReplaceAll(bar, "▓", "█") != unselectedBar || upper != unselectedUpper || lower != unselectedLower {
			t.Fatalf("width %d selection does not cover the complete booked interval: %q", width, bar)
		}

		withoutWorklog := m
		withoutWorklog.worklogs = nil
		configuredLunchCells := strings.Count(withoutWorklog.renderDayTimeline(nil, ""), "╱")
		m.worklogs[0].DurationSeconds = 90 * 60
		partialOverlap := m.renderDayTimeline(nil, "")
		visibleLunchCells := strings.Count(partialOverlap, "╱")
		if visibleLunchCells == 0 || visibleLunchCells >= configuredLunchCells {
			t.Fatalf("width %d did not replace only the booked portion of lunch: baseline=%d overlap=%d timeline=%q", width, configuredLunchCells, visibleLunchCells, partialOverlap)
		}
		if legend = m.renderTimelineLegend(nil, "selected"); !strings.Contains(legend, "╱ lunch") {
			t.Fatalf("width %d legend omitted partially visible lunch: %q", width, legend)
		}
	}
}

func TestWeekDayTimelineShowsPersistedLunchOverlapAsBooked(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.worklogView = weekWorklogView
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00",
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"},
	}
	m.worklogs = []worklogs.LocalWorklog{{
		IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC), DurationSeconds: 3 * 3600,
	}}

	detailLines := strings.Split(m.renderWeekDayDetail(79, 10), "\n")
	if len(detailLines) < 2 || strings.Contains(detailLines[2], "╱") || strings.Contains(strings.Join(detailLines, "\n"), "╱ lunch") {
		t.Fatalf("week detail rendered lunch over persisted worklog: %q", detailLines)
	}
}

func TestDayTimelineKeepsLunchVisibleAtPersistedWorklogBoundaries(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00",
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"},
	}

	for _, test := range []struct {
		name     string
		start    time.Time
		duration int
	}{
		{name: "ends at lunch start", start: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC), duration: 3600},
		{name: "starts at lunch end", start: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), duration: 3600},
	} {
		t.Run(test.name, func(t *testing.T) {
			m.worklogs = []worklogs.LocalWorklog{{StartedAtUTC: test.start, DurationSeconds: test.duration}}
			timeline := m.renderDayTimeline(nil, "")
			if !strings.Contains(timeline, "█") || !strings.Contains(timeline, "╱") {
				t.Fatalf("timeline lost booked time or non-overlapping lunch: %q", timeline)
			}
			if legend := m.renderTimelineLegend(nil, ""); !strings.Contains(legend, "╱ lunch") {
				t.Fatalf("legend omitted lunch at worklog boundary: %q", legend)
			}
		})
	}
}

func TestDayTimelineShowsBookedTimeBeforeWorkdayStartAsOvertime(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 7, 0, 0, 0, time.UTC), DurationSeconds: 2 * 3600,
	}}

	timeline := m.renderDayTimeline(nil, "")
	ruler, upper, bar, _ := timelineParts(t, timeline)
	overtimeIndex := strings.Index(bar, "▒")
	regularIndex := strings.Index(bar, "█")
	if !strings.HasPrefix(ruler, "07:00 ") || !strings.HasSuffix(ruler, "17:00") {
		t.Fatalf("early-overtime timeline has wrong bounds: %q", timeline)
	}
	if overtimeIndex < 0 || regularIndex <= overtimeIndex || !strings.Contains(timelineWithoutDividers(bar), "▒█") || strings.Contains(ruler, "│") || strings.Contains(upper, "│") {
		t.Fatalf("early-overtime timeline lost its continuous fill or plain ruler: %q", timeline)
	}
	legend := m.renderTimelineLegend(nil, "")
	if !strings.Contains(legend, "▒ overtime") || strings.Contains(legend, "│") {
		t.Fatalf("early-overtime legend = %q", legend)
	}
}

func timelineParts(t *testing.T, rendered string) (ruler, upper, band, lower string) {
	t.Helper()
	lines := strings.Split(stripANSI(rendered), "\n")
	start := 0
	framed := strings.HasPrefix(lines[0], "┌")
	if framed {
		start++
	}
	if len(lines) < start+4 {
		t.Fatalf("timeline lost its ruler or interval band: %q", rendered)
	}
	parts := []string{lines[start], lines[start+1], lines[start+2], lines[start+3]}
	if framed {
		for index, part := range parts {
			runes := []rune(part)
			parts[index] = string(runes[2 : len(runes)-2])
		}
	}
	// Frame padding after the continuous upper edge is outside the time
	// axis. Keep whitespace within the actual bar for free-time assertions.
	barWidth := len([]rune(strings.TrimRight(parts[1], " ")))
	for index, part := range parts {
		parts[index] = string([]rune(part)[:barWidth])
	}
	return parts[0], parts[1], parts[2], parts[3]
}

// Internal quarter rules may separate adjacent state glyphs; spaces still
// represent real free time and must remain in occupancy comparisons.
func timelineWithoutDividers(band string) string {
	return strings.NewReplacer("▏", "", "▕", "").Replace(band)
}

func TestTimelineQuarterCellsCountSelectedDurations(t *testing.T) {
	for _, width := range []int{minimumWidth, 120} {
		for _, minutes := range []int{15, 60, 90} {
			ws, deps := testWorkspace(t)
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = width, minimumHeight
			m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
			m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: minutes * 60}}
			ruler, upper, band, lower := timelineParts(t, m.renderDayTimeline(nil, "selected"))
			quarters := strings.FieldsFunc(band, func(r rune) bool { return r != '▓' })
			if len(quarters) != minutes/15 {
				t.Fatalf("width %d: %dm has %d selected cells, want %d: %q", width, minutes, len(quarters), minutes/15, band)
			}
			if strings.Contains(ruler, "│") || upper != strings.Repeat("▁", lipgloss.Width(band)) || lower != strings.Repeat("▔", lipgloss.Width(band)) {
				t.Fatalf("quarter cells escaped the bar: ruler=%q upper=%q lower=%q", ruler, upper, lower)
			}
		}
	}
}

func TestTimelineQuarterCellsShareAFullWidthGridAcrossStates(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, width := range []int{79, 99, 112, 143} {
			ws, deps := testWorkspace(t)
			deps.noColor = noColor
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = 180, 36
			m.contextSettings = worklogs.ContextSettings{
				DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
			}
			m.worklogs = []worklogs.LocalWorklog{
				{ID: "booked", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 3600},
				{ID: "selected", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 3600},
			}
			preview := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(11 * time.Hour), DurationSeconds: 3600}}
			_, upper, band, lower := timelineParts(t, m.renderDayTimelineWidth(preview, "selected", width))
			cells := []rune(band)
			if len(cells) != width-4 || len([]rune(upper)) != len(cells) || len([]rune(lower)) != len(cells) {
				t.Fatalf("width %d: shared grid does not fill the bar: %q", width, band)
			}
			boundaries := []int{0}
			for column, symbol := range cells {
				if column > 0 && symbol == '▏' {
					boundaries = append(boundaries, column)
				}
			}
			boundaries = append(boundaries, len(cells))
			if len(boundaries) != 37 {
				t.Fatalf("width %d: shared grid has %d quarters, want 36: %q", width, len(boundaries)-1, band)
			}
			for quarter := 0; quarter < 36; quarter++ {
				quarterWidth := boundaries[quarter+1] - boundaries[quarter]
				if quarterWidth < len(cells)/36 || quarterWidth > (len(cells)+35)/36 {
					t.Fatalf("width %d: quarter %d has an inconsistent width of %d: %q", width, quarter, quarterWidth, band)
				}
			}
			selectedSymbol := "▓"
			if !noColor {
				selectedSymbol = "█"
			}
			for _, state := range []struct {
				name, symbols string
				first, count  int
			}{
				{"booked", "█", 0, 4}, {"selected", selectedSymbol, 4, 4},
				{"free", " ▕", 8, 4}, {"proposed", "░", 12, 4}, {"lunch", "╱", 16, 3},
			} {
				for quarter := state.first; quarter < state.first+state.count; quarter++ {
					cell := string(cells[boundaries[quarter]:boundaries[quarter+1]])
					if !strings.ContainsAny(cell, state.symbols) {
						t.Fatalf("width %d: %s quarter lost its state on the shared grid: %q", width, state.name, cell)
					}
				}
			}
		}
	}
}

func TestTimelineQuarterDividersKeepTheSegmentFill(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, selected := range []bool{false, true} {
			ws, deps := testWorkspace(t)
			deps.noColor = false
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = minimumWidth, minimumHeight
			if light {
				m.colors = deps.theme.Light
			}
			m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
			m.worklogs = []worklogs.LocalWorklog{{ID: "booked", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 90 * 60}}
			color, selectedID := m.colors.Information, ""
			if selected {
				color, selectedID = m.colors.Focus, "booked"
			}
			timeline := m.renderDayTimeline(nil, selectedID)
			filledRule := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Reverse(true).Render("▏")
			filledCell := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Background(lipgloss.Color(color)).Render("█")
			if strings.Count(timeline, filledRule) < 5 || !strings.Contains(timeline, filledCell) {
				t.Fatalf("six quarter cells lost their thin rules or background fill (light=%t selected=%t): %q", light, selected, timeline)
			}
		}
	}
}

func TestTimelineOutlinedCellsShareFilledCellBounds(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		ws, deps := testWorkspace(t)
		deps.noColor = noColor
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
		m.worklogs = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 3600}}
		// The first eight columns cover free time from 08:00 to 09:00.
		_, upper, band, lower := timelineParts(t, m.renderDayTimelineWidth(nil, "", 76))
		cells := []rune(band)
		if cells[0] != '▏' || cells[7] != '▕' || cells[16] != '▏' || cells[len(cells)-1] != '▕' || strings.Contains(band, "│") {
			t.Fatalf("outlined sides are inset from their time-cell edges: %q", band)
		}
		if upper != strings.Repeat("▁", len(cells)) || lower != strings.Repeat("▔", len(cells)) {
			t.Fatalf("outlined and filled cells do not share the same height: upper=%q lower=%q", upper, lower)
		}
	}
}

func TestTimelineQuarterGridDoesNotAddATinyCellAtBookingBoundary(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(14 * time.Hour), DurationSeconds: 90 * 60}}
	// The booking ends exactly at a sampled midpoint. Its edge and the
	// shared 15:30 rule must occupy the same column.
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 79))
	if firstFreeQuarter := string([]rune(band)[62:65]); firstFreeQuarter != "▏  " {
		t.Fatalf("15:30 created an extra narrow cell before 15:45: %q in %q", firstFreeQuarter, band)
	}
}

func TestTimelineQuarterCellsPreserveStatePatterns(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.worklogs = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 3600}}
	preview := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(10 * time.Hour), DurationSeconds: 3600}}
	// The 72-column bar has two terminal columns per local quarter hour.
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(preview, "", 76))
	for _, symbol := range []rune{'█', '░'} {
		quarters := strings.FieldsFunc(band, func(r rune) bool { return r != symbol })
		if len(quarters) != 4 {
			t.Fatalf("one hour of %q has %d cells, want four: %q", symbol, len(quarters), band)
		}
	}
	lunch := []rune(band)[32:38]
	for quarter := 0; quarter < 3; quarter++ {
		if !strings.Contains(string(lunch[quarter*2:quarter*2+2]), "╱") {
			t.Fatalf("lunch quarter %d lost its hatch: %q", quarter+1, string(lunch))
		}
	}
	if strings.Contains(m.renderDayTimelineWidth(preview, "", 76), "\x1b") {
		t.Fatal("NO_COLOR quarter cells emitted ANSI styling")
	}
	m.openAddForm()
	m.form.previewConflict = "overlap"
	_, _, conflict, _ := timelineParts(t, m.renderDayTimelineWidth(m.worklogs, "", 76))
	if quarters := strings.FieldsFunc(conflict, func(r rune) bool { return r != '!' }); len(quarters) != 4 {
		t.Fatalf("conflict cells lost their visible state: %q", conflict)
	}
}

func TestTimelineQuarterCellsPreservePartialIntervals(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 36
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	start := m.selectedDate.Add(8*time.Hour + 5*time.Minute)
	m.worklogs = []worklogs.LocalWorklog{{ID: "partial", StartedAtUTC: start, DurationSeconds: 20 * 60}}
	// At five minutes per terminal column, 08:05–08:25 occupies columns 1–4.
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "partial", 112))
	cells := []rune(band)
	if cells[0] == '▓' || string(cells[1:5]) != "▓▓▏▓" || cells[5] == '▓' {
		t.Fatalf("partial interval was rounded to a full quarter: %q", band)
	}
	if !m.worklogs[0].StartedAtUTC.Equal(start) || m.worklogs[0].DurationSeconds != 20*60 {
		t.Fatal("rendering changed the worklog timestamps")
	}
}

func TestTimelineQuarterColumnsUseLocalClockBoundaries(t *testing.T) {
	for _, location := range []*time.Location{time.UTC, time.FixedZone("UTC+05:10", (5*60+10)*60)} {
		start := time.Date(2026, 10, 9, 7, 52, 0, 0, location)
		// One minute per column; the first/last cells are partial quarters.
		columns := timelineQuarterColumns(newTimelineScale(start, start.Add(75*time.Minute), 75))
		want := map[int]int{8: 15, 23: 15, 38: 15, 53: 15, 68: 7}
		if len(columns) != len(want) {
			t.Fatalf("%s: quarter columns = %v, want %v", location, columns, want)
		}
		for column, minutes := range want {
			if columns[column] != minutes {
				t.Fatalf("%s: boundary at minute %d has width %d, want %d", location, column, columns[column], minutes)
			}
		}
	}
}

func TestTimelineFullWidthGridPreservesPartialAxisBounds(t *testing.T) {
	start := time.Date(2026, 10, 9, 7, 52, 0, 0, time.UTC)
	end := start.Add(75 * time.Minute)
	scale := newTimelineScale(start, end, 27)
	columns := timelineQuarterColumns(scale)
	// Complete quarters occupy five or six columns, using the whole axis
	// without rounding the actual eight-minute and seven-minute tails.
	want := map[int]int{3: 5, 8: 6, 14: 5, 19: 5, 24: 3}
	if scale.width != 27 || len(columns) != len(want) || !scale.start.Equal(start) || !scale.end.Equal(end) {
		t.Fatalf("partial bounds changed the scale or timestamps: %+v, %v", scale, columns)
	}
	for column, width := range want {
		if columns[column] != width {
			t.Fatalf("quarter at column %d has width %d, want %d", column, columns[column], width)
		}
	}
	if first, last := scale.midpoint(0), scale.midpoint(scale.width-1); first.Before(start) || !last.Before(end) {
		t.Fatalf("partial cells sampled outside the actual axis: %s–%s", first, last)
	}
}

func TestTimelineLunchQuarterCellsUseTheFullGridWidth(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, minutes := range []int{15, 30, 45} {
			ws, deps := testWorkspace(t)
			deps.noColor = noColor
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = minimumWidth, minimumHeight
			m.contextSettings = worklogs.ContextSettings{
				DayStart: "08:00", DayEnd: "17:00",
				Lunch: &worklogs.ContextLunch{Start: "12:00", End: fmt.Sprintf("12:%02d", minutes)},
			}
			_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 76))
			cells := []rune(band)
			end := 32 + 2*(minutes/15)
			if lunch := string(cells[32:end]); lunch != strings.Repeat("▏╱", minutes/15) || cells[end] != '▏' {
				t.Fatalf("%dm lunch contains a smaller boxed cell or lost its enclosure: %q", minutes, band)
			}
		}
	}
}

func TestTimelineLunchEndUsesFullHeightHatchingOnAnOvertimeAxis(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		ws, deps := testWorkspace(t)
		deps.noColor = noColor
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height, m.worklogView = 260, 36, weekWorklogView
		m.contextSettings = worklogs.ContextSettings{
			DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
		}
		m.worklogs = []worklogs.LocalWorklog{
			{StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 3600},
			{StartedAtUTC: m.selectedDate.Add(13 * time.Hour), DurationSeconds: 4 * 3600},
		}
		// The 74-column bar leaves six lunch columns followed by one free
		// column. The last lunch quarter must match the preceding quarters.
		_, upper, band, lower := timelineParts(t, m.renderDayTimelineWidth(nil, "", 78))
		cells := []rune(band)
		if lunch := string(cells[37:43]); lunch != "▏╱▏╱▏╱" || cells[43] != '▏' || strings.Contains(band, "⊠") {
			t.Fatalf("lunch end shrank or lost its thin enclosure (color=%t): %q", !noColor, band)
		}
		if upper != strings.Repeat("▁", 74) || lower != strings.Repeat("▔", 74) {
			t.Fatalf("lunch no longer shares the bar's full height: upper=%q lower=%q", upper, lower)
		}
	}
}

func TestTimelineLunchEnclosurePreservesASingleAdjacentBooking(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 260, 36
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.worklogs = []worklogs.LocalWorklog{
		{StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 3600},
		{StartedAtUTC: m.selectedDate.Add(12*time.Hour + 45*time.Minute), DurationSeconds: 15 * 60},
	}
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 78))
	cells := []rune(band)
	// The border must stay inside lunch when borrowing the next column
	// would erase the only visible column of an adjacent booking.
	if string(cells[37:43]) != "▏╱▏╱╱▕" || cells[43] != '█' || strings.Contains(band, "⊠") {
		t.Fatalf("lunch lost its hatch or consumed the adjacent booking: %q", band)
	}
}

func TestTimelineOvertimeUsesFilledQuarterRules(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, preview := range []bool{false, true} {
			ws, deps := testWorkspace(t)
			deps.noColor = false
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height, m.worklogView = 260, 36, weekWorklogView
			if light {
				m.colors = deps.theme.Light
			}
			m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
			records := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 10 * 3600}}
			var previews []worklogs.LocalWorklog
			if preview {
				previews = records
			} else {
				m.worklogs = records
			}
			timeline := m.renderDayTimelineWidth(previews, "", 78)
			filledRule := lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Warning)).Reverse(true).Render("▏")
			filledCell := lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Warning)).Background(lipgloss.Color(m.colors.Warning)).Render("█")
			_, _, band, _ := timelineParts(t, timeline)
			if strings.Count(timeline, filledRule) != 3 || !strings.Contains(timeline, filledCell) || string([]rune(band)[:7]) != "██▏█▏█▏" {
				t.Fatalf("overtime cells lost their fill or shared quarter grid (light=%t preview=%t, filled rules=%d): %q", light, preview, strings.Count(timeline, filledRule), band)
			}
		}
	}
}

func TestTimelinePartialLunchKeepsItsPattern(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.worklogs = []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(12 * time.Hour), DurationSeconds: 35 * 60}}
	_, _, band, _ := timelineParts(t, m.renderDayTimelineWidth(nil, "", 76))
	// The ten-minute lunch remainder has one sampled column. Sharing its
	// neighbor's border must not replace its only visible pattern.
	if symbol := []rune(band)[37]; symbol != '╱' {
		t.Fatalf("partial lunch lost its hatch to a border: %q", band)
	}
}

func TestTimelineStatesShareOneVisibleBand(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		for _, width := range []int{minimumWidth, 120} {
			ws, deps := testWorkspace(t)
			deps.noColor = noColor
			m := newModel(context.Background(), deps, ws, nil)
			m.width, m.height = width, minimumHeight
			m.contextSettings = worklogs.ContextSettings{
				DayStart: "08:00", DayEnd: "17:00",
				Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
			}
			m.worklogs = []worklogs.LocalWorklog{
				{ID: "selected", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 3600},
				{ID: "booked", StartedAtUTC: m.selectedDate.Add(9 * time.Hour), DurationSeconds: 3600},
			}
			preview := []worklogs.LocalWorklog{{StartedAtUTC: m.selectedDate.Add(11 * time.Hour), DurationSeconds: 1800}}
			timeline := m.renderDayTimeline(preview, "selected")
			_, top, band, bottom := timelineParts(t, timeline)
			columns := lipgloss.Width(band)
			if top != strings.Repeat("▁", columns) || bottom != strings.Repeat("▔", columns) {
				t.Fatalf("states do not share the same thin edges at width %d: %q", width, timeline)
			}
			for _, symbol := range []string{"█", "░", "╱"} {
				if !strings.Contains(band, symbol) || strings.Contains(top+bottom, symbol) {
					t.Fatalf("state %q escapes the common fill row: %q", symbol, timeline)
				}
			}
			first, last := strings.Index(band, "╱"), strings.LastIndex(band, "╱")
			if !strings.HasSuffix(band[:first], "▏") || !strings.HasPrefix(band[last+len("╱"):], "▏") {
				t.Fatalf("lunch hatching has no enclosing sides: %q", band)
			}
			if noColor && strings.Contains(timeline, "\x1b") {
				t.Fatal("NO_COLOR timeline emitted ANSI styling")
			}
		}
	}
}

func TestTimelineKeepsTinyLunchRunsPatternedAndEnclosed(t *testing.T) {
	for _, minutes := range []int{15, 30, 45} {
		ws, deps := testWorkspace(t)
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.contextSettings = worklogs.ContextSettings{
			DayStart: "08:00", DayEnd: "17:00",
			Lunch: &worklogs.ContextLunch{Start: "12:00", End: m.selectedDate.Add(12*time.Hour + time.Duration(minutes)*time.Minute).Format("15:04")},
		}
		timeline, visibility := m.renderTimelineState(nil, "", 36)
		_, upper, band, lower := timelineParts(t, timeline)
		cells := []rune(band)
		end := 16 + minutes/15
		lunch := string(cells[16:end])
		if !visibility.lunch || !strings.Contains(lunch, "╱") || strings.Contains(lunch, "⊠") || cells[end] != '▏' {
			t.Fatalf("%dm lunch lost its pattern or border at narrow width: %q", minutes, lunch)
		}
		if upper != strings.Repeat("▁", 36) || lower != strings.Repeat("▔", 36) {
			t.Fatalf("%dm lunch lost its full-height thin edges", minutes)
		}
	}
}

func TestWorklogListRendersPersistentCurrentTimeline(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "selected", IssueKey: "WL-100", StartedAtUTC: m.selectedDate.Add(13 * time.Hour), DurationSeconds: 3 * 3600},
		{ID: "late", IssueKey: "WL-200", StartedAtUTC: m.selectedDate.Add(16*time.Hour + 30*time.Minute), DurationSeconds: 5 * 3600},
	}
	m.dayContext = worklogs.ContextDay{Date: "2026-05-21", BookedSeconds: 8 * 3600}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	rendered := m.render()
	for _, expected := range []string{"Thu 21 May 2026", "8h logged / 8h target", "target met", "[Day]  Week", "08:00", "▓ selected", "▒ overtime", "21:30", "Lunch 12:00–12:45"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("daily workspace missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "░ proposed") || strings.Contains(rendered, "Current") {
		t.Fatalf("daily workspace contains a redundant timeline label:\n%s", rendered)
	}
	timeline := m.renderDayTimeline(nil, "selected")
	ruler, _, bar, _ := timelineParts(t, timeline)
	unselectedRuler, _, unselectedBar, _ := timelineParts(t, m.renderDayTimeline(nil, ""))
	if ruler != unselectedRuler || strings.ReplaceAll(bar, "▓", "█") != unselectedBar {
		t.Fatalf("selection changed chronology or occupancy: %q", timeline)
	}
	if lipgloss.Width(timeline) != calculateLayout(m.width, m.height, false).workspace.width-2 {
		t.Fatalf("framed timeline does not use the available workspace width: %q", timeline)
	}
	if !strings.Contains(rendered, timeline) {
		// The containing pane adds padding, so compare the frame's last row.
		last := strings.Split(timeline, "\n")[lipgloss.Height(timeline)-1]
		lines := strings.Split(rendered, "\n")
		for index, line := range lines {
			if strings.Contains(line, last) && (index+2 >= len(lines) || !strings.Contains(lines[index+2], "▓ selected")) {
				t.Fatalf("timeline and legend are not separated by a blank row:\n%s", rendered)
			}
		}
	}
}

func TestSelectedTimelinePreservesScaleForShortAndLongWorklogs(t *testing.T) {
	for _, duration := range []int{1, 15 * 60, 2*3600 + 15*60} {
		ws, deps := testWorkspace(t)
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
		m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(10*time.Hour + 15*time.Minute), DurationSeconds: duration}}
		beforeRuler, beforeUpper, beforeBar, beforeLower := timelineParts(t, m.renderDayTimeline(nil, ""))
		ruler, upper, bar, lower := timelineParts(t, m.renderDayTimeline(nil, "selected"))
		if ruler != beforeRuler || lipgloss.Width(bar) != lipgloss.Width(beforeBar) || strings.Count(bar, "▓") < 1 || upper != beforeUpper || lower != beforeLower {
			t.Fatalf("%ds selection lost its interval or changed scale: %q", duration, bar)
		}
		if duration >= 900 && strings.ReplaceAll(bar, "▓", "█") != beforeBar {
			t.Fatalf("%ds selection changed occupancy: %q", duration, bar)
		}
		assertRedesignBounds(t, m, "selected timeline without color")
	}
}

func TestDayTimelineSelectionStopsBeforeAdjacentWorklog(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "selected", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 2*3600 + 15*60},
		{ID: "adjacent", StartedAtUTC: m.selectedDate.Add(10*time.Hour + 15*time.Minute), DurationSeconds: 15 * 60},
	}
	_, _, before, _ := timelineParts(t, m.renderDayTimeline(nil, ""))
	_, _, selected, _ := timelineParts(t, m.renderDayTimeline(nil, "selected"))
	if strings.ReplaceAll(selected, "▓", "█") != before || strings.Count(selected, "█") == 0 || strings.Count(selected, "▓") == 0 {
		t.Fatalf("selection does not distinguish the adjacent worklog: %q", selected)
	}
}

func TestDayTimelineUsesSolidFocusColorForCompleteSelection(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 2*3600 + 15*60}}
	selected := m.renderDayTimeline(nil, "selected")
	unselected := m.renderDayTimeline(nil, "")
	focusFill := lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Focus)).Background(lipgloss.Color(m.colors.Focus)).Render("█")
	bookedFill := lipgloss.NewStyle().Foreground(lipgloss.Color(m.colors.Information)).Background(lipgloss.Color(m.colors.Information)).Render("█")
	if lipgloss.Width(selected) != lipgloss.Width(unselected) || !strings.Contains(selected, focusFill) || !strings.Contains(selected, m.paint("▔", m.colors.Focus)) || strings.Contains(selected, bookedFill) {
		t.Fatalf("selection does not replace the complete booked fill with focus color: %q", selected)
	}
	if selector := m.renderWorklogViewSelector(); !strings.Contains(selector, "\x1b[7;") && !strings.Contains(selector, ";7;") {
		t.Fatalf("active mode has no filled treatment: %q", selector)
	}
}

func TestTimelineOvertimePreservesPlainRulerAndContinuousBandEdges(t *testing.T) {
	for _, scenario := range []string{"booked", "proposed", "overtime", "selected", "adjacent selected"} {
		for _, noColor := range []bool{false, true} {
			for _, width := range []int{minimumWidth, 120} {
				t.Run(fmt.Sprintf("%s/color=%t/width=%d", scenario, !noColor, width), func(t *testing.T) {
					ws, deps := testWorkspace(t)
					deps.noColor = noColor
					m := newModel(context.Background(), deps, ws, nil)
					m.width, m.height = width, minimumHeight
					m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
					records := []worklogs.LocalWorklog{{ID: "crossing", StartedAtUTC: m.selectedDate.Add(7 * time.Hour), DurationSeconds: 2 * 3600}}
					var previews []worklogs.LocalWorklog
					selectedID := ""
					transition := "▒█"
					firstClock, lastClock := "07:00", "17:00"
					switch scenario {
					case "proposed":
						previews = records
						transition = "▒░"
					case "overtime":
						records[0].StartedAtUTC = m.selectedDate.Add(16 * time.Hour)
						m.worklogs = records
						transition = "█▒"
						firstClock, lastClock = "08:00", "18:00"
					case "selected":
						m.worklogs, selectedID = records, "crossing"
						if noColor {
							transition = "▚▓"
						}
					case "adjacent selected":
						records[0].DurationSeconds = 3600
						m.worklogs = append(records, worklogs.LocalWorklog{ID: "selected", StartedAtUTC: m.selectedDate.Add(8 * time.Hour), DurationSeconds: 3600})
						selectedID = "selected"
						if noColor {
							transition = "▒▓"
						}
					default:
						m.worklogs = records
					}
					if !noColor && scenario != "selected" {
						transition = strings.ReplaceAll(transition, "▒", "█")
					}
					timeline := m.renderDayTimeline(previews, selectedID)
					ruler, upper, band, lower := timelineParts(t, timeline)
					if upper != strings.Repeat("▁", lipgloss.Width(band)) || lower != strings.Repeat("▔", lipgloss.Width(band)) {
						t.Fatalf("overtime breaks the bar's edge: upper=%q lower=%q", upper, lower)
					}
					if strings.Contains(ruler, "│") || !strings.HasPrefix(ruler, firstClock) || !strings.HasSuffix(ruler, lastClock) {
						t.Fatalf("ruler contains a divider or lost its clock labels: %q", ruler)
					}
					if legend := stripANSI(m.renderTimelineLegend(previews, selectedID)); strings.Contains(legend, "│") {
						t.Fatalf("legend still contains a removed workday marker: %q", legend)
					}
					// A one-column colored quarter carries its booked fill in
					// the inverse rule's background. Retain that occupancy
					// when checking continuity across the workday boundary.
					occupied := timeline
					if !noColor {
						for _, color := range []string{m.colors.Information, m.colors.Focus, m.colors.Warning} {
							filledRule := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Reverse(true).Render("▏")
							occupied = strings.ReplaceAll(occupied, filledRule, "█")
						}
					}
					_, _, occupiedBand, _ := timelineParts(t, occupied)
					if !strings.Contains(timelineWithoutDividers(occupiedBand), transition) {
						t.Fatalf("workday boundary interrupts occupied fill %q: %q", transition, band)
					}
					if noColor && strings.Contains(timeline, "\x1b") {
						t.Fatal("NO_COLOR timeline contains escape sequences")
					}
				})
			}
		}
	}
}

func TestDayTimelineSelectionSpansRegularAndOvertimePortions(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{ID: "selected", StartedAtUTC: m.selectedDate.Add(16*time.Hour + 30*time.Minute), DurationSeconds: 2 * 3600}}
	ruler, upper, bar, lower := timelineParts(t, m.renderDayTimeline(nil, "selected"))
	beforeRuler, _, before, _ := timelineParts(t, m.renderDayTimeline(nil, ""))
	if ruler != beforeRuler || lipgloss.Width(upper) != lipgloss.Width(before) || lipgloss.Width(lower) != lipgloss.Width(upper) || !strings.Contains(timelineWithoutDividers(bar), "▓▚") || strings.Contains(ruler, "│") || !strings.Contains(m.renderTimelineLegend(nil, "selected"), "▒/▚ overtime") || !strings.Contains(m.renderTimelineLegend(nil, "selected"), "▓ selected") {
		t.Fatalf("selection lost its complete range, plain ruler, or overtime pattern: bar=%q lower=%q", bar, lower)
	}
}

func TestTimelineKeepsShortSelectedOvertimeVisible(t *testing.T) {
	for _, offset := range []time.Duration{8*time.Hour - time.Second, 17 * time.Hour} {
		ws, deps := testWorkspace(t)
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height = minimumWidth, minimumHeight
		m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
		m.worklogs = []worklogs.LocalWorklog{{ID: "short", StartedAtUTC: m.selectedDate.Add(offset), DurationSeconds: 1}}
		ruler, _, bar, _ := timelineParts(t, m.renderDayTimeline(nil, "short"))
		if !strings.Contains(bar, "▚") || strings.Contains(ruler, "│") {
			t.Fatalf("timeline lost short selection or plain ruler at %s: bar=%q", offset, bar)
		}
	}
}

func TestPlacementControlsRemainVisibleWithActivityRetainedAtMinimumSize(t *testing.T) {
	for _, mode := range []placementMode{fillPlacement, fitPlacement, manualPlacement} {
		ws, deps := testWorkspace(t)
		m := newModel(context.Background(), deps, ws, nil)
		m.width, m.height, m.activityOpen = minimumWidth, minimumHeight, true
		m.contextSettings.Lunch = &worklogs.ContextLunch{Start: "12:00", End: "12:45"}
		m.openAddForm()
		m.form.placement = mode
		m.formError = "Invalid duration"
		if m.activityVisible() || !m.activityOpen {
			t.Fatal("form did not hide and retain the Activity drawer")
		}
		rendered := m.render()
		for _, cue := range []string{"Worklog details", "Placement", "Calculated", "Invalid duration", "Description", "Ctrl+S", "Esc"} {
			if !strings.Contains(rendered, cue) {
				t.Fatalf("%s with Activity lost %q:\n%s", mode, cue, rendered)
			}
		}
		if mode != manualPlacement && (!strings.Contains(rendered, "Allow starts after") || !strings.Contains(rendered, "Use lunch time")) {
			t.Fatalf("%s with Activity clipped placement controls:\n%s", mode, rendered)
		}
		assertRedesignBounds(t, m, "placement with Activity")
		next, _ := m.Update(specialKey(tea.KeyEsc))
		if !next.(model).activityVisible() {
			t.Fatal("cancel did not restore the retained Activity drawer")
		}
	}
}

func TestTimelineRulerUsesLocalHoursAndAvoidsCollisions(t *testing.T) {
	dayStart := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	minimumRuler := timelineRuler(dayStart, newTimelineScale(dayStart, dayStart.Add(9*time.Hour), 75))
	for hour := 8; hour <= 17; hour++ {
		if !strings.Contains(minimumRuler, fmt.Sprintf("%02d:00", hour)) {
			t.Fatalf("minimum ruler lost hour %d: %q", hour, minimumRuler)
		}
	}
	location := time.FixedZone("UTC+05:45", (5*60+45)*60)
	start := time.Date(2026, 5, 21, 8, 15, 0, 0, location)
	end := start.Add(9*time.Hour + 15*time.Minute)
	wide := timelineRuler(start, newTimelineScale(start, end, 112))
	for _, label := range []string{"08:15", "09:00", "10:00", "11:00", "12:00", "13:00", "14:00", "15:00", "16:00", "17:30"} {
		if !strings.Contains(wide, label) {
			t.Fatalf("local hour missing %s: %q", label, wide)
		}
	}
	for _, width := range []int{24, 75, 100} {
		scale := newTimelineScale(start, end, width)
		ruler := timelineRuler(start, scale)
		if lipgloss.Width(ruler) != scale.width || scale.width > width || !strings.HasPrefix(ruler, "08:15") || !strings.HasSuffix(ruler, "17:30") {
			t.Fatalf("width %d lost exact bounds: %q", width, ruler)
		}
		for _, label := range strings.Fields(ruler) {
			if len(label) != 5 || label[2] != ':' {
				t.Fatalf("width %d has colliding hour labels: %q", width, ruler)
			}
		}
	}
}

func TestWeekTimelineFillsItsPaneWhenOvertimeExtendsTheDay(t *testing.T) {
	for _, width := range []int{minimumWidth, 140, 260, 320} {
		for _, interval := range []struct {
			name, first, last string
			start, minutes    int
		}{
			{"regular", "08:00", "17:00", 8, 9 * 60},
			{"early overtime", "07:00", "17:00", 7, 10 * 60},
			{"late overtime", "08:00", "18:15", 8, 10*60 + 15},
		} {
			t.Run(fmt.Sprintf("%s/width=%d", interval.name, width), func(t *testing.T) {
				ws, deps := testWorkspace(t)
				m := newModel(context.Background(), deps, ws, nil)
				m.width, m.height, m.worklogView = width, 36, weekWorklogView
				m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
				m.worklogs = []worklogs.LocalWorklog{{
					StartedAtUTC: m.selectedDate.Add(time.Duration(interval.start) * time.Hour), DurationSeconds: interval.minutes * 60,
				}}
				layout := calculateLayout(width, m.height, false)
				_, detailWidth := inspectorWidths(layout.workspace.width)
				contentWidth := detailWidth - 2
				wantWidth := contentWidth
				if contentWidth >= 60 {
					wantWidth -= 4 // Thin frame and one padding column on each side.
				}
				detail := strings.Split(m.renderWeekDayDetail(contentWidth, layout.workspace.height-3), "\n")
				ruler, upper, band, lower := timelineParts(t, strings.Join(detail[2:], "\n"))
				for name, part := range map[string]string{"ruler": ruler, "upper": upper, "band": band, "lower": lower} {
					if got := lipgloss.Width(part); got != wantWidth {
						t.Fatalf("%s shrank to %d columns, want the full %d-column pane: %q", name, got, wantWidth, part)
					}
				}
				if !strings.HasPrefix(ruler, interval.first) || !strings.HasSuffix(ruler, interval.last) {
					t.Fatalf("full-width ruler lost its actual bounds: %q", ruler)
				}
				if interval.name != "regular" && !strings.Contains(band, "▒") {
					t.Fatalf("full-width timeline lost overtime: %q", band)
				}
			})
		}
	}
}

func TestWeekViewRendersOverviewAndSelectedDayAtMinimumViewport(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogView = weekWorklogView
	m.contextDays = testWeekDays()
	m.contextSummary = worklogs.ContextSummary{BookedSeconds: 32*3600 + 30*60, WorklogCount: 5, CollisionCount: 1}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.selectContextDate(m.selectedDate)
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "early", IssueKey: "WL-1234", StartedAtUTC: time.Date(2026, 5, 21, 7, 0, 0, 0, time.UTC), DurationSeconds: 2 * 3600,
	}}
	m.dayContext.Worklogs = m.worklogs

	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("weekly render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{
		"Week · 2026-05-18–2026-05-24 · 32h 30m logged",
		"Day  [Week]",
		"DAY DATE",
		"> Thu",
		"-6h 00m",
		"Thursday, 2026-05-21",
		"07:00",
		"▒ overtime",
		"d/w/m View",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("weekly render missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "3 Week") {
		t.Fatalf("weekly mode rendered a third rail tab:\n%s", rendered)
	}
	weekLines := strings.Split(rendered, "\n")
	selectorLine, headerLine := -1, -1
	for index, line := range weekLines {
		if strings.Contains(line, "Day  [Week]") {
			selectorLine = index
		}
		if strings.Contains(line, "DAY DATE") {
			headerLine = index
			break
		}
	}
	if selectorLine < 0 || headerLine != selectorLine+2 {
		t.Fatalf("week selector and overview are not separated by one blank row: selector=%d overview=%d\n%s", selectorLine, headerLine, rendered)
	}
	detailLines := strings.Split(m.renderWeekDayDetail(79, 10), "\n")
	timelineLine, legendLine := -1, -1
	for index, line := range detailLines {
		if strings.Contains(line, "07:00") {
			timelineLine = index
		}
		if strings.Contains(line, "█ booked") {
			legendLine = index
			break
		}
	}
	timelineHeight := lipgloss.Height(m.renderDayTimelineWidth(nil, "", 79))
	if timelineLine < 0 || legendLine != timelineLine+timelineHeight {
		t.Fatalf("weekly timeline and legend are not separated by one blank row: timeline=%d legend=%d\n%s", timelineLine, legendLine, strings.Join(detailLines, "\n"))
	}
	if path := os.Getenv("WORKLEDGER_TUI_WEEK_RENDER_PATH"); path != "" {
		if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
			t.Fatalf("write weekly reference render: %v", err)
		}
	}
}

func TestViewUsesSemanticColorWhenEnabled(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.statusLoading = false
	m.statusItems = []status.Item{{Status: "ok"}}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00",
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "13:00"},
	}
	rail := m.renderRail(28)
	for _, expected := range []string{
		m.paintBold("2 Worklogs", m.colors.Focus),
		m.paint("OK 1", m.colors.Success),
		m.paint("Warnings 0", m.colors.Warning),
		m.paint("0h", m.colors.Information),
		m.paint("8h remaining", m.colors.Warning),
	} {
		if !strings.Contains(rail+m.renderStatusDiagnosticsWorkspace(101, 25)+m.renderDayQuota(), expected) {
			t.Fatalf("view missing semantic state style %q", expected)
		}
	}
	if strings.Contains(rail, m.paintBold("1 Status", m.colors.Focus)) {
		t.Fatalf("inactive rail title unexpectedly uses the active style: %q", rail)
	}
	if rendered := m.render(); !strings.Contains(rendered, "\x1b[38;2;") {
		t.Fatalf("colored view contains no truecolor styling")
	}
	m.openAddForm()
	form := m.renderForm(25) + m.renderPlacementInspector(50, 25)
	for _, expected := range []string{
		m.paintBold(fmt.Sprintf("%-12s", "› Issue"), m.colors.Focus),
		m.paint(fmt.Sprintf("%-12s", "│ Duration"), m.colors.Secondary),
		m.renderFormSection("Calculated", m.colors.Information),
		m.secondary("After saving"),
	} {
		if !strings.Contains(form, expected) {
			t.Fatalf("form missing semantic role style %q: %q", expected, form)
		}
	}
	footer := m.renderFooter(140)
	if !strings.Contains(footer, m.paint("Ctrl+P", m.colors.Focus)) || strings.Contains(footer, m.paint("Allow overtime", m.colors.Focus)) {
		t.Fatalf("footer does not separate key hints from action copy: %q", footer)
	}
	picker := m
	picker.form = nil
	picker.presets = []presets.Preset{{Name: "daily-standup"}}
	picker.openPresetPicker()
	footer = picker.renderFooter(140)
	if !strings.Contains(footer, m.paint("Ctrl+F", m.colors.Focus)) || strings.Contains(footer, m.paint("Find", m.colors.Focus)) {
		t.Fatalf("preset picker footer does not style Ctrl+F as a key chord: %q", footer)
	}
	legend := m.renderTimelineLegend(nil, "")
	for _, expected := range []string{m.paint("█", m.colors.Information), m.paint("╱", m.colors.Secondary), m.paint("□", m.colors.Muted)} {
		if !strings.Contains(legend, expected) {
			t.Fatalf("timeline legend missing semantic swatch %q: %q", expected, legend)
		}
	}
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	if legend = m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, m.paint("░", m.colors.Proposed)) || !strings.Contains(legend, "proposed") {
		t.Fatalf("preview legend missing new-work swatch: %q", legend)
	}
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC), DurationSeconds: 2 * 3600,
	}}
	if legend = m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, m.paint("█", m.colors.Warning)) || strings.Contains(stripANSI(legend), "│") {
		t.Fatalf("overtime legend lost its warning swatch or retained a removed marker: %q", legend)
	}
}

func TestFocusIndicatorMatchesInputPane(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.worklogs = testRows()
	for _, focus := range []focusTarget{focusWorkspace, focusRail} {
		m.focus = focus
		rendered := m.render()
		line := strings.Split(rendered, "\n")[appHeaderHeight]
		expected := m.colors.Border
		if focus == focusRail {
			expected = m.colors.Focus
		}
		if !strings.Contains(line, m.paint("│", expected)) {
			t.Fatalf("focus %v has no owning-pane indicator: %q", focus, line)
		}
		listWidth, _ := inspectorWidths(calculateLayout(m.width, m.height, false).workspace.width)
		if !strings.Contains(rendered, m.worklogRows(listWidth-2, 1)[0]) {
			t.Fatal("missing full-row selection styling")
		}
	}
}

func TestRenderReferenceState(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.statusLoading = false
	m.worklogLoading = false
	m.statusChecked = deps.now()
	m.statusItems = []status.Item{
		{Category: "local", Target: "config", Status: "ok", Message: "config is valid"},
		{Category: "local", Target: "storage", Status: "ok", Message: "local SQLite storage is writable"},
		{Category: "routing", Target: "rules", Status: "warning", Message: "no routing rules configured"},
	}
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "row-1", IssueKey: "WL-1234", StartedAtUTC: time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC), DurationSeconds: 7200, Description: "Implement ledger export", Revision: 1},
		{ID: "row-2", IssueKey: "WL-1256", StartedAtUTC: time.Date(2026, 5, 21, 11, 15, 0, 0, time.UTC), DurationSeconds: 5400, Description: "Fix totals rounding edge case", Revision: 1},
		{ID: "row-3", IssueKey: "WL-1220", StartedAtUTC: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), DurationSeconds: 3600, Description: "Add context collision hints", Revision: 1},
	}
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.overlay = deleteOverlay
	m.deleteSelection = []worklogs.LocalWorklog{m.worklogs[0]}
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != 140 || height != 38 {
		t.Fatalf("reference render = %dx%d, want 140x38", width, height)
	}
	if path := os.Getenv("WORKLEDGER_TUI_RENDER_PATH"); path != "" {
		if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
			t.Fatalf("write reference render: %v", err)
		}
	}
}

func TestWorklogDetailRendersLifecycleTimestampsInLocalTime(t *testing.T) {
	ws, deps := testWorkspace(t)
	ws.cfg.Location = time.FixedZone("UTC+2", 2*60*60)
	m := newModel(context.Background(), deps, ws, nil)
	m.worklogs = []worklogs.LocalWorklog{{
		ID:              "row-1",
		IssueKey:        "WL-1234",
		StartedAtUTC:    time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
		DurationSeconds: 3600,
		Description:     "Implement ledger export",
		CreatedAt:       time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 21, 7, 45, 0, 0, time.UTC),
		Revision:        2,
	}}
	m.issueTotalIssue = "WL-1234"
	m.issueTotal = 5*3600 + 30*60

	detail := m.renderWorklogDetail()
	for _, expected := range []string{
		"Local issue total 5h 30m",
		"Created           2026-05-20 16:30",
		"Updated           2026-05-21 09:45",
	} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("worklog detail missing %q:\n%s", expected, detail)
		}
	}
}

func TestRenderAutomaticAddState(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.statusLoading = false
	m.worklogLoading = false
	m.statusChecked = deps.now()
	m.statusItems = []status.Item{
		{Category: "local", Target: "config", Status: "ok", Message: "config is valid"},
		{Category: "local", Target: "storage", Status: "ok", Message: "local SQLite storage is writable"},
	}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "row-1", IssueKey: "APPS-993", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
		DurationSeconds: 5400, Description: "Existing worklog", Revision: 1,
	}}
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.dayContext = worklogs.ContextDay{Date: "2026-05-21", BookedSeconds: 5400}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.openAddForm()
	m.form.inputs[issueInput].SetValue("APPS-992")
	m.form.inputs[durationInput].SetValue("1h30m")
	m.form.inputs[descriptionInput].SetValue("Implement placement preview")
	m.form.previewRecords = []worklogs.LocalWorklog{{
		IssueKey: "APPS-992", StartedAtUTC: time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC),
		DurationSeconds: 5400, Description: "Implement placement preview",
	}}
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != 140 || height != 38 {
		t.Fatalf("automatic add render = %dx%d, want 140x38", width, height)
	}
	if path := os.Getenv("WORKLEDGER_TUI_ADD_RENDER_PATH"); path != "" {
		if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
			t.Fatalf("write automatic add render: %v", err)
		}
	}
}
