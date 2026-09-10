package worklogs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

func TestDeleteTrashDryRunAndExecution(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	seedTrashDeleteRecord(t, store, "local-one", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")

	dry, err := service.DeleteTrash(context.Background(), "local-one", true)
	if err != nil {
		t.Fatalf("dry delete: %v", err)
	}
	if !dry.DryRun || len(dry.Items) != 1 || len(dry.DeletedIDs) != 0 {
		t.Fatalf("unexpected dry result: %#v", dry)
	}
	if _, err := service.ShowTrash("local-one"); err != nil {
		t.Fatalf("dry delete removed row: %v", err)
	}

	result, err := service.DeleteTrash(context.Background(), "local-one", false)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if result.DryRun || len(result.Items) != 1 || len(result.DeletedIDs) != 1 || result.DeletedIDs[0] != "local-one" {
		t.Fatalf("unexpected delete result: %#v", result)
	}
	if _, err := service.ShowTrash("local-one"); !errors.Is(err, ErrTrashNotFound) {
		t.Fatalf("deleted row error = %v, want not found", err)
	}
	if _, err := service.DeleteTrash(context.Background(), "missing", false); !errors.Is(err, ErrTrashNotFound) {
		t.Fatalf("missing delete error = %v, want not found", err)
	}
}

func TestDeleteTrashBatchFiltersScopeStartAndInclusiveTrashAge(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	seedTrashDeleteRecord(t, store, "lower", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", sqlitestore.RFC3339UTC(now.Add(-15*time.Minute)))
	seedTrashDeleteRecord(t, store, "upper", TrashScopeLocal, "APP-1", "2026-05-21T10:00:00Z", sqlitestore.RFC3339UTC(now))
	seedTrashDeleteRecord(t, store, "too-old", TrashScopeLocal, "APP-1", "2026-05-21T11:00:00Z", sqlitestore.RFC3339UTC(now.Add(-15*time.Minute-time.Second)))
	seedTrashDeleteRecord(t, store, "future", TrashScopeLocal, "APP-1", "2026-05-21T12:00:00Z", sqlitestore.RFC3339UTC(now.Add(time.Second)))
	seedTrashDeleteRecord(t, store, "remote", TrashScopeRemote, "APP-1", "2026-05-21T13:00:00Z", sqlitestore.RFC3339UTC(now.Add(-time.Minute)))
	seedTrashDeleteRecord(t, store, "wrong-issue", TrashScopeLocal, "OPS-1", "2026-05-21T14:00:00Z", sqlitestore.RFC3339UTC(now.Add(-time.Minute)))
	seedTrashDeleteRecord(t, store, "wrong-date", TrashScopeLocal, "APP-1", "2026-05-20T09:00:00Z", sqlitestore.RFC3339UTC(now.Add(-time.Minute)))

	nowCalls := 0
	service.now = func() time.Time {
		nowCalls++
		return now
	}
	result, err := service.DeleteTrashBatch(context.Background(), cfg, TrashDeleteFilters{
		ListFilters:   ListFilters{Issue: "APP-1", From: "2026-05-21", To: "2026-05-21"},
		StorageScope:  TrashScopeLocal,
		TrashedWithin: "15m",
	}, true)
	if err != nil {
		t.Fatalf("batch dry delete: %v", err)
	}
	if nowCalls != 1 {
		t.Fatalf("clock calls = %d, want 1", nowCalls)
	}
	if len(result.Items) != 2 || result.Items[0].ID != "lower" || result.Items[1].ID != "upper" {
		t.Fatalf("unexpected filtered rows: %#v", result.Items)
	}
	if result.Filters.TrashedFrom == nil || !result.Filters.TrashedFrom.Equal(now.Add(-15*time.Minute)) || result.Filters.TrashedTo == nil || !result.Filters.TrashedTo.Equal(now) {
		t.Fatalf("unexpected effective trash window: %#v", result.Filters)
	}
}

func TestDeleteTrashBatchScopeAloneAndZeroMatches(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC}
	seedTrashDeleteRecord(t, store, "local", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")
	seedTrashDeleteRecord(t, store, "remote", TrashScopeRemote, "APP-2", "2026-05-21T10:00:00Z", "2026-05-22T09:00:00Z")

	result, err := service.DeleteTrashBatch(context.Background(), cfg, TrashDeleteFilters{StorageScope: TrashScopeLocal}, false)
	if err != nil {
		t.Fatalf("scope-only delete: %v", err)
	}
	if len(result.DeletedIDs) != 1 || result.DeletedIDs[0] != "local" {
		t.Fatalf("unexpected scope deletion: %#v", result)
	}
	zero, err := service.DeleteTrashBatch(context.Background(), cfg, TrashDeleteFilters{ListFilters: ListFilters{Issue: "NONE-1"}}, false)
	if err != nil || len(zero.Items) != 0 || len(zero.DeletedIDs) != 0 {
		t.Fatalf("zero-match result=%#v err=%v", zero, err)
	}
	if _, err := service.ShowTrash("remote"); err != nil {
		t.Fatalf("scope deletion removed remote row: %v", err)
	}
}

func TestDeleteTrashBatchValidation(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC}
	for _, filters := range []TrashDeleteFilters{
		{},
		{StorageScope: "invalid"},
		{TrashedWithin: "invalid"},
		{TrashedWithin: "0s"},
		{TrashedWithin: "-1s"},
		{TrashedWithin: "1500ms"},
	} {
		if _, err := service.DeleteTrashBatch(context.Background(), cfg, filters, true); !errors.Is(err, ErrValidation) {
			t.Fatalf("filters=%#v error=%v, want validation", filters, err)
		}
	}
}

func TestClearTrashDeletesBothScopesWithoutChangingActiveOrPlans(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	seedTrashDeleteRecord(t, store, "local", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")
	seedTrashDeleteRecord(t, store, "remote", TrashScopeRemote, "APP-2", "2026-05-21T10:00:00Z", "2026-05-22T09:00:00Z")
	if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at, revision) VALUES('active','APP-3','2026-05-21T11:00:00Z',900,'active','2026-05-22T09:00:00Z','2026-05-22T09:00:00Z',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status) VALUES('plan','pull','clockify','fp','2026-05-21T00:00:00Z','2026-05-21T23:59:59Z','2026-05-22T09:00:00Z','ready')`); err != nil {
		t.Fatal(err)
	}

	dry, err := service.ClearTrash(context.Background(), true)
	if err != nil || len(dry.Items) != 2 || len(dry.DeletedIDs) != 0 {
		t.Fatalf("clear dry result=%#v err=%v", dry, err)
	}
	result, err := service.ClearTrash(context.Background(), false)
	if err != nil || len(result.DeletedIDs) != 2 {
		t.Fatalf("clear result=%#v err=%v", result, err)
	}
	for table, want := range map[string]int{"trashed_worklogs": 0, "worklogs": 1, "saved_plans": 1} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d err=%v, want %d", table, count, err, want)
		}
	}
}

func TestClearTrashRollsBackPartialFailureAndHonorsCancellation(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	seedTrashDeleteRecord(t, store, "one", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")
	seedTrashDeleteRecord(t, store, "two", TrashScopeRemote, "APP-2", "2026-05-21T10:00:00Z", "2026-05-22T09:00:00Z")
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_second_trash_delete BEFORE DELETE ON trashed_worklogs WHEN OLD.id = 'two' BEGIN SELECT RAISE(ABORT, 'reject delete'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClearTrash(context.Background(), false); err == nil {
		t.Fatal("expected clear failure")
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("partial clear count=%d err=%v", count, err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER reject_second_trash_delete`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ClearTrash(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled clear error=%v", err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("canceled clear count=%d err=%v", count, err)
	}
}

func TestPermanentDeleteRacesRestoreWithoutPartialState(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	seedTrashDeleteRecord(t, store, "race", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")
	cfg := config.EffectiveConfig{Location: time.UTC}
	start := make(chan struct{})
	deleteResult := make(chan error, 1)
	restoreResult := make(chan error, 1)
	go func() {
		<-start
		_, err := service.DeleteTrash(context.Background(), "race", false)
		deleteResult <- err
	}()
	go func() {
		<-start
		_, err := service.RestoreTrash(context.Background(), cfg, "race")
		restoreResult <- err
	}()
	close(start)
	deleteErr := <-deleteResult
	restoreErr := <-restoreResult
	if (deleteErr == nil) == (restoreErr == nil) {
		t.Fatalf("exactly one operation must succeed: delete=%v restore=%v", deleteErr, restoreErr)
	}
	loser := deleteErr
	if loser == nil {
		loser = restoreErr
	}
	if !errors.Is(loser, ErrTrashNotFound) {
		t.Fatalf("losing operation error=%v, want trash not found", loser)
	}
	var trashCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs WHERE id = 'race'`).Scan(&trashCount); err != nil || trashCount != 0 {
		t.Fatalf("race trash count=%d err=%v", trashCount, err)
	}
}

func TestPermanentDeleteTransactionExcludesLaterTrashInsert(t *testing.T) {
	store, _ := newTestService(t)
	defer store.Close()
	seedTrashDeleteRecord(t, store, "selected", TrashScopeLocal, "APP-1", "2026-05-21T09:00:00Z", "2026-05-22T09:00:00Z")
	ctx := context.Background()
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	selected, err := listTrashForDeleteWithQueryer(ctx, conn, EffectiveTrashDeleteFilters{})
	if err != nil || len(selected) != 1 {
		t.Fatalf("selection=%#v err=%v", selected, err)
	}

	insertStarted := make(chan struct{})
	insertDone := make(chan error, 1)
	go func() {
		close(insertStarted)
		_, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES('late','remote','APP-2','2026-05-21T10:00:00Z',900,'late','2026-05-22T10:00:00Z','test','','push')`)
		insertDone <- err
	}()
	<-insertStarted
	if _, err := deleteTrashRecordsTx(ctx, conn, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := <-insertDone; err != nil {
		t.Fatalf("late insert: %v", err)
	}
	var selectedCount, lateCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs WHERE id = 'selected'`).Scan(&selectedCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs WHERE id = 'late'`).Scan(&lateCount); err != nil {
		t.Fatal(err)
	}
	if selectedCount != 0 || lateCount != 1 {
		t.Fatalf("selected=%d late=%d", selectedCount, lateCount)
	}
}

func seedTrashDeleteRecord(t *testing.T, store *sqlitestore.Store, id, scope, issue, startedAt, trashedAt string) {
	t.Helper()
	sourceID := any(nil)
	if scope == TrashScopeLocal {
		sourceID = id + "-source"
	}
	if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES(?,?,?,?,?,900,?,?,?,?,?)`,
		id, scope, sourceID, issue, startedAt, id+" description", trashedAt, "test_deleted", "test", "local"); err != nil {
		t.Fatalf("seed trash %s: %v", id, err)
	}
}
