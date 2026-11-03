package worklogs

import (
	"context"
	"fmt"
	"strings"
	"time"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

type AssignmentExpectation struct {
	ID       string
	Revision int64
}

type AssignInput struct {
	IDs      []string
	IssueKey string
	Expected []AssignmentExpectation
}

type AssignResult struct {
	DryRun  bool
	Records []LocalWorklog
}

// LookupIDs returns existing records only, in requested order, including records
// outside a frontend's loaded date or filter. Queries stay below SQLite's limit.
func (s *Service) LookupIDs(ctx context.Context, ids []string) ([]LocalWorklog, error) {
	return lookupWorklogIDs(ctx, ids, s.store.DB())
}

func lookupWorklogIDs(ctx context.Context, ids []string, queryer sqlQueryer) ([]LocalWorklog, error) {
	found := make(map[string]LocalWorklog, len(ids))
	for start := 0; start < len(ids); start += sqliteQueryBatchSize {
		batch := ids[start:min(start+sqliteQueryBatchSize, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := queryer.QueryContext(ctx, `SELECT id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at, revision FROM worklogs WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			item, err := scanWorklog(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			found[item.ID] = item
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	items := make([]LocalWorklog, 0, len(found))
	for _, id := range ids {
		if item, ok := found[id]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// Assign changes only the issue keys and mutation metadata of explicitly
// selected unassigned records. Validation and writes share one transaction.
func (s *Service) Assign(ctx context.Context, input AssignInput, dry bool) (AssignResult, error) {
	invalid := func(message string) error {
		return ValidationError{Issues: []ValidationIssue{{Field: "assign", Message: message}}}
	}
	if !IsValidIssueKey(input.IssueKey) {
		return AssignResult{}, invalid("issue must match <PROJECTKEY>-<NUMBER>")
	}
	if len(input.IDs) == 0 {
		return AssignResult{}, invalid("select at least one worklog ID")
	}
	seen := make(map[string]bool, len(input.IDs))
	for _, id := range input.IDs {
		if id == "" || seen[id] {
			return AssignResult{}, invalid("worklog IDs must be non-empty and unique")
		}
		seen[id] = true
	}
	expected := make(map[string]int64, len(input.Expected))
	for _, item := range input.Expected {
		if !seen[item.ID] || item.Revision <= 0 || expected[item.ID] != 0 {
			return AssignResult{}, invalid("expected revisions must match the selected IDs")
		}
		expected[item.ID] = item.Revision
	}
	if len(input.Expected) > 0 && len(expected) != len(seen) {
		return AssignResult{}, invalid("each selected worklog requires an expected revision")
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return AssignResult{}, err
	}
	defer conn.Close()
	begin := "BEGIN IMMEDIATE"
	if dry {
		begin = "BEGIN"
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
		return AssignResult{}, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	items, err := lookupWorklogIDs(ctx, input.IDs, conn)
	if err != nil {
		return AssignResult{}, err
	}
	if len(items) != len(input.IDs) {
		return AssignResult{}, fmt.Errorf("%w: a selected worklog no longer exists", ErrNotFound)
	}
	for _, item := range items {
		if revision, ok := expected[item.ID]; ok && revision != item.Revision {
			return AssignResult{}, fmt.Errorf("%w: selected worklog %s changed; reopen assignment", ErrConflict, item.ID)
		}
		if item.IssueKey != "" {
			return AssignResult{}, invalid(fmt.Sprintf("worklog %s already has issue %s; edit it individually", item.ID, item.IssueKey))
		}
		candidate := item
		candidate.IssueKey = input.IssueKey
		if err := s.validateConflictsWithQueryer(ctx, candidate, item.ID, false, time.UTC, conn); err != nil {
			return AssignResult{}, err
		}
	}
	result := AssignResult{DryRun: dry, Records: items}
	if dry {
		for i := range result.Records {
			result.Records[i].IssueKey = input.IssueKey
		}
		return result, nil
	}
	statement, err := conn.PrepareContext(ctx, `UPDATE worklogs SET issue_key = ?, updated_at = ?, revision = revision + 1 WHERE id = ? AND revision = ? AND issue_key = ''`)
	if err != nil {
		return AssignResult{}, err
	}
	defer statement.Close()
	now := s.now().UTC()
	for i, item := range items {
		updated, err := statement.ExecContext(ctx, input.IssueKey, sqlitestore.RFC3339UTC(now), item.ID, item.Revision)
		if err != nil {
			return AssignResult{}, err
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return AssignResult{}, err
		}
		if count != 1 {
			return AssignResult{}, ErrConflict
		}
		result.Records[i].IssueKey = input.IssueKey
		result.Records[i].UpdatedAt = now
		result.Records[i].Revision++
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return AssignResult{}, err
	}
	return result, nil
}
