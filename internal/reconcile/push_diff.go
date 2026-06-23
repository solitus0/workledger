package reconcile

import (
	"strconv"
	"time"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/reconcile/model"
	"github.com/solitus0/workledger/internal/worklogs"
)

type scopedRemoteRow[T any] struct {
	raw T
	row model.Row
}

func diffScopedRemoteRows[T any](remoteRows []scopedRemoteRow[T], desiredRows []model.Row) ([]scopedRemoteRow[T], []model.Row) {
	remainingDesired := make(map[string][]model.Row, len(desiredRows))
	for _, row := range desiredRows {
		key := pushRowKey(row)
		remainingDesired[key] = append(remainingDesired[key], row)
	}

	deleteRows := make([]scopedRemoteRow[T], 0)
	for _, remote := range remoteRows {
		key := pushRowKey(remote.row)
		candidates := remainingDesired[key]
		if len(candidates) > 0 {
			remainingDesired[key] = candidates[1:]
			continue
		}
		deleteRows = append(deleteRows, remote)
	}

	createRows := make([]model.Row, 0)
	for _, rows := range remainingDesired {
		createRows = append(createRows, rows...)
	}

	return deleteRows, sortRows(createRows)
}

func pushRowKey(row model.Row) string {
	return row.IssueKey + "\x00" + row.StartedAtUTC.UTC().Format(time.RFC3339) + "\x00" + strconv.Itoa(row.DurationSeconds) + "\x00" + row.Description
}

func scopedRemoteRowValues[T any](rows []scopedRemoteRow[T]) []model.Row {
	items := make([]model.Row, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.row)
	}
	return sortRows(items)
}

func scopedRemoteRawValues[T any](rows []scopedRemoteRow[T]) []T {
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.raw)
	}
	return items
}

func buildClockifyScopedRows(entries []clockify.TimeEntry, tagsByID map[string]clockify.Tag, fallbackIssueKey string) []scopedRemoteRow[clockify.TimeEntry] {
	rows := make([]scopedRemoteRow[clockify.TimeEntry], 0, len(entries))
	for _, entry := range entries {
		row, ok := buildClockifyScopedRow(entry, tagsByID, fallbackIssueKey)
		if !ok {
			continue
		}
		rows = append(rows, scopedRemoteRow[clockify.TimeEntry]{raw: entry, row: row})
	}
	return rows
}

func buildClockifyScopedRow(entry clockify.TimeEntry, tagsByID map[string]clockify.Tag, fallbackIssueKey string) (model.Row, bool) {
	valid, _ := clockify.NormalizeEntries([]clockify.TimeEntry{entry}, tagsByID)
	if len(valid) == 1 {
		row := valid[0]
		if row.IssueKey == "" {
			row.IssueKey = fallbackIssueKey
		}
		return row, true
	}
	return fallbackClockifyTrashRow(entry, fallbackIssueKey)
}

func buildJiraDataScopedRows(issueKey string, scope []jiradatacenter.Worklog) []scopedRemoteRow[jiradatacenter.Worklog] {
	rows := make([]scopedRemoteRow[jiradatacenter.Worklog], 0, len(scope))
	for _, item := range scope {
		row, ok := buildJiraDataScopedRow(issueKey, item)
		if !ok {
			continue
		}
		rows = append(rows, scopedRemoteRow[jiradatacenter.Worklog]{raw: item, row: row})
	}
	return rows
}

func buildJiraDataScopedRow(issueKey string, item jiradatacenter.Worklog) (model.Row, bool) {
	startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
	if err != nil {
		return model.Row{}, false
	}
	description, normalizeErr := worklogs.NormalizeDescription(remoteCommentText(item.Comment))
	if normalizeErr != nil {
		description = remoteCommentText(item.Comment)
	}
	return model.Row{
		IssueKey:        issueKey,
		StartedAtUTC:    startedAt.UTC(),
		DurationSeconds: item.TimeSpentSeconds,
		Description:     description,
		SourceRowID:     item.ID,
	}, true
}

func buildJiraCloudScopedRows(issueKey string, scope []jiracloud.Worklog) []scopedRemoteRow[jiracloud.Worklog] {
	rows := make([]scopedRemoteRow[jiracloud.Worklog], 0, len(scope))
	for _, item := range scope {
		row, ok := buildJiraCloudScopedRow(issueKey, item)
		if !ok {
			continue
		}
		rows = append(rows, scopedRemoteRow[jiracloud.Worklog]{raw: item, row: row})
	}
	return rows
}

func buildJiraCloudScopedRow(issueKey string, item jiracloud.Worklog) (model.Row, bool) {
	startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
	if err != nil {
		return model.Row{}, false
	}
	description, normalizeErr := worklogs.NormalizeDescription(remoteCommentText(item.Comment))
	if normalizeErr != nil {
		description = remoteCommentText(item.Comment)
	}
	return model.Row{
		IssueKey:        issueKey,
		StartedAtUTC:    startedAt.UTC(),
		DurationSeconds: item.TimeSpentSeconds,
		Description:     description,
		SourceRowID:     item.ID,
	}, true
}
