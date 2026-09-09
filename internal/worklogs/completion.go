package worklogs

import (
	"context"
	"strings"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

// ListActiveByIDPrefix returns recent active worklogs whose IDs start with the
// supplied prefix. The result is bounded for latency-sensitive advisory uses.
func (s *Service) ListActiveByIDPrefix(prefix string, limit int) ([]LocalWorklog, error) {
	if limit <= 0 {
		return []LocalWorklog{}, nil
	}

	lower, upper := sqlitestore.PrefixRange(strings.ToLower(strings.TrimSpace(prefix)))
	rows, err := s.store.DB().Query(
		`SELECT id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at, revision
		 FROM worklogs
		 WHERE id >= ? AND id < ?
		 ORDER BY updated_at DESC, id
		 LIMIT ?`,
		lower,
		upper,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]LocalWorklog, 0)
	for rows.Next() {
		item, err := scanWorklog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListRestorableTrashByIDPrefix(prefix string, limit int) ([]TrashRecord, error) {
	if limit <= 0 {
		return []TrashRecord{}, nil
	}
	lower, upper := sqlitestore.PrefixRange(strings.ToLower(strings.TrimSpace(prefix)))
	rows, err := s.store.DB().Query(`SELECT `+trashSelectColumns+` FROM trashed_worklogs WHERE storage_scope = ? AND source_worklog_id IS NOT NULL AND id >= ? AND id < ? ORDER BY trashed_at DESC, id LIMIT ?`, TrashScopeLocal, lower, upper, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TrashRecord, 0)
	for rows.Next() {
		item, err := scanTrashRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListKnownIssueKeys returns locally known issue keys from active worklogs and
// cached issue metadata. Results are case-insensitively prefix-filtered and
// ordered by recent local activity, with metadata-only issues last.
func (s *Service) ListKnownIssueKeys(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}

	trimmedPrefix := strings.ToUpper(strings.TrimSpace(prefix))
	lower, upper := sqlitestore.PrefixRange(trimmedPrefix)
	rows, err := s.store.DB().QueryContext(ctx,
		`WITH candidates AS (
			SELECT issue_key, MAX(updated_at) AS recent_at, 0 AS metadata_only
			FROM worklogs
			WHERE issue_key >= ? AND issue_key < ?
			GROUP BY issue_key
			UNION ALL
			SELECT metadata.issue_key, metadata.refreshed_at AS recent_at, 1 AS metadata_only
			FROM issue_metadata AS metadata
			WHERE metadata.issue_key >= ? AND metadata.issue_key < ?
			  AND NOT EXISTS (
				SELECT 1 FROM worklogs WHERE worklogs.issue_key = metadata.issue_key
			  )
		 )
		 SELECT issue_key
		 FROM candidates
		 ORDER BY metadata_only, recent_at DESC, issue_key COLLATE NOCASE, issue_key
		 LIMIT ?`,
		lower,
		upper,
		lower,
		upper,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var issueKey string
		if err := rows.Scan(&issueKey); err != nil {
			return nil, err
		}
		items = append(items, issueKey)
	}
	return items, rows.Err()
}

// ListRecentDescriptions returns distinct descriptions recently used by one
// active local issue. The result is bounded for interactive completion.
func (s *Service) ListRecentDescriptions(ctx context.Context, issueKey string, limit int) ([]string, error) {
	if limit <= 0 || strings.TrimSpace(issueKey) == "" {
		return []string{}, nil
	}

	rows, err := s.store.DB().QueryContext(ctx,
		`SELECT description
		 FROM worklogs
		 WHERE issue_key = ?
		 GROUP BY description
		 ORDER BY MAX(updated_at) DESC, description COLLATE NOCASE, description
		 LIMIT ?`,
		strings.ToUpper(strings.TrimSpace(issueKey)),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var description string
		if err := rows.Scan(&description); err != nil {
			return nil, err
		}
		items = append(items, description)
	}
	return items, rows.Err()
}
