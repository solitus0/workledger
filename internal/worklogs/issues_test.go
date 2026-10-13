package worklogs

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestIssuesKeyOnlyCreationEpicPatchAndClear(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	keyOnly := []IssueMetadataPatch{{IssueKey: "APP-1"}}
	if err := service.ApplyIssueMetadata(ctx, keyOnly, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ShowIssueMetadata(ctx, "APP-1"); !errors.Is(err, ErrIssueMetadataNotFound) {
		t.Fatalf("dry create changed catalog: %v", err)
	}
	if err := service.ApplyIssueMetadata(ctx, keyOnly, false); err != nil {
		t.Fatal(err)
	}
	item, err := service.ShowIssueMetadata(ctx, "APP-1")
	if err != nil || item.Title != "" || item.Epic != "" || item.MaxEstimateSeconds != nil || item.RemainingSeconds != nil || item.LoggedSeconds != 0 {
		t.Fatalf("key-only issue: %#v %v", item, err)
	}
	title, epic, estimate := "  Project title  ", "  Document lifecycle  ", int64(16*3600)
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-1", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true, MaxEstimateSeconds: &estimate, MaxEstimateSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, _ = service.ShowIssueMetadata(ctx, "APP-1")
	if item.Title != "Project title" || item.Epic != "Document lifecycle" || *item.RemainingSeconds != estimate {
		t.Fatalf("issue fields not normalized: %#v", item)
	}
	newEpic := "Redesign"
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-1", Epic: &newEpic, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, _ = service.ShowIssueMetadata(ctx, "APP-1")
	if item.Epic != newEpic || item.Title != "Project title" || *item.MaxEstimateSeconds != estimate {
		t.Fatalf("epic replacement changed other fields: %#v", item)
	}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-1", TitleSet: true, EpicSet: true, MaxEstimateSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, err = service.ShowIssueMetadata(ctx, "APP-1")
	if err != nil || item.Epic != "" || item.Title != "" || item.MaxEstimateSeconds != nil {
		t.Fatalf("clear removed identity: %#v %v", item, err)
	}
}

func TestIssuesSearchRollupUsesLifetimeActiveWorkAndNaturalOrder(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	epic, title, twoHours := "Documents", "Shared document removal", int64(2*3600)
	patches := []IssueMetadataPatch{
		{IssueKey: "APP-10", Epic: &epic, EpicSet: true, MaxEstimateSeconds: &twoHours, MaxEstimateSet: true},
		{IssueKey: "APP-2", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true, MaxEstimateSeconds: &twoHours, MaxEstimateSet: true},
		{IssueKey: "APP-1", Epic: &epic, EpicSet: true},
		{IssueKey: "APPX-1"},
	}
	if err := service.ApplyIssueMetadata(ctx, patches, false); err != nil {
		t.Fatal(err)
	}
	for _, input := range []AddInput{
		{IssueKey: "APP-1", StartedUTC: "2025-01-01T09:00:00Z", Duration: "30m", Description: "Unestimated work"},
		{IssueKey: "APP-2", StartedUTC: "2025-02-01T09:00:00Z", Duration: "1h", Description: "First year"},
		{IssueKey: "APP-2", StartedUTC: "2026-02-01T09:00:00Z", Duration: "2h", Description: "Second year"},
		{IssueKey: "APP-10", StartedUTC: "2026-03-01T09:00:00Z", Duration: "30m", Description: "Within estimate"},
	} {
		if _, err := service.Add(ctx, cfg, input); err != nil {
			t.Fatal(err)
		}
	}
	trashed := mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-10", StartedUTC: "2026-04-01T09:00:00Z", Duration: "4h", Description: "Discarded"})
	if _, err := service.Delete(ctx, trashed.ID, trashed.Revision); err != nil {
		t.Fatal(err)
	}
	items, err := service.SearchIssues(ctx, "APP", "", "")
	if err != nil || len(items) != 3 || items[0].IssueKey != "APP-1" || items[1].IssueKey != "APP-2" || items[2].IssueKey != "APP-10" {
		t.Fatalf("catalog scope/order: %#v %v", items, err)
	}
	if items[0].RemainingSeconds != nil || items[0].LoggedSeconds != 1800 || items[1].LoggedSeconds != 10800 || *items[1].RemainingSeconds != 0 || items[1].OverrunSeconds != 3600 || items[2].LoggedSeconds != 1800 || *items[2].RemainingSeconds != 5400 {
		t.Fatalf("issue totals: %#v", items)
	}
	want := IssueSummary{IssueCount: 3, EstimatedIssueCount: 2, UnestimatedIssueCount: 1, LoggedSeconds: 14400, EstimatedSeconds: 14400, RemainingSeconds: 5400, OverrunSeconds: 3600}
	if got := SummarizeIssues(items); got != want {
		t.Fatalf("epic summary = %#v, want %#v", got, want)
	}
	for _, test := range []struct {
		search string
		epic   string
		count  int
	}{{"APP-10", "", 1}, {"SHARED", "", 1}, {"documents", "", 3}, {"", " Documents ", 3}, {"", "Other", 0}} {
		found, err := service.SearchIssues(ctx, "", test.search, test.epic)
		if err != nil || len(found) != test.count {
			t.Errorf("SearchIssues(%q, %q): %d %v", test.search, test.epic, len(found), err)
		}
	}
	lookup, err := service.LookupIssueMetadata(ctx, []string{"APP-2", "APP-10"})
	if err != nil || lookup["APP-2"].LoggedSeconds != 10800 || *lookup["APP-10"].RemainingSeconds != 5400 {
		t.Fatalf("bounded totals disagree: %#v %v", lookup, err)
	}
}

func TestIssueEpicPatchJSONValidationAndGrouping(t *testing.T) {
	for _, data := range []string{`[{"issue_key":"APP-1","epic":" "}]`, `[{"issue_key":"APP-1","epic":"two\nlines"}]`, `[{"issue_key":"APP-1","epic":"two\u2028lines"}]`, `[{"issue_key":"APP-1","epic":"two\u2029lines"}]`, `[{"issue_key":"APP-1","epic":1}]`} {
		if _, err := ParseIssueMetadataPatches([]byte(data)); !errors.Is(err, ErrValidation) {
			t.Errorf("accepted invalid epic %s: %v", data, err)
		}
	}
	patches, err := ParseIssueMetadataPatches([]byte(`[{"issue_key":"APP-1"},{"issue_key":"APP-2","epic":null},{"issue_key":"APP-3","epic":"Documents"}]`))
	if err != nil || patches[0].EpicSet || !patches[1].EpicSet || patches[1].Epic != nil || *patches[2].Epic != "Documents" {
		t.Fatalf("epic patch presence: %#v %v", patches, err)
	}
	encoded, err := json.Marshal(patches)
	if err != nil || string(encoded) != `[{"issue_key":"APP-1"},{"epic":null,"issue_key":"APP-2"},{"epic":"Documents","issue_key":"APP-3"}]` {
		t.Fatalf("patch JSON: %s %v", encoded, err)
	}
	estimate := int64(3600)
	groups := GroupIssueEpics([]IssueMetadata{
		{IssueKey: "APP-1", Epic: "Zebra", MaxEstimateSeconds: &estimate, LoggedSeconds: 7200},
		{IssueKey: "APP-2", Epic: "Alpha", LoggedSeconds: 900},
		{IssueKey: "APP-3"},
		{IssueKey: "APP-4", Epic: "Zebra", MaxEstimateSeconds: &estimate},
	})
	want := []EpicSummary{
		{Epic: "Alpha", IssueSummary: IssueSummary{IssueCount: 1, UnestimatedIssueCount: 1, LoggedSeconds: 900}},
		{Epic: "Zebra", IssueSummary: IssueSummary{IssueCount: 2, EstimatedIssueCount: 2, LoggedSeconds: 7200, EstimatedSeconds: 7200, RemainingSeconds: 3600, OverrunSeconds: 3600}},
		{Epic: "", IssueSummary: IssueSummary{IssueCount: 1, UnestimatedIssueCount: 1}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
}

func TestIssuesSearchFoldsUnicodeTitlesAndEpics(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	title, epic := "Žalios dokumentų kortelės", "Šiaurės rinka"
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-1", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	for _, search := range []string{"žalios", "DOKUMENTŲ", "šiaurės", "RINKA", "app-1"} {
		items, err := service.SearchIssues(ctx, "APP", search, "")
		if err != nil || len(items) != 1 || items[0].IssueKey != "APP-1" {
			t.Errorf("SearchIssues(%q): %#v %v", search, items, err)
		}
	}
}

func TestIssueCreationIsImmutableAcrossMetadataWorklogAndCacheWrites(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	created := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return created }
	if err := service.ApplyIssueMetadata(t.Context(), []IssueMetadataPatch{{IssueKey: "APP-1"}}, false); err != nil {
		t.Fatal(err)
	}
	updated := created.Add(24 * time.Hour)
	service.now = func() time.Time { return updated }
	title, epic := "Edited title", "October work"
	if err := service.ApplyIssueMetadata(t.Context(), []IssueMetadataPatch{{IssueKey: "APP-1", Title: &title, TitleSet: true, Epic: &epic, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	cfg := config.EffectiveConfig{Location: time.UTC}
	for _, key := range []string{"APP-1", "APP-2"} {
		if _, err := service.Add(t.Context(), cfg, AddInput{IssueKey: key, StartedUTC: updated.Format(time.RFC3339), Duration: "15m", Description: "Time logged"}); err != nil {
			t.Fatal(err)
		}
		updated = updated.Add(time.Hour)
	}
	refreshed := created.Add(48 * time.Hour)
	if err := service.UpsertIssueMetadataBatch(t.Context(), []IssueMetadata{{IssueKey: "APP-1", RefreshedAt: refreshed, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main"}, {IssueKey: "APP-3", RefreshedAt: refreshed, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main"}}); err != nil {
		t.Fatal(err)
	}
	item, err := service.ShowIssueMetadata(t.Context(), "APP-1")
	if err != nil || !item.CreatedAt.Equal(created) || !item.UpdatedAt.Equal(created.Add(24*time.Hour)) {
		t.Fatalf("metadata edit replaced creation: %+v %v", item, err)
	}
	items, err := service.SearchIssues(t.Context(), "APP", "", "")
	if err != nil || len(items) != 3 || !items[0].CreatedAt.Equal(created) || !items[1].CreatedAt.Equal(created.Add(25*time.Hour)) || !items[2].CreatedAt.Equal(refreshed) {
		t.Fatalf("creation paths/query disagree: %+v %v", items, err)
	}
	if err := service.ApplyIssueMetadata(t.Context(), []IssueMetadataPatch{{IssueKey: "APP-1", TitleSet: true, EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, err = service.ShowIssueMetadata(t.Context(), "APP-1")
	if err != nil || !item.CreatedAt.Equal(created) {
		t.Fatal("clearing optional metadata lost creation")
	}
}
