package worklogs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestDeleteArchivesAndRestoreConsumesLocalTrash(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	createdAt := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return createdAt }
	added, err := service.Add(ctx, cfg, AddInput{IssueKey: "APP-1", StartedUTC: "2026-05-21T09:00:00Z", Duration: "1h", Description: "restorable"})
	if err != nil {
		t.Fatal(err)
	}
	original := added.Records[0]
	deletedAt := createdAt.Add(time.Hour)
	service.now = func() time.Time { return deletedAt }
	deleted, err := service.Delete(ctx, original.ID, original.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.TrashID == "" {
		t.Fatal("delete did not return trash id")
	}
	trash, err := service.ShowTrash(deleted.TrashID)
	if err != nil {
		t.Fatal(err)
	}
	if trash.SourceCreatedAt == nil || !trash.SourceCreatedAt.Equal(createdAt) || trash.SourceRevision == nil || *trash.SourceRevision != 1 || trash.ReasonCode != "local_user_deleted" {
		t.Fatalf("unexpected archive: %#v", trash)
	}
	restoredAt := deletedAt.Add(time.Hour)
	service.now = func() time.Time { return restoredAt }
	restored, err := service.RestoreTrash(ctx, cfg, deleted.TrashID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Record.ID != original.ID || !restored.Record.CreatedAt.Equal(createdAt) || !restored.Record.UpdatedAt.Equal(restoredAt) || restored.Record.Revision != 2 {
		t.Fatalf("unexpected restored record: %#v", restored.Record)
	}
	if _, err := service.ShowTrash(deleted.TrashID); !errors.Is(err, ErrTrashNotFound) {
		t.Fatalf("consumed trash error = %v", err)
	}
	if _, err := service.RestoreTrash(ctx, cfg, deleted.TrashID); !errors.Is(err, ErrTrashNotFound) {
		t.Fatalf("second restore error = %v", err)
	}
}

func TestDeleteRollsBackWhenTrashArchivalFails(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	added, err := service.Add(ctx, cfg, AddInput{IssueKey: "APP-1", StartedUTC: "2026-05-21T09:00:00Z", Duration: "1h", Description: "keep me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_trash BEFORE INSERT ON trashed_worklogs BEGIN SELECT RAISE(ABORT, 'archive failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delete(ctx, added.Records[0].ID, added.Records[0].Revision); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if _, err := service.Show(ctx, added.Records[0].ID); err != nil {
		t.Fatalf("active row was removed after archive failure: %v", err)
	}
}

func TestRestoreRejectsRemoteAndConflictsWithoutMutation(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES
		('remote-trash','remote',NULL,'APP-1','2026-05-21T09:00:00Z',3600,'remote','2026-05-22T09:00:00Z','remote_deleted','','push'),
		('local-trash','local','restored-id','APP-2','2026-05-21T10:00:00Z',3600,'local','2026-05-22T09:00:00Z','local_user_deleted','','local')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RestoreTrash(ctx, cfg, "remote-trash"); !errors.Is(err, ErrValidation) {
		t.Fatalf("remote restore error = %v", err)
	}
	if _, err := service.Add(ctx, cfg, AddInput{IssueKey: "APP-3", StartedUTC: "2026-05-21T10:30:00Z", Duration: "1h", Description: "active overlap"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RestoreTrash(ctx, cfg, "local-trash"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict restore error = %v", err)
	}
	if _, err := service.ShowTrash("local-trash"); err != nil {
		t.Fatalf("conflicting restore consumed trash: %v", err)
	}
}

func TestLegacyLocalTrashRestoresWithFallbackMetadata(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES('legacy','local','legacy-id','APP-1','2026-05-21T09:00:00Z',3600,'legacy','2026-05-22T09:00:00Z','pull_merge_removed_local','','pull')`); err != nil {
		t.Fatal(err)
	}
	restoredAt := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return restoredAt }
	item, err := service.RestoreTrash(ctx, cfg, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if !item.Record.CreatedAt.Equal(restoredAt) || !item.Record.UpdatedAt.Equal(restoredAt) || item.Record.Revision != 1 {
		t.Fatalf("legacy fallback = %#v", item.Record)
	}
}

func TestTrashScopeFiltering(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES ('l','local','l-id','APP-1','2026-05-21T09:00:00Z',900,'local','2026-05-22T09:00:00Z','deleted','','local'), ('r','remote',NULL,'APP-2','2026-05-21T10:00:00Z',900,'remote','2026-05-22T09:00:00Z','deleted','','push')`); err != nil {
		t.Fatal(err)
	}
	cfg := config.EffectiveConfig{Location: time.UTC}
	items, _, err := service.ListTrash(cfg, TrashFilters{ListFilters: ListFilters{From: "2026-05-21", To: "2026-05-21"}, StorageScope: TrashScopeLocal})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "l" {
		t.Fatalf("local filter = %#v", items)
	}
	if _, _, err := service.ListTrash(cfg, TrashFilters{ListFilters: ListFilters{Today: true}, StorageScope: "invalid"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid scope error = %v", err)
	}
}

func TestBatchRestoreRejectsInternalConflictsAndChangedMembershipAtomically(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES ('one','local','one-id','APP-1','2026-05-21T09:00:00Z',3600,'one','2026-05-22T09:00:00Z','deleted','','local'), ('two','local','two-id','APP-2','2026-05-21T09:30:00Z',3600,'two','2026-05-22T09:00:00Z','deleted','','local')`); err != nil {
		t.Fatal(err)
	}
	filters := TrashFilters{ListFilters: ListFilters{From: "2026-05-21", To: "2026-05-21"}, StorageScope: TrashScopeLocal}
	if _, err := service.RestoreTrashBatchExpected(ctx, cfg, filters, []string{"one"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("membership error=%v", err)
	}
	if _, err := service.RestoreTrashBatch(ctx, cfg, filters, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("internal conflict error=%v", err)
	}
	var active, trash int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM worklogs`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM trashed_worklogs`).Scan(&trash); err != nil {
		t.Fatal(err)
	}
	if active != 0 || trash != 2 {
		t.Fatalf("partial batch restore active=%d trash=%d", active, trash)
	}
}
