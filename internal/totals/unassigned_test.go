package totals

import (
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestUnassignedCountsLocallyAndKeepsAdapterScoping(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	insertTestWorklog(t, store, "unassigned", "", "2026-05-01T09:00:00Z", 3600)
	insertTestWorklog(t, store, "assigned", "AAPP-1", "2026-05-01T11:00:00Z", 1800)
	service := NewService(store)
	cfg := config.EffectiveConfig{Location: time.UTC}
	from, to := mustTime("2026-05-01T00:00:00Z"), mustTime("2026-05-01T23:59:59Z")
	local, err := service.SummarizeLocal(t.Context(), cfg, from, to)
	if err != nil || local.LocalTotalSeconds != 5400 {
		t.Fatalf("local=%#v err=%v", local, err)
	}
	clockify, err := service.CompareClockify(t.Context(), cfg, from, to, nil)
	if err != nil || clockify.LocalTotalSeconds != 5400 {
		t.Fatalf("clockify=%#v err=%v", clockify, err)
	}
	for _, family := range []string{"cloud", "data"} {
		var result Result
		if family == "cloud" {
			result, err = service.CompareRowsWithLocalScope(t.Context(), cfg, from, to, nil, []string{"AAPP"}, nil)
		} else {
			result, err = service.CompareJiraDataWithExclusions(t.Context(), cfg, from, to, nil, []string{"AAPP"}, nil)
		}
		if err != nil || result.LocalTotalSeconds != 1800 {
			t.Fatalf("jira %s=%#v err=%v", family, result, err)
		}
	}
}
