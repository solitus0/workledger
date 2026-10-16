package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (m model) renderIssuesWorkspace(width, height int) string {
	if m.workspace == nil || m.workspace.issues == nil {
		return m.pane("Issues unavailable", width, height)
	}
	if m.issueForm != nil {
		return m.pane(m.renderIssueForm(), width, height)
	}
	if len(m.issues) == 0 && (m.issuesLoading || m.issuesErr != "") {
		message := m.paint("Loading issues…", m.colors.Progress)
		if m.issuesErr != "" {
			message = m.paint("Unable to load issues.", m.colors.Destructive) + "\n\n" + m.issuesErr + "\n\nRefresh to retry."
		}
		title := "Issue details"
		if m.issueView == epicListView {
			title = "Epic details"
		}
		return m.inspector(m.renderIssueViewSelector()+"\n\n"+message, title+"\n\nIssue data unavailable.", width, height)
	}
	if m.issueView == epicListView {
		return m.renderEpicsWorkspace(width, height)
	}
	listWidth, detailWidth := inspectorWidths(width)
	contentWidth := listWidth - 2
	lines, indices, titleWidth := m.issueListContent(contentWidth)
	keyWidth, loggedWidth, remainingWidth := 12, 9, 10
	selected := slices.Index(indices, m.issueSelected)
	start, end := visibleRange(selected, len(indices), max(1, height-len(lines)))
	for _, index := range indices[start:end] {
		item := m.issues[index]
		prefix := "  "
		if index == m.issueSelected {
			prefix = "> "
		}
		mark := "[ ] "
		if m.issueMarks[item.IssueKey] {
			mark = "[x] "
		}
		prefix += mark
		remaining := issueRemaining(item)
		if item.OverrunSeconds > 0 {
			remaining = m.paint(remaining, m.colors.Warning)
		} else if item.RemainingSeconds == nil {
			remaining = m.muted(remaining)
		}
		logged := truncate(formatEstimateDuration(item.LoggedSeconds), loggedWidth)
		row := fmt.Sprintf("%s%-*s %-*s %*s %s", prefix, keyWidth, truncate(item.IssueKey, keyWidth), titleWidth, truncate(issueTitle(item), titleWidth), loggedWidth, logged, strings.Repeat(" ", max(0, remainingWidth-len(issueRemaining(item))))+remaining)
		lines = append(lines, m.selectedRow(row, contentWidth, index == m.issueSelected))
	}
	if len(indices) == 0 {
		message := "No issues yet. Add one or create a worklog."
		if m.issueSearch != nil {
			message = "No matching issues."
		}
		lines = append(lines, m.muted(message))
	}
	return m.inspector(strings.Join(lines, "\n"), m.renderIssueDetails(detailWidth-2), width, height)
}

// Rendering and paging share the exact header height, including wrapped labels.
func (m model) issueListContent(contentWidth int) ([]string, []int, int) {
	indices := m.visibleIssues()
	lines := []string{m.renderIssueViewSelector()}
	if state := m.issueRefreshLabel(); state != "" {
		lines = append(lines, strings.Split(ansi.Wrap(state, contentWidth, ""), "\n")...)
	}
	if m.issueEpic != nil {
		lines = append(lines, m.secondary(truncate("Epic: "+epicName(*m.issueEpic), contentWidth)))
	}
	countLabel := issueCountLabel(len(indices))
	if m.issuePull != nil {
		countLabel += " · " + m.issuePullLabel()
	}
	lines = append(lines, strings.Split(ansi.Wrap(countLabel, contentWidth, ""), "\n")...)
	if label := m.issueSelectionLabel(); label != "" {
		lines = append(lines, strings.Split(ansi.Wrap(m.paint(label, m.colors.Focus), contentWidth, ""), "\n")...)
	}
	lines = append(lines, "")
	if m.issueSearch != nil {
		lines = append(lines, m.paint("Find ", m.colors.Focus)+m.issueSearch.input.View(), "")
	}
	keyWidth, loggedWidth, remainingWidth := 12, 9, 10
	titleWidth := max(4, contentWidth-keyWidth-loggedWidth-remainingWidth-9)
	lines = append(lines, m.muted(fmt.Sprintf("      %-*s %-*s %*s %*s", keyWidth, "Issue", titleWidth, "Title", loggedWidth, "Logged", remainingWidth, "Remaining")))
	return lines, indices, titleWidth
}

func (m model) issueRefreshLabel() string {
	if m.issuePull != nil && m.issueView == epicListView {
		return m.paint(m.issuePullLabel(), m.colors.Progress)
	}
	if m.issuesLoading {
		return m.paint("Refreshing issues…", m.colors.Progress)
	}
	if m.issuesErr != "" {
		return m.paint("Refresh failed; showing previous data.", m.colors.Warning)
	}
	return ""
}

func (m model) renderIssueViewSelector() string {
	sort := m.muted("  ·  Sort: " + m.currentIssueSort().label(m.issueView))
	if m.issueView == epicListView {
		return "Issues  " + m.paintBold("[Epics]", m.colors.Focus) + sort
	}
	return m.paintBold("[Issues]", m.colors.Focus) + "  Epics" + sort
}

func (m model) renderEpicsWorkspace(width, height int) string {
	listWidth, detailWidth := inspectorWidths(width)
	contentWidth := listWidth - 2
	lines, groups, rows := m.epicListContent(contentWidth)
	selected := -1
	for index, group := range groups {
		if m.epicSelected && group.Epic == m.epicsSelected {
			selected = index
		}
	}
	start, end := visibleEpicRange(rows, selected, max(1, height-len(lines)))
	for index := start; index < end; index++ {
		for row, line := range rows[index] {
			if row == 0 && index == selected {
				line = "> " + line[2:]
			}
			lines = append(lines, m.selectedRow(line, contentWidth, index == selected && line != ""))
		}
	}
	if len(groups) == 0 {
		message := "No epics yet. Assign an epic while editing an issue."
		if m.epicSearch != nil {
			message = "No matching epics."
		}
		lines = append(lines, m.muted(message))
	}
	return m.inspector(strings.Join(lines, "\n"), m.renderEpicDetail(detailWidth-2), width, height)
}

func (m model) epicListContent(contentWidth int) ([]string, []worklogs.EpicSummary, [][]string) {
	groups := m.visibleEpics()
	lines := []string{m.renderIssueViewSelector()}
	if state := m.issueRefreshLabel(); state != "" {
		lines = append(lines, strings.Split(ansi.Wrap(state, contentWidth, ""), "\n")...)
	}
	lines = append(lines, "")
	if m.epicSearch != nil {
		lines = append(lines, m.paint("Find ", m.colors.Focus)+m.epicSearch.input.View(), "")
	}
	lines = append(lines, m.muted(fmt.Sprintf("  %-*s %8s", contentWidth-11, "Epic", "Issues")))
	rows := make([][]string, len(groups))
	for index, group := range groups {
		name := fmt.Sprintf("  %-*s %8d", contentWidth-11, truncate(epicName(group.Epic), contentWidth-11), group.IssueCount)
		rows[index] = []string{name}
		for _, line := range strings.Split(ansi.Wrap(m.epicEffortLine(group.IssueSummary), contentWidth-2, ""), "\n") {
			rows[index] = append(rows[index], "  "+line)
		}
		rows[index] = append(rows[index], "")
	}
	return lines, groups, rows
}

func visibleEpicRange(rows [][]string, selected, budget int) (start, end int) {
	if len(rows) == 0 {
		return 0, 0
	}
	selected = min(max(0, selected), len(rows)-1)
	start, used := selected, len(rows[selected])
	for start > 0 && used+len(rows[start-1]) <= budget {
		start--
		used += len(rows[start])
	}
	end = selected + 1
	for end < len(rows) && used+len(rows[end]) <= budget {
		used += len(rows[end])
		end++
	}
	return start, end
}

func (m model) epicEffortLine(summary worklogs.IssueSummary) string {
	parts := strings.Split(m.issueSummaryLine(summary), " · ")
	return formatEstimateDuration(summary.LoggedSeconds) + " logged · " + strings.Join(parts[1:], " · ")
}

func (m model) renderEpicDetail(compactWidth int) string {
	group := m.selectedEpic()
	if group == nil {
		return m.paintBold("Epic details", m.colors.Secondary) + "\n\nNo epic selected."
	}
	name := epicName(group.Epic)
	if compactWidth > 0 {
		name = truncate(name, compactWidth)
	}
	remaining := formatEstimateDuration(group.RemainingSeconds)
	estimated := formatEstimateDuration(group.EstimatedSeconds)
	if group.UnestimatedIssueCount > 0 {
		remaining += " known"
		estimated += " known"
		if group.EstimatedIssueCount == 0 {
			remaining = "Unknown"
			estimated = "Unknown"
		}
	}
	lines := []string{m.paintBold("Epic details", m.colors.Secondary), "", m.secondary(name), "",
		fmt.Sprintf("Issues      %d", group.IssueCount),
		"Logged      " + formatEstimateDuration(group.LoggedSeconds),
		"Estimated   " + estimated,
		"Remaining   " + remaining,
		fmt.Sprintf("Unestimated %d", group.UnestimatedIssueCount),
	}
	if group.OverrunSeconds > 0 {
		lines = append(lines, m.paint("Overrun     "+formatEstimateDuration(group.OverrunSeconds), m.colors.Warning))
	}
	if group.UnestimatedIssueCount > 0 {
		lines = append(lines, "", m.muted("Remaining excludes issues without estimates."))
	}
	return strings.Join(lines, "\n")
}

func (m model) issueSummaryLine(summary worklogs.IssueSummary) string {
	parts := []string{issueCountLabel(summary.IssueCount), formatEstimateDuration(summary.RemainingSeconds) + " left"}
	if summary.UnestimatedIssueCount > 0 {
		parts[1] = formatEstimateDuration(summary.RemainingSeconds) + " known left"
		if summary.EstimatedIssueCount == 0 {
			parts[1] = "Remaining unknown"
		}
		parts = append(parts, fmt.Sprintf("%d unestimated", summary.UnestimatedIssueCount))
	}
	if summary.OverrunSeconds > 0 {
		parts = append(parts, formatEstimateDuration(summary.OverrunSeconds)+" over")
	}
	return strings.Join(parts, " · ")
}

func issueTitle(item worklogs.IssueMetadata) string {
	if item.Title == "" {
		return "—"
	}
	return item.Title
}

func issueRemaining(item worklogs.IssueMetadata) string {
	if item.RemainingSeconds == nil {
		return "Unknown"
	}
	return formatEstimateDuration(*item.RemainingSeconds)
}

func (m model) renderIssueDetail() string {
	return m.renderIssueDetails(0)
}

func (m model) renderIssueDetails(compactWidth int) string {
	item := m.selectedIssue()
	if item == nil {
		return m.paintBold("Issue details", m.colors.Secondary) + "\n\nNo issue selected."
	}
	return m.renderIssueMetadataDetails(item, compactWidth)
}

func (m model) renderIssueMetadataDetails(item *worklogs.IssueMetadata, compactWidth int) string {
	lines := []string{m.paintBold("Issue details", m.colors.Secondary), "", item.IssueKey}
	if item.Title != "" {
		title := item.Title
		if compactWidth > 0 {
			title = truncate(title, compactWidth)
		}
		lines = append(lines, m.secondary(title))
	}
	estimate := "Not set"
	if item.MaxEstimateSeconds != nil && *item.MaxEstimateSeconds > 0 {
		estimate = formatEstimateDuration(*item.MaxEstimateSeconds)
		if item.EstimateSource != "none" && item.EstimateSource != "" {
			estimate += " · " + item.EstimateSource
		}
	}
	created := "Unknown"
	if !item.CreatedAt.IsZero() {
		created = item.CreatedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04")
	}
	lines = append(lines, "", "Created   "+created, "Logged    "+formatEstimateDuration(item.LoggedSeconds), "Estimate  "+estimate, "Remaining "+issueRemaining(*item))
	if item.OverrunSeconds > 0 {
		lines = append(lines, m.paint("Overrun   "+formatEstimateDuration(item.OverrunSeconds), m.colors.Warning))
	}
	epic := item.Epic
	if epic == "" {
		epic = "Not grouped"
	}
	if compactWidth > 0 {
		epic = truncate(epic, compactWidth)
	}
	if item.CachedTitle != "" || !item.RefreshedAt.IsZero() {
		lines = append(lines, "", "Jira cache · "+item.SourceAdapterFamily+"/"+item.SourceAdapterInst)
		if item.CachedTitle != "" {
			label := "Title     " + item.CachedTitle
			if item.ManualTitle != "" {
				label += " · manual override active"
			}
			lines = append(lines, label)
		}
		label := "Estimate  Unknown"
		if item.CachedMaxEstimateSeconds != nil {
			label = "Estimate  " + formatEstimateDuration(*item.CachedMaxEstimateSeconds)
		}
		if item.ManualMaxEstimateSeconds != nil {
			label += " · manual override active"
		}
		lines = append(lines, label)
		if !item.RefreshedAt.IsZero() {
			lines = append(lines, "Refreshed "+item.RefreshedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"))
		}
	}
	lines = append(lines, "", "Epic", epic)
	if item.Epic != "" {
		members := make([]worklogs.IssueMetadata, 0)
		for _, issue := range m.issues {
			if issue.Epic == item.Epic {
				members = append(members, issue)
			}
		}
		lines = append(lines, "", "Epic effort", m.issueSummaryLine(worklogs.SummarizeIssues(members)))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderIssueForm() string {
	title := "Add issue"
	if m.issueForm.editing {
		title = "Edit issue metadata"
	}
	lines := []string{m.paintBold(title, m.colors.Secondary), ""}
	labels := []string{"Issue", "Title", "Max estimate", "Epic"}
	for index, label := range labels {
		value := m.issueForm.inputs[index].View()
		if index == 0 && m.issueForm.editing {
			value = m.issueForm.inputs[index].Value()
		}
		lines = append(lines, m.renderFormRow(label, value, index == m.issueForm.focus))
	}
	helper := "Title, estimate and epic are optional."
	if m.issueForm.editing {
		helper = "Blank fields clear local overrides and epic assignment."
	}
	lines = append(lines, "", m.muted(helper))
	if item := m.selectedIssue(); m.issueForm.editing && item != nil && (item.CachedMaxEstimateSeconds != nil || item.CachedTitle != "") {
		lines = append(lines, m.muted("Blank title or estimate uses the cached Jira value."))
	}
	if m.issueForm.saving {
		lines = append(lines, "", m.paint("Saving…", m.colors.Progress))
	}
	if m.issueForm.stale {
		lines = append(lines, "", m.paint("Changed externally — cancel and reopen before saving.", m.colors.Warning))
	}
	if m.issueFormError != "" {
		lines = append(lines, "", m.paint(m.issueFormError, m.colors.Destructive))
	}
	return strings.Join(lines, "\n")
}
