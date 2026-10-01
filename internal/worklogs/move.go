package worklogs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

// MovePreview contains same-day destinations with all other bookings fixed.
type MovePreview struct {
	Source    LocalWorklog
	Positions []time.Time
	Bookings  []LocalWorklog
	Settings  ContextSettings
}

// PreviewMove accepts revision zero only for an explicit refresh of the source.
func (s *Service) PreviewMove(ctx context.Context, cfg config.EffectiveConfig, id string, revision int64) (MovePreview, error) {
	return s.previewMove(ctx, cfg, id, revision, s.store.DB())
}

func (s *Service) previewMove(ctx context.Context, cfg config.EffectiveConfig, id string, revision int64, queryer sqlQueryer) (MovePreview, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at, revision FROM worklogs WHERE id = ?`, id)
	if err != nil {
		return MovePreview{}, err
	}
	if !rows.Next() {
		err := rows.Err()
		rows.Close()
		if err != nil {
			return MovePreview{}, err
		}
		return MovePreview{}, ErrNotFound
	}
	source, err := scanWorklog(rows)
	rows.Close()
	if err != nil {
		return MovePreview{}, err
	}
	if revision > 0 && source.Revision != revision {
		return MovePreview{}, ErrConflict
	}
	base, err := buildAddBaseCandidate(cfg, source.IssueKey, fmt.Sprintf("%ds", source.DurationSeconds), source.Description)
	if err != nil {
		return MovePreview{}, err
	}
	if base.DurationSeconds != source.DurationSeconds {
		return MovePreview{}, ValidationError{Issues: []ValidationIssue{{Field: "duration", Message: "edit the worklog duration to whole minutes before moving"}}}
	}
	window, err := normalizeWorkdayWindow(cfg, ContextInput{})
	if err != nil {
		return MovePreview{}, err
	}
	day := source.StartedAtUTC.In(cfg.Location)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, cfg.Location)
	active, err := s.listActiveOverlappingWithQueryer(ctx, day.UTC(), day.AddDate(0, 0, 1).UTC(), queryer)
	if err != nil {
		return MovePreview{}, err
	}
	result := MovePreview{Source: source, Settings: ContextSettings{Timezone: cfg.Location.String(), DayStart: window.dayStart, DayEnd: window.dayEnd, Lunch: window.lunch}}
	duration := time.Duration(source.DurationSeconds) * time.Second
	anchors := []time.Time{applyClock(day, cfg.Location, window.dayStartMinutes), applyClock(day, cfg.Location, window.dayEndMinutes).Add(-duration)}
	occupied := []localInterval{}
	for _, item := range active {
		if item.ID == id {
			continue
		}
		result.Bookings = append(result.Bookings, item)
		occupied = append(occupied, localInterval{Start: item.StartedAtUTC, End: worklogEnd(item), ID: item.ID})
		anchors = append(anchors, item.StartedAtUTC.Add(-duration), worklogEnd(item))
	}
	slots := buildFreeSlots(day, cfg.Location, window, occupied)
	for _, anchor := range anchors {
		start := anchor.UTC()
		if !start.Equal(start.Round(time.Minute)) {
			continue
		}
		local := start.In(cfg.Location)
		if local.Format("2006-01-02") != day.Format("2006-01-02") {
			continue
		}
		// Do not present ambiguous or nonexistent local wall-clock destinations.
		parsed, err := parseLocalTimestamp(local.Format("2006-01-02T15:04"), cfg.Location)
		if err != nil || !parsed.Equal(start) {
			continue
		}
		end := start.Add(duration)
		for _, slot := range slots {
			if !start.Before(slot.Start) && !end.After(slot.End) {
				result.Positions = append(result.Positions, start)
				break
			}
		}
	}
	slices.SortFunc(result.Positions, func(a, b time.Time) int { return a.Compare(b) })
	result.Positions = dedupeTimes(result.Positions)
	return result, nil
}

// Move rechecks the displayed destination inside the same transaction as its update.
func (s *Service) Move(ctx context.Context, cfg config.EffectiveConfig, id string, revision int64, destination time.Time) (LocalWorklog, error) {
	if revision <= 0 {
		return LocalWorklog{}, ErrConflict
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return LocalWorklog{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return LocalWorklog{}, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
	preview, err := s.previewMove(ctx, cfg, id, revision, conn)
	if err != nil {
		return LocalWorklog{}, err
	}
	if !slices.ContainsFunc(preview.Positions, func(position time.Time) bool { return position.Equal(destination) }) {
		return LocalWorklog{}, ErrPlacementChanged
	}
	source := preview.Source
	if source.StartedAtUTC.Equal(destination) {
		return source, nil
	}
	source.StartedAtUTC = destination.UTC()
	source.UpdatedAt = s.now().UTC()
	result, err := conn.ExecContext(ctx, `UPDATE worklogs SET started_at_utc = ?, updated_at = ?, revision = revision + 1 WHERE id = ? AND revision = ?`, sqlitestore.RFC3339UTC(source.StartedAtUTC), sqlitestore.RFC3339UTC(source.UpdatedAt), id, revision)
	if err != nil {
		return LocalWorklog{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return LocalWorklog{}, err
	}
	if count != 1 {
		return LocalWorklog{}, ErrConflict
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return LocalWorklog{}, err
	}
	source.Revision++
	return source, nil
}
