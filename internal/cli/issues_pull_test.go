package cli

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const cloudIssuePullRouting = "      routing:\n        profiles:\n          default:\n            issue_prefixes: [APP]\n"
const dataIssuePullRouting = "      routing:\n        profiles:\n          default:\n            issue_prefixes: [OPS]\n"

func TestIssuesPullImportsAssignedIssuesAcrossJiraInstances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var removedEstimate atomic.Bool
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, token, ok := r.BasicAuth()
		if !ok || email != "user@example.com" || token != os.Getenv("WORKLEDGER_TEST_JIRA_CLOUD_TOKEN") || r.Method != http.MethodGet {
			t.Errorf("unexpected authentication or method for %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/rest/api/3/myself":
			fmt.Fprint(w, `{"accountId":"cloud-user"}`)
		case "/rest/api/3/search/jql":
			assertAssignedIssueQuery(t, r)
			switch r.URL.Query().Get("nextPageToken") {
			case "":
				firstIssue := `{"key":"APP-1","fields":{"summary":"From Jira","timetracking":{"originalEstimateSeconds":3600},"assignee":{"accountId":"cloud-user"}}}`
				if removedEstimate.Load() {
					firstIssue = `{"key":"APP-1","fields":{"assignee":{"accountId":"cloud-user"}}}`
				}
				fmt.Fprintf(w, `{"nextPageToken":"second","issues":[%s,
					{"key":"APP-2","fields":{"assignee":{"accountId":"data-user"}}},
					{"key":"APP-3","fields":{"assignee":null}},
					{"key":"APP-4","fields":{"assignee":{"accountId":"cloud-user"}}},
					{"key":"APP-5","fields":{"assignee":{"accountId":"cloud-user"}}},
					{"key":"APPX-1","fields":{"assignee":{"accountId":"cloud-user"}}},
					{"key":"OPS-3","fields":{"assignee":{"accountId":"cloud-user"}}}
				]}`, firstIssue)
			case "second":
				fmt.Fprint(w, `{"nextPageToken":"third","issues":[]}`)
			case "third":
				fmt.Fprint(w, `{"isLast":true,"issues":[{"id":"10006"},{"key":"APP-1","fields":{"assignee":{"accountId":"cloud-user"}}}]}`)
			default:
				t.Errorf("unexpected pagination: %s", r.URL.RawQuery)
				http.Error(w, "unexpected pagination", http.StatusBadRequest)
			}
		case "/rest/api/3/issue/10006":
			if r.URL.Query().Get("fields") != "summary,timetracking,assignee" {
				t.Errorf("issue lookup fields: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"key":"APP-6","fields":{"summary":"No worklogs","assignee":{"accountId":"cloud-user"}}}`)
		default:
			t.Errorf("unexpected Cloud endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+os.Getenv("WORKLEDGER_TEST_JIRA_DATA_TOKEN") || r.Method != http.MethodGet {
			t.Errorf("unexpected authentication or method for %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/rest/api/2/myself":
			fmt.Fprint(w, `{"key":"data-user","name":"operator"}`)
		case "/rest/api/2/search":
			assertAssignedIssueQuery(t, r)
			switch r.URL.Query().Get("startAt") {
			case "0":
				fmt.Fprint(w, `{"total":3,"issues":[{"key":"OPS-1","fields":{"assignee":{"key":"other-user","name":"operator"}}}]}`)
			case "1":
				fmt.Fprint(w, `{"total":3,"issues":[
					{"key":"OPS-2","fields":{"summary":"Data Center assignment","assignee":{"key":"data-user"}}},
					{"key":"OPS-4","fields":{"assignee":{"name":"operator"}}}
				]}`)
			default:
				t.Errorf("unexpected pagination: %s", r.URL.RawQuery)
				http.Error(w, "unexpected pagination", http.StatusBadRequest)
			}
		default:
			t.Errorf("unexpected Data Center endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer data.Close()
	writeConfigWithUTCAndJiraCloudAndJiraData(t,
		map[string]string{"main": cloud.URL, "unrouted": cloud.URL}, map[string]string{"main": data.URL},
		map[string]string{"main": cloudIssuePullRouting + "          reporting:\n            reporting_targets: {OPS: APP-4}\n      pull:\n        exclude_issues: [APP-5]\n"},
		map[string]string{"main": dataIssuePullRouting})

	for _, args := range [][]string{
		{"init"},
		{"issues", "set", "--issue", "APP-1", "--title", "Local title", "--epic", "Local epic", "--max-estimate", "2h"},
		{"worklogs", "add", "--issue", "APP-1", "--started", "2026-05-04T09:00", "--duration", "30m", "--description", "Local history"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatalf("%v: %+v", args, result)
		}
	}
	seedIssueMetadata(t, "APP-1", int64Ptr(1800))
	db, err := sql.Open("sqlite", filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const createdAt = "2026-05-01T09:00:00Z"
	const updatedAt = "2026-05-02T09:00:00Z"
	if _, err := db.ExecContext(t.Context(), `UPDATE issues SET created_at = ?, updated_at = ? WHERE issue_key = 'APP-1'`, createdAt, updatedAt); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{{"--instance", "main"}, {"--adapter", "clockify"}, {"--instance", "unknown"}}
	for _, args := range commands {
		result := runCLI(t, append([]string{"issues", "pull", "--output", "json"}, args...)...)
		if result.code != 2 || result.stdout != "" || pullDiagnostic(t, result)["code"] != "validation_error" {
			t.Fatalf("%v: %+v", args, result)
		}
	}
	result := runCLI(t, "issues", "pull", "--output", "json")
	if result.code != 0 || result.stderr != "" {
		t.Fatal(result)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["total"] != float64(4) || !reflect.DeepEqual(pulledIssueKeys(payload), []string{"APP-1", "APP-6", "OPS-2", "OPS-4"}) {
		t.Fatalf("imported issues: %s", result.stdout)
	}
	targets := payload["targets"].([]any)
	if len(targets) != 3 || targets[0].(map[string]any)["imported"] != float64(2) || targets[1].(map[string]any)["status"] != "skipped" || targets[2].(map[string]any)["imported"] != float64(2) {
		t.Fatalf("target outcomes: %s", result.stdout)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE issue_metadata SET refreshed_at = ? WHERE issue_key = 'APP-6'`, updatedAt); err != nil {
		t.Fatal(err)
	}
	result = runCLI(t, "issues", "pull", "--adapter", "jira-cloud", "--instance", "main")
	if result.code != 0 || !strings.Contains(result.stdout, "Issues created or updated from Jira: 0") || strings.Contains(result.stdout, "APP-1") || strings.Contains(result.stdout, "APP-6") {
		t.Fatalf("narrowed repeat pull: %+v", result)
	}
	result = runCLI(t, "issues", "pull", "--output", "plain")
	if result.code != 0 || result.stdout != "" || result.stderr != "" {
		t.Fatalf("plain no-change pull: %+v", result)
	}
	result = runCLI(t, "issues", "list", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload = decodeJSONMap(t, []byte(result.stdout))
	if !reflect.DeepEqual(pulledIssueKeys(payload), []string{"APP-1", "APP-6", "OPS-2", "OPS-4"}) {
		t.Fatalf("catalog: %s", result.stdout)
	}
	first := payload["items"].([]any)[0].(map[string]any)
	var creation string
	if err := db.QueryRowContext(t.Context(), `SELECT created_at FROM issues WHERE issue_key = 'APP-1'`).Scan(&creation); err != nil {
		t.Fatal(err)
	}
	if first["title"] != "Local title" || first["epic"] != "Local epic" || first["cached_title"] != "From Jira" ||
		first["max_estimate_seconds"] != float64(7200) || first["cached_max_estimate_seconds"] != float64(3600) ||
		first["logged_seconds"] != float64(1800) || creation != createdAt || first["updated_at"] != updatedAt {
		t.Fatalf("local state or Jira cache: %+v", first)
	}
	second := payload["items"].([]any)[1].(map[string]any)
	if second["refreshed_at"] != updatedAt {
		t.Fatalf("unchanged cache was refreshed: %+v", second)
	}
	removedEstimate.Store(true)
	result = runCLI(t, "issues", "pull", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload = decodeJSONMap(t, []byte(result.stdout))
	if payload["total"] != float64(1) || !reflect.DeepEqual(pulledIssueKeys(payload), []string{"APP-1"}) {
		t.Fatalf("expected only the issue with the cleared estimate: %s", result.stdout)
	}
	first = payload["items"].([]any)[0].(map[string]any)
	if first["title"] != "From Jira" || first["max_estimate_seconds"] != nil {
		t.Fatalf("updated cache result: %+v", first)
	}
	targets = payload["targets"].([]any)
	if targets[0].(map[string]any)["imported"] != float64(1) || targets[2].(map[string]any)["imported"] != float64(0) {
		t.Fatalf("changed counts: %s", result.stdout)
	}
	removedEstimate.Store(false)
	result = runCLI(t, "--output", "plain", "issues", "pull")
	if result.code != 0 || result.stdout != "APP-1\n" || result.stderr != "" {
		t.Fatalf("plain pull with one change: %+v", result)
	}
	removedEstimate.Store(true)
	if _, err := db.ExecContext(t.Context(), `UPDATE issue_metadata SET title = 'Old title' WHERE issue_key = 'OPS-2'`); err != nil {
		t.Fatal(err)
	}
	result = runCLI(t, "issues", "pull", "--output", "table")
	if result.code != 0 || !strings.Contains(result.stdout, "jira-cloud/main") || !strings.Contains(result.stdout, "jira-data-center/main") ||
		strings.Contains(result.stdout, "APP-6") || strings.Contains(result.stdout, "OPS-4") {
		t.Fatalf("table pull with duplicate instance names: %+v", result)
	}
	completion := runCLI(t, "__complete", "worklogs", "add", "--issue", "APP-")
	if completion.code != 0 || !strings.Contains(completion.stdout, "APP-6") {
		t.Fatalf("local completion: %+v", completion)
	}
	completion = runCLI(t, "__complete", "issues", "pull", "--adapter", "jira-")
	if completion.code != 0 || !strings.Contains(completion.stdout, "jira-cloud") || !strings.Contains(completion.stdout, "jira-data-center") {
		t.Fatalf("adapter completion: %+v", completion)
	}
	completion = runCLI(t, "__complete", "issues", "pull", "--adapter", "jira-cloud", "--instance", "unr")
	if completion.code != 0 || !strings.Contains(completion.stdout, "unrouted") {
		t.Fatalf("instance completion: %+v", completion)
	}
	completion = runCLI(t, "__complete", "issues", "pull", "--output", "p")
	if completion.code != 0 || !strings.Contains(completion.stdout, "plain") {
		t.Fatalf("output completion: %+v", completion)
	}
}

func TestIssuesPullReportsPartialSuccessAndDiscardsIncompleteBatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var cloudStatus, dataStatus atomic.Int32
	cloudStatus.Store(http.StatusServiceUnavailable)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			fmt.Fprint(w, `{"accountId":"cloud-user"}`)
		case "/rest/api/3/search/jql":
			if r.URL.Query().Get("nextPageToken") == "" {
				fmt.Fprint(w, `{"nextPageToken":"fail","issues":[{"key":"APP-1","fields":{"assignee":{"accountId":"cloud-user"}}}]}`)
				return
			}
			http.Error(w, "Jira request failed", int(cloudStatus.Load()))
		default:
			t.Errorf("unexpected Cloud endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			if status := dataStatus.Load(); status != 0 {
				http.Error(w, "Jira request failed", int(status))
				return
			}
			fmt.Fprint(w, `{"name":"data-user"}`)
		case "/rest/api/2/search":
			fmt.Fprint(w, `{"total":1,"issues":[{"key":"OPS-1","fields":{"assignee":{"name":"data-user"}}}]}`)
		default:
			t.Errorf("unexpected Data Center endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer data.Close()
	writeConfigWithUTCAndJiraCloudAndJiraData(t,
		map[string]string{"product": cloud.URL}, map[string]string{"internal": data.URL},
		map[string]string{"product": cloudIssuePullRouting}, map[string]string{"internal": dataIssuePullRouting})
	if result := runCLI(t, "init"); result.code != 0 {
		t.Fatal(result)
	}
	result := runCLI(t, "issues", "pull", "--output", "json")
	if result.code != 6 {
		t.Fatal(result)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	targets := payload["targets"].([]any)
	if len(targets) != 2 || targets[0].(map[string]any)["reason_code"] != "remote_error" || targets[0].(map[string]any)["message"] != nil || targets[1].(map[string]any)["status"] != "succeeded" ||
		!reflect.DeepEqual(pulledIssueKeys(payload), []string{"OPS-1"}) {
		t.Fatalf("failed target missing: %s", result.stdout)
	}
	diagnostic := pullDiagnostic(t, result)
	failures := diagnostic["details"].([]any)
	if diagnostic["code"] != "partial_success" || len(failures) != 1 || failures[0].(map[string]any)["run"] != "workledger issues pull --adapter jira-cloud --instance 'product'" {
		t.Fatalf("partial success diagnostic: %s", result.stderr)
	}
	result = runCLI(t, "issues", "pull", "--output", "plain")
	if result.code != 6 || result.stdout != "" || !strings.Contains(result.stderr, "--adapter jira-cloud --instance 'product'") {
		t.Fatalf("plain partial success without a change: %+v", result)
	}
	if result = runCLI(t, "issues", "pull", "--instance", "product", "--output", "json"); result.code != 5 || result.stdout != "" || pullDiagnostic(t, result)["code"] != "remote_error" {
		t.Fatalf("total failure: %+v", result)
	}
	result = runCLI(t, "issues", "list", "--output", "json")
	if result.code != 0 || !reflect.DeepEqual(pulledIssueKeys(decodeJSONMap(t, []byte(result.stdout))), []string{"OPS-1"}) {
		t.Fatalf("incomplete batch was saved: %+v", result)
	}
	for _, statuses := range [][2]int32{{http.StatusServiceUnavailable, http.StatusUnauthorized}, {http.StatusUnauthorized, http.StatusServiceUnavailable}} {
		cloudStatus.Store(statuses[0])
		dataStatus.Store(statuses[1])
		result := runCLI(t, "issues", "pull", "--output", "json")
		if result.code != 4 || result.stdout != "" {
			t.Fatalf("failure precedence for %v: %+v", statuses, result)
		}
		diagnostic := pullDiagnostic(t, result)
		if diagnostic["code"] != "auth_error" || len(diagnostic["details"].([]any)) != 2 {
			t.Fatalf("all-failed diagnostics for %v: %s", statuses, result.stderr)
		}
	}
	writeConfigWithUTCAndJiraCloudAndJiraData(t,
		map[string]string{"product": cloud.URL}, map[string]string{"internal": data.URL},
		map[string]string{"product": cloudIssuePullRouting}, nil)
	result = runCLI(t, "issues", "pull", "--output", "json")
	if result.code != 4 || result.stdout != "" {
		t.Fatalf("total failure with an unrouted source: %+v", result)
	}
	details := pullDiagnostic(t, result)["details"].([]any)
	if len(details) != 2 {
		t.Fatalf("expected one failure and one skip: %s", result.stderr)
	}
	skipped := details[1].(map[string]any)
	if skipped["adapter"] != "jira-data-center" || skipped["instance"] != "internal" || skipped["status"] != "skipped" ||
		skipped["code"] != "no_ownership_routes" || skipped["run"] != nil {
		t.Fatalf("skipped diagnostic: %s", result.stderr)
	}
	result = runCLI(t, "issues", "pull", "--output", "table")
	if result.code != 4 || result.stdout != "" || !strings.Contains(result.stderr, "jira-data-center/internal skipped: no Jira ownership routes configured") {
		t.Fatalf("table total failure with a skipped source: %+v", result)
	}
}

func TestIssuesPullRejectsAmbiguousOwnershipAndAllowsEmptySearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			fmt.Fprint(w, `{"name":"operator"}`)
		case "/rest/api/2/search":
			fmt.Fprint(w, `{"total":0,"issues":[]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	writeConfigWithUTCAndJiraCloudAndJiraData(t,
		map[string]string{"product": server.URL}, map[string]string{"internal": server.URL},
		map[string]string{"product": cloudIssuePullRouting}, map[string]string{"internal": cloudIssuePullRouting})
	if result := runCLI(t, "init"); result.code != 0 {
		t.Fatal(result)
	}
	result := runCLI(t, "issues", "pull", "--instance", "product")
	if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, "ambiguous Jira ownership routes") {
		t.Fatalf("ambiguous ownership: %+v", result)
	}
	writeConfigWithUTCAndJiraData(t, map[string]string{"internal": server.URL}, map[string]string{"internal": dataIssuePullRouting})
	result = runCLI(t, "issues", "pull", "--output", "json")
	if result.code != 0 || !reflect.DeepEqual(pulledIssueKeys(decodeJSONMap(t, []byte(result.stdout))), []string{}) {
		t.Fatalf("empty search: %+v", result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"issues", "pull", "--output", "json"}, &stdout, &stderr); code != 130 {
		t.Fatalf("canceled pull: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || pullDiagnostic(t, cliResult{code: 130, stderr: stderr.String()})["code"] != "interrupted" {
		t.Fatalf("canceled output: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestIssuesPullUsageDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"--bogus", "--output", "json"}, "unknown flag: --bogus"},
		{[]string{"--output=json", "--bogus"}, "unknown flag: --bogus"},
		{[]string{"---bogus", "--output", "json"}, "bad flag syntax: ---bogus"},
		{[]string{"--=bad", "--output", "json"}, "bad flag syntax: --=bad"},
		{[]string{"--output", "table", "---bogus", "--output=json"}, "bad flag syntax: ---bogus"},
		{[]string{"--help=nope", "--output", "json"}, "invalid argument"},
		{[]string{"-h=nope", "--output=json"}, "invalid argument"},
		{[]string{"--bogus", "--help=nope", "--output", "json"}, "unknown flag: --bogus"},
		{[]string{"--adapter=", "--output", "json"}, "--adapter requires a Jira adapter name"},
		{[]string{"--instance", "", "--output", "json"}, "--instance requires a configured Jira instance name"},
		{[]string{"--instance=   ", "--output", "json"}, "--instance requires a configured Jira instance name"},
		{[]string{"--output", "json", "--instance"}, "flag needs an argument"},
		{[]string{"--instance", "--output", "json"}, "unknown command"},
		{[]string{"--output", "json", "extra"}, "unknown command"},
		{[]string{"--output", "json"}, "config file does not exist"},
	} {
		result := runCLI(t, append([]string{"issues", "pull"}, test.args...)...)
		if result.code != 2 || result.stdout != "" {
			t.Fatalf("%v: %+v", test.args, result)
		}
		diagnostic := pullDiagnostic(t, result)
		if diagnostic["code"] != "validation_error" || !strings.Contains(diagnostic["message"].(string), test.message) {
			t.Fatalf("%v: %s", test.args, result.stderr)
		}
	}
	result := runCLI(t, "issues", "pull", "---bogus", "--", "--output", "json")
	if result.code != 2 || result.stdout != "" || !strings.HasPrefix(result.stderr, "Error: bad flag syntax: ---bogus") {
		t.Fatalf("output flag after --: %+v", result)
	}
	result = runCLI(t, "issues", "pull", "--output", "yaml")
	if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, "output must be table, json, or plain") {
		t.Fatalf("invalid output mode: %+v", result)
	}
	writeConfigContent(t, "default_output: json\n")
	result = runCLI(t, "issues", "pull", "--bogus", "--output", "plain")
	if result.code != 2 || result.stdout != "" || !strings.HasPrefix(result.stderr, "Error: unknown flag: --bogus") {
		t.Fatalf("plain diagnostic with JSON default: %+v", result)
	}
}

func pullDiagnostic(t *testing.T, result cliResult) map[string]any {
	t.Helper()
	diagnostic := decodeJSONMap(t, []byte(result.stderr))["error"].(map[string]any)
	if diagnostic["exit_code"] != float64(result.code) {
		t.Fatalf("diagnostic lacks exit code %d: %s", result.code, result.stderr)
	}
	return diagnostic
}

func assertAssignedIssueQuery(t *testing.T, r *http.Request) {
	t.Helper()
	if query := r.URL.Query(); query.Get("jql") != "assignee = currentUser() ORDER BY key ASC" || query.Get("fields") != "summary,timetracking,assignee" {
		t.Errorf("unexpected Jira search query: %s", r.URL.RawQuery)
	}
}

func pulledIssueKeys(payload map[string]any) []string {
	items := payload["items"].([]any)
	keys := make([]string, 0, len(items))
	for _, issue := range items {
		keys = append(keys, issue.(map[string]any)["issue_key"].(string))
	}
	return keys
}
