package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/solitus0/workledger/internal/reconcile"
)

func (m model) renderPlansWorkspace(width, height int) string {
	if m.workspace == nil || m.workspace.plans == nil {
		return m.pane("Plans unavailable", width, height)
	}
	if m.planOperation != nil {
		return m.pane(m.renderPlanOperation(), width, height)
	}
	if m.planForm != nil {
		return m.pane(m.renderPlanForm(), width, height)
	}
	if m.plan != nil {
		return m.renderPlanDetailWorkspace(width, height)
	}
	return m.renderPlanListWorkspace(width, height)
}

func (m model) renderPlanListWorkspace(width, height int) string {
	listWidth, _ := inspectorWidths(width)
	rangeLabel := m.selectedDate.Format("Monday, 2006-01-02")
	if m.planListView == planWeekListView {
		start, end := weekBounds(m.selectedDate)
		rangeLabel = start.Format("2006-01-02") + "–" + end.Format("2006-01-02")
	}
	chrome := m.pane("Saved plans · "+rangeLabel+" (newest 100)\n"+m.renderPlanListSelectors()+"\n", width, 3)
	height -= 3
	lines := []string{m.muted("  Created     DIR  STATE             OPEN DONE")}
	start, end := visibleRange(m.planSelected, len(m.plans), height-len(lines))
	for index := start; index < end; index++ {
		item := m.plans[index]
		prefix := "  "
		if index == m.planSelected {
			prefix = "> "
		}
		row := fmt.Sprintf("%s%-11s %-4s %-17s %4d %4d", prefix, item.CreatedAt.In(m.workspace.cfg.Location).Format("02 Jan 15:04"), item.Direction, truncate(item.ExecutionState, 17), item.OpenItems, item.SucceededItems)
		lines = append(lines, m.selectedRow(row, listWidth-2, index == m.planSelected))
	}
	if len(m.plans) == 0 {
		message := "No saved plans in this " + m.planListViewLabel() + "."
		if m.planLoading {
			message = "Loading…"
		}
		lines = append(lines, message)
	}
	detail := "No saved plan selected."
	if selected := m.selectedPlanEntry(); selected != nil {
		detail = fmt.Sprintf("Saved plan\n\nDirection %s\nPlanning %s\nExecution %s\n\n%d scopes\n%d actionable · %d open\n%d succeeded\n\nWindow\n%s\nAdapters %s\nInstances %s\n\nPlan ID\n%s", selected.Direction, m.paintState(selected.PlanningStatus), m.paintState(selected.ExecutionState), selected.TotalItems, selected.ActionableItems, selected.OpenItems, selected.SucceededItems, formatPlanWindow(selected.WindowFromUTC, selected.WindowToUTC, m.workspace.cfg.Location), emptyDash(strings.Join(selected.AdapterFamilies, ", ")), emptyDash(strings.Join(selected.TargetInstances, ", ")), selected.ID)
	}
	return lipgloss.JoinVertical(lipgloss.Left, chrome, m.inspector(strings.Join(lines, "\n"), detail, width, height))
}

func (m model) renderPlanListSelectors() string {
	day, week := m.secondary("Day"), m.secondary("Week")
	if m.planListView == planDayListView {
		day = m.paint("[Day]", m.colors.Focus)
	} else {
		week = m.paint("[Week]", m.colors.Focus)
	}
	return day + "  " + week
}

func (m model) planListViewLabel() string {
	if m.planListView == planDayListView {
		return "day"
	}
	return "week"
}

func (m model) renderPlanDetailWorkspace(width, height int) string {
	listWidth, detailWidth := inspectorWidths(width)
	items := m.visiblePlanItems()
	mode := "ready scopes"
	if m.planShowAll {
		mode = "all scopes"
	}
	lines := []string{"Scope review · " + m.plan.Direction, fmt.Sprintf("Planning %s · Execution %s", m.paintState(m.plan.PlanningStatus), m.paintState(m.plan.ExecutionState)), "Showing " + mode, "", m.muted("  Target                 Action       State")}
	start, end := visibleRange(m.planItemSelected, len(items), max(1, height-len(lines)-6))
	for index := start; index < end; index++ {
		item := items[index]
		prefix := "  "
		if index == m.planItemSelected {
			prefix = "> "
		}
		row := fmt.Sprintf("%s%-22s %-12s %s", prefix, truncate(item.TargetAdapterInstance+"/"+item.TargetIssue, 22), truncate(item.PlannedAction, 12), m.paintState(item.ExecutionState))
		lines = append(lines, m.selectedRow(row, listWidth-2, index == m.planItemSelected))
	}
	detail := "No plan scope selected."
	if item := m.selectedPlanItem(); item != nil {
		lines = append(lines, "", m.rule(listWidth-2), "Profile "+emptyDash(item.RouteProfile), "Comparison "+m.paintState(item.ComparisonStatus), "Reason "+emptyDash(item.ReasonCode))
		if item.ReasonDetail != "" {
			lines = append(lines, wrapIssueDescription(item.ReasonDetail, listWidth-2)...)
		}
		detail = m.renderPlanItemInspector(*item, detailWidth-2, height)
	}
	if len(items) == 0 {
		lines = append(lines, "No scopes in this view.")
	}
	return m.inspector(strings.Join(lines, "\n"), detail, width, height)
}

func (m model) renderPlanItemInspector(item reconcile.PlanItem, width, height int) string {
	lines := m.planComparisonLines(item)
	if len(item.Payload) > 0 {
		lines = append(lines, "", m.secondary("Saved desired entries"))
		for index, row := range item.Payload {
			if len(lines) >= height-1 {
				lines = append(lines, fmt.Sprintf("+ %d more entries", len(item.Payload)-index))
				break
			}
			lines = append(lines, truncate(row.StartedAtUTC.In(m.workspace.cfg.Location).Format("02 Jan 15:04")+" "+formatEstimateDuration(int64(row.DurationSeconds)), width))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) planComparisonLines(item reconcile.PlanItem) []string {
	lines := []string{m.paintBold("Saved comparison", m.colors.Secondary), "", "Local at planning", fmt.Sprintf("%d rows · %s", item.LocalRowCount, formatEstimateDuration(int64(item.LocalTotal))), "Remote at planning", fmt.Sprintf("%d rows · %s", item.RemoteRowCount, formatEstimateDuration(int64(item.RemoteTotal))), "", m.paintBold("After apply", m.colors.Proposed)}
	if item.ComparisonStatus == "not_checked" || item.ComparisonStatus == "check_failed" {
		lines[5] = "Unavailable"
	}
	rows, total := item.LocalRowCount, item.LocalTotal
	target := "Remote"
	if m.plan.Direction == "pull" {
		rows, total, target = item.RemoteRowCount, item.RemoteTotal, "Local"
	}
	if len(item.Payload) > 0 {
		rows, total = len(item.Payload), 0
		for _, row := range item.Payload {
			total += row.DurationSeconds
		}
	}
	if item.PlanStatus != "ready" {
		lines = append(lines, "No executable changes")
	} else if len(item.Payload) == 0 && !item.HasDiffMetrics() {
		lines = append(lines, "Desired state unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("%s: %d rows · %s", target, rows, formatEstimateDuration(int64(total))))
	}
	lines = append(lines, fmt.Sprintf("Create %s · Delete %s", planDiffMetric(item, item.InspectionSummary.CreateRowCount), planDiffMetric(item, item.InspectionSummary.DeleteRowCount)), "Matched "+planDiffMetric(item, item.InspectionSummary.MatchedRowCount), "Execution "+m.paintState(item.ExecutionState))
	return lines
}

func planDiffMetric(item reconcile.PlanItem, value int) string {
	if !item.HasDiffMetrics() {
		return "-"
	}
	return fmt.Sprint(value)
}

func (m model) renderPlanScopeDetails(item reconcile.PlanItem) string {
	lines := []string{"Target " + item.TargetAdapterFamily + " / " + item.TargetAdapterInstance + " / " + item.TargetIssue,
		"Profile " + emptyDash(item.RouteProfile), "Window " + formatPlanWindow(item.WindowFromUTC, item.WindowToUTC, m.workspace.cfg.Location),
		"Planning " + item.PlanStatus + " · Action " + item.PlannedAction, "Comparison " + item.ComparisonStatus,
		"Reason " + emptyDash(item.ReasonCode), item.ReasonDetail, "", strings.Join(m.planComparisonLines(item), "\n")}
	if len(item.Payload) > 0 {
		lines = append(lines, "", "All saved desired entries")
		for index, row := range item.Payload {
			lines = append(lines, fmt.Sprintf("%d. %s · %s · %s", index+1, row.StartedAtUTC.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), formatEstimateDuration(int64(row.DurationSeconds)), row.IssueKey), row.Description, "")
		}
	}
	lines = append(lines, "", "Plan ID "+emptyDash(m.plan.ID), "Scope ID "+emptyDash(item.ID))
	return strings.Join(lines, "\n")
}

func (m model) renderPlanForm() string {
	directionPush, directionPull := m.secondary("Push"), m.secondary("Pull")
	if m.planForm.direction == "push" {
		directionPush = m.paint("[Push]", m.colors.Focus)
	} else {
		directionPull = m.paint("[Pull]", m.colors.Focus)
	}
	day, week, custom := m.secondary("Day"), m.secondary("Week"), m.secondary("Custom")
	if m.planForm.window == planDayWindow {
		day = m.paint("[Day]", m.colors.Focus)
	} else if m.planForm.window == planWeekWindow {
		week = m.paint("[Week]", m.colors.Focus)
	} else {
		custom = m.paint("[Custom]", m.colors.Focus)
	}
	target := "All valid configured targets"
	if m.planForm.targetIndex >= 0 && m.planForm.targetIndex < len(m.planForm.targets) {
		target = planTargetLabel(m.planForm.targets[m.planForm.targetIndex])
	}
	profile := ""
	if len(m.planForm.profiles) > 0 {
		profile = "\nProfile     " + m.planFormProfileLabel()
	}
	impact := "Planning reads remote worklogs and saves a reviewable plan. It does not apply changes."
	if m.planForm.direction == "pull" {
		impact += " Pull apply may later replace local rows and archive removed rows to Trash."
	} else {
		impact += " Push apply may later create or delete remote worklogs."
	}
	dates := "Dates       " + m.planFormWindowLabel()
	if m.planForm.window == planCustomWindow {
		fromLabel, toLabel := "From", "To"
		if m.planForm.dateFocus == 0 {
			fromLabel = "› From"
		} else {
			toLabel = "› To"
		}
		dates = fmt.Sprintf("%-11s %s\n%-11s %s", fromLabel, m.planForm.dateInputs[0].View(), toLabel, m.planForm.dateInputs[1].View())
	}
	return fmt.Sprintf("New reconcile plan\n\nDirection   %s  %s\nWindow      %s  %s  %s\n%s\nTarget      %s%s\n\n%s",
		directionPush, directionPull, day, week, custom, dates, target, profile, impact)
}

func (m model) renderPlanOperation() string {
	event := m.planOperation.progress
	message := "Starting…"
	if event.Message != "" {
		message = event.Message
	}
	progressText := ""
	if event.ScopeTotal > 0 {
		progressText = fmt.Sprintf("\nScopes %d/%d", event.ScopeDone, event.ScopeTotal)
	}
	if event.WorkTotal > 0 {
		progressText += fmt.Sprintf(" · work %d/%d", event.WorkDone, event.WorkTotal)
	}
	return fmt.Sprintf("%s\n\n%s%s\n\nThe operation remains cancellable. An in-flight remote mutation may finish with an uncertain outcome.",
		strings.ReplaceAll(m.planOperation.kind, ".", " "), m.paint(message, m.colors.Progress), progressText)
}

func (m model) paintPlanState(state string) string {
	if color, ok := m.stateColor(state); ok {
		return m.paint(state, color)
	}
	return state
}

func formatPlanWindow(from, to time.Time, location *time.Location) string {
	from = from.In(location)
	to = to.In(location)
	if sameDay(from, to) {
		return from.Format("2006-01-02")
	}
	return from.Format("2006-01-02") + "–" + to.Format("2006-01-02")
}
