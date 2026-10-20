package tui

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type issueSearchMode int

const (
	searchIssues issueSearchMode = iota
	searchWorklogText
)

func (mode issueSearchMode) label() string {
	if mode == searchWorklogText {
		return "Worklog text"
	}
	return "Issues"
}

type issueTextSearchReadyMsg struct{ generation int }

type issueTextSearchResultMsg struct {
	generation int
	query      string
	keys       []string
	err        error
}

func (m *model) openIssueSearch() tea.Cmd {
	if m.issueSearch == nil {
		key := m.selectedIssueKey()
		m.issueSearch = &issueSearchState{input: m.searchInput("Issue key, title or epic"), previousKey: key, selectedKey: key}
	}
	m.focus = focusWorkspace
	m.issueSearch.editing = true
	m.resizeIssueInputs()
	return m.issueSearch.input.Focus()
}

func (m model) switchIssueSearchMode() (tea.Model, tea.Cmd) {
	var focus tea.Cmd
	if m.issueSearch == nil {
		focus = m.openIssueSearch()
	}
	state := m.issueSearch
	key := m.selectedIssueKey()
	if key != "" {
		state.selectedKey = key
	}
	state.queries[state.mode] = state.input.Value()
	state.mode = 1 - state.mode
	state.input.SetValue(state.queries[state.mode])
	state.input.CursorEnd()
	state.input.Placeholder = "Issue key, title or epic"
	if state.mode == searchWorklogText {
		state.input.Placeholder = "Worklog description text"
	}
	m.resizeIssueInputs()
	m.issueRange = nil
	cmd := m.loadIssueTextSearch(false)
	m.restoreIssueSelection(state.selectedKey)
	return m, tea.Batch(focus, cmd)
}

func (m *model) loadIssueTextSearch(debounce bool) tea.Cmd {
	state := m.issueSearch
	if state == nil {
		return nil
	}
	m.issueSearchGen++
	if key := m.selectedIssueKey(); key != "" {
		state.selectedKey = key
	}
	state.loading, state.err, state.matches = false, "", nil
	if state.mode != searchWorklogText || strings.TrimSpace(state.input.Value()) == "" {
		return nil
	}
	state.loading = true
	if debounce {
		generation := m.issueSearchGen
		return tea.Tick(issuePickerSearchWait, func(time.Time) tea.Msg {
			return issueTextSearchReadyMsg{generation: generation}
		})
	}
	return m.issueTextSearchCmd()
}

func (m model) issueTextSearchCmd() tea.Cmd {
	ws, ctx := m.workspace, m.ctx
	generation, query := m.issueSearchGen, m.issueSearch.input.Value()
	return func() tea.Msg {
		if ws == nil || ws.worklogs == nil {
			return issueTextSearchResultMsg{generation: generation, query: query, err: errors.New("worklog text search unavailable")}
		}
		keys, err := ws.worklogs.FindIssuesByWorklogText(ctx, query)
		return issueTextSearchResultMsg{generation: generation, query: query, keys: keys, err: err}
	}
}

func (m model) applyIssueTextSearchResult(msg issueTextSearchResultMsg) (tea.Model, tea.Cmd) {
	state := m.issueSearch
	if state == nil || state.mode != searchWorklogText || msg.generation != m.issueSearchGen || msg.query != state.input.Value() {
		return m, nil
	}
	state.loading = false
	if msg.err != nil {
		state.err = msg.err.Error()
	} else {
		state.matches = make(map[string]bool, len(msg.keys))
		for _, key := range msg.keys {
			state.matches[key] = true
		}
	}
	m.restoreIssueSelection(state.selectedKey)
	return m, nil
}

func (m model) issueHistoryQuery() string {
	if m.issueSearch != nil && m.issueSearch.mode == searchWorklogText {
		return strings.ToLower(strings.TrimSpace(m.issueSearch.input.Value()))
	}
	return ""
}
