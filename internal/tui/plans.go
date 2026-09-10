package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile"
)

const planListLimit = 100

type planWindowMode int

const (
	planDayWindow planWindowMode = iota
	planWeekWindow
	planCustomWindow
)

type planFormState struct {
	direction    string
	window       planWindowMode
	targets      []reconcile.ReconcileTarget
	targetIndex  int // -1 selects every valid target.
	profiles     []string
	profileIndex int // -1 uses automatic profile selection.
	dateInputs   [2]textinput.Model
	dateFocus    int
}

type planOperationState struct {
	kind     string
	planID   string
	cancel   context.CancelFunc
	progress progress.Event
}

type planListResultMsg struct {
	generation int
	items      []reconcile.ListEntry
	err        error
}

type planLoadResultMsg struct {
	generation int
	plan       reconcile.Plan
	err        error
}

type planProgressMsg struct {
	event progress.Event
	ch    <-chan progress.Event
}

type planProgressClosedMsg struct{}

type planOperationResultMsg struct {
	kind      string
	planID    string
	reconcile reconcile.ReconcileResult
	apply     reconcile.ApplyResult
	err       error
}

type tuiProgressReporter struct {
	ch chan<- progress.Event
}

func (r tuiProgressReporter) Start(event progress.Event)  { r.send(event) }
func (r tuiProgressReporter) Event(event progress.Event)  { r.send(event) }
func (r tuiProgressReporter) Finish(event progress.Event) { r.send(event) }
func (r tuiProgressReporter) send(event progress.Event) {
	select {
	case r.ch <- event:
	default:
	}
}

func (m *model) startPlanLoad() tea.Cmd {
	if m.workspace == nil || m.workspace.plans == nil {
		return nil
	}
	m.planGen++
	m.planLoading = true
	return m.planListCmd(m.planGen)
}

func (m model) planListCmd(generation int) tea.Cmd {
	if m.workspace == nil || m.workspace.plans == nil {
		return nil
	}
	service := m.workspace.plans
	from, to := m.planCreatedRange()
	return func() tea.Msg {
		items, err := service.ListRecentPlansInCreatedRange(planListLimit, from, to)
		return planListResultMsg{generation: generation, items: items, err: err}
	}
}

func (m model) planCreatedRange() (time.Time, time.Time) {
	location := time.UTC
	if m.workspace != nil && m.workspace.cfg.Location != nil {
		location = m.workspace.cfg.Location
	}
	selectedDay := beginningOfDay(m.selectedDate, location)
	if m.planListView == planDayListView {
		return selectedDay.UTC(), selectedDay.AddDate(0, 0, 1).UTC()
	}
	weekStart, _ := weekBounds(selectedDay)
	return weekStart.UTC(), weekStart.AddDate(0, 0, 7).UTC()
}

func (m *model) startSelectedPlanLoad() tea.Cmd {
	selected := m.selectedPlanEntry()
	if selected == nil || m.workspace == nil || m.workspace.plans == nil {
		return nil
	}
	m.planGen++
	m.planLoading = true
	return m.planLoadCmd(m.planGen, selected.ID)
}

func (m model) planLoadCmd(generation int, id string) tea.Cmd {
	service := m.workspace.plans
	return func() tea.Msg {
		plan, err := service.LoadPlan(id)
		return planLoadResultMsg{generation: generation, plan: plan, err: err}
	}
}

func (m *model) openPlanForm() {
	targets := m.resolvePlanTargets("push")
	from := textinput.New()
	from.Prompt = ""
	from.Placeholder = "YYYY-MM-DD"
	from.SetValue(m.selectedDate.Format("2006-01-02"))
	to := textinput.New()
	to.Prompt = ""
	to.Placeholder = "YYYY-MM-DD"
	to.SetValue(m.selectedDate.Format("2006-01-02"))
	m.planForm = &planFormState{direction: "push", window: planDayWindow, targets: targets, targetIndex: -1, profileIndex: -1, dateInputs: [2]textinput.Model{from, to}}
}

func (m model) resolvePlanTargets(direction string) []reconcile.ReconcileTarget {
	if m.workspace == nil {
		return nil
	}
	selection, err := reconcile.ResolveSelection(m.workspace.cfg, reconcile.SelectionRequest{Direction: direction})
	if err != nil {
		return nil
	}
	return selection.Targets
}

func (m model) updatePlanForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return m, nil
	}
	key := keyMsg.String()
	switch key {
	case "esc":
		m.planForm = nil
	case "p":
		m.planForm.direction = "push"
		m.planForm.targets = m.resolvePlanTargets("push")
		m.planForm.targetIndex = -1
		m.resetPlanProfiles()
	case "u":
		m.planForm.direction = "pull"
		m.planForm.targets = m.resolvePlanTargets("pull")
		m.planForm.targetIndex = -1
		m.resetPlanProfiles()
	case "d":
		m.planForm.window = planDayWindow
		m.blurPlanDateInputs()
	case "w":
		m.planForm.window = planWeekWindow
		m.blurPlanDateInputs()
	case "c":
		m.planForm.window = planCustomWindow
		m.focusPlanDateInput()
	case "tab", "shift+tab", "up", "down":
		if m.planForm.window == planCustomWindow {
			m.planForm.dateInputs[m.planForm.dateFocus].Blur()
			m.planForm.dateFocus = 1 - m.planForm.dateFocus
			m.focusPlanDateInput()
		}
	case "x":
		if len(m.planForm.targets) > 0 {
			m.planForm.targetIndex++
			if m.planForm.targetIndex >= len(m.planForm.targets) {
				m.planForm.targetIndex = -1
			}
			m.resetPlanProfiles()
		}
	case "v":
		if len(m.planForm.profiles) > 0 {
			m.planForm.profileIndex++
			if m.planForm.profileIndex >= len(m.planForm.profiles) {
				m.planForm.profileIndex = -1
			}
		}
	case "enter", "ctrl+s":
		if len(m.planForm.targets) == 0 {
			m.setNotice(noticeWarning, "no valid reconcile targets are configured")
			return m, nil
		}
		if _, _, err := m.planFormWindow(); err != nil {
			m.setNotice(noticeError, err.Error())
			return m, nil
		}
		m.overlay = reconcilePlanOverlay
	default:
		if m.planForm.window == planCustomWindow {
			input := m.planForm.dateInputs[m.planForm.dateFocus]
			updated, cmd := input.Update(keyMsg)
			m.planForm.dateInputs[m.planForm.dateFocus] = updated
			return m, cmd
		}
	}
	return m, nil
}

func (m *model) focusPlanDateInput() {
	if m.planForm == nil {
		return
	}
	for index := range m.planForm.dateInputs {
		m.planForm.dateInputs[index].Blur()
	}
	m.planForm.dateInputs[m.planForm.dateFocus].Focus()
}

func (m *model) blurPlanDateInputs() {
	if m.planForm == nil {
		return
	}
	for index := range m.planForm.dateInputs {
		m.planForm.dateInputs[index].Blur()
	}
}

func (m *model) resetPlanProfiles() {
	if m.planForm == nil {
		return
	}
	m.planForm.profiles = nil
	m.planForm.profileIndex = -1
	if m.planForm.direction != "push" || m.planForm.targetIndex < 0 || m.planForm.targetIndex >= len(m.planForm.targets) {
		return
	}
	target := m.planForm.targets[m.planForm.targetIndex]
	var profiles map[string]config.JiraRouteProfile
	switch target.AdapterFamily {
	case "jira-cloud":
		if cfg := m.workspace.cfg.File.JiraCloud; cfg != nil {
			if instance, ok := cfg.Instances[target.Instance]; ok && instance.Routing != nil {
				profiles = instance.Routing.Profiles
			}
		}
	case "jira-data-center":
		if cfg := m.workspace.cfg.File.JiraData; cfg != nil {
			if instance, ok := cfg.Instances[target.Instance]; ok && instance.Routing != nil {
				profiles = instance.Routing.Profiles
			}
		}
	}
	for name := range profiles {
		m.planForm.profiles = append(m.planForm.profiles, name)
	}
	sort.Strings(m.planForm.profiles)
}

func (m model) updatePlans(key string) (tea.Model, tea.Cmd) {
	if m.planOperation != nil {
		if key == "esc" {
			m.planOperation.cancel()
			m.setNotice(noticeInfo, "canceling plan operation…")
		}
		return m, nil
	}
	if m.plan != nil {
		switch key {
		case "esc":
			m.plan = nil
			m.planItemSelected = 0
		case "up", "k":
			m.planItemSelected = max(0, m.planItemSelected-1)
		case "down", "j":
			m.planItemSelected = min(max(0, len(m.visiblePlanItems())-1), m.planItemSelected+1)
		case "a":
			m.planShowAll = !m.planShowAll
			m.planItemSelected = clampIndex(m.planItemSelected, len(m.visiblePlanItems()))
		case "r":
			return m, m.startSelectedPlanLoad()
		case "A":
			if m.planExecutableCount("not_attempted") == 0 {
				m.setNotice(noticeInfo, "this plan has no unapplied ready scopes")
				return m, nil
			}
			m.overlay = applyPlanOverlay
		case "f":
			if m.planExecutableCount("failed") == 0 {
				m.setNotice(noticeInfo, "this plan has no failed ready scopes")
				return m, nil
			}
			m.overlay = retryFailedPlanOverlay
		case "u":
			if m.planExecutableCount("uncertain") == 0 {
				m.setNotice(noticeInfo, "this plan has no uncertain ready scopes")
				return m, nil
			}
			m.overlay = retryUncertainPlanOverlay
		}
		return m, nil
	}

	switch key {
	case "d":
		if m.planListView == planDayListView {
			return m, nil
		}
		m.planListView = planDayListView
		m.plans = nil
		m.planSelected = 0
		return m, m.startPlanLoad()
	case "w":
		if m.planListView == planWeekListView {
			return m, nil
		}
		m.planListView = planWeekListView
		m.plans = nil
		m.planSelected = 0
		return m, m.startPlanLoad()
	case "up", "k":
		m.planSelected = max(0, m.planSelected-1)
	case "down", "j":
		m.planSelected = min(max(0, len(m.plans)-1), m.planSelected+1)
	case "left", "h":
		days := -1
		if m.planListView == planWeekListView {
			days = -7
		}
		m.selectedDate = m.selectedDate.AddDate(0, 0, days)
		m.plans = nil
		m.planSelected = 0
		return m, m.refreshCmd(refreshDateData)
	case "right", "l":
		days := 1
		if m.planListView == planWeekListView {
			days = 7
		}
		m.selectedDate = m.selectedDate.AddDate(0, 0, days)
		m.plans = nil
		m.planSelected = 0
		return m, m.refreshCmd(refreshDateData)
	case "t":
		m.selectedDate = beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
		m.plans = nil
		m.planSelected = 0
		return m, m.refreshCmd(refreshDateData)
	case "enter":
		return m, m.startSelectedPlanLoad()
	case "n":
		m.openPlanForm()
	case "r":
		return m, m.startPlanLoad()
	}
	return m, nil
}

func (m *model) startPlanOperation(kind, retryScope string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	progressCh := make(chan progress.Event, 32)
	planID := ""
	if m.plan != nil {
		planID = m.plan.ID
	}
	m.planOperation = &planOperationState{kind: kind, planID: planID, cancel: cancel}
	operation := m.planOperationCmd(ctx, kind, retryScope, progressCh)
	summary := planID
	if kind == "plan.reconcile" && m.planForm != nil {
		summary = m.planForm.direction + " · " + m.planFormWindowLabel()
	}
	return tea.Batch(m.beginActivity(kind, summary, operation), waitPlanProgressCmd(progressCh))
}

func (m model) planOperationCmd(ctx context.Context, kind, retryScope string, progressCh chan progress.Event) tea.Cmd {
	service, cfg := m.workspace.plans, m.workspace.cfg
	planID := ""
	if m.plan != nil {
		planID = m.plan.ID
	}
	var request reconcile.ReconcileRequest
	if m.planForm != nil {
		request = m.planReconcileRequest()
	}
	return func() tea.Msg {
		defer close(progressCh)
		reporter := tuiProgressReporter{ch: progressCh}
		switch kind {
		case "plan.reconcile":
			result, err := service.Reconcile(ctx, cfg, request, reconcile.PlanOptions{Reporter: reporter})
			return planOperationResultMsg{kind: kind, reconcile: result, err: err}
		case "plan.apply":
			result, err := service.ApplyPlan(ctx, cfg, planID, reconcile.ApplyOptions{Reporter: reporter})
			return planOperationResultMsg{kind: kind, planID: planID, apply: result, err: err}
		default:
			result, err := service.RetryPlan(ctx, cfg, planID, retryScope, reconcile.ApplyOptions{Reporter: reporter})
			return planOperationResultMsg{kind: kind, planID: planID, apply: result, err: err}
		}
	}
}

func waitPlanProgressCmd(ch <-chan progress.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return planProgressClosedMsg{}
		}
		return planProgressMsg{event: event, ch: ch}
	}
}

func (m model) planReconcileRequest() reconcile.ReconcileRequest {
	from, to, _ := m.planFormWindow()
	request := reconcile.ReconcileRequest{Direction: m.planForm.direction, WindowFrom: from.UTC(), WindowTo: to.UTC()}
	if m.planForm.targetIndex >= 0 && m.planForm.targetIndex < len(m.planForm.targets) {
		target := m.planForm.targets[m.planForm.targetIndex]
		request.Adapters = []string{target.AdapterFamily}
		request.Instances = []string{target.Instance}
	}
	if m.planForm.profileIndex >= 0 && m.planForm.profileIndex < len(m.planForm.profiles) {
		request.RouteProfile = m.planForm.profiles[m.planForm.profileIndex]
	}
	return request
}

func (m model) planFormWindow() (time.Time, time.Time, error) {
	if m.planForm != nil && m.planForm.window == planWeekWindow {
		from, finalDay := weekBounds(m.selectedDate)
		return from, finalDay.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
	}
	if m.planForm != nil && m.planForm.window == planCustomWindow {
		from, fromErr := time.ParseInLocation("2006-01-02", strings.TrimSpace(m.planForm.dateInputs[0].Value()), m.workspace.cfg.Location)
		to, toErr := time.ParseInLocation("2006-01-02", strings.TrimSpace(m.planForm.dateInputs[1].Value()), m.workspace.cfg.Location)
		if fromErr != nil || toErr != nil {
			return time.Time{}, time.Time{}, errors.New("custom plan dates must use YYYY-MM-DD")
		}
		if to.Before(from) {
			from, to = to, from
		}
		return from, to.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
	}
	from := beginningOfDay(m.selectedDate, m.workspace.cfg.Location)
	return from, from.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
}

func (m model) planFormWindowLabel() string {
	from, to, err := m.planFormWindow()
	if err != nil {
		return "invalid custom range"
	}
	if m.planForm != nil && m.planForm.window == planWeekWindow {
		return from.Format("2006-01-02") + "–" + to.Format("2006-01-02")
	}
	return from.Format("2006-01-02")
}

func (m model) planFormTargetLabel() string {
	if m.planForm != nil && m.planForm.targetIndex >= 0 && m.planForm.targetIndex < len(m.planForm.targets) {
		return planTargetLabel(m.planForm.targets[m.planForm.targetIndex])
	}
	return "all valid configured targets"
}

func (m model) planFormProfileLabel() string {
	if m.planForm != nil && m.planForm.profileIndex >= 0 && m.planForm.profileIndex < len(m.planForm.profiles) {
		return m.planForm.profiles[m.planForm.profileIndex]
	}
	return "automatic"
}

func (m model) planApplyImpact() string {
	if m.plan == nil {
		return ""
	}
	if m.plan.Direction == "pull" {
		return "Local rows may be replaced. Removed rows will be archived to Trash."
	}
	return "Remote worklogs may be created or deleted."
}

func (m model) selectedPlanEntry() *reconcile.ListEntry {
	if m.planSelected < 0 || m.planSelected >= len(m.plans) {
		return nil
	}
	return &m.plans[m.planSelected]
}

func (m model) visiblePlanItems() []reconcile.PlanItem {
	if m.plan == nil {
		return nil
	}
	if m.planShowAll {
		return m.plan.Items
	}
	items := make([]reconcile.PlanItem, 0, len(m.plan.Items))
	for _, item := range m.plan.Items {
		if item.PlanStatus == "ready" {
			items = append(items, item)
		}
	}
	return items
}

func (m model) selectedPlanItem() *reconcile.PlanItem {
	items := m.visiblePlanItems()
	if m.planItemSelected < 0 || m.planItemSelected >= len(items) {
		return nil
	}
	return &items[m.planItemSelected]
}

func (m model) planExecutableCount(state string) int {
	if m.plan == nil {
		return 0
	}
	count := 0
	for _, item := range m.plan.Items {
		if item.PlanStatus == "ready" && item.ExecutionState == state {
			count++
		}
	}
	return count
}

func (m model) planMutationCounts() (creates, deletes int, complete bool) {
	if m.plan == nil {
		return 0, 0, false
	}
	complete = true
	for _, item := range m.plan.Items {
		if item.PlanStatus != "ready" || item.ExecutionState != "not_attempted" {
			continue
		}
		if !item.HasDiffMetrics() {
			complete = false
		}
		creates += item.InspectionSummary.CreateRowCount
		deletes += item.InspectionSummary.DeleteRowCount
	}
	return creates, deletes, complete
}

func (m *model) restorePlanSelection(id string) {
	for index := range m.plans {
		if m.plans[index].ID == id {
			m.planSelected = index
			return
		}
	}
	m.planSelected = clampIndex(m.planSelected, len(m.plans))
}

func planTargetLabel(target reconcile.ReconcileTarget) string {
	return strings.ReplaceAll(target.AdapterFamily, "-", " ") + "/" + target.Instance
}

func planOperationErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "plan operation canceled; review the plan for uncertain outcomes"
	}
	return fmt.Sprintf("plan operation failed: %v", err)
}
