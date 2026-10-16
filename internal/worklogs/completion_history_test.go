package worklogs

import (
	"context"
	"fmt"
	"testing"
)

func TestUsedIssueSearchExcludesTrashAndRanksByEntry(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	active := []struct{ id, key, created, updated, description string }{
		{"older", "APP-1", "2026-05-01T09:00:00Z", "2026-09-01T09:00:00Z", "needle in older worklog"},
		{"newer", "APP-1", "2026-05-03T09:00:00Z", "2026-05-03T09:00:00Z", "latest APP-1 entry"},
		{"pulled", "APP-2", "2026-05-04T09:00:00Z", "2026-05-04T09:00:00Z", "pulled into ledger"},
		{"percent", "APP-4", "2026-05-02T09:00:00Z", "2026-05-02T09:00:00Z", "literal % match"},
	}
	for _, row := range active {
		if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, '2026-04-01T09:00:00Z', 900, ?, ?, ?)`, row.id, row.key, row.description, row.created, row.updated); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, scope, source, key, description string }{
		{"local", "local", "former", "APP-3", "needle only in trash"},
		{"local-same-key", "local", "former-app-2", "APP-2", "trash-only phrase"},
		{"remote", "remote", "former", "REMOTE-1", "needle remote"},
		{"audit", "local", "", "AUDIT-1", "needle audit"},
	} {
		var source any
		if row.source != "" {
			source = row.source
		}
		if _, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, source_created_at, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES(?, ?, ?, '2026-05-06T09:00:00Z', ?, '2026-04-01T09:00:00Z', 900, ?, '2026-05-08T09:00:00Z', 'test', '', 'local')`, row.id, row.scope, source, row.key, row.description); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB().Exec(`INSERT INTO issue_metadata(issue_key, source_adapter_family, source_adapter_instance, refreshed_at) VALUES('META-1', 'jira-cloud', 'product', '2026-05-09T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	items, more, err := service.ListUsedIssues(ctx, IssueFindByPrefix, "", 20, 0)
	if err != nil || more || len(items) != 4 {
		t.Fatalf("browse = %#v, more=%t, err=%v", items, more, err)
	}
	for index, want := range []string{"APP-2", "APP-1", "APP-4", "META-1"} {
		if items[index].IssueKey != want {
			t.Fatalf("item %d = %q, want %q", index, items[index].IssueKey, want)
		}
	}
	if got := items[1].LastEnteredAt.Format("2006-01-02"); got != "2026-05-03" {
		t.Fatalf("edited row changed rank: %s", got)
	}
	prefix, _, err := service.ListUsedIssues(ctx, IssueFindByPrefix, "app-1", 10, 0)
	if err != nil || len(prefix) != 1 || prefix[0].IssueKey != "APP-1" {
		t.Fatalf("prefix = %#v, err=%v", prefix, err)
	}
	byDescription, _, err := service.ListUsedIssues(ctx, IssueFindByDescription, "NEEDLE", 10, 0)
	if err != nil || len(byDescription) != 1 || byDescription[0].IssueKey != "APP-1" {
		t.Fatalf("description = %#v, err=%v", byDescription, err)
	}
	byDescription, _, err = service.ListUsedIssues(ctx, IssueFindByDescription, "trash-only phrase", 10, 0)
	if err != nil || len(byDescription) != 0 {
		t.Fatalf("trash description matched active issue: %#v, err=%v", byDescription, err)
	}
	literal, _, err := service.ListUsedIssues(ctx, IssueFindByDescription, "%", 10, 0)
	if err != nil || len(literal) != 1 || literal[0].IssueKey != "APP-4" {
		t.Fatalf("literal percent = %#v, err=%v", literal, err)
	}
	details, more, err := service.ListIssueWorklogDescriptions(ctx, "APP-1", 1, 0)
	if err != nil || !more || len(details) != 1 || details[0].Description != "latest APP-1 entry" {
		t.Fatalf("details first page = %#v, more=%t, err=%v", details, more, err)
	}
	details, more, err = service.ListIssueWorklogDescriptions(ctx, "APP-1", 1, 1)
	if err != nil || more || len(details) != 1 || details[0].Description != "needle in older worklog" {
		t.Fatalf("details second page = %#v, more=%t, err=%v", details, more, err)
	}
	details, more, err = service.ListIssueWorklogDescriptions(ctx, "APP-2", 10, 0)
	if err != nil || more || len(details) != 1 || details[0].Description != "pulled into ledger" {
		t.Fatalf("trash leaked into active issue details: %#v, more=%t, err=%v", details, more, err)
	}
	for _, key := range []string{"APP-3", "REMOTE-1", "AUDIT-1", "META-1"} {
		details, _, err := service.ListIssueWorklogDescriptions(ctx, key, 10, 0)
		if err != nil || len(details) != 0 {
			t.Fatalf("excluded details %s = %#v, err=%v", key, details, err)
		}
	}
}

func TestUsedIssueSearchPagesBeyondHundred(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	for i := 0; i < 125; i++ {
		key := fmt.Sprintf("APP-%03d", i)
		created := fmt.Sprintf("2026-05-01T00:%02d:%02dZ", i/60, i%60)
		if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, '2026-04-01T09:00:00Z', 900, 'same description', ?, ?)`, key, key, created, created); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []IssueFindMode{IssueFindByPrefix, IssueFindByDescription} {
		query := "app-"
		if mode == IssueFindByDescription {
			query = "same"
		}
		for page := 0; page < 3; page++ {
			items, more, err := service.ListUsedIssues(context.Background(), mode, query, 50, page*50)
			wantLen := 50
			if page == 2 {
				wantLen = 25
			}
			if err != nil || len(items) != wantLen || more != (page < 2) {
				t.Fatalf("%s page %d: len=%d more=%t err=%v", mode, page, len(items), more, err)
			}
			if items[0].IssueKey != fmt.Sprintf("APP-%03d", 124-page*50) {
				t.Fatalf("%s page %d starts with %q", mode, page, items[0].IssueKey)
			}
		}
	}
}

func TestIssueDescriptionsKeepRepeatedWorklogs(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	for index, id := range []string{"one", "two"} {
		if _, err := store.DB().Exec(`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, 'APP-1', '2026-04-01T09:00:00Z', ?, 'same text', '2026-05-01T09:00:00Z', '2026-05-01T09:00:00Z')`, id, []int{900, 5400}[index]); err != nil {
			t.Fatal(err)
		}
	}
	items, more, err := service.ListIssueWorklogDescriptions(context.Background(), "APP-1", 10, 0)
	if err != nil || more || len(items) != 2 || items[0].Description != "same text" || items[1].Description != "same text" {
		t.Fatalf("repeated descriptions = %#v, more=%t, err=%v", items, more, err)
	}
	for offset, wantDuration := range []int{900, 5400} {
		if items[offset].DurationSeconds != wantDuration {
			t.Fatalf("entry %d duration = %d, want %d", offset, items[offset].DurationSeconds, wantDuration)
		}
		page, hasMore, err := service.ListIssueWorklogDescriptions(context.Background(), "APP-1", 1, offset)
		if err != nil || hasMore != (offset == 0) || len(page) != 1 || page[0].DurationSeconds != wantDuration || page[0].Description != "same text" {
			t.Fatalf("page %d = %#v, more=%t, err=%v", offset, page, hasMore, err)
		}
	}
}

func TestUsedIssueUnicodeSearchPreservesPagingAndLiteralSubstrings(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("APP-%d", i)
		title := "ŽURNALAS %_\\"
		if i == 3 {
			title = "Žurnalas without punctuation"
		}
		if err := service.ApplyIssueMetadata(t.Context(), []IssueMetadataPatch{{IssueKey: key, Title: &title, TitleSet: true}}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().Exec(`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES(?,?,'2026-05-01T09:00:00Z',900,?,'2026-05-01T00:00:00Z','2026-05-01T00:00:00Z')`, key, key, title); err != nil {
			t.Fatal(err)
		}
	}
	// An unused issue can match by title, but never by worklog description.
	cached := "ŽURNALAS %_\\"
	if err := service.UpsertIssueMetadataBatch(t.Context(), []IssueMetadata{{IssueKey: "APP-4", Title: cached, RefreshedAt: service.now().UTC(), SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main"}}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []IssueFindMode{IssueFindByTitle, IssueFindByDescription} {
		t.Run(string(mode), func(t *testing.T) {
			for _, query := range []string{"žurnalas", "ŽURNALAS"} {
				items, more, err := service.ListUsedIssues(t.Context(), mode, query, 1, 1)
				if err != nil || len(items) != 1 || items[0].IssueKey != "APP-2" || !more {
					t.Fatalf("second page for %q = %#v, more=%t, err=%v", query, items, more, err)
				}
			}
			items, more, err := service.ListUsedIssues(t.Context(), mode, "žurnalas %_\\", 10, 0)
			want := 2
			if mode == IssueFindByTitle {
				want = 3
			}
			if err != nil || len(items) != want || more {
				t.Fatalf("literal substring = %#v, more=%t, err=%v", items, more, err)
			}
		})
	}
}
