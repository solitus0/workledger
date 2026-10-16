package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/issues"
)

type issuePullState struct {
	ctx        context.Context
	cancel     context.CancelFunc
	keys       []string
	completed  int
	updated    int
	failed     int
	firstError string
}

type issuePullResultMsg struct {
	generation int
	result     issues.PullResult
}

func (m model) startIssuePull() (tea.Model, tea.Cmd) {
	if m.issuePull != nil || m.workspace == nil || m.workspace.issuePuller == nil || m.issuesLoading || m.issuesErr != "" {
		return m, nil
	}
	keys := m.markedIssueKeys()
	if len(keys) == 0 {
		if key := m.selectedIssueKey(); key != "" {
			keys = []string{key}
		}
	}
	if len(keys) == 0 {
		return m, nil
	}
	m.issueRange = nil
	if m.issueMarks == nil {
		m.issueMarks = map[string]bool{}
	}
	for _, key := range keys {
		m.issueMarks[key] = true
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.issuePull = &issuePullState{ctx: ctx, cancel: cancel, keys: keys}
	m.issuePullGen++
	m.setNotice(noticeInfo, m.issuePullLabel())
	return m, m.issuePullCmd()
}

// One request per command yields progress to Bubble Tea and makes the frozen
// batch cancellable between requests without blocking navigation or rendering.
func (m model) issuePullCmd() tea.Cmd {
	state, ws, generation := m.issuePull, m.workspace, m.issuePullGen
	key := state.keys[state.completed]
	return func() tea.Msg {
		if err := m.deps.checkWritable(ws.sqlitePath, "tui pull Jira metadata"); err != nil {
			return issuePullResultMsg{generation: generation, result: issues.PullResult{Failures: []issues.PullFailure{{IssueKey: key, Err: err}}}}
		}
		return issuePullResultMsg{generation: generation, result: ws.issuePuller.Pull(state.ctx, ws.cfg, []string{key})}
	}
}

func (m model) handleIssuePullResult(msg issuePullResultMsg) (tea.Model, tea.Cmd) {
	state := m.issuePull
	if state == nil || msg.generation != m.issuePullGen {
		return m, nil
	}
	for _, item := range msg.result.Items {
		delete(m.issueMarks, item.IssueKey)
		state.updated++
	}
	for _, failure := range msg.result.Failures {
		m.issueMarks[failure.IssueKey] = true
		state.failed++
		if state.firstError == "" {
			state.firstError = failure.IssueKey + ": " + failure.Err.Error()
		}
	}
	state.completed++
	if state.completed < len(state.keys) && state.ctx.Err() == nil {
		m.setNotice(noticeInfo, m.issuePullLabel())
		return m, m.issuePullCmd()
	}
	remaining := len(state.keys) - state.completed
	// Pending keys remain selected even if marks were changed during the pull.
	for _, key := range state.keys[state.completed:] {
		m.issueMarks[key] = true
	}
	label := fmt.Sprintf("Jira metadata: %d updated · %d failed", state.updated, state.failed)
	if remaining > 0 {
		label += fmt.Sprintf(" · %d pending", remaining)
	}
	kind := noticeSuccess
	if state.failed > 0 || remaining > 0 {
		kind = noticeWarning
		label += " · Shift+R to retry selected"
		if state.firstError != "" {
			label += " · " + strings.Join(strings.Fields(state.firstError), " ")
		}
	}
	state.cancel()
	m.issuePull = nil
	m.setNotice(kind, label)
	return m, m.refreshCmd(refreshIssues | refreshWorklogs)
}

func (m model) issuePullLabel() string {
	if m.issuePull == nil {
		return ""
	}
	state := m.issuePull
	return fmt.Sprintf("Pulling Jira metadata %d/%d · %d updated · %d failed · Esc stop", state.completed, len(state.keys), state.updated, state.failed)
}
