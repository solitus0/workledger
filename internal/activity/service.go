package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

const RetentionLimit = 500

type Source string

const (
	SourceCLI Source = "cli"
	SourceTUI Source = "tui"
)

type State string

const (
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StatePartial   State = "partial"
	StateCanceled  State = "canceled"
)

type Entry struct {
	ID           string            `json:"id"`
	Source       Source            `json:"source"`
	Operation    string            `json:"operation"`
	Summary      string            `json:"summary"`
	Attributes   map[string]string `json:"attributes"`
	State        State             `json:"state"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   *time.Time        `json:"finished_at"`
	DurationMS   *int64            `json:"duration_ms"`
	ExitCode     *int              `json:"exit_code"`
	ErrorCode    string            `json:"error_code"`
	ErrorMessage string            `json:"error_message"`
}

type StartInput struct {
	ID         string
	Source     Source
	Operation  string
	Summary    string
	Attributes map[string]string
	StartedAt  time.Time
}

type FinishInput struct {
	State        State
	FinishedAt   time.Time
	ExitCode     *int
	ErrorCode    string
	ErrorMessage string
}

type ListFilters struct {
	Limit     int
	Source    Source
	State     State
	ExcludeID string
}

type Service struct {
	store *sqlitestore.Store
	now   func() time.Time
	id    func() string
}

func NewService(store *sqlitestore.Store) *Service {
	return &Service{store: store, now: time.Now, id: uuid.NewString}
}

func (s *Service) Start(ctx context.Context, input StartInput) (Entry, error) {
	if !validSource(input.Source) {
		return Entry{}, fmt.Errorf("invalid activity source %q", input.Source)
	}
	operation := strings.TrimSpace(input.Operation)
	if operation == "" {
		return Entry{}, errors.New("activity operation is required")
	}
	started := input.StartedAt.UTC()
	if started.IsZero() {
		started = s.now().UTC()
	}
	id := input.ID
	if id == "" {
		id = s.id()
	}
	attributes := cloneAttributes(input.Attributes)
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return Entry{}, err
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO activity_entries(id, source, operation, summary, attributes_json, state, started_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id, input.Source, operation, strings.TrimSpace(input.Summary), string(encoded), StateRunning, sqlitestore.RFC3339UTC(started)); err != nil {
		return Entry{}, err
	}
	if err := prune(ctx, tx); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Source: input.Source, Operation: operation, Summary: strings.TrimSpace(input.Summary), Attributes: attributes, State: StateRunning, StartedAt: started}, nil
}

func (s *Service) Finish(ctx context.Context, id string, input FinishInput) (Entry, error) {
	if input.State == StateRunning || !validState(input.State) {
		return Entry{}, fmt.Errorf("invalid terminal activity state %q", input.State)
	}
	current, err := s.find(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	finished := input.FinishedAt.UTC()
	if finished.IsZero() {
		finished = s.now().UTC()
	}
	if finished.Before(current.StartedAt) {
		finished = current.StartedAt
	}
	duration := finished.Sub(current.StartedAt).Milliseconds()
	result, err := s.store.DB().ExecContext(ctx, `UPDATE activity_entries SET state = ?, finished_at = ?, duration_ms = ?, exit_code = ?, error_code = ?, error_message = ? WHERE id = ? AND state = ?`,
		input.State, sqlitestore.RFC3339UTC(finished), duration, nullableInt(input.ExitCode), strings.TrimSpace(input.ErrorCode), strings.TrimSpace(input.ErrorMessage), id, StateRunning)
	if err != nil {
		return Entry{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Entry{}, err
	}
	if changed == 0 {
		return Entry{}, errors.New("activity entry is not running")
	}
	return s.find(ctx, id)
}

func (s *Service) List(ctx context.Context, filters ListFilters) ([]Entry, error) {
	limit := filters.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > RetentionLimit {
		return nil, fmt.Errorf("activity limit must be between 1 and %d", RetentionLimit)
	}
	if filters.Source != "" && !validSource(filters.Source) {
		return nil, fmt.Errorf("invalid activity source %q", filters.Source)
	}
	if filters.State != "" && !validState(filters.State) {
		return nil, fmt.Errorf("invalid activity state %q", filters.State)
	}
	query := `SELECT id, source, operation, summary, attributes_json, state, started_at, finished_at, duration_ms, exit_code, error_code, error_message FROM activity_entries WHERE 1 = 1`
	args := make([]any, 0, 4)
	if filters.Source != "" {
		query += ` AND source = ?`
		args = append(args, filters.Source)
	}
	if filters.State != "" {
		query += ` AND state = ?`
		args = append(args, filters.State)
	}
	if filters.ExcludeID != "" {
		query += ` AND id <> ?`
		args = append(args, filters.ExcludeID)
	}
	query += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Entry, 0)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) find(ctx context.Context, id string) (Entry, error) {
	return scanEntry(s.store.DB().QueryRowContext(ctx, `SELECT id, source, operation, summary, attributes_json, state, started_at, finished_at, duration_ms, exit_code, error_code, error_message FROM activity_entries WHERE id = ?`, id))
}

type scanner interface{ Scan(...any) error }

func scanEntry(row scanner) (Entry, error) {
	var item Entry
	var attributes, started string
	var finished sql.NullString
	var duration, exitCode sql.NullInt64
	if err := row.Scan(&item.ID, &item.Source, &item.Operation, &item.Summary, &attributes, &item.State, &started, &finished, &duration, &exitCode, &item.ErrorCode, &item.ErrorMessage); err != nil {
		return Entry{}, err
	}
	if err := json.Unmarshal([]byte(attributes), &item.Attributes); err != nil {
		return Entry{}, err
	}
	parsed, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return Entry{}, err
	}
	item.StartedAt = parsed
	if finished.Valid {
		parsed, err := time.Parse(time.RFC3339, finished.String)
		if err != nil {
			return Entry{}, err
		}
		item.FinishedAt = &parsed
	}
	if duration.Valid {
		value := duration.Int64
		item.DurationMS = &value
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	return item, nil
}

func prune(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM activity_entries WHERE id IN (SELECT id FROM activity_entries ORDER BY started_at DESC, id DESC LIMIT -1 OFFSET ?)`, RetentionLimit)
	return err
}

func cloneAttributes(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func validSource(value Source) bool { return value == SourceCLI || value == SourceTUI }

func validState(value State) bool {
	switch value {
	case StateRunning, StateSucceeded, StateFailed, StatePartial, StateCanceled:
		return true
	default:
		return false
	}
}
