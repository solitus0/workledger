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
	switch key {
	case "d":
		return m.switchWorklogView(dayWorklogView)
	case "w":
		return m.switchWorklogView(weekWorklogView)
	case "m":
		return m.switchWorklogView(monthWorklogView)
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
	if key == "D" {
		if m.worklogView == monthWorklogView {
			return m, nil
		}
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
	if m.worklogView == monthWorklogView {
		return m.updateMonth(key)
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
	case "M":
		if !m.worklogLoading {
			if selected := m.selectedWorklog(); selected != nil {
				return m, m.openMove(*selected)
			}
		}
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
		m.selectContextDate(m.selectedDate.AddDate(0, 0, -1))
	case "down", "j":
		m.selectContextDate(m.selectedDate.AddDate(0, 0, 1))
	case "left", "h":
		m.selectedDate = m.selectedDate.AddDate(0, 0, -7)
		return m, m.refreshCmd(refreshDateData)
	case "right", "l":
		m.selectedDate = m.selectedDate.AddDate(0, 0, 7)
		return m, m.refreshCmd(refreshDateData)
	case "enter":
		return m.switchWorklogView(dayWorklogView)
	}
	return m, nil
}

func (m *model) selectContextDate(date time.Time) {
	rangeStart, rangeEnd := m.worklogBounds(m.selectedDate)
	if date.Before(rangeStart) || date.After(rangeEnd) {
		return
	}
	m.selectedDate = date
	for _, day := range m.contextDays {
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

func (m model) switchWorklogView(view worklogView) (tea.Model, tea.Cmd) {
	previous := m.worklogView
	m.worklogView = view
	if (previous == monthWorklogView) != (view == monthWorklogView) {
		return m, m.startWorklogLoad()
	}
	return m, m.startIssueTotalLoad()
}

func (m model) worklogBounds(date time.Time) (time.Time, time.Time) {
	if m.worklogView == monthWorklogView {
		return monthBounds(date)
	}
	return weekBounds(date)
}

func monthBounds(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	return start, start.AddDate(0, 1, -1)
}

func (m model) updateMonth(key string) (tea.Model, tea.Cmd) {
	shift := 0
	switch key {
	case "left", "h":
		shift = -1
	case "right", "l":
		shift = 1
	case "up", "k":
		shift = -7
	case "down", "j":
		shift = 7
	case "pgup", "pgdown":
		months := 1
		if key == "pgup" {
			months = -1
		}
		start, _ := monthBounds(m.selectedDate)
		start = start.AddDate(0, months, 0)
		_, end := monthBounds(start)
		m.selectedDate = start.AddDate(0, 0, min(m.selectedDate.Day(), end.Day())-1)
		return m, m.refreshCmd(refreshDateData)
	case "enter":
		return m.switchWorklogView(dayWorklogView)
	default:
		return m, nil
	}
	return m.selectMonthDate(m.selectedDate.AddDate(0, 0, shift))
}

func (m model) selectMonthDate(date time.Time) (tea.Model, tea.Cmd) {
	start, end := monthBounds(m.selectedDate)
	if date.Before(start) || date.After(end) || sameDay(date, m.selectedDate) {
		return m, nil
	}
	m.selectContextDate(date)
	scope := refreshTrash | refreshPlans
	if m.worklogLoading {
		// A pending result is tied to the previously selected date.
		scope |= refreshWorklogs
	}
	return m, m.refreshCmd(scope)
}
