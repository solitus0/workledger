package worklogs

import (
	"context"
	"fmt"
	"strings"
	"time"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

type IssueFindMode string

const (
	IssueFindByPrefix      IssueFindMode = "prefix"
	IssueFindByDescription IssueFindMode = "description"
)

// UsedIssue is one issue key evidenced by an active local worklog.
type UsedIssue struct {
	IssueKey      string
	LastEnteredAt time.Time
}

// ListUsedIssues returns active local issue keys in latest-entry order. When
// finding by description, a matching worklog admits its issue, but all active
// worklogs for that issue determine its latest-entry rank.
func (s *Service) ListUsedIssues(ctx context.Context, mode IssueFindMode, query string, limit, offset int) ([]UsedIssue, bool, error) {
	if limit <= 0 || offset < 0 {
		return []UsedIssue{}, false, nil
	}
	var where string
	var args []any
	switch mode {
	case IssueFindByPrefix:
		lower, upper := sqlitestore.PrefixRange(strings.ToUpper(strings.TrimSpace(query)))
		where, args = `issue_key >= ? AND issue_key < ?`, []any{lower, upper}
	case IssueFindByDescription:
		trimmed := strings.TrimSpace(query)
		if trimmed == "" {
			where = `1 = 1`
		} else {
			where, args = `issue_key IN (SELECT issue_key FROM worklogs WHERE description LIKE ? ESCAPE '\' COLLATE NOCASE)`, []any{literalSubstringPattern(trimmed)}
		}
	default:
		return nil, false, fmt.Errorf("unsupported issue find mode %q", mode)
	}
	args = append(args, limit+1, offset)
	rows, err := s.store.DB().QueryContext(ctx, `
		SELECT issue_key, MAX(created_at) AS last_entered_at
		FROM worklogs WHERE `+where+`
		GROUP BY issue_key
		ORDER BY last_entered_at DESC, issue_key
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]UsedIssue, 0, limit)
	for rows.Next() {
		var item UsedIssue
		var enteredAt string
		if err := rows.Scan(&item.IssueKey, &enteredAt); err != nil {
			return nil, false, err
		}
		item.LastEnteredAt, err = time.Parse(time.RFC3339, enteredAt)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

type IssueWorklogDescription struct {
	StartedAtUTC time.Time
	Description  string
}

// ListIssueWorklogDescriptions returns every active worklog description for an
// exact issue key, preserving repeated descriptions in latest-entry order.
func (s *Service) ListIssueWorklogDescriptions(ctx context.Context, issueKey string, limit, offset int) ([]IssueWorklogDescription, bool, error) {
	if limit <= 0 || offset < 0 || strings.TrimSpace(issueKey) == "" {
		return []IssueWorklogDescription{}, false, nil
	}
	rows, err := s.store.DB().QueryContext(ctx, `
		SELECT started_at_utc, description
		FROM worklogs WHERE issue_key = ?
		ORDER BY created_at DESC, id
		LIMIT ? OFFSET ?`, strings.ToUpper(strings.TrimSpace(issueKey)), limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]IssueWorklogDescription, 0, limit)
	for rows.Next() {
		var item IssueWorklogDescription
		var startedAt string
		if err := rows.Scan(&startedAt, &item.Description); err != nil {
			return nil, false, err
		}
		item.StartedAtUTC, err = time.Parse(time.RFC3339, startedAt)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

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

func (s *Service) ListTrashByIDPrefix(prefix string, limit int) ([]TrashRecord, error) {
	if limit <= 0 {
		return []TrashRecord{}, nil
	}
	lower, upper := sqlitestore.PrefixRange(strings.ToLower(strings.TrimSpace(prefix)))
	rows, err := s.store.DB().Query(`SELECT `+trashSelectColumns+` FROM trashed_worklogs WHERE id >= ? AND id < ? ORDER BY trashed_at DESC, id LIMIT ?`, lower, upper, limit)
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
