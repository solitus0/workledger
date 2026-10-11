package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type actionsState struct {
	input    textinput.Model
	selected int
}

type worklogFilterState struct {
	input      textinput.Model
	editing    bool
	previousID string
}

func (m model) searchInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = 128
	if m.deps.noColor {
		input.SetVirtualCursor(false)
		input.SetStyles(textinput.Styles{})
	} else {
		input.SetStyles(m.colors.textInputStyles())
	}
	return input
}

func (m *model) restyleSearchInputs() {
	for _, input := range m.searchInputs() {
		if !m.deps.noColor {
			input.SetStyles(m.colors.textInputStyles())
		}
	}
}

func (m *model) resizeSearchInputs() {
	if m.actions != nil {
		m.actions.input.SetWidth(min(64, max(20, m.width-navigationWidth-12)))
	}
	if m.worklogFilter != nil {
		width, _ := inspectorWidths(calculateLayout(m.width, m.height, false).workspace.width)
		m.worklogFilter.input.SetWidth(max(12, width-9))
	}
}

func (m *model) searchInputs() []*textinput.Model {
	var inputs []*textinput.Model
	if m.actions != nil {
		inputs = append(inputs, &m.actions.input)
	}
	if m.worklogFilter != nil {
		inputs = append(inputs, &m.worklogFilter.input)
	}
	return inputs
}

func (m *model) openActions() tea.Cmd {
	m.actions = &actionsState{input: m.searchInput("Find an action…")}
	m.overlay = helpOverlay
	m.resizeSearchInputs()
	return m.actions.input.Focus()
}

func (m model) matchingActions() []commandAction {
	query := ""
	if m.actions != nil {
		query = strings.ToLower(strings.TrimSpace(m.actions.input.Value()))
	}
	var matches []commandAction
	for _, action := range m.commandActions() {
		if strings.Contains(strings.ToLower(action.label+" "+action.chord), query) {
			matches = append(matches, action)
		}
	}
	return matches
}

func (m model) updateActions(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.actions == nil {
		return m, m.openActions()
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.overlay, m.actions = noOverlay, nil
			return m, nil
		case "up":
			m.actions.selected = max(0, m.actions.selected-1)
			return m, nil
		case "down":
			m.actions.selected = min(max(0, len(m.matchingActions())-1), m.actions.selected+1)
			return m, nil
		case "enter":
			matches := m.matchingActions()
			if len(matches) == 0 {
				return m, nil
			}
			action := matches[clampIndex(m.actions.selected, len(matches))]
			m.overlay, m.actions = noOverlay, nil
			if m.focus == focusRail {
				m.focus = focusWorkspace
			}
			return m.Update(actionKey(action.key))
		}
	}
	query := m.actions.input.Value()
	var cmd tea.Cmd
	m.actions.input, cmd = m.actions.input.Update(msg)
	if m.actions.input.Value() != query {
		m.actions.selected = 0
	}
	return m, cmd
}

func actionKey(value string) tea.KeyPressMsg {
	switch value {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	}
	return tea.KeyPressMsg{Code: []rune(value)[0], Text: value}
}

func (m *model) openWorklogFilter() tea.Cmd {
	if m.worklogFilter == nil {
		m.worklogFilter = &worklogFilterState{input: m.searchInput("Issue, title, or description…"), previousID: m.selectedWorklogID()}
	}
	m.worklogFilter.editing = true
	m.resizeSearchInputs()
	return m.worklogFilter.input.Focus()
}

func (m *model) clearWorklogFilter() tea.Cmd {
	id := m.worklogFilter.previousID
	m.worklogFilter = nil
	m.restoreWorklogSelection(id)
	return m.startIssueTotalLoad()
}

func (m model) updateWorklogFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return m, m.clearWorklogFilter()
		case "enter":
			m.worklogFilter.editing = false
			m.worklogFilter.input.Blur()
			return m, nil
		case "up", "down":
			direction := 1
			if key.String() == "up" {
				direction = -1
			}
			m.moveWorklogSelection(direction)
			return m, m.startIssueTotalLoad()
		}
	}
	id := m.selectedWorklogID()
	query := m.worklogFilter.input.Value()
	var cmd tea.Cmd
	m.worklogFilter.input, cmd = m.worklogFilter.input.Update(msg)
	if m.worklogFilter.input.Value() == query {
		return m, cmd
	}
	m.restoreWorklogSelection(id)
	if m.selectedWorklogID() == id {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.startIssueTotalLoad())
}

func (m model) visibleWorklogIndices() []int {
	query := ""
	if m.worklogFilter != nil && m.worklogView == dayWorklogView {
		query = strings.ToLower(strings.TrimSpace(m.worklogFilter.input.Value()))
	}
	indices := make([]int, 0, len(m.worklogs))
	for index, item := range m.worklogs {
		if strings.Contains(strings.ToLower(item.IssueKey+" "+m.issueMetadata[item.IssueKey].Title+" "+item.Description), query) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (m *model) moveWorklogSelection(direction int) {
	indices := m.visibleWorklogIndices()
	if len(indices) == 0 {
		m.worklogSelected = -1
		return
	}
	position := slices.Index(indices, m.worklogSelected)
	position = max(0, min(len(indices)-1, position+direction))
	m.worklogSelected = indices[position]
}

func (m model) renderActions(width, height int) string {
	lines := []string{m.paintBold("Actions · "+m.activeTabLabel(), m.colors.Focus), ""}
	if m.actions != nil {
		lines = append(lines, m.actions.input.View(), m.rule(width-4))
	}
	matches := m.matchingActions()
	selected := 0
	if m.actions != nil {
		selected = m.actions.selected
	}
	info := ""
	if m.activeTab == worklogsTab && m.worklogView == monthWorklogView {
		info = "Quota assumes Mon–Fri; holidays and leave are not adjusted."
	}
	reserved := 3
	if info != "" {
		reserved += 2
	}
	start, end := visibleRange(selected, len(matches), max(1, height-len(lines)-reserved))
	for index := start; index < end; index++ {
		action := matches[index]
		prefix := "  "
		if index == selected {
			prefix = "> "
		}
		lines = append(lines, m.selectedRow(fmt.Sprintf("%s%-12s %s", prefix, action.chord, action.label), width-4, index == selected))
	}
	if len(matches) == 0 {
		lines = append(lines, m.muted("No matching actions."))
	}
	if len(matches) > 0 {
		lines = append(lines, "", m.muted(fmt.Sprintf("%d of %d actions", selected+1, len(matches))))
	}
	if info != "" {
		lines = append(lines, "", m.muted(info))
	}
	return strings.Join(lines, "\n")
}

func (m model) activeTabLabel() string {
	return [...]string{"Status", "Worklogs", "Plans", "Presets", "Trash"}[m.activeTab]
}
