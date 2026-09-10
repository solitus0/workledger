package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

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

type OpenExistingErrorKind string

const (
	OpenExistingErrorSchemaMismatch OpenExistingErrorKind = "schema_mismatch"
	OpenExistingErrorCorrupt        OpenExistingErrorKind = "corrupt"
	OpenExistingErrorIncompatible   OpenExistingErrorKind = "incompatible"
)

type OpenExistingError struct {
	Kind OpenExistingErrorKind
	Path string
	Err  error
}

func (e *OpenExistingError) Error() string {
	return e.Err.Error()
}

func (e *OpenExistingError) Unwrap() error {
	return e.Err
}

type Store struct {
	db *sql.DB
}

type ChangeTracker struct {
	conn    *sql.Conn
	query   string
	version int64
	closed  bool
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
		updated_at TEXT NOT NULL,
		revision INTEGER NOT NULL DEFAULT 1
	)`,
	`CREATE TABLE IF NOT EXISTS trashed_worklogs (
		id TEXT PRIMARY KEY,
		storage_scope TEXT NOT NULL,
		source_worklog_id TEXT NULL,
		source_created_at TEXT NULL,
		source_updated_at TEXT NULL,
		source_revision INTEGER NULL,
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
	`CREATE TABLE IF NOT EXISTS worklog_presets (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		issue_key TEXT NOT NULL,
		start_time TEXT NOT NULL,
		duration_seconds INTEGER NOT NULL,
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		last_used_at TEXT NULL,
		revision INTEGER NOT NULL DEFAULT 1
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
		remote_total_seconds INTEGER NOT NULL
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
	`CREATE TABLE IF NOT EXISTS activity_entries (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		operation TEXT NOT NULL,
		summary TEXT NOT NULL,
		attributes_json TEXT NOT NULL DEFAULT '{}',
		state TEXT NOT NULL,
		started_at TEXT NOT NULL,
		finished_at TEXT NULL,
		duration_ms INTEGER NULL,
		exit_code INTEGER NULL,
		error_code TEXT NOT NULL DEFAULT '',
		error_message TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS domain_change_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		revision INTEGER NOT NULL
	)`,
	`INSERT OR IGNORE INTO domain_change_state(id, revision) VALUES(1, 0)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_worklogs_id ON worklogs(id)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_issue_started ON worklogs(issue_key, started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_started ON worklogs(started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_created_at ON worklogs(created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_interval_end ON worklogs(unixepoch(started_at_utc) + duration_seconds)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_updated_id ON worklogs(updated_at DESC, id)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_issue_updated_duration ON worklogs(issue_key, updated_at DESC, duration_seconds)`,
	`CREATE INDEX IF NOT EXISTS idx_worklogs_issue_description_updated ON worklogs(issue_key, description, updated_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_issue_started ON trashed_worklogs(issue_key, started_at_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_trashed_at ON trashed_worklogs(trashed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_reason_code ON trashed_worklogs(reason_code)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_scope_trashed_at ON trashed_worklogs(storage_scope, trashed_at)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_scope_started_id ON trashed_worklogs(storage_scope, started_at_utc, id)`,
	`CREATE INDEX IF NOT EXISTS idx_trashed_worklogs_scope_trashed_id ON trashed_worklogs(storage_scope, trashed_at DESC, id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_metadata_issue_key ON issue_metadata(issue_key)`,
	`CREATE INDEX IF NOT EXISTS idx_issue_metadata_refreshed_at ON issue_metadata(refreshed_at)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_worklog_presets_name ON worklog_presets(name)`,
	`CREATE INDEX IF NOT EXISTS idx_worklog_presets_last_used_name ON worklog_presets(last_used_at, name)`,
	`CREATE INDEX IF NOT EXISTS idx_worklog_presets_recency ON worklog_presets(last_used_at IS NULL, last_used_at DESC, name)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plans_created_at ON saved_plans(created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plans_created_id ON saved_plans(created_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_plan_id ON saved_plan_items(plan_id)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_issue_window ON saved_plan_items(issue_key, window_from_utc, window_to_utc)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_findings_plan_id ON saved_plan_findings(plan_id)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_plan_findings_plan_order ON saved_plan_findings(plan_id, source_row_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_attempts_plan_item_created ON delivery_attempts(plan_item_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_attempts_plan_created ON delivery_attempts(plan_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_attempts_state_created ON delivery_attempts(attempt_state, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_activity_started_id ON activity_entries(started_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_activity_source_state_started ON activity_entries(source, state, started_at DESC)`,
	`CREATE TRIGGER IF NOT EXISTS trg_worklogs_activity_insert AFTER INSERT ON worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_worklogs_activity_update AFTER UPDATE ON worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_worklogs_activity_delete AFTER DELETE ON worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_trash_activity_insert AFTER INSERT ON trashed_worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_trash_activity_update AFTER UPDATE ON trashed_worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_trash_activity_delete AFTER DELETE ON trashed_worklogs BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_metadata_activity_insert AFTER INSERT ON issue_metadata BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_metadata_activity_update AFTER UPDATE ON issue_metadata BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_metadata_activity_delete AFTER DELETE ON issue_metadata BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_presets_activity_insert AFTER INSERT ON worklog_presets BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_presets_activity_update AFTER UPDATE ON worklog_presets BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_presets_activity_delete AFTER DELETE ON worklog_presets BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plans_activity_insert AFTER INSERT ON saved_plans BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plans_activity_update AFTER UPDATE ON saved_plans BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plans_activity_delete AFTER DELETE ON saved_plans BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plan_items_activity_insert AFTER INSERT ON saved_plan_items BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plan_items_activity_update AFTER UPDATE ON saved_plan_items BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_saved_plan_items_activity_delete AFTER DELETE ON saved_plan_items BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_delivery_attempts_activity_insert AFTER INSERT ON delivery_attempts BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_delivery_attempts_activity_update AFTER UPDATE ON delivery_attempts BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
	`CREATE TRIGGER IF NOT EXISTS trg_delivery_attempts_activity_delete AFTER DELETE ON delivery_attempts BEGIN UPDATE domain_change_state SET revision = revision + 1 WHERE id = 1; END`,
}

func Bootstrap(path string) (*Store, BootstrapStatus, error) {
	existed := fileExists(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}

	store, status, err := bootstrap(path, existed, false)
	if err == nil {
		return store, status, nil
	}
	if !existed || !isSQLiteCorruptionError(err) {
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	repaired, repairErr := repairCorruptIndexes(path)
	if repairErr != nil {
		return nil, "", wrapBootstrapError(path, existed, errors.Join(err, fmt.Errorf("index repair: %w", repairErr)))
	}
	if !repaired {
		return nil, "", wrapBootstrapError(path, existed, err)
	}

	store, status, err = bootstrap(path, true, true)
	if err != nil {
		return nil, "", wrapBootstrapError(path, true, err)
	}
	return store, status, nil
}

func bootstrap(path string, existed, forceRepaired bool) (*Store, BootstrapStatus, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return nil, "", err
	}

	store := &Store{db: db}
	if err := store.pingAndConfigure(true); err != nil {
		_ = db.Close()
		return nil, "", err
	}

	before, err := store.schemaFingerprint()
	if err != nil {
		_ = db.Close()
		return nil, "", err
	}

	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, "", err
	}

	if err := store.validateSchemaCompatibility(); err != nil {
		_ = db.Close()
		return nil, "", err
	}

	after, err := store.schemaFingerprint()
	if err != nil {
		_ = db.Close()
		return nil, "", err
	}

	switch {
	case !existed:
		return store, StatusCreated, nil
	case forceRepaired || before != after:
		return store, StatusRepaired, nil
	default:
		return store, StatusReused, nil
	}
}

type repairableIndex struct {
	name     string
	rootPage int
	ddl      string
}

type catalogTree struct {
	name     string
	rootPage int
}

// repairCorruptIndexes repairs a single missing final page only when every tree
// that references it is an explicit index. Table corruption remains
// unrecoverable because guessing at table contents would risk data loss.
func repairCorruptIndexes(path string) (bool, error) {
	ctx := context.Background()
	indexes, trees, err := loadRepairableIndexes(ctx, path)
	if err != nil {
		return false, fmt.Errorf("load index catalog: %w", err)
	}
	damaged, err := truncatedIndexDamage(path, indexes, trees)
	if err != nil {
		return false, fmt.Errorf("inspect truncated SQLite file: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return false, err
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA writable_schema = ON`); err != nil {
		return false, err
	}

	var schemaVersion int
	if err := conn.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schemaVersion); err != nil {
		return false, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	for _, index := range damaged {
		result, err := tx.ExecContext(ctx, `DELETE FROM sqlite_schema WHERE type = 'index' AND name = ? AND rootpage = ?`, index.name, index.rootPage)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("remove corrupt index %s: %w", index.name, err)
		}
		if changed != 1 {
			return false, fmt.Errorf("remove corrupt index %s: changed %d catalog rows", index.name, changed)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA schema_version = %d`, schemaVersion+1)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if err := conn.Close(); err != nil {
		return false, err
	}
	if err := db.Close(); err != nil {
		return false, err
	}

	rebuild, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return false, err
	}
	defer rebuild.Close()
	rebuildTx, err := rebuild.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer rebuildTx.Rollback()
	for _, index := range damaged {
		if _, err := rebuildTx.ExecContext(ctx, index.ddl); err != nil {
			return false, fmt.Errorf("rebuild corrupt index %s: %w", index.name, err)
		}
	}
	if err := rebuildTx.Commit(); err != nil {
		return false, err
	}
	if _, err := rebuild.ExecContext(ctx, `VACUUM`); err != nil {
		return false, err
	}
	return integrityOK(ctx, rebuild)
}

func loadRepairableIndexes(ctx context.Context, path string) (map[int]repairableIndex, []catalogTree, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA writable_schema = ON`); err != nil {
		return nil, nil, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT type, name, rootpage, sql FROM sqlite_schema WHERE rootpage > 0`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	indexes := make(map[int]repairableIndex)
	trees := make([]catalogTree, 0)
	for rows.Next() {
		var itemType, name string
		var rootPage int
		var ddl sql.NullString
		if err := rows.Scan(&itemType, &name, &rootPage, &ddl); err != nil {
			return nil, nil, err
		}
		repairable := itemType == "index" && ddl.Valid
		trees = append(trees, catalogTree{name: name, rootPage: rootPage})
		if !repairable {
			continue
		}
		var index repairableIndex
		index.name, index.rootPage, index.ddl = name, rootPage, ddl.String
		indexes[index.rootPage] = index
	}
	return indexes, trees, rows.Err()
}

func truncatedIndexDamage(path string, indexes map[int]repairableIndex, trees []catalogTree) ([]repairableIndex, error) {
	if walInfo, err := os.Stat(path + "-wal"); err == nil && walInfo.Size() > 0 {
		return nil, errors.New("cannot inspect a truncated database while a WAL file is present")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 100)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, err
	}
	if string(header[:16]) != "SQLite format 3\x00" {
		return nil, errors.New("invalid SQLite header")
	}
	pageSize := int(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if pageSize < 512 || info.Size()%int64(pageSize) != 0 {
		return nil, errors.New("SQLite file is not aligned to its page size")
	}
	actualPages := int(info.Size() / int64(pageSize))
	declaredPages := int(binary.BigEndian.Uint32(header[28:32]))
	if declaredPages != actualPages+1 {
		return nil, fmt.Errorf("repair supports one missing final page; header declares %d pages and file contains %d", declaredPages, actualPages)
	}

	damaged := make([]repairableIndex, 0)
	for _, tree := range trees {
		missing, err := treeReferencesMissingPage(file, pageSize, actualPages, tree.rootPage)
		if err != nil {
			return nil, fmt.Errorf("inspect tree %s: %w", tree.name, err)
		}
		if !missing {
			continue
		}
		index, ok := indexes[tree.rootPage]
		if !ok {
			return nil, fmt.Errorf("missing page belongs to non-repairable tree %s", tree.name)
		}
		damaged = append(damaged, index)
	}
	if len(damaged) == 0 {
		return nil, errors.New("missing final page is not referenced by an explicit index")
	}
	return damaged, nil
}

func treeReferencesMissingPage(file *os.File, pageSize, actualPages, rootPage int) (bool, error) {
	pending := []int{rootPage}
	visited := make(map[int]struct{})
	page := make([]byte, pageSize)
	for len(pending) > 0 {
		pageNumber := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if pageNumber == actualPages+1 {
			return true, nil
		}
		if pageNumber < 1 || pageNumber > actualPages {
			return false, fmt.Errorf("invalid page reference %d", pageNumber)
		}
		if _, ok := visited[pageNumber]; ok {
			continue
		}
		visited[pageNumber] = struct{}{}
		if _, err := file.ReadAt(page, int64(pageNumber-1)*int64(pageSize)); err != nil {
			return false, err
		}
		headerOffset := 0
		if pageNumber == 1 {
			headerOffset = 100
		}
		pageType := page[headerOffset]
		if pageType == 0x0a || pageType == 0x0d {
			continue
		}
		if pageType != 0x02 && pageType != 0x05 {
			return false, fmt.Errorf("page %d has invalid b-tree type 0x%02x", pageNumber, pageType)
		}
		cellCount := int(binary.BigEndian.Uint16(page[headerOffset+3 : headerOffset+5]))
		pointerEnd := headerOffset + 12 + cellCount*2
		if pointerEnd > len(page) {
			return false, fmt.Errorf("page %d has an invalid cell pointer array", pageNumber)
		}
		pending = append(pending, int(binary.BigEndian.Uint32(page[headerOffset+8:headerOffset+12])))
		for cell := 0; cell < cellCount; cell++ {
			pointerOffset := headerOffset + 12 + cell*2
			cellOffset := int(binary.BigEndian.Uint16(page[pointerOffset : pointerOffset+2]))
			if cellOffset < headerOffset+12 || cellOffset+4 > len(page) {
				return false, fmt.Errorf("page %d has invalid cell offset %d", pageNumber, cellOffset)
			}
			pending = append(pending, int(binary.BigEndian.Uint32(page[cellOffset:cellOffset+4])))
		}
	}
	return false, nil
}

func integrityOK(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	ok := false
	for rows.Next() {
		var report string
		if err := rows.Scan(&report); err != nil {
			return false, err
		}
		if report != "ok" {
			return false, fmt.Errorf("SQLite integrity check failed after index repair: %s", report)
		}
		ok = true
	}
	return ok, rows.Err()
}

func OpenExisting(path string) (*Store, error) {
	if !fileExists(path) {
		return nil, os.ErrNotExist
	}

	db, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.pingAndConfigure(true); err != nil {
		_ = db.Close()
		return nil, wrapOpenExistingError(path, err)
	}

	if err := store.validateSchemaCompatibility(); err != nil {
		_ = db.Close()
		return nil, wrapOpenExistingError(path, err)
	}

	return store, nil
}

// OpenExistingReadOnly opens and validates an existing store without allowing
// schema repair or data mutation. It is intended for advisory readers such as
// shell completion.
func OpenExistingReadOnly(path string) (*Store, error) {
	if !fileExists(path) {
		return nil, os.ErrNotExist
	}

	dsn := sqliteDSN(path, true)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, wrapOpenExistingError(path, err)
	}
	if err := store.validateSchemaCompatibility(); err != nil {
		_ = db.Close()
		return nil, wrapOpenExistingError(path, err)
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) pingAndConfigure(writable bool) error {
	if err := s.db.Ping(); err != nil {
		return err
	}
	if !writable {
		return nil
	}
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("failed to enable WAL journal mode: SQLite returned %q", mode)
	}
	return nil
}

func sqliteDSN(path string, readOnly bool) string {
	query := url.Values{}
	if readOnly {
		query.Set("mode", "ro")
	}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func (s *Store) NewChangeTracker(ctx context.Context) (*ChangeTracker, error) {
	return s.newChangeTracker(ctx, `SELECT revision FROM domain_change_state WHERE id = 1`)
}

// NewActivityChangeTracker detects commits from other SQLite connections,
// including activity-only writes, without classifying them as domain changes.
func (s *Store) NewActivityChangeTracker(ctx context.Context) (*ChangeTracker, error) {
	return s.newChangeTracker(ctx, `PRAGMA data_version`)
}

func (s *Store) newChangeTracker(ctx context.Context, query string) (*ChangeTracker, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tracker := &ChangeTracker{conn: conn, query: query}
	if err := conn.QueryRowContext(ctx, query).Scan(&tracker.version); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tracker, nil
}

func (t *ChangeTracker) Poll(ctx context.Context) (bool, error) {
	if t.closed {
		return false, errors.New("change tracker is closed")
	}
	var version int64
	if err := t.conn.QueryRowContext(ctx, t.query).Scan(&version); err != nil {
		return false, err
	}
	changed := version != t.version
	t.version = version
	return changed, nil
}

func (t *ChangeTracker) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	return t.conn.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS worklog_tombstones`); err != nil {
		_ = tx.Rollback()
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
		{table: "saved_plans", column: "adapter_families_json", ddl: `ALTER TABLE saved_plans ADD COLUMN adapter_families_json TEXT NOT NULL DEFAULT '[]'`},
		{table: "worklogs", column: "revision", ddl: `ALTER TABLE worklogs ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`},
		{table: "trashed_worklogs", column: "source_created_at", ddl: `ALTER TABLE trashed_worklogs ADD COLUMN source_created_at TEXT NULL`},
		{table: "trashed_worklogs", column: "source_updated_at", ddl: `ALTER TABLE trashed_worklogs ADD COLUMN source_updated_at TEXT NULL`},
		{table: "trashed_worklogs", column: "source_revision", ddl: `ALTER TABLE trashed_worklogs ADD COLUMN source_revision INTEGER NULL`},
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
	if err := migrateLegacyPlanItemLifecycle(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	postRepairIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_target_scope ON saved_plan_items(target_adapter_family, target_adapter_instance, target_issue)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_delivery_key ON saved_plan_items(delivery_key)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_plan_items_plan_order ON saved_plan_items(plan_id, target_issue, target_adapter_family, target_adapter_instance, window_from_utc, id)`,
	}
	for _, statement := range postRepairIndexes {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

var legacyPlanItemLifecycleColumns = []string{"applied_state", "applied_at", "apply_message"}

func migrateLegacyPlanItemLifecycle(ctx context.Context, tx *sql.Tx) error {
	present := 0
	for _, column := range legacyPlanItemLifecycleColumns {
		exists, err := hasColumnTx(tx, "saved_plan_items", column)
		if err != nil {
			return err
		}
		if exists {
			present++
		}
	}
	if present == 0 {
		return nil
	}
	if present != len(legacyPlanItemLifecycleColumns) {
		return &schemaValidationError{
			Kind: schemaValidationErrorIncompatible,
			Err:  errors.New("saved_plan_items has an incomplete legacy lifecycle schema"),
		}
	}

	var invalidItemID string
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM saved_plan_items
		WHERE applied_state NOT IN ('not_attempted', 'succeeded', 'failed', 'uncertain')
			OR (applied_state = 'not_attempted' AND applied_at IS NOT NULL)
			OR (applied_state != 'not_attempted' AND applied_at IS NULL)
		LIMIT 1`).Scan(&invalidItemID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		return &schemaValidationError{
			Kind: schemaValidationErrorIncompatible,
			Err:  fmt.Errorf("saved plan item %s has an invalid legacy lifecycle result", invalidItemID),
		}
	}

	var contradictoryItemID string
	err = tx.QueryRowContext(ctx, `
		SELECT i.id
		FROM saved_plan_items i
		WHERE i.applied_state != 'not_attempted'
			AND EXISTS (SELECT 1 FROM delivery_attempts a WHERE a.plan_item_id = i.id)
			AND NOT EXISTS (
				SELECT 1
				FROM delivery_attempts a
				WHERE a.plan_item_id = i.id AND a.attempt_state = i.applied_state
			)
		LIMIT 1`).Scan(&contradictoryItemID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		return &schemaValidationError{
			Kind: schemaValidationErrorIncompatible,
			Err:  fmt.Errorf("saved plan item %s has contradictory legacy lifecycle history", contradictoryItemID),
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at)
		SELECT
			'legacy-plan-item-' || i.id,
			i.plan_id,
			i.id,
			i.applied_state,
			CASE
				WHEN trim(i.apply_message) = '' THEN 'migrated legacy ' || i.applied_state || ' result'
				ELSE 'migrated legacy result: ' || i.apply_message
			END,
			i.applied_at
		FROM saved_plan_items i
		WHERE i.applied_state != 'not_attempted'
			AND NOT EXISTS (SELECT 1 FROM delivery_attempts a WHERE a.plan_item_id = i.id)`); err != nil {
		return err
	}

	for _, column := range legacyPlanItemLifecycleColumns {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE saved_plan_items DROP COLUMN `+column); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasColumn(tx *sql.Tx, table, column string) (bool, error) {
	return hasColumnTx(tx, table, column)
}

func hasColumnTx(tx *sql.Tx, table, column string) (bool, error) {
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

type tableRequirement struct {
	table   string
	columns []columnRequirement
}

type schemaValidationErrorKind string

const (
	schemaValidationErrorMismatch     schemaValidationErrorKind = "mismatch"
	schemaValidationErrorIncompatible schemaValidationErrorKind = "incompatible"
)

type schemaValidationError struct {
	Kind schemaValidationErrorKind
	Err  error
}

func (e *schemaValidationError) Error() string {
	return e.Err.Error()
}

func (e *schemaValidationError) Unwrap() error {
	return e.Err
}

var requiredSchema = []tableRequirement{
	{
		table: "worklogs",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "issue_key", typ: "TEXT", notNull: true},
			{column: "started_at_utc", typ: "TEXT", notNull: true},
			{column: "duration_seconds", typ: "INTEGER", notNull: true},
			{column: "description", typ: "TEXT", notNull: true},
			{column: "created_at", typ: "TEXT", notNull: true},
			{column: "updated_at", typ: "TEXT", notNull: true},
			{column: "revision", typ: "INTEGER", notNull: true},
		},
	},
	{
		table: "trashed_worklogs",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "storage_scope", typ: "TEXT", notNull: true},
			{column: "source_worklog_id", typ: "TEXT", notNull: false},
			{column: "source_created_at", typ: "TEXT", notNull: false},
			{column: "source_updated_at", typ: "TEXT", notNull: false},
			{column: "source_revision", typ: "INTEGER", notNull: false},
			{column: "issue_key", typ: "TEXT", notNull: true},
			{column: "started_at_utc", typ: "TEXT", notNull: true},
			{column: "duration_seconds", typ: "INTEGER", notNull: true},
			{column: "description", typ: "TEXT", notNull: true},
			{column: "trashed_at", typ: "TEXT", notNull: true},
			{column: "reason_code", typ: "TEXT", notNull: true},
			{column: "reason_detail", typ: "TEXT", notNull: true},
			{column: "plan_direction", typ: "TEXT", notNull: true},
			{column: "origin_plan_id", typ: "TEXT", notNull: false},
			{column: "origin_plan_item_id", typ: "TEXT", notNull: false},
			{column: "adapter_family", typ: "TEXT", notNull: false},
			{column: "adapter_instance", typ: "TEXT", notNull: false},
		},
	},
	{
		table: "issue_metadata",
		columns: []columnRequirement{
			{column: "issue_key", typ: "TEXT", notNull: false},
			{column: "max_estimate_seconds", typ: "INTEGER", notNull: false},
			{column: "source_adapter_family", typ: "TEXT", notNull: true},
			{column: "source_adapter_instance", typ: "TEXT", notNull: true},
			{column: "refreshed_at", typ: "TEXT", notNull: true},
		},
	},
	{
		table: "worklog_presets",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "name", typ: "TEXT", notNull: true},
			{column: "issue_key", typ: "TEXT", notNull: true},
			{column: "start_time", typ: "TEXT", notNull: true},
			{column: "duration_seconds", typ: "INTEGER", notNull: true},
			{column: "description", typ: "TEXT", notNull: true},
			{column: "created_at", typ: "TEXT", notNull: true},
			{column: "updated_at", typ: "TEXT", notNull: true},
			{column: "last_used_at", typ: "TEXT", notNull: false},
			{column: "revision", typ: "INTEGER", notNull: true},
		},
	},
	{
		table: "saved_plans",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "plan_direction", typ: "TEXT", notNull: true},
			{column: "adapter_family", typ: "TEXT", notNull: true},
			{column: "adapter_families_json", typ: "TEXT", notNull: true},
			{column: "target_instances_json", typ: "TEXT", notNull: true},
			{column: "config_fingerprint", typ: "TEXT", notNull: true},
			{column: "window_from_utc", typ: "TEXT", notNull: true},
			{column: "window_to_utc", typ: "TEXT", notNull: true},
			{column: "created_at", typ: "TEXT", notNull: true},
			{column: "aggregate_status", typ: "TEXT", notNull: true},
			{column: "applied_at", typ: "TEXT", notNull: false},
		},
	},
	{
		table: "saved_plan_items",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "plan_id", typ: "TEXT", notNull: true},
			{column: "issue_key", typ: "TEXT", notNull: true},
			{column: "plan_direction", typ: "TEXT", notNull: true},
			{column: "target_adapter_family", typ: "TEXT", notNull: true},
			{column: "target_adapter_instance", typ: "TEXT", notNull: true},
			{column: "target_issue", typ: "TEXT", notNull: true},
			{column: "route_profile", typ: "TEXT", notNull: false},
			{column: "window_from_utc", typ: "TEXT", notNull: true},
			{column: "window_to_utc", typ: "TEXT", notNull: true},
			{column: "plan_status", typ: "TEXT", notNull: true},
			{column: "planned_action", typ: "TEXT", notNull: true},
			{column: "comparison_status", typ: "TEXT", notNull: true},
			{column: "reason_code", typ: "TEXT", notNull: true},
			{column: "reason_detail", typ: "TEXT", notNull: true},
			{column: "payload_json", typ: "TEXT", notNull: true},
			{column: "inspection_summary_json", typ: "TEXT", notNull: true},
			{column: "delivery_key", typ: "TEXT", notNull: true},
			{column: "content_hash", typ: "TEXT", notNull: true},
			{column: "local_row_count", typ: "INTEGER", notNull: true},
			{column: "local_total_seconds", typ: "INTEGER", notNull: true},
			{column: "remote_row_count", typ: "INTEGER", notNull: true},
			{column: "remote_total_seconds", typ: "INTEGER", notNull: true},
		},
	},
	{
		table: "saved_plan_findings",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "plan_id", typ: "TEXT", notNull: true},
			{column: "source_row_id", typ: "TEXT", notNull: true},
			{column: "reason_code", typ: "TEXT", notNull: true},
			{column: "reason_detail", typ: "TEXT", notNull: true},
			{column: "payload_json", typ: "TEXT", notNull: true},
		},
	},
	{
		table: "delivery_attempts",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "plan_id", typ: "TEXT", notNull: true},
			{column: "plan_item_id", typ: "TEXT", notNull: true},
			{column: "attempt_state", typ: "TEXT", notNull: true},
			{column: "message", typ: "TEXT", notNull: true},
			{column: "created_at", typ: "TEXT", notNull: true},
		},
	},
	{
		table: "activity_entries",
		columns: []columnRequirement{
			{column: "id", typ: "TEXT", notNull: false},
			{column: "source", typ: "TEXT", notNull: true},
			{column: "operation", typ: "TEXT", notNull: true},
			{column: "summary", typ: "TEXT", notNull: true},
			{column: "attributes_json", typ: "TEXT", notNull: true},
			{column: "state", typ: "TEXT", notNull: true},
			{column: "started_at", typ: "TEXT", notNull: true},
			{column: "finished_at", typ: "TEXT", notNull: false},
			{column: "duration_ms", typ: "INTEGER", notNull: false},
			{column: "exit_code", typ: "INTEGER", notNull: false},
			{column: "error_code", typ: "TEXT", notNull: true},
			{column: "error_message", typ: "TEXT", notNull: true},
		},
	},
	{
		table: "domain_change_state",
		columns: []columnRequirement{
			{column: "id", typ: "INTEGER", notNull: false},
			{column: "revision", typ: "INTEGER", notNull: true},
		},
	},
}

func (s *Store) validateSchemaCompatibility() error {
	for _, requirement := range requiredSchema {
		exists, err := s.tableExists(requirement.table)
		if err != nil {
			return err
		}
		if !exists {
			return &schemaValidationError{
				Kind: schemaValidationErrorMismatch,
				Err:  fmt.Errorf("table %s is missing", requirement.table),
			}
		}

		actual, err := s.tableColumns(requirement.table)
		if err != nil {
			return err
		}
		for _, columnRequirement := range requirement.columns {
			column, ok := actual[columnRequirement.column]
			if !ok {
				return &schemaValidationError{
					Kind: schemaValidationErrorMismatch,
					Err:  fmt.Errorf("table %s is missing required column %s", requirement.table, columnRequirement.column),
				}
			}
			if normalizeSQLiteType(column.typ) != columnRequirement.typ {
				return &schemaValidationError{
					Kind: schemaValidationErrorIncompatible,
					Err:  fmt.Errorf("table %s column %s has type %s; expected %s", requirement.table, columnRequirement.column, column.typ, columnRequirement.typ),
				}
			}
			if column.notNull != columnRequirement.notNull {
				return &schemaValidationError{
					Kind: schemaValidationErrorIncompatible,
					Err:  fmt.Errorf("table %s column %s has not_null=%t; expected %t", requirement.table, columnRequirement.column, column.notNull, columnRequirement.notNull),
				}
			}
		}
	}
	for _, column := range legacyPlanItemLifecycleColumns {
		exists, err := s.columnExists("saved_plan_items", column)
		if err != nil {
			return err
		}
		if exists {
			return &schemaValidationError{
				Kind: schemaValidationErrorMismatch,
				Err:  fmt.Errorf("table saved_plan_items contains obsolete column %s", column),
			}
		}
	}
	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	columns, err := s.tableColumns(table)
	if err != nil {
		return false, err
	}
	_, exists := columns[column]
	return exists, nil
}

type tableColumn struct {
	typ     string
	notNull bool
}

func (s *Store) tableExists(table string) (bool, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
	var schemaErr *schemaValidationError
	if errors.As(err, &schemaErr) {
		return &BootstrapError{Kind: BootstrapErrorIncompatible, Path: path, Err: err}
	}
	if isSQLiteCorruptionError(err) {
		return &BootstrapError{Kind: BootstrapErrorCorrupt, Path: path, Err: err}
	}
	if isSQLiteIncompatibilityError(err) {
		return &BootstrapError{Kind: BootstrapErrorIncompatible, Path: path, Err: err}
	}
	return err
}

func wrapOpenExistingError(path string, err error) error {
	if err == nil {
		return nil
	}

	var schemaErr *schemaValidationError
	if errors.As(err, &schemaErr) {
		kind := OpenExistingErrorIncompatible
		if schemaErr.Kind == schemaValidationErrorMismatch {
			kind = OpenExistingErrorSchemaMismatch
		}
		return &OpenExistingError{Kind: kind, Path: path, Err: err}
	}
	if isSQLiteCorruptionError(err) {
		return &OpenExistingError{Kind: OpenExistingErrorCorrupt, Path: path, Err: err}
	}
	if isSQLiteIncompatibilityError(err) {
		return &OpenExistingError{Kind: OpenExistingErrorIncompatible, Path: path, Err: err}
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

// PrefixRange returns inclusive and exclusive bounds for a BINARY-collated
// prefix scan over Workledger's canonical ASCII identifiers and names.
func PrefixRange(prefix string) (string, string) {
	return prefix, prefix + "\U0010FFFF"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
