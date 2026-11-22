package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func metadataCLIJSON(t *testing.T, result cliResult) map[string]any {
	t.Helper()
	if result.code != 0 || result.stderr != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	return decodeJSONMap(t, []byte(result.stdout))
}

func TestWorklogMetadataCLIEditing(t *testing.T) {
	initWorklogCLIFixture(t)
	base := []string{"worklogs", "add", "--started-utc", "2026-05-14T09:00:00Z", "--duration", "20m", "--description", "Review changes", "--metadata-file", "-", "--output", "json"}
	preview := metadataCLIJSON(t, runCLIWithInput(t, "null", append(base, "--dry")...))
	value, set := preview["record"].(map[string]any)["metadata"]
	if !set || value != nil || preview["dry_run"] != true || countActiveCLIWorklogs(t) != 0 {
		t.Fatal(preview)
	}
	added := metadataCLIJSON(t, runCLIWithInput(t, `{"private":true}`, base...))
	id := added["id"].(string)
	if added["metadata"].(map[string]any)["private"] != true {
		t.Fatal(added)
	}
	preview = metadataCLIJSON(t, runCLI(t, "worklogs", "update", id, "--clear-metadata", "--dry", "--output", "json"))
	_, set = preview["after"].(map[string]any)["metadata"]
	if set || preview["before"].(map[string]any)["metadata"].(map[string]any)["private"] != true {
		t.Fatal(preview)
	}
	found := metadataCLIJSON(t, runCLI(t, "worklogs", "search", "Review changes", "--fields", "id,metadata", "--output", "json"))
	if found["items"].([]any)[0].(map[string]any)["metadata"].(map[string]any)["private"] != true {
		t.Fatal(found)
	}
	updated := metadataCLIJSON(t, runCLI(t, "worklogs", "update", id, "--metadata", "null", "--output", "json"))
	if value, set := updated["metadata"]; !set || value != nil {
		t.Fatal(updated)
	}
	defaultList := metadataCLIJSON(t, runCLI(t, "worklogs", "list", "--unassigned", "--output", "json"))
	if _, set := defaultList["items"].([]any)[0].(map[string]any)["metadata"]; set {
		t.Fatal(defaultList)
	}
	match := []string{"worklogs", "list", "--metadata-path=", "--metadata-value", "null", "--fields", "id,metadata", "--output", "json"}
	found = metadataCLIJSON(t, runCLI(t, match...))
	if found["total"] != float64(1) {
		t.Fatal(found)
	}
	cleared := metadataCLIJSON(t, runCLI(t, "worklogs", "update", id, "--clear-metadata", "--output", "json"))
	if _, set := cleared["metadata"]; set {
		t.Fatal(cleared)
	}
	found = metadataCLIJSON(t, runCLI(t, match...))
	if found["total"] != float64(0) || len(found["items"].([]any)) != 0 {
		t.Fatal(found)
	}
}

func TestWorklogMetadataCLIApplyAndLookup(t *testing.T) {
	initWorklogCLIFixture(t)
	payload := `{"adds":[
		{"issue_key":"APP-1","started_at_utc":"2026-05-14T09:00:00Z","duration_seconds":1200,"description":"Review changes","metadata":{"codex":{"thread_ids":["example-thread","another-thread"]}}},
		{"issue_key":"APP-1","started_at_utc":"2026-05-14T11:00:00Z","duration_seconds":900,"description":"Review changes","metadata":{"codex":{"thread_ids":["example-thread","another-thread"]}}},
		{"issue_key":"APP-2","started_at_utc":"2026-05-15T09:00:00Z","duration_seconds":1200,"description":"Verify a separate fix","metadata":{"codex":{"thread_ids":["example-thread"]}}}
	]}`
	args := []string{"worklogs", "apply", "--stdin", "--output", "json"}
	invalid := strings.Replace(payload, "example-thread", "\xff", 1)
	rejected := runCLIWithInput(t, invalid, args...)
	if rejected.code != 2 || rejected.stdout != "" || !strings.Contains(rejected.stderr, "adds[0].metadata") {
		t.Fatalf("invalid metadata: %+v", rejected)
	}
	preview := metadataCLIJSON(t, runCLIWithInput(t, payload, append(args, "--dry")...))
	if preview["dry_run"] != true || countActiveCLIWorklogs(t) != 0 {
		t.Fatal(preview)
	}
	saved := metadataCLIJSON(t, runCLIWithInput(t, payload, args...))
	if saved["dry_run"] != false || countActiveCLIWorklogs(t) != 3 {
		t.Fatal(saved)
	}
	for _, result := range []map[string]any{preview, saved} {
		for i, entry := range result["items"].([]any) {
			record := entry.(map[string]any)["record"].(map[string]any)
			threads := record["metadata"].(map[string]any)["codex"].(map[string]any)["thread_ids"].([]any)
			if threads[0] != "example-thread" || (i < 2 && (len(threads) != 2 || threads[1] != "another-thread")) {
				t.Fatal(record)
			}
		}
	}
	matched := metadataCLIJSON(t, runCLI(t, "worklogs", "list", "--metadata-path", "/codex/thread_ids", "--metadata-value", `"example-thread"`, "--issue", "APP-1", "--from", "2026-05-14", "--to", "2026-05-14", "--fields", "id,metadata", "--output", "json"))
	if matched["total"] != float64(2) {
		t.Fatal(matched)
	}
	context := metadataCLIJSON(t, runCLI(t, "worklogs", "context", "--from", "2026-05-14", "--to", "2026-05-14", "--output", "json"))
	planning := context["planning"].(map[string]any)
	if planning["payload_contract"] != "apply_raw_adds_v1" || planning["payload_features"].([]any)[0] != "metadata" {
		t.Fatal(planning)
	}
}

func TestWorklogMetadataCLIValidationUsesStderr(t *testing.T) {
	for _, test := range []struct {
		args []string
		flag string
	}{
		{[]string{"add", "--metadata", "private-invalid-data"}, "--metadata"},
		{[]string{"add", "--metadata-file", filepath.Join(t.TempDir(), "missing.json")}, "--metadata-file"},
		{[]string{"update", "sample", "--metadata", "null", "--clear-metadata"}, "--clear-metadata"},
		{[]string{"list", "--metadata-path", "/codex"}, "--metadata-value"},
		{[]string{"list", "--metadata-path", "/bad~2escape", "--metadata-value", "null"}, "--metadata-path"},
	} {
		args := append([]string{"worklogs"}, test.args...)
		result := runCLI(t, append(args, "--output", "json")...)
		if result.code != 2 || result.stdout != "" || strings.Contains(result.stderr, "private-invalid-data") {
			t.Fatalf("invalid %v: %+v", test.args, result)
		}
		diagnostic := decodeJSONMap(t, []byte(result.stderr))["error"].(map[string]any)
		if diagnostic["code"] != "validation_error" || !strings.Contains(diagnostic["message"].(string), test.flag) {
			t.Fatal(diagnostic)
		}
	}
}

func TestWorklogMetadataFlagParseErrorsUseJSONStderr(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "--output", "json", "--metadata-path"}, "--metadata-path requires a value"},
		{[]string{"add", "--output=json", "--metadata-file"}, "--metadata-file requires a value"},
		{[]string{"update", "sample", "--clear-metadata=private-data", "--output", "json"}, "--clear-metadata has an invalid value"},
	} {
		result := runCLI(t, append([]string{"worklogs"}, test.args...)...)
		if result.code != 2 || result.stdout != "" || strings.Contains(result.stderr, "private-data") {
			t.Fatalf("%v: %+v", test.args, result)
		}
		if got := pullDiagnostic(t, result)["message"]; got != test.want {
			t.Fatalf("%v: want %q, got %v", test.args, test.want, got)
		}
	}
}
