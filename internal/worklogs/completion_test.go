package worklogs

import (
	"fmt"
	"testing"
	"time"
)

func TestCompletionQueriesAreBoundedOrderedAndDeduplicated(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("worklog-%03d", i)
		issueKey := fmt.Sprintf("APP-%03d", i)
		updatedAt := time.Date(2026, 5, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339)
		if _, err := store.DB().Exec(
			`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, '2026-05-01T09:00:00Z', 900, 'completion', ?, ?)`,
			id,
			issueKey,
			updatedAt,
			updatedAt,
		); err != nil {
			t.Fatalf("seed worklog %d: %v", i, err)
		}
	}
	if _, err := store.DB().Exec(`INSERT INTO issue_metadata(issue_key, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at) VALUES('APP-000', 3600, 'jira-cloud', 'product', '2026-05-01T12:00:00Z'), ('META-1', NULL, 'jira-cloud', 'product', '2026-05-01T12:00:00Z')`); err != nil {
		t.Fatalf("seed issue metadata: %v", err)
	}

	worklogs, err := service.ListActiveByIDPrefix("WORKLOG-", 100)
	if err != nil {
		t.Fatalf("list worklog candidates: %v", err)
	}
	if len(worklogs) != 100 {
		t.Fatalf("expected bounded 100 candidates, got %d", len(worklogs))
	}
	if worklogs[0].ID != "worklog-104" || worklogs[99].ID != "worklog-005" {
		t.Fatalf("expected newest-first candidates, first=%s last=%s", worklogs[0].ID, worklogs[99].ID)
	}

	issues, err := service.ListKnownIssueKeys("", 200)
	if err != nil {
		t.Fatalf("list issue candidates: %v", err)
	}
	if len(issues) != 106 {
		t.Fatalf("expected 105 worklog issues plus one metadata-only issue, got %d", len(issues))
	}
	if issues[0] != "APP-000" || issues[len(issues)-1] != "META-1" {
		t.Fatalf("expected deterministic deduplicated issue ordering, first=%s last=%s", issues[0], issues[len(issues)-1])
	}
}
