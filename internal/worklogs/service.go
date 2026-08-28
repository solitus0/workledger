package worklogs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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
	issuePrefixPattern       = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)
)

const (
	dateSelectorFormatMessage   = "date must use YYYY-MM-DD, today, yesterday, tomorrow, mon, tue, wed, thu, fri, sat, sun, +Nd, or -Nd"
	localTimestampFormatMessage = "started must use YYYY-MM-DDTHH:MM, todayTHH:MM, yesterdayTHH:MM, tomorrowTHH:MM, monTHH:MM, tueTHH:MM, wedTHH:MM, thuTHH:MM, friTHH:MM, satTHH:MM, sunTHH:MM, +NdTHH:MM, or -NdTHH:MM (time must use HH:MM, e.g. 09:00)"
	timeClockFormatMessage      = "time must use HH:MM, e.g. 09:00"
	startedUTCFormatMessage     = "started_utc must use RFC3339 UTC, e.g. 2026-05-14T09:00:00Z"
)

func IsValidIssueKey(value string) bool {
	return issueKeyPattern.MatchString(value)
}

func IsValidIssuePrefix(value string) bool {
	return issuePrefixPattern.MatchString(value)
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

type DeleteResult struct {
	ID        string
	IssueKey  string
	DeletedAt time.Time
}

type ListFilters struct {
	Issue         string
	IssuePrefix   string
	Today         bool
	Yesterday     bool
	Tomorrow      bool
	Monday        bool
	Tuesday       bool
	Wednesday     bool
	Thursday      bool
	Friday        bool
	Saturday      bool
	Sunday        bool
	CurrentWeek   bool
	LastWeek      bool
	CurrentMonth  bool
	LastMonth     bool
	From          string
	To            string
	WeekOffset    int
	WeekOffsetSet bool
	Fields        []string
}

type SearchInput struct {
	Query string
	ListFilters
}

type EffectiveFilters struct {
	IssueKey    *string
	IssuePrefix *string
	From        *time.Time
	To          *time.Time
	Timezone    string
	Fields      []string
}

type AddInput struct {
	IssueKey      string
	Started       string
	StartedUTC    string
	Fit           bool
	Fill          bool
	Overtime      bool
	Today         bool
	Yesterday     bool
	Tomorrow      bool
	Monday        bool
	Tuesday       bool
	Wednesday     bool
	Thursday      bool
	Friday        bool
	Saturday      bool
	Sunday        bool
	CurrentWeek   bool
	LastWeek      bool
	CurrentMonth  bool
	LastMonth     bool
	From          string
	To            string
	WeekOffset    int
	WeekOffsetSet bool
	DayStart      string
	DayEnd        string
	Lunch         string
	NoLunch       bool
	Duration      string
	Description   string
	Force         bool
}

type AddResult struct {
	DryRun  bool
	Records []LocalWorklog
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
	Filters EffectiveFilters
	Items   []LocalWorklog
	Deleted []string
	DryRun  bool
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

func (s *Service) List(cfg config.EffectiveConfig, filters ListFilters) ([]LocalWorklog, EffectiveFilters, error) {
	if !hasListTimeSelector(filters) {
		return nil, EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: "worklogs list requires at least one time selector"}}}
	}

	effective, err := normalizeListFiltersAt(cfg, filters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, err
	}

	items, err := s.listActive(effective)
	return items, effective, err
}

func (s *Service) Search(cfg config.EffectiveConfig, input SearchInput) ([]LocalWorklog, EffectiveFilters, string, error) {
	effective, err := normalizeListFiltersAt(cfg, input.ListFilters, false, s.now)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	query, err := normalizeSearchQuery(input.Query)
	if err != nil {
		return nil, EffectiveFilters{}, "", err
	}

	items, err := s.searchActive(effective, query)
	return items, effective, query, err
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

func (s *Service) PreviewAdd(cfg config.EffectiveConfig, input AddInput) (AddResult, error) {
	result, err := s.prepareAdd(cfg, input)
	if err != nil {
		return AddResult{}, err
	}
	result.DryRun = true
	return result, nil
}

func (s *Service) Add(cfg config.EffectiveConfig, input AddInput) (AddResult, error) {
	result, err := s.prepareAdd(cfg, input)
	if err != nil {
		return AddResult{}, err
	}

	now := s.now().UTC()
	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return AddResult{}, err
	}
	created := make([]LocalWorklog, 0, len(result.Records))
	for _, candidate := range result.Records {
		worklog := LocalWorklog{
			ID:              uuid.NewString(),
			IssueKey:        candidate.IssueKey,
			StartedAtUTC:    candidate.StartedAtUTC,
			DurationSeconds: candidate.DurationSeconds,
			Description:     candidate.Description,
		}

		_, err = tx.Exec(
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
			_ = tx.Rollback()
			return AddResult{}, err
		}
		created = append(created, worklog)
	}
	if err := tx.Commit(); err != nil {
		return AddResult{}, err
	}
	result.Records = created

	return result, nil
}

func (s *Service) prepareAdd(cfg config.EffectiveConfig, input AddInput) (AddResult, error) {
	candidates, err := s.prepareAddCandidates(cfg, input)
	if err != nil {
		return AddResult{}, err
	}
	if err := s.validateAddConflicts(cfg, candidates, input.Force); err != nil {
		return AddResult{}, err
	}
	return AddResult{Records: candidates}, nil
}

func (s *Service) prepareAddCandidates(cfg config.EffectiveConfig, input AddInput) ([]LocalWorklog, error) {
	if input.Overtime && !input.Fit && !input.Fill {
		return nil, ValidationError{Issues: []ValidationIssue{{Field: "placement", Message: "overtime requires fit or fill"}}}
	}
	if placementCount(input) != 1 {
		return nil, ValidationError{Issues: []ValidationIssue{{Field: "placement", Message: "provide exactly one of started, started_utc, fit, or fill"}}}
	}
	if input.Fit || input.Fill {
		return s.prepareAutomaticAddCandidates(cfg, input)
	}

	if hasAutomaticPlacementOnlyAddFlags(input) {
		return nil, ValidationError{Issues: []ValidationIssue{{Field: "placement", Message: "date-window and workday flags require fit or fill"}}}
	}

	candidate, err := buildCandidate(cfg, AddCandidateInput{
		IssueKey:    input.IssueKey,
		Started:     input.Started,
		StartedUTC:  input.StartedUTC,
		Duration:    input.Duration,
		Description: input.Description,
	})
	if err != nil {
		return nil, err
	}

	return []LocalWorklog{candidate}, nil
}

func (s *Service) prepareAutomaticAddCandidates(cfg config.EffectiveConfig, input AddInput) ([]LocalWorklog, error) {
	candidateBase, err := buildAddBaseCandidate(cfg, input.IssueKey, input.Duration, input.Description)
	if err != nil {
		return nil, err
	}

	filters, err := normalizeContextFiltersAt(cfg, ContextInput{
		Today:         input.Today,
		Yesterday:     input.Yesterday,
		Tomorrow:      input.Tomorrow,
		Monday:        input.Monday,
		Tuesday:       input.Tuesday,
		Wednesday:     input.Wednesday,
		Thursday:      input.Thursday,
		Friday:        input.Friday,
		Saturday:      input.Saturday,
		Sunday:        input.Sunday,
		CurrentWeek:   input.CurrentWeek,
		LastWeek:      input.LastWeek,
		CurrentMonth:  input.CurrentMonth,
		LastMonth:     input.LastMonth,
		From:          input.From,
		To:            input.To,
		WeekOffset:    input.WeekOffset,
		WeekOffsetSet: input.WeekOffsetSet,
	}, s.now)
	if err != nil {
		return nil, err
	}
	window, err := normalizeWorkdayWindow(cfg, ContextInput{
		DayStart: input.DayStart,
		DayEnd:   input.DayEnd,
		Lunch:    input.Lunch,
		NoLunch:  input.NoLunch,
	})
	if err != nil {
		return nil, err
	}

	active, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return nil, err
	}

	selectedDates := selectedContextDates(filters.From, filters.To, cfg.Location)
	plan := buildAutomaticAddPlan(cfg, selectedDates, window, active, candidateBase, input.Fit, input.Overtime)
	if len(plan) > 0 {
		return plan, nil
	}
	if !input.Overtime && len(buildAutomaticAddPlan(cfg, selectedDates, window, active, candidateBase, input.Fit, true)) > 0 {
		return nil, noAutomaticPlacementError(true)
	}
	return nil, noAutomaticPlacementError(false)
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

func (s *Service) Delete(id string) (DeleteResult, error) {
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
	if err := tx.Commit(); err != nil {
		return DeleteResult{}, err
	}

	return DeleteResult{
		ID:        current.ID,
		IssueKey:  current.IssueKey,
		DeletedAt: deletedAt,
	}, nil
}

func (s *Service) DeleteBatch(cfg config.EffectiveConfig, filters ListFilters, dryRun bool) (DeleteBatchResult, error) {
	effective, err := normalizeListFiltersAt(cfg, filters, false, s.now)
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
		Filters: effective,
		Items:   items,
		DryRun:  dryRun,
	}
	if dryRun || len(items) == 0 {
		return result, nil
	}

	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return DeleteBatchResult{}, err
	}

	result.Deleted = make([]string, 0, len(items))
	for _, item := range items {
		if _, err := tx.Exec(`DELETE FROM worklogs WHERE id = ?`, item.ID); err != nil {
			_ = tx.Rollback()
			return DeleteBatchResult{}, err
		}
		result.Deleted = append(result.Deleted, item.ID)
	}

	if err := tx.Commit(); err != nil {
		return DeleteBatchResult{}, err
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

func buildAddBaseCandidate(cfg config.EffectiveConfig, issueKey string, duration string, description string) (LocalWorklog, error) {
	issues := make([]ValidationIssue, 0)
	if !issueKeyPattern.MatchString(issueKey) {
		issues = append(issues, ValidationIssue{Field: "issue", Message: "must match <PROJECTKEY>-<NUMBER>"})
	}

	durationSeconds, durationErr := parseDuration(duration, cfg.MinimumDurationSeconds)
	if durationErr != nil {
		issues = append(issues, ValidationIssue{Field: "duration", Message: durationErr.Error()})
	}

	normalizedDescription, descriptionErr := normalizeDescription(description)
	if descriptionErr != nil {
		issues = append(issues, ValidationIssue{Field: "description", Message: descriptionErr.Error()})
	}

	if len(issues) > 0 {
		return LocalWorklog{}, ValidationError{Issues: issues}
	}

	return LocalWorklog{
		IssueKey:        issueKey,
		DurationSeconds: durationSeconds,
		Description:     normalizedDescription,
	}, nil
}

func placementCount(input AddInput) int {
	count := 0
	if input.Started != "" {
		count++
	}
	if input.StartedUTC != "" {
		count++
	}
	if input.Fit {
		count++
	}
	if input.Fill {
		count++
	}
	return count
}

func hasAutomaticPlacementOnlyAddFlags(input AddInput) bool {
	return input.Today ||
		input.Yesterday ||
		input.Tomorrow ||
		input.Monday ||
		input.Tuesday ||
		input.Wednesday ||
		input.Thursday ||
		input.Friday ||
		input.Saturday ||
		input.Sunday ||
		input.CurrentWeek ||
		input.LastWeek ||
		input.CurrentMonth ||
		input.LastMonth ||
		input.From != "" ||
		input.To != "" ||
		input.DayStart != "" ||
		input.DayEnd != "" ||
		input.Lunch != "" ||
		input.NoLunch
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

func (s *Service) validateAddConflicts(cfg config.EffectiveConfig, candidates []LocalWorklog, force bool) error {
	if force || len(candidates) == 0 {
		return nil
	}

	existing, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return err
	}

	checkSet := make([]LocalWorklog, 0, len(existing)+len(candidates))
	checkSet = append(checkSet, existing...)
	for index, candidate := range candidates {
		conflictingIDs := make([]string, 0)
		conflictingWindows := make([][2]string, 0)
		reason := ""
		for _, item := range checkSet {
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
		checkSet = append(checkSet, candidates[index])
	}

	return nil
}

func (s *Service) listActive(filters EffectiveFilters) ([]LocalWorklog, error) {
	query := `SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs`
	args := make([]any, 0)
	where := buildWhereClause(filters, false, &args)
	query += where + ` ORDER BY started_at_utc ASC, id ASC`

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

func normalizeListFilters(cfg config.EffectiveConfig, filters ListFilters, allowDeleted bool) (EffectiveFilters, error) {
	return normalizeListFiltersAt(cfg, filters, allowDeleted, time.Now)
}

func normalizeListFiltersAt(cfg config.EffectiveConfig, filters ListFilters, allowDeleted bool, now func() time.Time) (EffectiveFilters, error) {
	from, to, err := ResolveDateWindowSelectionAt(cfg, DateWindowSelection{
		Today:         filters.Today,
		Yesterday:     filters.Yesterday,
		Tomorrow:      filters.Tomorrow,
		Monday:        filters.Monday,
		Tuesday:       filters.Tuesday,
		Wednesday:     filters.Wednesday,
		Thursday:      filters.Thursday,
		Friday:        filters.Friday,
		Saturday:      filters.Saturday,
		Sunday:        filters.Sunday,
		CurrentWeek:   filters.CurrentWeek,
		LastWeek:      filters.LastWeek,
		CurrentMonth:  filters.CurrentMonth,
		LastMonth:     filters.LastMonth,
		From:          filters.From,
		To:            filters.To,
		WeekOffset:    filters.WeekOffset,
		WeekOffsetSet: filters.WeekOffsetSet,
	}, now)
	if err != nil {
		return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: err.Error()}}}
	}

	fields, err := normalizeFields(filters.Fields, allowDeleted)
	if err != nil {
		return EffectiveFilters{}, err
	}

	effective := EffectiveFilters{
		Fields: fields,
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
	if filters.IssuePrefix != "" {
		if !issuePrefixPattern.MatchString(filters.IssuePrefix) {
			return EffectiveFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "issue_prefix", Message: "must match <PROJECTKEY>"}}}
		}
		issuePrefix := filters.IssuePrefix
		effective.IssuePrefix = &issuePrefix
	}

	effective.From = from
	effective.To = to

	return effective, nil
}

func hasListTimeSelector(filters ListFilters) bool {
	return HasDateWindowSelection(DateWindowSelection{
		Today:        filters.Today,
		Yesterday:    filters.Yesterday,
		Tomorrow:     filters.Tomorrow,
		Monday:       filters.Monday,
		Tuesday:      filters.Tuesday,
		Wednesday:    filters.Wednesday,
		Thursday:     filters.Thursday,
		Friday:       filters.Friday,
		Saturday:     filters.Saturday,
		Sunday:       filters.Sunday,
		CurrentWeek:  filters.CurrentWeek,
		LastWeek:     filters.LastWeek,
		CurrentMonth: filters.CurrentMonth,
		LastMonth:    filters.LastMonth,
		From:         filters.From,
		To:           filters.To,
	})
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
			return time.Time{}, errors.New(startedUTCFormatMessage)
		}
		return parsed.UTC(), nil
	default:
		return time.Time{}, errors.New("started time is required")
	}
}

func parseLocalTimestamp(value string, location *time.Location) (time.Time, error) {
	return parseLocalTimestampAt(value, location, time.Now)
}

func parseLocalTimestampAt(value string, location *time.Location, now func() time.Time) (time.Time, error) {
	parts := strings.Split(value, "T")
	if len(parts) != 2 {
		return time.Time{}, errors.New(localTimestampFormatMessage)
	}

	dateValue, err := parseDateSelectorAt(parts[0], location, now)
	if err != nil {
		return time.Time{}, err
	}

	clockParts := strings.Split(parts[1], ":")
	if len(clockParts) != 2 {
		return time.Time{}, errors.New(localTimestampFormatMessage)
	}

	hour, minute := 0, 0
	if _, err := fmt.Sscanf(parts[1], "%02d:%02d", &hour, &minute); err != nil {
		return time.Time{}, errors.New(localTimestampFormatMessage)
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

func parseDateSelectorAt(value string, location *time.Location, now func() time.Time) (time.Time, error) {
	switch value {
	case "today":
		return now().In(location), nil
	case "yesterday":
		return now().In(location).AddDate(0, 0, -1), nil
	case "tomorrow":
		return now().In(location).AddDate(0, 0, 1), nil
	}
	if weekday, ok := semanticWeekday(value); ok {
		start, _ := weekdayBounds(now().In(location), weekday, location)
		return start, nil
	}

	if strings.HasSuffix(value, "d") && (strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-")) {
		offset, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return time.Time{}, errors.New(dateSelectorFormatMessage)
		}
		return now().In(location).AddDate(0, 0, offset), nil
	}

	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, errors.New(dateSelectorFormatMessage)
	}

	return parsed, nil
}

func semanticWeekday(value string) (time.Weekday, bool) {
	switch value {
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	case "sun":
		return time.Sunday, true
	default:
		return 0, false
	}
}

func dayBounds(date time.Time, location *time.Location) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	end := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, location)
	return start, end
}

func weekdayBounds(date time.Time, weekday time.Weekday, location *time.Location) (time.Time, time.Time) {
	weekStart, _ := weekBounds(date, location)
	target := weekStart.AddDate(0, 0, weekdayOffsetFromMonday(weekday))
	return dayBounds(target, location)
}

func weekdayOffsetFromMonday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 6
	}
	return int(weekday) - 1
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

func buildAutomaticAddPlan(cfg config.EffectiveConfig, selectedDates []time.Time, window workdayWindow, active []LocalWorklog, base LocalWorklog, fit bool, overtime bool) []LocalWorklog {
	if fit {
		for _, selectedDate := range selectedDates {
			if plan := buildFitPlanForDate(cfg, selectedDate, window, active, base, overtime); len(plan) > 0 {
				return plan
			}
		}
		return nil
	}
	return buildFillPlanAcrossDates(cfg, selectedDates, window, active, base, overtime)
}

func buildFitPlanForDate(cfg config.EffectiveConfig, selectedDate time.Time, window workdayWindow, active []LocalWorklog, base LocalWorklog, overtime bool) []LocalWorklog {
	freeSlots := buildAddPlacementFreeSlots(cfg, selectedDate, window, active, overtime)
	for _, slot := range freeSlots {
		if slot.DurationSeconds < base.DurationSeconds {
			continue
		}
		return []LocalWorklog{{
			IssueKey:        base.IssueKey,
			StartedAtUTC:    slot.Start.UTC(),
			DurationSeconds: base.DurationSeconds,
			Description:     base.Description,
		}}
	}
	return nil
}

func buildFillPlanAcrossDates(cfg config.EffectiveConfig, selectedDates []time.Time, window workdayWindow, active []LocalWorklog, base LocalWorklog, overtime bool) []LocalWorklog {
	slots := make([]ContextFreeSlot, 0)
	for _, selectedDate := range selectedDates {
		slots = append(slots, buildAddPlacementFreeSlots(cfg, selectedDate, window, active, overtime)...)
	}
	if totalSlotDuration(slots) < base.DurationSeconds {
		return nil
	}

	plan := make([]LocalWorklog, 0)
	remaining := base.DurationSeconds
	for index, slot := range slots {
		if remaining == 0 {
			break
		}
		if slot.DurationSeconds < cfg.MinimumDurationSeconds {
			continue
		}

		duration := min(remaining, slot.DurationSeconds)
		if duration < cfg.MinimumDurationSeconds {
			if len(plan) == 0 {
				return nil
			}
			previous := &plan[len(plan)-1]
			needed := cfg.MinimumDurationSeconds - duration
			if previous.DurationSeconds-needed < cfg.MinimumDurationSeconds {
				return nil
			}
			previous.DurationSeconds -= needed
			duration += needed
			remaining += needed
		}

		availableAfter := totalSlotDuration(slots[index+1:])
		if remaining-duration > availableAfter {
			duration = remaining - availableAfter
			if duration > slot.DurationSeconds || duration < cfg.MinimumDurationSeconds {
				return nil
			}
		}

		plan = append(plan, LocalWorklog{
			IssueKey:        base.IssueKey,
			StartedAtUTC:    slot.Start.UTC(),
			DurationSeconds: duration,
			Description:     base.Description,
		})
		remaining -= duration
	}
	if remaining != 0 {
		return nil
	}
	return plan
}

func totalSlotDuration(slots []ContextFreeSlot) int {
	total := 0
	for _, slot := range slots {
		total += slot.DurationSeconds
	}
	return total
}

func buildAddPlacementFreeSlots(cfg config.EffectiveConfig, selectedDate time.Time, window workdayWindow, active []LocalWorklog, overtime bool) []ContextFreeSlot {
	dayStart := time.Date(selectedDate.Year(), selectedDate.Month(), selectedDate.Day(), 0, 0, 0, 0, cfg.Location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	workdayEnd := applyClock(selectedDate, cfg.Location, window.dayEndMinutes)

	occupied := make([]localInterval, 0)
	for _, item := range active {
		startUTC := item.StartedAtUTC
		endUTC := item.StartedAtUTC.Add(time.Duration(item.DurationSeconds) * time.Second)
		startLocal := maxTime(startUTC.In(cfg.Location), dayStart)
		endLocal := minTime(endUTC.In(cfg.Location), dayEnd)
		if !startLocal.Before(endLocal) {
			continue
		}
		occupied = append(occupied, localInterval{
			Start: startLocal,
			End:   endLocal,
			ID:    item.ID,
		})
	}

	base := []localInterval{{
		Start: applyClock(selectedDate, cfg.Location, window.dayStartMinutes),
		End:   dayEnd,
	}}
	if window.lunchStart != nil && window.lunchEnd != nil {
		base = subtractIntervalSet(base, localInterval{
			Start: applyClock(selectedDate, cfg.Location, *window.lunchStart),
			End:   applyClock(selectedDate, cfg.Location, *window.lunchEnd),
		})
	}
	for _, item := range mergeIntervals(sortIntervals(occupied)) {
		base = subtractIntervalSet(base, item)
	}

	slots := make([]ContextFreeSlot, 0, len(base))
	for _, item := range base {
		if !item.Start.Before(item.End) {
			continue
		}
		if !overtime && !item.Start.Before(workdayEnd) {
			continue
		}
		slots = append(slots, ContextFreeSlot{
			Start:           item.Start,
			End:             item.End,
			DurationSeconds: int(item.End.Sub(item.Start) / time.Second),
		})
	}
	return slots
}

func noAutomaticPlacementError(overtimeWouldSucceed bool) error {
	message := "no free slot available in the current time window"
	if overtimeWouldSucceed {
		message += "; use --overtime to allow placement starting at or after day_end"
	}
	return ValidationError{Issues: []ValidationIssue{{Field: "placement", Message: message}}}
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

func buildWhereClause(filters EffectiveFilters, deleted bool, args *[]any) string {
	whereParts := make([]string, 0)
	if filters.IssueKey != nil {
		whereParts = append(whereParts, "issue_key = ?")
		*args = append(*args, *filters.IssueKey)
	}
	if filters.IssuePrefix != nil {
		whereParts = append(whereParts, "issue_key LIKE ?")
		*args = append(*args, *filters.IssuePrefix+"-%")
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
