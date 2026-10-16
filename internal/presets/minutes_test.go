package presets

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

func TestPresetCreationUpdateAndApplyRoundMinutes(t *testing.T) {
	store, service, cfg := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	input := validInput("rounded")
	input.Duration = "15m30s"
	preset, err := service.Create(ctx, cfg, input)
	if err != nil || preset.DurationSeconds != 960 {
		t.Fatalf("create = %#v, %v", preset, err)
	}
	duration := "16m30s"
	preset, err = service.Update(ctx, cfg, preset.Name, PatchInput{Duration: &duration, ExpectedRevision: preset.Revision})
	if err != nil || preset.DurationSeconds != 1020 {
		t.Fatalf("update = %#v, %v", preset, err)
	}
	result, err := service.Apply(ctx, cfg, preset.Name, ApplyInput{Date: "2026-05-01", DryRun: true})
	if err != nil || result.Records[0].DurationSeconds != 1020 {
		t.Fatalf("apply = %#v, %v", result, err)
	}
}

func TestApplyRetainedSecondPrecisionPresetAfterBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE worklog_presets (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, issue_key TEXT NOT NULL, start_time TEXT NOT NULL,
		duration_seconds INTEGER NOT NULL, description TEXT NOT NULL, created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL, last_used_at TEXT NULL, revision INTEGER NOT NULL DEFAULT 1
	);
	INSERT INTO worklog_presets VALUES('preset', 'standup', 'APP-1', '09:00', 931, 'Daily standup', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL, 1);
	CREATE TRIGGER trg_presets_minutes_update BEFORE UPDATE ON worklog_presets
	WHEN typeof(NEW.duration_seconds) != 'integer' OR NEW.duration_seconds <= 0 OR NEW.duration_seconds % 60 != 0
	BEGIN SELECT RAISE(ABORT, 'presets require positive whole-minute durations'); END`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, _, err := sqlitestore.Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(store)
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}
	before, err := service.Show(t.Context(), "standup")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Apply(t.Context(), cfg, before.Name, ApplyInput{Date: "2026-05-01", DryRun: true})
	if err != nil || len(preview.Records) != 1 || preview.Records[0].DurationSeconds != 960 {
		t.Fatalf("dry apply = %#v, %v", preview, err)
	}
	for _, method := range []string{"apply", "draft"} {
		t.Run(method, func(t *testing.T) {
			var result worklogs.AddResult
			var err error
			if method == "apply" {
				result, err = service.Apply(t.Context(), cfg, before.Name, ApplyInput{Date: "2026-05-01"})
			} else {
				result, err = service.ApplyDraft(t.Context(), cfg, before.ID, before.Revision, worklogs.AddInput{
					IssueKey: before.IssueKey, StartedUTC: "2026-05-02T09:00:00Z", Duration: "931s", Description: before.Description,
				})
			}
			if err != nil || len(result.Records) != 1 || result.Records[0].DurationSeconds != preview.Records[0].DurationSeconds {
				t.Fatalf("apply = %#v, %v", result, err)
			}
			after, err := service.Show(t.Context(), before.Name)
			if err != nil || after.LastUsedAt == nil {
				t.Fatalf("recency was not saved: %#v, %v", after, err)
			}
			after.LastUsedAt = nil
			if after != before {
				t.Fatalf("apply changed preset content: %#v", after)
			}
		})
	}
}
