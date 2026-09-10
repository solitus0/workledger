package tui

import tea "charm.land/bubbletea/v2"

func (m model) updateStatus(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "s", "left", "h":
		m.statusView = statusDiagnosticsView
	case "c", "right", "l":
		m.statusView = statusConfigView
	case "up", "k":
		if m.statusView == statusConfigView {
			m.statusConfigSelected = max(0, m.statusConfigSelected-1)
		} else {
			m.statusSelected = max(0, m.statusSelected-1)
		}
	case "down", "j":
		if m.statusView == statusConfigView {
			m.statusConfigSelected = min(max(0, len(m.statusConfigRows())-1), m.statusConfigSelected+1)
		} else {
			m.statusSelected = min(max(0, len(m.statusItems)-1), m.statusSelected+1)
		}
	case "r":
		refresh := m.startStatusRefresh()
		cmds := []tea.Cmd{m.beginActivity("status.refresh", "diagnostics", refresh)}
		if m.workspace == nil {
			cmds = append(cmds, m.startWorkspaceOpen())
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}
