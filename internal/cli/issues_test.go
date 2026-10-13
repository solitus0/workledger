package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssuesCLISetApplyListAndDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	seedIssueMetadata(t, "SBAH-2", int64Ptr(8*3600))
	for _, args := range [][]string{
		{"issues", "set", "--issue", "SBAH-2", "--title", "Product data lifecycle", "--max-estimate", "16h", "--output", "json"},
		{"issues", "set", "--issue", "SBAH-3", "--title", "Market-specific product images", "--max-estimate", "1s"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatal(result)
		}
	}
	result := runCLI(t, "issues", "list", "--search", "PRODUCT", "--issue-prefix", "SBAH", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("unused issues missing: %s", result.stdout)
	}
	first := items[0].(map[string]any)
	if first["max_estimate_seconds"] != float64(57600) || first["cached_max_estimate_seconds"] != float64(28800) || first["estimate_source"] != "manual" {
		t.Fatalf("override fields: %#v", first)
	}
	second := items[1].(map[string]any)
	if second["refreshed_at"] != nil || second["source_adapter_family"] != nil {
		t.Fatalf("local decoration has fake Jira provenance: %#v", second)
	}
	completion := runCLI(t, "__complete", "worklogs", "add", "--issue", "SBAH-")
	if completion.code != 0 || !strings.Contains(completion.stdout, "SBAH-2") || !strings.Contains(completion.stdout, "SBAH-3") {
		t.Fatalf("completion: %#v", completion)
	}
	result = runCLI(t, "issues", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(0) {
		t.Fatalf("date scope admitted unused metadata: %#v", result)
	}
	if result = runCLI(t, "issues", "set", "--issue", "SBAH-2", "--clear-max-estimate", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list", "--issue", "SBAH-2", "--output", "json")
	first = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)[0].(map[string]any)
	if first["title"] != "Product data lifecycle" || first["max_estimate_seconds"] != float64(28800) || first["estimate_source"] != "jira" {
		t.Fatalf("clear: %#v", first)
	}
	input := `[{"issue_key":"SBAH-2","title":null},{"issue_key":"SBAH-4","title":"Document assignment","max_estimate_seconds":14400}]`
	result = runCLIWithInput(t, input, "issues", "apply", "--stdin", "--dry", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["dry_run"] != true {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list", "--issue", "SBAH-4", "--output", "json")
	if result.code != 3 {
		t.Fatalf("dry run created metadata: %#v", result)
	}
	file := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if result = runCLI(t, "issues", "apply", "--file", file, "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(3) {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list")
	if result.code != 0 || !strings.Contains(result.stdout, "Document assignment") || !strings.Contains(result.stdout, "ESTIMATE_SOURCE") {
		t.Fatal(result)
	}
}

func TestIssuesCLIValidationDoesNotWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	for _, args := range [][]string{
		{"set", "--issue", "sbah-1", "--title", "Title"},
		{"set", "--issue", "SBAH-1", "--title", "Title", "--clear-title"},
		{"set", "--issue", "SBAH-1", "--epic", "Documents", "--clear-epic"},
		{"set", "--issue", "SBAH-1", "--epic", "  "},
		{"set", "--issue", "SBAH-1", "--epic", "Two\nlines"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "1h", "--clear-max-estimate"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "0s"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "1.5s"},
		{"apply"}, {"apply", "--file", "ignored", "--stdin"},
		{"list", "--issue-prefix", "SBAH-"}, {"list", "--week-offset", "1"},
	} {
		result := runCLI(t, append(append([]string{"issues"}, args...), "--output", "json")...)
		if result.code != 2 {
			t.Errorf("%v: %#v", args, result)
		}
	}
	for _, input := range []string{
		`[{"issue_key":"SBAH-1","title":"First"},{"issue_key":"SBAH-1","title":"Second"}]`,
		`[{"issue_key":"SBAH-1","title":"First"},{"issue_key":"SBAH-2","max_estimate_seconds":0}]`,
	} {
		result := runCLIWithInput(t, input, "issues", "apply", "--stdin", "--output", "json")
		if result.code != 2 {
			t.Errorf("invalid batch: %#v", result)
		}
	}
	result := runCLI(t, "issues", "list", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(0) {
		t.Fatalf("invalid metadata persisted: %#v", result)
	}
}

func TestIssuesCLILifetimeBudgetsAndEpicRollups(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	for _, args := range [][]string{
		{"issues", "set", "--issue", "APP-1", "--title", "Lifecycle", "--max-estimate", "2h", "--epic", "  Documents  "},
		{"issues", "set", "--issue", "APP-2", "--max-estimate", "2h", "--epic", "Documents"},
		{"issues", "set", "--issue", "APP-3", "--epic", "Documents"},
		{"issues", "set", "--issue", "APP-4"},
		{"issues", "set", "--issue", "APP-10", "--max-estimate", "1s", "--epic", "Performance"},
		{"worklogs", "add", "--issue", "APP-1", "--started", "2026-05-04T09:00", "--duration", "1h", "--description", "Earlier work"},
		{"worklogs", "add", "--issue", "APP-1", "--started", "2026-05-05T09:00", "--duration", "2h", "--description", "Later work"},
		{"worklogs", "add", "--issue", "APP-2", "--started", "2026-05-05T11:00", "--duration", "30m", "--description", "Assignment"},
		{"worklogs", "add", "--issue", "APP-3", "--started", "2026-05-05T12:00", "--duration", "1h", "--description", "Unestimated work"},
		{"worklogs", "add", "--issue", "APP-10", "--started", "2026-05-05T13:00", "--duration", "30m", "--description", "Performance work"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatalf("%v: %#v", args, result)
		}
	}
	result := runCLI(t, "issues", "list", "--issue-prefix", "APP", "--epic", " Documents ", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["total"] != float64(3) || payload["filters"].(map[string]any)["epic"] != "Documents" {
		t.Fatalf("exact group scope: %s", result.stdout)
	}
	items := payload["items"].([]any)
	first, second, third := items[0].(map[string]any), items[1].(map[string]any), items[2].(map[string]any)
	if first["issue_key"] != "APP-1" || first["logged_seconds"] != float64(10800) || first["remaining_seconds"] != float64(0) || first["overrun_seconds"] != float64(3600) {
		t.Fatalf("overrun row: %#v", first)
	}
	if second["remaining_seconds"] != float64(5400) || third["remaining_seconds"] != nil || third["max_estimate_seconds"] != nil {
		t.Fatalf("known and unknown remainder: %#v %#v", second, third)
	}
	assertIssueCLISummary(t, payload["summary"].(map[string]any), map[string]float64{
		"issue_count": 3, "estimated_issue_count": 2, "unestimated_issue_count": 1,
		"logged_seconds": 16200, "estimated_seconds": 14400, "remaining_seconds": 5400, "overrun_seconds": 3600,
	})
	result = runCLI(t, "issues", "list", "--from", "2026-05-04", "--to", "2026-05-04", "--epic", "Documents", "--search", "APP-1", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload = decodeJSONMap(t, []byte(result.stdout))
	if payload["total"] != float64(1) || payload["items"].([]any)[0].(map[string]any)["logged_seconds"] != float64(10800) {
		t.Fatalf("date selection truncated lifetime budget: %s", result.stdout)
	}
	result = runCLI(t, "issues", "epics", "--issue-prefix", "APP", "--search", "DOCUMENT", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload = decodeJSONMap(t, []byte(result.stdout))
	groups := payload["items"].([]any)
	if payload["total"] != float64(1) || groups[0].(map[string]any)["epic"] != "Documents" {
		t.Fatalf("group label search: %s", result.stdout)
	}
	assertIssueCLISummary(t, groups[0].(map[string]any), map[string]float64{"issue_count": 3, "remaining_seconds": 5400, "overrun_seconds": 3600, "unestimated_issue_count": 1})
	result = runCLI(t, "issues", "epics", "--issue-prefix", "APP")
	for _, want := range []string{"Documents", "Performance", "Ungrouped", "1h 30m known", "1s", "unknown", "UNESTIMATED"} {
		if result.code != 0 || !strings.Contains(result.stdout, want) {
			t.Fatalf("missing %q in group table: %#v", want, result)
		}
	}
	result = runCLI(t, "issues", "list", "--issue-prefix", "APP", "--output", "json")
	items = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)
	for i, key := range []string{"APP-1", "APP-2", "APP-3", "APP-4", "APP-10"} {
		if items[i].(map[string]any)["issue_key"] != key {
			t.Fatalf("natural issue order: %s", result.stdout)
		}
	}
	result = runCLI(t, "issues", "list", "--from", "2026-05-05", "--to", "2026-05-05", "--output", "json")
	items = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)
	for i, key := range []string{"APP-1", "APP-2", "APP-3", "APP-10"} {
		if items[i].(map[string]any)["issue_key"] != key {
			t.Fatalf("date-selected natural issue order: %s", result.stdout)
		}
	}
	completion := runCLI(t, "__complete", "issues", "set", "--epic", "Doc")
	if completion.code != 0 || !strings.Contains(completion.stdout, "Documents") {
		t.Fatalf("epic completion: %#v", completion)
	}
}

func TestIssuesCLIAutoCreationClearingAndRemovedCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	args := []string{"worklogs", "add", "--issue", "NEW-1", "--started", "2026-05-04T09:00", "--duration", "1h", "--description", "First issue work", "--output", "json"}
	if result := runCLI(t, append(args, "--dry")...); result.code != 0 {
		t.Fatal(result)
	}
	if result := runCLI(t, "issues", "list", "--issue", "NEW-1", "--output", "json"); result.code != 3 {
		t.Fatalf("dry worklog created issue: %#v", result)
	}
	result := runCLI(t, args...)
	if result.code != 0 {
		t.Fatal(result)
	}
	id := decodeJSONMap(t, []byte(result.stdout))["id"].(string)
	result = runCLI(t, "issues", "list", "--issue", "NEW-1", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	item := decodeJSONMap(t, []byte(result.stdout))["items"].([]any)[0].(map[string]any)
	if item["title"] != nil || item["epic"] != nil || item["remaining_seconds"] != nil || item["logged_seconds"] != float64(3600) {
		t.Fatalf("bare issue: %#v", item)
	}
	input := `[{"issue_key":"NEW-1","title":"First issue","max_estimate_seconds":7200,"epic":" New work "},{"issue_key":"NEW-2"}]`
	if result = runCLIWithInput(t, input, "issues", "apply", "--stdin", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLIWithInput(t, `[{"issue_key":"NEW-1","epic":null}]`, "issues", "apply", "--stdin", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list", "--issue", "NEW-1", "--output", "json")
	item = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)[0].(map[string]any)
	if item["epic"] != nil || item["title"] != "First issue" || item["max_estimate_seconds"] != float64(7200) {
		t.Fatalf("epic null changed other metadata: %#v", item)
	}
	if result = runCLI(t, "issues", "set", "--issue", "NEW-1", "--clear-title", "--clear-max-estimate", "--clear-epic"); result.code != 0 {
		t.Fatal(result)
	}
	if result = runCLI(t, "worklogs", "delete", id, "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issues", "list", "--issue", "NEW-1", "--output", "json")
	item = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)[0].(map[string]any)
	if item["logged_seconds"] != float64(0) || item["remaining_seconds"] != nil {
		t.Fatalf("trash counted or issue identity removed: %#v", item)
	}
	if result = runCLI(t, "issue-metadata", "list"); result.code == 0 || !strings.Contains(result.stderr, "unknown command") {
		t.Fatalf("removed command still supported: %#v", result)
	}
	result = runCLI(t, "issues", "list", "--issue-prefix", "EMPTY")
	if result.code != 0 || !strings.Contains(result.stdout, "0 issues") || !strings.Contains(result.stdout, "no estimates") {
		t.Fatalf("empty scope footer: %#v", result)
	}
}

func assertIssueCLISummary(t *testing.T, summary map[string]any, expected map[string]float64) {
	t.Helper()
	for name, value := range expected {
		if summary[name] != value {
			t.Errorf("summary %s = %#v, want %v; %#v", name, summary[name], value, summary)
		}
	}
}
