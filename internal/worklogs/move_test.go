package worklogs

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestMovePreviewAndSavePreserveRecordAndBookings(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	add := func(start, duration string) LocalWorklog {
		return mustAddWorklog(t, service, cfg, AddInput{IssueKey: "ITO-4807", Started: "2026-10-01T" + start, Duration: duration, Description: "current session work"})
	}
	first := add("08:00", "3h")
	source := add("11:00", "1h")
	source, _ = service.Show(t.Context(), source.ID)
	last := add("13:00", "2h")
	first, _ = service.Show(t.Context(), first.ID)
	last, _ = service.Show(t.Context(), last.ID)
	source, _ = service.Show(t.Context(), source.ID)
	preview, err := service.PreviewMove(t.Context(), cfg, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, position := range preview.Positions {
		got = append(got, position.Format("15:04"))
	}
	if !reflect.DeepEqual(got, []string{"11:00", "15:00", "16:00"}) {
		t.Fatalf("positions = %v", got)
	}
	unchanged, _ := service.Show(t.Context(), source.ID)
	if unchanged != source {
		t.Fatal("preview mutated source")
	}
	moved, err := service.Move(t.Context(), cfg, source.ID, source.Revision, preview.Positions[2])
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != source.ID || moved.DurationSeconds != source.DurationSeconds || moved.Description != source.Description || moved.IssueKey != source.IssueKey || moved.CreatedAt != source.CreatedAt || moved.Revision != source.Revision+1 {
		t.Fatalf("move changed content: %#v", moved)
	}
	for _, row := range []LocalWorklog{first, last} {
		actual, _ := service.Show(t.Context(), row.ID)
		if actual != row {
			t.Fatal("move changed neighbor")
		}
	}
	if countActiveWorklogs(t, store) != 3 {
		t.Fatal("move changed row count")
	}
	if _, err := service.Move(t.Context(), cfg, source.ID, source.Revision, preview.Positions[1]); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale move = %v", err)
	}
}

func TestMoveRejectsChangedAvailabilityAndInvalidDestinations(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	source := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", Started: "2026-10-01T09:00", Duration: "1h", Description: "work"})
	source, _ = service.Show(t.Context(), source.ID)
	preview, err := service.PreviewMove(t.Context(), cfg, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	destination := preview.Positions[len(preview.Positions)-1]
	mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-2", Started: "2026-10-01T16:00", Duration: "1h", Description: "work"})
	for _, start := range []string{"16:00", "12:00", "07:00", "17:00", "10:00"} {
		parsed, _ := time.Parse(time.RFC3339, "2026-10-01T"+start+":00Z")
		if start == "16:00" {
			parsed = destination
		}
		if _, err := service.Move(t.Context(), cfg, source.ID, source.Revision, parsed); !errors.Is(err, ErrPlacementChanged) {
			t.Fatalf("move to %s = %v", start, err)
		}
	}
	actual, _ := service.Show(t.Context(), source.ID)
	if actual != source {
		t.Fatal("failed move changed source")
	}
}

func TestMoveUsesLocalDayAndOvernightOccupancy(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.EffectiveConfig{Location: location, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-2", Started: "2026-09-30T23:00", Duration: "10h", Description: "work"})
	source := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", Started: "2026-10-01T10:00", Duration: "1h", Description: "work"})
	source, _ = service.Show(t.Context(), source.ID)
	preview, err := service.PreviewMove(t.Context(), cfg, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, position := range preview.Positions {
		got = append(got, position.In(location).Format("15:04"))
	}
	if !reflect.DeepEqual(got, []string{"09:00", "16:00"}) {
		t.Fatalf("positions = %v", got)
	}
}

func TestMovePackedDayAndNoOp(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	first := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", Started: "2026-10-01T08:00", Duration: "4h", Description: "work"})
	mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-2", Started: "2026-10-01T12:45", Duration: "4h15m", Description: "work"})
	first, _ = service.Show(t.Context(), first.ID)
	preview, err := service.PreviewMove(t.Context(), cfg, first.ID, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Positions) != 1 || !preview.Positions[0].Equal(first.StartedAtUTC) {
		t.Fatalf("packed positions = %v", preview.Positions)
	}
	actual, err := service.Move(t.Context(), cfg, first.ID, first.Revision, first.StartedAtUTC)
	if err != nil || actual != first {
		t.Fatalf("no-op = %#v, %v", actual, err)
	}
}

func TestMoveSkipsAmbiguousLocalDestinationsAndRejectsDeletedSource(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.EffectiveConfig{Location: location, DayStart: "02:00", DayEnd: "06:00", DailyLunch: "04:30-05:00"}
	source := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", Started: "2026-10-25T02:00", Duration: "30m", Description: "work"})
	mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-2", StartedUTC: "2026-10-25T00:00:00Z", Duration: "30m", Description: "work"})
	preview, err := service.PreviewMove(t.Context(), cfg, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, position := range preview.Positions {
		got = append(got, position.In(location).Format("15:04"))
	}
	if !reflect.DeepEqual(got, []string{"02:00", "02:30", "05:30"}) {
		t.Fatalf("DST positions = %v", got)
	}
	if _, err := service.Delete(t.Context(), source.ID, source.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Move(t.Context(), cfg, source.ID, source.Revision, preview.Positions[2]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted source move = %v", err)
	}
}

func TestMoveRespectsChangedMinimumDuration(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	source := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", Started: "2026-10-01T09:00", Duration: "15m", Description: "work"})
	cfg.MinimumDurationSeconds = 3600
	if _, err := service.PreviewMove(t.Context(), cfg, source.ID, source.Revision); !errors.Is(err, ErrValidation) {
		t.Fatalf("minimum-duration preview = %v", err)
	}
	destination := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if _, err := service.Move(t.Context(), cfg, source.ID, source.Revision, destination); !errors.Is(err, ErrValidation) {
		t.Fatalf("minimum-duration save = %v", err)
	}
	actual, _ := service.Show(t.Context(), source.ID)
	if !actual.StartedAtUTC.Equal(source.StartedAtUTC) || actual.Revision != source.Revision {
		t.Fatal("failed move changed source")
	}
}
