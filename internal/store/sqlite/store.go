package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrSchemaMissing = errors.New("sqlite schema missing")

type BootstrapErrorKind string

const (
	BootstrapErrorCorrupt      BootstrapErrorKind = "corrupt"
	BootstrapErrorIncompatible BootstrapErrorKind = "incompatible"
)

type BootstrapError struct {
	Kind BootstrapErrorKind
	Path string
	Err  error
}

func (e *BootstrapError) Error() string {
	return e.Err.Error()
}

func (e *BootstrapError) Unwrap() error {
	return e.Err
}

type Store struct {
	db *sql.DB
}

type BootstrapStatus string

const (
	StatusCreated  BootstrapStatus = "created"
	StatusReused   BootstrapStatus = "reused"
	StatusRepaired BootstrapStatus = "repaired"
)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS worklogs (
		id TEXT PRIMARY KEY,
		issue_key TEXT NOT NULL,
		started_at_utc TEXT NOT NULL,
		duration_seconds INTEGER NOT NULL,
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS worklog_tombstones (
		worklog_id TEXT PRIMARY KEY,
		issue_key TEXT NOT NULL,
		started_at_utc TEXT NOT NULL,
		duration_seconds INTEGER NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		deleted_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS trashed_worklogs (
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
	`CREATE TABLE IF NOT EXISTS issue_metadata (
		issue_key TEXT PRIMARY KEY,
		max_estimate_seconds INTEGER NULL,
		source_adapter_family TEXT NOT NULL,
		source_adapter_instance TEXT NOT NULL,
		refreshed_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS saved_plans (
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
	`CREATE TABLE IF NOT EXISTS saved_plan_items (
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
		delivery_key TEXT NOT NULL DEFAULT '',
		content_hash TEXT NOT NULL,
		local_row_count INTEGER NOT NULL,
		local_total_seconds INTEGER NOT NULL,
		remote_row_count INTEGER NOT NULL,
		remote_total_seconds INTEGER NOT NULL,
		applied_state TEXT NOT NULL,
		applied_at TEXT NULL,
		apply_message TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS saved_plan_findings (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		source_row_id TEXT NOT NULL,
		reason_code TEXT NOT NULL,
		reason_detail TEXT NOT NULL,
		payload_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS delivery_attempts (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL,
		plan_item_id TEXT NOT NULL,
		attempt_state TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_worklogs_id ON worklogs(id)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_issue_started ON worklogs(issue_key, started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_started ON worklogs(started_at_utc)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_worklog_tombstones_worklog_id ON worklog_tombstones(worklog_id)`,
	`CREATE INDEX IF NOT EXISTS idx_worklog_tombstones_issue_started ON worklog_tombstones(issue_key, started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_worklog_tombstones_deleted_at ON worklog_tombstones(deleted_at)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_issue_started ON trashed_worklogs(issue_key, started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_trashed_at ON trashed_worklogs(trashed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_reason_code ON trashed_worklogs(reason_code)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_scope_trashed_at ON trashed_worklogs(storage_scope, trashed_at)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_metadata_issue_key ON issue_metadata(issue_key)`,
	`CREATE INDEX IF NOT EXISTS idx_issue_metadata_refreshed_at ON issue_metadata(refreshed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plans_created_at ON saved_plans(created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_plan_id ON saved_plan_items(plan_id)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_issue_window ON saved_plan_items(issue_key, window_from_utc, window_to_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_findings_plan_id ON saved_plan_findings(plan_id)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_attempts_plan_item_created ON delivery_attempts(plan_item_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_attempts_state_created ON delivery_attempts(attempt_state, created_at)`,
}

func Bootstrap(path string) (*Store, BootstrapStatus, error) {
	existed := fileExists(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, "", err
	}

	store := &Store{db: db}
	if err := store.pingAndConfigure(); err != nil {
		_ = db.Close()
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	before, err := store.schemaFingerprint()
	if err != nil {
		_ = db.Close()
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	if err := store.validateSchemaCompatibility(); err != nil {
		_ = db.Close()
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	after, err := store.schemaFingerprint()
	if err != nil {
		_ = db.Close()
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	switch {
	case !existed:
		return store, StatusCreated, nil
	case before != after:
		return store, StatusRepaired, nil
	default:
		return store, StatusReused, nil
	}
}

func OpenExisting(path string) (*Store, error) {
	if !fileExists(path) {
		return nil, os.ErrNotExist
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.pingAndConfigure(); err != nil {
		_ = db.Close()
		return nil, err
	}

	ok, err := store.hasRequiredTables()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if !ok {
		_ = db.Close()
		return nil, ErrSchemaMissing
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) pingAndConfigure() error {
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	return s.db.Ping()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for _, statement := range schemaStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	columnRepairs := []struct {
		table  string
		column string
		ddl    string
	}{
		{table: "worklog_tombstones", column: "description", ddl: `ALTER TABLE worklog_tombstones ADD COLUMN description TEXT NOT NULL DEFAULT ''`},
		{table: "saved_plans", column: "adapter_families_json", ddl: `ALTER TABLE saved_plans ADD COLUMN adapter_families_json TEXT NOT NULL DEFAULT '[]'`},
		{table: "saved_plans", column: "target_instances_json", ddl: `ALTER TABLE saved_plans ADD COLUMN target_instances_json TEXT NOT NULL DEFAULT '[]'`},
		{table: "saved_plan_items", column: "plan_direction", ddl: `ALTER TABLE saved_plan_items ADD COLUMN plan_direction TEXT NOT NULL DEFAULT 'pull'`},
		{table: "saved_plan_items", column: "target_adapter_family", ddl: `ALTER TABLE saved_plan_items ADD COLUMN target_adapter_family TEXT NOT NULL DEFAULT ''`},
		{table: "saved_plan_items", column: "target_adapter_instance", ddl: `ALTER TABLE saved_plan_items ADD COLUMN target_adapter_instance TEXT NOT NULL DEFAULT ''`},
		{table: "saved_plan_items", column: "target_issue", ddl: `ALTER TABLE saved_plan_items ADD COLUMN target_issue TEXT NOT NULL DEFAULT ''`},
		{table: "saved_plan_items", column: "route_profile", ddl: `ALTER TABLE saved_plan_items ADD COLUMN route_profile TEXT NULL`},
		{table: "saved_plan_items", column: "inspection_summary_json", ddl: `ALTER TABLE saved_plan_items ADD COLUMN inspection_summary_json TEXT NOT NULL DEFAULT '{}'`},
		{table: "saved_plan_items", column: "delivery_key", ddl: `ALTER TABLE saved_plan_items ADD COLUMN delivery_key TEXT NOT NULL DEFAULT ''`},
	}
	for _, repair := range columnRepairs {
		ok, err := s.hasColumn(tx, repair.table, repair.column)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, repair.ddl); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	postRepairIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_target_scope ON saved_plan_items(target_adapter_family, target_adapter_instance, target_issue)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_delivery_key ON saved_plan_items(delivery_key)`,
	}
	for _, statement := range postRepairIndexes {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) hasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

func (s *Store) hasRequiredTables() (bool, error) {
	required := []string{"worklogs", "worklog_tombstones"}
	for _, table := range required {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

func (s *Store) schemaFingerprint() (string, error) {
	rows, err := s.db.Query(`SELECT type, name, sql FROM sqlite_master WHERE type IN ('table', 'index') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	parts := make([]string, 0)
	for rows.Next() {
		var itemType string
		var name string
		var sqlText sql.NullString
		if err := rows.Scan(&itemType, &name, &sqlText); err != nil {
			return "", err
		}
		parts = append(parts, itemType+":"+name+":"+sqlText.String)
	}

	return strings.Join(parts, "\n"), rows.Err()
}

type columnRequirement struct {
	column  string
	typ     string
	notNull bool
}

var requiredSchema = map[string][]columnRequirement{
	"worklogs": {
		{column: "id", typ: "TEXT", notNull: false},
		{column: "issue_key", typ: "TEXT", notNull: true},
		{column: "started_at_utc", typ: "TEXT", notNull: true},
		{column: "duration_seconds", typ: "INTEGER", notNull: true},
		{column: "description", typ: "TEXT", notNull: true},
		{column: "created_at", typ: "TEXT", notNull: true},
		{column: "updated_at", typ: "TEXT", notNull: true},
	},
	"worklog_tombstones": {
		{column: "worklog_id", typ: "TEXT", notNull: false},
		{column: "issue_key", typ: "TEXT", notNull: true},
		{column: "started_at_utc", typ: "TEXT", notNull: true},
		{column: "duration_seconds", typ: "INTEGER", notNull: true},
		{column: "description", typ: "TEXT", notNull: true},
		{column: "deleted_at", typ: "TEXT", notNull: true},
	},
}

func (s *Store) validateSchemaCompatibility() error {
	for table, columns := range requiredSchema {
		actual, err := s.tableColumns(table)
		if err != nil {
			return err
		}
		for _, requirement := range columns {
			column, ok := actual[requirement.column]
			if !ok {
				return fmt.Errorf("table %s is missing required column %s", table, requirement.column)
			}
			if normalizeSQLiteType(column.typ) != requirement.typ {
				return fmt.Errorf("table %s column %s has type %s; expected %s", table, requirement.column, column.typ, requirement.typ)
			}
			if column.notNull != requirement.notNull {
				return fmt.Errorf("table %s column %s has not_null=%t; expected %t", table, requirement.column, column.notNull, requirement.notNull)
			}
		}
	}
	return nil
}

type tableColumn struct {
	typ     string
	notNull bool
}

func (s *Store) tableColumns(table string) (map[string]tableColumn, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]tableColumn)
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = tableColumn{
			typ:     columnType,
			notNull: notNull == 1,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func normalizeSQLiteType(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func wrapBootstrapError(path string, existed bool, err error) error {
	if !existed || err == nil {
		return err
	}
	if isSQLiteCorruptionError(err) {
		return &BootstrapError{Kind: BootstrapErrorCorrupt, Path: path, Err: err}
	}
	if isSQLiteIncompatibilityError(err) {
		return &BootstrapError{Kind: BootstrapErrorIncompatible, Path: path, Err: err}
	}
	return err
}

func isSQLiteCorruptionError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "file is not a database") ||
		strings.Contains(message, "database disk image is malformed") ||
		strings.Contains(message, "database is malformed") ||
		strings.Contains(message, "malformed database schema")
}

func isSQLiteIncompatibilityError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "missing required column") ||
		strings.Contains(message, "expected text") ||
		strings.Contains(message, "expected integer") ||
		strings.Contains(message, "expected true") ||
		strings.Contains(message, "expected false") ||
		strings.Contains(message, "no such column")
}

func RFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
