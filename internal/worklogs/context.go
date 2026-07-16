package worklogs

import (
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

const (
	defaultContextDayStart = config.DefaultWorkdayStart
	defaultContextDayEnd   = config.DefaultWorkdayEnd
)

type IssueMetadata struct {
	IssueKey            string
	MaxEstimateSeconds  *int64
	SourceAdapterFamily string
	SourceAdapterInst   string
	RefreshedAt         time.Time
}

type ContextInput struct {
	Issues        []string
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
}

type ContextFilters struct {
	From     *time.Time
	To       *time.Time
	Timezone string
	Issues   []string
}

type ContextLunch struct {
	Start string
	End   string
}

type ContextSettings struct {
	Timezone                 string
	DayStart                 string
	DayEnd                   string
	DailyMinimumQuotaSeconds int
	Lunch                    *ContextLunch
}

type ContextIssueHint struct {
	IssueKey           string
	MaxEstimateSeconds *int64
}

type ContextFreeSlot struct {
	Start           time.Time
	End             time.Time
	DurationSeconds int
}

type ContextCollision struct {
	Start      time.Time
	End        time.Time
	WorklogIDs []string
}

type ContextDay struct {
	Date              string
	Worklogs          []LocalWorklog
	BookedSeconds     int
	UntilQuotaSeconds int
	FreeSlots         []ContextFreeSlot
	Collisions        []ContextCollision
}

type ContextSummary struct {
	DayCount          int
	WorklogCount      int
	BookedSeconds     int
	UntilQuotaSeconds int
	CollisionCount    int
}

type ContextResult struct {
	Filters  ContextFilters
	Settings ContextSettings
	Planning struct {
		IssueOrder             []string
		Issues                 []ContextIssueHint
		MinimumDurationSeconds int
		PayloadContract        string
		SlotOrder              string
	}
	Summary  ContextSummary
	Days     []ContextDay
	Metadata map[string]IssueMetadata
}

type localInterval struct {
	Start time.Time
	End   time.Time
	ID    string
}

type workdayWindow struct {
	dayStartMinutes int
	dayEndMinutes   int
	lunchStart      *int
	lunchEnd        *int
	dayStart        string
	dayEnd          string
	lunch           *ContextLunch
}

func (s *Service) LookupIssueMetadata(issueKeys []string) (map[string]IssueMetadata, error) {
	unique := uniqueIssueKeys(issueKeys)
	if len(unique) == 0 {
		return map[string]IssueMetadata{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, 0, len(unique))
	for _, issueKey := range unique {
		args = append(args, issueKey)
	}

	rows, err := s.store.DB().Query(
		`SELECT issue_key, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at FROM issue_metadata WHERE issue_key IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[string]IssueMetadata, len(unique))
	for rows.Next() {
		var item IssueMetadata
		var maxEstimate sql.NullInt64
		var refreshedAt string
		if err := rows.Scan(&item.IssueKey, &maxEstimate, &item.SourceAdapterFamily, &item.SourceAdapterInst, &refreshedAt); err != nil {
			return nil, err
		}
		if maxEstimate.Valid {
			value := maxEstimate.Int64
			item.MaxEstimateSeconds = &value
		}
		parsed, err := time.Parse(time.RFC3339, refreshedAt)
		if err != nil {
			return nil, err
		}
		item.RefreshedAt = parsed.UTC()
		items[item.IssueKey] = item
	}

	return items, rows.Err()
}

func (s *Service) ListIssueMetadata(issueKeys []string) ([]IssueMetadata, error) {
	unique := uniqueIssueKeys(issueKeys)
	if len(unique) == 0 {
		return []IssueMetadata{}, nil
	}

	items, err := s.LookupIssueMetadata(unique)
	if err != nil {
		return nil, err
	}

	ordered := make([]IssueMetadata, 0, len(items))
	for _, issueKey := range unique {
		item, ok := items[issueKey]
		if !ok {
			continue
		}
		ordered = append(ordered, item)
	}
	return ordered, nil
}

func (s *Service) ShowIssueMetadata(issueKey string) (IssueMetadata, error) {
	items, err := s.ListIssueMetadata([]string{issueKey})
	if err != nil {
		return IssueMetadata{}, err
	}
	if len(items) == 0 {
		return IssueMetadata{}, ErrIssueMetadataNotFound
	}
	return items[0], nil
}

func (s *Service) UpsertIssueMetadata(issueKey string, maxEstimateSeconds *int64, adapterFamily, adapterInstance string, refreshedAt time.Time) error {
	_, err := s.store.DB().Exec(
		`INSERT INTO issue_metadata(issue_key, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(issue_key) DO UPDATE SET
		 	max_estimate_seconds = excluded.max_estimate_seconds,
		 	source_adapter_family = excluded.source_adapter_family,
		 	source_adapter_instance = excluded.source_adapter_instance,
		 	refreshed_at = excluded.refreshed_at`,
		issueKey,
		maxEstimateSeconds,
		adapterFamily,
		adapterInstance,
		refreshedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func uniqueIssueKeys(issueKeys []string) []string {
	unique := make([]string, 0, len(issueKeys))
	seen := map[string]struct{}{}
	for _, issueKey := range issueKeys {
		if issueKey == "" {
			continue
		}
		if _, ok := seen[issueKey]; ok {
			continue
		}
		seen[issueKey] = struct{}{}
		unique = append(unique, issueKey)
	}
	return unique
}

func (s *Service) Context(cfg config.EffectiveConfig, input ContextInput) (ContextResult, error) {
	filters, err := normalizeContextFiltersAt(cfg, input, s.now)
	if err != nil {
		return ContextResult{}, err
	}

	window, err := normalizeWorkdayWindow(cfg, input)
	if err != nil {
		return ContextResult{}, err
	}

	selectedDates := selectedContextDates(filters.From, filters.To, cfg.Location)
	active, err := s.listActive(EffectiveFilters{})
	if err != nil {
		return ContextResult{}, err
	}

	keys := append([]string{}, filters.Issues...)
	for _, item := range active {
		keys = append(keys, item.IssueKey)
	}
	metadata, err := s.LookupIssueMetadata(keys)
	if err != nil {
		return ContextResult{}, err
	}

	result := ContextResult{
		Filters: filters,
		Settings: ContextSettings{
			Timezone:                 filters.Timezone,
			DayStart:                 window.dayStart,
			DayEnd:                   window.dayEnd,
			DailyMinimumQuotaSeconds: cfg.DailyMinimumQuotaSeconds,
			Lunch:                    window.lunch,
		},
		Metadata: metadata,
	}
	result.Planning.IssueOrder = append(result.Planning.IssueOrder, filters.Issues...)
	result.Planning.MinimumDurationSeconds = cfg.MinimumDurationSeconds
	result.Planning.PayloadContract = "apply_raw_adds_v1"
	result.Planning.SlotOrder = "date_asc,start_asc"
	for _, issueKey := range filters.Issues {
		result.Planning.Issues = append(result.Planning.Issues, ContextIssueHint{
			IssueKey:           issueKey,
			MaxEstimateSeconds: metadataMaxEstimate(metadata, issueKey),
		})
	}

	days := make([]ContextDay, 0, len(selectedDates))
	for _, selectedDate := range selectedDates {
		dayStart := time.Date(selectedDate.Year(), selectedDate.Month(), selectedDate.Day(), 0, 0, 0, 0, cfg.Location)
		dayEnd := dayStart.Add(24 * time.Hour)
		worklogsForDay := make([]LocalWorklog, 0)
		slicesForDay := make([]localInterval, 0)
		bookedSeconds := 0

		for _, item := range active {
			localStart := item.StartedAtUTC.In(cfg.Location)
			if localStart.Year() == selectedDate.Year() && localStart.Month() == selectedDate.Month() && localStart.Day() == selectedDate.Day() {
				worklogsForDay = append(worklogsForDay, item)
			}

			startUTC := item.StartedAtUTC
			endUTC := item.StartedAtUTC.Add(time.Duration(item.DurationSeconds) * time.Second)
			startLocal := maxTime(startUTC.In(cfg.Location), dayStart)
			endLocal := minTime(endUTC.In(cfg.Location), dayEnd)
			if !startLocal.Before(endLocal) {
				continue
			}

			slicesForDay = append(slicesForDay, localInterval{
				Start: startLocal,
				End:   endLocal,
				ID:    item.ID,
			})
			bookedSeconds += int(endLocal.Sub(startLocal) / time.Second)
		}

		slicesForDay = sortIntervals(slicesForDay)
		collisions := buildCollisions(slicesForDay)
		freeSlots := buildFreeSlots(selectedDate, cfg.Location, window, slicesForDay)
		untilQuotaSeconds := cfg.DailyMinimumQuotaSeconds - bookedSeconds

		day := ContextDay{
			Date:              selectedDate.Format("2006-01-02"),
			Worklogs:          worklogsForDay,
			BookedSeconds:     bookedSeconds,
			UntilQuotaSeconds: untilQuotaSeconds,
			FreeSlots:         freeSlots,
			Collisions:        collisions,
		}
		days = append(days, day)

		result.Summary.BookedSeconds += bookedSeconds
		result.Summary.UntilQuotaSeconds += untilQuotaSeconds
		result.Summary.CollisionCount += len(collisions)
		result.Summary.WorklogCount += len(worklogsForDay)
	}

	result.Days = days
	result.Summary.DayCount = len(days)
	return result, nil
}

func normalizeContextFilters(cfg config.EffectiveConfig, input ContextInput) (ContextFilters, error) {
	return normalizeContextFiltersAt(cfg, input, time.Now)
}

func normalizeContextFiltersAt(cfg config.EffectiveConfig, input ContextInput, now func() time.Time) (ContextFilters, error) {
	from, to, err := ResolveDateWindowSelectionAt(cfg, DateWindowSelection{
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
	}, now)
	if err != nil {
		return ContextFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "date", Message: err.Error()}}}
	}

	filters := ContextFilters{}
	if cfg.LocalTimezoneConfig != nil {
		filters.Timezone = *cfg.LocalTimezoneConfig
	} else {
		filters.Timezone = cfg.Location.String()
	}

	for _, issueKey := range input.Issues {
		if !issueKeyPattern.MatchString(issueKey) {
			return ContextFilters{}, ValidationError{Issues: []ValidationIssue{{Field: "issue", Message: "must match <PROJECTKEY>-<NUMBER>"}}}
		}
		filters.Issues = append(filters.Issues, issueKey)
	}

	if from == nil || to == nil {
		current := now().In(cfg.Location)
		start, end := dayBounds(current, cfg.Location)
		filters.From = &start
		filters.To = &end
		return filters, nil
	}
	filters.From = from
	filters.To = to

	return filters, nil
}

func normalizeWorkdayWindow(cfg config.EffectiveConfig, input ContextInput) (workdayWindow, error) {
	startValue := defaultContextDayStart
	if strings.TrimSpace(cfg.DayStart) != "" {
		startValue = strings.TrimSpace(cfg.DayStart)
	}
	if input.DayStart != "" {
		startValue = input.DayStart
	}
	endValue := defaultContextDayEnd
	if strings.TrimSpace(cfg.DayEnd) != "" {
		endValue = strings.TrimSpace(cfg.DayEnd)
	}
	if input.DayEnd != "" {
		endValue = input.DayEnd
	}

	startMinutes, err := parseClockMinutes(startValue)
	if err != nil {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "day_start", Message: err.Error()}}}
	}
	endMinutes, err := parseClockMinutes(endValue)
	if err != nil {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "day_end", Message: err.Error()}}}
	}
	if startMinutes >= endMinutes {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "day", Message: "day_start must be earlier than day_end"}}}
	}

	if input.NoLunch && input.Lunch != "" {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "lunch", Message: "lunch and no_lunch are mutually exclusive"}}}
	}

	window := workdayWindow{
		dayStartMinutes: startMinutes,
		dayEndMinutes:   endMinutes,
		dayStart:        startValue,
		dayEnd:          endValue,
	}
	if input.NoLunch {
		return window, nil
	}

	lunchValue := cfg.DailyLunch
	if input.Lunch != "" {
		lunchValue = input.Lunch
	}
	if err := config.ValidateDailyLunchWindow(strings.TrimSpace(lunchValue)); err != nil {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "lunch", Message: err.Error()}}}
	}

	parts := strings.Split(strings.TrimSpace(lunchValue), "-")
	lunchStart, err := parseClockMinutes(strings.TrimSpace(parts[0]))
	if err != nil {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "lunch", Message: err.Error()}}}
	}
	lunchEnd, err := parseClockMinutes(strings.TrimSpace(parts[1]))
	if err != nil {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "lunch", Message: err.Error()}}}
	}
	if lunchStart <= startMinutes || lunchEnd >= endMinutes {
		return workdayWindow{}, ValidationError{Issues: []ValidationIssue{{Field: "lunch", Message: "lunch must fit strictly inside the effective workday"}}}
	}

	window.lunchStart = &lunchStart
	window.lunchEnd = &lunchEnd
	window.lunch = &ContextLunch{
		Start: strings.TrimSpace(parts[0]),
		End:   strings.TrimSpace(parts[1]),
	}
	return window, nil
}

func parseClockMinutes(value string) (int, error) {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%02d:%02d", &hour, &minute); err != nil {
		return 0, fmt.Errorf("%s", timeClockFormatMessage)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("%s", timeClockFormatMessage)
	}
	return hour*60 + minute, nil
}

func selectedContextDates(from, to *time.Time, location *time.Location) []time.Time {
	dates := make([]time.Time, 0)
	start := time.Date(from.In(location).Year(), from.In(location).Month(), from.In(location).Day(), 0, 0, 0, 0, location)
	end := time.Date(to.In(location).Year(), to.In(location).Month(), to.In(location).Day(), 0, 0, 0, 0, location)
	for cursor := start; !cursor.After(end); cursor = cursor.AddDate(0, 0, 1) {
		dates = append(dates, cursor)
	}
	return dates
}

func buildFreeSlots(selectedDate time.Time, location *time.Location, window workdayWindow, occupied []localInterval) []ContextFreeSlot {
	base := []localInterval{{
		Start: applyClock(selectedDate, location, window.dayStartMinutes),
		End:   applyClock(selectedDate, location, window.dayEndMinutes),
	}}
	if window.lunchStart != nil && window.lunchEnd != nil {
		base = subtractIntervalSet(base, localInterval{
			Start: applyClock(selectedDate, location, *window.lunchStart),
			End:   applyClock(selectedDate, location, *window.lunchEnd),
		})
	}

	mergedOccupied := mergeIntervals(occupied)
	for _, item := range mergedOccupied {
		base = subtractIntervalSet(base, item)
	}

	slots := make([]ContextFreeSlot, 0, len(base))
	for _, item := range base {
		if !item.Start.Before(item.End) {
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

func buildCollisions(occupied []localInterval) []ContextCollision {
	if len(occupied) < 2 {
		return nil
	}

	points := make([]time.Time, 0, len(occupied)*2)
	for _, item := range occupied {
		points = append(points, item.Start, item.End)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Before(points[j]) })
	points = dedupeTimes(points)

	collisions := make([]ContextCollision, 0)
	for i := 0; i < len(points)-1; i++ {
		start := points[i]
		end := points[i+1]
		if !start.Before(end) {
			continue
		}

		ids := make([]string, 0)
		for _, item := range occupied {
			if item.Start.Before(end) && start.Before(item.End) {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) < 2 {
			continue
		}
		slices.Sort(ids)

		if len(collisions) > 0 {
			last := &collisions[len(collisions)-1]
			if last.End.Equal(start) && slices.Equal(last.WorklogIDs, ids) {
				last.End = end
				continue
			}
		}
		collisions = append(collisions, ContextCollision{
			Start:      start,
			End:        end,
			WorklogIDs: ids,
		})
	}
	return collisions
}

func subtractIntervalSet(base []localInterval, remove localInterval) []localInterval {
	out := make([]localInterval, 0, len(base)+1)
	for _, item := range base {
		if !remove.Start.Before(item.End) || !item.Start.Before(remove.End) {
			out = append(out, item)
			continue
		}
		if item.Start.Before(remove.Start) {
			out = append(out, localInterval{Start: item.Start, End: minTime(item.End, remove.Start)})
		}
		if remove.End.Before(item.End) {
			out = append(out, localInterval{Start: maxTime(item.Start, remove.End), End: item.End})
		}
	}
	return sortIntervals(out)
}

func mergeIntervals(items []localInterval) []localInterval {
	if len(items) == 0 {
		return nil
	}
	sorted := sortIntervals(items)
	merged := []localInterval{sorted[0]}
	for _, item := range sorted[1:] {
		last := &merged[len(merged)-1]
		if !last.End.Before(item.Start) {
			if item.End.After(last.End) {
				last.End = item.End
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func sortIntervals(items []localInterval) []localInterval {
	sorted := append([]localInterval(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start.Equal(sorted[j].Start) {
			if sorted[i].End.Equal(sorted[j].End) {
				return sorted[i].ID < sorted[j].ID
			}
			return sorted[i].End.Before(sorted[j].End)
		}
		return sorted[i].Start.Before(sorted[j].Start)
	})
	return sorted
}

func applyClock(day time.Time, location *time.Location, minutes int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, location)
}

func dedupeTimes(items []time.Time) []time.Time {
	if len(items) == 0 {
		return nil
	}
	out := []time.Time{items[0]}
	for _, item := range items[1:] {
		if item.Equal(out[len(out)-1]) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func metadataMaxEstimate(items map[string]IssueMetadata, issueKey string) *int64 {
	item, ok := items[issueKey]
	if !ok {
		return nil
	}
	return item.MaxEstimateSeconds
}
