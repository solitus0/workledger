package activity

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

func TestActivityLifecycleFilteringAndOrdering(t *testing.T) {
	service, store := testService(t)
	defer store.Close()
	ctx := context.Background()
	started := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	first, err := service.Start(ctx, StartInput{ID: "a", Source: SourceCLI, Operation: "worklogs.add", Summary: "issue=APP-1", Attributes: map[string]string{"issue": "APP-1"}, StartedAt: started})
	if err != nil || first.State != StateRunning {
		t.Fatalf("start entry=%+v err=%v", first, err)
	}
	exitCode := 0
	finished, err := service.Finish(ctx, first.ID, FinishInput{State: StateSucceeded, FinishedAt: started.Add(1250 * time.Millisecond), ExitCode: &exitCode})
	if err != nil || finished.DurationMS == nil || *finished.DurationMS != 1250 || finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("finish entry=%+v err=%v", finished, err)
	}
	if _, err := service.Start(ctx, StartInput{ID: "b", Source: SourceTUI, Operation: "trash.restore", StartedAt: started.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	items, err := service.List(ctx, ListFilters{Limit: 10, Source: SourceCLI, State: StateSucceeded})
	if err != nil || len(items) != 1 || items[0].ID != "a" || items[0].Attributes["issue"] != "APP-1" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	items, err = service.List(ctx, ListFilters{Limit: 10, ExcludeID: "b"})
	if err != nil || len(items) != 1 || items[0].ID != "a" {
		t.Fatalf("excluded items=%+v err=%v", items, err)
	}
	if _, err := service.Finish(ctx, "missing", FinishInput{State: StateFailed}); err == nil {
		t.Fatal("expected missing activity error")
	}
}

func TestActivityRetentionKeepsNewestFiveHundred(t *testing.T) {
	service, store := testService(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	for index := 0; index < RetentionLimit+5; index++ {
		_, err := service.Start(ctx, StartInput{ID: fmt.Sprintf("%03d", index), Source: SourceCLI, Operation: "status", StartedAt: base.Add(time.Duration(index) * time.Second)})
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
	}
	items, err := service.List(ctx, ListFilters{Limit: RetentionLimit})
	if err != nil || len(items) != RetentionLimit || items[0].ID != "504" || items[len(items)-1].ID != "005" {
		t.Fatalf("retained=%d first=%s last=%s err=%v", len(items), items[0].ID, items[len(items)-1].ID, err)
	}
}

func TestActivityConcurrentWriters(t *testing.T) {
	service, store := testService(t)
	defer store.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			entry, err := service.Start(ctx, StartInput{ID: fmt.Sprint(index), Source: SourceCLI, Operation: "status"})
			if err == nil {
				_, err = service.Finish(ctx, entry.ID, FinishInput{State: StateSucceeded})
			}
			if err != nil {
				errors <- err
			}
		}(index)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent activity: %v", err)
	}
}

func testService(t *testing.T) (*Service, *sqlitestore.Store) {
	t.Helper()
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store), store
}
