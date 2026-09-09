package reconcile

import (
	"fmt"
	"testing"
	"time"
)

func TestListPlansByIDPrefixIsBoundedAndNewestFirst(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("plan-%03d", i)
		createdAt := time.Date(2026, 5, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339)
		if _, err := store.DB().Exec(
			`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status) VALUES(?, 'push', 'clockify', 'fingerprint', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', ?, 'ready')`,
			id,
			createdAt,
		); err != nil {
			t.Fatalf("seed plan %d: %v", i, err)
		}
	}

	items, err := NewService(store).ListPlansByIDPrefix("PLAN-", 100)
	if err != nil {
		t.Fatalf("list plan candidates: %v", err)
	}
	if len(items) != 100 {
		t.Fatalf("expected bounded 100 candidates, got %d", len(items))
	}
	if items[0].ID != "plan-104" || items[99].ID != "plan-005" {
		t.Fatalf("expected newest-first candidates, first=%s last=%s", items[0].ID, items[99].ID)
	}
}

func TestListRecentPlansInCreatedRangeUsesHalfOpenBoundsAndLimit(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for id, createdAt := range map[string]string{
		"before": "2026-04-30T23:59:59Z",
		"from":   "2026-05-01T00:00:00Z",
		"inside": "2026-05-07T23:59:59Z",
		"to":     "2026-05-08T00:00:00Z",
		"after":  "2026-05-08T00:00:01Z",
	} {
		if _, err := store.DB().Exec(
			`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status) VALUES(?, 'push', 'clockify', 'fingerprint', '2026-05-01T00:00:00Z', '2026-05-01T23:59:59Z', ?, 'ready')`,
			id,
			createdAt,
		); err != nil {
			t.Fatalf("seed plan %s: %v", id, err)
		}
	}

	service := NewService(store)
	items, err := service.ListRecentPlansInCreatedRange(100, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("list plans in range: %v", err)
	}
	if len(items) != 2 || items[0].ID != "inside" || items[1].ID != "from" {
		t.Fatalf("expected half-open newest-first range, got %#v", items)
	}

	limited, err := service.ListRecentPlansInCreatedRange(1, mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-08T00:00:00Z"))
	if err != nil {
		t.Fatalf("list bounded plans in range: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "inside" {
		t.Fatalf("expected newest bounded result, got %#v", limited)
	}
}
