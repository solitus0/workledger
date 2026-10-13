package worklogs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestLocalIssueMetadataOverridePartialClearAndRefresh(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cached, manual := int64(8*3600), int64(16*3600)
	cache := IssueMetadata{IssueKey: "SBAH-2", MaxEstimateSeconds: &cached, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main", RefreshedAt: time.Now()}
	if err := service.UpsertIssueMetadataBatch(ctx, []IssueMetadata{cache}); err != nil {
		t.Fatal(err)
	}
	title := "Product data lifecycle"
	patch := IssueMetadataPatch{IssueKey: "SBAH-2", Title: &title, TitleSet: true, MaxEstimateSeconds: &manual, MaxEstimateSet: true}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{patch}, true); err != nil {
		t.Fatal(err)
	}
	item, err := service.ShowIssueMetadata(ctx, "SBAH-2")
	if err != nil || item.Title != "" || *item.MaxEstimateSeconds != cached {
		t.Fatalf("dry run changed metadata: %#v %v", item, err)
	}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{patch}, false); err != nil {
		t.Fatal(err)
	}
	cached = 4 * 3600
	if err := service.UpsertIssueMetadataBatch(ctx, []IssueMetadata{cache}); err != nil {
		t.Fatal(err)
	}
	item, err = service.ShowIssueMetadata(ctx, "SBAH-2")
	if err != nil || item.Title != title || *item.MaxEstimateSeconds != manual || *item.CachedMaxEstimateSeconds != cached || item.EstimateSource != "manual" {
		t.Fatalf("override lost on refresh: %#v %v", item, err)
	}
	newTitle := "Updated title"
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "SBAH-2", Title: &newTitle, TitleSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, _ = service.ShowIssueMetadata(ctx, "SBAH-2")
	if item.Title != newTitle || *item.MaxEstimateSeconds != manual {
		t.Fatalf("partial update: %#v", item)
	}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "SBAH-2", MaxEstimateSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, _ = service.ShowIssueMetadata(ctx, "SBAH-2")
	if item.Title != newTitle || item.ManualMaxEstimateSeconds != nil || *item.MaxEstimateSeconds != cached || item.EstimateSource != "jira" {
		t.Fatalf("clear did not reveal Jira: %#v", item)
	}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "SBAH-2", TitleSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	item, _ = service.ShowIssueMetadata(ctx, "SBAH-2")
	if item.Title != "" || *item.MaxEstimateSeconds != cached {
		t.Fatalf("clear title lost cache: %#v", item)
	}
}

func TestMetadataBatchValidationAndTransactionalRollback(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	invalid := []string{
		`null`, `[]`, `{}`, `[null]`, `[{"issue_key":"app-1","title":"Title"}]`,
		`[{"issue_key":"APP-1","title":""}]`, `[{"issue_key":"APP-1","title":"line\nbreak"}]`, `[{"issue_key":"APP-1","title":"line\u2028break"}]`,
		`[{"issue_key":"APP-1","max_estimate_seconds":0}]`, `[{"issue_key":"APP-1","max_estimate_seconds":1.5}]`,
		`[{"issue_key":"APP-1","max_estimate_seconds":-1}]`, `[{"issue_key":"APP-1","estimate":1}]`,
		`[{"issue_key":"APP-1","title":"First"},{"issue_key":"APP-1","title":"Second"}]`,
		`[{"issue_key":"APP-1","title":"Title"}] []`,
	}
	for _, input := range invalid {
		if _, err := ParseIssueMetadataPatches([]byte(input)); !errors.Is(err, ErrValidation) {
			t.Errorf("accepted %s: %v", input, err)
		}
	}
	patches, err := ParseIssueMetadataPatches([]byte(`[{"issue_key":"APP-1","title":"Title","max_estimate_seconds":1},{"issue_key":"APP-2","title":null}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !patches[1].TitleSet || patches[1].Title != nil || patches[1].MaxEstimateSet {
		t.Fatalf("null/omitted lost: %#v", patches[1])
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_second BEFORE INSERT ON issues WHEN NEW.issue_key = 'APP-2' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyIssueMetadata(ctx, patches, false); err == nil {
		t.Fatal("expected second-row failure")
	}
	items, err := service.SearchIssues(ctx, "", "", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("partial batch persisted: %#v %v", items, err)
	}
	for _, value := range []string{"0s", "-1h", "1.5s", "16", "8–16h"} {
		if _, err := ParseMaxEstimate(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for input, want := range map[string]int64{"1s": 1, "16h": 57600, "1h30m": 5400} {
		if got, err := ParseMaxEstimate(input); err != nil || got != want {
			t.Errorf("%s = %d %v", input, got, err)
		}
	}
}

func TestDecoratedIssueDiscoveryDeduplicationPagingAndTitles(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	patches := []IssueMetadataPatch{}
	for i := 1; i <= 62; i++ {
		title := fmt.Sprintf("Document work %d", i)
		patches = append(patches, IssueMetadataPatch{IssueKey: fmt.Sprintf("APP-%d", i), Title: &title, TitleSet: true})
	}
	if err := service.ApplyIssueMetadata(ctx, patches, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO issue_metadata VALUES('APP-1',3600,'jira-cloud','main','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('used','APP-2','2026-01-01T00:00:00Z',900,'needle','2026-01-01T00:00:00Z','2026-02-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	items, more, err := service.ListUsedIssues(ctx, IssueFindByTitle, "DOCUMENT", 50, 0)
	if err != nil || !more || len(items) != 50 || items[0].IssueKey != "APP-2" || items[0].LastEnteredAt.IsZero() {
		t.Fatalf("first page: %#v %v %v", items, more, err)
	}
	for _, item := range items[1:] {
		if !item.LastEnteredAt.IsZero() {
			t.Fatalf("unused issue has entry date: %#v", item)
		}
	}
	tail, more, err := service.ListUsedIssues(ctx, IssueFindByTitle, "document", 50, 50)
	if err != nil || more || len(tail) != 12 {
		t.Fatalf("tail page: %#v %v %v", tail, more, err)
	}
	keys, err := service.ListKnownIssueKeys(ctx, "app-", 100)
	if err != nil || len(keys) != 62 || keys[0] != "APP-2" {
		t.Fatalf("completion: %#v %v", keys, err)
	}
	catalog, err := service.SearchIssues(ctx, "APP", "DOCUMENT", "")
	if err != nil || len(catalog) != 62 {
		t.Fatalf("catalog: %d %v", len(catalog), err)
	}
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-3", TitleSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	if item, err := service.ShowIssueMetadata(ctx, "APP-3"); err != nil || item.Title != "" {
		t.Fatalf("cleared issue identity lost: %#v %v", item, err)
	}
}
