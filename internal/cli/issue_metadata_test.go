package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueMetadataCLISetApplyListAndDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	seedIssueMetadata(t, "SBAH-2", int64Ptr(8*3600))
	for _, args := range [][]string{
		{"issue-metadata", "set", "--issue", "SBAH-2", "--title", "Product data lifecycle", "--max-estimate", "16h", "--output", "json"},
		{"issue-metadata", "set", "--issue", "SBAH-3", "--title", "Market-specific product images", "--max-estimate", "1s"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatal(result)
		}
	}
	result := runCLI(t, "issue-metadata", "list", "--search", "PRODUCT", "--issue-prefix", "SBAH", "--output", "json")
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
	result = runCLI(t, "issue-metadata", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(0) {
		t.Fatalf("date scope admitted unused metadata: %#v", result)
	}
	if result = runCLI(t, "issue-metadata", "set", "--issue", "SBAH-2", "--clear-max-estimate", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issue-metadata", "list", "--issue", "SBAH-2", "--output", "json")
	first = decodeJSONMap(t, []byte(result.stdout))["items"].([]any)[0].(map[string]any)
	if first["title"] != "Product data lifecycle" || first["max_estimate_seconds"] != float64(28800) || first["estimate_source"] != "jira" {
		t.Fatalf("clear: %#v", first)
	}
	input := `[{"issue_key":"SBAH-2","title":null},{"issue_key":"SBAH-4","title":"Document assignment","max_estimate_seconds":14400}]`
	result = runCLIWithInput(t, input, "issue-metadata", "apply", "--stdin", "--dry", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["dry_run"] != true {
		t.Fatal(result)
	}
	result = runCLI(t, "issue-metadata", "list", "--issue", "SBAH-4", "--output", "json")
	if result.code != 3 {
		t.Fatalf("dry run created metadata: %#v", result)
	}
	file := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if result = runCLI(t, "issue-metadata", "apply", "--file", file, "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	result = runCLI(t, "issue-metadata", "list", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(3) {
		t.Fatal(result)
	}
	result = runCLI(t, "issue-metadata", "list")
	if result.code != 0 || !strings.Contains(result.stdout, "Document assignment") || !strings.Contains(result.stdout, "ESTIMATE_SOURCE") {
		t.Fatal(result)
	}
}

func TestIssueMetadataCLIValidationDoesNotWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatal(result)
	}
	for _, args := range [][]string{
		{"set", "--issue", "SBAH-1"},
		{"set", "--issue", "sbah-1", "--title", "Title"},
		{"set", "--issue", "SBAH-1", "--title", "Title", "--clear-title"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "1h", "--clear-max-estimate"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "0s"},
		{"set", "--issue", "SBAH-1", "--max-estimate", "1.5s"},
		{"apply"}, {"apply", "--file", "ignored", "--stdin"},
		{"list", "--issue-prefix", "SBAH-"}, {"list", "--week-offset", "1"},
	} {
		result := runCLI(t, append(append([]string{"issue-metadata"}, args...), "--output", "json")...)
		if result.code != 2 {
			t.Errorf("%v: %#v", args, result)
		}
	}
	for _, input := range []string{
		`[{"issue_key":"SBAH-1","title":"First"},{"issue_key":"SBAH-1","title":"Second"}]`,
		`[{"issue_key":"SBAH-1","title":"First"},{"issue_key":"SBAH-2","max_estimate_seconds":0}]`,
	} {
		result := runCLIWithInput(t, input, "issue-metadata", "apply", "--stdin", "--output", "json")
		if result.code != 2 {
			t.Errorf("invalid batch: %#v", result)
		}
	}
	result := runCLI(t, "issue-metadata", "list", "--output", "json")
	if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != float64(0) {
		t.Fatalf("invalid metadata persisted: %#v", result)
	}
}
