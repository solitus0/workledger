package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionCommandIsVisibleWithoutPowerShell(t *testing.T) {
	rootHelp := runCLI(t, "--help")
	if rootHelp.code != 0 || !strings.Contains(rootHelp.stdout, "completion") {
		t.Fatalf("expected visible completion command, code=%d stdout=%s stderr=%s", rootHelp.code, rootHelp.stdout, rootHelp.stderr)
	}

	help := runCLI(t, "completion", "--help")
	if help.code != 0 {
		t.Fatalf("completion help failed: code=%d stdout=%s stderr=%s", help.code, help.stdout, help.stderr)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if !strings.Contains(help.stdout, shell) {
			t.Fatalf("expected completion help to contain %q, stdout=%s", shell, help.stdout)
		}
	}
	if strings.Contains(strings.ToLower(help.stdout), "powershell") {
		t.Fatalf("completion help must not advertise PowerShell, stdout=%s", help.stdout)
	}

	powerShell := runCLI(t, "completion", "powershell")
	if powerShell.code == 0 || !strings.Contains(powerShell.stderr, `unknown command "powershell"`) {
		t.Fatalf("expected PowerShell rejection, code=%d stdout=%s stderr=%s", powerShell.code, powerShell.stdout, powerShell.stderr)
	}
}

func TestCompletionScriptsNeedNoConfigOrStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := map[string]string{
		"bash": "__start_workledger",
		"zsh":  "#compdef workledger",
		"fish": "complete -c workledger",
	}
	for shell, marker := range tests {
		t.Run(shell, func(t *testing.T) {
			result := runCLI(t, "completion", shell)
			if result.code != 0 || result.stderr != "" || !strings.Contains(result.stdout, marker) {
				t.Fatalf("completion generation failed: code=%d marker=%q stdout=%s stderr=%s", result.code, marker, result.stdout, result.stderr)
			}
		})
	}

	for _, path := range []string{
		filepath.Join(home, ".config", "workledger", "config.yaml"),
		filepath.Join(home, ".local", "share", "workledger", "worklogs.db"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("completion generation must not create %s", path)
		}
	}
}

func TestCompletionGeneratorsRejectNoDescriptionsFlag(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		result := runCLI(t, "completion", shell, "--no-descriptions")
		if result.code == 0 || !strings.Contains(result.stderr, "unknown flag: --no-descriptions") {
			t.Fatalf("expected %s to reject --no-descriptions, code=%d stdout=%s stderr=%s", shell, result.code, result.stdout, result.stderr)
		}
	}
}

func TestFixedFlagCompletionUsesPrefixAndOmitsRepeatedAdapters(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		want      []string
		doNotWant []string
	}{
		{name: "output", args: []string{"--output", "j"}, want: []string{"json"}, doNotWant: []string{"table"}},
		{name: "progress", args: []string{"plan", "apply", "--progress", "p"}, want: []string{"plain"}, doNotWant: []string{"auto", "bar", "off"}},
		{name: "retry scope", args: []string{"plan", "retry", "--only", "f"}, want: []string{"failed"}, doNotWant: []string{"uncertain"}},
		{name: "metadata field", args: []string{"issue-metadata", "refresh", "--field", "m"}, want: []string{"max-estimate"}},
		{name: "metadata adapter", args: []string{"issue-metadata", "refresh", "--adapter", "jira-"}, want: []string{"jira-cloud", "jira-data-center"}, doNotWant: []string{"clockify"}},
		{name: "repeated adapter", args: []string{"plan", "reconcile", "--adapter", "jira-cloud", "--adapter", ""}, want: []string{"clockify", "jira-data-center"}, doNotWant: []string{"jira-cloud"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runCompletion(t, tc.args...)
			if result.code != 0 {
				t.Fatalf("completion failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
			values := completionValues(result.stdout)
			for _, want := range tc.want {
				if !containsString(values, want) {
					t.Fatalf("expected %q in %#v; stdout=%s stderr=%s", want, values, result.stdout, result.stderr)
				}
			}
			for _, unwanted := range tc.doNotWant {
				if containsString(values, unwanted) {
					t.Fatalf("did not expect %q in %#v; stdout=%s stderr=%s", unwanted, values, result.stdout, result.stderr)
				}
			}
		})
	}
}

func TestCompletionCandidateDescriptionsAreSanitized(t *testing.T) {
	items := renderCompletionCandidates([]completionCandidate{{
		value:       "candidate",
		description: "line one\nline\ttwo",
	}}, "can", nil)
	if len(items) != 1 || items[0] != "candidate\tline one line two" {
		t.Fatalf("unexpected sanitized candidates %#v", items)
	}
}

func TestDynamicCompletionUsesOnlyLocalActiveData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "APP-10", "--started", "2026-05-01T09:00", "--duration", "1h", "--description", "First completion record", "--output", "json")
	second := runCLI(t, "worklogs", "add", "--issue", "OPS-20", "--started", "2026-05-01T11:00", "--duration", "1h", "--description", "Second completion record", "--output", "json")
	if first.code != 0 || second.code != 0 {
		t.Fatalf("seed worklogs failed: first=%+v second=%+v", first, second)
	}
	firstID := decodeJSONMap(t, []byte(first.stdout))["id"].(string)
	secondID := decodeJSONMap(t, []byte(second.stdout))["id"].(string)

	db := openTestDB(t)
	if _, err := db.Exec(`INSERT INTO issue_metadata(issue_key, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at) VALUES('META-30', 3600, 'jira-cloud', 'product', '2026-05-01T12:00:00Z')`); err != nil {
		db.Close()
		t.Fatalf("seed issue metadata: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction, adapter_family, adapter_instance) VALUES('trashed-only', 'local', NULL, 'OLD-1', '2026-05-01T08:00:00Z', 900, 'old', '2026-05-01T10:00:00Z', 'deleted', '', '', NULL, NULL)`); err != nil {
		db.Close()
		t.Fatalf("seed trash: %v", err)
	}
	_ = db.Close()

	seedSavedPlan(t, savedPlanSeed{planID: "plan-old", itemID: "item-old", direction: "push", adapter: "clockify", target: "APP-10", action: "create", payloadJSON: `[]`, createdAt: "2026-05-01T12:00:00Z"})
	seedSavedPlan(t, savedPlanSeed{planID: "plan-new", itemID: "item-new", direction: "push", adapter: "clockify", target: "OPS-20", action: "create", payloadJSON: `[]`, createdAt: "2026-05-02T12:00:00Z"})

	worklogResult := runCompletion(t, "worklogs", "update", firstID[:8])
	assertCompletionContains(t, worklogResult, firstID)
	if containsString(completionValues(worklogResult.stdout), secondID) || containsString(completionValues(worklogResult.stdout), "trashed-only") {
		t.Fatalf("worklog completion leaked a nonmatching or trashed ID: %s", worklogResult.stdout)
	}

	for _, args := range [][]string{
		{"worklogs", "add", "--issue", "APP"},
		{"route", "explain", "META"},
	} {
		result := runCompletion(t, args...)
		want := "APP-10"
		if args[0] == "route" {
			want = "META-30"
		}
		assertCompletionContains(t, result, want)
	}

	planResult := runCompletion(t, "plan", "show", "plan-")
	values := completionValues(planResult.stdout)
	if len(values) < 2 || values[0] != "plan-new" || values[1] != "plan-old" {
		t.Fatalf("expected newest plans first, got %#v; stdout=%s stderr=%s", values, planResult.stdout, planResult.stderr)
	}
}

func TestConfiguredInstanceAndRouteProfileCompletionRespectsSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndJiraCloudAndJiraData(
		t,
		map[string]string{"product": "https://cloud.example", "shared": "https://shared-cloud.example"},
		map[string]string{"internal": "https://dc.example", "shared": "https://shared-dc.example"},
		map[string]string{
			"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - APP\n          reporting:\n            issue_prefixes:\n              - REP\n",
		},
		map[string]string{
			"internal": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - OPS\n",
		},
	)

	cloudInstances := runCompletion(t, "plan", "reconcile", "--adapter", "jira-cloud", "--instance", "")
	values := completionValues(cloudInstances.stdout)
	for _, want := range []string{"product", "shared"} {
		if !containsString(values, want) {
			t.Fatalf("expected cloud instance %q in %#v; stdout=%s stderr=%s", want, values, cloudInstances.stdout, cloudInstances.stderr)
		}
	}
	if containsString(values, "internal") {
		t.Fatalf("did not expect Jira Data Center instance in %#v", values)
	}
	allInstances := runCompletion(t, "totals", "--instance", "")
	sharedCount := 0
	for _, value := range completionValues(allInstances.stdout) {
		if value == "shared" {
			sharedCount++
		}
	}
	if sharedCount != 1 {
		t.Fatalf("expected duplicate cross-family instance once, got %d in %s", sharedCount, allInstances.stdout)
	}

	repeated := runCompletion(t, "plan", "reconcile", "--adapter", "jira-cloud", "--instance", "product", "--instance", "")
	if containsString(completionValues(repeated.stdout), "product") {
		t.Fatalf("repeated instance should be omitted: %s", repeated.stdout)
	}

	profiles := runCompletion(t, "plan", "reconcile", "--adapter", "jira-cloud", "--instance", "product", "--route-profile", "r")
	assertCompletionContains(t, profiles, "reporting")

	clockifyProfiles := runCompletion(t, "plan", "reconcile", "--adapter", "clockify", "--route-profile", "")
	if values := completionValues(clockifyProfiles.stdout); len(values) != 0 {
		t.Fatalf("Clockify must not offer Jira route profiles, got %#v", values)
	}
}

func TestDynamicCompletionSilentlyDegradesForInvalidLocalState(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeConfigContent(t, "unknown: true\n")
		result := runCompletion(t, "totals", "--instance", "")
		if result.code != 0 || len(completionValues(result.stdout)) != 0 {
			t.Fatalf("expected invalid config to suppress candidates, code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
	})

	t.Run("corrupt storage", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		path := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create storage dir: %v", err)
		}
		writeConfigAtPath(t, path)
		original := []byte("not a sqlite database")
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatalf("seed corrupt storage: %v", err)
		}

		result := runCompletion(t, "worklogs", "update", "")
		if result.code != 0 || len(completionValues(result.stdout)) != 0 {
			t.Fatalf("expected corrupt storage to suppress candidates, code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read corrupt storage: %v", err)
		}
		if string(after) != string(original) {
			t.Fatal("completion modified corrupt storage")
		}
	})
}

func TestDynamicCompletionSilentlyDegradesWithoutLocalState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, args := range [][]string{
		{"worklogs", "update", ""},
		{"plan", "show", ""},
		{"worklogs", "add", "--issue", ""},
		{"totals", "--instance", ""},
	} {
		result := runCompletion(t, args...)
		if result.code != 0 || len(completionValues(result.stdout)) != 0 {
			t.Fatalf("expected empty successful completion for %v, code=%d stdout=%s stderr=%s", args, result.code, result.stdout, result.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "workledger", "worklogs.db")); !os.IsNotExist(err) {
		t.Fatal("dynamic completion must not create SQLite storage")
	}
}

func runCompletion(t *testing.T, args ...string) cliResult {
	t.Helper()
	return runCLI(t, append([]string{"__complete"}, args...)...)
}

func completionValues(output string) []string {
	values := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		value, _, _ := strings.Cut(line, "\t")
		values = append(values, value)
	}
	return values
}

func assertCompletionContains(t *testing.T, result cliResult, want string) {
	t.Helper()
	if result.code != 0 || !containsString(completionValues(result.stdout), want) {
		t.Fatalf("expected completion %q, code=%d stdout=%s stderr=%s", want, result.code, result.stdout, result.stderr)
	}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}
