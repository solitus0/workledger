package worklogs

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

func TestNormalizeListFiltersAtWeekShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	current, err := normalizeListFiltersAt(cfg, ListFilters{CurrentWeek: true}, false, fixedNow)
	if err != nil {
		t.Fatalf("current week filters failed: %v", err)
	}
	if current.From == nil || current.To == nil {
		t.Fatal("expected current week bounds")
	}
	if got := current.From.Format(time.RFC3339); got != "2026-05-04T00:00:00Z" {
		t.Fatalf("unexpected current week from %s", got)
	}
	if got := current.To.Format(time.RFC3339); got != "2026-05-10T23:59:59Z" {
		t.Fatalf("unexpected current week to %s", got)
	}

	last, err := normalizeListFiltersAt(cfg, ListFilters{LastWeek: true}, false, fixedNow)
	if err != nil {
		t.Fatalf("last week filters failed: %v", err)
	}
	if last.From == nil || last.To == nil {
		t.Fatal("expected last week bounds")
	}
	if got := last.From.Format(time.RFC3339); got != "2026-04-27T00:00:00Z" {
		t.Fatalf("unexpected last week from %s", got)
	}
	if got := last.To.Format(time.RFC3339); got != "2026-05-03T23:59:59Z" {
		t.Fatalf("unexpected last week to %s", got)
	}
}

func TestNormalizeListFiltersAtMonthShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	current, err := normalizeListFiltersAt(cfg, ListFilters{CurrentMonth: true}, false, fixedNow)
	if err != nil {
		t.Fatalf("current month filters failed: %v", err)
	}
	if current.From == nil || current.To == nil {
		t.Fatal("expected current month bounds")
	}
	if got := current.From.Format(time.RFC3339); got != "2026-05-01T00:00:00Z" {
		t.Fatalf("unexpected current month from %s", got)
	}
	if got := current.To.Format(time.RFC3339); got != "2026-05-31T23:59:59Z" {
		t.Fatalf("unexpected current month to %s", got)
	}

	last, err := normalizeListFiltersAt(cfg, ListFilters{LastMonth: true}, false, fixedNow)
	if err != nil {
		t.Fatalf("last month filters failed: %v", err)
	}
	if last.From == nil || last.To == nil {
		t.Fatal("expected last month bounds")
	}
	if got := last.From.Format(time.RFC3339); got != "2026-04-01T00:00:00Z" {
		t.Fatalf("unexpected last month from %s", got)
	}
	if got := last.To.Format(time.RFC3339); got != "2026-04-30T23:59:59Z" {
		t.Fatalf("unexpected last month to %s", got)
	}
}

func TestNormalizeListFiltersAtLastMonthYearBoundary(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
	}

	last, err := normalizeListFiltersAt(cfg, ListFilters{LastMonth: true}, false, fixedNow)
	if err != nil {
		t.Fatalf("last month filters failed: %v", err)
	}
	if last.From == nil || last.To == nil {
		t.Fatal("expected last month bounds")
	}
	if got := last.From.Format(time.RFC3339); got != "2025-12-01T00:00:00Z" {
		t.Fatalf("unexpected last month from %s", got)
	}
	if got := last.To.Format(time.RFC3339); got != "2025-12-31T23:59:59Z" {
		t.Fatalf("unexpected last month to %s", got)
	}
}

func TestNormalizeContextFiltersAtWeekShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	current, err := normalizeContextFiltersAt(cfg, ContextInput{CurrentWeek: true}, fixedNow)
	if err != nil {
		t.Fatalf("current week context failed: %v", err)
	}
	if current.From == nil || current.To == nil {
		t.Fatal("expected current week bounds")
	}
	if got := current.From.Format(time.RFC3339); got != "2026-05-04T00:00:00Z" {
		t.Fatalf("unexpected current week from %s", got)
	}
	if got := current.To.Format(time.RFC3339); got != "2026-05-10T23:59:59Z" {
		t.Fatalf("unexpected current week to %s", got)
	}

	last, err := normalizeContextFiltersAt(cfg, ContextInput{LastWeek: true}, fixedNow)
	if err != nil {
		t.Fatalf("last week context failed: %v", err)
	}
	if last.From == nil || last.To == nil {
		t.Fatal("expected last week bounds")
	}
	if got := last.From.Format(time.RFC3339); got != "2026-04-27T00:00:00Z" {
		t.Fatalf("unexpected last week from %s", got)
	}
	if got := last.To.Format(time.RFC3339); got != "2026-05-03T23:59:59Z" {
		t.Fatalf("unexpected last week to %s", got)
	}
}

func TestNormalizeContextFiltersAtMonthShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	current, err := normalizeContextFiltersAt(cfg, ContextInput{CurrentMonth: true}, fixedNow)
	if err != nil {
		t.Fatalf("current month context failed: %v", err)
	}
	if current.From == nil || current.To == nil {
		t.Fatal("expected current month bounds")
	}
	if got := current.From.Format(time.RFC3339); got != "2026-05-01T00:00:00Z" {
		t.Fatalf("unexpected current month from %s", got)
	}
	if got := current.To.Format(time.RFC3339); got != "2026-05-31T23:59:59Z" {
		t.Fatalf("unexpected current month to %s", got)
	}

	last, err := normalizeContextFiltersAt(cfg, ContextInput{LastMonth: true}, fixedNow)
	if err != nil {
		t.Fatalf("last month context failed: %v", err)
	}
	if last.From == nil || last.To == nil {
		t.Fatal("expected last month bounds")
	}
	if got := last.From.Format(time.RFC3339); got != "2026-04-01T00:00:00Z" {
		t.Fatalf("unexpected last month from %s", got)
	}
	if got := last.To.Format(time.RFC3339); got != "2026-04-30T23:59:59Z" {
		t.Fatalf("unexpected last month to %s", got)
	}
}

func TestNormalizeListFiltersAtRejectsMixedShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	_, err := normalizeListFiltersAt(cfg, ListFilters{Today: true, CurrentWeek: true}, false, time.Now)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestNormalizeListFiltersAtAcceptsMonthSelectorAsExplicitTimeSelector(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	if !hasListTimeSelector(ListFilters{CurrentMonth: true}) {
		t.Fatal("expected current-month to count as explicit selector")
	}
	if !hasListTimeSelector(ListFilters{LastMonth: true}) {
		t.Fatal("expected last-month to count as explicit selector")
	}
	_, err := normalizeListFiltersAt(cfg, ListFilters{CurrentMonth: true, From: "2026-05-01"}, false, time.Now)
	if err == nil {
		t.Fatal("expected mixed current-month and range validation error")
	}
}

func TestSearchMatchesCaseInsensitiveLiteralSubstringAndOrdering(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC}
	service.now = func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

	first := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T06:00:00Z",
		Duration:    "30m",
		Description: "Refined API docs",
	})
	second := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-04T06:00:00Z",
		Duration:    "45m",
		Description: "api DOCS follow-up",
	})

	active, deleted, effective, query, err := service.Search(cfg, SearchInput{
		Query: "Api DoCs",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no tombstones, got %d", len(deleted))
	}
	if query != "Api DoCs" {
		t.Fatalf("expected normalized query to preserve internal text, got %q", query)
	}
	if effective.From != nil || effective.To != nil {
		t.Fatal("expected no time selector requirement for search")
	}
	if len(active) != 2 {
		t.Fatalf("expected two matches, got %d", len(active))
	}
	if active[0].ID != second.ID || active[1].ID != first.ID {
		t.Fatalf("unexpected ordering: %#v", active)
	}
}

func TestSearchSupportsIssueDateLiteralAndDeletedMode(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC}
	service.now = func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

	literal := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T06:00:00Z",
		Duration:    "30m",
		Description: "Fix 100%_done behavior",
	})
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-04T06:00:00Z",
		Duration:    "30m",
		Description: "Fix 100percent done behavior",
	})
	if _, err := service.Delete(literal.ID, false); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	active, deleted, _, _, err := service.Search(cfg, SearchInput{
		Query: "%_done",
		ListFilters: ListFilters{
			From:  "2026-05-03",
			To:    "2026-05-04",
			Issue: "ABC-123",
		},
	})
	if err != nil {
		t.Fatalf("active search failed: %v", err)
	}
	if len(active) != 0 || len(deleted) != 0 {
		t.Fatalf("expected no active match after delete, got active=%d deleted=%d", len(active), len(deleted))
	}

	active, deleted, _, _, err = service.Search(cfg, SearchInput{
		Query: "%_done",
		ListFilters: ListFilters{
			From:        "2026-05-03",
			To:          "2026-05-04",
			Issue:       "ABC-123",
			OnlyDeleted: true,
		},
	})
	if err != nil {
		t.Fatalf("deleted search failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active matches in deleted mode, got %d", len(active))
	}
	if len(deleted) != 1 || deleted[0].ID != literal.ID {
		t.Fatalf("unexpected deleted matches: %#v", deleted)
	}
}

func TestListOrdersActiveWorklogsOldestFirst(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC}

	later := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-04T06:00:00Z",
		Duration:    "30m",
		Description: "Later",
	})
	earlier := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T06:00:00Z",
		Duration:    "30m",
		Description: "Earlier",
	})

	active, deleted, _, err := service.List(cfg, ListFilters{
		From: "2026-05-03",
		To:   "2026-05-04",
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no tombstones, got %d", len(deleted))
	}
	if len(active) != 2 {
		t.Fatalf("expected two active worklogs, got %d", len(active))
	}
	if active[0].ID != earlier.ID || active[1].ID != later.ID {
		t.Fatalf("unexpected ordering: %#v", active)
	}
}

func TestListOrdersDeletedWorklogsOldestFirst(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC}

	later := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-04T06:00:00Z",
		Duration:    "30m",
		Description: "Later",
	})
	earlier := mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T06:00:00Z",
		Duration:    "30m",
		Description: "Earlier",
	})
	if _, err := service.Delete(later.ID, false); err != nil {
		t.Fatalf("delete later failed: %v", err)
	}
	if _, err := service.Delete(earlier.ID, false); err != nil {
		t.Fatalf("delete earlier failed: %v", err)
	}

	active, deleted, _, err := service.List(cfg, ListFilters{
		From:        "2026-05-03",
		To:          "2026-05-04",
		OnlyDeleted: true,
	})
	if err != nil {
		t.Fatalf("deleted list failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active worklogs, got %d", len(active))
	}
	if len(deleted) != 2 {
		t.Fatalf("expected two tombstones, got %d", len(deleted))
	}
	if deleted[0].ID != earlier.ID || deleted[1].ID != later.ID {
		t.Fatalf("unexpected deleted ordering: %#v", deleted)
	}
}

func TestSearchRejectsBlankQuery(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC}
	_, _, _, _, err := service.Search(cfg, SearchInput{Query: "   "})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPreviewAddNormalizesAndDoesNotPersist(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}

	result, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-123",
		Started:     "2026-05-03T09:00",
		Duration:    "1h",
		Description: "  Investigated \n bug  ",
	})
	if err != nil {
		t.Fatalf("preview add failed: %v", err)
	}
	if !result.DryRun || len(result.Records) != 1 {
		t.Fatalf("unexpected preview result %#v", result)
	}
	record := result.Records[0]
	if record.ID != "" {
		t.Fatalf("expected preview to omit id, got %q", record.ID)
	}
	if record.IssueKey != "ABC-123" {
		t.Fatalf("unexpected issue key %q", record.IssueKey)
	}
	if got := record.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T09:00:00Z" {
		t.Fatalf("unexpected started_at_utc %s", got)
	}
	if record.DurationSeconds != 3600 {
		t.Fatalf("unexpected duration %d", record.DurationSeconds)
	}
	if record.Description != "Investigated bug" {
		t.Fatalf("unexpected description %q", record.Description)
	}
	if got := countActiveWorklogs(t, store); got != 0 {
		t.Fatalf("expected no persisted worklogs, got %d", got)
	}
}

func TestPreviewAddEnforcesConflictsAndForce(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T06:00:00Z",
		Duration:    "1h",
		Description: "First",
	})

	_, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-03T06:30:00Z",
		Duration:    "1h",
		Description: "Overlap",
	})
	if err == nil {
		t.Fatal("expected preview conflict")
	}

	result, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		StartedUTC:  "2026-05-03T06:30:00Z",
		Duration:    "1h",
		Description: "Overlap",
		Force:       true,
	})
	if err != nil {
		t.Fatalf("forced preview add failed: %v", err)
	}
	record := result.Records[0]
	if record.ID != "" {
		t.Fatalf("expected forced preview to omit id, got %q", record.ID)
	}
	if got := countActiveWorklogs(t, store); got != 1 {
		t.Fatalf("expected persisted row count to stay 1, got %d", got)
	}
}

func TestPreviewAddSnapSplitsAcrossLunch(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{
		Location:               time.UTC,
		MinimumDurationSeconds: 900,
		DayStart:               "09:00",
		DayEnd:                 "17:00",
		DailyLunch:             "12:00-12:45",
	}
	service.now = func() time.Time {
		return time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)
	}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T09:00:00Z",
		Duration:    "2h",
		Description: "Morning",
	})

	result, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		Snap:        true,
		Today:       true,
		Duration:    "2h",
		Description: "Snapped",
	})
	if err != nil {
		t.Fatalf("snap preview failed: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 snapped records, got %#v", result)
	}
	first := result.Records[0]
	second := result.Records[1]
	if got := first.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T11:00:00Z" {
		t.Fatalf("unexpected first start %s", got)
	}
	if first.DurationSeconds != 3600 {
		t.Fatalf("unexpected first duration %d", first.DurationSeconds)
	}
	if got := second.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T12:45:00Z" {
		t.Fatalf("unexpected second start %s", got)
	}
	if second.DurationSeconds != 3600 {
		t.Fatalf("unexpected second duration %d", second.DurationSeconds)
	}
}

func TestPreviewAddSnapSkipsLunchSplitBelowMinimumAndFindsLaterSlot(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{
		Location:               time.UTC,
		MinimumDurationSeconds: 900,
		DayStart:               "09:00",
		DayEnd:                 "17:00",
		DailyLunch:             "12:00-12:45",
	}
	service.now = func() time.Time {
		return time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)
	}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T09:00:00Z",
		Duration:    "2h50m",
		Description: "Morning",
	})

	result, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		Snap:        true,
		Today:       true,
		Duration:    "30m",
		Description: "Snapped",
	})
	if err != nil {
		t.Fatalf("snap preview failed: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 snapped record, got %#v", result)
	}
	record := result.Records[0]
	if got := record.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T12:45:00Z" {
		t.Fatalf("unexpected start %s", got)
	}
	if record.DurationSeconds != 1800 {
		t.Fatalf("unexpected duration %d", record.DurationSeconds)
	}
}

func TestPreviewAddSnapReturnsNoFitWhenLunchSplitFragmentsFallBelowMinimum(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{
		Location:               time.UTC,
		MinimumDurationSeconds: 900,
		DayStart:               "09:00",
		DayEnd:                 "17:00",
		DailyLunch:             "12:00-12:45",
	}
	service.now = func() time.Time {
		return time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)
	}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T09:00:00Z",
		Duration:    "2h50m",
		Description: "Morning",
	})
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-125",
		StartedUTC:  "2026-05-03T12:45:00Z",
		Duration:    "4h15m",
		Description: "Afternoon",
	})

	_, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		Snap:        true,
		Today:       true,
		Duration:    "30m",
		Description: "Snapped",
	})
	if err == nil {
		t.Fatal("expected snap no-fit validation error")
	}
	validationErr, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("expected validation error, got %T", err)
	}
	if len(validationErr.Issues) != 1 {
		t.Fatalf("expected one validation issue, got %#v", validationErr)
	}
	issue := validationErr.Issues[0]
	if issue.Field != "snap" || issue.Message != "no snapped placement fits inside the selected date window" {
		t.Fatalf("unexpected validation issue %#v", issue)
	}
}

func TestPreviewAddSnapAllowsLunchSplitFragmentAtMinimumBoundary(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{
		Location:               time.UTC,
		MinimumDurationSeconds: 900,
		DayStart:               "09:00",
		DayEnd:                 "17:00",
		DailyLunch:             "12:00-12:45",
	}
	service.now = func() time.Time {
		return time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)
	}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T09:00:00Z",
		Duration:    "2h45m",
		Description: "Morning",
	})

	result, err := service.PreviewAdd(cfg, AddInput{
		IssueKey:    "ABC-124",
		Snap:        true,
		Today:       true,
		Duration:    "30m",
		Description: "Snapped",
	})
	if err != nil {
		t.Fatalf("snap preview failed: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 snapped records, got %#v", result)
	}
	first := result.Records[0]
	second := result.Records[1]
	if got := first.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T11:45:00Z" {
		t.Fatalf("unexpected first start %s", got)
	}
	if first.DurationSeconds != 900 {
		t.Fatalf("unexpected first duration %d", first.DurationSeconds)
	}
	if got := second.StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T12:45:00Z" {
		t.Fatalf("unexpected second start %s", got)
	}
	if second.DurationSeconds != 900 {
		t.Fatalf("unexpected second duration %d", second.DurationSeconds)
	}
}

func TestAddSnapWarnsWhenExtendingPastDayEnd(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	cfg := config.EffectiveConfig{
		Location:               time.UTC,
		MinimumDurationSeconds: 900,
		DayStart:               "09:00",
		DayEnd:                 "17:00",
		DailyLunch:             "12:00-12:45",
	}
	service.now = func() time.Time {
		return time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC)
	}
	mustAddWorklog(t, service, cfg, AddInput{
		IssueKey:    "ABC-123",
		StartedUTC:  "2026-05-03T09:00:00Z",
		Duration:    "7h",
		Description: "Busy",
	})

	result, err := service.Add(cfg, AddInput{
		IssueKey:    "ABC-124",
		Snap:        true,
		Today:       true,
		Duration:    "2h",
		Description: "Overflow",
	})
	if err != nil {
		t.Fatalf("snap add failed: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != "day_end_boundary_reached" {
		t.Fatalf("expected day_end warning, got %#v", result.Warnings)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record, got %#v", result)
	}
	if got := result.Records[0].StartedAtUTC.Format(time.RFC3339); got != "2026-05-03T16:00:00Z" {
		t.Fatalf("unexpected start %s", got)
	}
	if got := countActiveWorklogs(t, store); got != 2 {
		t.Fatalf("expected two persisted rows, got %d", got)
	}
}

func newTestService(t *testing.T) (*sqlitestore.Store, *Service) {
	t.Helper()

	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatalf("bootstrap store: %v", err)
	}
	return store, NewService(store)
}

func mustAddWorklog(t *testing.T, service *Service, cfg config.EffectiveConfig, input AddInput) LocalWorklog {
	t.Helper()

	item, err := service.Add(cfg, input)
	if err != nil {
		t.Fatalf("add worklog: %v", err)
	}
	if len(item.Records) != 1 {
		t.Fatalf("expected single record add result, got %#v", item)
	}
	return item.Records[0]
}

func countActiveWorklogs(t *testing.T, store *sqlitestore.Store) int {
	t.Helper()

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM worklogs`).Scan(&count); err != nil {
		if err == sql.ErrNoRows {
			return 0
		}
		t.Fatalf("count worklogs: %v", err)
	}
	return count
}
