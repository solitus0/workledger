package presets

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func newTestService(t *testing.T) (*sqlitestore.Store, *Service, config.EffectiveConfig) {
	t.Helper()
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "workledger.db"))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cfg := config.EffectiveConfig{Location: time.UTC, TimezoneName: "UTC", MinimumDurationSeconds: 900}
	return store, NewService(store), cfg
}

func validInput(name string) CreateInput {
	return CreateInput{Name: name, IssueKey: "APP-1", StartTime: "09:00", Duration: "15m", Description: "  Daily\n standup  "}
}

func TestPresetCRUDValidationAndRename(t *testing.T) {
	store, service, cfg := newTestService(t)
	defer store.Close()
	ctx := context.Background()

	created, err := service.Create(ctx, cfg, validInput("daily-standup"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.Description != "Daily standup" || created.DurationSeconds != 900 || created.Revision != 1 {
		t.Fatalf("unexpected created preset: %+v", created)
	}
	if _, err := service.Create(ctx, cfg, validInput("daily-standup")); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := service.Create(ctx, cfg, validInput("Daily Standup")); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid slug error = %v", err)
	}

	newName, duration := "team-standup", "30m"
	updated, err := service.Update(ctx, cfg, created.Name, PatchInput{Name: &newName, Duration: &duration, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != newName || updated.DurationSeconds != 1800 || updated.Revision != 2 {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := service.Show(ctx, created.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name lookup error = %v", err)
	}
	if _, err := service.Update(ctx, cfg, newName, PatchInput{Description: ptr("changed"), ExpectedRevision: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	if _, err := service.Delete(ctx, newName, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete error = %v", err)
	}
	if _, err := service.Delete(ctx, newName, updated.Revision); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.Show(ctx, newName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted lookup error = %v", err)
	}
}

func TestPresetApplyDryOverridesConflictsAndRecency(t *testing.T) {
	store, service, cfg := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	first, err := service.Create(ctx, cfg, validInput("daily-standup"))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := service.Create(ctx, cfg, validInput("planning")); err != nil {
		t.Fatalf("create second: %v", err)
	}
	issue, start, duration, description := "OPS-2", "10:30", "30m", "Planning"
	dry, err := service.Apply(ctx, cfg, first.Name, ApplyInput{Date: "tomorrow", IssueKey: &issue, StartTime: &start, Duration: &duration, Description: &description, DryRun: true})
	if err != nil {
		t.Fatalf("dry apply: %v", err)
	}
	if len(dry.Records) != 1 || dry.Records[0].StartedAtUTC.Format("2006-01-02T15:04") != "2026-09-07T10:30" || dry.Records[0].IssueKey != issue {
		t.Fatalf("unexpected dry result: %+v", dry)
	}
	shown, _ := service.Show(ctx, first.Name)
	if shown.LastUsedAt != nil {
		t.Fatal("dry apply updated last_used_at")
	}

	applied, err := service.Apply(ctx, cfg, first.Name, ApplyInput{Date: "+1d", IssueKey: &issue, StartTime: &start, Duration: &duration, Description: &description})
	if err != nil || len(applied.Records) != 1 || applied.Records[0].ID == "" {
		t.Fatalf("apply result=%+v err=%v", applied, err)
	}
	if _, err := service.Apply(ctx, cfg, first.Name, ApplyInput{Date: "2026-09-07", IssueKey: &issue, StartTime: &start, Duration: &duration, Description: &description}); !errors.Is(err, worklogs.ErrConflict) {
		t.Fatalf("duplicate apply error = %v", err)
	}
	if _, err := service.Apply(ctx, cfg, first.Name, ApplyInput{Date: "2026-09-07", IssueKey: &issue, StartTime: &start, Duration: &duration, Description: &description, Force: true}); err != nil {
		t.Fatalf("forced apply: %v", err)
	}
	items, err := service.List(ctx, "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 || items[0].Name != first.Name || items[0].LastUsedAt == nil || items[1].Name != "planning" {
		t.Fatalf("unexpected recency order: %+v", items)
	}
	if items[0].Revision != 1 {
		t.Fatalf("application changed content revision: %d", items[0].Revision)
	}
}

func TestPresetApplyRejectsInvalidDateAndDSTTimes(t *testing.T) {
	store, service, cfg := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Location, cfg.TimezoneName = location, location.String()
	input := validInput("dst-test")
	input.StartTime = "03:30"
	if _, err := service.Create(ctx, cfg, input); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.Apply(ctx, cfg, input.Name, ApplyInput{Date: "not-a-date"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid date error = %v", err)
	}
	if _, err := service.Apply(ctx, cfg, input.Name, ApplyInput{Date: "2026-03-29"}); !errors.Is(err, worklogs.ErrValidation) {
		t.Fatalf("DST gap error = %v", err)
	}
}

func ptr(value string) *string { return &value }
