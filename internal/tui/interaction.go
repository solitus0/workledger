package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) updateOverlay(key string) (tea.Model, tea.Cmd) {
	if m.overlay == helpOverlay {
		if key == "esc" || key == "?" {
			m.overlay = noOverlay
		}
		return m, nil
	}
	if key == "esc" || key == "n" {
		m.overlay = noOverlay
		m.deleteSelection = nil
		m.restoreSelection = nil
		m.presetDelete = nil
		if m.conflict != nil {
			m.conflict = nil
		}
		return m, nil
	}
	if key != "enter" && key != "y" {
		return m, nil
	}
	switch m.overlay {
	case deleteOverlay:
		if len(m.deleteSelection) != 1 {
			m.overlay = noOverlay
			return m, nil
		}
		selected := m.deleteSelection[0]
		m.overlay = noOverlay
		m.deleteSelection = nil
		return m, m.beginActivity("worklogs.delete", tuiActivitySummary("id="+selected.ID, "issue="+selected.IssueKey), m.deleteCmd(selected))
	case deleteDayOverlay:
		if len(m.deleteSelection) == 0 {
			m.overlay = noOverlay
			return m, nil
		}
		selection := append([]worklogs.LocalWorklog(nil), m.deleteSelection...)
		m.overlay = noOverlay
		m.deleteSelection = nil
		return m, m.beginActivity("worklogs.delete-day", tuiActivitySummary(m.selectedDate.Format("2006-01-02"), fmt.Sprintf("count=%d", len(selection))), m.deleteDayCmd(m.selectedDate, selection))
	case forceOverlay:
		m.overlay = noOverlay
		m.conflict = nil
		if m.form != nil {
			m.form.force = true
			return m.beginFormSubmit()
		}
	case reloadOverlay:
		m.overlay = noOverlay
		m.form = nil
		m.formError = ""
		m.reloadAfterForm = false
		return m, m.refreshCmd(refreshWorkspace)
	case restoreOverlay:
		if len(m.restoreSelection) != 1 {
			m.overlay = noOverlay
			return m, nil
		}
		item := m.restoreSelection[0]
		m.overlay = noOverlay
		m.restoreSelection = nil
		return m, m.beginActivity("trash.restore", tuiActivitySummary("trash_id="+item.ID, "issue="+item.IssueKey), m.restoreTrashCmd(item))
	case restoreDayOverlay:
		if len(m.restoreSelection) == 0 {
			m.overlay = noOverlay
			return m, nil
		}
		selection := append([]worklogs.TrashRecord(nil), m.restoreSelection...)
		m.overlay = noOverlay
		m.restoreSelection = nil
		return m, m.beginActivity("trash.restore-day", tuiActivitySummary(m.selectedDate.Format("2006-01-02"), fmt.Sprintf("count=%d", len(selection))), m.restoreTrashDayCmd(m.selectedDate, selection))
	case deletePresetOverlay:
		if m.presetDelete == nil {
			m.overlay = noOverlay
			return m, nil
		}
		item := *m.presetDelete
		m.overlay = noOverlay
		m.presetDelete = nil
		return m, m.beginActivity("presets.delete", "name="+item.Name, m.deletePresetCmd(item))
	case reconcilePlanOverlay:
		m.overlay = noOverlay
		return m, m.startPlanOperation("plan.reconcile", "")
	case applyPlanOverlay:
		m.overlay = noOverlay
		return m, m.startPlanOperation("plan.apply", "")
	case retryFailedPlanOverlay:
		m.overlay = noOverlay
		return m, m.startPlanOperation("plan.retry-failed", "failed")
	case retryUncertainPlanOverlay:
		m.overlay = noOverlay
		return m, m.startPlanOperation("plan.retry-uncertain", "uncertain")
	}
	return m, nil
}
