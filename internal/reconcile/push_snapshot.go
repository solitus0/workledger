package reconcile

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/solitus0/workledger/internal/worklogs"
)

type PushSourceRevision struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// A present empty snapshot differs from an old plan without a snapshot.
// Prefixes capture reporting membership, including new source issues introduced
// after an initially empty reporting scope was planned.
type PushSourceSnapshot struct {
	IssueKeys     []string             `json:"issue_keys"`
	IssuePrefixes []string             `json:"issue_prefixes"`
	Rows          []PushSourceRevision `json:"rows"`
}

func (snapshot PushSourceSnapshot) selectRows(rows []worklogs.LocalWorklog) []PushSourceRevision {
	selected := make([]PushSourceRevision, 0)
	for _, row := range rows {
		if row.IssueKey == "" {
			continue
		}
		match := slices.Contains(snapshot.IssueKeys, row.IssueKey)
		for _, prefix := range snapshot.IssuePrefixes {
			match = match || strings.HasPrefix(row.IssueKey, prefix+"-")
		}
		if match {
			selected = append(selected, PushSourceRevision{ID: row.ID, Revision: row.Revision})
		}
	}
	slices.SortFunc(selected, func(a, b PushSourceRevision) int { return strings.Compare(a.ID, b.ID) })
	return selected
}

func sealPushSnapshot(plan *Plan, rows []worklogs.LocalWorklog, routes jiraRouteProfile) {
	for _, row := range rows {
		if row.IssueKey == "" {
			plan.SkippedUnassignedCount++
			plan.SkippedUnassignedSeconds += row.DurationSeconds
		}
	}
	for i := range plan.Items {
		item := &plan.Items[i]
		snapshot := &PushSourceSnapshot{IssueKeys: []string{}, IssuePrefixes: []string{}}
		for _, route := range routes.routes {
			if route.reporting && route.targetIssue == item.TargetIssue && route.targetInstance == item.TargetAdapterInstance {
				snapshot.IssuePrefixes = append(snapshot.IssuePrefixes, route.prefix)
			}
		}
		if len(snapshot.IssuePrefixes) == 0 {
			snapshot.IssueKeys = slices.Clone(item.InspectionSummary.SourceIssueKeys)
			if len(snapshot.IssueKeys) == 0 {
				snapshot.IssueKeys = []string{item.IssueKey}
			}
		}
		snapshot.Rows = snapshot.selectRows(rows)
		item.InspectionSummary.SourceSnapshot = snapshot
		item.InspectionSummary.SkippedUnassignedCount = plan.SkippedUnassignedCount
		item.InspectionSummary.SkippedUnassignedSeconds = plan.SkippedUnassignedSeconds
	}
}

func (s *Service) validatePushSources(ctx context.Context, items []PlanItem) error {
	for _, item := range items {
		snapshot := item.InspectionSummary.SourceSnapshot
		if snapshot == nil {
			return fmt.Errorf("saved push plan lacks a source snapshot; run 'workledger plan reconcile' to generate a new plan")
		}
		rows, err := s.listActiveWindowContext(ctx, item.WindowFromUTC, item.WindowToUTC)
		if err != nil {
			return err
		}
		if !slices.Equal(snapshot.Rows, snapshot.selectRows(rows)) {
			return fmt.Errorf("local worklogs changed for %s; run 'workledger plan reconcile' to generate a new plan", planItemScopeLabel(item))
		}
	}
	return nil
}
