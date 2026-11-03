package worklogs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestUnassignedLifecycleAndRetainedDuration(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}
	first := mustAddWorklog(t, service, cfg, AddInput{StartedUTC: "2026-05-01T09:00:00Z", Duration: "15m", Description: "Investigate retry"})
	second := mustAddWorklog(t, service, cfg, AddInput{StartedUTC: "2026-06-01T09:00:00Z", Duration: "15m", Description: "Review retry"})
	items, _, err := service.List(ctx, cfg, ListFilters{Unassigned: true})
	if err != nil || len(items) != 2 || items[0].ID != first.ID {
		t.Fatalf("all dates = %#v, %v", items, err)
	}
	filtered, _, err := service.List(ctx, cfg, ListFilters{Unassigned: true, From: "2026-06-01", To: "2026-06-01"})
	if err != nil || len(filtered) != 1 || filtered[0].ID != second.ID {
		t.Fatalf("date filter = %#v, %v", filtered, err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM issues`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty issue identities = %d, %v", count, err)
	}
	for _, bad := range []ListFilters{{Unassigned: true, Issue: "APP-1"}, {Unassigned: true, IssuePrefix: "APP"}, {Unassigned: true, Epic: new("Group")}} {
		if _, _, err := service.List(ctx, cfg, bad); !errors.Is(err, ErrValidation) {
			t.Fatalf("mixed filters = %v", err)
		}
	}
	cfg.MinimumDurationSeconds = 1800
	key := "APP-1"
	started := first.StartedAtUTC.Format(time.RFC3339)
	duration, description := "15m", first.Description
	updated, err := service.Update(ctx, cfg, first.ID, PatchInput{IssueKey: &key, StartedUTC: &started, Duration: &duration, Description: &description, ExpectedRevision: first.Revision})
	if err != nil || updated.ID != first.ID || updated.DurationSeconds != 900 || !updated.CreatedAt.Equal(first.CreatedAt.Truncate(time.Second)) {
		t.Fatalf("key-only update = %#v, %v", updated, err)
	}
	empty := ""
	updated, err = service.Update(ctx, cfg, first.ID, PatchInput{IssueKey: &empty, ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Assign(ctx, AssignInput{IDs: []string{first.ID, second.ID}, IssueKey: key}, true)
	if err != nil || !preview.DryRun || len(preview.Records) != 2 {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	shown, _ := service.Show(ctx, first.ID)
	if shown.IssueKey != "" || shown.Revision != updated.Revision {
		t.Fatal("dry run wrote source")
	}
	result, err := service.Assign(ctx, AssignInput{IDs: []string{first.ID}, IssueKey: key, Expected: []AssignmentExpectation{{ID: first.ID, Revision: shown.Revision}}}, false)
	if err != nil || result.Records[0].DurationSeconds != 900 {
		t.Fatalf("assign = %#v, %v", result, err)
	}
	untouched, _ := service.Show(ctx, second.ID)
	if untouched.IssueKey != "" || untouched.Revision != second.Revision {
		t.Fatal("assignment changed an unselected record")
	}
	deleted, err := service.Delete(ctx, second.ID, second.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.RestoreTrash(ctx, config.EffectiveConfig{Location: time.UTC}, deleted.TrashID)
	if err != nil {
		t.Fatalf("restore unassigned = %#v, %v", restored, err)
	}
}

func TestAssignRejectsWholeInvalidOrConflictingBatch(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC}
	a := mustAddWorklog(t, service, cfg, AddInput{StartedUTC: "2026-05-01T09:00:00Z", Duration: "1h", Description: "Unassigned"})
	b := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "OTHER-1", StartedUTC: "2026-05-01T11:00:00Z", Duration: "1h", Description: "Assigned"})
	for _, input := range []AssignInput{
		{IDs: []string{a.ID, b.ID}, IssueKey: "APP-1"},
		{IDs: []string{a.ID, "missing"}, IssueKey: "APP-1"},
		{IDs: []string{a.ID, a.ID}, IssueKey: "APP-1"},
		{IDs: []string{a.ID}, IssueKey: "APP-1", Expected: []AssignmentExpectation{{ID: a.ID, Revision: 99}}},
		{IDs: []string{a.ID}, IssueKey: ""},
	} {
		if _, err := service.Assign(t.Context(), input, false); err == nil {
			t.Fatalf("accepted %#v", input)
		}
		shown, _ := service.Show(t.Context(), a.ID)
		if shown.IssueKey != "" || shown.Revision != a.Revision {
			t.Fatal("partial assignment")
		}
	}
	// Pull's existing issue-scoped merge can leave a keyed row next to the
	// unassigned original. Assignment must expose the existing overlap conflict.
	mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", StartedUTC: "2026-05-01T09:00:00Z", Duration: "1h", Description: "Unassigned", Force: true})
	_, err := service.Assign(t.Context(), AssignInput{IDs: []string{a.ID}, IssueKey: "APP-1"}, false)
	var validation ValidationError
	if !errors.As(err, &validation) || validation.Conflict == nil || len(validation.Conflict.ConflictingIDs) != 1 {
		t.Fatalf("conflict = %#v, %v", validation, err)
	}
}

func TestUnassignedScaleAndBatchedAssignment(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES(?,'',?,900,'Unassigned activity','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	ids := make([]string, 1001)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("scale-%05d", i)
		if i < len(ids) {
			ids[i] = id
		}
		if _, err := stmt.Exec(id, start.Add(time.Duration(i)*30*time.Minute).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	items, _, err := service.List(t.Context(), config.EffectiveConfig{Location: time.UTC}, ListFilters{Unassigned: true})
	if err != nil || len(items) != 10000 || items[0].ID != ids[0] {
		t.Fatalf("backlog = %d, %v", len(items), err)
	}
	selected, err := service.LookupIDs(t.Context(), ids)
	if err != nil || len(selected) != 1001 {
		t.Fatalf("lookup = %d, %v", len(selected), err)
	}
	result, err := service.Assign(t.Context(), AssignInput{IDs: ids, IssueKey: "APP-1"}, false)
	if err != nil || len(result.Records) != 1001 {
		t.Fatalf("assignment = %d, %v", len(result.Records), err)
	}
	var unassigned int
	if err := store.DB().QueryRow(`SELECT count(*) FROM worklogs WHERE issue_key=''`).Scan(&unassigned); err != nil || unassigned != 8999 {
		t.Fatalf("remaining = %d, %v", unassigned, err)
	}
}
