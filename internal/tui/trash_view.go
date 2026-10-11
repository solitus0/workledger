package tui

import (
	"fmt"
	"strings"

	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderTrashWorkspace(width, height int) string {
	if m.workspace == nil {
		return m.pane("Trash unavailable", width, height)
	}
	listWidth, _ := inspectorWidths(width)
	lines := []string{m.trashTitle(), m.renderTrashSelectors(), ""}
	if m.trashView == weekWorklogView {
		lines = append(lines, m.muted("  Day Date       Local Remote Duration"))
		for _, row := range m.trashWeekRows() {
			lines = append(lines, m.selectedRow(row, listWidth-2, strings.HasPrefix(row, "> ")))
		}
	} else {
		lines = append(lines, m.muted("  Start Scope  Issue        Reason"))
		lines = append(lines, m.trashRows(listWidth-2, height-len(lines))...)
		if len(m.trashItems) == 0 {
			message := "No trash for this date and scope."
			if m.trashLoading {
				message = "Loading…"
			}
			lines = append(lines, message)
		}
	}
	return m.inspector(strings.Join(lines, "\n"), m.renderTrashDetail(), width, height)
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
	for index := start; index < end; index++ {
		item := m.trashItems[index]
		prefix := "  "
		if index == m.trashSelected {
			prefix = "> "
		}
		row := fmt.Sprintf("%s%-5s %-6s %-12s %s", prefix, item.StartedAtUTC.In(m.workspace.cfg.Location).Format("15:04"), item.StorageScope, truncate(item.IssueKey, 12), item.ReasonCode)
		rows = append(rows, m.selectedRow(row, width, index == m.trashSelected))
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
			prefix = "> "
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
	return fmt.Sprintf("%s\n\nIssue       %s\nStart       %s\nDuration    %s\nScope       %s\nReason      %s\nOriginal ID %s\nTrash ID    %s\n\n%s", restoreState, item.IssueKey, item.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), formatDuration(item.DurationSeconds), item.StorageScope, item.ReasonCode, original, item.ID, item.Description)
}
