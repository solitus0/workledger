package sqlite

import (
	"context"
	"database/sql"
	"time"
)

type IssueCacheEntry struct {
	IssueKey              string
	Title                 string
	MaxEstimateSeconds    *int64
	SourceAdapterFamily   string
	SourceAdapterInstance string
	RefreshedAt           time.Time
}

// UpsertIssueCache commits a batch and returns the written entries in input order.
// A missing title preserves the existing title. Explicit refreshes may update the
// refresh timestamp even when the cached fields match.
func (s *Store) UpsertIssueCache(ctx context.Context, entries []IssueCacheEntry, refreshUnchanged bool) ([]IssueCacheEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO issue_metadata(issue_key, title, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at)
		 VALUES(?, NULLIF(?, ''), ?, ?, ?, ?)
		 ON CONFLICT(issue_key) DO UPDATE SET
		   title = COALESCE(excluded.title, issue_metadata.title),
		   max_estimate_seconds = excluded.max_estimate_seconds,
		   source_adapter_family = excluded.source_adapter_family,
		   source_adapter_instance = excluded.source_adapter_instance,
		   refreshed_at = excluded.refreshed_at
		 WHERE ? OR issue_metadata.title IS NOT COALESCE(excluded.title, issue_metadata.title)
		    OR issue_metadata.max_estimate_seconds IS NOT excluded.max_estimate_seconds
		    OR issue_metadata.source_adapter_family IS NOT excluded.source_adapter_family
		    OR issue_metadata.source_adapter_instance IS NOT excluded.source_adapter_instance
		 RETURNING COALESCE(title, '')`)
	if err != nil {
		return nil, err
	}
	defer statement.Close()
	var written []IssueCacheEntry
	for _, entry := range entries {
		err := statement.QueryRowContext(ctx,
			entry.IssueKey,
			entry.Title,
			entry.MaxEstimateSeconds,
			entry.SourceAdapterFamily,
			entry.SourceAdapterInstance,
			entry.RefreshedAt.UTC().Format(time.RFC3339),
			refreshUnchanged,
		).Scan(&entry.Title)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		written = append(written, entry)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return written, nil
}
