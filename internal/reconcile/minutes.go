package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/solitus0/workledger/internal/reconcile/model"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

type pullMinuteCandidate struct {
	scope int
	raw   model.Row
	row   model.Row
}

// Normalize only pull candidates. Remote totals and cleanup comparisons keep
// the observed timing, so normalization cannot hide remote discrepancies.
func (s *Service) normalizePullPlan(ctx context.Context, plan *Plan) error {
	candidates := make([]pullMinuteCandidate, 0)
	for i := range plan.Items {
		item := &plan.Items[i]
		if item.PlanStatus != "ready" && item.PlanStatus != "skipped" {
			continue
		}
		payload := make([]model.Row, 0, len(item.Payload))
		changed := false
		for _, raw := range item.Payload {
			start, seconds, err := worklogs.NormalizeTiming(raw.StartedAtUTC, raw.DurationSeconds)
			reason := "invalid_minute_duration"
			if err == nil && !start.Equal(raw.StartedAtUTC) && (start.Before(item.WindowFromUTC) || start.After(item.WindowToUTC)) {
				err = fmt.Errorf("rounded start %s falls outside the selected window; select a wider window", start.Format(time.RFC3339))
				reason = "rounded_start_outside_window"
			}
			if err != nil {
				detail := fmt.Sprintf("remote row %s started %s, duration %ds: %s", raw.SourceRowID, raw.StartedAtUTC.Format(time.RFC3339Nano), raw.DurationSeconds, err)
				invalid := model.InvalidRow{SourceRowID: raw.SourceRowID, IssueKey: raw.IssueKey, Description: raw.Description, ReasonCode: reason, ReasonDetail: detail}
				plan.Findings = append(plan.Findings, PlanFinding{ID: uuid.NewString(), PlanID: plan.ID, SourceRowID: raw.SourceRowID, ReasonCode: reason, ReasonDetail: detail, Payload: invalid})
				item.PlanStatus, item.PlannedAction, item.ReasonCode, item.ReasonDetail = "invalid", "none", reason, detail
				continue
			}
			row := raw
			row.StartedAtUTC, row.DurationSeconds = start, seconds
			changed = changed || !start.Equal(raw.StartedAtUTC) || seconds != raw.DurationSeconds
			payload = append(payload, row)
			candidates = append(candidates, pullMinuteCandidate{scope: i, raw: raw, row: row})
		}
		item.Payload = sortRows(payload)
		if item.PlanStatus != "invalid" {
			local, err := s.listLocalScope(item.IssueKey, item.WindowFromUTC, item.WindowToUTC)
			if err != nil {
				return err
			}
			applyInspectionRowDiffSummary(item, item.Payload, localToRows(local))
			if rowsEqual(item.Payload, localToRows(local)) {
				item.PlanStatus, item.PlannedAction, item.ComparisonStatus = "skipped", "none", "match"
				item.ReasonCode, item.ReasonDetail = "exact_match", "Normalized remote rows already match the local ledger"
			} else {
				item.PlanStatus, item.PlannedAction, item.ComparisonStatus = "ready", "merge", "merge_needed"
				if changed {
					item.ReasonCode = "minute_normalized"
					item.ReasonDetail = "Remote timestamps and durations were rounded to minutes for the saved local merge"
				}
			}
		}
	}

	for i, a := range candidates {
		if plan.Items[a.scope].PlanStatus == "invalid" {
			continue
		}
		for _, b := range candidates[:i] {
			if plan.Items[b.scope].PlanStatus == "invalid" {
				continue
			}
			if introducedMinuteConflict(a.raw, b.raw, a.row, b.row) {
				blockMinuteScope(&plan.Items[a.scope])
				blockMinuteScope(&plan.Items[b.scope])
			}
		}
	}

	if len(candidates) > 0 {
		start, end := candidates[0].row.StartedAtUTC, rowEnd(candidates[0].row)
		for _, candidate := range candidates[1:] {
			if candidate.row.StartedAtUTC.Before(start) {
				start = candidate.row.StartedAtUTC
			}
			if rowEnd(candidate.row).After(end) {
				end = rowEnd(candidate.row)
			}
		}
		neighbors, err := s.pullMinuteNeighbors(ctx, start, end)
		if err != nil {
			return err
		}
		// Blocking one scope retains its local rows, which can in turn block
		// another scope. Recheck until the complete candidate set is stable.
		for blocked := true; blocked; {
			blocked = false
			for _, candidate := range candidates {
				if plan.Items[candidate.scope].PlanStatus != "ready" {
					continue
				}
				for _, neighbor := range neighbors {
					if !pullPlanReplacesRow(plan, neighbor) && introducedMinuteConflict(candidate.raw, neighbor, candidate.row, neighbor) {
						blockMinuteScope(&plan.Items[candidate.scope])
						blocked = true
						break
					}
				}
			}
		}
	}
	for i := range plan.Items {
		plan.Items[i].DeliveryKey = buildDeliveryKey(plan.Items[i])
	}
	plan.PlanningStatus = derivePlanningStatus(plan.Items, plan.Findings)
	return nil
}

func rowEnd(row model.Row) time.Time {
	return row.StartedAtUTC.Add(time.Duration(row.DurationSeconds) * time.Second)
}

func rowIntervalsOverlap(a, b model.Row) bool {
	return a.StartedAtUTC.Before(rowEnd(b)) && b.StartedAtUTC.Before(rowEnd(a))
}

func introducedMinuteConflict(rawA, rawB, a, b model.Row) bool {
	return (pushRowKey(a) == pushRowKey(b) && pushRowKey(rawA) != pushRowKey(rawB)) ||
		(rowIntervalsOverlap(a, b) && !rowIntervalsOverlap(rawA, rawB))
}

func blockMinuteScope(item *PlanItem) {
	item.PlanStatus, item.PlannedAction = "blocked", "none"
	item.ReasonCode, item.ReasonDetail = "minute_rounding_conflict", "Rounding to minutes introduces a duplicate or overlap; correct the source timing before applying"
}

func pullPlanReplacesRow(plan *Plan, row model.Row) bool {
	for _, item := range plan.Items {
		if (item.PlanStatus == "ready" || item.PlanStatus == "skipped") && item.IssueKey == row.IssueKey && !row.StartedAtUTC.Before(item.WindowFromUTC) && !row.StartedAtUTC.After(item.WindowToUTC) {
			return true
		}
	}
	return false
}

func (s *Service) pullMinuteNeighbors(ctx context.Context, start, end time.Time) ([]model.Row, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT issue_key, started_at_utc, duration_seconds, description FROM worklogs WHERE started_at_utc < ? AND unixepoch(started_at_utc) + duration_seconds > unixepoch(?)`, sqlitestore.RFC3339UTC(end), sqlitestore.RFC3339UTC(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Row
	for rows.Next() {
		var row model.Row
		var started string
		if err := rows.Scan(&row.IssueKey, &started, &row.DurationSeconds, &row.Description); err != nil {
			return nil, err
		}
		row.StartedAtUTC, err = time.Parse(time.RFC3339, started)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
