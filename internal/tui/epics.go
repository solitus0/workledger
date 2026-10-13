package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func epicName(name string) string {
	if name == "" {
		return "Ungrouped"
	}
	return name
}

func (m model) visibleEpics() []worklogs.EpicSummary {
	groups := worklogs.GroupIssueEpics(m.issues)
	query := ""
	if m.epicSearch != nil {
		query = strings.ToLower(strings.TrimSpace(m.epicSearch.input.Value()))
	}
	visible := make([]worklogs.EpicSummary, 0, len(groups))
	for _, group := range groups {
		if query == "" || strings.Contains(strings.ToLower(epicName(group.Epic)), query) {
			visible = append(visible, group)
		}
	}
	slices.SortFunc(visible, func(a, b worklogs.EpicSummary) int {
		return compareEpics(a, b, m.epicSort)
	})
	return visible
}

func (m model) selectedEpic() *worklogs.EpicSummary {
	if !m.epicSelected {
		return nil
	}
	for _, group := range m.visibleEpics() {
		if group.Epic == m.epicsSelected {
			return &group
		}
	}
	return nil
}

func (m *model) restoreEpicSelection(name string, selected bool) {
	groups := m.visibleEpics()
	if selected {
		for _, group := range groups {
			if group.Epic == name {
				m.epicsSelected, m.epicSelected = name, true
				return
			}
		}
	}
	m.epicSelected = len(groups) > 0
	if len(groups) > 0 {
		m.epicsSelected = groups[0].Epic
	}
}

func (m *model) moveEpicSelection(delta int) {
	groups := m.visibleEpics()
	for position, group := range groups {
		if m.epicSelected && group.Epic == m.epicsSelected {
			next := min(max(0, position+delta), len(groups)-1)
			m.epicsSelected = groups[next].Epic
			return
		}
	}
	m.restoreEpicSelection("", false)
}

func (m model) updateEpics(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.moveEpicSelection(-1)
	case "down", "j":
		m.moveEpicSelection(1)
	case "pgup", "pgdown", "home", "end":
		m.navigateEpicList(key)
	case "s":
		m.openIssueSort()
	case "enter":
		if group := m.selectedEpic(); group != nil {
			name := group.Epic
			m.issueEpic = &name
			m.issueView = issueListView
			m.issueSearch = nil
			m.restoreIssueSelection(m.selectedIssueKey())
		}
	case "/":
		input := textinput.New()
		input.Prompt, input.Placeholder = "", "Epic name"
		previous := m.epicsSelected
		if m.epicSearch != nil {
			input.SetValue(m.epicSearch.input.Value())
			previous = m.epicSearch.previousKey
		}
		m.epicSearch = &issueSearchState{input: input, editing: true, previousKey: previous}
		m.resizeIssueInputs()
		m.restyleIssueInputs()
		return m, m.epicSearch.input.Focus()
	case "esc":
		if m.epicSearch != nil {
			name := m.epicSearch.previousKey
			m.epicSearch = nil
			m.restoreEpicSelection(name, true)
		}
	case "r":
		cmd := m.startIssueLoad()
		cmd = m.beginActivity("issues.list", "", cmd)
		return m, cmd
	}
	return m, nil
}

func (m model) updateEpicSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return m.updateEpics("esc")
		case "enter":
			m.epicSearch.editing = false
			m.epicSearch.input.Blur()
			return m, nil
		case "up":
			m.moveEpicSelection(-1)
			return m, nil
		case "down":
			m.moveEpicSelection(1)
			return m, nil
		case "pgup", "pgdown":
			m.navigateEpicList(key.String())
			return m, nil
		}
	}
	name, selected := m.epicsSelected, m.epicSelected
	input, cmd := m.epicSearch.input.Update(msg)
	m.epicSearch.input = input
	m.restoreEpicSelection(name, selected)
	return m, cmd
}
