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
		return m.box("Plans unavailable", width, height, false)
	}
	if m.overlay == reconcilePlanOverlay || m.overlay == applyPlanOverlay || m.overlay == retryFailedPlanOverlay || m.overlay == retryUncertainPlanOverlay {
		return m.renderOverlay(width, height)
	}
	if m.planForm != nil && m.overlay == noOverlay && m.planOperation == nil {
		return m.box(m.renderPlanForm(), width, height, true)
	}
	if m.planOperation != nil {
		return m.box(m.renderPlanOperation(), width, height, true)
	}
	if m.plan != nil {
		return m.renderPlanDetailWorkspace(width, height)
	}
	return m.renderPlanListWorkspace(width, height)
}

func (m model) renderPlanListWorkspace(width, height int) string {
	topHeight := max(10, height*3/5)
	detailHeight := height - topHeight
	rangeLabel := m.selectedDate.Format("Monday, 2006-01-02")
	if m.planListView == planWeekListView {
		start, end := weekBounds(m.selectedDate)
		rangeLabel = start.Format("2006-01-02") + "–" + end.Format("2006-01-02")
	}
	lines := []string{"Saved plans · " + rangeLabel + " (newest 100)", m.renderPlanListSelectors(), "", m.muted(fmt.Sprintf("  %-17s %-6s %-19s %-16s %5s %5s", "CREATED", "DIR", "WINDOW", "STATE", "OPEN", "DONE"))}
	start, end := visibleRange(m.planSelected, len(m.plans), max(1, topHeight-6))
	for index := start; index < end; index++ {
		item := m.plans[index]
		prefix := "  "
		if index == m.planSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		window := formatPlanWindow(item.WindowFromUTC, item.WindowToUTC, m.workspace.cfg.Location)
		lines = append(lines, fmt.Sprintf("%s%-17s %-6s %-19s %s %5d %5d",
			prefix, item.CreatedAt.In(m.workspace.cfg.Location).Format("2006-01-02 15:04"), item.Direction,
			truncate(window, 19), m.paintPlanState(fmt.Sprintf("%-16s", truncate(item.ExecutionState, 16))), item.OpenItems, item.SucceededItems))
	}
	if len(m.plans) == 0 {
		if m.planLoading {
			lines = append(lines, "  "+m.paint("Loading…", m.colors.Progress))
		} else {
			lines = append(lines, "  No saved plans in this "+m.planListViewLabel()+".")
		}
	}
	detail := "No saved plan selected."
	if selected := m.selectedPlanEntry(); selected != nil {
		detail = fmt.Sprintf("Plan       %s\nDirection  %s\nAdapters   %s\nInstances  %s\nWindow     %s\nStatus     Planning %s · Execution %s\n\n%d scopes · %d actionable · %d open · %d succeeded",
			selected.ID, selected.Direction, emptyDash(strings.Join(selected.AdapterFamilies, ", ")), emptyDash(strings.Join(selected.TargetInstances, ", ")),
			formatPlanWindow(selected.WindowFromUTC, selected.WindowToUTC, m.workspace.cfg.Location), m.paintState(selected.PlanningStatus), m.paintState(selected.ExecutionState),
			selected.TotalItems, selected.ActionableItems, selected.OpenItems, selected.SucceededItems)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.box(strings.Join(lines, "\n"), width, topHeight, m.focus != focusRail), m.box(detail, width, detailHeight, false))
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
	topHeight := max(12, height*3/5)
	detailHeight := height - topHeight
	items := m.visiblePlanItems()
	mode := "actionable scopes"
	if m.planShowAll {
		mode = "all scopes"
	}
	lines := []string{
		fmt.Sprintf("Plan %s · %s · %s", m.plan.ID, m.plan.Direction, formatPlanWindow(m.plan.WindowFromUTC, m.plan.WindowToUTC, m.workspace.cfg.Location)),
		fmt.Sprintf("Planning %s · Execution %s", m.paintState(m.plan.PlanningStatus), m.paintState(m.plan.ExecutionState)),
		"Showing " + mode,
		"",
		m.muted(fmt.Sprintf("  %-18s %-16s %-12s %-10s %5s %6s", "TARGET", "PROFILE", "ACTION", "STATE", "CREATE", "DELETE")),
	}
	start, end := visibleRange(m.planItemSelected, len(items), max(1, topHeight-7))
	for index := start; index < end; index++ {
		item := items[index]
		prefix := "  "
		if index == m.planItemSelected {
			prefix = m.paint("> ", m.colors.Focus)
		}
		target := item.TargetAdapterInstance + "/" + item.TargetIssue
		createCount := planDiffMetric(item, item.InspectionSummary.CreateRowCount)
		deleteCount := planDiffMetric(item, item.InspectionSummary.DeleteRowCount)
		execution := m.paintPlanState(fmt.Sprintf("%-10s", truncate(item.ExecutionState, 10)))
		lines = append(lines, fmt.Sprintf("%s%-18s %-16s %-12s %s %5s %6s", prefix, truncate(target, 18), truncate(emptyDash(item.RouteProfile), 16),
			truncate(item.PlannedAction, 12), execution, createCount, deleteCount))
	}
	if len(items) == 0 {
		lines = append(lines, "  No scopes in this view.")
	}
	detail := "No plan scope selected."
	if item := m.selectedPlanItem(); item != nil {
		detail = m.renderPlanItemDetail(*item)
	}
	confirmationOpen := m.overlay == applyPlanOverlay || m.overlay == retryFailedPlanOverlay || m.overlay == retryUncertainPlanOverlay
	if confirmationOpen {
		detail = m.renderConfirmation(width-2, detailHeight-2)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.box(strings.Join(lines, "\n"), width, topHeight, m.focus != focusRail && !confirmationOpen), m.box(detail, width, detailHeight, false))
}

func (m model) renderPlanItemDetail(item reconcile.PlanItem) string {
	return fmt.Sprintf("Target      %s / %s\nIssue       %s\nProfile     %s\nPlan status %s\nComparison  %s\nExecution   %s\nRows        local %d · remote %d · matched %s\nChanges     create %s · delete %s\nReason      %s\n\n%s",
		item.TargetAdapterFamily, item.TargetAdapterInstance, item.TargetIssue, emptyDash(item.RouteProfile), m.paintState(item.PlanStatus),
		m.paintState(item.ComparisonStatus), m.paintState(item.ExecutionState), item.LocalRowCount, item.RemoteRowCount, planDiffMetric(item, item.InspectionSummary.MatchedRowCount),
		planDiffMetric(item, item.InspectionSummary.CreateRowCount), planDiffMetric(item, item.InspectionSummary.DeleteRowCount), emptyDash(item.ReasonCode), emptyDash(item.ReasonDetail))
}

func planDiffMetric(item reconcile.PlanItem, value int) string {
	if !item.HasDiffMetrics() {
		return "-"
	}
	return fmt.Sprint(value)
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
