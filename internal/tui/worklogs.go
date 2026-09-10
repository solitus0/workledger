package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) updateWorklogs(key string) (tea.Model, tea.Cmd) {
	if m.workspace == nil {
		return m, nil
	}
	if key == "d" {
		m.worklogView = dayWorklogView
		return m, m.startIssueTotalLoad()
	}
	if key == "D" {
		if m.worklogView == weekWorklogView {
			if len(m.worklogs) == 0 {
				m.setNotice(noticeInfo, "no worklogs to delete on the selected day")
				return m, nil
			}
			m.deleteSelection = append([]worklogs.LocalWorklog(nil), m.worklogs...)
			m.overlay = deleteDayOverlay
			return m, nil
		}
		if selected := m.selectedWorklog(); selected != nil {
			m.deleteSelection = []worklogs.LocalWorklog{*selected}
			m.overlay = deleteOverlay
		}
		return m, nil
	}
	if key == "w" {
		m.worklogView = weekWorklogView
		return m, nil
	}
	if m.worklogView == weekWorklogView {
		return m.updateWeek(key)
	}
	switch key {
	case "up", "k":
		m.worklogSelected = max(0, m.worklogSelected-1)
		return m, m.startIssueTotalLoad()
	case "down", "j":
		m.worklogSelected = min(max(0, len(m.worklogs)-1), m.worklogSelected+1)
		return m, m.startIssueTotalLoad()
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
		return m, m.beginActivity("worklogs.refresh", m.selectedDate.Format("2006-01-02"), m.startWorklogLoad())
	case "a":
		return m, m.openAddForm()
	case "A":
		return m, m.openPresetPicker()
	case "e":
		if selected := m.selectedWorklog(); selected != nil {
			m.openEditForm(*selected)
		}
	}
	return m, nil
}

func (m model) updateWeek(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.selectWeekDate(m.selectedDate.AddDate(0, 0, -1))
	case "down", "j":
		m.selectWeekDate(m.selectedDate.AddDate(0, 0, 1))
	case "left", "h":
		m.selectedDate = m.selectedDate.AddDate(0, 0, -7)
		return m, m.refreshCmd(refreshDateData)
	case "right", "l":
		m.selectedDate = m.selectedDate.AddDate(0, 0, 7)
		return m, m.refreshCmd(refreshDateData)
	case "enter":
		m.worklogView = dayWorklogView
		return m, m.startIssueTotalLoad()
	case "t":
		m.selectedDate = beginningOfDay(m.deps.now(), m.workspace.cfg.Location)
		return m, m.refreshCmd(refreshDateData)
	case "r":
		return m, m.beginActivity("worklogs.refresh", m.selectedDate.Format("2006-01-02"), m.startWorklogLoad())
	case "a":
		return m, m.openAddForm()
	case "A":
		return m, m.openPresetPicker()
	}
	return m, nil
}

func (m *model) selectWeekDate(date time.Time) {
	weekStart, weekEnd := weekBounds(m.selectedDate)
	if date.Before(weekStart) || date.After(weekEnd) {
		return
	}
	m.selectedDate = date
	for _, day := range m.weekDays {
		if day.Date != date.Format("2006-01-02") {
			continue
		}
		m.worklogs = day.Worklogs
		m.dayContext = day
		m.worklogSelected = clampIndex(m.worklogSelected, len(m.worklogs))
		return
	}
	m.worklogs = nil
	m.dayContext = worklogs.ContextDay{Date: date.Format("2006-01-02")}
	m.worklogSelected = 0
}
