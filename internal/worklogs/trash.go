package worklogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

const (
	TrashScopeLocal  = "local"
	TrashScopeRemote = "remote"
)

var ErrTrashNotFound = errors.New("trash record not found")

const trashSelectColumns = `id, storage_scope, source_worklog_id, source_created_at, source_updated_at, source_revision, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance`

type TrashOrigin struct {
	PlanDirection   string
	PlanID          *string
	PlanItemID      *string
	AdapterFamily   *string
	AdapterInstance *string
}

type TrashRecord struct {
	ID              string
	StorageScope    string
	SourceWorklogID *string
	SourceCreatedAt *time.Time
	SourceUpdatedAt *time.Time
	SourceRevision  *int64
	IssueKey        string
	StartedAtUTC    time.Time
	DurationSeconds int
	Description     string
	TrashedAt       time.Time
	ReasonCode      string
	ReasonDetail    string
	Origin          TrashOrigin
}

type TrashArchiveInput struct {
	StorageScope    string
	SourceWorklogID string
	SourceCreatedAt *time.Time
	SourceUpdatedAt *time.Time
	SourceRevision  *int64
	IssueKey        string
	StartedAtUTC    time.Time
	DurationSeconds int
	Description     string
	TrashedAt       time.Time
	ReasonCode      string
	ReasonDetail    string
	PlanDirection   string
	PlanID          string
	PlanItemID      string
	AdapterFamily   string
	AdapterInstance string
}

type TrashFilters struct {
	ListFilters
	StorageScope string
}

type TrashSearchInput struct {
	Query string
	TrashFilters
}

type TrashRestoreItem struct {
	TrashID string
	Record  LocalWorklog
}

type TrashRestoreResult struct {
	Filters EffectiveFilters
	Scope   string
	DryRun  bool
	Items   []TrashRestoreItem
}

type trashWriteQueryer interface {
	sqlQueryer
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Service) ListTrash(cfg config.EffectiveConfig, filters TrashFilters) ([]TrashRecord, EffectiveFilters, error) {
	if !hasListTimeSelector(filters.ListFilters) {
		return nil, EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "workledger trash list requires at least one time selector"}}}
	}

	if err := validateTrashScope(filters.StorageScope); err != nil {
		return nil, EffectiveFilters{}, err
	}
	effective, err := normalizeListFiltersAt(cfg, filters.ListFilters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, err
	}

	items, err := s.listTrash(effective, filters.StorageScope)
	return items, effective, err
}

func (s *Service) SearchTrash(cfg config.EffectiveConfig, input TrashSearchInput) ([]TrashRecord, EffectiveFilters, string, error) {
	if !hasListTimeSelector(input.ListFilters) {
		return nil, EffectiveFilters{}, "", ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "workledger trash search requires at least one time selector"}}}
	}

	if err := validateTrashScope(input.StorageScope); err != nil {
		return nil, EffectiveFilters{}, "", err
	}
	effective, err := normalizeListFiltersAt(cfg, input.ListFilters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	query, err := normalizeSearchQuery(input.Query)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	items, err := s.searchTrash(effective, input.StorageScope, query)
	return items, effective, query, err
}

func (s *Service) ShowTrash(id string) (TrashRecord, error) {
	row := s.store.DB().QueryRow(
		`SELECT `+trashSelectColumns+` FROM trashed_worklogs WHERE id = ?`,
		id,
	)
	record, err := scanTrashRecord(row)
	if err == nil {
		return record, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return TrashRecord{}, ErrTrashNotFound
	}
	return TrashRecord{}, err
}

func (s *Service) RestoreTrash(ctx context.Context, cfg config.EffectiveConfig, id string) (TrashRestoreItem, error) {
	if id == "" {
		return TrashRestoreItem{}, ValidationError{Issues: []ValidationIssue{{Field: "id", Message: "trash id is required"}}}
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return TrashRestoreItem{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return TrashRestoreItem{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	record, err := showTrashWithQueryer(ctx, conn, id)
	if err != nil {
		return TrashRestoreItem{}, err
	}
	items, err := s.restoreTrashRowsTx(ctx, cfg, conn, []TrashRecord{record}, s.now().UTC())
	if err != nil {
		return TrashRestoreItem{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return TrashRestoreItem{}, err
	}
	committed = true
	return items[0], nil
}

func (s *Service) RestoreTrashBatch(ctx context.Context, cfg config.EffectiveConfig, filters TrashFilters, dryRun bool) (TrashRestoreResult, error) {
	effective, err := normalizeTrashRestoreFilters(cfg, filters, s.now)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return TrashRestoreResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	records, err := listTrashWithQueryer(ctx, conn, effective, TrashScopeLocal)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	items, err := s.prepareTrashRestoreTx(ctx, cfg, conn, records, s.now().UTC())
	if err != nil {
		return TrashRestoreResult{}, err
	}
	result := TrashRestoreResult{Filters: effective, Scope: TrashScopeLocal, DryRun: dryRun, Items: items}
	if dryRun || len(items) == 0 {
		return result, nil
	}
	if err := persistTrashRestoreTx(ctx, conn, items); err != nil {
		return TrashRestoreResult{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return TrashRestoreResult{}, err
	}
	committed = true
	return result, nil
}

func (s *Service) RestoreTrashBatchExpected(ctx context.Context, cfg config.EffectiveConfig, filters TrashFilters, expectedIDs []string) (TrashRestoreResult, error) {
	effective, err := normalizeTrashRestoreFilters(cfg, filters, s.now)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	if len(expectedIDs) == 0 {
		return TrashRestoreResult{}, ValidationError{Issues: []ValidationIssue{{Field: "expected", Message: "at least one expected trash id is required"}}}
	}
	expected := make(map[string]struct{}, len(expectedIDs))
	for _, id := range expectedIDs {
		if id == "" {
			return TrashRestoreResult{}, ValidationError{Issues: []ValidationIssue{{Field: "expected", Message: "expected trash ids cannot be empty"}}}
		}
		if _, exists := expected[id]; exists {
			return TrashRestoreResult{}, ValidationError{Issues: []ValidationIssue{{Field: "expected", Message: "expected trash ids must be unique"}}}
		}
		expected[id] = struct{}{}
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return TrashRestoreResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	records, err := listTrashWithQueryer(ctx, conn, effective, TrashScopeLocal)
	if err != nil {
		return TrashRestoreResult{}, err
	}
	records = restorableTrashRecords(records)
	if len(records) != len(expected) {
		return TrashRestoreResult{}, fmt.Errorf("%w: selected trash changed since confirmation", ErrConflict)
	}
	for _, record := range records {
		if _, ok := expected[record.ID]; !ok {
			return TrashRestoreResult{}, fmt.Errorf("%w: selected trash changed since confirmation", ErrConflict)
		}
	}
	items, err := s.restoreTrashRowsTx(ctx, cfg, conn, records, s.now().UTC())
	if err != nil {
		return TrashRestoreResult{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return TrashRestoreResult{}, err
	}
	committed = true
	return TrashRestoreResult{Filters: effective, Scope: TrashScopeLocal, Items: items}, nil
}

func restorableTrashRecords(records []TrashRecord) []TrashRecord {
	result := make([]TrashRecord, 0, len(records))
	for _, record := range records {
		if record.StorageScope == TrashScopeLocal && record.SourceWorklogID != nil {
			result = append(result, record)
		}
	}
	return result
}

func normalizeTrashRestoreFilters(cfg config.EffectiveConfig, filters TrashFilters, now func() time.Time) (EffectiveFilters, error) {
	if filters.StorageScope == TrashScopeRemote {
		return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "scope", Message: "remote trash is audit-only and cannot be restored"}}}
	}
	if err := validateTrashScope(filters.StorageScope); err != nil {
		return EffectiveFilters{}, err
	}
	if !hasListTimeSelector(filters.ListFilters) {
		return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "filtered trash restore requires at least one time selector"}}}
	}
	effective, err := normalizeListFiltersAt(cfg, filters.ListFilters, false, now)
	if err != nil {
		return EffectiveFilters{}, err
	}
	if effective.IssueKey == nil && effective.IssuePrefix == nil && effective.From == nil && effective.To == nil {
		return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "restore", Message: "batch restore requires at least one selector"}}}
	}
	return effective, nil
}

func (s *Service) restoreTrashRowsTx(ctx context.Context, cfg config.EffectiveConfig, tx trashWriteQueryer, records []TrashRecord, restoredAt time.Time) ([]TrashRestoreItem, error) {
	items, err := s.prepareTrashRestoreTx(ctx, cfg, tx, records, restoredAt)
	if err != nil {
		return nil, err
	}
	if err := persistTrashRestoreTx(ctx, tx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) prepareTrashRestoreTx(ctx context.Context, cfg config.EffectiveConfig, tx trashWriteQueryer, records []TrashRecord, restoredAt time.Time) ([]TrashRestoreItem, error) {
	candidates := make([]LocalWorklog, 0, len(records))
	items := make([]TrashRestoreItem, 0, len(records))
	sourceIDs := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.StorageScope != TrashScopeLocal {
			return nil, ValidationError{Issues: []ValidationIssue{{Field: "scope", Message: "remote trash is audit-only and cannot be restored"}}}
		}
		if record.SourceWorklogID == nil || *record.SourceWorklogID == "" {
			return nil, ValidationError{Issues: []ValidationIssue{{Field: "source_worklog_id", Message: "local trash without an original worklog id cannot be restored"}}}
		}
		if _, duplicate := sourceIDs[*record.SourceWorklogID]; duplicate {
			return nil, fmt.Errorf("%w: multiple trash rows reference active worklog id %s", ErrConflict, *record.SourceWorklogID)
		}
		sourceIDs[*record.SourceWorklogID] = struct{}{}
		var occupied int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM worklogs WHERE id = ?`, *record.SourceWorklogID).Scan(&occupied); err != nil {
			return nil, err
		}
		if occupied != 0 {
			return nil, fmt.Errorf("%w: active worklog id %s already exists", ErrConflict, *record.SourceWorklogID)
		}
		createdAt := restoredAt
		revision := int64(1)
		if record.SourceCreatedAt != nil {
			createdAt = record.SourceCreatedAt.UTC()
		}
		if record.SourceRevision != nil {
			revision = *record.SourceRevision + 1
		}
		candidate := LocalWorklog{ID: *record.SourceWorklogID, IssueKey: record.IssueKey, StartedAtUTC: record.StartedAtUTC, DurationSeconds: record.DurationSeconds, Description: record.Description, CreatedAt: createdAt, UpdatedAt: restoredAt, Revision: revision}
		candidates = append(candidates, candidate)
		items = append(items, TrashRestoreItem{TrashID: record.ID, Record: candidate})
	}
	if err := s.validateAddConflictsWithQueryer(ctx, cfg, candidates, false, tx); err != nil {
		return nil, err
	}
	return items, nil
}

func persistTrashRestoreTx(ctx context.Context, tx trashWriteQueryer, items []TrashRestoreItem) error {
	for _, item := range items {
		record := item.Record
		if _, err := tx.ExecContext(ctx, `INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at, revision) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.IssueKey, sqlitestore.RFC3339UTC(record.StartedAtUTC), record.DurationSeconds, record.Description, sqlitestore.RFC3339UTC(record.CreatedAt), sqlitestore.RFC3339UTC(record.UpdatedAt), record.Revision); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM trashed_worklogs WHERE id = ? AND storage_scope = ?`, item.TrashID, TrashScopeLocal)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("%w: trash changed during restore", ErrConflict)
		}
	}
	return nil
}

func showTrashWithQueryer(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (TrashRecord, error) {
	record, err := scanTrashRecord(queryer.QueryRowContext(ctx, `SELECT `+trashSelectColumns+` FROM trashed_worklogs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return TrashRecord{}, ErrTrashNotFound
	}
	return record, err
}

func InsertTrashRowsTx(tx *sql.Tx, items []TrashArchiveInput) ([]string, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id := uuid.NewString()
		_, err := tx.Exec(
			`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, source_created_at, source_updated_at, source_revision, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			item.StorageScope,
			nullableString(item.SourceWorklogID),
			nullableTime(item.SourceCreatedAt),
			nullableTime(item.SourceUpdatedAt),
			nullableInt64(item.SourceRevision),
			item.IssueKey,
			sqlitestore.RFC3339UTC(item.StartedAtUTC.UTC()),
			item.DurationSeconds,
			item.Description,
			sqlitestore.RFC3339UTC(item.TrashedAt.UTC()),
			item.ReasonCode,
			item.ReasonDetail,
			item.PlanDirection,
			nullableString(item.PlanID),
			nullableString(item.PlanItemID),
			nullableString(item.AdapterFamily),
			nullableString(item.AdapterInstance),
		)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) listTrash(filters EffectiveFilters, scope string) ([]TrashRecord, error) {
	return listTrashWithQueryer(context.Background(), s.store.DB(), filters, scope)
}

func listTrashWithQueryer(ctx context.Context, queryer sqlQueryer, filters EffectiveFilters, scope string) ([]TrashRecord, error) {
	query := `SELECT ` + trashSelectColumns + ` FROM trashed_worklogs`
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	where = appendTrashScope(where, scope, &args)
	query += where + ` ORDER BY started_at_utc ASC, id ASC`

	rows, err := queryer.QueryContext(ctx, query, args...)
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

func (s *Service) searchTrash(filters EffectiveFilters, scope, query string) ([]TrashRecord, error) {
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	where = appendTrashScope(where, scope, &args)
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	args = append(args, literalSubstringPattern(query))
	statement := `SELECT ` + trashSelectColumns + ` FROM trashed_worklogs` + where + `description LIKE ? ESCAPE '\' COLLATE NOCASE ORDER BY started_at_utc DESC, id ASC`

	rows, err := s.store.DB().Query(statement, args...)
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

func scanTrashRecord(scanner interface{ Scan(dest ...any) error }) (TrashRecord, error) {
	var item TrashRecord
	var startedAt string
	var trashedAt string
	var sourceWorklogID sql.NullString
	var sourceCreatedAt sql.NullString
	var sourceUpdatedAt sql.NullString
	var sourceRevision sql.NullInt64
	var planID sql.NullString
	var planItemID sql.NullString
	var adapterFamily sql.NullString
	var adapterInstance sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.StorageScope,
		&sourceWorklogID,
		&sourceCreatedAt,
		&sourceUpdatedAt,
		&sourceRevision,
		&item.IssueKey,
		&startedAt,
		&item.DurationSeconds,
		&item.Description,
		&trashedAt,
		&item.ReasonCode,
		&item.ReasonDetail,
		&item.Origin.PlanDirection,
		&planID,
		&planItemID,
		&adapterFamily,
		&adapterInstance,
	); err != nil {
		return TrashRecord{}, err
	}

	parsedStart, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return TrashRecord{}, err
	}
	parsedTrashedAt, err := time.Parse(time.RFC3339, trashedAt)
	if err != nil {
		return TrashRecord{}, err
	}

	item.StartedAtUTC = parsedStart.UTC()
	item.TrashedAt = parsedTrashedAt.UTC()
	item.SourceWorklogID = nullableStringPointer(sourceWorklogID)
	item.SourceCreatedAt, err = nullableTimePointer(sourceCreatedAt)
	if err != nil {
		return TrashRecord{}, err
	}
	item.SourceUpdatedAt, err = nullableTimePointer(sourceUpdatedAt)
	if err != nil {
		return TrashRecord{}, err
	}
	if sourceRevision.Valid {
		revision := sourceRevision.Int64
		item.SourceRevision = &revision
	}
	item.Origin.PlanID = nullableStringPointer(planID)
	item.Origin.PlanItemID = nullableStringPointer(planItemID)
	item.Origin.AdapterFamily = nullableStringPointer(adapterFamily)
	item.Origin.AdapterInstance = nullableStringPointer(adapterInstance)
	return item, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	typed := value.String
	return &typed
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return sqlitestore.RFC3339UTC(value.UTC())
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTimePointer(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value.String)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func validateTrashScope(scope string) error {
	if scope == "" || scope == TrashScopeLocal || scope == TrashScopeRemote {
		return nil
	}
	return ValidationError{Issues: []ValidationIssue{{Field: "scope", Message: "scope must be local or remote"}}}
}

func appendTrashScope(where, scope string, args *[]any) string {
	if scope == "" {
		return where
	}
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	*args = append(*args, scope)
	return where + "storage_scope = ?"
}

func ArchiveLocalTrashTx(tx *sql.Tx, planID, planItemID string, rows []LocalWorklog, trashedAt time.Time, reasonCode, reasonDetail string) error {
	records := make([]TrashArchiveInput, 0, len(rows))
	for _, row := range rows {
		records = append(records, TrashArchiveInput{
			StorageScope:    TrashScopeLocal,
			SourceWorklogID: row.ID,
			SourceCreatedAt: &row.CreatedAt,
			SourceUpdatedAt: &row.UpdatedAt,
			SourceRevision:  &row.Revision,
			IssueKey:        row.IssueKey,
			StartedAtUTC:    row.StartedAtUTC,
			DurationSeconds: row.DurationSeconds,
			Description:     row.Description,
			TrashedAt:       trashedAt,
			ReasonCode:      reasonCode,
			ReasonDetail:    reasonDetail,
			PlanDirection:   "pull",
			PlanID:          planID,
			PlanItemID:      planItemID,
		})
	}
	_, err := InsertTrashRowsTx(tx, records)
	return err
}

func DeleteActiveWorklogsTx(ctx context.Context, tx *sql.Tx, rows []LocalWorklog) error {
	for _, row := range rows {
		result, err := tx.ExecContext(ctx, `DELETE FROM worklogs WHERE id = ? AND revision = ?`, row.ID, row.Revision)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: worklog %s changed during pull apply", ErrConflict, row.ID)
		}
	}
	return nil
}
