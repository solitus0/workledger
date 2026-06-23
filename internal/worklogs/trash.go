package worklogs

import (
	"context"
	"database/sql"
	"errors"
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

func (s *Service) ListTrash(cfg config.EffectiveConfig, filters ListFilters) ([]TrashRecord, EffectiveFilters, error) {
	if !hasListTimeSelector(filters) {
		return nil, EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "workledger trash list requires at least one time selector"}}}
	}

	effective, err := normalizeListFiltersAt(cfg, filters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, err
	}

	items, err := s.listTrash(effective)
	return items, effective, err
}

func (s *Service) SearchTrash(cfg config.EffectiveConfig, input SearchInput) ([]TrashRecord, EffectiveFilters, string, error) {
	if !hasListTimeSelector(input.ListFilters) {
		return nil, EffectiveFilters{}, "", ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "workledger trash search requires at least one time selector"}}}
	}

	effective, err := normalizeListFiltersAt(cfg, input.ListFilters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	query, err := normalizeSearchQuery(input.Query)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	items, err := s.searchTrash(effective, query)
	return items, effective, query, err
}

func (s *Service) ShowTrash(id string) (TrashRecord, error) {
	row := s.store.DB().QueryRow(
		`SELECT id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance FROM trashed_worklogs WHERE id = ?`,
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

func InsertTrashRowsTx(tx *sql.Tx, items []TrashArchiveInput) error {
	for _, item := range items {
		_, err := tx.Exec(
			`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(),
			item.StorageScope,
			nullableString(item.SourceWorklogID),
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
			return err
		}
	}
	return nil
}

func (s *Service) listTrash(filters EffectiveFilters) ([]TrashRecord, error) {
	query := `SELECT id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance FROM trashed_worklogs`
	args := make([]any, 0)
	query += buildWhereClause(filters, false, &args) + ` ORDER BY started_at_utc ASC, id ASC`

	rows, err := s.store.DB().Query(query, args...)
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

func (s *Service) searchTrash(filters EffectiveFilters, query string) ([]TrashRecord, error) {
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	args = append(args, literalSubstringPattern(query))
	statement := `SELECT id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, origin_plan_id, origin_plan_item_id, adapter_family, adapter_instance FROM trashed_worklogs` + where + `description LIKE ? ESCAPE '\' COLLATE NOCASE ORDER BY started_at_utc DESC, id ASC`

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
	var planID sql.NullString
	var planItemID sql.NullString
	var adapterFamily sql.NullString
	var adapterInstance sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.StorageScope,
		&sourceWorklogID,
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

func ArchiveLocalTrashTx(tx *sql.Tx, planID, planItemID string, rows []LocalWorklog, trashedAt time.Time, reasonCode, reasonDetail string) error {
	records := make([]TrashArchiveInput, 0, len(rows))
	for _, row := range rows {
		records = append(records, TrashArchiveInput{
			StorageScope:    TrashScopeLocal,
			SourceWorklogID: row.ID,
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
	return InsertTrashRowsTx(tx, records)
}

func DeleteActiveWorklogsTx(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM worklogs WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}
