package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

var footerKeyTokens = map[string]struct{}{
	"1": {}, "2": {}, "3": {}, "4": {}, "5": {}, "?": {}, "D": {}, "R": {}, "Tab": {}, "Enter": {}, "Esc": {},
	"A": {}, "a": {}, "c": {}, "d": {}, "d/w": {}, "e": {}, "f": {}, "g": {}, "h/l": {}, "j/k": {}, "n": {}, "p": {}, "q": {}, "r": {}, "s": {}, "s/c": {}, "t": {}, "u": {}, "v": {}, "w": {}, "x": {}, "y": {},
	"Ctrl+C": {}, "Ctrl+F": {}, "Ctrl+L": {}, "Ctrl+O": {}, "Ctrl+P": {}, "Ctrl+S": {}, "End": {}, "Home/End": {}, "PgUp/PgDn": {}, "Shift+Tab": {}, "←/→": {}, "↑/↓": {},
}

func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "Workledger"
	return view
}

func (m model) render() string {
	if m.width < minimumWidth || m.height < minimumHeight {
		message := fmt.Sprintf("WORKLEDGER\n\nTerminal too small: %dx%d\nResize to at least %dx%d", m.width, m.height, minimumWidth, minimumHeight)
		return lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, m.paint(message, m.colors.Warning))
	}
	showActivity := m.activityVisible()
	layout := calculateLayout(m.width, m.height, showActivity)
	rail := m.renderRail(layout.rail.height)
	rightPanel := m.renderWorkspace(layout.workspace.width, layout.workspace.height)
	if showActivity {
		rightPanel = lipgloss.JoinVertical(lipgloss.Left, rightPanel, m.renderActivityDrawer(layout.activity.width, layout.activity.height))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rail, " ", rightPanel)
	footer := m.renderFooter(m.width)
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func (m model) renderActivityDrawer(width, height int) string {
	title := "Activity"
	if m.activityLoading {
		title += "  " + m.paint("refreshing…", m.colors.Progress)
	}
	start, end := visibleRange(m.activitySelected, len(m.activities), activityDrawerListRows)
	rows := make([]string, 0, activityDrawerListRows)
	for index := start; index < end; index++ {
		item := m.activities[index]
		prefix := "  "
		if index == m.activitySelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		stateText := fmt.Sprintf("%-9s", item.State)
		state := stateText
		if color, ok := m.stateColor(string(item.State)); ok {
			state = m.paint(stateText, color)
		}
		const fixedWidth = 64
		summary := truncate(item.Summary, max(0, width-4-fixedWidth))
		row := fmt.Sprintf("%s%s  %-3s  %-24s  %s  %-8s  %s", prefix, item.StartedAt.Local().Format("15:04:05"), strings.ToUpper(string(item.Source)), truncate(item.Operation, 24), state, activityElapsed(item), summary)
		rows = append(rows, row)
	}
	for len(rows) < activityDrawerListRows {
		rows = append(rows, "")
	}
	detail := "No activity recorded."
	if len(m.activities) > 0 {
		item := m.activities[clampIndex(m.activitySelected, len(m.activities))]
		detail = item.Summary
		if item.ErrorCode != "" {
			detail = strings.TrimSpace(item.ErrorCode + "  " + item.ErrorMessage)
		}
		if detail == "" {
			detail = item.Operation
		}
	}
	detailColor := m.colors.Muted
	if len(m.activities) > 0 {
		switch m.activities[clampIndex(m.activitySelected, len(m.activities))].State {
		case activity.StateFailed:
			detailColor = m.colors.Destructive
		case activity.StatePartial:
			detailColor = m.colors.Warning
		}
	}
	content := title + "\n" + strings.Join(rows, "\n") + "\n" + m.paint(truncate(detail, width-4), detailColor)
	return m.box(content, width, height, m.focus == focusActivity)
}

func (m model) renderRail(height int) string {
	rail, _ := m.renderRailLayout(height)
	return rail
}

func (m model) renderRailLayout(height int) (string, [5]rectangle) {
	innerWidth := 26
	compact := height < 36
	statusActive := m.activeTab == statusTab
	worklogsActive := m.activeTab == worklogsTab
	trashActive := m.activeTab == trashTab
	presetsActive := m.activeTab == presetsTab
	plansActive := m.activeTab == plansTab
	statusTitle := m.railTitle("1 Status", statusActive)
	statusState := m.paint("Loading diagnostics…", m.colors.Progress)
	if !m.statusLoading {
		if m.statusErr != "" {
			statusState = m.paint("Check failed", m.colors.Destructive) + "\n" + truncate(m.statusErr, innerWidth)
		} else {
			ok, warnings, failures := statusCounts(m.statusItems)
			statusState = strings.Join([]string{
				m.statusSummaryLine("OK", ok, m.colors.Success),
				m.statusSummaryLine("Warnings", warnings, m.colors.Warning),
				m.statusSummaryLine("Errors", failures, m.colors.Destructive),
			}, "\n")
			if !m.statusChecked.IsZero() {
				statusState += "\n" + m.muted(fmt.Sprintf("%-10s%s", "Checked", m.statusChecked.Format("15:04:05")))
			}
		}
	}
	if compact {
		if m.statusLoading {
			statusState = m.paint("Loading…", m.colors.Progress)
		} else if m.statusErr != "" {
			statusState = m.paint("Check failed", m.colors.Destructive)
		} else {
			ok, warnings, failures := statusCounts(m.statusItems)
			statusState = strings.Join([]string{
				m.statusSummaryLine("OK", ok, m.colors.Success),
				m.statusSummaryLine("Warnings", warnings, m.colors.Warning),
				m.statusSummaryLine("Errors", failures, m.colors.Destructive),
			}, "  ")
		}
	}
	separator := "\n\n"
	if compact {
		separator = "\n"
	}
	statusCard := statusTitle + separator + statusState

	worklogsTitle := m.railTitle("2 Worklogs", worklogsActive)
	worklogsState := "Unavailable"
	if m.workspace != nil {
		daySeconds := 0
		for _, item := range m.worklogs {
			daySeconds += item.DurationSeconds
		}
		worklogsState = fmt.Sprintf("%-10s%s\n%-10s%s", "Selected", m.paint(fmt.Sprintf("%s  (%d)", formatDuration(daySeconds), len(m.worklogs)), m.colors.Information),
			"This week", m.paint(fmt.Sprintf("%s  (%d)", formatDuration(m.weekSummary.BookedSeconds), m.weekSummary.WorklogCount), m.colors.Information))
		if m.worklogLoading {
			worklogsState += "\n" + m.paint("Refreshing…", m.colors.Progress)
		}
	} else if m.workspaceLoading {
		worklogsState = m.paint("Loading workspace…", m.colors.Progress)
	} else if m.workspaceErr != nil {
		worklogsState += "\n" + truncate(m.workspaceErr.Error(), innerWidth)
	}
	worklogsCard := worklogsTitle + "\n\n" + worklogsState
	if compact {
		worklogsCard = worklogsTitle + "\n" + fmt.Sprintf("%s · %d rows", formatDuration(m.dayBookedSeconds()), len(m.worklogs))
	}
	trashTitle := m.railTitle("5 Trash", trashActive)
	trashState := "Unavailable"
	if m.workspace != nil {
		localCount, remoteCount := 0, 0
		for _, item := range m.trashItems {
			if item.StorageScope == worklogs.TrashScopeLocal {
				localCount++
			} else {
				remoteCount++
			}
		}
		trashState = fmt.Sprintf("Selected  %d\nLocal     %d\nRemote    %d", len(m.trashItems), localCount, remoteCount)
		if m.trashLoading {
			trashState += "\n" + m.paint("Refreshing…", m.colors.Progress)
		}
	}
	trashCard := trashTitle + "\n\n" + trashState
	if compact {
		trashCard = trashTitle + "\n" + fmt.Sprintf("%d selected", len(m.trashItems))
	}
	presetsTitle := m.railTitle("4 Presets", presetsActive)
	presetsState := "Unavailable"
	if m.workspace != nil {
		presetsState = fmt.Sprintf("Available  %d", len(m.presets))
		if selected := m.selectedPreset(); selected != nil {
			presetsState += "\nSelected   " + truncate(selected.Name, 14)
		}
		if m.presetLoading {
			presetsState += "\n" + m.paint("Refreshing…", m.colors.Progress)
		}
	}
	presetsCard := presetsTitle + "\n\n" + presetsState
	if compact {
		presetsCard = presetsTitle + "\n" + fmt.Sprintf("%d available", len(m.presets))
	}
	plansTitle := m.railTitle("3 Plans", plansActive)
	plansState := "Unavailable"
	if m.workspace != nil {
		unapplied, failed, uncertain := m.planRailCounts()
		plansState = fmt.Sprintf("%-11s%s\n%-11s%s\n%-11s%s",
			"Unapplied", m.paint(fmt.Sprint(unapplied), m.colors.Information),
			"Failed", m.paint(fmt.Sprint(failed), m.colors.Destructive),
			"Uncertain", m.paint(fmt.Sprint(uncertain), m.colors.Warning))
		if m.planLoading && !compact {
			plansTitle += " " + m.paint("↻", m.colors.Progress)
		}
	}
	plansCard := plansTitle + "\n\n" + plansState
	if compact {
		statusCard = statusTitle + "\n" + statusState
		worklogsCard = worklogsTitle + "  " + fmt.Sprintf("%s · %d", formatDuration(m.dayBookedSeconds()), len(m.worklogs))
		trashCard = trashTitle + "  " + fmt.Sprintf("%d selected", len(m.trashItems))
		presetsCard = presetsTitle + "  " + fmt.Sprintf("%d available", len(m.presets))
		unapplied, failed, uncertain := m.planRailCounts()
		plansCard = plansTitle + "  " + fmt.Sprintf("%d apply · %d retry", unapplied, failed+uncertain)
	}

	statusFocused := m.focus == focusRail && statusActive
	worklogsFocused := m.focus == focusRail && worklogsActive
	plansFocused := m.focus == focusRail && plansActive
	presetsFocused := m.focus == focusRail && presetsActive
	trashFocused := m.focus == focusRail && trashActive
	cardSpace := max(15, height)
	firstHeight := max(3, cardSpace/5)
	secondHeight := max(3, cardSpace/5)
	thirdHeight := max(3, cardSpace/5)
	fourthHeight := max(3, cardSpace/5)
	fifthHeight := max(3, cardSpace-firstHeight-secondHeight-thirdHeight-fourthHeight)
	first := m.box(statusCard, 30, firstHeight, statusFocused)
	second := m.box(worklogsCard, 30, secondHeight, worklogsFocused)
	third := m.box(plansCard, 30, thirdHeight, plansFocused)
	fourth := m.box(presetsCard, 30, fourthHeight, presetsFocused)
	fifth := m.box(trashCard, 30, fifthHeight, trashFocused)
	parts := []string{first, second, third, fourth, fifth}
	rail := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if delta := height - lipgloss.Height(rail); delta != 0 {
		fifthHeight = max(3, fifthHeight+delta)
		parts[4] = m.box(trashCard, 30, fifthHeight, trashFocused)
		rail = lipgloss.JoinVertical(lipgloss.Left, parts...)
	}
	var cards [5]rectangle
	top := 0
	for index, part := range parts {
		partHeight := lipgloss.Height(part)
		cards[index] = rectangle{x: 0, y: top, width: 30, height: partHeight}
		top += partHeight
	}
	return rail, cards
}

func (m model) planRailCounts() (unapplied, failed, uncertain int) {
	for _, plan := range m.plans {
		unapplied += plan.NotAttemptedItems
		failed += plan.FailedItems
		uncertain += plan.UncertainItems
	}
	return unapplied, failed, uncertain
}

func (m model) renderWorkspace(width, height int) string {
	if m.presetPicker != nil {
		return m.renderPresetPicker(width, height)
	}
	if m.overlay == helpOverlay {
		return m.renderOverlay(width, height)
	}
	if m.activeTab == statusTab {
		return m.renderStatusWorkspace(width, height)
	}
	if m.activeTab == trashTab {
		return m.renderTrashWorkspace(width, height)
	}
	if m.activeTab == presetsTab {
		return m.renderPresetWorkspace(width, height)
	}
	if m.activeTab == plansTab {
		return m.renderPlansWorkspace(width, height)
	}
	return m.renderWorklogWorkspace(width, height)
}

func (m model) renderOverlay(width, height int) string {
	content, color := m.overlayContent()
	modalWidth := min(68, width-8)
	modal := m.boxWithBorder(m.renderOverlayCopy(content, color), modalWidth, min(14, height-4), color)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (m model) renderOverlayCopy(content overlayCopy, color string) string {
	return strings.Join([]string{m.paint(content.title, color), content.body}, "\n\n")
}

type overlayCopy struct {
	title string
	body  string
}

func (m model) overlayContent() (overlayCopy, string) {
	var title, body string
	color := m.colors.Focus
	switch m.overlay {
	case helpOverlay:
		title = "Help"
		body = "The action bar is the authoritative shortcut reference and adapts to the current view and selection.\n\nThe left rail switches workspaces. Worklogs and Trash offer Day and Week views. Plans move from saved plans to scope review. Remote trash is audit-only."
	case deleteOverlay:
		title, color = "Move selected worklog to trash?", m.colors.Destructive
		if len(m.deleteSelection) == 1 {
			item := m.deleteSelection[0]
			body = fmt.Sprintf("%s  %s  %s\n%s\n\nIt can be restored from local trash if conflict-free.", item.StartedAtUTC.In(m.workspace.cfg.Location).Format("15:04"), formatDuration(item.DurationSeconds), item.IssueKey, item.Description)
		}
	case deleteDayOverlay:
		title, color = "Move selected day's worklogs to trash?", m.colors.Destructive
		body = fmt.Sprintf("Delete %s (%s) from %s?\n\nThey can be restored if conflict-free.",
			pluralizeWorklogs(len(m.deleteSelection)), formatDuration(deleteSelectionDuration(m.deleteSelection)), m.selectedDate.Format("Monday, 2006-01-02"))
	case forceOverlay:
		title, color = "Overlap detected", m.colors.Destructive
		if m.conflict != nil {
			lines := []string{
				fmt.Sprintf("Reason: %s", m.conflict.Reason),
				fmt.Sprintf("Attempted: %s  %s  %s", m.conflict.Attempted.StartedAt, formatDuration(m.conflict.Attempted.DurationSeconds), m.conflict.Attempted.IssueKey),
				"Conflicting rows:",
			}
			for _, id := range m.conflict.ConflictingIDs {
				lines = append(lines, m.conflictingWorklogSummary(id))
			}
			body = strings.Join(lines, "\n")
		}
	case reloadOverlay:
		title, color = "Worklog changed externally", m.colors.Warning
		body = "Reload the latest record and discard this draft?"
	case restoreOverlay, restoreDayOverlay:
		title = "Restore local worklogs?"
		count, duration := len(m.restoreSelection), 0
		reasons := make([]string, 0)
		pull := false
		for _, item := range m.restoreSelection {
			duration += item.DurationSeconds
			if !slices.Contains(reasons, item.ReasonCode) {
				reasons = append(reasons, item.ReasonCode)
			}
			pull = pull || item.Origin.PlanDirection == "pull"
		}
		body = fmt.Sprintf("Date        %s\nCount       %d\nDuration    %s\nOrigin      %s", m.selectedDate.Format("2006-01-02"), count, formatDuration(duration), strings.Join(reasons, ", "))
		if count == 1 && m.restoreSelection[0].SourceWorklogID != nil {
			body += "\nOriginal ID " + *m.restoreSelection[0].SourceWorklogID
		}
		if pull {
			body += "\n\n" + m.paint("Warning: restoring pull-origin trash may reintroduce reconciliation drift.", m.colors.Warning)
			color = m.colors.Warning
		}
	case deletePresetOverlay:
		title, color = "Delete worklog preset?", m.colors.Destructive
		if m.presetDelete != nil {
			body = fmt.Sprintf("%s\n%s\n\nExisting worklogs will not be changed.", m.presetDelete.Name, presetSummary(*m.presetDelete))
		}
	case reconcilePlanOverlay:
		title = "Inspect remote worklogs and save a plan?"
		profile := ""
		if len(m.planForm.profiles) > 0 {
			profile = "\nProfile    " + m.planFormProfileLabel()
		}
		body = fmt.Sprintf("Direction  %s\nWindow     %s\nTarget     %s%s\n\nThis operation reads remote worklogs. It does not apply changes.", m.planForm.direction, m.planFormWindowLabel(), m.planFormTargetLabel(), profile)
	case applyPlanOverlay:
		title, color = "Apply saved plan?", m.colors.Warning
		creates, deletes, complete := m.planMutationCounts()
		createText, deleteText := fmt.Sprint(creates), fmt.Sprint(deletes)
		if !complete {
			createText, deleteText = "-", "-"
		}
		body = fmt.Sprintf("Plan       %s\nDirection  %s\nScopes     %d\nCreates    %s\nDeletes    %s\n\n%s", m.plan.ID, m.plan.Direction, m.planExecutableCount("not_attempted"), createText, deleteText, m.planApplyImpact())
	case retryFailedPlanOverlay:
		title, color = "Retry failed plan scopes?", m.colors.Warning
		body = fmt.Sprintf("Plan    %s\nScopes  %d\n\nThe saved scope and payload will be reused.", m.plan.ID, m.planExecutableCount("failed"))
	case retryUncertainPlanOverlay:
		title, color = "Reconcile uncertain outcomes?", m.colors.Warning
		body = fmt.Sprintf("Plan    %s\nScopes  %d\n\nA prior remote mutation may already have succeeded. Current remote state will be checked before retrying.", m.plan.ID, m.planExecutableCount("uncertain"))
	}
	return overlayCopy{title: title, body: body}, color
}

func deleteSelectionDuration(items []worklogs.LocalWorklog) int {
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	return total
}

func (m model) renderConfirmation(width, height int) string {
	content, color := m.overlayContent()
	text := m.renderOverlayCopy(content, color)
	modalWidth := min(68, max(24, width-6))
	desiredHeight := 10
	if m.conflict != nil {
		desiredHeight = max(desiredHeight, 8+len(m.conflict.ConflictingIDs))
	}
	modalHeight := min(desiredHeight, max(7, height-2))
	modal := m.boxWithBorder(text, modalWidth, modalHeight, color)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (m model) renderFooter(width int) string {
	text := "1 Status   2 Worklogs   3 Plans   4 Presets   5 Trash   ? Help   q Quit"
	if m.overlay == helpOverlay {
		text = "Esc / ? Close help"
	} else if m.overlay == deleteOverlay {
		text = "y / Enter Confirm delete   n / Esc Cancel"
	} else if m.overlay == deleteDayOverlay {
		text = "y / Enter Move day to trash   n / Esc Cancel"
	} else if m.overlay == forceOverlay {
		text = "y / Enter Save despite conflict   n / Esc Return"
	} else if m.overlay == reloadOverlay {
		text = "y / Enter Reload and discard draft   n / Esc Keep draft"
	} else if m.overlay == restoreOverlay || m.overlay == restoreDayOverlay {
		text = "y / Enter Restore   n / Esc Cancel"
	} else if m.overlay == deletePresetOverlay {
		text = "y / Enter Delete preset   n / Esc Cancel"
	} else if m.overlay == reconcilePlanOverlay {
		text = "y / Enter Inspect and save plan   n / Esc Cancel"
	} else if m.overlay == applyPlanOverlay {
		text = "y / Enter Apply plan   n / Esc Cancel"
	} else if m.overlay == retryFailedPlanOverlay || m.overlay == retryUncertainPlanOverlay {
		text = "y / Enter Retry plan scopes   n / Esc Cancel"
	} else if m.presetPicker != nil {
		if m.presetPicker.searching {
			text = "Type Filter   Tab Complete   ↑/↓ Select   Enter Choose   Esc Close find"
		} else {
			text = "Ctrl+F Find   ↑/↓ Select   Enter Choose   Esc Cancel"
		}
	} else if m.presetForm != nil {
		text = "↑/↓ or Tab/Shift+Tab Fields   Ctrl+S Save   Esc Cancel"
	} else if m.focus == focusActivity {
		text = "↑/↓ or j/k Select   PgUp/PgDn Page   Home/End Edge   r Refresh   g Close activity   q Quit"
	} else if m.activeTab == statusTab {
		text = "s/c View   ←/→ Switch   ↑/↓ Select   r Refresh   ? Help   q Quit"
	} else if m.activeTab == trashTab {
		if m.trashView == weekWorklogView {
			text = "d/w View  s Scope  R Restore day  h/l Week  j/k Day  t Today  r Refresh  ? Help  q Quit"
		} else {
			text = "d/w View  s Scope  R Restore  h/l Date  j/k Row  t Today  r Refresh  ? Help  q Quit"
		}
	} else if m.activeTab == presetsTab {
		text = "a Add   e Edit   D Delete   j/k Select   r Refresh   ? Help   q Quit"
	} else if m.activeTab == plansTab {
		if m.planOperation != nil {
			text = "Esc Cancel operation   Ctrl+C Interrupt"
		} else if m.planForm != nil {
			text = "p Push   u Pull   d/w/c Window   x Target   v Profile   Enter Inspect   Esc Cancel"
		} else if m.plan != nil {
			actions := []string{"a Ready/All"}
			if m.planExecutableCount("not_attempted") > 0 {
				actions = append(actions, "A Apply")
			}
			if m.planExecutableCount("failed") > 0 {
				actions = append(actions, "f Retry failed")
			}
			if m.planExecutableCount("uncertain") > 0 {
				actions = append(actions, "u Retry uncertain")
			}
			actions = append(actions, "j/k Scope", "r Refresh", "Esc Back")
			text = strings.Join(actions, "   ")
		} else {
			text = "d/w View   h/l Date/week   n New plan   Enter Review   j/k Select   t Today   r Refresh   ? Help   q Quit"
		}
	} else if m.form != nil {
		text = m.renderFormNavigationHint() + "   Ctrl+S Save   Esc Cancel"
		if !m.form.editing {
			text = m.renderAddFormFooter(width)
		}
	} else if m.worklogView == weekWorklogView {
		text = "d/w View  D Delete day  h/l Week  j/k Day  Enter Open  a Add  A Preset  t Today  ? Help  q Quit"
	} else {
		text = "d Day  w Week  D Delete  a Add  A Preset  e Edit  h/l Date  t Today  r Refresh  ? Help  q Quit"
	}
	if m.overlay == noOverlay && m.form == nil && m.presetForm == nil && m.presetPicker == nil && m.focus != focusActivity {
		activityAction := "g Activity"
		if m.activityOpen {
			activityAction = "g Close activity"
		}
		text += "   " + activityAction
		if lipgloss.Width(text) > width-4 {
			text = strings.Join(strings.Fields(text), " ")
		}
		text = truncate(text, width-4)
	}
	notice := strings.TrimSpace(m.notice)
	styled := m.styleFooterHints(text)
	if m.confirmationOverlayOpen() {
		_, confirmationColor := m.overlayContent()
		styled = m.styleFooterHintsWith(text, map[string]string{
			"y": confirmationColor, "Enter": confirmationColor,
			"n": m.colors.Secondary, "Esc": m.colors.Secondary,
		})
	}
	if notice != "" {
		noticeColor := m.colors.Information
		switch m.noticeKind {
		case noticeSuccess:
			noticeColor = m.colors.Success
		case noticeWarning:
			noticeColor = m.colors.Warning
		case noticeError:
			noticeColor = m.colors.Destructive
		}
		notice = truncate(notice, width-4)
		styled = m.paint(notice, noticeColor)
		if actionWidth := width - 4 - lipgloss.Width(notice) - 3; actionWidth > 0 {
			styled += "   " + m.styleFooterHints(truncate(text, actionWidth))
		}
	}
	if m.form != nil && !m.form.editing && len(m.form.previewRecords) > 0 {
		action := "create " + pluralizeWorklogs(len(m.form.previewRecords))
		styled = strings.Replace(styled, action, m.paint(action, m.colors.Proposed), 1)
	}
	return m.box(styled, width, 2, false)
}

func (m model) renderAddFormFooter(width int) string {
	navigation := m.renderFormNavigationHint()
	nextPlacement := "Manual"
	switch m.form.placement {
	case manualPlacement:
		nextPlacement = "Fit"
	case fitPlacement:
		nextPlacement = "Fill"
	}
	if m.form.placement == manualPlacement {
		return fmt.Sprintf("%s   Ctrl+P %s   Ctrl+S Save   Esc Cancel", navigation, nextPlacement)
	}
	overtimeAction := "Allow overtime"
	compactOvertimeAction := "OT on"
	if m.form.overtime {
		overtimeAction = "Disable overtime"
		compactOvertimeAction = "OT off"
	}
	lunchAction := "Use lunch time"
	if m.form.noLunch {
		lunchAction = "Reserve lunch"
	}
	saveAction := "Save"
	if len(m.form.previewRecords) > 0 {
		saveAction = "create " + pluralizeWorklogs(len(m.form.previewRecords))
	}
	footer := fmt.Sprintf("%s   Ctrl+P %s   Ctrl+O %s   Ctrl+L %s   Ctrl+S %s   Esc Cancel", navigation, nextPlacement, overtimeAction, lunchAction, saveAction)
	if lipgloss.Width(footer) <= width-4 {
		return footer
	}
	completion := ""
	if hint := m.formCompletionHint(); hint != "" {
		completion = hint + "  "
	}
	if m.form.focus == descriptionField {
		completion += "Enter Save  "
	}
	return fmt.Sprintf("%sCtrl+P %s  Ctrl+O %s  Ctrl+L %s  Ctrl+S %s  Esc", completion, nextPlacement, compactOvertimeAction, lunchAction, saveAction)
}

func (m model) renderFormNavigationHint() string {
	if hint := m.formCompletionHint(); hint != "" {
		if m.form.focus == descriptionField {
			return hint + "   Enter Save   ↑/↓ Fields"
		}
		return hint + "   ↑/↓ Fields"
	}
	if m.form != nil && m.form.focus == descriptionField {
		return "Enter Save   ↑/↓ or Tab/Shift+Tab Fields"
	}
	return "↑/↓ or Tab/Shift+Tab Fields"
}

func (m model) formCompletionHint() string {
	if m.form == nil {
		return ""
	}
	switch m.form.focus {
	case issueField:
		input := m.form.inputs[issueInput]
		if input.Value() == "" && m.form.suggestedIssue != "" {
			return "Tab Use " + m.form.suggestedIssue
		}
		if input.CurrentSuggestion() != "" && input.CurrentSuggestion() != input.Value() {
			return "Tab Complete"
		}
	case startField:
		if !m.form.editing && m.form.placement == manualPlacement &&
			m.form.inputs[startInput].Value() == "" && m.form.suggestedStart != "" {
			return "Tab Use " + m.form.suggestedStart
		}
	case descriptionField:
		input := m.form.inputs[descriptionInput]
		if input.Value() == "" && m.form.suggestedDescription != "" {
			return "Tab Reuse description"
		}
		if input.CurrentSuggestion() != "" && input.CurrentSuggestion() != input.Value() {
			return "Tab Complete"
		}
	}
	return ""
}

func (m model) conflictingWorklogSummary(id string) string {
	for _, item := range m.worklogs {
		if item.ID != id {
			continue
		}
		local := item.StartedAtUTC
		if m.workspace != nil && m.workspace.cfg.Location != nil {
			local = local.In(m.workspace.cfg.Location)
		}
		return fmt.Sprintf("  %s  %s  %s  %s", local.Format("15:04"), formatDuration(item.DurationSeconds), item.IssueKey, truncate(item.Description, 28))
	}
	return "  " + id
}

func (m model) box(content string, width, height int, focused bool) string {
	borderColor := m.colors.Border
	if focused {
		borderColor = m.colors.Focus
	}
	return m.boxWithBorder(content, width, height, borderColor)
}

func (m model) boxWithBorder(content string, width, height int, borderColor string) string {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(max(1, width)).Height(max(1, height)).Padding(0, 1)
	if !m.deps.noColor && m.colorReady {
		style = style.BorderForeground(lipgloss.Color(borderColor))
	}
	return style.Render(content)
}

func (m model) paint(value, colorValue string) string {
	if m.deps.noColor || !m.colorReady {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorValue)).Render(value)
}

func (m model) paintBold(value, colorValue string) string {
	if m.deps.noColor || !m.colorReady {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorValue)).Render(value)
}

func (m model) railTitle(label string, active bool) string {
	if active {
		return m.paintBold(label, m.colors.Focus)
	}
	return label
}

func (m model) statusSummaryLine(label string, count int, color string) string {
	line := fmt.Sprintf("%-10s%d", label, count)
	if count == 0 {
		return m.muted(line)
	}
	return m.paint(line, color)
}

func (m model) styleFooterHints(value string) string {
	return m.styleFooterHintsWith(value, nil)
}

func (m model) styleFooterHintsWith(value string, overrides map[string]string) string {
	if m.deps.noColor {
		return value
	}
	var styled strings.Builder
	for index := 0; index < len(value); {
		if value[index] == ' ' || value[index] == '\t' {
			styled.WriteByte(value[index])
			index++
			continue
		}
		end := index
		for end < len(value) && value[end] != ' ' && value[end] != '\t' {
			end++
		}
		token := value[index:end]
		if _, ok := footerKeyTokens[token]; ok {
			color := m.colors.Focus
			if override, ok := overrides[token]; ok {
				color = override
			}
			styled.WriteString(m.paintBold(token, color))
		} else {
			styled.WriteString(token)
		}
		index = end
	}
	return styled.String()
}

func (m model) confirmationOverlayOpen() bool {
	return m.overlay != noOverlay && m.overlay != helpOverlay
}

func (m model) muted(value string) string { return m.paint(value, m.colors.Muted) }

func (m model) secondary(value string) string { return m.paint(value, m.colors.Secondary) }

func (m model) paintState(value string) string {
	color, ok := m.stateColor(value)
	if !ok {
		return value
	}
	return m.paint(value, color)
}

func (m model) paintStateMessage(state, message string) string {
	if state != "warning" && state != "error" && state != "failed" && state != "partial" && state != "uncertain" && state != "blocked" && state != "check_failed" {
		return message
	}
	color, _ := m.stateColor(state)
	return m.paint(message, color)
}

func (m model) stateColor(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "ok", "ready", "applied", "succeeded", "match":
		return m.colors.Success, true
	case "warning", "blocked", "check_failed", "partial", "partially_applied", "uncertain":
		return m.colors.Warning, true
	case "error", "failed":
		return m.colors.Destructive, true
	case "running", "loading", "pending":
		return m.colors.Progress, true
	case "canceled", "not_applicable":
		return m.colors.Secondary, true
	default:
		return "", false
	}
}

func (m model) statusPaint(value string) string {
	padded := fmt.Sprintf("%-10s", value)
	if color, ok := m.stateColor(value); ok {
		return m.paint(padded, color)
	}
	return m.muted(padded)
}

func statusCounts(items []status.Item) (ok, warnings, failures int) {
	for _, item := range items {
		switch item.Status {
		case "ok":
			ok++
		case "warning":
			warnings++
		case "error":
			failures++
		}
	}
	return ok, warnings, failures
}

func visibleRange(selected, total, height int) (int, int) {
	height = max(1, height)
	start := max(0, selected-height+1)
	end := min(total, start+height)
	return start, end
}

func formatDuration(seconds int) string {
	if seconds < 0 {
		return "-" + formatDuration(-seconds)
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%dh %02dm", hours, minutes)
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func emptyDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
