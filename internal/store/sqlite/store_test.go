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

	columns := []string{"description", "adapter_families_json", "target_instances_json", "plan_direction", "target_adapter_family", "target_adapter_instance", "target_issue", "route_profile", "inspection_summary_json", "delivery_key"}
	for _, column := range columns {
		var exists int
		query := `SELECT COUNT(1) FROM pragma_table_info('saved_plan_items') WHERE name = ?`
		if column == "description" {
			query = `SELECT COUNT(1) FROM pragma_table_info('worklog_tombstones') WHERE name = ?`
		}
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
