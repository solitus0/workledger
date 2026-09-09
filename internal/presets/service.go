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
	row := s.store.DB().QueryRowContext(ctx, `SELECT id, name, issue_key, start_time, duration_seconds, description, created_at, updated_at, last_used_at, revision FROM worklog_presets WHERE name = ?`, name)
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
	preset, err := s.show(ctx, strings.TrimSpace(name))
	if err != nil {
		return worklogs.AddResult{}, err
	}
	date, err := worklogs.ResolveLocalDateAt(cfg, strings.TrimSpace(input.Date), s.now)
	if err != nil {
		return worklogs.AddResult{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: err.Error()}}}
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
		return worklogs.AddResult{}, ValidationError{Issues: []ValidationIssue{{Field: "start", Message: "start must use HH:MM, e.g. 09:00"}}}
	}
	add := worklogs.AddInput{IssueKey: issue, Started: date.Format("2006-01-02") + "T" + start, Duration: duration, Description: description, Force: input.Force}
	if input.DryRun {
		return s.worklogs.PreviewAdd(ctx, cfg, add)
	}
	result, err := s.worklogs.Add(ctx, cfg, add)
	if err != nil {
		return worklogs.AddResult{}, err
	}
	_, _ = s.store.DB().ExecContext(ctx, `UPDATE worklog_presets SET last_used_at = ? WHERE id = ?`, sqlitestore.RFC3339UTC(s.now().UTC()), preset.ID)
	return result, nil
}

func (s *Service) MarkUsed(ctx context.Context, id string) error {
	_, err := s.store.DB().ExecContext(ctx, `UPDATE worklog_presets SET last_used_at = ? WHERE id = ?`, sqlitestore.RFC3339UTC(s.now().UTC()), id)
	return err
}

func (s *Service) CheckRevision(ctx context.Context, id string, revision int64) error {
	var count int
	if err := s.store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM worklog_presets WHERE id = ? AND revision = ?`, id, revision).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrConflict
	}
	return nil
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
