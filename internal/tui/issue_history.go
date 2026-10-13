package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

type issueHistoryState struct {
	issueKey                             string
	generation                           int
	items                                []worklogs.IssueWorklogDescription
	loading, more, historyFocus, seekEnd bool
	advance                              int
	err                                  string
	metadataText                         string
	metadata, history                    viewport.Model
}

type issueHistoryResultMsg struct {
	generation int
	issueKey   string
	offset     int
	items      []worklogs.IssueWorklogDescription
	more       bool
	err        error
}

func (m model) openIssueHistory() (tea.Model, tea.Cmd) {
	item := m.selectedIssue()
	if item == nil {
		return m, nil
	}
	m.overlay = detailsOverlay
	m.issueHistory = &issueHistoryState{issueKey: item.IssueKey, historyFocus: true, metadataText: m.renderIssueDetail(), metadata: viewport.New(), history: viewport.New()}
	m.clearNotice()
	return m, m.reloadIssueHistory()
}

func (m *model) reloadIssueHistory() tea.Cmd {
	state := m.issueHistory
	if state == nil {
		return nil
	}
	m.issueHistoryGen++
	state.generation = m.issueHistoryGen
	state.items = nil
	state.loading = false
	state.more = false
	state.err = ""
	state.advance = 0
	state.seekEnd = false
	state.history.GotoTop()
	m.resizeIssueHistory()
	return m.loadIssueHistoryPage()
}

func (m *model) loadIssueHistoryPage() tea.Cmd {
	state := m.issueHistory
	if state == nil || state.loading {
		return nil
	}
	if m.workspace == nil || m.workspace.worklogs == nil {
		state.err = "Worklog history unavailable"
		return nil
	}
	state.loading = true
	ws, ctx := m.workspace, m.ctx
	generation, issueKey, offset := state.generation, state.issueKey, len(state.items)
	return func() tea.Msg {
		items, more, err := ws.worklogs.ListIssueWorklogDescriptions(ctx, issueKey, issueDetailPageSize, offset)
		return issueHistoryResultMsg{generation: generation, issueKey: issueKey, offset: offset, items: items, more: more, err: err}
	}
}

func (m model) applyIssueHistoryResult(msg issueHistoryResultMsg) (tea.Model, tea.Cmd) {
	state := m.issueHistory
	if state == nil || m.overlay != detailsOverlay || msg.generation != state.generation || msg.issueKey != state.issueKey || msg.offset != len(state.items) {
		return m, nil
	}
	state.loading = false
	if msg.err != nil {
		state.err = msg.err.Error()
		state.seekEnd = false
		state.advance = 0
		return m, nil
	}
	state.items = append(state.items, msg.items...)
	state.more = msg.more
	state.err = ""
	m.resizeIssueHistory()
	if state.seekEnd {
		if state.more {
			return m, m.loadIssueHistoryPage()
		}
		state.seekEnd = false
		state.history.GotoBottom()
	} else if state.advance > 0 {
		advance := state.advance
		state.advance = 0
		return m.scrollIssueHistory(advance)
	}
	return m, nil
}

func (m *model) resizeIssueHistory() {
	state := m.issueHistory
	if state == nil {
		return
	}
	layout := calculateLayout(m.width, m.height, false)
	left, right := issuePickerPanelWidths(layout.workspace.width)
	state.metadata.SetWidth(max(1, left-4))
	state.metadata.SetHeight(max(1, layout.workspace.height-2))
	state.metadata.SetContent(ansi.Wrap(state.metadataText, state.metadata.Width(), ""))
	state.history.SetWidth(max(1, right-4))
	state.history.SetHeight(max(1, layout.workspace.height-6))
	var lines []string
	for _, item := range state.items {
		date := item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02 15:04")
		lines = append(lines, wrapIssueDescription(date+" · "+formatDuration(item.DurationSeconds), state.history.Width())...)
		lines = append(lines, wrapIssueDescription(item.Description, state.history.Width())...)
		lines = append(lines, "")
	}
	state.history.SetContent(strings.Join(lines, "\n"))
}

func (m model) updateIssueHistory(key string) (tea.Model, tea.Cmd) {
	state := m.issueHistory
	switch key {
	case "esc", "enter":
		m.issueHistory = nil
		m.overlay = noOverlay
		return m, nil
	case "tab", "shift+tab":
		state.historyFocus = !state.historyFocus
	case "left":
		state.historyFocus = false
	case "right":
		state.historyFocus = true
	case "r":
		return m, tea.Batch(m.reloadIssueHistory(), m.startIssueLoad())
	case "home":
		if state.historyFocus {
			state.seekEnd = false
			state.advance = 0
			state.history.GotoTop()
		} else {
			state.metadata.GotoTop()
		}
	case "end":
		if !state.historyFocus {
			state.metadata.GotoBottom()
			return m, nil
		}
		state.seekEnd = false
		state.advance = 0
		state.history.GotoBottom()
		if state.err == "" && (state.more || state.loading) {
			state.seekEnd = true
			return m, m.loadIssueHistoryPage()
		}
	case "up", "k", "down", "j", "pgup", "pgdown":
		if !state.historyFocus {
			state.metadata, _ = state.metadata.Update(actionKey(key))
			return m, nil
		}
		state.seekEnd = false
		delta := 1
		if key == "pgup" || key == "pgdown" {
			delta = max(1, state.history.Height()-1)
		}
		if key == "up" || key == "k" || key == "pgup" {
			delta = -delta
			state.advance = 0
		}
		return m.scrollIssueHistory(delta)
	}
	return m, nil
}

func (m model) scrollIssueHistory(delta int) (tea.Model, tea.Cmd) {
	state := m.issueHistory
	if delta < 0 {
		state.history.ScrollUp(-delta)
	} else {
		state.history.ScrollDown(delta)
	}
	if delta > 0 && state.history.AtBottom() && (state.more || state.loading) && state.err == "" {
		state.advance += delta
		return m, m.loadIssueHistoryPage()
	}
	return m, nil
}

func (m model) renderIssueHistory(width, height int) string {
	state := m.issueHistory
	leftWidth, rightWidth := issuePickerPanelWidths(width)
	right := []string{m.paintBold(truncate("Worklogs · "+state.issueKey, rightWidth-4), m.colors.Focus), m.muted("All active local worklogs"), ""}
	content := state.history.View()
	if len(state.items) == 0 && !state.loading && state.err == "" {
		content = m.muted("No worklogs for this issue.")
	}
	right = append(right, lipgloss.NewStyle().Height(max(1, height-6)).Render(content))
	status := fmt.Sprintf("%d worklogs", len(state.items))
	if state.loading {
		status = "Loading worklogs…"
	} else if state.err != "" {
		status = "Worklogs unavailable: " + state.err
	} else if state.more {
		status += " loaded · more available"
	} else if !state.history.AtBottom() {
		status += " · ↓ more"
	}
	if state.err != "" {
		status = m.paint(truncate(status, rightWidth-4), m.colors.Destructive)
	} else {
		status = m.muted(truncate(status, rightWidth-4))
	}
	right = append(right, status)
	return lipgloss.JoinHorizontal(lipgloss.Top, m.box(state.metadata.View(), leftWidth, height, !state.historyFocus), m.box(strings.Join(right, "\n"), rightWidth, height, state.historyFocus))
}

func (m *model) refreshIssueHistoryMetadata() {
	if m.issueHistory == nil {
		return
	}
	for index := range m.issues {
		if m.issues[index].IssueKey == m.issueHistory.issueKey {
			m.issueHistory.metadataText = m.renderIssueMetadataDetails(&m.issues[index], 0)
			m.resizeIssueHistory()
			return
		}
	}
}
