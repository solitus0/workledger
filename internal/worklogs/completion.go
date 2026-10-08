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
	IssueFindByTitle       IssueFindMode = "title"
)

// UsedIssue is a known local issue, with optional worklog history and metadata.
type UsedIssue struct {
	IssueKey           string
	LastEnteredAt      time.Time
	Title              string
	MaxEstimateSeconds *int64
}

// ListUsedIssues returns the local issue catalog, with used issues first by
// latest entry and unused metadata by latest update. History search never uses trash.
func (s *Service) ListUsedIssues(ctx context.Context, mode IssueFindMode, query string, limit, offset int) ([]UsedIssue, bool, error) {
	if limit <= 0 || offset < 0 {
		return []UsedIssue{}, false, nil
	}
	var where string
	var args []any
	switch mode {
	case IssueFindByPrefix:
		lower, upper := sqlitestore.PrefixRange(strings.ToUpper(strings.TrimSpace(query)))
		where, args = `k.issue_key >= ? AND k.issue_key < ?`, []any{lower, upper}
	case IssueFindByDescription:
		if strings.TrimSpace(query) == "" {
			where = `1 = 1`
		} else {
			where, args = `EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_key = k.issue_key AND w.description LIKE ? ESCAPE '\' COLLATE NOCASE)`, []any{literalSubstringPattern(strings.TrimSpace(query))}
		}
	case IssueFindByTitle:
		if strings.TrimSpace(query) == "" {
			where = `1 = 1`
		} else {
			where, args = `m.title LIKE ? ESCAPE '\' COLLATE NOCASE`, []any{literalSubstringPattern(strings.TrimSpace(query))}
		}
	default:
		return nil, false, fmt.Errorf("unsupported issue find mode %q", mode)
	}
	args = append(args, limit+1, offset)
	rows, err := s.store.DB().QueryContext(ctx, issueCatalogSQL+`, known_keys AS (
        SELECT issue_key FROM worklogs UNION SELECT issue_key FROM metadata
    ), used AS (SELECT issue_key, MAX(created_at) AS last_entered_at FROM worklogs GROUP BY issue_key)
    SELECT k.issue_key, COALESCE(u.last_entered_at, ''), COALESCE(m.title, ''),
        COALESCE(m.manual_estimate, CASE WHEN m.cached_estimate > 0 THEN m.cached_estimate END)
    FROM known_keys k LEFT JOIN used u ON u.issue_key = k.issue_key
    LEFT JOIN metadata m ON m.issue_key = k.issue_key WHERE `+where+`
    ORDER BY u.last_entered_at IS NULL, u.last_entered_at DESC,
        CASE WHEN u.last_entered_at IS NULL THEN m.recent_at END DESC, k.issue_key
    LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]UsedIssue, 0, limit)
	for rows.Next() {
		var item UsedIssue
		var enteredAt string
		var estimate *int64
		if err := rows.Scan(&item.IssueKey, &enteredAt, &item.Title, &estimate); err != nil {
			return nil, false, err
		}
		item.MaxEstimateSeconds = estimate
		if enteredAt != "" {
			item.LastEnteredAt, err = time.Parse(time.RFC3339, enteredAt)
			if err != nil {
				return nil, false, err
			}
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
	StartedAtUTC    time.Time
	DurationSeconds int
	Description     string
}

// ListIssueWorklogDescriptions returns every active worklog description for an
// exact issue key, preserving repeated descriptions in latest-entry order.
func (s *Service) ListIssueWorklogDescriptions(ctx context.Context, issueKey string, limit, offset int) ([]IssueWorklogDescription, bool, error) {
	if limit <= 0 || offset < 0 || strings.TrimSpace(issueKey) == "" {
		return []IssueWorklogDescription{}, false, nil
	}
	rows, err := s.store.DB().QueryContext(ctx, `
		SELECT started_at_utc, duration_seconds, description
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
		if err := rows.Scan(&startedAt, &item.DurationSeconds, &item.Description); err != nil {
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

// ListKnownIssueKeys shares the picker catalog and its entry ordering.
func (s *Service) ListKnownIssueKeys(ctx context.Context, prefix string, limit int) ([]string, error) {
	issues, _, err := s.ListUsedIssues(ctx, IssueFindByPrefix, prefix, limit, 0)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(issues))
	for _, issue := range issues {
		keys = append(keys, issue.IssueKey)
	}
	return keys, nil
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
