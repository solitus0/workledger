package presets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

var (
	ErrNotFound   = errors.New("worklog preset not found")
	ErrConflict   = errors.New("worklog preset conflict")
	ErrValidation = errors.New("worklog preset validation failed")
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	clockPattern  = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
)

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct{ Issues []ValidationIssue }

func (e ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return ErrValidation.Error()
	}
	return e.Issues[0].Message
}

func (e ValidationError) Unwrap() error { return ErrValidation }

type Preset struct {
	ID              string
	Name            string
	IssueKey        string
	StartTime       string
	DurationSeconds int
	Description     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      *time.Time
	Revision        int64
}

type CreateInput struct {
	Name        string
	IssueKey    string
	StartTime   string
	Duration    string
	Description string
}

type PatchInput struct {
	Name             *string
	IssueKey         *string
	StartTime        *string
	Duration         *string
	Description      *string
	ExpectedRevision int64
}

type ApplyInput struct {
	Date        string
	IssueKey    *string
	StartTime   *string
	Duration    *string
	Description *string
	DryRun      bool
	Force       bool
}

type DeleteResult struct {
	Name      string
	DeletedAt time.Time
}

type Service struct {
	store    *sqlitestore.Store
	worklogs *worklogs.Service
	now      func() time.Time
}

func NewService(store *sqlitestore.Store) *Service {
	return &Service{store: store, worklogs: worklogs.NewService(store), now: time.Now}
}

func (s *Service) Create(ctx context.Context, cfg config.EffectiveConfig, input CreateInput) (Preset, error) {
	name, issue, start, seconds, description, err := s.normalize(cfg, input.Name, input.IssueKey, input.StartTime, input.Duration, input.Description)
	if err != nil {
		return Preset{}, err
	}
	now := s.now().UTC()
	preset := Preset{ID: uuid.NewString(), Name: name, IssueKey: issue, StartTime: start, DurationSeconds: seconds, Description: description, CreatedAt: now, UpdatedAt: now, Revision: 1}
	_, err = s.store.DB().ExecContext(ctx, `INSERT INTO worklog_presets(id, name, issue_key, start_time, duration_seconds, description, created_at, updated_at, revision) VALUES(?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		preset.ID, preset.Name, preset.IssueKey, preset.StartTime, preset.DurationSeconds, preset.Description, sqlitestore.RFC3339UTC(now), sqlitestore.RFC3339UTC(now))
	if uniqueConstraint(err) {
		return Preset{}, ValidationError{Issues: []ValidationIssue{{Field: "name", Message: "preset name already exists"}}}
	}
	return preset, err
}

func (s *Service) List(ctx context.Context, prefix string, limit int) ([]Preset, error) {
	if limit <= 0 {
		limit = 10000
	}
	query := `SELECT id, name, issue_key, start_time, duration_seconds, description, created_at, updated_at, last_used_at, revision FROM worklog_presets`
	args := make([]any, 0, 3)
	trimmedPrefix := strings.ToLower(strings.TrimSpace(prefix))
	if trimmedPrefix != "" {
		lower, upper := sqlitestore.PrefixRange(trimmedPrefix)
		query += ` WHERE name >= ? AND name < ?`
		args = append(args, lower, upper)
	}
	query += ` ORDER BY last_used_at IS NULL, last_used_at DESC, name LIMIT ?`
	args = append(args, limit)
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Preset, 0)
	for rows.Next() {
		item, err := scanPreset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Show(ctx context.Context, name string) (Preset, error) {
	return s.show(ctx, strings.TrimSpace(name))
}

func (s *Service) show(ctx context.Context, name string) (Preset, error) {
	return showPresetByName(ctx, s.store.DB(), name)
}

type presetQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func showPresetByName(ctx context.Context, queryer presetQueryer, name string) (Preset, error) {
	return scanExistingPreset(queryer.QueryRowContext(ctx, `SELECT id, name, issue_key, start_time, duration_seconds, description, created_at, updated_at, last_used_at, revision FROM worklog_presets WHERE name = ?`, name))
}

func showPresetByID(ctx context.Context, queryer presetQueryer, id string) (Preset, error) {
	return scanExistingPreset(queryer.QueryRowContext(ctx, `SELECT id, name, issue_key, start_time, duration_seconds, description, created_at, updated_at, last_used_at, revision FROM worklog_presets WHERE id = ?`, id))
}

func scanExistingPreset(row *sql.Row) (Preset, error) {
	item, err := scanPreset(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Preset{}, ErrNotFound
	}
	return item, err
}

func (s *Service) Update(ctx context.Context, cfg config.EffectiveConfig, currentName string, patch PatchInput) (Preset, error) {
	if patch.Name == nil && patch.IssueKey == nil && patch.StartTime == nil && patch.Duration == nil && patch.Description == nil {
		return Preset{}, ValidationError{Issues: []ValidationIssue{{Field: "fields", Message: "at least one update field is required"}}}
	}
	current, err := s.show(ctx, strings.TrimSpace(currentName))
	if err != nil {
		return Preset{}, err
	}
	name, issue, start, duration, description := current.Name, current.IssueKey, current.StartTime, fmt.Sprintf("%ds", current.DurationSeconds), current.Description
	if patch.Name != nil {
		name = *patch.Name
	}
	if patch.IssueKey != nil {
		issue = *patch.IssueKey
	}
	if patch.StartTime != nil {
		start = *patch.StartTime
	}
	if patch.Duration != nil {
		duration = *patch.Duration
	}
	if patch.Description != nil {
		description = *patch.Description
	}
	name, issue, start, seconds, description, err := s.normalize(cfg, name, issue, start, duration, description)
	if err != nil {
		return Preset{}, err
	}
	expected := current.Revision
	if patch.ExpectedRevision > 0 {
		expected = patch.ExpectedRevision
	}
	now := s.now().UTC()
	result, err := s.store.DB().ExecContext(ctx, `UPDATE worklog_presets SET name = ?, issue_key = ?, start_time = ?, duration_seconds = ?, description = ?, updated_at = ?, revision = revision + 1 WHERE id = ? AND revision = ?`,
		name, issue, start, seconds, description, sqlitestore.RFC3339UTC(now), current.ID, expected)
	if uniqueConstraint(err) {
		return Preset{}, ValidationError{Issues: []ValidationIssue{{Field: "name", Message: "preset name already exists"}}}
	}
	if err != nil {
		return Preset{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Preset{}, err
	}
	if affected == 0 {
		return Preset{}, ErrConflict
	}
	return s.show(ctx, name)
}

func (s *Service) Delete(ctx context.Context, name string, expectedRevision int64) (DeleteResult, error) {
	current, err := s.show(ctx, strings.TrimSpace(name))
	if err != nil {
		return DeleteResult{}, err
	}
	if expectedRevision <= 0 {
		expectedRevision = current.Revision
	}
	result, err := s.store.DB().ExecContext(ctx, `DELETE FROM worklog_presets WHERE id = ? AND revision = ?`, current.ID, expectedRevision)
	if err != nil {
		return DeleteResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return DeleteResult{}, err
	}
	if affected == 0 {
		return DeleteResult{}, ErrConflict
	}
	return DeleteResult{Name: current.Name, DeletedAt: s.now().UTC()}, nil
}

func (s *Service) Apply(ctx context.Context, cfg config.EffectiveConfig, name string, input ApplyInput) (worklogs.AddResult, error) {
	name = strings.TrimSpace(name)
	if input.DryRun {
		preset, err := s.show(ctx, name)
		if err != nil {
			return worklogs.AddResult{}, err
		}
		add, err := s.presetAddInput(cfg, preset, input)
		if err != nil {
			return worklogs.AddResult{}, err
		}
		return s.worklogs.PreviewAdd(ctx, cfg, add)
	}

	return s.applyImmediate(ctx, func(conn *sql.Conn) (worklogs.AddResult, error) {
		preset, err := showPresetByName(ctx, conn, name)
		if err != nil {
			return worklogs.AddResult{}, err
		}
		add, err := s.presetAddInput(cfg, preset, input)
		if err != nil {
			return worklogs.AddResult{}, err
		}
		return s.applyWorklogTx(ctx, cfg, conn, preset, add)
	})
}

// ApplyDraft atomically creates a worklog from a TUI preset draft and records
// preset recency only when the selected preset revision is still current.
func (s *Service) ApplyDraft(ctx context.Context, cfg config.EffectiveConfig, presetID string, expectedRevision int64, add worklogs.AddInput) (worklogs.AddResult, error) {
	if presetID == "" || expectedRevision <= 0 {
		return worklogs.AddResult{}, ErrConflict
	}
	return s.applyImmediate(ctx, func(conn *sql.Conn) (worklogs.AddResult, error) {
		preset, err := showPresetByID(ctx, conn, presetID)
		if errors.Is(err, ErrNotFound) || (err == nil && preset.Revision != expectedRevision) {
			return worklogs.AddResult{}, ErrConflict
		}
		if err != nil {
			return worklogs.AddResult{}, err
		}
		return s.applyWorklogTx(ctx, cfg, conn, preset, add)
	})
}

func (s *Service) presetAddInput(cfg config.EffectiveConfig, preset Preset, input ApplyInput) (worklogs.AddInput, error) {
	date, err := worklogs.ResolveLocalDateAt(cfg, strings.TrimSpace(input.Date), s.now)
	if err != nil {
		return worklogs.AddInput{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: err.Error()}}}
	}
	issue, start, duration, description := preset.IssueKey, preset.StartTime, fmt.Sprintf("%ds", preset.DurationSeconds), preset.Description
	if input.IssueKey != nil {
		issue = *input.IssueKey
	}
	if input.StartTime != nil {
		start = *input.StartTime
	}
	if input.Duration != nil {
		duration = *input.Duration
	}
	if input.Description != nil {
		description = *input.Description
	}
	if !clockPattern.MatchString(start) {
		return worklogs.AddInput{}, ValidationError{Issues: []ValidationIssue{{Field: "start", Message: "start must use HH:MM, e.g. 09:00"}}}
	}
	return worklogs.AddInput{IssueKey: issue, Started: date.Format("2006-01-02") + "T" + start, Duration: duration, Description: description, Force: input.Force}, nil
}

func (s *Service) applyImmediate(ctx context.Context, apply func(*sql.Conn) (worklogs.AddResult, error)) (worklogs.AddResult, error) {
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return worklogs.AddResult{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return worklogs.AddResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	result, err := apply(conn)
	if err != nil {
		return worklogs.AddResult{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return worklogs.AddResult{}, err
	}
	committed = true
	return result, nil
}

func (s *Service) applyWorklogTx(ctx context.Context, cfg config.EffectiveConfig, conn *sql.Conn, preset Preset, add worklogs.AddInput) (worklogs.AddResult, error) {
	result, err := s.worklogs.AddInImmediateTransaction(ctx, cfg, add, conn)
	if err != nil {
		return worklogs.AddResult{}, err
	}
	updated, err := conn.ExecContext(ctx, `UPDATE worklog_presets SET last_used_at = ? WHERE id = ? AND revision = ?`, sqlitestore.RFC3339UTC(s.now().UTC()), preset.ID, preset.Revision)
	if err != nil {
		return worklogs.AddResult{}, err
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return worklogs.AddResult{}, err
	}
	if affected != 1 {
		return worklogs.AddResult{}, ErrConflict
	}
	return result, nil
}

func (s *Service) normalize(cfg config.EffectiveConfig, name, issue, start, duration, description string) (string, string, string, int, string, error) {
	issues := make([]ValidationIssue, 0)
	name = strings.TrimSpace(name)
	start = strings.TrimSpace(start)
	if len(name) == 0 || len(name) > 64 || !slugPattern.MatchString(name) {
		issues = append(issues, ValidationIssue{Field: "name", Message: "name must be a lowercase slug of 1-64 alphanumeric characters separated by hyphens"})
	}
	if !clockPattern.MatchString(start) {
		issues = append(issues, ValidationIssue{Field: "start", Message: "start must use HH:MM, e.g. 09:00"})
	}
	if len(issues) > 0 {
		return "", "", "", 0, "", ValidationError{Issues: issues}
	}
	record, err := worklogs.NormalizeCoreFields(cfg, issue, duration, description)
	if err != nil {
		var worklogValidation worklogs.ValidationError
		if errors.As(err, &worklogValidation) {
			for _, item := range worklogValidation.Issues {
				issues = append(issues, ValidationIssue{Field: item.Field, Message: item.Message})
			}
			return "", "", "", 0, "", ValidationError{Issues: issues}
		}
		return "", "", "", 0, "", err
	}
	return name, record.IssueKey, start, record.DurationSeconds, record.Description, nil
}

type scanner interface{ Scan(...any) error }

func scanPreset(row scanner) (Preset, error) {
	var item Preset
	var created, updated string
	var lastUsed sql.NullString
	if err := row.Scan(&item.ID, &item.Name, &item.IssueKey, &item.StartTime, &item.DurationSeconds, &item.Description, &created, &updated, &lastUsed, &item.Revision); err != nil {
		return Preset{}, err
	}
	var err error
	if item.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return Preset{}, err
	}
	if item.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
		return Preset{}, err
	}
	if lastUsed.Valid {
		parsed, err := time.Parse(time.RFC3339, lastUsed.String)
		if err != nil {
			return Preset{}, err
		}
		item.LastUsedAt = &parsed
	}
	return item, nil
}

func uniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
