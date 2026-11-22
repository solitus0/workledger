package worklogs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestWorklogMetadataFilter(t *testing.T) {
	metadata := Metadata(`{"parent":{"a/b~c":[null, true, 1, 1.00, 9007199254740993, "1", ["nested"]]}}`)
	for _, test := range []struct {
		path, value string
		matches     bool
	}{
		{"/parent/a~1b~0c", "null", true},
		{"/parent/a~1b~0c", "true", true},
		{"/parent/a~1b~0c", "9007199254740993", true},
		{"/parent/a~1b~0c", "9007199254740992", false},
		{"/parent/a~1b~0c", "1.0", false},
		{"/parent/a~1b~0c/2", `"1"`, false},
		{"/parent/a~1b~0c/5", `"1"`, true},
		{"/parent/a~1b~0c", `"nested"`, false},
		{"/parent/missing", "null", false},
	} {
		filter, err := NewMetadataFilter(test.path, test.value)
		if err != nil || filter.matches(metadata) != test.matches {
			t.Errorf("filter %q %s: want %v, got error %v", test.path, test.value, test.matches, err)
		}
	}
	filter, err := NewMetadataFilter("", "null")
	if err != nil || filter.matches("") || !filter.matches("null") {
		t.Fatalf("root null and unset must differ: %v", err)
	}
}

func TestWorklogMetadataRoundTripAndRestore(t *testing.T) {
	store, service := newTestService(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900, DailyLunch: config.DefaultDailyLunch}
	values := []Metadata{"", "null", `""`, "true", "9007199254740993", `[1,false]`, `{"large":9007199254740993}`}
	start := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	for _, value := range values {
		record := mustAddWorklog(t, service, cfg, AddInput{
			StartedUTC: start.Format(time.RFC3339), Duration: "20m", Description: "Verified metadata storage", Metadata: value,
		})
		stored, err := service.Show(ctx, record.ID)
		if err != nil || stored.Metadata != value {
			t.Fatalf("round trip %q: got %q, %v", value, stored.Metadata, err)
		}
		start = start.Add(time.Hour)
	}
	record := mustAddWorklog(t, service, cfg, AddInput{
		StartedUTC: start.Format(time.RFC3339), Duration: "20m", Description: "Verified metadata lifecycle", Metadata: `{"codex":{"thread_ids":["thread-one"]}}`,
	})
	updatedDescription := "Verified metadata through an unrelated edit"
	updated, err := service.Update(ctx, cfg, record.ID, PatchInput{Description: &updatedDescription, ExpectedRevision: record.Revision})
	if err != nil || updated.Metadata != record.Metadata {
		t.Fatalf("unrelated edit: %+v, %v", updated, err)
	}
	placements, err := service.PreviewMove(ctx, cfg, record.ID, updated.Revision)
	if err != nil || len(placements.Positions) == 0 {
		t.Fatalf("preview move: %+v, %v", placements, err)
	}
	moved, err := service.Move(ctx, cfg, record.ID, updated.Revision, placements.Positions[0])
	if err != nil || moved.Metadata != record.Metadata {
		t.Fatalf("move: %+v, %v", moved, err)
	}
	deleted, err := service.Delete(ctx, record.ID, moved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.RestoreTrash(ctx, cfg, deleted.TrashID)
	if err != nil || restored.Record.Metadata != record.Metadata || restored.Record.ID != record.ID {
		t.Fatalf("restore: %+v, %v", restored, err)
	}
	stored, err := service.Show(ctx, record.ID)
	if err != nil || stored.Metadata != record.Metadata {
		t.Fatalf("persisted restore: %+v, %v", stored, err)
	}
}

func TestConcurrentApplyMetadataRejectsReplay(t *testing.T) {
	store, service := newTestService(t)
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}
	payload, err := ParseRawApplyPayload([]byte(`{"adds":[{"started_at_utc":"2026-05-14T08:00:00Z","duration_seconds":1200,"description":"Verified concurrent apply","metadata":{"source":"test"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	const attempts = 8
	results := make(chan error, attempts)
	for range attempts {
		go func() {
			<-start
			_, err := service.Apply(context.Background(), cfg, payload, false, false)
			results <- err
		}()
	}
	close(start)
	var saved int
	for range attempts {
		err := <-results
		if err == nil {
			saved++
		} else if !errors.Is(err, ErrConflict) {
			t.Errorf("expected a conflict, got %v", err)
		}
	}
	if count := countActiveWorklogs(t, store); saved != 1 || count != 1 {
		t.Fatalf("expected one successful apply and one row, got %d and %d", saved, count)
	}
	payload.Adds[0].Metadata = json.RawMessage(`{"source":"other"}`)
	if _, err := service.Apply(context.Background(), cfg, payload, false, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("changing metadata should leave duplicate identity unchanged: %v", err)
	}
}
