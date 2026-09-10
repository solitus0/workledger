package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderTrashWorkspace(width, height int) string {
	if m.workspace == nil {
		return m.box("Trash unavailable", width, height, false)
	}
	topHeight := max(13, height*3/5)
	if m.trashView == dayWorklogView {
		topHeight = max(10, height/2)
	}
	detailHeight := height - topHeight
	lines := []string{m.trashTitle(), m.renderTrashSelectors(), ""}
	if m.trashView == weekWorklogView {
		lines = append(lines, m.muted(fmt.Sprintf("  %-5s %-11s %-7s %-8s %s", "DAY", "DATE", "LOCAL", "REMOTE", "DURATION")))
		lines = append(lines, m.trashWeekRows()...)
	} else {
		lines = append(lines, m.muted(fmt.Sprintf("  %-7s %-8s %-14s %-18s %s", "TIME", "SCOPE", "ISSUE", "REASON", "DESCRIPTION")))
		lines = append(lines, m.trashRows(width-6, max(1, topHeight-6))...)
		if len(m.trashItems) == 0 {
			if m.trashLoading {
				lines = append(lines, "  "+m.paint("Loading…", m.colors.Progress))
			} else {
				lines = append(lines, "  No trash for this date and scope.")
			}
		}
	}
	detail := m.renderTrashDetail()
	if m.overlay == restoreOverlay || m.overlay == restoreDayOverlay {
		detail = m.renderConfirmation(width-2, detailHeight-2)
	}
	confirmationOpen := m.overlay == restoreOverlay || m.overlay == restoreDayOverlay
	return lipgloss.JoinVertical(lipgloss.Left, m.box(strings.Join(lines, "\n"), width, topHeight, m.focus != focusRail && !confirmationOpen), m.box(detail, width, detailHeight, false))
}

func (m model) trashTitle() string {
	if m.trashView == weekWorklogView {
		start, end := weekBounds(m.selectedDate)
		return fmt.Sprintf("Trash week · %s–%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	return m.selectedDate.Format("Trash · Monday, 2006-01-02")
}

func (m model) renderTrashSelectors() string {
	day, week := m.secondary("Day"), m.secondary("Week")
	if m.trashView == dayWorklogView {
		day = m.paint("[Day]", m.colors.Focus)
	} else {
		week = m.paint("[Week]", m.colors.Focus)
	}
	all, local, remote := m.secondary("All"), m.secondary("Local"), m.secondary("Remote")
	switch m.trashScope {
	case worklogs.TrashScopeLocal:
		local = m.paint("[Local]", m.colors.Focus)
	case worklogs.TrashScopeRemote:
		remote = m.paint("[Remote]", m.colors.Focus)
	default:
		all = m.paint("[All]", m.colors.Focus)
	}
	return day + "  " + week + "     " + all + "  " + local + "  " + remote
}

func (m model) trashRows(width, height int) []string {
	start, end := visibleRange(m.trashSelected, len(m.trashItems), height)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		item := m.trashItems[i]
		prefix := "  "
		if i == m.trashSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		local := item.StartedAtUTC.In(m.workspace.cfg.Location)
		scope := fmt.Sprintf("%-8s", item.StorageScope)
		if item.StorageScope == worklogs.TrashScopeRemote {
			scope = m.paint(scope, m.colors.Warning)
		} else {
			scope = m.paint(scope, m.colors.Secondary)
		}
		rows = append(rows, fmt.Sprintf("%s%-7s %s %-14s %-18s %s", prefix, local.Format("15:04"), scope, item.IssueKey, truncate(item.ReasonCode, 18), truncate(item.Description, max(8, width-55))))
	}
	return rows
}

func (m model) trashWeekRows() []string {
	start, _ := weekBounds(m.selectedDate)
	rows := make([]string, 0, 7)
	for offset := 0; offset < 7; offset++ {
		date := start.AddDate(0, 0, offset)
		items := trashForDate(m.trashWeekItems, date, m.workspace.cfg.Location)
		local, remote, total := 0, 0, 0
		for _, item := range items {
			if item.StorageScope == worklogs.TrashScopeLocal {
				local++
			} else {
				remote++
			}
			total += item.DurationSeconds
		}
		prefix := "  "
		if sameDay(date, m.selectedDate) {
			prefix = m.paint("> ", m.colors.Focus)
		}
		rows = append(rows, fmt.Sprintf("%s%-5s %-11s %-7d %-8d %s", prefix, date.Format("Mon"), date.Format("2006-01-02"), local, remote, formatDuration(total)))
	}
	return rows
}

func (m model) renderTrashDetail() string {
	if m.trashView == weekWorklogView {
		local := len(restorableLocalTrash(m.trashItems))
		return fmt.Sprintf("%s\n\n%d archived rows; %d local rows can be restored atomically.", m.selectedDate.Format("Monday, 2006-01-02"), len(m.trashItems), local)
	}
	item := m.selectedTrash()
	if item == nil {
		return "No trash record selected."
	}
	original := "—"
	if item.SourceWorklogID != nil {
		original = *item.SourceWorklogID
	}
	restoreState := m.paint("Restorable", m.colors.Success)
	if item.StorageScope == worklogs.TrashScopeRemote {
		restoreState = m.paint("Audit-only; remote trash cannot be restored", m.colors.Warning)
	} else if item.SourceWorklogID == nil {
		restoreState = m.paint("Not restorable; original identity is unavailable", m.colors.Destructive)
	}
	return fmt.Sprintf("Issue       %s\nStart       %s\nDuration    %s\nScope       %s\nReason      %s\nOriginal ID %s\nTrash ID    %s\n\n%s\n%s", item.IssueKey, item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), formatDuration(item.DurationSeconds), item.StorageScope, item.ReasonCode, original, item.ID, item.Description, restoreState)
}
