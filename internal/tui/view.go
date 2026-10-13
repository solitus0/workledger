package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/status"
	"github.com/solitus0/workledger/internal/worklogs"
)

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
	dividerColor := m.colors.Border
	if m.focus == focusRail {
		dividerColor = m.colors.Focus
	}
	divider := m.paint(strings.TrimSuffix(strings.Repeat("│\n", layout.body.height), "\n"), dividerColor)
	body := lipgloss.JoinHorizontal(lipgloss.Top, rail, divider, rightPanel)
	footer := m.renderFooter(m.width)
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(m.width), body, footer)
}

func (m model) renderHeader(width int) string {
	title := m.paintBold("Workledger", m.colors.Secondary)
	date := m.secondary(m.selectedDate.Format("Mon 02 Jan 2006"))
	line := " " + title + strings.Repeat(" ", max(1, width-lipgloss.Width(title)-lipgloss.Width(date)-2)) + date + " "
	return line + "\n" + m.rule(width)
}

func (m model) rule(width int) string {
	return m.paint(strings.Repeat("─", max(0, width)), m.colors.Border)
}

func (m model) pane(content string, width, height int) string {
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Padding(0, 1).Render(content)
}

func (m model) inspector(list, detail string, width, height int) string {
	listWidth, detailWidth := inspectorWidths(width)
	divider := m.paint(strings.TrimSuffix(strings.Repeat("│\n", height), "\n"), m.colors.Border)
	return lipgloss.JoinHorizontal(lipgloss.Top, m.pane(list, listWidth, height), divider, m.pane(ansi.Wrap(detail, detailWidth-2, ""), detailWidth, height))
}

func (m model) selectedRow(value string, width int, selected bool) string {
	value = truncate(value, width)
	if !selected {
		return value
	}
	style := lipgloss.NewStyle().Width(width)
	value = ansi.Strip(value)
	if !m.deps.noColor && m.colorReady {
		style = style.Foreground(lipgloss.Color(m.colors.Focus)).Reverse(true)
	}
	return style.Render(value)
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

func (m model) renderRailLayout(height int) (string, [tabCount]rectangle) {
	lines := []string{"", ""}
	var rows [tabCount]rectangle
	for index, name := range tabLabels {
		label := fmt.Sprintf("%d %s", index+1, name)
		rows[index] = rectangle{x: 0, y: appHeaderHeight + len(lines), width: navigationWidth, height: 1}
		prefix := "  "
		if tab(index) == m.activeTab {
			prefix = "> "
		}
		line := prefix + m.railTitle(label, tab(index) == m.activeTab)
		if m.focus == focusRail && tab(index) == m.activeTab {
			line = m.selectedRow(prefix+label, navigationWidth-2, true)
		}
		lines = append(lines, line, "")
	}
	lines = append(lines, m.rule(navigationWidth-2))
	unapplied, failed, uncertain := m.planRailCounts()
	lines = append(lines, "Plans "+m.paint(fmt.Sprintf("%d ready", unapplied), m.colors.Information))
	if failed > 0 {
		lines = append(lines, m.paint(fmt.Sprintf("Failed %d", failed), m.colors.Destructive))
	}
	if uncertain > 0 {
		lines = append(lines, m.paint(fmt.Sprintf("Uncertain %d", uncertain), m.colors.Warning))
	}
	if m.statusErr != "" {
		lines = append(lines, m.paint("Check failed", m.colors.Destructive))
	} else if !m.statusLoading {
		_, warnings, failures := statusCounts(m.statusItems)
		if failures > 0 {
			lines = append(lines, m.paint(fmt.Sprintf("%d errors", failures), m.colors.Destructive))
		} else if warnings > 0 {
			lines = append(lines, m.paint(fmt.Sprintf("%d warnings", warnings), m.colors.Warning))
		}
	}
	return m.pane(strings.Join(lines, "\n"), navigationWidth, height), rows
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
	if m.overlay == detailsOverlay && m.issueHistory != nil {
		return m.renderIssueHistory(width, height)
	}
	if m.overlay != noOverlay {
		background := m
		background.overlay = noOverlay
		base := background.renderWorkspace(width, height)
		modalWidth := min(72, width-6)
		var text string
		color := m.colors.Focus
		switch m.overlay {
		case helpOverlay:
			text = m.renderActions(modalWidth, height-2)
		case issueSortOverlay:
			text = m.renderIssueSortMenu(modalWidth - 4)
		case epicAssignmentOverlay:
			text = m.renderEpicAssignment(modalWidth-4, height-4)
		case detailsOverlay:
			text = m.paintBold(m.detailTitle, color) + "\n\n" + m.detailViewport.View()
		default:
			content, borderColor := m.overlayContent()
			color = borderColor
			text = m.renderOverlayCopy(content, color)
		}
		text = lipgloss.NewStyle().Width(modalWidth - 4).Render(text)
		modalHeight := min(height-2, lipgloss.Height(text)+2)
		modal := m.boxWithBorder(text, modalWidth, modalHeight, color)
		return lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(modal).X((width-modalWidth)/2).Y((height-modalHeight)/2).Z(1)).Render()
	}
	if m.move != nil {
		return m.renderMove(width, height)
	}
	if m.issuePicker != nil {
		return m.renderIssuePicker(width, height)
	}
	if m.presetPicker != nil {
		return m.renderPresetPicker(width, height)
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
	if m.activeTab == issuesTab {
		return m.renderIssuesWorkspace(width, height)
	}
	if m.activeTab == plansTab {
		return m.renderPlansWorkspace(width, height)
	}
	return m.renderWorklogWorkspace(width, height)
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

func (m model) renderFooter(width int) string {
	primary := m.idleHints()
	var secondary []actionHint
	switch {
	case m.overlay == helpOverlay:
		primary = []actionHint{{"↑/↓", "Select"}, {"Enter", "Run action"}, {"Esc", "Close"}}
		secondary = []actionHint{{"Type", "Filter actions"}}
	case m.overlay == detailsOverlay:
		primary = []actionHint{{"↑/↓", "Scroll"}, {"PgUp/PgDn", "Page"}, {"Enter / Esc", "Close details"}}
		secondary = nil
		if m.issueHistory != nil {
			primary = []actionHint{{"↑/↓ PgUp/Dn", "Scroll"}, {"Home/End", "Edge"}, {"Tab", "Pane"}, {"r", "Refresh"}, {"Esc", "Back"}}
		}
	case m.overlay == issueSortOverlay:
		primary = []actionHint{{"↑/↓", "Select sort"}, {"Enter", "Apply"}, {"Esc", "Cancel"}}
		secondary = nil
	case m.overlay == epicAssignmentOverlay:
		primary = []actionHint{{"↑/↓", "Destination"}, {"Enter", "Apply"}, {"Esc", "Cancel"}}
		secondary = []actionHint{{"Type", "Find or create epic"}}
		if m.epicAssignment != nil && m.epicAssignment.saving {
			primary, secondary = []actionHint{{"", "Saving epic assignments…"}}, nil
		} else if m.epicAssignment != nil && m.epicAssignment.stale {
			primary, secondary = []actionHint{{"Esc", "Cancel and reopen"}}, nil
		}
	case m.confirmationOverlayOpen():
		primary = []actionHint{{"y / Enter", m.confirmationAction()}, {"n / Esc", "Cancel"}}
		secondary = nil
	case m.move != nil:
		primary = []actionHint{{"↑/↓", "Position"}, {"Ctrl+S", "Save"}, {"r", "Refresh positions"}, {"Esc", "Cancel"}}
		secondary = nil
	case m.issuePicker != nil:
		primary = []actionHint{{"↑/↓", "Select"}, {"PgUp/PgDn", "Page"}, {"Enter", "Choose"}, {"Esc", "Close"}}
		secondary = []actionHint{{"Ctrl+F", "Find / mode"}, {"Tab", "Pane"}}
	case m.presetPicker != nil:
		primary = []actionHint{{"↑/↓", "Select"}, {"Enter", "Choose"}, {"Esc", "Cancel"}}
		secondary = []actionHint{{"Ctrl+F", "Find"}, {"Tab", "Complete"}}
	case m.presetForm != nil:
		primary = []actionHint{{"Tab", "Fields"}, {"Ctrl+S", "Save"}, {"Esc", "Cancel"}}
		secondary = nil
	case m.issueForm != nil:
		primary = []actionHint{{"Tab", "Fields"}, {"Ctrl+S", "Save"}, {"Esc", "Cancel"}}
		secondary = nil
	case m.planOperation != nil:
		primary = []actionHint{{"Esc", "Cancel operation"}, {"Ctrl+C", "Interrupt"}}
		secondary = nil
	case m.planForm != nil:
		primary = []actionHint{{"Enter", "Inspect"}, {"Esc", "Cancel"}, {"p", "Push"}, {"u", "Pull"}, {"d/w/c", "Window"}}
		secondary = []actionHint{{"x", "Target"}, {"v", "Profile"}, {"Tab", "Dates"}}
	case m.form != nil:
		primary = []actionHint{{"Ctrl+S", "Save"}, {"Esc", "Cancel"}}
		secondary = nil
		if m.form.focus == descriptionField {
			primary = append(primary, actionHint{"Enter", "Save"})
		}
		if !m.form.editing {
			next := "Manual"
			if m.form.placement == manualPlacement {
				next = "Fit"
			} else if m.form.placement == fitPlacement {
				next = "Fill"
			}
			secondary = append(secondary, actionHint{"Ctrl+P", next})
			if m.form.placement != manualPlacement {
				overtime, lunch := "Late starts on", "Use lunch"
				if m.form.overtime {
					overtime = "Late starts off"
				}
				if m.form.noLunch {
					lunch = "Reserve lunch"
				}
				secondary = append(secondary, actionHint{"Ctrl+O", overtime}, actionHint{"Ctrl+L", lunch})
			}
			if len(m.form.previewRecords) > 0 && m.form.previewError == "" && m.form.previewConflict == "" {
				primary[0].label = "Create " + pluralizeWorklogs(len(m.form.previewRecords))
			}
		}
		if hint := m.formCompletionHint(); hint != "" {
			secondary = append(secondary, actionHint{"Tab", strings.TrimPrefix(hint, "Tab ")})
		} else {
			secondary = append(secondary, actionHint{"Tab", "Fields"})
		}
		if m.form.focus == issueField && !m.form.editing {
			secondary = append(secondary, actionHint{"Ctrl+K", "Issues"})
		}
	case m.worklogFilter != nil && m.worklogFilter.editing && m.activeTab == worklogsTab && m.worklogView == dayWorklogView:
		primary = []actionHint{{"Type", "Filter worklogs"}, {"↑/↓", "Select"}, {"Enter", "Browse results"}, {"Esc", "Clear filter"}}
		secondary = nil
	case m.issueSearch != nil && m.issueSearch.editing && m.activeTab == issuesTab && m.issueView == issueListView:
		primary = []actionHint{{"Type", "Filter issues"}, {"↑/↓ PgUp/Dn", "Select"}, {"Enter", "Browse results"}, {"Esc", "Clear filter"}}
		secondary = nil
	case m.epicSearch != nil && m.epicSearch.editing && m.activeTab == issuesTab && m.issueView == epicListView:
		primary = []actionHint{{"Type", "Filter epic names"}, {"↑/↓ PgUp/Dn", "Select"}, {"Enter", "Browse results"}, {"Esc", "Clear filter"}}
		secondary = nil
	case m.focus == focusActivity:
		primary = []actionHint{{"↑/↓", "Select"}, {"PgUp/PgDn", "Page"}, {"r", "Refresh"}, {"g", "Close activity"}, {"?", "Actions"}, {"q", "Quit"}}
		secondary = []actionHint{{"Home/End", "Edge"}, {"Tab", "Pane"}}
	case m.focus == focusRail:
		primary = []actionHint{{"↑/↓", "Workspace"}, {"Enter / Tab", "Open"}, {"?", "Actions"}, {"q", "Quit"}}
	}
	primary = append(primary, secondary...)
	first := m.renderHints(primary, width-2)
	if m.confirmationOverlayOpen() {
		_, color := m.overlayContent()
		first = m.paint(primary[0].chord, color) + " " + primary[0].label + "  " + m.paint(primary[1].chord, m.colors.Secondary) + " " + primary[1].label
	}
	if m.interactionState.kind() == interactionIdle && m.focus != focusActivity && len(primary) >= 2 {
		tail := m.renderHints(primary[len(primary)-2:], width-2)
		first = m.renderHints(primary[:len(primary)-2], width-4-lipgloss.Width(tail)) + "  " + tail
	}
	if m.interactionState.kind() == interactionIdle && m.activeTab == worklogsTab && m.worklogView == dayWorklogView && m.selectedWorklog() != nil {
		indices := m.visibleWorklogIndices()
		context := fmt.Sprintf("%d of %d", slices.Index(indices, m.worklogSelected)+1, len(indices))
		if width-2-lipgloss.Width(first) >= lipgloss.Width(context)+4 {
			first += strings.Repeat(" ", width-2-lipgloss.Width(first)-lipgloss.Width(context)) + m.muted(context)
		}
	}
	if m.notice != "" {
		color := m.colors.Information
		switch m.noticeKind {
		case noticeError:
			color = m.colors.Destructive
		case noticeWarning:
			color = m.colors.Warning
		case noticeSuccess:
			color = m.colors.Success
		}
		// Notices share the action line while retaining save/cancel or quit controls.
		essential := []actionHint{}
		for _, hint := range primary {
			switch hint.chord {
			case "Ctrl+S", "Esc", "Enter", "Enter / Esc", "y / Enter", "n / Esc", "?", "q":
				essential = append(essential, hint)
			}
		}
		tail := m.renderHints(essential, width-2)
		notice := strings.Join(strings.Fields(m.notice), " ")
		first = m.paint(truncate(notice, max(1, width-4-lipgloss.Width(tail))), color)
		if tail != "" {
			first += "  " + tail
		}
	}
	return m.rule(width) + "\n" + m.pane(first, width, 1)
}

func (m model) renderHints(hints []actionHint, width int) string {
	parts := []string{}
	used := 0
	for _, hint := range hints {
		length := lipgloss.Width(hint.chord) + 1 + lipgloss.Width(hint.label)
		if len(parts) > 0 {
			length += 2
		}
		if used+length > width {
			continue
		}
		parts = append(parts, m.paint(hint.chord, m.colors.Focus)+" "+hint.label)
		used += length
	}
	return strings.Join(parts, "  ")
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
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Width(max(1, width)).Height(max(3, height)).MaxWidth(max(1, width)).MaxHeight(max(3, height)).Padding(0, 1)
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

func (m model) confirmationOverlayOpen() bool {
	return m.overlay != noOverlay && m.overlay != helpOverlay && m.overlay != detailsOverlay && m.overlay != issueSortOverlay && m.overlay != epicAssignmentOverlay
}

func (m model) confirmationAction() string {
	switch m.overlay {
	case deleteOverlay:
		return "Confirm delete"
	case deleteDayOverlay:
		return "Move day to trash"
	case restoreOverlay, restoreDayOverlay:
		return "Restore"
	case deletePresetOverlay:
		return "Delete preset"
	case forceOverlay:
		return "Save despite conflict"
	case reloadOverlay:
		return "Reload and discard draft"
	case reconcilePlanOverlay:
		return "Inspect and save plan"
	case applyPlanOverlay:
		return "Apply plan"
	default:
		return "Retry plan scopes"
	}
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
	return ansi.Truncate(value, width, "…")
}

func emptyDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
