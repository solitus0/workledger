package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

	next, _ := m.Update(mouseClick(1, 1, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != statusTab || m.focus == focusRail {
		t.Fatalf("status click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(31, 1, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != statusTab || m.focus == focusRail {
		t.Fatalf("workspace click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(29, 6, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != worklogsTab || m.focus == focusRail {
		t.Fatalf("worklogs click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	m.focus = focusRail
	next, _ = m.Update(mouseClick(30, 1, tea.MouseLeft))
	m = next.(model)
	if m.focus != focusRail {
		t.Fatal("separator click changed focus")
	}

	next, _ = m.Update(mouseClick(31, m.height-3, tea.MouseLeft))
	m = next.(model)
	if m.focus != focusRail {
		t.Fatal("footer click changed focus")
	}

	next, _ = m.Update(mouseClick(31, m.height-4, tea.MouseLeft))
	m = next.(model)
	if m.focus == focusRail {
		t.Fatal("bottom workspace cell did not receive focus")
	}

	next, _ = m.Update(mouseClick(29, 12, tea.MouseLeft))
	m = next.(model)
	if m.activeTab != plansTab || m.focus == focusRail {
		t.Fatalf("plans click tab=%v railFocused=%v", m.activeTab, m.focus == focusRail)
	}

	next, _ = m.Update(mouseClick(29, 22, tea.MouseLeft))
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
	for _, expected := range []string{"▓ proposed", "1 worklog · 12:00–13:00", "After"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("manual preview render missing %q:\n%s", expected, rendered)
		}
	}
	if timeline := m.renderDayTimeline(m.form.previewRecords, ""); !strings.Contains(timeline, "▓") {
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
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+P Manual") || !strings.Contains(footer, "Ctrl+O Allow overtime") || !strings.Contains(footer, "Ctrl+L Use lunch time") {
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
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+O Disable overtime") || !strings.Contains(footer, "Ctrl+L Use lunch time") {
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
	if strings.Contains(rendered, "Options") || strings.Contains(rendered, "· lunch") {
		t.Fatalf("enabled automatic options are duplicated or not reflected in the legend:\n%s", rendered)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "Ctrl+O Disable overtime") || !strings.Contains(footer, "Ctrl+L Reserve lunch") {
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
		helpOverlay, deleteOverlay, deleteDayOverlay, forceOverlay, reloadOverlay,
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
	if body := m.renderForm(); !strings.Contains(body, "\n\n─ Placement") {
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
	for _, expected := range []string{"Worklog details", "› Issue", "│ Duration", "│ Description", "─ Placement", "[Fill]", "─ Calculated", "Ctrl+P Manual", "Ctrl+O OT on", "Ctrl+L Use lunch", "09:30–11:00", "Ctrl+S create 1 worklog"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("automatic add render missing %q", expected)
		}
	}
	if body := m.renderForm(); strings.Contains(body, "Ctrl+S") || strings.Contains(body, "Esc") {
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
	for _, expected := range []string{"Start time", "[Manual]", "─ Calculated", "▓"} {
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
	regularIndex := strings.Index(timeline, "▓")
	boundaryIndex := strings.Index(timeline, "│")
	overtimeIndex := strings.Index(timeline, "▒")
	if !strings.HasPrefix(timeline, "08:00 ") || !strings.HasSuffix(timeline, " 20:00") {
		t.Fatalf("overtime timeline did not extend through preview: %q", timeline)
	}
	if regularIndex < 0 || boundaryIndex <= regularIndex || overtimeIndex <= boundaryIndex {
		t.Fatalf("overtime timeline order regular=%d boundary=%d overtime=%d: %q", regularIndex, boundaryIndex, overtimeIndex, timeline)
	}
	if legend := m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, "▒ overtime") || !strings.Contains(legend, "│ day end") {
		t.Fatalf("overtime legend = %q", legend)
	}

	m.width, m.height = minimumWidth, 34
	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != 34 {
		t.Fatalf("narrow overtime add render = %dx%d, want %dx%d", width, height, minimumWidth, 34)
	}
	if !strings.Contains(rendered, "▒ OT") || !strings.Contains(rendered, "│ end") {
		t.Fatalf("narrow overtime render does not use compact legend:\n%s", rendered)
	}

	m.width, m.height = minimumWidth, minimumHeight
	rendered = m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("overtime add render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"│", "▒", "20:00"} {
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
	if !strings.HasSuffix(timeline, " 17:00") || strings.Contains(timeline, "│") || strings.Contains(timeline, "▒") {
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
	if !strings.Contains(legend, "· lunch") || strings.Contains(legend, "─ free") {
		t.Fatalf("fully booked legend = %q, want booked and lunch without free", legend)
	}

	freeStart := time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC)
	freeEnd := time.Date(2026, 5, 21, 17, 0, 0, 0, time.UTC)
	m.worklogs[1].DurationSeconds = 3 * 3600
	m.dayContext.FreeSlots = []worklogs.ContextFreeSlot{{Start: freeStart, End: freeEnd, DurationSeconds: 3600}}
	if legend = m.renderTimelineLegend(nil, ""); !strings.Contains(legend, "─ free") {
		t.Fatalf("legend with free interval = %q, want free", legend)
	}

	preview := []worklogs.LocalWorklog{{StartedAtUTC: freeStart, DurationSeconds: 3600}}
	legend = m.renderTimelineLegend(preview, "")
	if !strings.Contains(legend, "▓ proposed") || strings.Contains(legend, "─ free") {
		t.Fatalf("legend after preview fills final free interval = %q", legend)
	}

	m.worklogs[1].DurationSeconds = 4 * 3600
	preview = []worklogs.LocalWorklog{{StartedAtUTC: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), DurationSeconds: 3600}}
	legend = m.renderTimelineLegend(preview, "")
	if !strings.Contains(legend, "▓ proposed") || strings.Contains(legend, "· lunch") {
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
		if strings.Contains(unselected, "·") {
			t.Fatalf("width %d rendered lunch over persisted worklog: %q", width, unselected)
		}
		legend := m.renderTimelineLegend(nil, "selected")
		if strings.Contains(legend, "· lunch") || !strings.Contains(legend, "─ free") || !strings.Contains(legend, "─ selected") {
			t.Fatalf("width %d legend does not match fully booked lunch timeline: %q", width, legend)
		}

		lines := strings.Split(m.renderDayTimeline(nil, "selected"), "\n")
		if len(lines) != 3 || lines[1] != unselected || strings.Count(lines[0], "▔") != strings.Count(unselected, "█") || strings.Count(lines[2], "▁") != strings.Count(unselected, "█") {
			t.Fatalf("width %d selection does not outline the complete booked fill: %q", width, lines)
		}

		withoutWorklog := m
		withoutWorklog.worklogs = nil
		configuredLunchCells := strings.Count(withoutWorklog.renderDayTimeline(nil, ""), "·")
		m.worklogs[0].DurationSeconds = 90 * 60
		partialOverlap := m.renderDayTimeline(nil, "")
		visibleLunchCells := strings.Count(partialOverlap, "·")
		if visibleLunchCells == 0 || visibleLunchCells >= configuredLunchCells {
			t.Fatalf("width %d did not replace only the booked portion of lunch: baseline=%d overlap=%d timeline=%q", width, configuredLunchCells, visibleLunchCells, partialOverlap)
		}
		if legend = m.renderTimelineLegend(nil, "selected"); !strings.Contains(legend, "· lunch") {
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

	detailLines := strings.Split(m.renderWeekDayDetail(10), "\n")
	if len(detailLines) < 2 || strings.Contains(detailLines[1], "·") || strings.Contains(strings.Join(detailLines, "\n"), "· lunch") {
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
			if !strings.Contains(timeline, "█") || !strings.Contains(timeline, "·") {
				t.Fatalf("timeline lost booked time or non-overlapping lunch: %q", timeline)
			}
			if legend := m.renderTimelineLegend(nil, ""); !strings.Contains(legend, "· lunch") {
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
	overtimeIndex := strings.Index(timeline, "▒")
	boundaryIndex := strings.Index(timeline, "│")
	regularIndex := strings.Index(timeline, "█")
	if !strings.HasPrefix(timeline, "07:00 ") || !strings.HasSuffix(timeline, " 17:00") {
		t.Fatalf("early-overtime timeline has wrong bounds: %q", timeline)
	}
	if overtimeIndex < 0 || boundaryIndex <= overtimeIndex || regularIndex <= boundaryIndex {
		t.Fatalf("early-overtime timeline order overtime=%d boundary=%d regular=%d: %q", overtimeIndex, boundaryIndex, regularIndex, timeline)
	}
	legend := m.renderTimelineLegend(nil, "")
	if !strings.Contains(legend, "▒ overtime") || !strings.Contains(legend, "│ day start") {
		t.Fatalf("early-overtime legend = %q", legend)
	}
}

func TestWorklogListRendersPersistentCurrentTimeline(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "selected", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC), DurationSeconds: 3 * 3600},
		{ID: "late", IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 16, 30, 0, 0, time.UTC), DurationSeconds: 5 * 3600},
	}
	m.dayContext = worklogs.ContextDay{Date: "2026-05-21", BookedSeconds: 8 * 3600}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}

	rendered := m.render()
	for _, expected := range []string{
		"Thursday, 2026-05-21 · 8h 00m logged · target met",
		"[Day]  Week",
		"08:00",
		"▔",
		"─ selected",
		"▒ overtime",
		"│ day end",
		"21:30",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("daily list timeline missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "▓ proposed") {
		t.Fatalf("persisted-only timeline unexpectedly includes preview legend:\n%s", rendered)
	}
	if strings.Contains(rendered, "Current") {
		t.Fatalf("daily timeline retained redundant Current label:\n%s", rendered)
	}
	timeline := m.renderDayTimeline(nil, "selected")
	timelineLines := strings.Split(timeline, "\n")
	if len(timelineLines) != 3 {
		t.Fatalf("selection focus lines = %d, want 3: %q", len(timelineLines), timeline)
	}
	for _, line := range timelineLines {
		if lipgloss.Width(line) != lipgloss.Width(timelineLines[1]) {
			t.Fatalf("selection focus changed timeline width: %q", timeline)
		}
	}
	if timelineLines[1] != m.renderDayTimeline(nil, "") || !strings.Contains(timelineLines[0], "▔") ||
		!strings.Contains(timelineLines[1], "█") || !strings.Contains(timelineLines[2], "▁") ||
		strings.Contains(timeline, "13:00–16:00") {
		t.Fatalf("selection focus rails are incomplete or repeat row details: %q", timeline)
	}
	renderedLines := strings.Split(rendered, "\n")
	bottomRailLine, legendLine := -1, -1
	for index, line := range renderedLines {
		if strings.Contains(line, "▁") {
			bottomRailLine = index
		}
		if strings.Contains(line, "─ selected") {
			legendLine = index
		}
	}
	if bottomRailLine < 0 || legendLine != bottomRailLine+2 {
		t.Fatalf("timeline selection and legend are not separated by one blank row: selection=%d legend=%d\n%s", bottomRailLine, legendLine, rendered)
	}
	selectorLine, topRailLine := -1, -1
	for index, line := range renderedLines {
		if strings.Contains(line, "[Day]  Week") {
			selectorLine = index
		}
		if strings.Contains(line, "▔") && !strings.Contains(line, "selected") {
			topRailLine = index
			break
		}
	}
	if selectorLine < 0 || topRailLine != selectorLine+2 {
		t.Fatalf("day/week selector and timeline are not separated by one blank row: selector=%d timeline=%d\n%s", selectorLine, topRailLine, rendered)
	}
}

func TestDayTimelineKeepsFillVisibleForShortSelectedWorklog(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "short", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 10, 15, 0, 0, time.UTC), DurationSeconds: 15 * 60,
	}}

	unselected := m.renderDayTimeline(nil, "")
	lines := strings.Split(m.renderDayTimeline(nil, "short"), "\n")
	if len(lines) != 3 || lines[1] != unselected || strings.Count(lines[0], "▔") != 1 || strings.Contains(m.renderDayTimeline(nil, "short"), "10:15–10:30") {
		t.Fatalf("short selected worklog has no visible booked fill: %q", lines)
	}
}

func TestSelectedTimelineFitsMinimumViewportWithoutColor(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "selected", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 10, 15, 0, 0, time.UTC), DurationSeconds: 15 * 60,
	}}

	rendered := m.render()
	if width, height := lipgloss.Width(rendered), lipgloss.Height(rendered); width != minimumWidth || height != minimumHeight {
		t.Fatalf("selected day render = %dx%d, want %dx%d", width, height, minimumWidth, minimumHeight)
	}
	for _, expected := range []string{"▔", "█", "▁"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("minimum no-color timeline missing %q:\n%s", expected, rendered)
		}
	}
}

func TestDayTimelineSelectionCoversEntireLongWorklog(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "long", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 2*3600 + 15*60,
	}}

	unselected := m.renderDayTimeline(nil, "")
	lines := strings.Split(m.renderDayTimeline(nil, "long"), "\n")
	if len(lines) != 3 {
		t.Fatalf("long selected worklog timeline lines = %d, want 3: %q", len(lines), lines)
	}
	if lines[1] != unselected || strings.Count(lines[0], "▔") != strings.Count(unselected, "█") {
		t.Fatalf("selection rails do not match the complete booked fill: unselected=%q selected=%q", unselected, lines)
	}
}

func TestDayTimelineSelectionStopsBeforeAdjacentWorklog(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{
		{ID: "selected", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 2*3600 + 15*60},
		{ID: "adjacent", IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 10, 15, 0, 0, time.UTC), DurationSeconds: 15 * 60},
	}

	unselected := m.renderDayTimeline(nil, "")
	selectedOnly := m
	selectedOnly.worklogs = m.worklogs[:1]
	selectedWidth := strings.Count(selectedOnly.renderDayTimeline(nil, ""), "█")
	lines := strings.Split(m.renderDayTimeline(nil, "selected"), "\n")
	if lines[1] != unselected || strings.Count(lines[0], "▔") != selectedWidth || selectedWidth >= strings.Count(unselected, "█") {
		t.Fatalf("selection scope does not distinguish the adjacent worklog: %q", lines)
	}
}

func TestDayTimelineSelectionRailsPreserveBookedColor(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "selected", IssueKey: "WL-100", StartedAtUTC: time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC), DurationSeconds: 2*3600 + 15*60,
	}}

	lines := strings.Split(m.renderDayTimeline(nil, "selected"), "\n")
	focus := m.paint("▔", m.colors.Focus)
	booked := m.paint("█", m.colors.Information)
	if len(lines) != 3 || !strings.Contains(lines[0], focus) || !strings.Contains(lines[1], booked) || lines[1] != m.renderDayTimeline(nil, "") {
		t.Fatalf("selection focus does not preserve booked color or scale: %q", lines)
	}
}

func TestDayTimelineSelectionSpansRegularAndOvertimePortions(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	m.selectedDate = time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	m.contextSettings = worklogs.ContextSettings{DayStart: "08:00", DayEnd: "17:00"}
	m.worklogs = []worklogs.LocalWorklog{{
		ID: "selected", IssueKey: "WL-200", StartedAtUTC: time.Date(2026, 5, 21, 16, 30, 0, 0, time.UTC), DurationSeconds: 2 * 3600,
	}}

	timeline := m.renderDayTimeline(nil, "selected")
	lines := strings.Split(timeline, "\n")
	if len(lines) != 3 || lipgloss.Width(lines[0]) != lipgloss.Width(lines[1]) || lipgloss.Width(lines[1]) != lipgloss.Width(lines[2]) {
		t.Fatalf("overtime selection changed timeline dimensions: %q", timeline)
	}
	unselected := m.renderDayTimeline(nil, "")
	if lines[1] != unselected || !strings.Contains(lines[0], "▔") || !strings.Contains(lines[1], "▒") || !strings.Contains(m.renderTimelineLegend(nil, "selected"), "─ selected") {
		t.Fatalf("overtime selection changed scale or lost its focus treatment: %q", timeline)
	}
	boundaryByteIndex := strings.Index(lines[1], "│")
	boundaryIndex := -1
	if boundaryByteIndex >= 0 {
		boundaryIndex = utf8.RuneCountInString(lines[1][:boundaryByteIndex])
	}
	if boundaryIndex < 0 || []rune(lines[0])[boundaryIndex] != '▔' || []rune(lines[2])[boundaryIndex] != '▁' {
		t.Fatalf("selection rails break at day-end boundary: %q", timeline)
	}
}

func TestWeekViewRendersOverviewAndSelectedDayAtMinimumViewport(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = minimumWidth, minimumHeight
	m.worklogView = weekWorklogView
	m.weekDays = testWeekDays()
	m.weekSummary = worklogs.ContextSummary{BookedSeconds: 32*3600 + 30*60, WorklogCount: 5, CollisionCount: 1}
	m.contextSettings = worklogs.ContextSettings{
		DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800,
		Lunch: &worklogs.ContextLunch{Start: "12:00", End: "12:45"},
	}
	m.selectWeekDate(m.selectedDate)
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
		"DAY   DATE",
		"> Thu",
		"-6h 00m",
		"Thursday, 2026-05-21",
		"07:00",
		"▒ overtime",
		"│ day start",
		"h/l Week",
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
		if strings.Contains(line, "DAY   DATE") {
			headerLine = index
			break
		}
	}
	if selectorLine < 0 || headerLine != selectorLine+2 {
		t.Fatalf("week selector and overview are not separated by one blank row: selector=%d overview=%d\n%s", selectorLine, headerLine, rendered)
	}
	detailLines := strings.Split(m.renderWeekDayDetail(10), "\n")
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
	if timelineLine < 0 || legendLine != timelineLine+2 {
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
		m.paint("OK        1", m.colors.Success),
		m.muted("Warnings  0"),
		m.paint("0h 00m logged", m.colors.Information),
		m.paint("8h 00m remaining", m.colors.Warning),
	} {
		if !strings.Contains(rail+m.renderDayHeading("Thursday", 80), expected) {
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
	form := m.renderForm()
	for _, expected := range []string{
		m.paintBold(fmt.Sprintf("%-12s", "› Issue"), m.colors.Focus),
		m.paint(fmt.Sprintf("%-12s", "│ Duration"), m.colors.Secondary),
		m.paint(fmt.Sprintf("%-12s", "Result"), m.colors.Information),
		m.paint(fmt.Sprintf("%-12s", "After"), m.colors.Information),
	} {
		if !strings.Contains(form, expected) {
			t.Fatalf("form missing semantic role style %q: %q", expected, form)
		}
	}
	footer := m.renderFooter(140)
	if !strings.Contains(footer, m.paintBold("Ctrl+P", m.colors.Focus)) || strings.Contains(footer, m.paint("Allow overtime", m.colors.Focus)) {
		t.Fatalf("footer does not separate key hints from action copy: %q", footer)
	}
	picker := m
	picker.form = nil
	picker.presets = []presets.Preset{{Name: "daily-standup"}}
	picker.openPresetPicker()
	footer = picker.renderFooter(140)
	if !strings.Contains(footer, m.paintBold("Ctrl+F", m.colors.Focus)) || strings.Contains(footer, m.paint("Find", m.colors.Focus)) {
		t.Fatalf("preset picker footer does not style Ctrl+F as a key chord: %q", footer)
	}
	legend := m.renderTimelineLegend(nil, "")
	for _, expected := range []string{m.paint("█", m.colors.Information), m.paint("·", m.colors.Secondary), m.paint("─", m.colors.Muted)} {
		if !strings.Contains(legend, expected) {
			t.Fatalf("timeline legend missing semantic swatch %q: %q", expected, legend)
		}
	}
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC), DurationSeconds: 3600,
	}}
	if legend = m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, m.paint("▓", m.colors.Proposed)) || !strings.Contains(legend, "proposed") {
		t.Fatalf("preview legend missing new-work swatch: %q", legend)
	}
	m.form.previewRecords = []worklogs.LocalWorklog{{
		StartedAtUTC: time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC), DurationSeconds: 2 * 3600,
	}}
	if legend = m.renderTimelineLegend(m.form.previewRecords, ""); !strings.Contains(legend, m.paint("▒", m.colors.Warning)) || !strings.Contains(legend, m.paint("│", m.colors.Secondary)) {
		t.Fatalf("overtime legend missing semantic swatches: %q", legend)
	}
}

func TestOnlyInputOwningPaneUsesFocusBorder(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = false
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 38
	focusPrefix := strings.Split(m.paint("x", m.colors.Focus), "x")[0]

	for _, railFocused := range []bool{false, true} {
		m.focus = focusWorkspace
		if railFocused {
			m.focus = focusRail
		}
		rendered := m.render()
		if count := strings.Count(rendered, focusPrefix+"╭"); count != 1 {
			t.Fatalf("railFocused=%t chrome has %d focus borders, want 1", railFocused, count)
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
