package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

type issuesScreen struct {
	issues          []worklogs.IssueMetadata
	issueSelected   int
	issuesLoading   bool
	issuesErr       string
	issuesGen       int
	issueSearch     *issueSearchState
	issueFormError  string
	issueSelectKey  string
	issueView       issueView
	issueEpic       *string
	epicsSelected   string
	epicSelected    bool
	epicSearch      *issueSearchState
	issueSort       issueSortMode
	epicSort        issueSortMode
	sortSelected    int
	issueMarks      map[string]bool
	issueRange      *issueRangeState
	issueHistoryGen int
}

type issueView int

const (
	issueListView issueView = iota
	epicListView
)

type issueFormState struct {
	inputs  [4]textinput.Model
	focus   int
	editing bool
	stale   bool
	saving  bool
}

type issueSearchState struct {
	input       textinput.Model
	editing     bool
	previousKey string
}

type issueListResultMsg struct {
	generation int
	items      []worklogs.IssueMetadata
	err        error
}

type issueMutationResultMsg struct {
	issueKey string
	err      error
}

func (msg issueListResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}

func (msg issueMutationResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}

func (m *model) startIssueLoad() tea.Cmd {
	if m.workspace == nil || m.workspace.issues == nil {
		return nil
	}
	m.issuesGen++
	m.issuesLoading = true
	if m.issuesErr != "" && m.notice == m.issuesErr {
		m.clearNotice()
	}
	m.issuesErr = ""
	return m.issueListCmd(m.issuesGen)
}

func (m model) issueListCmd(generation int) tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	return func() tea.Msg {
		if ws == nil || ws.issues == nil {
			return issueListResultMsg{generation: generation, items: []worklogs.IssueMetadata{}}
		}
		items, err := ws.issues.SearchIssues(ctx, "", "", "")
		return issueListResultMsg{generation: generation, items: items, err: err}
	}
}

func (m model) handleIssueListResult(msg issueListResultMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.issuesGen {
		return m, nil
	}
	m.issuesLoading = false
	if msg.err != nil {
		m.issuesErr = msg.err.Error()
		m.setNotice(noticeError, msg.err.Error())
		return m, nil
	}
	m.issuesErr = ""
	key := m.selectedIssueKey()
	if m.issueSelectKey != "" {
		key = m.issueSelectKey
		m.issueSelectKey = ""
	}
	m.issues = msg.items
	m.issueRange = nil
	m.pruneIssueMarks()
	m.restoreIssueSelection(key)
	m.restoreEpicSelection(m.epicsSelected, m.epicSelected)
	m.refreshIssueHistoryMetadata()
	return m, nil
}

func (m model) visibleIssues() []int {
	indices := make([]int, 0, len(m.issues))
	query := ""
	if m.issueSearch != nil {
		query = strings.ToLower(strings.TrimSpace(m.issueSearch.input.Value()))
	}
	for index, issue := range m.issues {
		if m.issueEpic != nil && issue.Epic != *m.issueEpic {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(issue.IssueKey+" "+issue.Title+" "+issue.Epic), query) {
			indices = append(indices, index)
		}
	}
	slices.SortFunc(indices, func(a, b int) int {
		return compareIssues(m.issues[a], m.issues[b], m.issueSort)
	})
	return indices
}

func (m model) selectedIssue() *worklogs.IssueMetadata {
	for _, index := range m.visibleIssues() {
		if index == m.issueSelected {
			return &m.issues[index]
		}
	}
	return nil
}

func (m model) selectedIssueKey() string {
	if issue := m.selectedIssue(); issue != nil {
		return issue.IssueKey
	}
	return ""
}

func (m *model) restoreIssueSelection(key string) {
	indices := m.visibleIssues()
	if len(indices) == 0 {
		m.issueSelected = -1
		return
	}
	for _, index := range indices {
		if m.issues[index].IssueKey == key {
			m.issueSelected = index
			return
		}
	}
	for _, index := range indices {
		if index == m.issueSelected {
			return
		}
	}
	m.issueSelected = indices[0]
}

func (m *model) moveIssueSelection(delta int) {
	indices := m.visibleIssues()
	for position, index := range indices {
		if index == m.issueSelected {
			m.issueSelected = indices[min(max(0, position+delta), len(indices)-1)]
			return
		}
	}
	m.restoreIssueSelection("")
}

func (m model) updateIssues(key string) (tea.Model, tea.Cmd) {
	if key != "up" && key != "k" && key != "down" && key != "j" && key != "pgup" && key != "pgdown" && key != "home" && key != "end" && key != "v" && key != "V" && key != "esc" {
		m.issueRange = nil
	}
	if key == "t" {
		if m.issueView == epicListView {
			m.issueView = issueListView
			m.restoreIssueSelection(m.selectedIssueKey())
		} else {
			m.issueView = epicListView
			if m.issueEpic != nil {
				m.restoreEpicSelection(*m.issueEpic, true)
			} else {
				m.restoreEpicSelection(m.epicsSelected, m.epicSelected)
			}
		}
		return m, nil
	}
	if m.issueView == epicListView {
		return m.updateEpics(key)
	}
	switch key {
	case "v", "V":
		m.toggleIssueRange(key == "V")
	case "ctrl+a":
		m.markVisibleIssues()
	case "space", " ":
		m.toggleIssueMark()
	case "b":
		return m.openEpicAssignment()
	case "up", "k":
		m.moveIssueSelection(-1)
		m.updateIssueRange()
	case "down", "j":
		m.moveIssueSelection(1)
		m.updateIssueRange()
	case "pgup", "pgdown", "home", "end":
		m.navigateIssueList(key)
		m.updateIssueRange()
	case "s":
		m.openIssueSort()
	case "a":
		if m.issuesLoading || m.issuesErr != "" {
			return m, nil
		}
		cmd := m.openIssueForm(nil)
		return m, cmd
	case "e":
		if m.issuesLoading || m.issuesErr != "" {
			return m, nil
		}
		if item := m.selectedIssue(); item != nil {
			copy := *item
			cmd := m.openIssueForm(&copy)
			return m, cmd
		}
	case "enter":
		if m.selectedIssue() != nil {
			return m.openIssueHistory()
		}
	case "/":
		previous := m.selectedIssueKey()
		input := textinput.New()
		input.Prompt, input.Placeholder = "", "Issue key, title or epic"
		if m.issueSearch != nil {
			input.SetValue(m.issueSearch.input.Value())
			previous = m.issueSearch.previousKey
		}
		m.issueSearch = &issueSearchState{input: input, editing: true, previousKey: previous}
		m.restyleIssueInputs()
		m.resizeIssueInputs()
		return m, m.issueSearch.input.Focus()
	case "esc":
		if m.issueRange != nil {
			m.issueRange = nil
		} else if len(m.issueMarks) > 0 {
			m.issueMarks = nil
		} else if m.issueSearch != nil {
			previous := m.issueSearch.previousKey
			m.issueSearch = nil
			m.restoreIssueSelection(previous)
		} else if m.issueEpic != nil {
			epic := *m.issueEpic
			m.issueEpic = nil
			m.issueView = epicListView
			m.restoreEpicSelection(epic, true)
		}
	case "r":
		cmd := m.startIssueLoad()
		cmd = m.beginActivity("issues.list", "", cmd)
		return m, cmd
	}
	return m, nil
}

func (m model) updateIssueSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			previous := m.issueSearch.previousKey
			m.issueSearch = nil
			m.restoreIssueSelection(previous)
			return m, nil
		case "enter":
			m.issueSearch.editing = false
			m.issueSearch.input.Blur()
			return m, nil
		case "up":
			m.moveIssueSelection(-1)
			return m, nil
		case "down":
			m.moveIssueSelection(1)
			return m, nil
		case "pgup", "pgdown":
			m.navigateIssueList(key.String())
			return m, nil
		}
	}
	key := m.selectedIssueKey()
	input, cmd := m.issueSearch.input.Update(msg)
	m.issueSearch.input = input
	m.restoreIssueSelection(key)
	return m, cmd
}

func (m *model) openIssueForm(item *worklogs.IssueMetadata) tea.Cmd {
	state := issueFormState{}
	values := []string{"", "", "", ""}
	if item != nil {
		values[0], values[1], values[3] = item.IssueKey, item.Title, item.Epic
		if item.ManualMaxEstimateSeconds != nil {
			values[2] = formatEstimateDuration(*item.ManualMaxEstimateSeconds)
		}
		state.editing, state.focus = true, 1
	} else if m.issueEpic != nil {
		values[3] = *m.issueEpic
	}
	placeholders := []string{"SBAH-2", "Optional title", "Optional, e.g. 16h", "Optional local group"}
	for index := range state.inputs {
		input := textinput.New()
		input.Prompt, input.Placeholder = "", placeholders[index]
		input.SetValue(values[index])
		state.inputs[index] = input
	}
	m.issueForm = &state
	m.issueFormError = ""
	m.restyleIssueInputs()
	m.resizeIssueInputs()
	return m.issueForm.inputs[state.focus].Focus()
}

func (m *model) resizeIssueInputs() {
	if m.epicAssignment != nil {
		m.epicAssignment.input.SetWidth(max(10, min(72, m.width-navigationWidth-7)-10))
	}
	width := max(20, m.width-navigationWidth-18)
	if m.issueForm != nil {
		for index := range m.issueForm.inputs {
			m.issueForm.inputs[index].SetWidth(width)
		}
	}
	if m.issueSearch != nil {
		list, _ := inspectorWidths(m.width - navigationWidth - 1)
		m.issueSearch.input.SetWidth(max(10, list-9))
	}
	if m.epicSearch != nil {
		list, _ := inspectorWidths(m.width - navigationWidth - 1)
		m.epicSearch.input.SetWidth(max(10, list-9))
	}
}

func (m *model) restyleIssueInputs() {
	styles := m.colors.textInputStyles()
	if m.deps.noColor {
		styles = textinput.Styles{}
	}
	if m.epicAssignment != nil {
		m.epicAssignment.input.SetStyles(styles)
		if m.deps.noColor {
			m.epicAssignment.input.SetVirtualCursor(false)
		}
	}
	if m.issueForm != nil {
		for index := range m.issueForm.inputs {
			m.issueForm.inputs[index].SetStyles(styles)
			if m.deps.noColor {
				m.issueForm.inputs[index].SetVirtualCursor(false)
			}
		}
	}
	if m.issueSearch != nil {
		m.issueSearch.input.SetStyles(styles)
		if m.deps.noColor {
			m.issueSearch.input.SetVirtualCursor(false)
		}
	}
	if m.epicSearch != nil {
		m.epicSearch.input.SetStyles(styles)
		if m.deps.noColor {
			m.epicSearch.input.SetVirtualCursor(false)
		}
	}
}

func (m model) updateIssueForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.issueForm.saving {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			stale := m.issueForm.stale
			m.issueForm = nil
			m.issueFormError = ""
			if stale {
				cmd := m.refreshCmd(refreshWorklogs)
				return m, cmd
			}
			return m, nil
		case "ctrl+s":
			return m.beginIssueFormSubmit()
		case "enter":
			if m.issueForm.focus == 3 {
				return m.beginIssueFormSubmit()
			}
			fallthrough
		case "tab", "down":
			return m.moveIssueFormFocus(1)
		case "shift+tab", "up":
			return m.moveIssueFormFocus(-1)
		}
	}
	index := m.issueForm.focus
	input, cmd := m.issueForm.inputs[index].Update(msg)
	m.issueForm.inputs[index] = input
	m.issueFormError = ""
	return m, cmd
}

func (m model) moveIssueFormFocus(delta int) (tea.Model, tea.Cmd) {
	start := 0
	if m.issueForm.editing {
		start = 1
	}
	for index := range m.issueForm.inputs {
		m.issueForm.inputs[index].Blur()
	}
	m.issueForm.focus = start + (m.issueForm.focus-start+delta+4-start)%(4-start)
	return m, m.issueForm.inputs[m.issueForm.focus].Focus()
}

func (m model) beginIssueFormSubmit() (tea.Model, tea.Cmd) {
	if m.issueForm.stale {
		m.issueFormError = "issues changed externally; cancel and reopen before saving"
		return m, nil
	}
	patch, err := m.issueFormPatch()
	if err != nil {
		m.issueFormError = err.Error()
		return m, nil
	}
	m.issueForm.saving = true
	ws, ctx := m.workspace, m.ctx
	cmd := func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui issue save"); err != nil {
			return issueMutationResultMsg{issueKey: patch.IssueKey, err: err}
		}
		err := ws.issues.ApplyIssueMetadata(ctx, []worklogs.IssueMetadataPatch{patch}, false)
		return issueMutationResultMsg{issueKey: patch.IssueKey, err: err}
	}
	cmd = m.beginActivity("issues.set", "issue="+patch.IssueKey, cmd)
	return m, cmd
}

func (m model) issueFormPatch() (worklogs.IssueMetadataPatch, error) {
	values := make([]string, len(m.issueForm.inputs))
	for index := range m.issueForm.inputs {
		values[index] = strings.TrimSpace(m.issueForm.inputs[index].Value())
	}
	patch := worklogs.IssueMetadataPatch{IssueKey: strings.ToUpper(values[0]), TitleSet: m.issueForm.editing, MaxEstimateSet: m.issueForm.editing, EpicSet: m.issueForm.editing}
	if !m.issueForm.editing {
		for _, issue := range m.issues {
			if issue.IssueKey == patch.IssueKey {
				return patch, fmt.Errorf("issue %s already exists; edit its metadata", patch.IssueKey)
			}
		}
	}
	if values[1] != "" {
		patch.Title = &values[1]
		patch.TitleSet = true
	}
	if values[2] != "" {
		estimate, err := worklogs.ParseMaxEstimate(normalizeTUIDuration(values[2]))
		if err != nil {
			return patch, err
		}
		patch.MaxEstimateSeconds = &estimate
		patch.MaxEstimateSet = true
	}
	if values[3] != "" {
		patch.Epic = &values[3]
		patch.EpicSet = true
	}
	return patch, worklogs.ValidateIssueMetadataPatches([]worklogs.IssueMetadataPatch{patch})
}

func (m model) handleIssueMutationResult(msg issueMutationResultMsg) (tea.Model, tea.Cmd) {
	if m.issueForm != nil {
		m.issueForm.saving = false
	}
	if msg.err != nil {
		m.issueFormError = msg.err.Error()
		m.setNotice(noticeError, msg.err.Error())
		return m, nil
	}
	m.issueForm = nil
	m.issueFormError = ""
	m.issueSelectKey = msg.issueKey
	m.setNotice(noticeSuccess, "saved issue "+msg.issueKey)
	return m, m.consumeTrackerCmd(refreshIssues | refreshWorklogs)
}
