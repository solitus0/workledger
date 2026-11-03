package cli

import (
	"strings"
	"testing"
)

func TestUnassignedCLIWorkflowAndCreationPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init"); result.code != 0 {
		t.Fatal(result)
	}
	for _, args := range [][]string{{"worklogs", "list", "--help"}, {"issues", "list", "--help"}} {
		result := runCLI(t, args...)
		if result.code != 0 {
			t.Fatal(result)
		}
		if args[0] == "worklogs" && !strings.Contains(result.stdout, "--unassigned") {
			t.Fatal("skill capability probe unavailable")
		}
	}
	first := runCLI(t, "worklogs", "add", "--started", "2026-05-01T09:00", "--duration", "15m", "--description", "Investigate missing ticket", "--output", "json")
	if first.code != 0 {
		t.Fatal(first)
	}
	firstRecord := decodeJSONMap(t, []byte(first.stdout))
	id := firstRecord["id"].(string)
	if firstRecord["issue_key"] != "" {
		t.Fatal(first.stdout)
	}
	for _, args := range [][]string{
		{"worklogs", "add", "--fit", "--from", "2026-05-02", "--to", "2026-05-02", "--duration", "30m", "--description", "Fit without key"},
		{"worklogs", "add", "--fill", "--from", "2026-05-03", "--to", "2026-05-03", "--duration", "1h", "--description", "Fill without key"},
		{"presets", "add", "pending", "--start", "09:00", "--duration", "15m", "--description", "Pending preset"},
		{"presets", "apply", "pending", "--date", "2026-05-04"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatal(result)
		}
	}
	batch := runCLIWithInput(t, `{"adds":[{"started_at_utc":"2026-05-05T09:00:00Z","duration_seconds":900,"description":"Batch without key"}]}`, "worklogs", "apply", "--stdin", "--output", "json")
	if batch.code != 0 {
		t.Fatal(batch)
	}
	listed := runCLI(t, "worklogs", "list", "--unassigned", "--output", "json")
	if listed.code != 0 {
		t.Fatal(listed)
	}
	payload := decodeJSONMap(t, []byte(listed.stdout))
	items := payload["items"].([]any)
	if len(items) != 5 || items[0].(map[string]any)["id"] != id {
		t.Fatal(listed.stdout)
	}
	table := runCLI(t, "worklogs", "list", "--unassigned")
	if table.code != 0 || !strings.Contains(table.stdout, "Unassigned") {
		t.Fatal(table)
	}
	preview := runCLI(t, "worklogs", "assign", id, "--issue", "APP-1", "--dry", "--output", "json")
	if preview.code != 0 || decodeJSONMap(t, []byte(preview.stdout))["dry_run"] != true {
		t.Fatal(preview)
	}
	if result := runCLI(t, "worklogs", "assign", id, "missing", "--issue", "APP-1"); result.code == 0 {
		t.Fatal("accepted missing selected ID")
	}
	secondID := items[1].(map[string]any)["id"].(string)
	assigned := runCLI(t, "worklogs", "assign", id, secondID, "--issue", "APP-1", "--output", "json")
	if assigned.code != 0 || decodeJSONMap(t, []byte(assigned.stdout))["total"] != float64(2) {
		t.Fatal(assigned)
	}
	cleared := runCLI(t, "worklogs", "update", id, "--issue", "", "--output", "json")
	if cleared.code != 0 || decodeJSONMap(t, []byte(cleared.stdout))["issue_key"] != "" {
		t.Fatal(cleared)
	}
	for _, args := range [][]string{{"worklogs", "add", "--issue", "bad", "--started", "2026-05-06T09:00", "--duration", "15m", "--description", "Invalid"}, {"worklogs", "list", "--unassigned", "--issue", "APP-1"}} {
		if result := runCLI(t, args...); result.code != 2 {
			t.Fatal(result)
		}
	}
	catalog := runCLI(t, "issues", "list", "--output", "json")
	if catalog.code != 0 || decodeJSONMap(t, []byte(catalog.stdout))["total"] != float64(1) {
		t.Fatal(catalog)
	}
	context := runCLI(t, "worklogs", "context", "--from", "2026-05-01", "--to", "2026-05-01", "--output", "json")
	if context.code != 0 {
		t.Fatal(context)
	}
	if got := decodeJSONMap(t, []byte(context.stdout))["summary"].(map[string]any)["booked_seconds"]; got != float64(900) {
		t.Fatalf("unassigned accounting %v: %s", got, context.stdout)
	}
}
