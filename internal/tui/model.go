package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

const (
	minimumWidth               = 100
	minimumHeight              = 28
	issueSuggestionLimit       = 100
	descriptionSuggestionLimit = 20
	presetSuggestionLimit      = 100
)

type tab int

const (
	statusTab tab = iota
	worklogsTab
	plansTab
	presetsTab
	trashTab
)

type worklogView int

const (
	dayWorklogView worklogView = iota
	weekWorklogView
)

type statusView int

const (
	statusDiagnosticsView statusView = iota
	statusConfigView
)

type planListView int

const (
	planDayListView planListView = iota
	planWeekListView
)

type overlay int

const (
	noOverlay overlay = iota
	helpOverlay
	deleteOverlay
	deleteDayOverlay
	forceOverlay
	reloadOverlay
	restoreOverlay
	restoreDayOverlay
	deletePresetOverlay
	reconcilePlanOverlay
	applyPlanOverlay
	retryFailedPlanOverlay
	retryUncertainPlanOverlay
)

type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeSuccess
	noticeWarning
	noticeError
)

type focusTarget int

const (
	focusWorkspace focusTarget = iota
	focusRail
	focusActivity
)

type runtimeState struct {
	ctx              context.Context
	deps             dependencies
	workspace        *workspace
	workspaceErr     error
	workspaceLoading bool
	workspaceGen     int
}

type shellState struct {
	activeTab    tab
	focus        focusTarget
	width        int
	height       int
	colors       Palette
	colorReady   bool
	selectedDate time.Time
	notice       string
	noticeKind   noticeKind
}

type worklogsScreen struct {
	worklogView              worklogView
	worklogs                 []worklogs.LocalWorklog
	weekDays                 []worklogs.ContextDay
	dayContext               worklogs.ContextDay
	contextSettings          worklogs.ContextSettings
	issueMetadata            map[string]worklogs.IssueMetadata
	worklogSelected          int
	worklogLoading           bool
	worklogGen               int
	issueTotalIssue          string
	issueTotal               int
	issueTotalLoading        bool
	issueTotalErr            string
	issueTotalGen            int
	issueSuggestionGen       int
	descriptionSuggestionGen int
	lastAddedIssue           string
	weekSummary              worklogs.ContextSummary
	formError                string
	conflict                 *worklogs.ConflictDetail
	selectAfterID            string
	reloadAfterForm          bool
	deleteSelection          []worklogs.LocalWorklog
}

type trashScreen struct {
	trashItems       []worklogs.TrashRecord
	trashWeekItems   []worklogs.TrashRecord
	trashSelected    int
	trashView        worklogView
	trashScope       string
	trashLoading     bool
	trashGen         int
	restoreSelection []worklogs.TrashRecord
}

type statusScreen struct {
	statusView           statusView
	statusItems          []status.Item
	statusSelected       int
	statusConfig         *config.ConfigSummary
	statusConfigIssues   []config.ValidationIssue
	statusConfigSelected int
	statusLoading        bool
	statusGen            int
	statusCtx            context.Context
	statusCancel         context.CancelFunc
	statusChecked        time.Time
	statusErr            string
}

type interactionState struct {
	overlay       overlay
	form          *formState
	planForm      *planFormState
	planOperation *planOperationState
	presetForm    *presetFormState
	presetPicker  *presetPickerState
}

type interactionKind int

const (
	interactionIdle interactionKind = iota
	interactionHelp
	interactionConfirmation
	interactionWorklogForm
	interactionPresetForm
	interactionPresetPicker
	interactionPlanForm
	interactionPlanOperation
)

func (state interactionState) kind() interactionKind {
	if state.overlay == helpOverlay {
		return interactionHelp
	}
	if state.overlay != noOverlay {
		return interactionConfirmation
	}
	if state.planOperation != nil {
		return interactionPlanOperation
	}
	if state.presetPicker != nil {
		return interactionPresetPicker
	}
	if state.presetForm != nil {
		return interactionPresetForm
	}
	if state.planForm != nil {
		return interactionPlanForm
	}
	if state.form != nil {
		return interactionWorklogForm
	}
	return interactionIdle
}

type presetsScreen struct {
	presets         []presets.Preset
	presetSelected  int
	presetLoading   bool
	presetGen       int
	presetFormError string
	presetDelete    *presets.Preset
}

type activityDrawer struct {
	activities       []activity.Entry
	activitySelected int
	activityOpen     bool
	activityLoading  bool
	activityGen      int
}

type plansScreen struct {
	plans            []reconcile.ListEntry
	plan             *reconcile.Plan
	planSelected     int
	planItemSelected int
	planShowAll      bool
	planListView     planListView
	planLoading      bool
	planGen          int
}

type model struct {
	runtimeState
	shellState
	worklogsScreen
	trashScreen
	statusScreen
	interactionState
	presetsScreen
	activityDrawer
	plansScreen
}

func (m *model) setNotice(kind noticeKind, message string) {
	m.noticeKind = kind
	m.notice = message
}

func (m *model) clearNotice() {
	m.notice = ""
}

type statusResultMsg struct {
	generation int
	report     status.Report
	err        error
	checkedAt  time.Time
}

type worklogResultMsg struct {
	generation int
	date       time.Time
	items      []worklogs.LocalWorklog
	day        worklogs.ContextDay
	settings   worklogs.ContextSettings
	metadata   map[string]worklogs.IssueMetadata
	week       worklogs.ContextSummary
	weekDays   []worklogs.ContextDay
	err        error
}

type issueTotalResultMsg struct {
	generation   int
	issueKey     string
	totalSeconds int
	err          error
}

type issueSuggestionsResultMsg struct {
	generation int
	items      []string
	err        error
}

type descriptionSuggestionsResultMsg struct {
	generation int
	issueKey   string
	items      []string
	err        error
}

type trashResultMsg struct {
	generation int
	date       time.Time
	items      []worklogs.TrashRecord
	err        error
}

type workspaceResultMsg struct {
	generation int
	workspace  *workspace
	err        error
}

type pollTickMsg time.Time

type pollResultMsg struct {
	changed         bool
	activityChanged bool
	err             error
}

type trackerConsumedMsg struct {
	err     error
	refresh refreshScope
}

type refreshScope uint8

const (
	refreshWorklogs refreshScope = 1 << iota
	refreshTrash
	refreshPresets
	refreshPlans
	refreshActivity
	refreshDateData  = refreshWorklogs | refreshTrash | refreshPlans
	refreshWorkspace = refreshDateData | refreshPresets | refreshActivity
)

type mutationResultMsg struct {
	kind    string
	records []worklogs.LocalWorklog
	deleted int
	refresh refreshScope
	err     error
}

type presetResultMsg struct {
	generation int
	items      []presets.Preset
	err        error
}

type presetMutationResultMsg struct {
	kind string
	item presets.Preset
	err  error
}

type activityListResultMsg struct {
	generation int
	items      []activity.Entry
	err        error
}

type activityResultMsg struct {
	entry activity.Entry
	inner tea.Msg
}

type previewReadyMsg struct {
	generation int
}

type previewResultMsg struct {
	generation int
	records    []worklogs.LocalWorklog
	metadata   map[string]worklogs.IssueMetadata
	err        error
}

func newModel(ctx context.Context, deps dependencies, ws *workspace, workspaceErr error) model {
	now := deps.now()
	location := time.Local
	if ws != nil && ws.cfg.Location != nil {
		location = ws.cfg.Location
	}
	selectedDate := now.In(location)
	selectedDate = time.Date(selectedDate.Year(), selectedDate.Month(), selectedDate.Day(), 0, 0, 0, 0, location)
	m := model{
		runtimeState:   runtimeState{ctx: ctx, deps: deps, workspace: ws, workspaceErr: workspaceErr, workspaceLoading: ws == nil && workspaceErr == nil, workspaceGen: 1},
		shellState:     shellState{activeTab: worklogsTab, colors: deps.theme.Dark, colorReady: !deps.waitForBackground, selectedDate: selectedDate},
		statusScreen:   statusScreen{statusLoading: true, statusGen: 1},
		worklogsScreen: worklogsScreen{worklogLoading: ws != nil, worklogGen: 1},
		trashScreen:    trashScreen{trashScope: "", trashLoading: ws != nil, trashGen: 1},
		presetsScreen:  presetsScreen{presetLoading: ws != nil, presetGen: 1},
		activityDrawer: activityDrawer{activityLoading: ws != nil, activityGen: 1},
		plansScreen:    plansScreen{planLoading: ws != nil, planGen: 1, planListView: planWeekListView},
	}
	if workspaceErr != nil {
		m.activeTab = statusTab
	}
	statusCtx, cancel := context.WithCancel(ctx)
	m.statusCtx = statusCtx
	m.statusCancel = cancel
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.statusCmd(m.statusGen), tickCmd()}
	if !m.deps.noColor {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	if m.workspace != nil {
		cmds = append(cmds, m.worklogCmd(m.worklogGen, m.selectedDate), m.trashCmd(m.trashGen, m.selectedDate), m.presetCmd(m.presetGen), m.planListCmd(m.planGen), m.activityListCmd(m.activityGen))
	} else if m.workspaceLoading {
		cmds = append(cmds, m.workspaceCmd(m.workspaceGen))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	if completed, ok := msg.(activityResultMsg); ok {
		m.upsertActivity(completed.entry)
		msg = completed.inner
		defer func() {
			nextModel, ok := next.(model)
			if !ok {
				return
			}
			nextModel.clearNotice()
			next = nextModel
			cmd = tea.Batch(cmd, nextModel.startActivityLoad())
		}()
	}
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.colors = m.deps.theme.palette(msg.IsDark())
		m.colorReady = true
		m.restyleFormInputs()
		m.restylePresetInputs()
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeForm()
		m.resizePresetInputs()
		return m, nil
	case statusResultMsg:
		if msg.generation != m.statusGen {
			return m, nil
		}
		m.statusLoading = false
		m.statusChecked = msg.checkedAt
		if msg.err != nil {
			m.statusErr = msg.err.Error()
			m.statusItems = nil
			m.statusConfig = nil
			m.statusConfigIssues = nil
			m.setNotice(noticeError, msg.err.Error())
		} else {
			m.statusErr = ""
			m.statusItems = msg.report.Items
			m.statusSelected = clampIndex(m.statusSelected, len(m.statusItems))
			m.statusConfig = msg.report.ConfigSummary
			m.statusConfigIssues = append([]config.ValidationIssue(nil), msg.report.ConfigIssues...)
			m.statusConfigSelected = clampIndex(m.statusConfigSelected, len(m.statusConfigRows()))
		}
		return m, nil
	case worklogResultMsg:
		if msg.generation != m.worklogGen || !sameDay(msg.date, m.selectedDate) {
			return m, nil
		}
		m.worklogLoading = false
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, nil
		}
		selectedID := m.selectedWorklogID()
		if m.selectAfterID != "" {
			selectedID = m.selectAfterID
			m.selectAfterID = ""
		}
		m.worklogs = msg.items
		m.dayContext = msg.day
		m.contextSettings = msg.settings
		m.issueMetadata = msg.metadata
		m.weekSummary = msg.week
		m.weekDays = msg.weekDays
		if m.form != nil && !m.form.editing {
			m.form.suggestedStart = suggestedManualStart(m.worklogs, m.selectedDate, m.workspace.cfg.Location)
		}
		m.restoreWorklogSelection(selectedID)
		return m, m.startIssueTotalLoad()
	case issueTotalResultMsg:
		selected := m.selectedWorklog()
		if msg.generation != m.issueTotalGen || selected == nil || msg.issueKey != selected.IssueKey {
			return m, nil
		}
		m.issueTotalLoading = false
		if msg.err != nil {
			m.issueTotalErr = msg.err.Error()
			return m, nil
		}
		m.issueTotal = msg.totalSeconds
		m.issueTotalErr = ""
		return m, nil
	case issueSuggestionsResultMsg:
		if m.form == nil || m.form.editing || msg.generation != m.issueSuggestionGen {
			return m, nil
		}
		if msg.err == nil {
			m.form.inputs[issueInput].SetSuggestions(msg.items)
			if m.form.suggestedIssue == "" && len(msg.items) > 0 {
				m.form.suggestedIssue = msg.items[0]
			}
		}
		return m, nil
	case descriptionSuggestionsResultMsg:
		if m.form == nil || msg.generation != m.descriptionSuggestionGen ||
			!strings.EqualFold(strings.TrimSpace(m.form.inputs[issueInput].Value()), msg.issueKey) {
			return m, nil
		}
		if msg.err == nil {
			m.form.inputs[descriptionInput].SetSuggestions(msg.items)
			m.form.suggestedDescription = ""
			if len(msg.items) > 0 {
				m.form.suggestedDescription = msg.items[0]
			}
		}
		return m, nil
	case trashResultMsg:
		if msg.generation != m.trashGen || !sameDay(msg.date, m.selectedDate) {
			return m, nil
		}
		m.trashLoading = false
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, nil
		}
		selectedID := m.selectedTrashID()
		m.trashWeekItems = msg.items
		m.trashItems = trashForDate(msg.items, m.selectedDate, m.workspace.cfg.Location)
		m.restoreTrashSelection(selectedID)
		return m, nil
	case presetResultMsg:
		if msg.generation != m.presetGen {
			return m, nil
		}
		m.presetLoading = false
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, nil
		}
		selectedID := m.selectedPresetID()
		m.presets = msg.items
		m.restorePresetSelection(selectedID)
		if m.presetPicker != nil {
			m.presetPicker.items = append([]presets.Preset(nil), msg.items...)
			m.filterPresetPicker()
		}
		return m, nil
	case planListResultMsg:
		if msg.generation != m.planGen {
			return m, nil
		}
		m.planLoading = false
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, nil
		}
		selectedID := ""
		if m.plan != nil {
			selectedID = m.plan.ID
		} else if selected := m.selectedPlanEntry(); selected != nil {
			selectedID = selected.ID
		}
		m.plans = msg.items
		m.restorePlanSelection(selectedID)
		return m, nil
	case planLoadResultMsg:
		if msg.generation != m.planGen {
			return m, nil
		}
		m.planLoading = false
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, nil
		}
		m.plan = &msg.plan
		m.planItemSelected = 0
		return m, m.startPlanLoad()
	case planProgressMsg:
		if m.planOperation == nil {
			return m, nil
		}
		m.planOperation.progress = msg.event
		return m, waitPlanProgressCmd(msg.ch)
	case planProgressClosedMsg:
		return m, nil
	case planOperationResultMsg:
		m.planOperation = nil
		m.planForm = nil
		if msg.err != nil {
			m.setNotice(noticeError, planOperationErrorMessage(msg.err))
			if msg.planID != "" {
				m.planGen++
				return m, m.planLoadCmd(m.planGen, msg.planID)
			}
			return m, m.startPlanLoad()
		}
		if msg.kind == "plan.reconcile" {
			if msg.reconcile.Plan == nil {
				m.setNotice(noticeInfo, "remote and local worklogs already match; no plan was created")
				return m, m.startPlanLoad()
			}
			m.plan = msg.reconcile.Plan
			m.planItemSelected = 0
			m.setNotice(noticeSuccess, "saved reconcile plan created; review it before applying")
			return m, m.startPlanLoad()
		}
		kind := noticeSuccess
		if msg.apply.FailedCount > 0 || msg.apply.SkippedCount > 0 {
			kind = noticeWarning
		}
		m.setNotice(kind, fmt.Sprintf("plan operation complete: applied %d, failed %d, skipped %d", msg.apply.AppliedCount, msg.apply.FailedCount, msg.apply.SkippedCount))
		m.planGen++
		return m, tea.Batch(m.planLoadCmd(m.planGen, msg.planID), m.startWorklogLoad(), m.startTrashLoad())
	case workspaceResultMsg:
		if msg.generation != m.workspaceGen {
			if msg.workspace != nil && msg.workspace.close != nil {
				_ = msg.workspace.close()
			}
			return m, nil
		}
		m.workspaceLoading = false
		if msg.err != nil {
			m.workspaceErr = msg.err
			m.setNotice(noticeError, msg.err.Error())
			m.activeTab = statusTab
			return m, nil
		}
		m.workspace = msg.workspace
		m.workspaceErr = nil
		m.selectedDate = beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
		m.activeTab = worklogsTab
		cmd := m.refreshCmd(refreshWorkspace)
		return m, cmd
	case pollTickMsg:
		if m.workspace == nil {
			return m, tickCmd()
		}
		return m, m.pollCmd()
	case pollResultMsg:
		cmds := []tea.Cmd{tickCmd()}
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
			return m, tea.Batch(cmds...)
		}
		if msg.activityChanged {
			cmds = append(cmds, m.startActivityLoad())
		}
		if msg.changed {
			if m.planOperation != nil {
				return m, tea.Batch(cmds...)
			}
			if m.presetForm != nil {
				m.presetForm.stale = true
				m.setNotice(noticeWarning, "presets changed externally; reload before saving")
				cmds = append(cmds, m.startPresetLoad())
			} else if m.form != nil {
				m.reloadAfterForm = true
				cmds = append(cmds, m.descriptionSuggestionsCmd())
				if !m.form.editing {
					m.issueSuggestionGen++
					cmds = append(cmds, m.issueSuggestionsCmd(m.issueSuggestionGen))
				}
				if m.form.editing {
					m.form.stale = true
					m.setNotice(noticeWarning, "worklogs changed externally; reload before saving")
				} else {
					m.setNotice(noticeInfo, "availability changed; recalculating placement")
					cmds = append(cmds, m.startWorklogLoad(), m.schedulePreview())
				}
			} else {
				cmds = append(cmds, m.refreshCmd(refreshWorkspace))
				if m.plan != nil && m.workspace.plans != nil {
					m.planGen++
					cmds = append(cmds, m.planLoadCmd(m.planGen, m.plan.ID))
				}
			}
		}
		return m, tea.Batch(cmds...)
	case trackerConsumedMsg:
		if msg.err != nil {
			m.setNotice(noticeError, msg.err.Error())
		}
		if msg.refresh != 0 {
			return m, m.refreshCmd(msg.refresh)
		}
		return m, nil
	case mutationResultMsg:
		return m.handleMutationResult(msg)
	case presetMutationResultMsg:
		return m.handlePresetMutationResult(msg)
	case activityListResultMsg:
		if msg.generation != m.activityGen {
			return m, nil
		}
		m.activityLoading = false
		if msg.err != nil {
			return m, nil
		}
		selectedID := m.selectedActivityID()
		wasNewest := m.activitySelected == 0
		m.activities = drawerActivities(msg.items)
		if wasNewest {
			m.activitySelected = 0
		} else {
			m.restoreActivitySelection(selectedID)
		}
		return m, nil
	case presetIssueSuggestionsResultMsg:
		if m.presetForm != nil && msg.err == nil {
			m.presetForm.inputs[1].SetSuggestions(msg.items)
		}
		return m, nil
	case presetDescriptionSuggestionsResultMsg:
		if m.presetForm != nil && msg.err == nil && strings.EqualFold(strings.TrimSpace(m.presetForm.inputs[1].Value()), msg.issue) {
			m.presetForm.inputs[4].SetSuggestions(msg.items)
		}
		return m, nil
	case previewReadyMsg:
		if m.form == nil || m.form.editing || msg.generation != m.form.previewGeneration {
			return m, nil
		}
		return m, m.previewCmd(msg.generation)
	case previewResultMsg:
		if m.form == nil || msg.generation != m.form.previewGeneration {
			return m, nil
		}
		m.form.previewLoading = false
		m.form.previewRecords = nil
		m.form.previewError = ""
		m.form.previewConflict = ""
		m.form.previewOvertimeAvailable = false
		if msg.err != nil {
			if m.form.placement == manualPlacement {
				m.form.previewError = msg.err.Error()
				if record, reason, ok := manualConflictPreview(msg.err); ok {
					m.form.previewRecords = []worklogs.LocalWorklog{record}
					m.form.previewConflict = reason
				}
			} else {
				m.form.previewOvertimeAvailable = overtimePlacementAvailable(msg.err)
				m.form.previewError = automaticPlacementMessage(msg.err)
			}
		} else {
			m.form.previewRecords = msg.records
		}
		if len(msg.metadata) > 0 {
			if m.issueMetadata == nil {
				m.issueMetadata = make(map[string]worklogs.IssueMetadata)
			}
			for key, metadata := range msg.metadata {
				m.issueMetadata[key] = metadata
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		return m.updateMouse(msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		switch m.interactionState.kind() {
		case interactionPresetPicker:
			return m.updatePresetPicker(msg)
		case interactionPresetForm:
			return m.updatePresetForm(msg)
		case interactionWorklogForm:
			return m.updateForm(msg)
		case interactionPlanForm:
			return m.updatePlanForm(msg)
		}
		return m, nil
	}
	keyName := key.String()
	if keyName == "ctrl+c" {
		return m, tea.Interrupt
	}
	if m.width < minimumWidth || m.height < minimumHeight {
		if keyName == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	switch m.interactionState.kind() {
	case interactionHelp, interactionConfirmation:
		return m.updateOverlay(keyName)
	case interactionPresetPicker:
		return m.updatePresetPicker(msg)
	case interactionPresetForm:
		return m.updatePresetForm(msg)
	case interactionPlanForm:
		return m.updatePlanForm(msg)
	case interactionPlanOperation:
		return m.updatePlans(keyName)
	case interactionWorklogForm:
		return m.updateForm(msg)
	}
	if keyName == "g" {
		m.activityOpen = !m.activityOpen
		m.focus = focusWorkspace
		if m.activityOpen {
			m.focus = focusActivity
		}
		if m.activityOpen {
			return m, m.startActivityLoad()
		}
		return m, nil
	}
	if keyName == "1" {
		m.activateTab(statusTab)
		return m, nil
	}
	if keyName == "2" && m.workspace != nil {
		m.activateTab(worklogsTab)
		return m, nil
	}
	if keyName == "3" && m.workspace != nil {
		m.activateTab(plansTab)
		return m, nil
	}
	if keyName == "4" && m.workspace != nil {
		m.activateTab(presetsTab)
		return m, nil
	}
	if keyName == "5" && m.workspace != nil {
		m.activateTab(trashTab)
		return m, nil
	}
	if m.focus == focusActivity {
		return m.updateActivity(keyName)
	}
	if keyName == "q" {
		return m, tea.Quit
	}
	if keyName == "?" {
		m.overlay = helpOverlay
		return m, nil
	}
	if keyName == "tab" || keyName == "shift+tab" {
		if m.activityOpen {
			if keyName == "tab" {
				switch m.focus {
				case focusRail:
					m.focus = focusWorkspace
				case focusWorkspace:
					m.focus = focusActivity
				default:
					m.focus = focusRail
				}
			} else {
				switch m.focus {
				case focusRail:
					m.focus = focusActivity
				case focusActivity:
					m.focus = focusWorkspace
				default:
					m.focus = focusRail
				}
			}
		} else {
			if m.focus == focusRail {
				m.focus = focusWorkspace
			} else {
				m.focus = focusRail
			}
		}
		return m, nil
	}
	if m.focus == focusRail {
		switch keyName {
		case "up", "k":
			m.activeTab = max(statusTab, m.activeTab-1)
		case "down", "j":
			if m.workspace != nil {
				m.activeTab = min(trashTab, m.activeTab+1)
			}
		case "enter", " ":
			m.focus = focusWorkspace
		}
		return m, nil
	}
	if m.activeTab == statusTab {
		return m.updateStatus(keyName)
	}
	if m.activeTab == trashTab {
		return m.updateTrash(keyName)
	}
	if m.activeTab == presetsTab {
		return m.updatePresets(keyName)
	}
	if m.activeTab == plansTab {
		return m.updatePlans(keyName)
	}
	return m.updateWorklogs(keyName)
}

func (m *model) activateTab(destination tab) {
	m.activeTab = destination
	m.focus = focusWorkspace
}

func (m model) updateMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.width < minimumWidth || m.height < minimumHeight || m.interactionState.kind() != interactionIdle {
		return m, nil
	}

	layout := calculateLayout(m.width, m.height, m.activityVisible())
	_, railCards := m.renderRailLayout(layout.rail.height)
	if layout.activity.contains(msg.X, msg.Y) {
		m.focus = focusActivity
		row := msg.Y - layout.activity.y - 2
		if row >= 0 {
			start, end := visibleRange(m.activitySelected, len(m.activities), activityDrawerListRows)
			if index := start + row; index < end {
				m.activitySelected = index
			}
		}
		return m, nil
	}
	if !layout.body.contains(msg.X, msg.Y) {
		return m, nil
	}
	if layout.rail.contains(msg.X, msg.Y) {
		switch {
		case railCards[0].contains(msg.X, msg.Y):
			m.activateTab(statusTab)
		case railCards[1].contains(msg.X, msg.Y) && m.workspace != nil:
			m.activateTab(worklogsTab)
		case railCards[2].contains(msg.X, msg.Y) && m.workspace != nil:
			m.activateTab(plansTab)
		case railCards[3].contains(msg.X, msg.Y) && m.workspace != nil:
			m.activateTab(presetsTab)
		case railCards[4].contains(msg.X, msg.Y) && m.workspace != nil:
			m.activateTab(trashTab)
		}
		return m, nil
	}
	if layout.workspace.contains(msg.X, msg.Y) {
		m.focus = focusWorkspace
	}
	return m, nil
}

func (m *model) startStatusRefresh() tea.Cmd {
	if m.statusCancel != nil {
		m.statusCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.statusCtx = ctx
	m.statusCancel = cancel
	m.statusGen++
	m.statusLoading = true
	return m.statusCmd(m.statusGen)
}

func (m model) statusCmd(generation int) tea.Cmd {
	ctx := m.statusCtx
	return func() tea.Msg {
		report, err := m.deps.status.Check(ctx)
		return statusResultMsg{generation: generation, report: report, err: err, checkedAt: m.deps.now()}
	}
}

func (m *model) startWorklogLoad() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	m.clearWeekOutsideSelection()
	m.worklogGen++
	m.worklogLoading = true
	return m.worklogCmd(m.worklogGen, m.selectedDate)
}

func (m *model) startIssueTotalLoad() tea.Cmd {
	m.issueTotalGen++
	m.issueTotal = 0
	m.issueTotalErr = ""
	selected := m.selectedWorklog()
	if m.workspace == nil || m.worklogView != dayWorklogView || selected == nil {
		m.issueTotalIssue = ""
		m.issueTotalLoading = false
		return nil
	}

	m.issueTotalIssue = selected.IssueKey
	m.issueTotalLoading = true
	return m.issueTotalCmd(m.issueTotalGen, selected.IssueKey)
}

func (m model) issueTotalCmd(generation int, issueKey string) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	return func() tea.Msg {
		total, err := ws.worklogs.ActiveIssueTotalSeconds(ctx, issueKey)
		return issueTotalResultMsg{generation: generation, issueKey: issueKey, totalSeconds: total, err: err}
	}
}

func (m *model) refreshCmd(scope refreshScope) tea.Cmd {
	cmds := make([]tea.Cmd, 0, 5)
	if scope&refreshWorklogs != 0 {
		cmds = append(cmds, m.startWorklogLoad())
	}
	if scope&refreshTrash != 0 {
		cmds = append(cmds, m.startTrashLoad())
	}
	if scope&refreshPresets != 0 {
		cmds = append(cmds, m.startPresetLoad())
	}
	if scope&refreshPlans != 0 {
		cmds = append(cmds, m.startPlanLoad())
	}
	if scope&refreshActivity != 0 {
		cmds = append(cmds, m.startActivityLoad())
	}
	return tea.Batch(cmds...)
}

func (m *model) startTrashLoad() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	m.trashGen++
	m.trashLoading = true
	return m.trashCmd(m.trashGen, m.selectedDate)
}

func (m model) trashCmd(generation int, date time.Time) tea.Cmd {
	ws := m.workspace
	weekStart, weekEnd := weekBounds(date)
	scope := m.trashScope
	return func() tea.Msg {
		items, _, err := ws.trash.ListTrash(ws.cfg, worklogs.TrashFilters{ListFilters: worklogs.ListFilters{From: weekStart.Format("2006-01-02"), To: weekEnd.Format("2006-01-02")}, StorageScope: scope})
		return trashResultMsg{generation: generation, date: date, items: items, err: err}
	}
}

func (m *model) clearWeekOutsideSelection() {
	if len(m.weekDays) == 0 {
		return
	}
	weekStart, weekEnd := weekBounds(m.selectedDate)
	firstDate, firstErr := time.ParseInLocation("2006-01-02", m.weekDays[0].Date, m.workspace.cfg.Location)
	lastDate, lastErr := time.ParseInLocation("2006-01-02", m.weekDays[len(m.weekDays)-1].Date, m.workspace.cfg.Location)
	if firstErr == nil && lastErr == nil && sameDay(firstDate, weekStart) && sameDay(lastDate, weekEnd) {
		return
	}
	m.weekDays = nil
	m.weekSummary = worklogs.ContextSummary{}
	m.worklogs = nil
	m.dayContext = worklogs.ContextDay{Date: m.selectedDate.Format("2006-01-02")}
	m.worklogSelected = 0
}

func (m model) worklogCmd(generation int, date time.Time) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	dateValue := date.Format("2006-01-02")
	weekStart, weekEnd := weekBounds(date)
	return func() tea.Msg {
		week, err := ws.worklogs.Context(ctx, ws.cfg, worklogs.ContextInput{
			From: weekStart.Format("2006-01-02"),
			To:   weekEnd.Format("2006-01-02"),
		})
		if err != nil {
			return worklogResultMsg{generation: generation, date: date, err: err}
		}
		selectedDay := worklogs.ContextDay{Date: dateValue}
		for _, day := range week.Days {
			if day.Date == dateValue {
				selectedDay = day
				break
			}
		}
		return worklogResultMsg{
			generation: generation,
			date:       date,
			items:      selectedDay.Worklogs,
			day:        selectedDay,
			settings:   week.Settings,
			metadata:   week.Metadata,
			week:       week.Summary,
			weekDays:   week.Days,
		}
	}
}

func (m *model) startWorkspaceOpen() tea.Cmd {
	m.workspaceGen++
	m.workspaceLoading = true
	return m.workspaceCmd(m.workspaceGen)
}

func (m model) workspaceCmd(generation int) tea.Cmd {
	return func() tea.Msg {
		ws, err := m.deps.openWorkspace(m.ctx)
		return workspaceResultMsg{generation: generation, workspace: ws, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(value time.Time) tea.Msg { return pollTickMsg(value) })
}

func (m model) pollCmd() tea.Cmd {
	return func() tea.Msg {
		changed, err := m.workspace.tracker.Poll(m.ctx)
		if err != nil {
			return pollResultMsg{err: err}
		}
		activityChanged, err := m.workspace.activityTracker.Poll(m.ctx)
		return pollResultMsg{changed: changed, activityChanged: activityChanged, err: err}
	}
}

func (m model) consumeTrackerCmd(refresh refreshScope) tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	return func() tea.Msg {
		_, err := m.workspace.tracker.Poll(m.ctx)
		return trackerConsumedMsg{err: err, refresh: refresh}
	}
}

func refreshForMutation(kind string) refreshScope {
	switch kind {
	case "restore", "restore-day", "delete", "delete-day":
		return refreshWorklogs | refreshTrash
	case "add", "edit":
		return refreshWorklogs
	default:
		return refreshWorkspace
	}
}

func (m model) submitFormCmd() tea.Cmd {
	form := *m.form
	ws := m.workspace
	checkWritable := m.deps.checkWritable
	ctx := m.ctx
	selectedDate := m.selectedDate
	return func() tea.Msg {
		if err := checkWritable(ws.sqlitePath, "tui worklog save"); err != nil {
			return mutationResultMsg{kind: "save", err: err}
		}
		issue := strings.TrimSpace(form.inputs[0].Value())
		started := normalizeFormStart(form.inputs[1].Value())
		duration := normalizeTUIDuration(form.inputs[2].Value())
		description := form.inputs[3].Value()
		if !form.editing {
			refresh := refreshScope(0)
			if form.sourcePresetID != "" {
				if err := ws.presets.CheckRevision(ctx, form.sourcePresetID, form.sourcePresetRevision); err != nil {
					return mutationResultMsg{kind: "preset-source", err: err}
				}
				refresh = refreshPresets
			}
			input := worklogs.AddInput{}
			if form.placement == manualPlacement {
				started = manualAddStart(selectedDate, ws.cfg.Location, form.inputs[startInput].Value())
				input = worklogs.AddInput{
					IssueKey: issue, Started: started, Duration: duration, Description: description, Force: form.force,
				}
			} else {
				input = automaticAddInput(m.selectedDate, form.placement, form.overtime, form.noLunch, issue, duration, description)
				input.ExpectedPlacement = placementExpectations(form.previewRecords)
			}
			result, err := ws.mutations.Add(ctx, ws.cfg, input)
			if err == nil && form.sourcePresetID != "" {
				_ = ws.presets.MarkUsed(ctx, form.sourcePresetID)
			}
			return mutationResultMsg{kind: "add", records: result.Records, refresh: refresh, err: err}
		}
		_, err := ws.mutations.Update(ctx, ws.cfg, form.id, worklogs.PatchInput{
			IssueKey: &issue, Started: &started, Duration: &duration, Description: &description,
			Force: form.force, ExpectedRevision: form.revision,
		})
		return mutationResultMsg{kind: "edit", err: err}
	}
}

func (m model) deleteCmd(item worklogs.LocalWorklog) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui worklog delete"); err != nil {
			return mutationResultMsg{kind: "delete", err: err}
		}
		_, err := ws.mutations.Delete(ctx, item.ID, item.Revision)
		return mutationResultMsg{kind: "delete", err: err}
	}
}

func (m model) deleteDayCmd(date time.Time, items []worklogs.LocalWorklog) tea.Cmd {
	ws := m.workspace
	checkWritable := m.deps.checkWritable
	ctx := m.ctx
	dateValue := date.Format("2006-01-02")
	expected := make([]worklogs.DeleteExpectation, 0, len(items))
	for _, item := range items {
		expected = append(expected, worklogs.DeleteExpectation{ID: item.ID, Revision: item.Revision})
	}
	return func() tea.Msg {
		if err := checkWritable(ws.sqlitePath, "tui worklog day delete"); err != nil {
			return mutationResultMsg{kind: "delete-day", err: err}
		}
		result, err := ws.mutations.DeleteBatchExpected(ctx, ws.cfg, worklogs.ListFilters{From: dateValue, To: dateValue}, expected)
		return mutationResultMsg{kind: "delete-day", deleted: len(result.Deleted), err: err}
	}
}

func (m model) restoreTrashCmd(item worklogs.TrashRecord) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui trash restore"); err != nil {
			return mutationResultMsg{kind: "restore", err: err}
		}
		_, err := ws.trash.RestoreTrash(ctx, ws.cfg, item.ID)
		return mutationResultMsg{kind: "restore", deleted: 1, err: err}
	}
}

func (m model) restoreTrashDayCmd(date time.Time, items []worklogs.TrashRecord) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	dateValue := date.Format("2006-01-02")
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui trash day restore"); err != nil {
			return mutationResultMsg{kind: "restore-day", err: err}
		}
		result, err := ws.trash.RestoreTrashBatchExpected(ctx, ws.cfg, worklogs.TrashFilters{ListFilters: worklogs.ListFilters{From: dateValue, To: dateValue}, StorageScope: worklogs.TrashScopeLocal}, ids)
		return mutationResultMsg{kind: "restore-day", deleted: len(result.Items), err: err}
	}
}

func (m *model) openAddForm() tea.Cmd {
	form := m.newForm(false, "", 0, "", "", "", "")
	if selected := m.selectedWorklog(); selected != nil {
		form.suggestedIssue = selected.IssueKey
	} else {
		form.suggestedIssue = m.lastAddedIssue
	}
	form.suggestedStart = suggestedManualStart(m.worklogs, m.selectedDate, m.workspace.cfg.Location)
	m.form = &form
	m.formError = ""
	m.resizeForm()
	m.issueSuggestionGen++
	m.descriptionSuggestionGen++
	return m.issueSuggestionsCmd(m.issueSuggestionGen)
}

func (m *model) openEditForm(item worklogs.LocalWorklog) {
	local := item.StartedAtUTC.In(m.workspace.cfg.Location)
	form := m.newForm(true, item.ID, item.Revision, item.IssueKey, local.Format("2006-01-02 15:04"), formatDurationInput(item.DurationSeconds), item.Description)
	m.form = &form
	m.formError = ""
	m.resizeForm()
	m.descriptionSuggestionGen++
}

func (m model) newForm(editing bool, id string, revision int64, issue, started, duration, description string) formState {
	values := []string{issue, started, duration, description}
	startPlaceholder := "HH:MM"
	if editing {
		startPlaceholder = "YYYY-MM-DD HH:MM"
	}
	placeholders := []string{"PROJ-123", startPlaceholder, "1h30m", "What was done"}
	var inputs [4]textinput.Model
	for index := range inputs {
		input := textinput.New()
		if m.deps.noColor {
			input.SetVirtualCursor(false)
			input.SetStyles(textinput.Styles{})
		} else {
			input.SetStyles(m.colors.textInputStyles())
		}
		input.Prompt = ""
		input.Placeholder = placeholders[index]
		input.SetValue(values[index])
		input.SetWidth(48)
		inputs[index] = input
	}
	inputs[0].CharLimit = 64
	inputs[0].ShowSuggestions = true
	inputs[1].CharLimit = 5
	if editing {
		inputs[1].CharLimit = 32
	}
	inputs[2].CharLimit = 24
	inputs[3].CharLimit = 240
	inputs[3].ShowSuggestions = true
	placement := fillPlacement
	if editing {
		placement = manualPlacement
	}
	form := formState{inputs: inputs, focus: issueField, editing: editing, id: id, revision: revision, placement: placement}
	form.inputs[issueInput].Focus()
	return form
}

func (m *model) restyleFormInputs() {
	if m.form == nil || m.deps.noColor {
		return
	}
	styles := m.colors.textInputStyles()
	for index := range m.form.inputs {
		m.form.inputs[index].SetStyles(styles)
	}
}

func (m model) issueSuggestionsCmd(generation int) tea.Cmd {
	ws := m.workspace
	ctx := m.ctx
	return func() tea.Msg {
		items, err := ws.worklogs.ListKnownIssueKeys(ctx, "", issueSuggestionLimit)
		return issueSuggestionsResultMsg{generation: generation, items: items, err: err}
	}
}

func (m *model) descriptionSuggestionsCmd() tea.Cmd {
	issueKey := strings.ToUpper(strings.TrimSpace(m.form.inputs[issueInput].Value()))
	m.descriptionSuggestionGen++
	m.form.inputs[descriptionInput].SetSuggestions(nil)
	m.form.suggestedDescription = ""
	if issueKey == "" {
		return nil
	}
	generation := m.descriptionSuggestionGen
	ws := m.workspace
	ctx := m.ctx
	return func() tea.Msg {
		items, err := ws.worklogs.ListRecentDescriptions(ctx, issueKey, descriptionSuggestionLimit)
		return descriptionSuggestionsResultMsg{generation: generation, issueKey: issueKey, items: items, err: err}
	}
}

func (m *model) invalidateDescriptionSuggestions() {
	m.descriptionSuggestionGen++
	m.form.inputs[descriptionInput].SetSuggestions(nil)
	m.form.suggestedDescription = ""
}

func (m *model) focusForm(field formField) {
	for i := range m.form.inputs {
		m.form.inputs[i].Blur()
	}
	m.form.focus = field
	if index, ok := inputIndexForField(field); ok {
		m.form.inputs[index].Focus()
	}
}

func (m *model) moveFormFocus(delta int) tea.Cmd {
	fields := m.formFields()
	current := 0
	for index, field := range fields {
		if field == m.form.focus {
			current = index
			break
		}
	}
	currentField := fields[current]
	if currentField == durationField {
		m.normalizeDurationInput()
	}
	next := (current + delta + len(fields)) % len(fields)
	m.focusForm(fields[next])
	if currentField == issueField {
		return m.descriptionSuggestionsCmd()
	}
	return nil
}

func (m *model) normalizeDurationInput() {
	input := m.form.inputs[durationInput]
	input.SetValue(normalizeTUIDuration(input.Value()))
	m.form.inputs[durationInput] = input
}

func normalizeTUIDuration(value string) string {
	trimmed := strings.TrimSpace(value)
	compact := strings.Join(strings.Fields(trimmed), "")
	if hoursText, minutesText, found := strings.Cut(compact, ":"); found &&
		strings.Count(compact, ":") == 1 && decimalDigits(hoursText) && decimalDigits(minutesText) {
		hours, hoursErr := strconv.Atoi(hoursText)
		minutes, minutesErr := strconv.Atoi(minutesText)
		if hoursErr == nil && minutesErr == nil && minutes < 60 {
			switch {
			case hours == 0:
				return fmt.Sprintf("%dm", minutes)
			case minutes == 0:
				return fmt.Sprintf("%dh", hours)
			default:
				return fmt.Sprintf("%dh%dm", hours, minutes)
			}
		}
	}
	if _, err := time.ParseDuration(compact); err == nil {
		return compact
	}
	return trimmed
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func suggestedManualStart(items []worklogs.LocalWorklog, selectedDate time.Time, location *time.Location) string {
	if location == nil {
		location = selectedDate.Location()
	}
	var latestEnd time.Time
	for _, item := range items {
		if item.DurationSeconds <= 0 || !sameDay(item.StartedAtUTC.In(location), selectedDate.In(location)) {
			continue
		}
		end := item.StartedAtUTC.Add(time.Duration(item.DurationSeconds) * time.Second)
		if latestEnd.IsZero() || end.After(latestEnd) {
			latestEnd = end
		}
	}
	if latestEnd.IsZero() {
		return ""
	}
	if truncated := latestEnd.Truncate(time.Minute); !latestEnd.Equal(truncated) {
		latestEnd = truncated.Add(time.Minute)
	}
	localEnd := latestEnd.In(location)
	if !sameDay(localEnd, selectedDate.In(location)) || ambiguousLocalMinute(latestEnd, location) {
		return ""
	}
	return localEnd.Format("15:04")
}

func ambiguousLocalMinute(value time.Time, location *time.Location) bool {
	local := value.In(location)
	matches := 0
	for offset := -4 * time.Hour; offset <= 4*time.Hour; offset += time.Minute {
		candidate := value.Add(offset).In(location)
		if candidate.Year() == local.Year() && candidate.Month() == local.Month() && candidate.Day() == local.Day() &&
			candidate.Hour() == local.Hour() && candidate.Minute() == local.Minute() {
			matches++
			if matches > 1 {
				return true
			}
		}
	}
	return false
}

func (m model) formFields() []formField {
	if m.form.editing {
		return []formField{issueField, startField, durationField, descriptionField}
	}
	if m.form.placement == manualPlacement {
		return []formField{issueField, startField, durationField, descriptionField}
	}
	return []formField{issueField, durationField, descriptionField}
}

func inputIndexForField(field formField) (int, bool) {
	switch field {
	case issueField:
		return issueInput, true
	case startField:
		return startInput, true
	case durationField:
		return durationInput, true
	case descriptionField:
		return descriptionInput, true
	default:
		return 0, false
	}
}

func (m model) changePlacement(delta int) (tea.Model, tea.Cmd) {
	placements := []placementMode{manualPlacement, fitPlacement, fillPlacement}
	index := 0
	for candidate, placement := range placements {
		if placement == m.form.placement {
			index = candidate
			break
		}
	}
	m.form.placement = placements[(index+delta+len(placements))%len(placements)]
	m.formError = ""
	return m, m.schedulePreview()
}

func (m *model) resizeForm() {
	if m.form == nil {
		return
	}
	width := max(20, m.width-30-12)
	for index := range m.form.inputs {
		m.form.inputs[index].SetWidth(width)
	}
}

func (m *model) invalidatePreview() {
	m.form.previewGeneration++
	m.form.previewLoading = false
	m.form.previewRecords = nil
	m.form.previewError = ""
	m.form.previewConflict = ""
	m.form.previewOvertimeAvailable = false
}

func (m *model) schedulePreview() tea.Cmd {
	if m.form == nil {
		return nil
	}
	m.invalidatePreview()
	if m.form.editing {
		return nil
	}
	if strings.TrimSpace(m.form.inputs[issueInput].Value()) == "" || strings.TrimSpace(m.form.inputs[durationInput].Value()) == "" ||
		(m.form.placement == manualPlacement && strings.TrimSpace(m.form.inputs[startInput].Value()) == "") {
		return nil
	}
	m.form.previewLoading = true
	generation := m.form.previewGeneration
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return previewReadyMsg{generation: generation}
	})
}

func (m model) previewCmd(generation int) tea.Cmd {
	form := *m.form
	ws := m.workspace
	ctx := m.ctx
	issue := strings.TrimSpace(form.inputs[issueInput].Value())
	duration := normalizeTUIDuration(form.inputs[durationInput].Value())
	input := automaticAddInput(m.selectedDate, form.placement, form.overtime, form.noLunch, issue, duration, "placement preview")
	if form.placement == manualPlacement {
		input = worklogs.AddInput{
			IssueKey:    issue,
			Started:     manualAddStart(m.selectedDate, ws.cfg.Location, form.inputs[startInput].Value()),
			Duration:    duration,
			Description: "placement preview",
		}
	}
	return func() tea.Msg {
		result, err := ws.worklogs.PreviewAdd(ctx, ws.cfg, input)
		metadata, metadataErr := ws.worklogs.LookupIssueMetadata(ctx, []string{input.IssueKey})
		if metadataErr != nil {
			metadata = nil
		}
		return previewResultMsg{generation: generation, records: result.Records, metadata: metadata, err: err}
	}
}

func (m model) previewInputField(field formField) bool {
	if field == issueField || field == durationField {
		return true
	}
	return m.form.placement == manualPlacement && field == startField
}

func manualConflictPreview(err error) (worklogs.LocalWorklog, string, bool) {
	var validation worklogs.ValidationError
	if !errors.As(err, &validation) || validation.Conflict == nil {
		return worklogs.LocalWorklog{}, "", false
	}
	attempted := validation.Conflict.Attempted
	startedAt, parseErr := time.Parse(time.RFC3339, attempted.StartedAtUTC)
	if parseErr != nil || attempted.DurationSeconds <= 0 {
		return worklogs.LocalWorklog{}, "", false
	}
	reason := validation.Conflict.Reason
	if reason == "" {
		reason = "conflict"
	}
	return worklogs.LocalWorklog{
		IssueKey:        attempted.IssueKey,
		StartedAtUTC:    startedAt,
		DurationSeconds: attempted.DurationSeconds,
		Description:     attempted.Description,
	}, reason, true
}

func automaticAddInput(date time.Time, placement placementMode, overtime, noLunch bool, issue, duration, description string) worklogs.AddInput {
	dateValue := date.Format("2006-01-02")
	return worklogs.AddInput{
		IssueKey:    issue,
		Fit:         placement == fitPlacement,
		Fill:        placement == fillPlacement,
		Overtime:    overtime,
		NoLunch:     noLunch,
		From:        dateValue,
		To:          dateValue,
		Duration:    duration,
		Description: description,
	}
}

func placementExpectations(records []worklogs.LocalWorklog) []worklogs.PlacementExpectation {
	expected := make([]worklogs.PlacementExpectation, 0, len(records))
	for _, record := range records {
		expected = append(expected, worklogs.PlacementExpectation{
			StartedAtUTC:    record.StartedAtUTC,
			DurationSeconds: record.DurationSeconds,
		})
	}
	return expected
}

func overtimePlacementAvailable(err error) bool {
	return strings.Contains(err.Error(), "use --overtime")
}

func automaticPlacementMessage(err error) string {
	if overtimePlacementAvailable(err) {
		return "No regular slot is available; overtime placement can succeed."
	}
	if strings.Contains(err.Error(), "no free slot available") {
		return "No free slot is available on the selected day."
	}
	return err.Error()
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (m model) selectedWorklog() *worklogs.LocalWorklog {
	if m.worklogSelected < 0 || m.worklogSelected >= len(m.worklogs) {
		return nil
	}
	return &m.worklogs[m.worklogSelected]
}

func (m model) selectedWorklogID() string {
	if selected := m.selectedWorklog(); selected != nil {
		return selected.ID
	}
	return ""
}

func (m *model) restoreWorklogSelection(id string) {
	if id != "" {
		for index := range m.worklogs {
			if m.worklogs[index].ID == id {
				m.worklogSelected = index
				return
			}
		}
	}
	m.worklogSelected = clampIndex(m.worklogSelected, len(m.worklogs))
}

func (m model) selectedTrash() *worklogs.TrashRecord {
	if m.trashSelected < 0 || m.trashSelected >= len(m.trashItems) {
		return nil
	}
	return &m.trashItems[m.trashSelected]
}

func (m model) selectedTrashID() string {
	if item := m.selectedTrash(); item != nil {
		return item.ID
	}
	return ""
}

func (m *model) restoreTrashSelection(id string) {
	for index := range m.trashItems {
		if m.trashItems[index].ID == id {
			m.trashSelected = index
			return
		}
	}
	m.trashSelected = clampIndex(m.trashSelected, len(m.trashItems))
}

func trashForDate(items []worklogs.TrashRecord, date time.Time, location *time.Location) []worklogs.TrashRecord {
	result := make([]worklogs.TrashRecord, 0)
	for _, item := range items {
		if sameDay(item.StartedAtUTC.In(location), date) {
			result = append(result, item)
		}
	}
	return result
}

func restorableLocalTrash(items []worklogs.TrashRecord) []worklogs.TrashRecord {
	result := make([]worklogs.TrashRecord, 0, len(items))
	for _, item := range items {
		if item.StorageScope == worklogs.TrashScopeLocal && item.SourceWorklogID != nil {
			result = append(result, item)
		}
	}
	return result
}

func (m model) close() {
	if m.statusCancel != nil {
		m.statusCancel()
	}
	if m.planOperation != nil && m.planOperation.cancel != nil {
		m.planOperation.cancel()
	}
	if m.workspace != nil && m.workspace.close != nil {
		_ = m.workspace.close()
	}
}

func normalizeFormStart(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 10 && value[10] == ' ' {
		return value[:10] + "T" + value[11:]
	}
	return value
}

func manualAddStart(selectedDate time.Time, location *time.Location, value string) string {
	if location == nil {
		location = selectedDate.Location()
	}
	return selectedDate.In(location).Format("2006-01-02") + "T" + strings.TrimSpace(value)
}

func formatDurationInput(seconds int) string {
	duration := time.Duration(seconds) * time.Second
	hours := int(duration / time.Hour)
	minutes := int(duration%time.Hour) / int(time.Minute)
	if hours > 0 && minutes > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}

func beginningOfDay(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func weekBounds(date time.Time) (time.Time, time.Time) {
	day := beginningOfDay(date, date.Location())
	daysSinceMonday := (int(day.Weekday()) + 6) % 7
	start := day.AddDate(0, 0, -daysSinceMonday)
	return start, start.AddDate(0, 0, 6)
}

func sameDay(left, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

func clampIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return min(max(index, 0), length-1)
}
