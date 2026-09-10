package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	"github.com/solitus0/workledger/internal/worklogs"
)

const (
	activityDrawerHeight   = 7
	activityDrawerListRows = 3
)

var visibleActivityOperations = map[string]struct{}{
	"plan.apply":           {},
	"plan.reconcile":       {},
	"plan.retry":           {},
	"plan.retry-failed":    {},
	"plan.retry-uncertain": {},
	"presets.add":          {},
	"presets.apply":        {},
	"presets.create":       {},
	"presets.delete":       {},
	"presets.update":       {},
	"trash.restore":        {},
	"trash.restore-day":    {},
	"worklogs.add":         {},
	"worklogs.apply":       {},
	"worklogs.delete":      {},
	"worklogs.delete-day":  {},
	"worklogs.shift":       {},
	"worklogs.update":      {},
}

func (m *model) beginActivity(operation, summary string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil || m.workspace == nil || m.workspace.activities == nil {
		return cmd
	}
	entry := activity.Entry{ID: uuid.NewString(), Source: activity.SourceTUI, Operation: operation, Summary: strings.TrimSpace(summary), Attributes: map[string]string{}, State: activity.StateRunning, StartedAt: m.deps.now().UTC()}
	m.upsertActivity(entry)
	service, ctx := m.workspace.activities, m.ctx
	return func() tea.Msg {
		_, startErr := service.Start(ctx, activity.StartInput{ID: entry.ID, Source: entry.Source, Operation: entry.Operation, Summary: entry.Summary, StartedAt: entry.StartedAt})
		inner := cmd()
		state, code, message := activityOutcome(inner)
		finished := m.deps.now().UTC()
		entry.State = state
		entry.FinishedAt = &finished
		duration := finished.Sub(entry.StartedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		entry.DurationMS = &duration
		entry.ErrorCode, entry.ErrorMessage = code, message
		if startErr == nil {
			if saved, err := service.Finish(ctx, entry.ID, activity.FinishInput{State: state, FinishedAt: finished, ErrorCode: code, ErrorMessage: message}); err == nil {
				entry = saved
			}
		}
		return activityResultMsg{entry: entry, inner: inner}
	}
}

func activityOutcome(msg tea.Msg) (activity.State, string, string) {
	result, ok := msg.(trackedOperationResult)
	if !ok {
		return activity.StateFailed, "unexpected_result", "operation returned an unsupported result"
	}
	switch result.trackedOutcome() {
	case trackedOutcomeSuccess:
		return activity.StateSucceeded, "", ""
	case trackedOutcomePartial:
		return activity.StatePartial, "partial", "operation completed with partial results"
	case trackedOutcomeCanceled:
		return activity.StateCanceled, "canceled", "operation canceled"
	case trackedOutcomeConflict:
		return activity.StateFailed, "conflict", "operation conflicted with current state"
	case trackedOutcomeValidation:
		return activity.StateFailed, "validation_error", "validation failed"
	default:
		return activity.StateFailed, "unexpected_error", "operation failed"
	}
}

type trackedOutcomeKind int

const (
	trackedOutcomeSuccess trackedOutcomeKind = iota
	trackedOutcomePartial
	trackedOutcomeCanceled
	trackedOutcomeConflict
	trackedOutcomeValidation
	trackedOutcomeUnexpected
)

type trackedOperationResult interface {
	trackedOutcome() trackedOutcomeKind
}

func classifyTrackedOutcome(err error, partial bool) trackedOutcomeKind {
	if err == nil {
		if partial {
			return trackedOutcomePartial
		}
		return trackedOutcomeSuccess
	}
	if errors.Is(err, context.Canceled) {
		return trackedOutcomeCanceled
	}
	if errors.Is(err, worklogs.ErrConflict) || errors.Is(err, worklogs.ErrPlacementChanged) || errors.Is(err, presets.ErrConflict) {
		return trackedOutcomeConflict
	}
	if errors.Is(err, worklogs.ErrValidation) || errors.Is(err, presets.ErrValidation) {
		return trackedOutcomeValidation
	}
	var reconcileValidation reconcile.ValidationError
	var reconcileSelection reconcile.SelectionError
	if errors.As(err, &reconcileValidation) || errors.As(err, &reconcileSelection) {
		return trackedOutcomeValidation
	}
	return trackedOutcomeUnexpected
}

func (msg statusResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg worklogResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg trashResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg presetResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg mutationResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg presetMutationResultMsg) trackedOutcome() trackedOutcomeKind {
	return classifyTrackedOutcome(msg.err, false)
}
func (msg planOperationResultMsg) trackedOutcome() trackedOutcomeKind {
	partial := msg.apply.MixedResult || len(msg.reconcile.SkippedTargets) > 0 || planHasCheckFailures(msg.reconcile.Plan)
	return classifyTrackedOutcome(msg.err, partial)
}

func planHasCheckFailures(plan *reconcile.Plan) bool {
	if plan == nil {
		return false
	}
	for _, item := range plan.Items {
		if item.PlanStatus == "check_failed" {
			return true
		}
	}
	return false
}

func (m *model) startActivityLoad() tea.Cmd {
	if m.workspace == nil || m.workspace.activities == nil {
		return nil
	}
	m.activityGen++
	m.activityLoading = true
	return m.activityListCmd(m.activityGen)
}

func (m model) activityListCmd(generation int) tea.Cmd {
	if m.workspace == nil || m.workspace.activities == nil {
		return nil
	}
	service, ctx := m.workspace.activities, m.ctx
	return func() tea.Msg {
		items, err := service.List(ctx, activity.ListFilters{Limit: activity.RetentionLimit})
		return activityListResultMsg{generation: generation, items: items, err: err}
	}
}

func (m model) activityVisible() bool {
	return m.activityOpen && m.interactionState.kind() == interactionIdle
}

func (m model) updateActivity(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.overlay = helpOverlay
		m.focus = focusWorkspace
	case "up", "k":
		m.activitySelected = max(0, m.activitySelected-1)
	case "down", "j":
		m.activitySelected = min(max(0, len(m.activities)-1), m.activitySelected+1)
	case "pgup":
		m.activitySelected = max(0, m.activitySelected-activityDrawerListRows)
	case "pgdown":
		m.activitySelected = min(max(0, len(m.activities)-1), m.activitySelected+activityDrawerListRows)
	case "home":
		m.activitySelected = max(0, len(m.activities)-1)
	case "end":
		m.activitySelected = 0
	case "r":
		return m, m.startActivityLoad()
	case "enter", " ":
		m.focus = focusWorkspace
	}
	return m, nil
}

func (m *model) upsertActivity(entry activity.Entry) {
	if !activityVisibleInDrawer(entry) {
		return
	}
	selectedID := m.selectedActivityID()
	wasNewest := m.activitySelected == 0
	for index := range m.activities {
		if m.activities[index].ID == entry.ID {
			m.activities[index] = entry
			return
		}
	}
	m.activities = append([]activity.Entry{entry}, m.activities...)
	if len(m.activities) > activity.RetentionLimit {
		m.activities = m.activities[:activity.RetentionLimit]
	}
	if wasNewest {
		m.activitySelected = 0
	} else {
		m.restoreActivitySelection(selectedID)
	}
}

func activityVisibleInDrawer(entry activity.Entry) bool {
	_, ok := visibleActivityOperations[entry.Operation]
	return ok
}

func drawerActivities(entries []activity.Entry) []activity.Entry {
	visible := make([]activity.Entry, 0, len(entries))
	for _, entry := range entries {
		if activityVisibleInDrawer(entry) {
			visible = append(visible, entry)
		}
	}
	return visible
}

func (m model) selectedActivityID() string {
	if m.activitySelected >= 0 && m.activitySelected < len(m.activities) {
		return m.activities[m.activitySelected].ID
	}
	return ""
}

func (m *model) restoreActivitySelection(id string) {
	for index := range m.activities {
		if m.activities[index].ID == id {
			m.activitySelected = index
			return
		}
	}
	m.activitySelected = clampIndex(m.activitySelected, len(m.activities))
}

func tuiActivitySummary(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, " · ")
}

func durationSummary(value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("duration=%s", normalizeTUIDuration(value))
}

func activityElapsed(item activity.Entry) string {
	if item.DurationMS == nil {
		return "running"
	}
	return (time.Duration(*item.DurationMS) * time.Millisecond).Round(time.Millisecond).String()
}
