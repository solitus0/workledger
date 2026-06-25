package sqlite

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	if _, err := db.Exec(`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status) VALUES('plan-1', 'pull', 'clockify', 'fp', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', '2026-05-02T00:00:00Z', 'ready')`); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO saved_plan_items(id, plan_id, issue_key, window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail, payload_json, content_hash, local_row_count, local_total_seconds, remote_row_count, remote_total_seconds, applied_state, apply_message) VALUES('item-1', 'plan-1', 'AAPP-1', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', 'ready', 'merge', 'merge_needed', 'remote_diff', 'diff', '[]', 'hash', 0, 0, 0, 0, 'not_attempted', '')`); err != nil {
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

	var tombstoneTables int
	if err := store.DB().QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'worklog_tombstones'`).Scan(&tombstoneTables); err != nil {
		t.Fatalf("check tombstone table absence: %v", err)
	}
	if tombstoneTables != 0 {
		t.Fatalf("expected no tombstone table in current schema, got %d", tombstoneTables)
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
