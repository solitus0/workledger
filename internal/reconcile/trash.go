package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/reconcile/model"
	"github.com/solitus0/workledger/internal/worklogs"
)

const (
	pullTrashReasonCode   = "pull_merge_removed_local"
	pullTrashReasonDetail = "Removed from local ledger during pull plan apply"
	pushTrashReasonCode   = "push_cleanup_deleted_remote"
	pushTrashReasonDetail = "Deleted from remote target during push plan apply cleanup"
)

type applyItemExecution struct {
	executed           bool
	failed             bool
	applyMessage       string
	trashArchivedCount int
	warnings           []string
}

type pushApplyResult struct {
	trashArchivedCount int
	deletedRows        []model.Row
	warnings           []string
}

func (s *Service) buildPullMergeExecution(item PlanItem) (preserved []worklogs.LocalWorklog, removed []worklogs.LocalWorklog, inserted []worklogs.LocalWorklog, err error) {
	localRows, err := s.listLocalScope(item.IssueKey, item.WindowFromUTC, item.WindowToUTC)
	if err != nil {
		return nil, nil, nil, err
	}

	localBuckets := make(map[string][]worklogs.LocalWorklog, len(localRows))
	for _, row := range localRows {
		key := pullRowKey(row.IssueKey, row.StartedAtUTC, row.DurationSeconds, row.Description)
		localBuckets[key] = append(localBuckets[key], row)
	}

	for _, row := range item.Payload {
		key := pullRowKey(row.IssueKey, row.StartedAtUTC, row.DurationSeconds, row.Description)
		candidates := localBuckets[key]
		if len(candidates) > 0 {
			preserved = append(preserved, candidates[0])
			localBuckets[key] = candidates[1:]
			continue
		}
		inserted = append(inserted, worklogs.LocalWorklog{
			IssueKey:        row.IssueKey,
			StartedAtUTC:    row.StartedAtUTC.UTC(),
			DurationSeconds: row.DurationSeconds,
			Description:     row.Description,
		})
	}

	for _, rows := range localBuckets {
		removed = append(removed, rows...)
	}

	return preserved, removed, inserted, nil
}

func pullRowKey(issueKey string, startedAt time.Time, durationSeconds int, description string) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", issueKey, startedAt.UTC().Format(time.RFC3339), durationSeconds, description)
}

func (s *Service) archiveRemoteTrashRows(item PlanItem, rows []model.Row) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	inputs := make([]worklogs.TrashArchiveInput, 0, len(rows))
	trashedAt := s.now().UTC()
	for _, row := range rows {
		inputs = append(inputs, worklogs.TrashArchiveInput{
			StorageScope:    worklogs.TrashScopeRemote,
			IssueKey:        row.IssueKey,
			StartedAtUTC:    row.StartedAtUTC.UTC(),
			DurationSeconds: row.DurationSeconds,
			Description:     row.Description,
			TrashedAt:       trashedAt,
			ReasonCode:      pushTrashReasonCode,
			ReasonDetail:    pushTrashReasonDetail,
			PlanDirection:   "push",
			PlanID:          item.PlanID,
			PlanItemID:      item.ID,
			AdapterFamily:   item.TargetAdapterFamily,
			AdapterInstance: item.TargetAdapterInstance,
		})
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	if err := worklogs.InsertTrashRowsTx(tx, inputs); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(inputs), nil
}

func archiveWarning(adapterFamily string, deletedCount int, err error) string {
	return fmt.Sprintf("%s remote cleanup deleted %d worklogs but trash archival failed: %v", adapterFamily, deletedCount, err)
}

func normalizeDeletedJiraDataRows(issueKey string, scope []jiradatacenter.Worklog) []model.Row {
	rows := make([]model.Row, 0, len(scope))
	for _, remote := range normalizeJiraDataScope(scope) {
		remote.IssueKey = issueKey
		rows = append(rows, remote)
	}
	return rows
}

func normalizeDeletedJiraCloudRows(issueKey string, scope []jiracloud.Worklog) []model.Row {
	rows := make([]model.Row, 0, len(scope))
	for _, remote := range normalizeJiraCloudScope(scope) {
		remote.IssueKey = issueKey
		rows = append(rows, remote)
	}
	return rows
}

func normalizeDeletedClockifyRows(entries []clockify.TimeEntry, tagsByID map[string]clockify.Tag, fallbackIssueKey string) []model.Row {
	valid, _ := clockify.NormalizeEntries(entries, tagsByID)
	rows := make([]model.Row, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, row := range valid {
		if row.IssueKey == "" {
			row.IssueKey = fallbackIssueKey
		}
		rows = append(rows, row)
		seen[row.SourceRowID] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		row, ok := fallbackClockifyTrashRow(entry, fallbackIssueKey)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return sortRows(rows)
}

func fallbackClockifyTrashRow(entry clockify.TimeEntry, issueKey string) (model.Row, bool) {
	if entry.TimeInterval.Start == "" || entry.TimeInterval.End == "" {
		return model.Row{}, false
	}
	startedAt, err := time.Parse(time.RFC3339, entry.TimeInterval.Start)
	if err != nil {
		return model.Row{}, false
	}
	endedAt, err := time.Parse(time.RFC3339, entry.TimeInterval.End)
	if err != nil || !endedAt.After(startedAt) {
		return model.Row{}, false
	}
	description, err := worklogs.NormalizeDescription(entry.Description)
	if err != nil {
		description = entry.Description
	}
	return model.Row{
		IssueKey:        issueKey,
		StartedAtUTC:    startedAt.UTC(),
		DurationSeconds: int(endedAt.Sub(startedAt).Seconds()),
		Description:     description,
		SourceRowID:     entry.ID,
		ProjectID:       entry.ProjectID,
	}, true
}

func deleteRemoteClockifyEntries(ctx context.Context, client clockifyClient, workspaceID string, entries []clockify.TimeEntry) error {
	for _, entry := range entries {
		if err := client.DeleteTimeEntry(ctx, workspaceID, entry.ID); err != nil {
			return err
		}
	}
	return nil
}

func deleteRemoteJiraDataWorklogs(ctx context.Context, client jiraDataClient, issueKey string, scope []jiradatacenter.Worklog) error {
	for _, remote := range scope {
		if err := client.DeleteWorklog(ctx, issueKey, remote.ID); err != nil {
			return err
		}
	}
	return nil
}

func deleteRemoteJiraCloudWorklogs(ctx context.Context, client jiraCloudClient, issueKey string, scope []jiracloud.Worklog) error {
	for _, remote := range scope {
		if err := client.DeleteWorklog(ctx, issueKey, remote.ID); err != nil {
			return err
		}
	}
	return nil
}
