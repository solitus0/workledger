package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

type issueHistoryState struct {
	issueKey                          string
	query                             string
	generation                        int
	items                             []worklogs.IssueWorklogDescription
	loading, more, seekEnd, seekMatch bool
	advance                           int
	err                               string
	metadata, history                 viewport.Model
}

type issueHistoryResultMsg struct {
	generation int
	issueKey   string
	offset     int
	items      []worklogs.IssueWorklogDescription
	more       bool
	err        error
}

// Selection owns the preview lifecycle, including search and epic navigation.
func (m *model) syncIssueHistory() tea.Cmd {
	key := ""
	if m.activeTab == issuesTab && m.issueView == issueListView {
		key = m.selectedIssueKey()
	}
	if key == "" {
		m.issueHistory = nil
		if m.focus == focusIssueHistory || m.focus == focusIssueMetadata {
			m.focus = focusWorkspace
		}
		return nil
	}
	query := m.issueHistoryQuery()
	if m.issueHistory != nil && m.issueHistory.issueKey == key && m.issueHistory.query == query {
		width, metadataHeight, historyHeight := m.issueInspectorSizes()
		if m.issueHistory.history.Width() != width || m.issueHistory.history.Height() != historyHeight || m.issueHistory.metadata.Height() != metadataHeight {
			m.resizeIssueHistory()
		}
		return nil
	}
	m.issueHistory = &issueHistoryState{issueKey: key, query: query, metadata: viewport.New(), history: viewport.New()}
	return m.reloadIssueHistory()
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
	state.seekMatch = state.query != ""
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
	if state == nil || msg.generation != state.generation || msg.issueKey != state.issueKey || msg.offset != len(state.items) {
		return m, nil
	}
	state.loading = false
	if msg.err != nil {
		state.err = msg.err.Error()
		state.seekEnd = false
		state.seekMatch = false
		state.advance = 0
		m.resizeIssueHistory()
		state.history.GotoTop()
		return m, nil
	}
	state.items = append(state.items, msg.items...)
	state.more = msg.more
	state.err = ""
	firstMatch := m.resizeIssueHistory()
	if state.seekMatch {
		if firstMatch >= 0 {
			state.history.SetYOffset(firstMatch)
			state.seekMatch = false
		} else if state.more {
			return m, m.loadIssueHistoryPage()
		} else {
			state.seekMatch = false
		}
	}
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

func (m model) issueInspectorSizes() (width, metadataHeight, historyHeight int) {
	// Temporary dialogs hide Activity but must preserve the inspector's scroll.
	layout := calculateLayout(m.width, m.height, m.activityOpen)
	_, right := inspectorWidths(layout.workspace.width)
	width = max(1, right-2)
	available := max(2, layout.workspace.height-len(m.issueInspectorHeader(width))-4)
	metadataHeight = max(1, available/2)
	return width, metadataHeight, max(1, available-metadataHeight)
}

func (m *model) resizeIssueHistory() int {
	state := m.issueHistory
	if state == nil {
		return -1
	}
	width, metadataHeight, historyHeight := m.issueInspectorSizes()
	state.metadata.SetWidth(width)
	state.metadata.SetHeight(metadataHeight)
	state.metadata.SetContent(ansi.Wrap(m.renderIssueMetadata(), width, ""))
	state.history.SetWidth(width)
	state.history.SetHeight(historyHeight)
	var lines []string
	if state.err != "" {
		lines = append(lines, wrapIssueDescription("History unavailable: "+state.err, width)...)
		lines = append(lines, "Refresh to retry. Full details are available.", "")
	}
	firstMatch := -1
	for _, item := range state.items {
		matches := state.query != "" && strings.Contains(strings.ToLower(item.Description), state.query)
		if matches {
			if firstMatch < 0 {
				firstMatch = len(lines)
			}
			lines = append(lines, m.paintBold("[match]", m.colors.Focus))
		}
		date := item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02 15:04")
		lines = append(lines, wrapIssueDescription(date+" · "+formatDuration(item.DurationSeconds), width)...)
		description := wrapIssueDescription(item.Description, width)
		if matches {
			for n := range description {
				description[n] = m.paintBold(description[n], m.colors.Focus)
			}
		}
		lines = append(lines, description...)
		lines = append(lines, "")
	}
	state.history.SetContent(strings.Join(lines, "\n"))
	return firstMatch
}

func (m model) updateIssueHistory(key string) (tea.Model, tea.Cmd) {
	state := m.issueHistory
	switch key {
	case "f2":
		content := m.renderIssueMetadata()
		if state.err != "" {
			content += "\n\nHistory unavailable\n" + state.err + "\n\nRefresh issue history to retry."
		} else {
			content += "\n\n" + strings.Join(m.issueHistoryLines(), "\n")
		}
		m.openDetails("Issue details", content)
	case "esc", "left":
		m.focus = focusWorkspace
	case "/":
		return m, m.openIssueSearch()
	case "ctrl+t":
		m.focus = focusWorkspace
		return m.switchIssueSearchMode()
	case "r":
		return m, tea.Batch(m.reloadIssueHistory(), m.startIssueLoad())
	case "home":
		if m.focus == focusIssueMetadata {
			state.metadata.GotoTop()
			return m, nil
		}
		state.seekEnd, state.seekMatch, state.advance = false, false, 0
		state.history.GotoTop()
	case "end":
		if m.focus == focusIssueMetadata {
			state.metadata.GotoBottom()
			return m, nil
		}
		state.seekMatch, state.advance = false, 0
		state.history.GotoBottom()
		if state.err == "" && (state.more || state.loading) {
			state.seekEnd = true
			return m, m.loadIssueHistoryPage()
		}
	case "up", "k", "down", "j", "pgup", "pgdown":
		if m.focus == focusIssueMetadata {
			state.metadata, _ = state.metadata.Update(actionKey(key))
			return m, nil
		}
		state.seekEnd, state.seekMatch = false, false
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

func (m model) issueInspectorHeader(width int) []string {
	item := m.selectedIssue()
	if item == nil {
		return []string{m.paintBold("Issue details", m.colors.Secondary), "", "No issue selected."}
	}
	estimate := "Not set"
	if item.MaxEstimateSeconds != nil {
		estimate = formatEstimateDuration(*item.MaxEstimateSeconds)
		if item.EstimateSource != "" && item.EstimateSource != "none" {
			estimate += " · " + item.EstimateSource
		}
	}
	title := "Issue details"
	if m.focus == focusIssueMetadata {
		title = "> " + title
	}
	if m.issueHistory != nil && m.issueHistory.metadata.TotalLineCount() > m.issueHistory.metadata.Height() {
		title += " · more"
	}
	lines := []string{m.paintBold(truncate(title, width), m.colors.Secondary), item.IssueKey,
		"Logged    " + formatEstimateDuration(item.LoggedSeconds), truncate("Estimate  "+estimate, width), "Remaining " + issueRemaining(*item)}
	if item.OverrunSeconds > 0 {
		lines = append(lines, m.paint("Overrun   "+formatEstimateDuration(item.OverrunSeconds), m.colors.Warning))
	}
	return lines
}

func (m model) renderIssueInspector(width, height int) string {
	lines := m.issueInspectorHeader(width)
	state := m.issueHistory
	if m.selectedIssue() == nil {
		return strings.Join(lines, "\n")
	}
	if state == nil || state.issueKey != m.selectedIssueKey() {
		return strings.Join(append(lines, m.muted("Loading worklogs…")), "\n")
	}
	lines = append(lines, state.metadata.View(), m.rule(width))
	title := "Worklogs"
	if m.focus == focusIssueHistory {
		title = "> " + title
	}
	lines = append(lines, m.paintBold(title, m.colors.Focus), m.muted("All active local worklogs"))
	content := state.history.View()
	if len(state.items) == 0 && !state.loading && state.err == "" {
		content = "No worklogs for this issue."
	}
	lines = append(lines, strings.Split(content, "\n")...)
	for rows := strings.Count(strings.Join(lines, "\n"), "\n") + 1; rows < height-1; rows++ {
		lines = append(lines, "")
	}
	status := fmt.Sprintf("%d worklogs", len(state.items))
	if state.loading {
		status = "Loading worklogs…"
	} else if state.err != "" {
		status = "History unavailable · full details available"
	} else if state.more {
		status += " loaded · more available"
	} else if !state.history.AtBottom() {
		status += " · more below"
	}
	if state.err != "" {
		lines = append(lines, m.paint(truncate(status, width), m.colors.Destructive))
	} else {
		lines = append(lines, m.muted(truncate(status, width)))
	}
	return strings.Join(lines, "\n")
}

func (m model) issueHistoryLines() []string {
	state := m.issueHistory
	var lines []string
	for _, item := range state.items {
		lines = append(lines, item.StartedAtUTC.In(m.selectedDate.Location()).Format("2006-01-02 15:04 MST")+" · "+formatDuration(item.DurationSeconds), item.Description, "")
	}
	if state.more {
		lines = append(lines, "More history is available in the issue inspector.")
	}
	return lines
}
