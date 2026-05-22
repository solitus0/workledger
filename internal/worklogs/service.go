package worklogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

var (
	ErrNotFound              = errors.New("worklog not found")
	ErrIssueMetadataNotFound = errors.New("issue metadata not found")
	ErrValidation            = errors.New("validation failed")
	ErrConflict              = errors.New("worklog conflict")
	issueKeyPattern          = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[1-9][0-9]*$`)
)

func IsValidIssueKey(value string) bool {
	return issueKeyPattern.MatchString(value)
}

func NormalizeDescription(value string) (string, error) {
	return normalizeDescription(value)
}

type LocalWorklog struct {
	ID              string
	IssueKey        string
	StartedAtUTC    time.Time
	DurationSeconds int
	Description     string
}

type Tombstone struct {
	ID              string
	IssueKey        string
	StartedAtUTC    time.Time
	DurationSeconds int
	Description     string
	DeletedAt       time.Time
}

type DeleteResult struct {
	ID         string
	IssueKey   string
	DeletedAt  time.Time
	HardDelete bool
}

type ListFilters struct {
	Issue        string
	Today        bool
	Yesterday    bool
	CurrentWeek  bool
	LastWeek     bool
	CurrentMonth bool
	LastMonth    bool
	From         string
	To           string
	OnlyDeleted  bool
	Fields       []string
}

type SearchInput struct {
	Query string
	ListFilters
}

type EffectiveFilters struct {
	IssueKey    *string
	From        *time.Time
	To          *time.Time
	Timezone    string
	OnlyDeleted bool
	Fields      []string
}

type AddInput struct {
	IssueKey    string
	Started     string
	StartedUTC  string
	Duration    string
	Description string
	Force       bool
}

type PatchInput struct {
	IssueKey    *string
	Started     *string
	StartedUTC  *string
	Duration    *string
	Description *string
	Force       bool
}

type DeleteBatchResult struct {
	Filters    EffectiveFilters
	Items      []LocalWorklog
	Deleted    []string
	DryRun     bool
	HardDelete bool
}

type RestorePreviewItem struct {
	Tombstone Tombstone
	Record    LocalWorklog
}

type RestoreBatchResult struct {
	Filters  EffectiveFilters
	Items    []RestorePreviewItem
	Restored []string
	DryRun   bool
	Force    bool
}

type ConflictDetail struct {
	Reason             string      `json:"reason"`
	Attempted          RecordView  `json:"attempted"`
	ConflictingIDs     []string    `json:"conflicting_ids"`
	ConflictingWindows [][2]string `json:"conflicting_windows,omitempty"`
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type RecordView struct {
	ID              string `json:"id,omitempty"`
	IssueKey        string `json:"issue_key"`
	StartedAt       string `json:"started_at"`
	StartedAtUTC    string `json:"started_at_utc"`
	DurationSeconds int    `json:"duration_seconds"`
	Description     string `json:"description"`
}

type ValidationError struct {
	Issues   []ValidationIssue
	Conflict *ConflictDetail
}

func (e ValidationError) Error() string {
	if e.Conflict != nil {
		return "worklog conflict"
	}
	if len(e.Issues) == 0 {
		return "validation failed"
	}
	return e.Issues[0].Message
}

func (e ValidationError) Unwrap() error {
	if e.Conflict != nil {
		return ErrConflict
	}
	return ErrValidation
}

type Service struct {
	store *sqlitestore.Store
	now   func() time.Time
}

func NewService(store *sqlitestore.Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

func (s *Service) List(cfg config.EffectiveConfig, filters ListFilters) ([]LocalWorklog, []Tombstone, EffectiveFilters, error) {
	if !hasListTimeSelector(filters) {
		return nil, nil, EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "worklogs list requires at least one time selector"}}}
	}

	effective, err := normalizeListFilters(cfg, filters, filters.OnlyDeleted)
	if err != nil {
		return nil, nil, EffectiveFilters{}, err
	}

	if filters.OnlyDeleted {
		items, err := s.listDeleted(effective)
		return nil, items, effective, err
	}

	items, err := s.listActive(effective)
	return items, nil, effective, err
}

func (s *Service) Search(cfg config.EffectiveConfig, input SearchInput) ([]LocalWorklog, []Tombstone, EffectiveFilters, string, error) {
	effective, err := normalizeListFilters(cfg, input.ListFilters, input.OnlyDeleted)
	if err != nil {
		return nil, nil, EffectiveFilters{}, "", err
	}

	query, err := normalizeSearchQuery(input.Query)
	if err != nil {
		return nil, nil, EffectiveFilters{}, "", err
	}

	if input.OnlyDeleted {
		items, err := s.searchDeleted(effective, query)
		return nil, items, effective, query, err
	}

	items, err := s.searchActive(effective, query)
	return items, nil, effective, query, err
}

func (s *Service) Show(id string) (LocalWorklog, error) {
	row := s.store.DB().QueryRow(`SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs WHERE id = ?`, id)
	worklog, err := scanWorklog(row)
	if err == nil {
		return worklog, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return LocalWorklog{}, ErrNotFound
	}
	return LocalWorklog{}, err
}

func (s *Service) PreviewAdd(cfg config.EffectiveConfig, input AddInput) (LocalWorklog, error) {
	return s.prepareAddCandidate(cfg, input)
}

func (s *Service) Add(cfg config.EffectiveConfig, input AddInput) (LocalWorklog, error) {
	candidate, err := s.prepareAddCandidate(cfg, input)
	if err != nil {
		return LocalWorklog{}, err
	}

	now := s.now().UTC()
	worklog := LocalWorklog{
		ID:              uuid.NewString(),
		IssueKey:        candidate.IssueKey,
		StartedAtUTC:    candidate.StartedAtUTC,
		DurationSeconds: candidate.DurationSeconds,
		Description:     candidate.Description,
	}

	_, err = s.store.DB().Exec(
		`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		worklog.ID,
		worklog.IssueKey,
		sqlitestore.RFC3339UTC(worklog.StartedAtUTC),
		worklog.DurationSeconds,
		worklog.Description,
		sqlitestore.RFC3339UTC(now),
		sqlitestore.RFC3339UTC(now),
	)
	if err != nil {
		return LocalWorklog{}, err
	}

	return worklog, nil
}

func (s *Service) prepareAddCandidate(cfg config.EffectiveConfig, input AddInput) (LocalWorklog, error) {
	candidate, err := buildCandidate(cfg, AddCandidateInput{
		IssueKey:    input.IssueKey,
		Started:     input.Started,
		StartedUTC:  input.StartedUTC,
		Duration:    input.Duration,
		Description: input.Description,
	})
	if err != nil {
		return LocalWorklog{}, err
	}

	if err := s.validateConflicts(cfg, candidate, "", input.Force); err != nil {
		return LocalWorklog{}, err
	}

	return candidate, nil
}

func (s *Service) Update(cfg config.EffectiveConfig, id string, patch PatchInput) (LocalWorklog, error) {
	current, err := s.Show(id)
	if err != nil {
		return LocalWorklog{}, err
	}

	if patch.IssueKey == nil && patch.Started == nil && patch.StartedUTC == nil && patch.Duration == nil && patch.Description == nil {
		return LocalWorklog{}, ValidationError{Issues: []ValidationIssue{{Field: "worklogs.update", Message: "at least one patch flag is required"}}}
	}
	if patch.Started != nil && patch.StartedUTC != nil {
		return LocalWorklog{}, ValidationError{Issues: []ValidationIssue{{Field: "started", Message: "cannot be combined with started_utc"}}}
	}

	input := AddCandidateInput{
		IssueKey:    current.IssueKey,
		Description: current.Description,
		StartedUTC:  sqlitestore.RFC3339UTC(current.StartedAtUTC),
		Duration:    fmt.Sprintf("%ds", current.DurationSeconds),
	}
	if patch.IssueKey != nil {
		input.IssueKey = *patch.IssueKey
	}
	if patch.Started != nil {
		input.Started = *patch.Started
		input.StartedUTC = ""
	}
	if patch.StartedUTC != nil {
		input.StartedUTC = *patch.StartedUTC
		input.Started = ""
	}
	if patch.Duration != nil {
		input.Duration = *patch.Duration
	}
	if patch.Description != nil {
		input.Description = *patch.Description
	}

	candidate, err := buildCandidate(cfg, input)
	if err != nil {
		return LocalWorklog{}, err
	}

	if err := s.validateConflicts(cfg, candidate, id, patch.Force); err != nil {
		return LocalWorklog{}, err
	}

	_, err = s.store.DB().Exec(
		`UPDATE worklogs SET issue_key = ?, started_at_utc = ?, duration_seconds = ?, description = ?, updated_at = ? WHERE id = ?`,
		candidate.IssueKey,
		sqlitestore.RFC3339UTC(candidate.StartedAtUTC),
		candidate.DurationSeconds,
		candidate.Description,
		sqlitestore.RFC3339UTC(s.now().UTC()),
		id,
	)
	if err != nil {
		return LocalWorklog{}, err
	}

	current.IssueKey = candidate.IssueKey
	current.StartedAtUTC = candidate.StartedAtUTC
	current.DurationSeconds = candidate.DurationSeconds
	current.Description = candidate.Description

	return current, nil
}

func (s *Service) Delete(id string, hardDelete bool) (DeleteResult, error) {
	current, err := s.Show(id)
	if err != nil {
		return DeleteResult{}, err
	}

	deletedAt := s.now().UTC()
	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return DeleteResult{}, err
	}

	if _, err := tx.Exec(`DELETE FROM worklogs WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return DeleteResult{}, err
	}
	if !hardDelete {
		if _, err := tx.Exec(
			`INSERT INTO worklog_tombstones(worklog_id, issue_key, started_at_utc, duration_seconds, description, deleted_at) VALUES(?, ?, ?, ?, ?, ?)`,
			current.ID,
			current.IssueKey,
			sqlitestore.RFC3339UTC(current.StartedAtUTC),
			current.DurationSeconds,
			current.Description,
			sqlitestore.RFC3339UTC(deletedAt),
		); err != nil {
			_ = tx.Rollback()
			return DeleteResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DeleteResult{}, err
	}

	return DeleteResult{
		ID:         current.ID,
		IssueKey:   current.IssueKey,
		DeletedAt:  deletedAt,
		HardDelete: hardDelete,
	}, nil
}

func (s *Service) DeleteBatch(cfg config.EffectiveConfig, filters ListFilters, dryRun bool, hardDelete bool) (DeleteBatchResult, error) {
	if filters.OnlyDeleted {
		return DeleteBatchResult{}, ValidationError{Issues: []ValidationIssue{{Field: "only_deleted", Message: "is not valid for batch delete"}}}
	}

	effective, err := normalizeListFilters(cfg, filters, false)
	if err != nil {
		return DeleteBatchResult{}, err
	}
	if effective.IssueKey == nil && effective.From == nil && effective.To == nil {
		return DeleteBatchResult{}, ValidationError{Issues: []ValidationIssue{{Field: "delete", Message: "batch delete requires at least one selector"}}}
	}

	items, err := s.listActive(effective)
	if err != nil {
		return DeleteBatchResult{}, err
	}

	result := DeleteBatchResult{
		Filters:    effective,
		Items:      items,
		DryRun:     dryRun,
		HardDelete: hardDelete,
	}
	if dryRun || len(items) == 0 {
		return result, nil
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return DeleteBatchResult{}, err
	}

	deletedAt := sqlitestore.RFC3339UTC(s.now().UTC())
	result.Deleted = make([]string, 0, len(items))
	for _, item := range items {
		if _, err := tx.Exec(`DELETE FROM worklogs WHERE id = ?`, item.ID); err != nil {
			_ = tx.Rollback()
			return DeleteBatchResult{}, err
		}
		if !hardDelete {
			if _, err := tx.Exec(
				`INSERT INTO worklog_tombstones(worklog_id, issue_key, started_at_utc, duration_seconds, description, deleted_at) VALUES(?, ?, ?, ?, ?, ?)`,
				item.ID,
				item.IssueKey,
				sqlitestore.RFC3339UTC(item.StartedAtUTC),
				item.DurationSeconds,
				item.Description,
				deletedAt,
			); err != nil {
				_ = tx.Rollback()
				return DeleteBatchResult{}, err
			}
		}
		result.Deleted = append(result.Deleted, item.ID)
	}

	if err := tx.Commit(); err != nil {
		return DeleteBatchResult{}, err
	}

	return result, nil
}

func (s *Service) RestoreBatch(cfg config.EffectiveConfig, filters ListFilters, dryRun bool, force bool) (RestoreBatchResult, error) {
	if filters.OnlyDeleted {
		return RestoreBatchResult{}, ValidationError{Issues: []ValidationIssue{{Field: "only_deleted", Message: "is not valid for restore"}}}
	}
	if !hasListTimeSelector(filters) {
		return RestoreBatchResult{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "worklogs restore requires at least one time selector"}}}
	}

	effective, err := normalizeListFilters(cfg, filters, false)
	if err != nil {
		return RestoreBatchResult{}, err
	}

	tombstones, err := s.listDeleted(effective)
	if err != nil {
		return RestoreBatchResult{}, err
	}

	previewItems := make([]RestorePreviewItem, 0, len(tombstones))
	restoreRows := make([]LocalWorklog, 0, len(tombstones))
	for _, item := range tombstones {
		record := LocalWorklog{
			ID:              item.ID,
			IssueKey:        item.IssueKey,
			StartedAtUTC:    item.StartedAtUTC,
			DurationSeconds: item.DurationSeconds,
			Description:     item.Description,
		}
		restoreRows = append(restoreRows, record)
		previewItems = append(previewItems, RestorePreviewItem{
			Tombstone: item,
			Record:    record,
		})
	}

	result := RestoreBatchResult{
		Filters: effective,
		Items:   previewItems,
		DryRun:  dryRun,
		Force:   force,
	}
	if dryRun || len(restoreRows) == 0 {
		return result, nil
	}

	if err := s.validateRestoreConflicts(cfg, restoreRows, force); err != nil {
		return RestoreBatchResult{}, err
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return RestoreBatchResult{}, err
	}

	result.Restored = make([]string, 0, len(restoreRows))
	now := sqlitestore.RFC3339UTC(s.now().UTC())
	for _, item := range restoreRows {
		if _, err := tx.Exec(
			`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			item.ID,
			item.IssueKey,
			sqlitestore.RFC3339UTC(item.StartedAtUTC),
			item.DurationSeconds,
			item.Description,
			now,
			now,
		); err != nil {
			_ = tx.Rollback()
			return RestoreBatchResult{}, err
		}
		if _, err := tx.Exec(`DELETE FROM worklog_tombstones WHERE worklog_id = ?`, item.ID); err != nil {
			_ = tx.Rollback()
			return RestoreBatchResult{}, err
		}
		result.Restored = append(result.Restored, item.ID)
	}

	if err := tx.Commit(); err != nil {
		return RestoreBatchResult{}, err
	}

	return result, nil
}

type AddCandidateInput struct {
	IssueKey    string
	Started     string
	StartedUTC  string
	Duration    string
	Description string
}

func buildCandidate(cfg config.EffectiveConfig, input AddCandidateInput) (LocalWorklog, error) {
	issues := make([]ValidationIssue, 0)
	if !issueKeyPattern.MatchString(input.IssueKey) {
		issues = append(issues, ValidationIssue{Field: "issue", Message: "must match <PROJECTKEY>-<NUMBER>"})
	}

	startedCount := 0
	if input.Started != "" {
		startedCount++
	}
	if input.StartedUTC != "" {
		startedCount++
	}
	if startedCount != 1 {
		issues = append(issues, ValidationIssue{Field: "started", Message: "provide exactly one of started or started_utc"})
	}

	startedAtUTC, startErr := parseStartedAt(cfg, input.Started, input.StartedUTC)
	if startErr != nil {
		issues = append(issues, ValidationIssue{Field: "started", Message: startErr.Error()})
	}

	durationSeconds, durationErr := parseDuration(input.Duration, cfg.MinimumDurationSeconds)
	if durationErr != nil {
		issues = append(issues, ValidationIssue{Field: "duration", Message: durationErr.Error()})
	}

	description, descriptionErr := normalizeDescription(input.Description)
	if descriptionErr != nil {
		issues = append(issues, ValidationIssue{Field: "description", Message: descriptionErr.Error()})
	}

	if len(issues) > 0 {
		return LocalWorklog{}, ValidationError{Issues: issues}
	}

	return LocalWorklog{
		IssueKey:        input.IssueKey,
		StartedAtUTC:    startedAtUTC,
		DurationSeconds: durationSeconds,
		Description:     description,
	}, nil
}

func (s *Service) validateConflicts(cfg config.EffectiveConfig, candidate LocalWorklog, excludeID string, force bool) error {
	if force {
		return nil
	}

	existing, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return err
	}

	conflictingIDs := make([]string, 0)
	conflictingWindows := make([][2]string, 0)
	reason := ""
	for _, item := range existing {
		if item.ID == excludeID {
			continue
		}
		if isDuplicate(candidate, item) {
			reason = "duplicate"
			conflictingIDs = append(conflictingIDs, item.ID)
			continue
		}
		if overlaps(candidate, item) {
			if reason == "" {
				reason = "overlap"
			}
			conflictingIDs = append(conflictingIDs, item.ID)
			conflictingWindows = append(conflictingWindows, [2]string{
				sqlitestore.RFC3339UTC(item.StartedAtUTC),
				sqlitestore.RFC3339UTC(item.StartedAtUTC.Add(time.Duration(item.DurationSeconds) * time.Second)),
			})
		}
	}

	if len(conflictingIDs) == 0 {
		return nil
	}

	return ValidationError{
		Conflict: &ConflictDetail{
			Reason:             reason,
			Attempted:          ToRecordView(candidate, cfg.Location),
			ConflictingIDs:     conflictingIDs,
			ConflictingWindows: conflictingWindows,
		},
	}
}

func (s *Service) validateRestoreConflicts(cfg config.EffectiveConfig, candidates []LocalWorklog, force bool) error {
	if force || len(candidates) == 0 {
		return nil
	}

	existing, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return err
	}

	restoreSet := make([]LocalWorklog, 0, len(existing)+len(candidates))
	restoreSet = append(restoreSet, existing...)
	for index, candidate := range candidates {
		conflictingIDs := make([]string, 0)
		conflictingWindows := make([][2]string, 0)
		reason := ""
		for _, item := range restoreSet {
			if isDuplicate(candidate, item) {
				reason = "duplicate"
				conflictingIDs = append(conflictingIDs, item.ID)
				continue
			}
			if overlaps(candidate, item) {
				if reason == "" {
					reason = "overlap"
				}
				conflictingIDs = append(conflictingIDs, item.ID)
				conflictingWindows = append(conflictingWindows, [2]string{
					sqlitestore.RFC3339UTC(item.StartedAtUTC),
					sqlitestore.RFC3339UTC(item.StartedAtUTC.Add(time.Duration(item.DurationSeconds) * time.Second)),
				})
			}
		}
		if len(conflictingIDs) > 0 {
			return ValidationError{
				Conflict: &ConflictDetail{
					Reason:             reason,
					Attempted:          ToRecordView(candidate, cfg.Location),
					ConflictingIDs:     conflictingIDs,
					ConflictingWindows: conflictingWindows,
				},
			}
		}
		restoreSet = append(restoreSet, candidates[index])
	}

	return nil
}

func (s *Service) listActive(filters EffectiveFilters) ([]LocalWorklog, error) {
	query := `SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs`
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	query += where + ` ORDER BY started_at_utc DESC, id ASC`

	rows, err := s.store.DB().Query(query, args...)
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

func (s *Service) listDeleted(filters EffectiveFilters) ([]Tombstone, error) {
	query := `SELECT worklog_id, issue_key, started_at_utc, duration_seconds, description, deleted_at FROM worklog_tombstones`
	args := make([]any, 0)
	where := buildWhereClause(filters, true, &args)
	query += where + ` ORDER BY started_at_utc DESC, worklog_id ASC`

	rows, err := s.store.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Tombstone, 0)
	for rows.Next() {
		item, err := scanTombstone(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Service) searchActive(filters EffectiveFilters, query string) ([]LocalWorklog, error) {
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	args = append(args, literalSubstringPattern(query))
	statement := `SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs` + where + `description LIKE ? ESCAPE '\' COLLATE NOCASE ORDER BY started_at_utc DESC, id ASC`

	rows, err := s.store.DB().Query(statement, args...)
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

func (s *Service) searchDeleted(filters EffectiveFilters, query string) ([]Tombstone, error) {
	args := make([]any, 0)
	where := buildWhereClause(filters, true, &args)
	if where == "" {
		where = " WHERE "
	} else {
		where += " AND "
	}
	args = append(args, literalSubstringPattern(query))
	statement := `SELECT worklog_id, issue_key, started_at_utc, duration_seconds, description, deleted_at FROM worklog_tombstones` + where + `description LIKE ? ESCAPE '\' COLLATE NOCASE ORDER BY started_at_utc DESC, worklog_id ASC`

	rows, err := s.store.DB().Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Tombstone, 0)
	for rows.Next() {
		item, err := scanTombstone(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func normalizeListFilters(cfg config.EffectiveConfig, filters ListFilters, allowDeleted bool) (EffectiveFilters, error) {
	return normalizeListFiltersAt(cfg, filters, allowDeleted, time.Now)
}

func normalizeListFiltersAt(cfg config.EffectiveConfig, filters ListFilters, allowDeleted bool, now func() time.Time) (EffectiveFilters, error) {
	if err := validateDateWindowSelection(filters.Today, filters.Yesterday, filters.CurrentWeek, filters.LastWeek, filters.CurrentMonth, filters.LastMonth, filters.From != "" || filters.To != ""); err != nil {
		return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: err.Error()}}}
	}

	fields, err := normalizeFields(filters.Fields, allowDeleted)
	if err != nil {
		return EffectiveFilters{}, err
	}

	effective := EffectiveFilters{
		OnlyDeleted: filters.OnlyDeleted,
		Fields:      fields,
	}

	if cfg.LocalTimezoneConfig != nil {
		effective.Timezone = *cfg.LocalTimezoneConfig
	} else {
		effective.Timezone = cfg.Location.String()
	}

	if filters.Issue != "" {
		if !issueKeyPattern.MatchString(filters.Issue) {
			return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "issue", Message: "must match <PROJECTKEY>-<NUMBER>"}}}
		}
		issue := filters.Issue
		effective.IssueKey = &issue
	}

	switch {
	case filters.Today:
		current := now().In(cfg.Location)
		from, to := dayBounds(current, cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.Yesterday:
		current := now().In(cfg.Location).AddDate(0, 0, -1)
		from, to := dayBounds(current, cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.CurrentWeek:
		from, to := weekBounds(now().In(cfg.Location), cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.LastWeek:
		from, to := weekBounds(now().In(cfg.Location).AddDate(0, 0, -7), cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.CurrentMonth:
		from, to := monthBounds(now().In(cfg.Location), cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.LastMonth:
		from, to := monthBounds(now().In(cfg.Location).AddDate(0, -1, 0), cfg.Location)
		effective.From = &from
		effective.To = &to
	case filters.From != "" || filters.To != "":
		var fromValue *time.Time
		var toValue *time.Time
		if filters.From != "" {
			parsed, err := parseDateSelector(filters.From, cfg.Location)
			if err != nil {
				return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "from", Message: err.Error()}}}
			}
			start, _ := dayBounds(parsed, cfg.Location)
			fromValue = &start
		}
		if filters.To != "" {
			parsed, err := parseDateSelector(filters.To, cfg.Location)
			if err != nil {
				return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "to", Message: err.Error()}}}
			}
			_, end := dayBounds(parsed, cfg.Location)
			toValue = &end
		}
		if fromValue != nil && toValue != nil && fromValue.After(*toValue) {
			effective.From = toValue
			effective.To = fromValue
		} else {
			effective.From = fromValue
			effective.To = toValue
		}
	}

	return effective, nil
}

func validateDateWindowSelection(today, yesterday, currentWeek, lastWeek, currentMonth, lastMonth, hasRange bool) error {
	shortcuts := 0
	for _, selected := range []bool{today, yesterday, currentWeek, lastWeek, currentMonth, lastMonth} {
		if selected {
			shortcuts++
		}
	}
	if shortcuts > 1 {
		return errors.New("today, yesterday, current-week, last-week, current-month, and last-month are mutually exclusive")
	}
	if shortcuts > 0 && hasRange {
		return errors.New("today, yesterday, current-week, last-week, current-month, and last-month cannot be combined with from or to")
	}
	return nil
}

func hasListTimeSelector(filters ListFilters) bool {
	return filters.Today || filters.Yesterday || filters.CurrentWeek || filters.LastWeek || filters.CurrentMonth || filters.LastMonth || filters.From != "" || filters.To != ""
}

func normalizeSearchQuery(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ValidationError{Issues: []ValidationIssue{{Field: "query", Message: "query is required"}}}
	}
	return trimmed, nil
}

func literalSubstringPattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}

func normalizeFields(fields []string, allowDeleted bool) ([]string, error) {
	if len(fields) == 0 {
		return nil, nil
	}

	allowed := []string{"id", "issue_key", "started_at", "started_at_utc", "duration_seconds", "description"}
	if allowDeleted {
		allowed = []string{"id", "issue_key", "deleted_at"}
	}

	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, ok := seen[field]; ok {
			return nil, ValidationError{Issues: []ValidationIssue{{Field: "fields", Message: "contains duplicate field names"}}}
		}
		if !slices.Contains(allowed, field) {
			return nil, ValidationError{Issues: []ValidationIssue{{Field: "fields", Message: fmt.Sprintf("field %q is not valid for this record type", field)}}}
		}
		seen[field] = struct{}{}
		normalized = append(normalized, field)
	}

	return normalized, nil
}

func parseStartedAt(cfg config.EffectiveConfig, localValue, utcValue string) (time.Time, error) {
	switch {
	case localValue != "":
		return parseLocalTimestamp(localValue, cfg.Location)
	case utcValue != "":
		parsed, err := time.Parse(time.RFC3339, utcValue)
		if err != nil || parsed.Location() != time.UTC {
			return time.Time{}, errors.New("started_utc must be RFC3339 UTC")
		}
		return parsed.UTC(), nil
	default:
		return time.Time{}, errors.New("started time is required")
	}
}

func parseLocalTimestamp(value string, location *time.Location) (time.Time, error) {
	parts := strings.Split(value, "T")
	if len(parts) != 2 {
		return time.Time{}, errors.New("started must use YYYY-MM-DDTHH:MM or relative-day grammar")
	}

	dateValue, err := parseDateSelector(parts[0], location)
	if err != nil {
		return time.Time{}, err
	}

	clockParts := strings.Split(parts[1], ":")
	if len(clockParts) != 2 {
		return time.Time{}, errors.New("time must use HH:MM")
	}

	hour, minute := 0, 0
	if _, err := fmt.Sscanf(parts[1], "%02d:%02d", &hour, &minute); err != nil {
		return time.Time{}, errors.New("time must use HH:MM")
	}

	candidates := matchingLocalCandidates(dateValue.Year(), dateValue.Month(), dateValue.Day(), hour, minute, location)
	switch len(candidates) {
	case 0:
		return time.Time{}, errors.New("local time does not exist in the effective timezone")
	case 1:
		return candidates[0].UTC(), nil
	default:
		return time.Time{}, errors.New("local time is ambiguous in the effective timezone")
	}
}

func matchingLocalCandidates(year int, month time.Month, day, hour, minute int, location *time.Location) []time.Time {
	guess := time.Date(year, month, day, hour, minute, 0, 0, location)
	start := guess.UTC().Add(-4 * time.Hour)
	end := guess.UTC().Add(4 * time.Hour)

	candidates := make([]time.Time, 0, 2)
	for cursor := start; !cursor.After(end); cursor = cursor.Add(time.Minute) {
		local := cursor.In(location)
		if local.Year() == year && local.Month() == month && local.Day() == day && local.Hour() == hour && local.Minute() == minute {
			candidates = append(candidates, cursor)
		}
	}

	return candidates
}

func parseDateSelector(value string, location *time.Location) (time.Time, error) {
	switch value {
	case "today":
		return time.Now().In(location), nil
	case "yesterday":
		return time.Now().In(location).AddDate(0, 0, -1), nil
	case "tomorrow":
		return time.Now().In(location).AddDate(0, 0, 1), nil
	}

	if strings.HasSuffix(value, "d") && (strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-")) {
		offset := 0
		if _, err := fmt.Sscanf(strings.TrimSuffix(value, "d"), "%d", &offset); err != nil {
			return time.Time{}, errors.New("invalid relative day offset")
		}
		return time.Now().In(location).AddDate(0, 0, offset), nil
	}

	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, errors.New("date must use YYYY-MM-DD, today, yesterday, tomorrow, +Nd, or -Nd")
	}

	return parsed, nil
}

func dayBounds(date time.Time, location *time.Location) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	end := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, location)
	return start, end
}

func weekBounds(date time.Time, location *time.Location) (time.Time, time.Time) {
	local := date.In(location)
	daysSinceMonday := (int(local.Weekday()) + 6) % 7
	weekStartDate := local.AddDate(0, 0, -daysSinceMonday)
	start, _ := dayBounds(weekStartDate, location)
	_, end := dayBounds(weekStartDate.AddDate(0, 0, 6), location)
	return start, end
}

func monthBounds(date time.Time, location *time.Location) (time.Time, time.Time) {
	local := date.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	nextMonth := start.AddDate(0, 1, 0)
	end := nextMonth.Add(-time.Second)
	return start, end
}

func parseDuration(value string, minimumSeconds int) (int, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, errors.New("duration must be a valid Go duration")
	}
	if duration <= 0 {
		return 0, errors.New("duration must be positive")
	}
	if duration%time.Second != 0 {
		return 0, errors.New("duration must normalize to whole seconds")
	}

	seconds := int(duration / time.Second)
	if seconds < minimumSeconds {
		return 0, fmt.Errorf("duration must be at least %d seconds", minimumSeconds)
	}
	return seconds, nil
}

func normalizeDescription(value string) (string, error) {
	normalized := strings.Join(strings.Fields(value), " ")
	if normalized == "" {
		return "", errors.New("description must be non-empty")
	}
	return normalized, nil
}

func isDuplicate(a, b LocalWorklog) bool {
	return a.IssueKey == b.IssueKey &&
		a.StartedAtUTC.Equal(b.StartedAtUTC) &&
		a.DurationSeconds == b.DurationSeconds &&
		a.Description == b.Description
}

func overlaps(a, b LocalWorklog) bool {
	aEnd := a.StartedAtUTC.Add(time.Duration(a.DurationSeconds) * time.Second)
	bEnd := b.StartedAtUTC.Add(time.Duration(b.DurationSeconds) * time.Second)
	return a.StartedAtUTC.Before(bEnd) && b.StartedAtUTC.Before(aEnd)
}

func scanWorklog(scanner interface{ Scan(dest ...any) error }) (LocalWorklog, error) {
	var worklog LocalWorklog
	var startedAt string
	if err := scanner.Scan(&worklog.ID, &worklog.IssueKey, &startedAt, &worklog.DurationSeconds, &worklog.Description); err != nil {
		return LocalWorklog{}, err
	}

	parsed, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return LocalWorklog{}, err
	}
	worklog.StartedAtUTC = parsed.UTC()

	return worklog, nil
}

func scanTombstone(scanner interface{ Scan(dest ...any) error }) (Tombstone, error) {
	var item Tombstone
	var startedAt string
	var deletedAt string
	if err := scanner.Scan(&item.ID, &item.IssueKey, &startedAt, &item.DurationSeconds, &item.Description, &deletedAt); err != nil {
		return Tombstone{}, err
	}

	parsedStart, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return Tombstone{}, err
	}
	parsedDeletedAt, err := time.Parse(time.RFC3339, deletedAt)
	if err != nil {
		return Tombstone{}, err
	}

	item.StartedAtUTC = parsedStart.UTC()
	item.DeletedAt = parsedDeletedAt.UTC()
	return item, nil
}

func buildWhereClause(filters EffectiveFilters, deleted bool, args *[]any) string {
	whereParts := make([]string, 0)
	if filters.IssueKey != nil {
		whereParts = append(whereParts, "issue_key = ?")
		*args = append(*args, *filters.IssueKey)
	}

	column := "started_at_utc"
	if deleted {
		column = "started_at_utc"
	}
	if filters.From != nil {
		whereParts = append(whereParts, column+" >= ?")
		*args = append(*args, sqlitestore.RFC3339UTC(filters.From.UTC()))
	}
	if filters.To != nil {
		whereParts = append(whereParts, column+" <= ?")
		*args = append(*args, sqlitestore.RFC3339UTC(filters.To.UTC()))
	}

	if len(whereParts) == 0 {
		return ""
	}

	return " WHERE " + strings.Join(whereParts, " AND ")
}

func ToRecordView(item LocalWorklog, location *time.Location) RecordView {
	return RecordView{
		ID:              item.ID,
		IssueKey:        item.IssueKey,
		StartedAt:       item.StartedAtUTC.In(location).Format(time.RFC3339),
		StartedAtUTC:    sqlitestore.RFC3339UTC(item.StartedAtUTC),
		DurationSeconds: item.DurationSeconds,
		Description:     item.Description,
	}
}
