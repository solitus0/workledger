package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestBootstrapRepairsSavedPlanPushColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	statements := []string{
		`CREATE TABLE worklogs (
			id TEXT PRIMARY KEY,
			issue_key TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			description TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE worklog_tombstones (
			worklog_id TEXT PRIMARY KEY,
			issue_key TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			deleted_at TEXT NOT NULL
		)`,
		`CREATE TABLE saved_plans (
			id TEXT PRIMARY KEY,
			plan_direction TEXT NOT NULL,
			adapter_family TEXT NOT NULL,
			config_fingerprint TEXT NOT NULL,
			window_from_utc TEXT NOT NULL,
			window_to_utc TEXT NOT NULL,
			created_at TEXT NOT NULL,
			aggregate_status TEXT NOT NULL,
			applied_at TEXT NULL
		)`,
		`CREATE TABLE saved_plan_items (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			issue_key TEXT NOT NULL,
			window_from_utc TEXT NOT NULL,
			window_to_utc TEXT NOT NULL,
			plan_status TEXT NOT NULL,
			planned_action TEXT NOT NULL,
			comparison_status TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			reason_detail TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			local_row_count INTEGER NOT NULL,
			local_total_seconds INTEGER NOT NULL,
			remote_row_count INTEGER NOT NULL,
			remote_total_seconds INTEGER NOT NULL,
			applied_state TEXT NOT NULL,
			applied_at TEXT NULL,
			apply_message TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('row-1', 'AAPP-1', '2026-05-01T08:00:00Z', 3600, 'preserved', '2026-05-01T08:00:00Z', '2026-05-01T08:00:00Z')`); err != nil {
		t.Fatalf("seed worklog: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status, applied_at) VALUES('plan-1', 'pull', 'clockify', 'fp', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', '2026-05-02T00:00:00Z', 'ready', '2026-05-02T00:01:00Z')`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO saved_plan_items(id, plan_id, issue_key, window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail, payload_json, content_hash, local_row_count, local_total_seconds, remote_row_count, remote_total_seconds, applied_state, applied_at, apply_message) VALUES('item-1', 'plan-1', 'AAPP-1', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', 'ready', 'merge', 'merge_needed', 'remote_diff', 'diff', '[]', 'hash', 0, 0, 0, 0, 'succeeded', '2026-05-02T00:01:00Z', 'merged saved pull payload into local ledger')`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	_ = db.Close()

	store, status, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap repair: %v", err)
	}
	defer store.Close()
	if status != StatusRepaired {
		t.Fatalf("expected repaired status, got %s", status)
	}

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM saved_plan_items WHERE id = 'item-1'`).Scan(&count); err != nil {
		t.Fatalf("count repaired rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected repaired row to be preserved, got %d", count)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM worklogs WHERE id = 'row-1'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected worklog to be preserved: count=%d err=%v", count, err)
	}

	var state, message, createdAt string
	if err := store.DB().QueryRow(`SELECT attempt_state, message, created_at FROM delivery_attempts WHERE plan_item_id = 'item-1'`).Scan(&state, &message, &createdAt); err != nil {
		t.Fatalf("load migrated attempt: %v", err)
	}
	if state != "succeeded" || message != "migrated legacy result: merged saved pull payload into local ledger" || createdAt != "2026-05-02T00:01:00Z" {
		t.Fatalf("unexpected migrated attempt: state=%q message=%q created_at=%q", state, message, createdAt)
	}

	var tombstoneTables int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'worklog_tombstones'`).Scan(&tombstoneTables); err != nil {
		t.Fatalf("check tombstone table removal: %v", err)
	}
	if tombstoneTables != 0 {
		t.Fatalf("expected legacy tombstone table to be dropped, got %d", tombstoneTables)
	}

	columns := []string{"adapter_families_json", "target_instances_json", "plan_direction", "target_adapter_family", "target_adapter_instance", "target_issue", "route_profile", "inspection_summary_json", "delivery_key"}
	for _, column := range columns {
		var exists int
		query := `SELECT COUNT(1) FROM pragma_table_info('saved_plan_items') WHERE name = ?`
		if column == "adapter_families_json" || column == "target_instances_json" {
			query = `SELECT COUNT(1) FROM pragma_table_info('saved_plans') WHERE name = ?`
		}
		if err := store.DB().QueryRow(query, column).Scan(&exists); err != nil {
			t.Fatalf("check column %s: %v", column, err)
		}
		if exists != 1 {
			t.Fatalf("expected column %s to exist after repair", column)
		}
	}
	for _, column := range legacyPlanItemLifecycleColumns {
		var exists int
		if err := store.DB().QueryRow(`SELECT COUNT(1) FROM pragma_table_info('saved_plan_items') WHERE name = ?`, column).Scan(&exists); err != nil {
			t.Fatalf("check removed column %s: %v", column, err)
		}
		if exists != 0 {
			t.Fatalf("legacy column %s still exists after migration", column)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close migrated store: %v", err)
	}
	reopened, secondStatus, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	defer reopened.Close()
	if secondStatus != StatusReused {
		t.Fatalf("expected idempotent bootstrap to reuse schema, got %s", secondStatus)
	}
}

func TestWritableStoreUsesWALAndConnectionPragmas(t *testing.T) {
	store, _, err := Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.DB().QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}

	ctx := context.Background()
	connections := make([]*sql.Conn, 0, 3)
	for range 3 {
		conn, err := store.DB().Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		connections = append(connections, conn)
		var foreignKeys int
		var busyTimeout int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatalf("foreign_keys: %v", err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatalf("busy_timeout: %v", err)
		}
		if foreignKeys != 1 || busyTimeout != 5000 {
			t.Fatalf("unexpected pragmas foreign_keys=%d busy_timeout=%d", foreignKeys, busyTimeout)
		}
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func TestChangeTrackerDetectsAnotherStoreCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer store.Close()
	other, err := OpenExisting(path)
	if err != nil {
		t.Fatalf("OpenExisting failed: %v", err)
	}
	defer other.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tracker, err := store.NewChangeTracker(ctx)
	if err != nil {
		t.Fatalf("NewChangeTracker failed: %v", err)
	}
	defer tracker.Close()
	changed, err := tracker.Poll(ctx)
	if err != nil || changed {
		t.Fatalf("initial poll changed=%t err=%v", changed, err)
	}

	_, err = other.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('external', 'APP-1', '2026-05-01T09:00:00Z', 900, 'external', '2026-05-01T09:00:00Z', '2026-05-01T09:00:00Z')`)
	if err != nil {
		t.Fatalf("external insert: %v", err)
	}
	changed, err = tracker.Poll(ctx)
	if err != nil || !changed {
		t.Fatalf("external poll changed=%t err=%v", changed, err)
	}
}

func TestWALAllowsReaderWhileAnotherStoreHasUncommittedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	writer, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer writer.Close()
	reader, err := OpenExistingReadOnly(path)
	if err != nil {
		t.Fatalf("OpenExistingReadOnly failed: %v", err)
	}
	defer reader.Close()

	tx, err := writer.DB().Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('pending', 'APP-1', '2026-05-01T09:00:00Z', 900, 'pending', '2026-05-01T09:00:00Z', '2026-05-01T09:00:00Z')`)
	if err != nil {
		t.Fatalf("uncommitted insert: %v", err)
	}
	var count int
	if err := reader.DB().QueryRow(`SELECT COUNT(*) FROM worklogs`).Scan(&count); err != nil {
		t.Fatalf("read during write transaction: %v", err)
	}
	if count != 0 {
		t.Fatalf("reader observed uncommitted row: count=%d", count)
	}
}

func TestBootstrapRejectsCorruptExistingSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("seed invalid sqlite file: %v", err)
	}

	_, _, err := Bootstrap(path)
	if err == nil {
		t.Fatal("expected bootstrap error")
	}

	var bootstrapErr *BootstrapError
	if !errors.As(err, &bootstrapErr) {
		t.Fatalf("expected bootstrap error type, got %T", err)
	}
	if bootstrapErr.Kind != BootstrapErrorCorrupt {
		t.Fatalf("expected corrupt error kind, got %s", bootstrapErr.Kind)
	}
	if bootstrapErr.Path != path {
		t.Fatalf("expected path %s, got %s", path, bootstrapErr.Path)
	}
}

func TestBootstrapRepairsTruncatedExplicitIndexWithoutLosingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap fixture: %v", err)
	}

	if _, err := store.DB().Exec(`DROP INDEX idx_activity_source_state_started`); err != nil {
		t.Fatalf("drop fixture index: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('preserved-worklog', 'APP-1', '2026-09-09T07:00:00Z', 3600, 'must survive repair', '2026-09-09T08:00:00Z', '2026-09-09T08:00:00Z')`); err != nil {
		t.Fatalf("insert preserved worklog: %v", err)
	}
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin fixture rows: %v", err)
	}
	for index := range 2_000 {
		if _, err := tx.Exec(`INSERT INTO activity_entries(id, source, operation, summary, attributes_json, state, started_at) VALUES(?, 'cli', 'status', '', '{}', 'succeeded', ?)`, fmt.Sprintf("activity-%04d", index), fmt.Sprintf("2026-09-09T08:%02d:%02dZ", index/60%60, index%60)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert fixture row %d: %v", index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit fixture rows: %v", err)
	}
	if _, err := store.DB().Exec(`CREATE INDEX idx_activity_source_state_started ON activity_entries(source, state, started_at DESC)`); err != nil {
		t.Fatalf("create fixture index: %v", err)
	}
	if _, err := store.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint fixture: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	if err := os.Truncate(path, info.Size()-4096); err != nil {
		t.Fatalf("truncate fixture index page: %v", err)
	}

	repaired, status, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("repair truncated index: %v", err)
	}
	defer repaired.Close()
	if status != StatusRepaired {
		t.Fatalf("status = %q, want %q", status, StatusRepaired)
	}
	var count int
	if err := repaired.DB().QueryRow(`SELECT COUNT(*) FROM activity_entries`).Scan(&count); err != nil {
		t.Fatalf("count preserved rows: %v", err)
	}
	if count != 2_000 {
		t.Fatalf("preserved rows = %d, want 2000", count)
	}
	if err := repaired.DB().QueryRow(`SELECT COUNT(*) FROM worklogs WHERE id = 'preserved-worklog' AND description = 'must survive repair'`).Scan(&count); err != nil {
		t.Fatalf("count preserved worklog: %v", err)
	}
	if count != 1 {
		t.Fatalf("preserved worklogs = %d, want 1", count)
	}
	var integrity string
	if err := repaired.DB().QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity check = %q, want ok", integrity)
	}
}

func TestBootstrapRejectsIncompatibleExistingSQLiteSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE worklogs (
		id TEXT PRIMARY KEY,
		issue_key TEXT NOT NULL,
		started_at_utc TEXT NOT NULL,
		duration_seconds TEXT NOT NULL,
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("seed incompatible worklogs: %v", err)
	}
	_ = db.Close()

	_, _, err = Bootstrap(path)
	if err == nil {
		t.Fatal("expected bootstrap error")
	}

	var bootstrapErr *BootstrapError
	if !errors.As(err, &bootstrapErr) {
		t.Fatalf("expected bootstrap error type, got %T", err)
	}
	if bootstrapErr.Kind != BootstrapErrorIncompatible {
		t.Fatalf("expected incompatible error kind, got %s", bootstrapErr.Kind)
	}
}

func TestBootstrapCreatesTrashTableAndIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")

	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer store.Close()

	for _, table := range []string{"trashed_worklogs"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("expected table %s to exist", table)
		}
	}

	for _, index := range []string{
		"idx_trashed_worklogs_issue_started",
		"idx_trashed_worklogs_trashed_at",
		"idx_trashed_worklogs_reason_code",
		"idx_trashed_worklogs_scope_trashed_at",
	} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatalf("check index %s: %v", index, err)
		}
		if count != 1 {
			t.Fatalf("expected index %s to exist", index)
		}
	}
	for _, column := range []string{"source_created_at", "source_updated_at", "source_revision"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('trashed_worklogs') WHERE name = ?`, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("trash column %s count=%d err=%v", column, count, err)
		}
	}

	var tombstoneTables int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'worklog_tombstones'`).Scan(&tombstoneTables); err != nil {
		t.Fatalf("check tombstone table absence: %v", err)
	}
	if tombstoneTables != 0 {
		t.Fatalf("expected no tombstone table in current schema, got %d", tombstoneTables)
	}
}

func TestBootstrapCreatesWorklogPresetSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer store.Close()

	for _, name := range []string{"worklog_presets", "idx_worklog_presets_name", "idx_worklog_presets_last_used_name"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("schema object %s count=%d err=%v", name, count, err)
		}
	}
	for _, column := range []string{"id", "name", "issue_key", "start_time", "duration_seconds", "description", "created_at", "updated_at", "last_used_at", "revision"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('worklog_presets') WHERE name = ?`, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("preset column %s count=%d err=%v", column, count, err)
		}
	}
}

func TestBootstrapCreatesAndUsesQueryPerformanceIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		index string
		query string
		args  []any
	}{
		{name: "interval end", index: "idx_worklogs_interval_end", query: `SELECT id FROM worklogs WHERE unixepoch(started_at_utc) + duration_seconds > unixepoch(?)`, args: []any{"2026-05-01T00:00:00Z"}},
		{name: "worklog completion order", index: "idx_worklogs_updated_id", query: `SELECT id FROM worklogs ORDER BY updated_at DESC, id LIMIT ?`, args: []any{100}},
		{name: "issue recency", index: "idx_worklogs_issue_updated_duration", query: `SELECT issue_key, MAX(updated_at) FROM worklogs WHERE issue_key >= ? AND issue_key < ? GROUP BY issue_key`, args: []any{"APP", "APP\U0010FFFF"}},
		{name: "issue duration total", index: "idx_worklogs_issue_updated_duration", query: `SELECT COALESCE(SUM(duration_seconds), 0) FROM worklogs WHERE issue_key = ?`, args: []any{"APP-1"}},
		{name: "description recency", index: "idx_worklogs_issue_description_updated", query: `SELECT description FROM worklogs WHERE issue_key = ? GROUP BY description ORDER BY MAX(updated_at) DESC`, args: []any{"APP-1"}},
		{name: "preset recency", index: "idx_worklog_presets_recency", query: `SELECT name FROM worklog_presets ORDER BY last_used_at IS NULL, last_used_at DESC, name LIMIT ?`, args: []any{100}},
		{name: "trash scope window", index: "idx_trashed_worklogs_scope_started_id", query: `SELECT id FROM trashed_worklogs WHERE storage_scope = ? AND started_at_utc >= ? AND started_at_utc <= ? ORDER BY started_at_utc, id`, args: []any{"local", "2026-05-01T00:00:00Z", "2026-05-01T23:59:59Z"}},
		{name: "trash completion order", index: "idx_trashed_worklogs_scope_trashed_id", query: `SELECT id FROM trashed_worklogs WHERE storage_scope = ? ORDER BY trashed_at DESC, id LIMIT ?`, args: []any{"local", 100}},
		{name: "recent plans", index: "idx_saved_plans_created_id", query: `SELECT id FROM saved_plans ORDER BY created_at DESC, id DESC LIMIT ?`, args: []any{100}},
		{name: "plan item order", index: "idx_saved_plan_items_plan_order", query: `SELECT id FROM saved_plan_items WHERE plan_id = ? ORDER BY target_issue, target_adapter_family, target_adapter_instance, window_from_utc, id`, args: []any{"plan-1"}},
		{name: "finding order", index: "idx_saved_plan_findings_plan_order", query: `SELECT id FROM saved_plan_findings WHERE plan_id = ? ORDER BY source_row_id, id`, args: []any{"plan-1"}},
		{name: "attempt plan history", index: "idx_delivery_attempts_plan_created", query: `SELECT plan_item_id FROM delivery_attempts WHERE plan_id = ? ORDER BY created_at`, args: []any{"plan-1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := store.DB().Query(`EXPLAIN QUERY PLAN `+test.query, test.args...)
			if err != nil {
				t.Fatalf("explain query: %v", err)
			}
			defer rows.Close()
			details := make([]string, 0)
			for rows.Next() {
				var id, parent, notUsed int
				var detail string
				if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
					t.Fatalf("scan query plan: %v", err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("query plan rows: %v", err)
			}
			if !strings.Contains(strings.Join(details, "\n"), test.index) {
				t.Fatalf("query plan did not use %s: %v", test.index, details)
			}
		})
	}
}

func TestOpenExistingRejectsSchemaMissingTrashTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	seedLegacyStoreMissingTrashTable(t, path)

	_, err := OpenExisting(path)
	if err == nil {
		t.Fatal("expected open existing error")
	}

	var openErr *OpenExistingError
	if !errors.As(err, &openErr) {
		t.Fatalf("expected open existing error type, got %T", err)
	}
	if openErr.Kind != OpenExistingErrorSchemaMismatch {
		t.Fatalf("expected schema mismatch error kind, got %s", openErr.Kind)
	}
}

func TestOpenExistingRejectsSchemaMissingSavedPlanItemDeliveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	seedLegacyStoreMissingSavedPlanItemDeliveryKey(t, path)

	_, err := OpenExisting(path)
	if err == nil {
		t.Fatal("expected open existing error")
	}

	var openErr *OpenExistingError
	if !errors.As(err, &openErr) {
		t.Fatalf("expected open existing error type, got %T", err)
	}
	if openErr.Kind != OpenExistingErrorSchemaMismatch {
		t.Fatalf("expected schema mismatch error kind, got %s", openErr.Kind)
	}
}

func TestOpenExistingReadOnlyAllowsQueriesAndRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('one', 'APP-1', '2026-05-01T09:00:00Z', 900, 'seed', '2026-05-01T09:00:00Z', '2026-05-01T09:00:00Z')`); err != nil {
		store.Close()
		t.Fatalf("seed worklog: %v", err)
	}
	_ = store.Close()

	readOnly, err := OpenExistingReadOnly(path)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer readOnly.Close()

	var count int
	if err := readOnly.DB().QueryRow(`SELECT COUNT(*) FROM worklogs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("read through read-only store: count=%d err=%v", count, err)
	}
	if _, err := readOnly.DB().Exec(`DELETE FROM worklogs`); err == nil {
		t.Fatal("expected read-only store to reject writes")
	}
}

func TestOpenExistingReadOnlyDoesNotCreateOrRepairStorage(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	if _, err := OpenExistingReadOnly(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing-file error, got %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read-only open created a missing database")
	}

	legacy := filepath.Join(dir, "legacy.db")
	seedLegacyStoreMissingSavedPlanItemDeliveryKey(t, legacy)
	if _, err := OpenExistingReadOnly(legacy); err == nil {
		t.Fatal("expected schema mismatch from read-only open")
	}

	db, err := sql.Open("sqlite", legacy)
	if err != nil {
		t.Fatalf("reopen legacy store: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('saved_plan_items') WHERE name = 'delivery_key'`).Scan(&count); err != nil {
		t.Fatalf("inspect legacy schema: %v", err)
	}
	if count != 0 {
		t.Fatal("read-only open repaired the legacy schema")
	}
}

func TestOpenExistingRejectsUnmigratedPlanLifecycleColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	_ = store.Close()
	addLegacyPlanItemLifecycleColumns(t, path)

	_, err = OpenExisting(path)
	if err == nil {
		t.Fatal("expected schema mismatch")
	}
	var openErr *OpenExistingError
	if !errors.As(err, &openErr) || openErr.Kind != OpenExistingErrorSchemaMismatch {
		t.Fatalf("expected schema mismatch error, got %v", err)
	}
}

func TestBootstrapRejectsInvalidLegacyPlanLifecycleAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	seedCurrentStoreWithLegacyPlanItem(t, path, "succeeded", "", "")

	_, _, err := Bootstrap(path)
	if err == nil {
		t.Fatal("expected invalid legacy lifecycle error")
	}
	var bootstrapErr *BootstrapError
	if !errors.As(err, &bootstrapErr) || bootstrapErr.Kind != BootstrapErrorIncompatible {
		t.Fatalf("expected incompatible bootstrap error, got %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer db.Close()
	var columns, attempts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('saved_plan_items') WHERE name IN ('applied_state', 'applied_at', 'apply_message')`).Scan(&columns); err != nil {
		t.Fatalf("count legacy columns: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM delivery_attempts`).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if columns != 3 || attempts != 0 {
		t.Fatalf("failed migration was not atomic: columns=%d attempts=%d", columns, attempts)
	}
}

func TestBootstrapRejectsContradictoryLegacyPlanLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	seedCurrentStoreWithLegacyPlanItem(t, path, "succeeded", "2026-05-02T00:01:00Z", "merged")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES('attempt-1', 'plan-1', 'item-1', 'failed', 'failed', '2026-05-02T00:00:30Z')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed contradictory attempt: %v", err)
	}
	_ = db.Close()

	_, _, err = Bootstrap(path)
	if err == nil {
		t.Fatal("expected contradictory legacy lifecycle error")
	}
	var bootstrapErr *BootstrapError
	if !errors.As(err, &bootstrapErr) || bootstrapErr.Kind != BootstrapErrorIncompatible {
		t.Fatalf("expected incompatible bootstrap error, got %v", err)
	}
}

func TestBootstrapDoesNotDuplicateMatchingLegacyAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	seedCurrentStoreWithLegacyPlanItem(t, path, "succeeded", "2026-05-02T00:01:00Z", "merged")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES('attempt-1', 'plan-1', 'item-1', 'succeeded', 'already recorded', '2026-05-02T00:01:00Z')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed matching attempt: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO trashed_worklogs(id, storage_scope, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES('trash-1', 'local', 'AAPP-1', '2026-05-01T08:00:00Z', 3600, 'preserved trash', '2026-05-02T00:01:00Z', 'pull_replaced', 'preserved', 'pull')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed trash row: %v", err)
	}
	_ = db.Close()

	store, status, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer store.Close()
	if status != StatusRepaired {
		t.Fatalf("expected repaired status, got %s", status)
	}
	var attempts int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM delivery_attempts WHERE plan_item_id = 'item-1'`).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected matching attempt to remain singular, got %d", attempts)
	}
	var trashRows int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs WHERE id = 'trash-1'`).Scan(&trashRows); err != nil || trashRows != 1 {
		t.Fatalf("expected trash row to survive migration: count=%d err=%v", trashRows, err)
	}
}

func addLegacyPlanItemLifecycleColumns(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	for _, statement := range []string{
		`ALTER TABLE saved_plan_items ADD COLUMN applied_state TEXT NOT NULL DEFAULT 'not_attempted'`,
		`ALTER TABLE saved_plan_items ADD COLUMN applied_at TEXT NULL`,
		`ALTER TABLE saved_plan_items ADD COLUMN apply_message TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("add legacy lifecycle column: %v", err)
		}
	}
}

func seedCurrentStoreWithLegacyPlanItem(t *testing.T, path, state, appliedAt, message string) {
	t.Helper()
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	_ = store.Close()
	addLegacyPlanItemLifecycleColumns(t, path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO saved_plans(id, plan_direction, adapter_family, adapter_families_json, target_instances_json, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status, applied_at) VALUES('plan-1', 'pull', 'clockify', '["clockify"]', '["clockify"]', 'fp', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', '2026-05-02T00:00:00Z', 'ready', NULL)`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO saved_plan_items(id, plan_id, issue_key, plan_direction, target_adapter_family, target_adapter_instance, target_issue, route_profile, window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail, payload_json, inspection_summary_json, delivery_key, content_hash, local_row_count, local_total_seconds, remote_row_count, remote_total_seconds, applied_state, applied_at, apply_message) VALUES('item-1', 'plan-1', 'AAPP-1', 'pull', 'clockify', 'clockify', 'AAPP-1', NULL, '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', 'ready', 'merge', 'merge_needed', 'remote_diff', 'diff', '[]', '{}', 'delivery-1', 'hash', 0, 0, 0, 0, ?, NULLIF(?, ''), ?)`, state, appliedAt, message); err != nil {
		t.Fatalf("seed plan item: %v", err)
	}
}

func seedLegacyStoreMissingTrashTable(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE worklogs (
			id TEXT PRIMARY KEY,
			issue_key TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			description TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
}

func seedLegacyStoreMissingSavedPlanItemDeliveryKey(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE worklogs (
			id TEXT PRIMARY KEY,
			issue_key TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			description TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE trashed_worklogs (
			id TEXT PRIMARY KEY,
			storage_scope TEXT NOT NULL,
			source_worklog_id TEXT NULL,
			issue_key TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			description TEXT NOT NULL,
			trashed_at TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			reason_detail TEXT NOT NULL,
			plan_direction TEXT NOT NULL,
			origin_plan_id TEXT NULL,
			origin_plan_item_id TEXT NULL,
			adapter_family TEXT NULL,
			adapter_instance TEXT NULL
		)`,
		`CREATE TABLE issue_metadata (
			issue_key TEXT PRIMARY KEY,
			max_estimate_seconds INTEGER NULL,
			source_adapter_family TEXT NOT NULL,
			source_adapter_instance TEXT NOT NULL,
			refreshed_at TEXT NOT NULL
		)`,
		`CREATE TABLE saved_plans (
			id TEXT PRIMARY KEY,
			plan_direction TEXT NOT NULL,
			adapter_family TEXT NOT NULL,
			adapter_families_json TEXT NOT NULL DEFAULT '[]',
			target_instances_json TEXT NOT NULL DEFAULT '[]',
			config_fingerprint TEXT NOT NULL,
			window_from_utc TEXT NOT NULL,
			window_to_utc TEXT NOT NULL,
			created_at TEXT NOT NULL,
			aggregate_status TEXT NOT NULL,
			applied_at TEXT NULL
		)`,
		`CREATE TABLE saved_plan_items (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			issue_key TEXT NOT NULL,
			plan_direction TEXT NOT NULL DEFAULT 'pull',
			target_adapter_family TEXT NOT NULL DEFAULT '',
			target_adapter_instance TEXT NOT NULL DEFAULT '',
			target_issue TEXT NOT NULL DEFAULT '',
			route_profile TEXT NULL,
			window_from_utc TEXT NOT NULL,
			window_to_utc TEXT NOT NULL,
			plan_status TEXT NOT NULL,
			planned_action TEXT NOT NULL,
			comparison_status TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			reason_detail TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			inspection_summary_json TEXT NOT NULL DEFAULT '{}',
			content_hash TEXT NOT NULL,
			local_row_count INTEGER NOT NULL,
			local_total_seconds INTEGER NOT NULL,
			remote_row_count INTEGER NOT NULL,
			remote_total_seconds INTEGER NOT NULL,
			applied_state TEXT NOT NULL,
			applied_at TEXT NULL,
			apply_message TEXT NOT NULL
		)`,
		`CREATE TABLE saved_plan_findings (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			source_row_id TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			reason_detail TEXT NOT NULL,
			payload_json TEXT NOT NULL
		)`,
		`CREATE TABLE delivery_attempts (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			plan_item_id TEXT NOT NULL,
			attempt_state TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
}
