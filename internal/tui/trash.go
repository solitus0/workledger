package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) updateTrash(key string) (tea.Model, tea.Cmd) {
	if m.workspace == nil {
		return m, nil
	}
	switch key {
	case "d":
		m.trashView = dayWorklogView
		return m, nil
	case "w":
		m.trashView = weekWorklogView
		return m, nil
	case "s":
		scopes := []string{"", worklogs.TrashScopeLocal, worklogs.TrashScopeRemote}
		index := 0
		for i, scope := range scopes {
			if scope == m.trashScope {
				index = i
			}
		}
		m.trashScope = scopes[(index+1)%len(scopes)]
		return m, m.startTrashLoad()
	case "R":
		if m.trashView == weekWorklogView {
			selection := restorableLocalTrash(m.trashItems)
			if len(selection) == 0 {
				m.setNotice(noticeInfo, "no restorable local trash on the selected day")
				return m, nil
			}
			m.restoreSelection = selection
			m.overlay = restoreDayOverlay
			return m, nil
		}
		selected := m.selectedTrash()
		if selected == nil {
			return m, nil
		}
		if selected.StorageScope != worklogs.TrashScopeLocal {
			m.setNotice(noticeWarning, "remote trash is audit-only and cannot be restored")
			return m, nil
		}
		if selected.SourceWorklogID == nil {
			m.setNotice(noticeWarning, "local trash without an original ID cannot be restored")
			return m, nil
		}
		m.restoreSelection = []worklogs.TrashRecord{*selected}
		m.overlay = restoreOverlay
		return m, nil
	}
	if m.trashView == weekWorklogView {
		switch key {
		case "up", "k":
			m.selectTrashWeekDate(m.selectedDate.AddDate(0, 0, -1))
		case "down", "j":
			m.selectTrashWeekDate(m.selectedDate.AddDate(0, 0, 1))
		case "left", "h":
			m.selectedDate = m.selectedDate.AddDate(0, 0, -7)
			return m, m.refreshCmd(refreshDateData)
		case "right", "l":
			m.selectedDate = m.selectedDate.AddDate(0, 0, 7)
			return m, m.refreshCmd(refreshDateData)
		case "enter":
			m.trashView = dayWorklogView
		case "t":
			m.selectedDate = beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
			return m, m.refreshCmd(refreshDateData)
		case "r":
			return m, m.beginActivity("trash.refresh", m.selectedDate.Format("2006-01-02"), m.startTrashLoad())
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.trashSelected = max(0, m.trashSelected-1)
	case "down", "j":
		m.trashSelected = min(max(0, len(m.trashItems)-1), m.trashSelected+1)
	case "left", "h":
		m.selectedDate = m.selectedDate.AddDate(0, 0, -1)
		return m, m.refreshCmd(refreshDateData)
	case "right", "l":
		m.selectedDate = m.selectedDate.AddDate(0, 0, 1)
		return m, m.refreshCmd(refreshDateData)
	case "t":
		m.selectedDate = beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
		return m, m.refreshCmd(refreshDateData)
	case "r":
		return m, m.beginActivity("trash.refresh", m.selectedDate.Format("2006-01-02"), m.startTrashLoad())
	}
	return m, nil
}

func (m *model) selectTrashWeekDate(date time.Time) {
	start, end := weekBounds(m.selectedDate)
	if date.Before(start) || date.After(end) {
		return
	}
	m.selectedDate = date
	m.trashItems = trashForDate(m.trashWeekItems, date, m.workspace.cfg.Location)
	m.trashSelected = clampIndex(m.trashSelected, len(m.trashItems))
	m.selectWeekDate(date)
}
