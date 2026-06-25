package worklogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

type ShiftPreviewItem struct {
	ID                 string
	IssueKey           string
	StartedAtBefore    time.Time
	StartedAtAfter     time.Time
	StartedAtUTCBefore time.Time
	StartedAtUTCAfter  time.Time
	DurationSeconds    int
	Description        string
}

type ShiftResult struct {
	Filters      EffectiveFilters
	DeltaSeconds int
	Matched      int
	DryRun       bool
	PreviewItems []ShiftPreviewItem
	Items        []LocalWorklog
}

type RawApplyPayload struct {
	Adds []RawApplyAdd `json:"adds"`
}

type RawApplyAdd struct {
	IssueKey        string  `json:"issue_key"`
	StartedAt       *string `json:"started_at,omitempty"`
	StartedAtUTC    *string `json:"started_at_utc,omitempty"`
	DurationSeconds *int    `json:"duration_seconds,omitempty"`
	Description     string  `json:"description"`
}

type ApplyResultItem struct {
	Op     string
	Index  int
	ID     *string
	Record LocalWorklog
}

type ApplyResult struct {
	DryRun  bool
	Adds    int
	Items   []ApplyResultItem
	Records []LocalWorklog
}

func ParseRawApplyPayload(data []byte) (RawApplyPayload, error) {
	var payload RawApplyPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return RawApplyPayload{}, ValidationError{Issues: []ValidationIssue{{Field: "payload", Message: "must be valid JSON"}}}
	}
	if len(payload.Adds) == 0 {
		return RawApplyPayload{}, ValidationError{Issues: []ValidationIssue{{Field: "adds", Message: "must contain at least one add operation"}}}
	}
	return payload, nil
}

func (s *Service) Shift(cfg config.EffectiveConfig, filters ListFilters, by string, dryRun bool) (ShiftResult, error) {
	if !hasExplicitSelector(filters) {
		return ShiftResult{}, ValidationError{Issues: []ValidationIssue{{Field: "shift", Message: "requires at least one selector"}}}
	}

	effective, err := normalizeListFilters(cfg, filters, false)
	if err != nil {
		return ShiftResult{}, err
	}

	deltaSeconds, err := parseShiftDelta(by)
	if err != nil {
		return ShiftResult{}, ValidationError{Issues: []ValidationIssue{{Field: "by", Message: err.Error()}}}
	}

	matched, err := s.listActive(effective)
	if err != nil {
		return ShiftResult{}, err
	}
	if len(matched) == 0 {
		return ShiftResult{}, ErrNotFound
	}

	all, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return ShiftResult{}, err
	}

	shiftedByID := make(map[string]LocalWorklog, len(matched))
	previews := make([]ShiftPreviewItem, 0, len(matched))
	shifted := make([]LocalWorklog, 0, len(matched))
	for _, item := range matched {
		next := item
		next.StartedAtUTC = next.StartedAtUTC.Add(time.Duration(deltaSeconds) * time.Second)
		shiftedByID[item.ID] = next
		shifted = append(shifted, next)
		previews = append(previews, ShiftPreviewItem{
			ID:                 item.ID,
			IssueKey:           item.IssueKey,
			StartedAtBefore:    item.StartedAtUTC.In(cfg.Location),
			StartedAtAfter:     next.StartedAtUTC.In(cfg.Location),
			StartedAtUTCBefore: item.StartedAtUTC.UTC(),
			StartedAtUTCAfter:  next.StartedAtUTC.UTC(),
			DurationSeconds:    item.DurationSeconds,
			Description:        item.Description,
		})
	}

	finalSet := make([]LocalWorklog, 0, len(all))
	for _, item := range all {
		if shiftedItem, ok := shiftedByID[item.ID]; ok {
			finalSet = append(finalSet, shiftedItem)
			continue
		}
		finalSet = append(finalSet, item)
	}
	if err := validateShiftSet(finalSet, shiftedByID, cfg.Location); err != nil {
		return ShiftResult{}, err
	}

	result := ShiftResult{
		Filters:      effective,
		DeltaSeconds: deltaSeconds,
		Matched:      len(matched),
		DryRun:       dryRun,
		PreviewItems: previews,
		Items:        shifted,
	}
	if dryRun {
		return result, nil
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return ShiftResult{}, err
	}
	updatedAt := sqlitestore.RFC3339UTC(s.now().UTC())
	for _, item := range shifted {
		if _, err := tx.Exec(
			`UPDATE worklogs SET started_at_utc = ?, updated_at = ? WHERE id = ?`,
			sqlitestore.RFC3339UTC(item.StartedAtUTC),
			updatedAt,
			item.ID,
		); err != nil {
			_ = tx.Rollback()
			return ShiftResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ShiftResult{}, err
	}

	return result, nil
}

func (s *Service) Apply(cfg config.EffectiveConfig, payload RawApplyPayload, force bool, dryRun bool) (ApplyResult, error) {
	if len(payload.Adds) == 0 {
		return ApplyResult{}, ValidationError{Issues: []ValidationIssue{{Field: "adds", Message: "must contain at least one add operation"}}}
	}

	candidates := make([]LocalWorklog, 0, len(payload.Adds))
	resultItems := make([]ApplyResultItem, 0, len(payload.Adds))
	for index, item := range payload.Adds {
		candidate, err := buildApplyCandidate(cfg, index, item)
		if err != nil {
			return ApplyResult{}, err
		}

		record := candidate
		candidates = append(candidates, candidate)
		resultItems = append(resultItems, ApplyResultItem{
			Op:     "add",
			Index:  index,
			Record: record,
		})
	}

	existing, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return ApplyResult{}, err
	}
	if err := validateApplySet(existing, candidates, cfg.Location, force); err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{
		DryRun:  dryRun,
		Adds:    len(candidates),
		Items:   resultItems,
		Records: candidates,
	}
	if dryRun {
		return result, nil
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return ApplyResult{}, err
	}

	now := s.now().UTC()
	createdAt := sqlitestore.RFC3339UTC(now)
	for index, item := range candidates {
		id := uuid.NewString()
		item.ID = id
		result.Items[index].ID = &id
		result.Items[index].Record = item
		result.Records[index] = item

		if _, err := tx.Exec(
			`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			item.ID,
			item.IssueKey,
			sqlitestore.RFC3339UTC(item.StartedAtUTC),
			item.DurationSeconds,
			item.Description,
			createdAt,
			createdAt,
		); err != nil {
			_ = tx.Rollback()
			return ApplyResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ApplyResult{}, err
	}

	return result, nil
}

func buildApplyCandidate(cfg config.EffectiveConfig, index int, item RawApplyAdd) (LocalWorklog, error) {
	issues := make([]ValidationIssue, 0)
	prefix := fmt.Sprintf("adds[%d]", index)

	if item.DurationSeconds == nil {
		issues = append(issues, ValidationIssue{Field: prefix + ".duration_seconds", Message: "is required"})
	}

	startedCount := 0
	started := ""
	startedUTC := ""
	if item.StartedAt != nil {
		startedCount++
		started = *item.StartedAt
	}
	if item.StartedAtUTC != nil {
		startedCount++
		startedUTC = *item.StartedAtUTC
	}
	if startedCount != 1 {
		issues = append(issues, ValidationIssue{Field: prefix + ".started_at", Message: "provide exactly one of started_at or started_at_utc"})
	}
	if len(issues) > 0 {
		return LocalWorklog{}, ValidationError{Issues: issues}
	}

	candidate, err := buildCandidate(cfg, AddCandidateInput{
		IssueKey:    item.IssueKey,
		Started:     started,
		StartedUTC:  startedUTC,
		Duration:    fmt.Sprintf("%ds", *item.DurationSeconds),
		Description: item.Description,
	})
	if err == nil {
		return candidate, nil
	}

	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		return LocalWorklog{}, err
	}

	mapped := make([]ValidationIssue, 0, len(validationErr.Issues))
	for _, issue := range validationErr.Issues {
		field := prefix + "." + issue.Field
		switch issue.Field {
		case "issue":
			field = prefix + ".issue_key"
		case "started":
			field = prefix + ".started_at"
		case "duration":
			field = prefix + ".duration_seconds"
		}
		mapped = append(mapped, ValidationIssue{Field: field, Message: issue.Message})
	}
	return LocalWorklog{}, ValidationError{Issues: mapped}
}

func parseShiftDelta(value string) (int, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("by must be a valid Go duration")
	}
	if duration == 0 {
		return 0, fmt.Errorf("by must be non-zero")
	}
	if duration%time.Second != 0 {
		return 0, fmt.Errorf("by must normalize to whole seconds")
	}
	return int(duration / time.Second), nil
}

func hasExplicitSelector(filters ListFilters) bool {
	return filters.Issue != "" || filters.Today || filters.Yesterday || filters.From != "" || filters.To != ""
}

func validateShiftSet(finalSet []LocalWorklog, shiftedByID map[string]LocalWorklog, location *time.Location) error {
	for i := 0; i < len(finalSet); i++ {
		for j := i + 1; j < len(finalSet); j++ {
			if !isDuplicate(finalSet[i], finalSet[j]) && !overlaps(finalSet[i], finalSet[j]) {
				continue
			}

			attempted := conflictAttemptedShift(finalSet[i], finalSet[j], shiftedByID)
			conflicting := finalSet[i]
			if attempted.ID == finalSet[i].ID {
				conflicting = finalSet[j]
			}
			return conflictValidation(attempted, conflicting, location)
		}
	}
	return nil
}

func validateApplySet(existing []LocalWorklog, candidates []LocalWorklog, location *time.Location, force bool) error {
	if force {
		return nil
	}

	for index, candidate := range candidates {
		for _, item := range existing {
			if isDuplicate(candidate, item) || overlaps(candidate, item) {
				return conflictValidation(candidate, item, location)
			}
		}
		for otherIndex := 0; otherIndex < index; otherIndex++ {
			if isDuplicate(candidate, candidates[otherIndex]) || overlaps(candidate, candidates[otherIndex]) {
				return conflictValidation(candidate, candidates[otherIndex], location)
			}
		}
	}
	return nil
}

func compactConflictIDs(ids ...string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func conflictAttemptedShift(a, b LocalWorklog, shiftedByID map[string]LocalWorklog) LocalWorklog {
	_, aShifted := shiftedByID[a.ID]
	_, bShifted := shiftedByID[b.ID]
	switch {
	case aShifted:
		return a
	case bShifted:
		return b
	default:
		return a
	}
}

func conflictValidation(attempted LocalWorklog, conflicting LocalWorklog, location *time.Location) error {
	detail := &ConflictDetail{
		Reason:         "overlap",
		Attempted:      ToRecordView(attempted, location),
		ConflictingIDs: compactConflictIDs(conflicting.ID),
	}
	if isDuplicate(attempted, conflicting) {
		detail.Reason = "duplicate"
		return ValidationError{Conflict: detail}
	}
	detail.ConflictingWindows = [][2]string{{
		sqlitestore.RFC3339UTC(conflicting.StartedAtUTC),
		sqlitestore.RFC3339UTC(conflicting.StartedAtUTC.Add(time.Duration(conflicting.DurationSeconds) * time.Second)),
	}}
	return ValidationError{Conflict: detail}
}
