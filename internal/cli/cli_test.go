package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/worklogs"
	_ "modernc.org/sqlite"
)

func TestVersionJSONWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := Run(context.Background(), []string{"version", "--output", "json"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr=%s", code, stderr.String())
	}

	payload := decodeJSONMap(t, stdout.Bytes())
	if payload["version"] != Version {
		t.Fatalf("expected version %q, got %#v", Version, payload["version"])
	}
}

func TestVersionLongFlagWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	result := runCLI(t, "--version")
	if result.code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr=%s", result.code, result.stderr)
	}
	if result.stdout != Version+"\n" {
		t.Fatalf("expected version output %q, got %q", Version+"\n", result.stdout)
	}
	if result.stderr != "" {
		t.Fatalf("expected empty stderr, got %q", result.stderr)
	}
}

func TestVersionShortFlagJSONWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := Run(context.Background(), []string{"-v", "--output", "json"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr=%s", code, stderr.String())
	}

	payload := decodeJSONMap(t, stdout.Bytes())
	if payload["version"] != Version {
		t.Fatalf("expected version %q, got %#v", Version, payload["version"])
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestHelpShowsAcceptedDateFormatsAndExamples(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name: "worklogs add",
			args: []string{"worklogs", "add", "--help"},
			contains: []string{
				"Local started timestamp: YYYY-MM-DDTHH:MM",
				"UTC started timestamp in RFC3339",
				"workledger worklogs add --issue PROJ-123 --started todayT09:00",
				"Date filter modifiers:",
				"--week-offset int",
			},
		},
		{
			name: "worklogs context",
			args: []string{"worklogs", "context", "--help"},
			contains: []string{
				"From Date selector: YYYY-MM-DD",
				"Clock time in HH:MM, e.g. 09:00",
				"Lunch exclusion window in HH:MM-HH:MM",
				"Weekday filters:",
			},
		},
		{
			name: "worklogs apply",
			args: []string{"worklogs", "apply", "--help"},
			contains: []string{
				"Payload timestamps:",
				"started_at uses the same local timestamp grammar as --started",
				"started_at_utc uses RFC3339 UTC, e.g. 2026-05-14T09:00:00Z",
			},
		},
		{
			name: "totals",
			args: []string{"totals", "--help"},
			contains: []string{
				"From Date selector: YYYY-MM-DD",
				"workledger totals --from 2026-05-14 --to 2026-05-16 --adapter clockify",
				"Date filters:",
			},
		},
		{
			name: "plan reconcile",
			args: []string{"plan", "reconcile", "--help"},
			contains: []string{
				"To Date selector: YYYY-MM-DD",
				"workledger plan reconcile --pull --adapter jira-cloud --from 2026-05-14 --to 2026-05-16",
				"Date filter modifiers:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)
			if result.code != 0 {
				t.Fatalf("expected help exit 0, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
			for _, want := range tc.contains {
				if !strings.Contains(result.stdout, want) {
					t.Fatalf("expected help to contain %q, got stdout=%s", want, result.stdout)
				}
			}
		})
	}
}

func TestWorklogsListHelpKeepsGroupedFlagIndentation(t *testing.T) {
	result := runCLI(t, "worklogs", "list", "--help")
	if result.code != 0 {
		t.Fatalf("expected help exit 0, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	for _, want := range []string{
		"workledger worklogs list --mon --week-offset -1",
		"Flags:\n      --fields string",
		"Date filters:\n      --today",
		"Weekday filters:\n      --mon",
		"Date filter modifiers:\n      --week-offset int",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("expected help to contain %q, got stdout=%s", want, result.stdout)
		}
	}
}

func TestWorklogsAddRejectsFullISOLocalTimestampWithExplicitFormatHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00:00+03:00", "--duration", "1h", "--description", "Invalid started", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "started must use YYYY-MM-DDTHH:MM") {
		t.Fatalf("expected explicit started format hint, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "time must use HH:MM, e.g. 09:00") {
		t.Fatalf("expected explicit HH:MM hint, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestWorklogsContextRejectsInvalidDayStartWithExample(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "worklogs", "context", "--today", "--day-start", "9am", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "time must use HH:MM, e.g. 09:00") {
		t.Fatalf("expected explicit clock format hint, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestStatusWithoutConfigSuggestsInit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	result := runCLI(t, "status")
	if result.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "config file does not exist; run workledger init") {
		t.Fatalf("expected init hint in stdout, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestInitValidateAndListEmptyForToday(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	validate := runCLI(t, "config", "validate", "--output", "json")
	if validate.code != 0 {
		t.Fatalf("validate failed: code=%d stdout=%s stderr=%s", validate.code, validate.stdout, validate.stderr)
	}
	validatePayload := decodeJSONMap(t, []byte(validate.stdout))
	effective := validatePayload["effective"].(map[string]any)
	if effective["local_timezone"] != "Europe/Vilnius" {
		t.Fatalf("expected effective local_timezone, got %#v", effective["local_timezone"])
	}
	worklogs := effective["worklogs"].(map[string]any)
	if worklogs["daily_minimum_quota_seconds"] != float64(28800) {
		t.Fatalf("expected effective daily minimum quota, got %#v", worklogs["daily_minimum_quota_seconds"])
	}
	if worklogs["day_start"] != "08:00" || worklogs["day_end"] != "17:00" {
		t.Fatalf("expected effective workday defaults, got %#v", worklogs)
	}
	if worklogs["daily_lunch"] != "12:00-12:45" {
		t.Fatalf("expected effective daily lunch, got %#v", worklogs["daily_lunch"])
	}
	if _, ok := effective["selection"]; ok {
		t.Fatalf("expected no nested selection in effective payload, got %#v", effective["selection"])
	}

	list := runCLI(t, "worklogs", "list", "--today", "--output", "json")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}

	payload := decodeJSONMap(t, []byte(list.stdout))
	if payload["total"].(float64) != 0 {
		t.Fatalf("expected empty list, got %#v", payload["total"])
	}
}

func TestInitTableOutputShowsBootstrapPathsAndNextSteps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOCKIFY_API_KEY", "")

	result := runCLI(t, "init")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	expected := fmt.Sprintf(
		"Local worklogs are ready.\n\nConfig:\n  Status: created new config\n  Path: %s\n\nClockify:\n  Status: not auto-configured from CLOCKIFY_API_KEY\n\nDatabase:\n  Path: %s\n\nNext:\n  workledger worklogs add\n\nOptional adapter setup:\n  workledger setup jira-cloud --instance <name>\n  workledger setup jira-data-center --instance <name>\n  workledger setup clockify\n\nValidate anytime:\n  workledger config validate\n",
		filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml"),
		filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db"),
	)
	if result.stdout != expected {
		t.Fatalf("unexpected init stdout\nexpected:\n%s\ngot:\n%s", expected, result.stdout)
	}
	if result.stderr != "" {
		t.Fatalf("expected empty stderr, got %s", result.stderr)
	}
}

func TestInitTableOutputExplicitlyReportsReusedExistingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOCKIFY_API_KEY", "")

	first := runCLI(t, "init")
	if first.code != 0 {
		t.Fatalf("first init failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	second := runCLI(t, "init")
	if second.code != 0 {
		t.Fatalf("second init failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}
	if !strings.Contains(second.stdout, "Status: reused existing valid config") {
		t.Fatalf("expected reused config status in stdout, got %s", second.stdout)
	}
	if !strings.Contains(second.stdout, "Clockify:\n  Status: kept existing config") {
		t.Fatalf("expected reused clockify status in stdout, got %s", second.stdout)
	}
}

func TestWorklogsListRequiresTimeSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--output", "json")
	if list.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "requires at least one time selector") && !strings.Contains(list.stderr, "requires at least one time selector") {
		t.Fatalf("expected time selector validation message, got stdout=%s stderr=%s", list.stdout, list.stderr)
	}
}

func TestInitWithoutClockifyAPIKeyWritesCommentedTemplate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOCKIFY_API_KEY", "")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !bytes.Contains(data, []byte("local_timezone: Europe/Vilnius\n")) {
		t.Fatalf("expected default timezone block, got %s", content)
	}
	fragments := []string{
		"worklogs:\n  minimum_duration_seconds: 900\n  # daily_minimum_quota_seconds is for workledger worklogs context\n  daily_minimum_quota_seconds: 28800\n  # day_start and day_end are only used for workledger worklogs context\n  day_start: 08:00\n  day_end: 17:00\n  # daily_lunch is only used for workledger worklogs context\n  daily_lunch: 12:00-12:45\n",
		"# clockify:\n#   workspace_id: your-workspace-id\n#   user_id: your-user-id\n#   auth:\n#     api_key_env: CLOCKIFY_API_KEY\n#   project_mapping:\n#     issue_prefixes:\n#       WEB: App Project\n#     default_project: Default Project # fallback project when no issue prefix matches\n#     create_issue_tag_if_missing: true # automation default; create missing issue tags on push\n",
		"# jira_cloud:\n#   instances:\n#     product:\n#       base_url: https://example.atlassian.net\n#       auth:\n#         email: user@example.com\n#         token_env: WORKLEDGER_JIRA_CLOUD_PRODUCT_TOKEN\n#       pull:\n#         exclude_issues: # issue keys that pull must never import into local storage; reporting issues are excluded by default\n#           - REPORT-2\n#       routing:\n#         profiles:\n#           default:\n#             issue_prefixes:\n#               - WEB\n#           # Reconcile this reporting profile with:\n#           # workledger plan reconcile --push --adapter=jira-cloud --instance product --route-profile reporting --today\n#           reporting: # non-default profile for fixed reporting issue routing\n#             reporting_targets: # canonical prefix -> fixed reporting issue key; OPS matches jira_data_center.instances.internal.routing.profiles.default.issue_prefixes\n#               OPS: REPORT-1\n",
		"# jira_data_center:\n#   instances:\n#     internal:\n#       base_url: https://jira.example.com\n#       auth:\n#         bearer:\n#           token_env: WORKLEDGER_JIRA_DC_INTERNAL_TOKEN\n#       routing:\n#         profiles:\n#           default:\n#             issue_prefixes:\n#               - OPS\n",
	}
	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected scaffold fragment %q, got %s", fragment, content)
		}
	}

	validate := runCLI(t, "config", "validate", "--output", "json")
	if validate.code != 0 {
		t.Fatalf("validate failed: code=%d stdout=%s stderr=%s", validate.code, validate.stdout, validate.stderr)
	}
}

func TestInitRejectsCorruptSQLiteWithDedicatedMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	sqlitePath := filepath.Join(t.TempDir(), "worklogs.db")
	if err := os.WriteFile(sqlitePath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("seed invalid sqlite: %v", err)
	}
	writeConfigAtPath(t, sqlitePath)

	result := runCLI(t, "init")
	if result.code != 1 {
		t.Fatalf("expected exit 1, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("expected empty stdout, got %q", result.stdout)
	}
	if !strings.Contains(result.stderr, "Local SQLite store is corrupt or incompatible and cannot be repaired additively.") {
		t.Fatalf("expected unrecoverable sqlite message, got %s", result.stderr)
	}
	if !strings.Contains(result.stderr, "sqlite_path: "+sqlitePath) {
		t.Fatalf("expected sqlite path in stderr, got %s", result.stderr)
	}
	if !strings.Contains(result.stderr, "inspect, replace, or restore the SQLite file") {
		t.Fatalf("expected next-step guidance, got %s", result.stderr)
	}
}

func TestInitRejectsIncompatibleSQLiteWithDedicatedJSONPayload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sqlitePath := filepath.Join(t.TempDir(), "worklogs.db")
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE worklogs (
		id TEXT PRIMARY KEY,
		issue_key TEXT NOT NULL,
		started_at_utc TEXT NOT NULL,
		duration_seconds TEXT NOT NULL,
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("seed incompatible worklogs: %v", err)
	}
	_ = db.Close()
	writeConfigAtPath(t, sqlitePath)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 1 {
		t.Fatalf("expected exit 1, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if len(payload) != 3 {
		t.Fatalf("expected exact json payload keys, got %#v", payload)
	}
	if payload["reason"] != "sqlite_unrecoverable" {
		t.Fatalf("expected sqlite_unrecoverable reason, got %#v", payload["reason"])
	}
	if payload["message"] != "Local SQLite store is corrupt or incompatible and cannot be repaired additively." {
		t.Fatalf("unexpected message %#v", payload["message"])
	}
	if payload["sqlite_path"] != sqlitePath {
		t.Fatalf("expected sqlite_path %s, got %#v", sqlitePath, payload["sqlite_path"])
	}
	if !strings.Contains(result.stderr, "sqlite_path: "+sqlitePath) {
		t.Fatalf("expected stderr diagnostic path, got %s", result.stderr)
	}
}

func TestInitWithClockifyAPIKeyPersistsActiveClockifyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := newClockifyTestServer(t)
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !bytes.Contains(data, []byte("local_timezone: Europe/Vilnius\n")) {
		t.Fatalf("expected default timezone block, got %s", content)
	}
	if !strings.Contains(content, "worklogs:\n  minimum_duration_seconds: 900\n  # daily_minimum_quota_seconds is for workledger worklogs context\n  daily_minimum_quota_seconds: 28800\n  # day_start and day_end are only used for workledger worklogs context\n  day_start: 08:00\n  day_end: 17:00\n  # daily_lunch is only used for workledger worklogs context\n  daily_lunch: 12:00-12:45\n") {
		t.Fatalf("expected daily quota config block, got %s", content)
	}
	if !strings.Contains(content, "clockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: CLOCKIFY_API_KEY\n  # project_mapping:\n  #   issue_prefixes:\n  #     WEB: App Project\n  #   default_project: Default Project # fallback project when no issue prefix matches\n  #   create_issue_tag_if_missing: true # automation default; create missing issue tags on push\n") {
		t.Fatalf("expected active clockify config, got %s", content)
	}
	if !strings.Contains(content, "# jira_cloud:\n#   instances:\n#     product:\n#       base_url: https://example.atlassian.net\n") {
		t.Fatalf("expected commented jira cloud reference, got %s", content)
	}
	if !strings.Contains(content, "# jira_data_center:\n#   instances:\n#     internal:\n#       base_url: https://jira.example.com\n") {
		t.Fatalf("expected commented jira data center reference, got %s", content)
	}
	if bytes.Contains(data, []byte("clockify-key")) {
		t.Fatalf("expected active clockify config, got %s", content)
	}

	table := runCLI(t, "init")
	if table.code != 0 {
		t.Fatalf("table init failed: code=%d stdout=%s stderr=%s", table.code, table.stdout, table.stderr)
	}
	if !strings.Contains(table.stdout, "Clockify:\n  Status: kept existing config") {
		t.Fatalf("expected reused clockify status after bootstrap, got %s", table.stdout)
	}
}

func TestInitTableOutputReportsClockifyAutoConfiguredFromDiscoveredEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := newClockifyTestServer(t)
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "init")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "Clockify:\n  Status: auto-configured from discovered CLOCKIFY_API_KEY") {
		t.Fatalf("expected auto-configured clockify status in stdout, got %s", result.stdout)
	}
}

func TestInitWithClockifyAPIKeySkipsClockifyConfigWhenLookupFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !bytes.Contains(data, []byte("local_timezone: Europe/Vilnius\n")) {
		t.Fatalf("expected default timezone block, got %s", content)
	}
	if bytes.Contains(data, []byte("\nclockify:\n")) {
		t.Fatalf("expected no active clockify block, got %s", content)
	}
	if !strings.Contains(content, "# clockify:\n#   workspace_id: your-workspace-id\n#   user_id: your-user-id\n#   auth:\n#     api_key_env: CLOCKIFY_API_KEY\n#   project_mapping:\n") {
		t.Fatalf("expected commented clockify scaffold fallback, got %s", content)
	}
	if !strings.Contains(content, "# jira_cloud:\n#   instances:\n#     product:\n#       base_url: https://example.atlassian.net\n") {
		t.Fatalf("expected commented jira cloud reference, got %s", content)
	}
}

func TestInitWithClockifyAPIKeySkipsClockifyConfigWhenLookupIsIncomplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"","activeWorkspace":"","defaultWorkspace":""}`))
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !bytes.Contains(data, []byte("local_timezone: Europe/Vilnius\n")) {
		t.Fatalf("expected default timezone block, got %s", content)
	}
	if bytes.Contains(data, []byte("\nclockify:\n")) {
		t.Fatalf("expected no active clockify block, got %s", content)
	}
	if !strings.Contains(content, "# clockify:\n#   workspace_id: your-workspace-id\n#   user_id: your-user-id\n#   auth:\n#     api_key_env: CLOCKIFY_API_KEY\n#   project_mapping:\n") {
		t.Fatalf("expected commented clockify scaffold fallback, got %s", content)
	}
	if !strings.Contains(content, "# jira_data_center:\n#   instances:\n#     internal:\n#       base_url: https://jira.example.com\n") {
		t.Fatalf("expected commented jira data center reference, got %s", content)
	}
}

func TestSetupJiraCloudCreatesInstanceAndRejectsExisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t,
		"setup", "jira-cloud",
		"--instance", "product",
		"--base-url", "https://example.atlassian.net/",
		"--email", "user@example.com",
		"--token-env", "WORKLEDGER_JIRA_TOKEN",
		"--issue-prefix", "AAPP",
		"--output", "json",
	)
	if result.code != 0 {
		t.Fatalf("setup failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "jira_cloud:") || !strings.Contains(content, "token_env: WORKLEDGER_JIRA_TOKEN") {
		t.Fatalf("expected jira cloud block, got %s", content)
	}
	if strings.Contains(content, "jira-token") {
		t.Fatalf("unexpected inline secret in config: %s", content)
	}

	duplicate := runCLI(t,
		"setup", "jira-cloud",
		"--instance", "product",
		"--base-url", "https://example.atlassian.net",
		"--email", "user@example.com",
		"--token-env", "WORKLEDGER_JIRA_TOKEN",
		"--issue-prefix", "AAPP",
		"--output", "json",
	)
	if duplicate.code != 2 {
		t.Fatalf("expected duplicate setup failure, got code=%d stdout=%s stderr=%s", duplicate.code, duplicate.stdout, duplicate.stderr)
	}
}

func TestSetupJiraCloudTableOutputShowsEnvVarAndVerifyHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t,
		"setup", "jira-cloud",
		"--instance", "product",
		"--base-url", "https://example.atlassian.net/",
		"--email", "user@example.com",
		"--token-env", "WORKLEDGER_JIRA_TOKEN",
		"--issue-prefix", "AAPP",
	)
	if result.code != 0 {
		t.Fatalf("setup failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	expected := "Add this environment variable before running adapter commands:\n\nexport WORKLEDGER_JIRA_TOKEN=...\n\nThen verify:\n  workledger status --adapter jira-cloud --instance product --explain\n"
	if result.stdout != expected {
		t.Fatalf("unexpected setup stdout\nexpected:\n%s\ngot:\n%s", expected, result.stdout)
	}
}

func TestSetupJiraCloudPromptsOnTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	originalTTY := isTTYReader
	isTTYReader = func(io.Reader) bool { return true }
	defer func() { isTTYReader = originalTTY }()

	result := runCLIWithInput(t,
		"product\nhttps://example.atlassian.net\nuser@example.com\nWORKLEDGER_JIRA_TOKEN\nAAPP\n",
		"setup", "jira-cloud",
		"--output", "json",
	)
	if result.code != 0 {
		t.Fatalf("prompted setup failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter"] != "jira-cloud" || payload["instance"] != "product" {
		t.Fatalf("unexpected setup payload %s", result.stdout)
	}
}

func TestSetupJiraDataCenterPromptsOnTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	originalTTY := isTTYReader
	isTTYReader = func(io.Reader) bool { return true }
	defer func() { isTTYReader = originalTTY }()

	result := runCLIWithInput(t,
		"ito\nhttps://jira.example.com\nWORKLEDGER_JIRA_DC_TOKEN\nAAPP\n",
		"setup", "jira-data-center",
		"--output", "json",
	)
	if result.code != 0 {
		t.Fatalf("prompted setup failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter"] != "jira-data-center" || payload["instance"] != "ito" {
		t.Fatalf("unexpected setup payload %s", result.stdout)
	}
}

func TestSetupJiraDataCenterTableOutputShowsEnvVarAndVerifyHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t,
		"setup", "jira-data-center",
		"--instance", "ito",
		"--base-url", "https://jira.example.com",
		"--token-env", "WORKLEDGER_JIRA_DC_ITO_TOKEN",
		"--issue-prefix", "AAPP",
	)
	if result.code != 0 {
		t.Fatalf("setup failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	expected := "Add this environment variable before running adapter commands:\n\nexport WORKLEDGER_JIRA_DC_ITO_TOKEN=...\n\nThen verify:\n  workledger status --adapter jira-data-center --instance ito --explain\n"
	if result.stdout != expected {
		t.Fatalf("unexpected setup stdout\nexpected:\n%s\ngot:\n%s", expected, result.stdout)
	}
}

func TestSetupClockifyUsesDefaultEnvAndBootstrapLookup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	server := newClockifyTestServer(t)
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "setup", "clockify", "--output", "json")
	if result.code != 0 {
		t.Fatalf("setup clockify failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["workspace_id"] != "ws-active" || payload["user_id"] != "user-1" {
		t.Fatalf("unexpected setup payload %s", result.stdout)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "clockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: CLOCKIFY_API_KEY\n") {
		t.Fatalf("expected bootstrapped clockify block, got %s", content)
	}
	if strings.Contains(content, "clockify-key") {
		t.Fatalf("unexpected inline secret in config: %s", content)
	}
}

func TestSetupClockifyUsesDefaultWorkspaceFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"","defaultWorkspace":"ws-default"}`))
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "setup", "clockify", "--output", "json")
	if result.code != 0 {
		t.Fatalf("setup clockify failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["workspace_id"] != "ws-default" || payload["user_id"] != "user-1" {
		t.Fatalf("unexpected setup payload %s", result.stdout)
	}
}

func TestSetupClockifyPromptsForMissingIDAfterLookupOnTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"","defaultWorkspace":""}`))
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	originalTTY := isTTYReader
	isTTYReader = func(io.Reader) bool { return true }
	defer func() { isTTYReader = originalTTY }()

	result := runCLIWithInput(t,
		"manual-workspace\n",
		"setup", "clockify",
		"--output", "json",
	)
	if result.code != 0 {
		t.Fatalf("prompted setup clockify failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["workspace_id"] != "manual-workspace" || payload["user_id"] != "user-1" {
		t.Fatalf("unexpected setup payload %s", result.stdout)
	}
}

func TestSetupClockifyFailsWithoutTTYWhenLookupIncomplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"","defaultWorkspace":""}`))
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CLOCKIFY_API_KEY", "clockify-key")

	result := runCLI(t, "setup", "clockify", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected setup clockify validation failure, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "workspace-id is required") {
		t.Fatalf("expected workspace-id validation error, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestSetupClockifyUsesCustomAPIKeyEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	server := newClockifyTestServer(t)
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()
	t.Setenv("CUSTOM_CLOCKIFY_KEY", "clockify-key")

	result := runCLI(t, "setup", "clockify", "--api-key-env", "CUSTOM_CLOCKIFY_KEY", "--output", "json")
	if result.code != 0 {
		t.Fatalf("setup clockify failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	configPath := filepath.Join(os.Getenv("HOME"), ".config", "workledger", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "api_key_env: CUSTOM_CLOCKIFY_KEY") {
		t.Fatalf("expected custom env var name in config, got %s", string(data))
	}
}

func TestConfigEnvAndSummaryJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setClockifyTestEnv(t)
	setJiraCloudTestEnv(t)
	setJiraDataTestEnv(t)
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n  project_mapping:\n    issue_prefixes:\n      AAPP: App Project\n      ORPH: Old Project\njira_cloud:\n  instances:\n    product:\n      base_url: https://cloud.example\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          reporting:\n            reporting_targets:\n              RAPP: AAPP-999\njira_data_center:\n  instances:\n    internal:\n      base_url: https://dc.example\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - DAPP\n")

	envResult := runCLI(t, "config", "env", "--output", "json")
	if envResult.code != 0 {
		t.Fatalf("config env failed: code=%d stdout=%s stderr=%s", envResult.code, envResult.stdout, envResult.stderr)
	}
	envItems := statusItems(t, envResult.stdout)
	if len(envItems) != 3 {
		t.Fatalf("expected 3 unique env vars, got %s", envResult.stdout)
	}

	summary := runCLI(t, "config", "summary", "--output", "json")
	if summary.code != 0 {
		t.Fatalf("config summary failed: code=%d stdout=%s stderr=%s", summary.code, summary.stdout, summary.stderr)
	}
	payload := decodeJSONMap(t, []byte(summary.stdout))
	if payload["jira_instance_count"] != float64(2) || payload["unique_routed_prefix_count"] != float64(3) || payload["clockify_mapping_count"] != float64(2) {
		t.Fatalf("unexpected summary payload %s", summary.stdout)
	}
	if payload["daily_minimum_quota_seconds"] != float64(28800) {
		t.Fatalf("expected default daily minimum quota in summary, got %s", summary.stdout)
	}
	if payload["day_start"] != "08:00" || payload["day_end"] != "17:00" {
		t.Fatalf("expected default workday in summary, got %s", summary.stdout)
	}
	if payload["daily_lunch"] != "12:00-12:45" {
		t.Fatalf("expected default daily lunch in summary, got %s", summary.stdout)
	}
}

func TestRoutingListAndRouteExplainJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setClockifyTestEnv(t)
	setJiraCloudTestEnv(t)
	setJiraDataTestEnv(t)
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n  project_mapping:\n    issue_prefixes:\n      AAPP: App Project\n    default_project: Default Project\njira_cloud:\n  instances:\n    product:\n      base_url: https://cloud.example\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          reporting:\n            reporting_targets:\n              RAPP: AAPP-999\njira_data_center:\n  instances:\n    internal:\n      base_url: https://dc.example\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - RAPP\n")

	list := runCLI(t, "routing", "list", "--output", "json")
	if list.code != 0 {
		t.Fatalf("routing list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	items := statusItems(t, list.stdout)
	if len(items) != 3 {
		t.Fatalf("expected 3 routing rules, got %s", list.stdout)
	}

	owned := runCLI(t, "route", "explain", "AAPP-123", "--output", "json")
	if owned.code != 0 {
		t.Fatalf("route explain failed: code=%d stdout=%s stderr=%s", owned.code, owned.stdout, owned.stderr)
	}
	ownedPayload := decodeJSONMap(t, []byte(owned.stdout))
	if ownedPayload["result"] != "owned" {
		t.Fatalf("expected owned result, got %s", owned.stdout)
	}

	ambiguous := runCLI(t, "route", "explain", "RAPP-123", "--output", "json")
	if ambiguous.code != 0 {
		t.Fatalf("route explain ambiguous failed: code=%d stdout=%s stderr=%s", ambiguous.code, ambiguous.stdout, ambiguous.stderr)
	}
	ambiguousPayload := decodeJSONMap(t, []byte(ambiguous.stdout))
	if ambiguousPayload["result"] != "ambiguous" {
		t.Fatalf("expected ambiguous result, got %s", ambiguous.stdout)
	}
}

func TestDoctorDefaultSkipsConnectivityAndReportsMissingEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY_MISSING\n")

	result := runCLI(t, "doctor", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected doctor env failure, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	items := statusItems(t, result.stdout)
	for _, item := range items {
		if item["category"] == "connectivity" {
			t.Fatalf("did not expect connectivity item in bare doctor: %s", result.stdout)
		}
	}
}

func TestDoctorReportsWritableLocalStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "doctor", "--output", "json")
	if result.code != 0 {
		t.Fatalf("doctor failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	items := statusItems(t, result.stdout)
	found := false
	for _, item := range items {
		if item["category"] == "local" && item["target"] == "storage" {
			found = true
			if item["status"] != "ok" {
				t.Fatalf("expected writable local storage, got %v", item)
			}
		}
	}
	if !found {
		t.Fatalf("expected local storage doctor item, got %s", result.stdout)
	}
}

func TestDoctorReportsUnwritableLocalStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	setReadOnly(t, filepath.Dir(sqlitePath), 0o500)

	result := runCLI(t, "doctor", "--output", "json")
	if result.code != 1 {
		t.Fatalf("expected storage failure, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	items := statusItems(t, result.stdout)
	for _, item := range items {
		if item["category"] == "local" && item["target"] == "storage" {
			if item["status"] != "error" || !strings.Contains(item["message"].(string), "cannot write local ledger") {
				t.Fatalf("unexpected storage item %v", item)
			}
			return
		}
	}
	t.Fatalf("expected local storage error item, got %s", result.stdout)
}

func TestWorklogsAddReturnsLocalStorageNotWritable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	setReadOnly(t, sqlitePath, 0o400)

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Blocked write", "--output", "json")
	if result.code != 1 {
		t.Fatalf("expected local storage failure, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["reason"] != "local_storage_not_writable" {
		t.Fatalf("expected local_storage_not_writable, got %s", result.stdout)
	}
	if payload["sqlite_path"] != sqlitePath {
		t.Fatalf("expected sqlite path %s, got %s", sqlitePath, result.stdout)
	}
	if payload["parent_dir"] != filepath.Dir(sqlitePath) {
		t.Fatalf("expected parent dir %s, got %s", filepath.Dir(sqlitePath), result.stdout)
	}
	if payload["operation"] != "worklogs add" {
		t.Fatalf("expected operation worklogs add, got %s", result.stdout)
	}
}

func TestWorklogsAddDryJSONPreviewsAndDoesNotPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "  Investigated \n bug  ", "--dry", "--output", "json")
	if result.code != 0 {
		t.Fatalf("dry add failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["dry_run"] != true {
		t.Fatalf("expected dry_run=true, got %s", result.stdout)
	}
	record, ok := payload["record"].(map[string]any)
	if !ok {
		t.Fatalf("expected record object, got %s", result.stdout)
	}
	if _, exists := record["id"]; exists {
		t.Fatalf("expected preview record without id, got %s", result.stdout)
	}
	if record["issue_key"] != "ABC-123" {
		t.Fatalf("unexpected issue key: %s", result.stdout)
	}
	if record["started_at"] != "2026-05-03T09:00:00Z" || record["started_at_utc"] != "2026-05-03T09:00:00Z" {
		t.Fatalf("unexpected started fields: %s", result.stdout)
	}
	if record["duration_seconds"] != float64(3600) {
		t.Fatalf("unexpected duration: %s", result.stdout)
	}
	if record["description"] != "Investigated bug" {
		t.Fatalf("unexpected description: %s", result.stdout)
	}
	if countActiveCLIWorklogs(t) != 0 {
		t.Fatalf("expected dry add to leave storage unchanged")
	}
}

func TestWorklogsAddDryTableOmitsID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "Preview row", "--dry")
	if result.code != 0 {
		t.Fatalf("dry add table failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and one row, got %q", result.stdout)
	}
	headers := strings.Fields(lines[0])
	expected := []string{"ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}
	if !reflect.DeepEqual(headers, expected) {
		t.Fatalf("unexpected headers %v in %q", headers, result.stdout)
	}
	if strings.Contains(lines[0], "ID") {
		t.Fatalf("expected no ID column, got %q", result.stdout)
	}
	if !strings.Contains(result.stdout, "2026-05-03 - 09:00 - 10:00") {
		t.Fatalf("expected localized window in %q", result.stdout)
	}
	if !strings.Contains(result.stdout, "60m") {
		t.Fatalf("expected duration in minutes in %q", result.stdout)
	}
}

func TestWorklogsListTableUsesLocalizedWindowWhileJSONStaysPrecise(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sqlitePath := filepath.Join(t.TempDir(), "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: Europe/Vilnius\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n")

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "30m", "--description", "Localized row", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	table := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03")
	if table.code != 0 {
		t.Fatalf("list table failed: code=%d stdout=%s stderr=%s", table.code, table.stdout, table.stderr)
	}
	if !strings.Contains(table.stdout, "WINDOW") || !strings.Contains(table.stdout, "DURATION") {
		t.Fatalf("expected WINDOW and DURATION headers, stdout=%q", table.stdout)
	}
	if !strings.Contains(table.stdout, "2026-05-03 - 09:30 - 10:00") {
		t.Fatalf("expected localized window, stdout=%q", table.stdout)
	}
	if !strings.Contains(table.stdout, "30m") {
		t.Fatalf("expected minute duration, stdout=%q", table.stdout)
	}
	if strings.Contains(table.stdout, "2026-05-03T09:30:00+03:00") {
		t.Fatalf("expected table output without RFC3339 timestamp, stdout=%q", table.stdout)
	}

	jsonResult := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "issue_key,started_at,started_at_utc,duration_seconds", "--output", "json")
	if jsonResult.code != 0 {
		t.Fatalf("list json failed: code=%d stdout=%s stderr=%s", jsonResult.code, jsonResult.stdout, jsonResult.stderr)
	}
	payload := decodeJSONMap(t, []byte(jsonResult.stdout))
	item := payload["items"].([]any)[0].(map[string]any)
	if item["started_at"] != "2026-05-03T09:30:00+03:00" {
		t.Fatalf("expected precise local started_at, got %#v", item)
	}
	if item["started_at_utc"] != "2026-05-03T06:30:00Z" {
		t.Fatalf("expected precise UTC started_at_utc, got %#v", item)
	}
	if item["duration_seconds"] != float64(1800) {
		t.Fatalf("expected precise duration_seconds, got %#v", item)
	}
}

func TestWorklogsShiftDryTableUsesWindowColumns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	dry := runCLI(t, "worklogs", "shift", "--issue", "ABC-123", "--by", "15m", "--dry")
	if dry.code != 0 {
		t.Fatalf("shift dry table failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	if !strings.Contains(dry.stdout, "WINDOW_BEFORE") || !strings.Contains(dry.stdout, "WINDOW_AFTER") {
		t.Fatalf("expected window headers, stdout=%q", dry.stdout)
	}
	if !strings.Contains(dry.stdout, "2026-05-03 - 06:00 - 07:00") || !strings.Contains(dry.stdout, "2026-05-03 - 06:15 - 07:15") {
		t.Fatalf("expected localized before/after windows, stdout=%q", dry.stdout)
	}
	if !strings.Contains(dry.stdout, "60m") {
		t.Fatalf("expected minute duration, stdout=%q", dry.stdout)
	}
}

func TestWorklogsAddDryConflictLeavesRowsUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("seed add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	conflict := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "1h", "--description", "Overlap", "--dry", "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected dry conflict exit 2, got code=%d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}
	if countActiveCLIWorklogs(t) != 1 {
		t.Fatalf("expected conflicting dry add to leave row count unchanged")
	}
}

func TestWorklogsAddDryForceSucceedsWithoutPersisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("seed add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "1h", "--description", "Overlap", "--dry", "--force", "--output", "json")
	if result.code != 0 {
		t.Fatalf("forced dry add failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if countActiveCLIWorklogs(t) != 1 {
		t.Fatalf("expected forced dry add to leave row count unchanged")
	}
}

func TestWorklogsAddDrySkipsLocalStorageWritabilityPrecheck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	setReadOnly(t, sqlitePath, 0o400)

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Preview", "--dry", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected dry add to succeed on read-only db, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["dry_run"] != true {
		t.Fatalf("expected dry preview payload, got %s", result.stdout)
	}
	if countActiveCLIWorklogs(t) != 0 {
		t.Fatalf("expected dry add on read-only db to leave storage unchanged")
	}
}

func TestWorklogsAddRejectsMissingPlacementAndSnapOnlyFlagsWithoutSnap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	missingPlacement := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--duration", "1h", "--description", "No placement", "--output", "json")
	if missingPlacement.code != 2 {
		t.Fatalf("expected validation exit 2, got code=%d stdout=%s stderr=%s", missingPlacement.code, missingPlacement.stdout, missingPlacement.stderr)
	}

	snapOnly := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--today", "--duration", "1h", "--description", "Bad flags", "--output", "json")
	if snapOnly.code != 2 {
		t.Fatalf("expected validation exit 2, got code=%d stdout=%s stderr=%s", snapOnly.code, snapOnly.stdout, snapOnly.stderr)
	}

	weekOffsetSnapOnly := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--mon", "--week-offset", "-1", "--duration", "1h", "--description", "Bad flags", "--output", "json")
	if weekOffsetSnapOnly.code != 2 {
		t.Fatalf("expected validation exit 2, got code=%d stdout=%s stderr=%s", weekOffsetSnapOnly.code, weekOffsetSnapOnly.stdout, weekOffsetSnapOnly.stderr)
	}
	if !strings.Contains(weekOffsetSnapOnly.stdout, "date-window and workday flags require snap") {
		t.Fatalf("expected snap-only validation failure, stdout=%s stderr=%s", weekOffsetSnapOnly.stdout, weekOffsetSnapOnly.stderr)
	}
}

func TestWorklogsAddSnapDryJSONReturnsSplitRecords(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sqlitePath := filepath.Join(t.TempDir(), "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  day_start: 09:00\n  day_end: 17:00\n  daily_lunch: 12:00-12:45\n")

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	seed := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T09:00:00Z", "--duration", "2h", "--description", "Morning", "--output", "json")
	if seed.code != 0 {
		t.Fatalf("seed add failed: code=%d stdout=%s stderr=%s", seed.code, seed.stdout, seed.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--snap", "--from", "2026-05-03", "--to", "2026-05-03", "--duration", "2h", "--description", "Snapped", "--dry", "--output", "json")
	if result.code != 0 {
		t.Fatalf("snap dry add failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["dry_run"] != true {
		t.Fatalf("expected dry_run in %s", result.stdout)
	}
	records, ok := payload["records"].([]any)
	if !ok || len(records) != 2 {
		t.Fatalf("expected two preview records, got %s", result.stdout)
	}
	first := records[0].(map[string]any)
	second := records[1].(map[string]any)
	if first["started_at_utc"] != "2026-05-03T11:00:00Z" || first["duration_seconds"] != float64(3600) {
		t.Fatalf("unexpected first record %v", first)
	}
	if second["started_at_utc"] != "2026-05-03T12:45:00Z" || second["duration_seconds"] != float64(3600) {
		t.Fatalf("unexpected second record %v", second)
	}
}

func TestWorklogsAddSnapSupportsWeekOffset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--snap", "--mon", "--week-offset", "-1", "--duration", "1h", "--description", "Snapped", "--dry", "--output", "json")
	if result.code != 0 {
		t.Fatalf("snap dry add failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	records := payload["records"].([]any)
	first := records[0].(map[string]any)
	if !strings.HasPrefix(first["started_at_utc"].(string), "2026-05-18T") {
		t.Fatalf("expected previous-week monday placement, got %s", result.stdout)
	}
}

func TestWorklogsAddSnapTableWritesWarningsToStderr(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sqlitePath := filepath.Join(t.TempDir(), "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  day_start: 09:00\n  day_end: 17:00\n  daily_lunch: 12:00-12:45\n")

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	seed := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T09:00:00Z", "--duration", "7h", "--description", "Busy", "--output", "json")
	if seed.code != 0 {
		t.Fatalf("seed add failed: code=%d stdout=%s stderr=%s", seed.code, seed.stdout, seed.stderr)
	}

	result := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--snap", "--from", "2026-05-03", "--to", "2026-05-03", "--duration", "2h", "--description", "Overflow")
	if result.code != 0 {
		t.Fatalf("snap add failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "day_end boundary reached; snapped worklog extends past the effective workday end") {
		t.Fatalf("expected warning on stderr, got %q", result.stderr)
	}
	if !strings.Contains(result.stdout, "2026-05-03 - 16:00 - 18:00") {
		t.Fatalf("expected overflow row in stdout, got %q", result.stdout)
	}
}

func TestClockifyMappingsValidateWarningsAndMissingProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setClockifyTestEnv(t)
	setJiraCloudTestEnv(t)
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n  project_mapping:\n    issue_prefixes:\n      AAPP: App Project\n      ORPH: Old Project\njira_cloud:\n  instances:\n    product:\n      base_url: https://cloud.example\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n              - BAPP\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/ws-active/projects":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[{"id":"1","name":"App Project"},{"id":"2","name":"Old Project"}]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "clockify", "mappings", "validate", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected warning-only success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	items := statusItems(t, result.stdout)
	if len(items) != 4 {
		t.Fatalf("expected 4 mapping rows, got %s", result.stdout)
	}

	missingProjectConfig := "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n  project_mapping:\n    issue_prefixes:\n      AAPP: Missing Project\njira_cloud:\n  instances:\n    product:\n      base_url: https://cloud.example\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n"
	writeConfigContent(t, missingProjectConfig)
	missing := runCLI(t, "clockify", "mappings", "validate", "--output", "json")
	if missing.code != 3 {
		t.Fatalf("expected missing project exit 3, got code=%d stdout=%s stderr=%s", missing.code, missing.stdout, missing.stderr)
	}
}

func TestAuthCommandsFailWhenSecretEnvIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndMissingClockifySecretEnv(t)

	init := runCLI(t, "init", "--output", "json")
	if init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	cases := [][]string{
		{"status", "--output", "json"},
		{"totals", "--today", "--output", "json"},
		{"plan", "reconcile", "--pull", "--adapter", "clockify", "--today", "--output", "json"},
	}

	for _, args := range cases {
		result := runCLI(t, args...)
		if result.code != 2 {
			t.Fatalf("expected validation error for %v, got code=%d stdout=%s stderr=%s", args, result.code, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, "clockify.auth.api_key_env") && !strings.Contains(result.stderr, "clockify.auth.api_key_env") {
			t.Fatalf("expected missing env field in output for %v, got stdout=%s stderr=%s", args, result.stdout, result.stderr)
		}
	}
}

func TestConfigValidateAllowsUnsetOptionalAdapterSecretEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY_MISSING\n\njira_cloud:\n  instances:\n    product:\n      base_url: https://example.atlassian.net\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN_MISSING\n\njira_data_center:\n  instances:\n    internal:\n      base_url: https://jira.example.com\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN_MISSING\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	validate := runCLI(t, "config", "validate", "--output", "json")
	if validate.code != 0 {
		t.Fatalf("validate failed: code=%d stdout=%s stderr=%s", validate.code, validate.stdout, validate.stderr)
	}

	init := runCLI(t, "init", "--output", "json")
	if init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--today", "--output", "json")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
}

func TestStatusBareJSONReturnsNormalizedRowsAcrossConfiguredFamilies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clockifyServer := newClockifyTestServer(t)
	defer clockifyServer.Close()
	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{})
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndAllStatusFamilies(t, jiraCloudServer.URL, jiraDataServer.URL)

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	status := runCLI(t, "status", "--output", "json")
	if status.code != 0 {
		t.Fatalf("status failed: code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}

	items := statusItems(t, status.stdout)
	if len(items) != 3 {
		t.Fatalf("expected 3 status items, got %s", status.stdout)
	}

	if items[0]["adapter"] != "clockify" || items[0]["instance"] != "clockify" || items[0]["status"] != "OK" || items[0]["workspace_id"] != "ws-active" || items[0]["user_id"] != "user-1" || items[0]["base_url"] != nil || items[0]["user"] != nil {
		t.Fatalf("unexpected clockify item %v", items[0])
	}
	if items[1]["adapter"] != "jira-cloud" || items[1]["instance"] != "product" || items[1]["status"] != "OK" || items[1]["base_url"] != jiraCloudServer.URL || items[1]["user"] != "User One" || items[1]["workspace_id"] != nil || items[1]["user_id"] != nil {
		t.Fatalf("unexpected jira cloud item %v", items[1])
	}
	if items[2]["adapter"] != "jira-data-center" || items[2]["instance"] != "internal" || items[2]["status"] != "OK" || items[2]["base_url"] != jiraDataServer.URL || items[2]["user"] != "User One" || items[2]["workspace_id"] != nil || items[2]["user_id"] != nil {
		t.Fatalf("unexpected jira data item %v", items[2])
	}
}

func TestStatusBareSkipsUnconfiguredFamilies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{})
	defer server.Close()
	writeConfigWithUTCAndStatusAdapters(t, false, map[string]string{"product": server.URL}, nil)

	status := runCLI(t, "status", "--output", "json")
	if status.code != 0 {
		t.Fatalf("status failed: code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}

	items := statusItems(t, status.stdout)
	if len(items) != 1 || items[0]["adapter"] != "jira-cloud" || items[0]["instance"] != "product" {
		t.Fatalf("unexpected filtered bare status %s", status.stdout)
	}
}

func TestStatusBareContinuesRenderingWhenOneAdapterFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := newClockifyTestServer(t)
	defer clockifyServer.Close()
	jiraCloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndAllStatusFamilies(t, jiraCloudServer.URL, jiraDataServer.URL)

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	status := runCLI(t, "status", "--output", "json")
	if status.code != 4 {
		t.Fatalf("expected auth exit 4 with rendered output, got code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}

	items := statusItems(t, status.stdout)
	if len(items) != 3 {
		t.Fatalf("expected 3 status items, got %s", status.stdout)
	}
	if items[0]["adapter"] != "clockify" || items[0]["status"] != "OK" {
		t.Fatalf("unexpected clockify item %v", items[0])
	}
	if items[1]["adapter"] != "jira-cloud" || items[1]["instance"] != "product" || items[1]["status"] != "jira cloud request failed: 401 Unauthorized" || items[1]["base_url"] != jiraCloudServer.URL || items[1]["user"] != nil {
		t.Fatalf("unexpected failed jira cloud item %v", items[1])
	}
	if items[2]["adapter"] != "jira-data-center" || items[2]["status"] != "OK" {
		t.Fatalf("unexpected jira data item %v", items[2])
	}
}

func TestStatusReturnsEmptyItemsWhenNoAdaptersConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	status := runCLI(t, "status", "--output", "json")
	if status.code != 0 {
		t.Fatalf("status failed: code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}

	items := statusItems(t, status.stdout)
	if len(items) != 0 {
		t.Fatalf("expected empty items, got %s", status.stdout)
	}
}

func TestStatusAdapterFiltersAllowZeroConfiguredTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	for _, adapter := range []string{"clockify", "jira-cloud", "jira-data-center"} {
		status := runCLI(t, "status", "--adapter", adapter, "--output", "json")
		if status.code != 0 {
			t.Fatalf("status failed for %s: code=%d stdout=%s stderr=%s", adapter, status.code, status.stdout, status.stderr)
		}
		items := statusItems(t, status.stdout)
		if len(items) != 0 {
			t.Fatalf("expected empty items for %s, got %s", adapter, status.stdout)
		}
	}
}

func TestStatusClockifyValidationFailureAndUnsupportedAdapter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	unsupported := runCLI(t, "status", "--adapter", "jira_cloud", "--output", "json")
	if unsupported.code != 2 {
		t.Fatalf("expected unsupported adapter validation, got code=%d stdout=%s stderr=%s", unsupported.code, unsupported.stdout, unsupported.stderr)
	}
	if !strings.Contains(unsupported.stdout, "supported adapters are clockify, jira-cloud, and jira-data-center") &&
		!strings.Contains(unsupported.stderr, "supported adapters are clockify, jira-cloud, and jira-data-center") {
		t.Fatalf("unexpected unsupported adapter message stdout=%s stderr=%s", unsupported.stdout, unsupported.stderr)
	}

	writeBrokenClockifyConfig(t)
	failed := runCLI(t, "status", "--adapter", "clockify", "--output", "json")
	if failed.code != 2 {
		t.Fatalf("expected validation failure, got code=%d stdout=%s stderr=%s", failed.code, failed.stdout, failed.stderr)
	}
	if !strings.Contains(failed.stdout, "config validation failed") && !strings.Contains(failed.stderr, "config validation failed") {
		t.Fatalf("expected config validation message, got stdout=%s stderr=%s", failed.stdout, failed.stderr)
	}
}

func TestStatusJiraRowsAreSortedByInstanceName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{})
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndStatusAdapters(t, false, map[string]string{"zeta": jiraCloudServer.URL, "alpha": jiraCloudServer.URL}, map[string]string{"ops": jiraDataServer.URL, "core": jiraDataServer.URL})

	jiraCloudStatus := runCLI(t, "status", "--adapter", "jira-cloud", "--output", "json")
	if jiraCloudStatus.code != 0 {
		t.Fatalf("jira cloud status failed: code=%d stdout=%s stderr=%s", jiraCloudStatus.code, jiraCloudStatus.stdout, jiraCloudStatus.stderr)
	}
	jiraCloudItems := statusItems(t, jiraCloudStatus.stdout)
	if len(jiraCloudItems) != 2 || jiraCloudItems[0]["instance"] != "alpha" || jiraCloudItems[1]["instance"] != "zeta" {
		t.Fatalf("expected sorted jira cloud instances, got %s", jiraCloudStatus.stdout)
	}

	jiraDataStatus := runCLI(t, "status", "--adapter", "jira-data-center", "--output", "json")
	if jiraDataStatus.code != 0 {
		t.Fatalf("jira data status failed: code=%d stdout=%s stderr=%s", jiraDataStatus.code, jiraDataStatus.stdout, jiraDataStatus.stderr)
	}
	jiraDataItems := statusItems(t, jiraDataStatus.stdout)
	if len(jiraDataItems) != 2 || jiraDataItems[0]["instance"] != "core" || jiraDataItems[1]["instance"] != "ops" {
		t.Fatalf("expected sorted jira data instances, got %s", jiraDataStatus.stdout)
	}
}

func TestStatusJiraAdapterAcceptsInstanceFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{})
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndStatusAdapters(t, false, map[string]string{"zeta": jiraCloudServer.URL, "alpha": jiraCloudServer.URL}, map[string]string{"ops": jiraDataServer.URL, "core": jiraDataServer.URL})

	jiraCloudStatus := runCLI(t, "status", "--adapter", "jira-cloud", "--instance", "zeta", "--output", "json")
	if jiraCloudStatus.code != 0 {
		t.Fatalf("jira cloud status failed: code=%d stdout=%s stderr=%s", jiraCloudStatus.code, jiraCloudStatus.stdout, jiraCloudStatus.stderr)
	}
	jiraCloudItems := statusItems(t, jiraCloudStatus.stdout)
	if len(jiraCloudItems) != 1 || jiraCloudItems[0]["instance"] != "zeta" {
		t.Fatalf("expected one filtered jira cloud instance, got %s", jiraCloudStatus.stdout)
	}

	jiraDataStatus := runCLI(t, "status", "--adapter", "jira-data-center", "--instance", "ops", "--output", "json")
	if jiraDataStatus.code != 0 {
		t.Fatalf("jira data status failed: code=%d stdout=%s stderr=%s", jiraDataStatus.code, jiraDataStatus.stdout, jiraDataStatus.stderr)
	}
	jiraDataItems := statusItems(t, jiraDataStatus.stdout)
	if len(jiraDataItems) != 1 || jiraDataItems[0]["instance"] != "ops" {
		t.Fatalf("expected one filtered jira data instance, got %s", jiraDataStatus.stdout)
	}
}

func TestStatusClockifyAcceptsImplicitInstanceName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := newClockifyTestServer(t)
	defer clockifyServer.Close()
	writeConfigWithUTCAndClockify(t)

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	status := runCLI(t, "status", "--adapter", "clockify", "--instance", "clockify", "--output", "json")
	if status.code != 0 {
		t.Fatalf("clockify status failed: code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}
	items := statusItems(t, status.stdout)
	if len(items) != 1 || items[0]["instance"] != "clockify" || items[0]["status"] != "OK" {
		t.Fatalf("expected one filtered clockify instance, got %s", status.stdout)
	}
}

func TestStatusClockifyFailsWhenTagsReadFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[]`))
		case "/v1/workspaces/ws-active/tags":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	status := runCLI(t, "status", "--adapter", "clockify", "--output", "json")
	if status.code != 4 {
		t.Fatalf("expected auth failure, got code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}
	if !strings.Contains(status.stdout, "401 Unauthorized") && !strings.Contains(status.stderr, "401 Unauthorized") {
		t.Fatalf("expected tags auth failure, got stdout=%s stderr=%s", status.stdout, status.stderr)
	}
}

func TestStatusTableHeaders(t *testing.T) {
	headers := statusTableHeaders()
	expected := []string{"ADAPTER", "INSTANCE", "BASE_URL", "USER", "STATUS"}
	if len(headers) != len(expected) {
		t.Fatalf("unexpected header count %v", headers)
	}
	for i := range expected {
		if headers[i] != expected[i] {
			t.Fatalf("unexpected headers %v", headers)
		}
	}
}

func TestStatusTableRowsMapClockifyFields(t *testing.T) {
	rows := statusTableRows([]statusRow{
		{
			Adapter:     "clockify",
			Instance:    "clockify",
			Status:      "OK",
			WorkspaceID: "ws-active",
			UserID:      "user-1",
		},
		{
			Adapter:  "jira-cloud",
			Instance: "product",
			Status:   "jira cloud request failed: 401 Unauthorized",
			BaseURL:  "https://example.atlassian.net",
			User:     "User One",
		},
	})

	expected := [][]string{
		{"clockify", "clockify", "", "user-1", "OK"},
		{"jira-cloud", "product", "https://example.atlassian.net", "User One", "jira cloud request failed: 401 Unauthorized"},
	}
	if len(rows) != len(expected) {
		t.Fatalf("unexpected row count %v", rows)
	}
	for i := range expected {
		for j := range expected[i] {
			if rows[i][j] != expected[i][j] {
				t.Fatalf("unexpected rows %v", rows)
			}
		}
	}
}

func TestTotalsClockifyMatchAcrossMidnight(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Night work","timeInterval":{"start":"2026-04-01T23:30:00Z","end":"2026-04-02T01:30:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-01T23:30:00Z", "--duration", "2h", "--description", "Night work", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected summary %s", totals.stdout)
	}
	days := payload["days"].([]any)
	if len(days) != 2 {
		t.Fatalf("expected 2 day rows, got %s", totals.stdout)
	}
	firstDay := days[0].(map[string]any)
	secondDay := days[1].(map[string]any)
	if firstDay["date"] != "2026-04-01" || firstDay["local_total_seconds"].(float64) != 1800 || firstDay["remote_total_seconds"].(float64) != 1800 {
		t.Fatalf("unexpected first day %s", totals.stdout)
	}
	if secondDay["date"] != "2026-04-02" || secondDay["local_total_seconds"].(float64) != 5400 || secondDay["remote_total_seconds"].(float64) != 5400 {
		t.Fatalf("unexpected second day %s", totals.stdout)
	}

	withInstance := runCLI(t, "totals", "--adapter", "clockify", "--instance", "clockify", "--from", "2026-04-01", "--to", "2026-04-02", "--output", "json")
	if withInstance.code != 0 {
		t.Fatalf("totals with clockify instance failed: code=%d stdout=%s stderr=%s", withInstance.code, withInstance.stdout, withInstance.stderr)
	}
	withInstancePayload := decodeJSONMap(t, []byte(withInstance.stdout))
	effectiveFilters := withInstancePayload["filters"].(map[string]any)["effective"].(map[string]any)
	if effectiveFilters["instance"] != "clockify" {
		t.Fatalf("expected resolved clockify instance in filters, got %s", withInstance.stdout)
	}
}

func TestTotalsBareReturnsConfiguredAdapterItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	jiraCloudZetaServer := newJiraCloudTestServer(t, map[string][]string{
		"ZAPP-100": {
			`{"id":"wl-zeta","started":"2026-04-02T10:00:00.000+0000","timeSpentSeconds":3600,"comment":"Zeta cloud","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudZetaServer.Close()
	jiraCloudAlphaServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-100": {
			`{"id":"wl-alpha","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Alpha cloud","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudAlphaServer.Close()
	jiraDataOpsServer := newJiraDataTotalsTestServer(t, map[string][]string{
		"OPS-200": {
			`{"id":"wl-ops","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":3600,"comment":"Ops data","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer jiraDataOpsServer.Close()
	jiraDataCoreServer := newJiraDataTotalsTestServer(t, map[string][]string{
		"COR-200": {
			`{"id":"wl-core","started":"2026-04-02T14:00:00.000+0000","timeSpentSeconds":3600,"comment":"Core data","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer jiraDataCoreServer.Close()
	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"zeta": jiraCloudZetaServer.URL, "alpha": jiraCloudAlphaServer.URL},
		map[string]string{"ops": jiraDataOpsServer.URL, "core": jiraDataCoreServer.URL},
		map[string]string{
			"zeta":  "ZAPP",
			"alpha": "AAPP",
		},
		map[string]string{
			"ops":  "OPS",
			"core": "COR",
		},
	)

	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Clockify work","timeInterval":{"start":"2026-04-02T06:00:00Z","end":"2026-04-02T07:00:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	for _, args := range [][]string{
		{"worklogs", "add", "--issue", "AAPP-100", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Alpha cloud", "--output", "json"},
		{"worklogs", "add", "--issue", "ZAPP-100", "--started-utc", "2026-04-02T10:00:00Z", "--duration", "1h", "--description", "Zeta cloud", "--output", "json"},
		{"worklogs", "add", "--issue", "OPS-200", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "1h", "--description", "Ops data", "--output", "json"},
		{"worklogs", "add", "--issue", "COR-200", "--started-utc", "2026-04-02T14:00:00Z", "--duration", "1h", "--description", "Core data", "--output", "json"},
		{"worklogs", "add", "--issue", "LOC-999", "--started-utc", "2026-04-02T16:00:00Z", "--duration", "1h", "--description", "Not routed", "--output", "json"},
	} {
		add := runCLI(t, args...)
		if add.code != 0 {
			t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
		}
	}

	totals := runCLI(t, "totals", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	items := totalsItems(t, totals.stdout)
	if len(items) != 5 {
		t.Fatalf("expected 5 totals items, got %s", totals.stdout)
	}
	expected := []struct {
		adapter  string
		instance any
	}{
		{"clockify", "clockify"},
		{"jira-cloud", "alpha"},
		{"jira-cloud", "zeta"},
		{"jira-data-center", "core"},
		{"jira-data-center", "ops"},
	}
	for i, want := range expected {
		if items[i]["adapter"] != want.adapter || items[i]["instance"] != want.instance {
			t.Fatalf("unexpected item order at %d: %v", i, items[i])
		}
		if items[i]["from"] != "2026-04-02T00:00:00Z" || items[i]["to"] != "2026-04-02T23:59:59Z" || items[i]["timezone"] != "UTC" {
			t.Fatalf("unexpected window metadata %v", items[i])
		}
		if items[i]["summary"] == nil {
			t.Fatalf("expected summary for item %v", items[i])
		}
	}
}

func TestTotalsBareContinuesRenderingWhenOneAdapterFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	jiraCloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Fatalf("unexpected request path %s", r.URL.Path)
	}))
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{
		"OPS-200": {
			`{"id":"wl-ops","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":3600,"comment":"Ops data","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer jiraDataServer.Close()
	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"product": jiraCloudServer.URL},
		map[string]string{"ops": jiraDataServer.URL},
		map[string]string{"product": "AAPP"},
		map[string]string{"ops": "OPS"},
	)

	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Clockify work","timeInterval":{"start":"2026-04-02T06:00:00Z","end":"2026-04-02T07:00:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	for _, args := range [][]string{
		{"worklogs", "add", "--issue", "AAPP-100", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Cloud work", "--output", "json"},
		{"worklogs", "add", "--issue", "OPS-200", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "1h", "--description", "Ops data", "--output", "json"},
	} {
		add := runCLI(t, args...)
		if add.code != 0 {
			t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
		}
	}

	totals := runCLI(t, "totals", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if totals.code != 4 {
		t.Fatalf("expected auth exit 4, got code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	items := totalsItems(t, totals.stdout)
	if len(items) != 3 {
		t.Fatalf("expected 3 totals items, got %s", totals.stdout)
	}
	if items[0]["adapter"] != "clockify" || items[0]["summary"] == nil {
		t.Fatalf("expected successful clockify item, got %v", items[0])
	}
	if items[1]["adapter"] != "jira-cloud" || items[1]["instance"] != "product" || items[1]["status"] != "auth_error" {
		t.Fatalf("expected jira cloud auth failure item, got %v", items[1])
	}
	if items[2]["adapter"] != "jira-data-center" || items[2]["summary"] == nil {
		t.Fatalf("expected successful jira data item, got %v", items[2])
	}
}

func TestTotalsBareTableFlattensMultilineErrorsIntoSingleRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	jiraCloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"user-1","displayName":"User One"}`))
		case "/rest/api/3/search/jql":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("{\n    \"errorMessages\": [\n        \"You're unable to access content because your IP address is not listed in the IP allowlist. Contact your admin for help.\"\n    ],\n    \"errors\": {}\n}"))
			return
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer jiraCloudServer.Close()
	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"product": jiraCloudServer.URL},
		nil,
		map[string]string{"product": "AAPP"},
		nil,
	)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-100", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Cloud work", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--from", "2026-04-02", "--to", "2026-04-02")
	if totals.code != 4 {
		t.Fatalf("expected auth exit 4, got code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	if strings.Count(totals.stdout, "\n") != 3 {
		t.Fatalf("expected header plus two data rows, stdout=%q", totals.stdout)
	}
	if !strings.Contains(totals.stdout, "auth_error") {
		t.Fatalf("expected auth_error state, stdout=%q", totals.stdout)
	}
	if !regexp.MustCompile(`jira-cloud\s+product\s+\S+\s+\S+\s+UTC\s+1h\s+auth_error`).MatchString(totals.stdout) {
		t.Fatalf("expected failed jira-cloud row to keep local total, stdout=%q", totals.stdout)
	}
	if strings.Contains(totals.stdout, "\"errorMessages\": [\n") {
		t.Fatalf("expected multiline error body to be flattened, stdout=%q", totals.stdout)
	}
	if !strings.Contains(totals.stdout, "jira cloud request failed: 403 Forbidden: You're unable to access content because your IP address is not listed in the IP allowlist. Contact your admin for help.") {
		t.Fatalf("expected flattened error message, stdout=%q", totals.stdout)
	}
}

func TestTotalsBareReturnsEmptyItemsWhenNoAdaptersConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	totals := runCLI(t, "totals", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	if len(totalsItems(t, totals.stdout)) != 0 {
		t.Fatalf("expected empty items, got %s", totals.stdout)
	}
}

func TestTotalsBareTableShowsClockifyImplicitInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndBareTotalsAdapters(t, nil, nil, nil, nil)

	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer clockifyServer.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	totals := runCLI(t, "totals", "--from", "2026-04-02", "--to", "2026-04-02")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	if !strings.Contains(totals.stdout, "clockify") {
		t.Fatalf("expected clockify instance in table output, stdout=%q", totals.stdout)
	}
}

func TestTotalsUnsupportedAdapterValidationMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	invalid := runCLI(t, "totals", "--adapter", "jira_cloud", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if invalid.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", invalid.code, invalid.stdout, invalid.stderr)
	}
	if !strings.Contains(invalid.stdout, "supported adapters are clockify, jira-cloud, and jira-data-center") &&
		!strings.Contains(invalid.stderr, "supported adapters are clockify, jira-cloud, and jira-data-center") {
		t.Fatalf("expected supported adapters message, got stdout=%s stderr=%s", invalid.stdout, invalid.stderr)
	}
}

func TestTotalsClockifyMismatchRunningAndAuthFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Mismatch","timeInterval":{"start":"2026-04-02T08:00:00Z","end":"2026-04-02T09:00:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "2h", "--description", "Mismatch", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	mismatch := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if mismatch.code != 0 {
		t.Fatalf("mismatch totals failed: code=%d stdout=%s stderr=%s", mismatch.code, mismatch.stdout, mismatch.stderr)
	}
	mismatchPayload := decodeJSONMap(t, []byte(mismatch.stdout))
	mismatchSummary := mismatchPayload["summary"].(map[string]any)
	if mismatchSummary["state"] != "mismatch" || mismatchSummary["delta_seconds"].(float64) != 3600 {
		t.Fatalf("unexpected mismatch summary %s", mismatch.stdout)
	}

	runningServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"running-1","description":"Running","timeInterval":{"start":"2026-04-02T08:00:00Z","end":""}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer runningServer.Close()

	clockifyadapter.BaseURL = runningServer.URL
	indeterminate := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if indeterminate.code != 0 {
		t.Fatalf("indeterminate totals failed: code=%d stdout=%s stderr=%s", indeterminate.code, indeterminate.stdout, indeterminate.stderr)
	}
	indeterminatePayload := decodeJSONMap(t, []byte(indeterminate.stdout))
	indeterminateSummary := indeterminatePayload["summary"].(map[string]any)
	if indeterminateSummary["state"] != "indeterminate" || indeterminateSummary["running_remote_entry_detected"] != true {
		t.Fatalf("unexpected indeterminate summary %s", indeterminate.stdout)
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer authServer.Close()

	clockifyadapter.BaseURL = authServer.URL
	auth := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if auth.code != 4 {
		t.Fatalf("expected auth failure, got code=%d stdout=%s stderr=%s", auth.code, auth.stdout, auth.stderr)
	}
}

func TestTotalsClockifyTableCompactByDefaultAndDetailedWhenRequested(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Night work","timeInterval":{"start":"2026-04-01T23:30:00Z","end":"2026-04-02T01:30:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-01T23:30:00Z", "--duration", "2h", "--description", "Night work", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	compact := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-02")
	if compact.code != 0 {
		t.Fatalf("compact totals failed: code=%d stdout=%s stderr=%s", compact.code, compact.stdout, compact.stderr)
	}
	if !strings.Contains(compact.stdout, "TOTAL") {
		t.Fatalf("expected TOTAL row, stdout=%q", compact.stdout)
	}
	if strings.Contains(compact.stdout, "\n2026-04-01") || strings.Contains(compact.stdout, "\n2026-04-02") {
		t.Fatalf("expected compact output without day rows, stdout=%q", compact.stdout)
	}
	if !strings.Contains(compact.stdout, "adapter=clockify from=2026-04-01T00:00:00Z to=2026-04-02T23:59:59Z timezone=UTC instance=clockify state=match") {
		t.Fatalf("expected totals footer, stdout=%q", compact.stdout)
	}

	detailed := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-02", "--details")
	if detailed.code != 0 {
		t.Fatalf("detailed totals failed: code=%d stdout=%s stderr=%s", detailed.code, detailed.stdout, detailed.stderr)
	}
	if !strings.Contains(detailed.stdout, "2026-04-01") || !strings.Contains(detailed.stdout, "2026-04-02") {
		t.Fatalf("expected detailed day rows, stdout=%q", detailed.stdout)
	}
	if !strings.Contains(detailed.stdout, "TOTAL") {
		t.Fatalf("expected TOTAL row in detailed output, stdout=%q", detailed.stdout)
	}
}

func TestTotalsTableCompactByDefaultForMismatchAndIndeterminate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"entry-1","description":"Mismatch","timeInterval":{"start":"2026-04-02T08:00:00Z","end":"2026-04-02T09:00:00Z"}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "2h", "--description", "Mismatch", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	mismatch := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02")
	if mismatch.code != 0 {
		t.Fatalf("mismatch totals failed: code=%d stdout=%s stderr=%s", mismatch.code, mismatch.stdout, mismatch.stderr)
	}
	if strings.Contains(mismatch.stdout, "\n2026-04-02") {
		t.Fatalf("expected compact mismatch output without day rows, stdout=%q", mismatch.stdout)
	}
	if !strings.Contains(mismatch.stdout, "state=mismatch") {
		t.Fatalf("expected mismatch footer state, stdout=%q", mismatch.stdout)
	}

	runningServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"running-1","description":"Running","timeInterval":{"start":"2026-04-02T08:00:00Z","end":""}}
			]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer runningServer.Close()

	clockifyadapter.BaseURL = runningServer.URL
	indeterminate := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02")
	if indeterminate.code != 0 {
		t.Fatalf("indeterminate totals failed: code=%d stdout=%s stderr=%s", indeterminate.code, indeterminate.stdout, indeterminate.stderr)
	}
	if strings.Contains(indeterminate.stdout, "\n2026-04-02") {
		t.Fatalf("expected compact indeterminate output without day rows, stdout=%q", indeterminate.stdout)
	}
	if !strings.Contains(indeterminate.stdout, "state=indeterminate") {
		t.Fatalf("expected indeterminate footer state, stdout=%q", indeterminate.stdout)
	}
}

func TestTotalsJiraDataRespectsRoutedPrefixesAcrossProfiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataTotalsTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-01T23:30:00.000+0000","timeSpentSeconds":7200,"comment":"Night work","author":{"name":"user-1","key":"user-1"}}`,
		},
		"BAPP-222": {
			`{"id":"wl-2","started":"2026-04-02T10:00:00.000+0000","timeSpentSeconds":3600,"comment":"Second route","author":{"name":"user-1","key":"user-1"}}`,
		},
		"ZAPP-999": {
			`{"id":"wl-3","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":1800,"comment":"Unrouted remote","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL}, map[string]string{
		"main": "      pull:\n        exclude_issues:\n          - MAIN-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          secondary:\n            issue_prefixes:\n              - BAPP\n          reporting-only:\n            reporting_targets:\n              RAPP: MAIN-1\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-01T23:30:00Z", "--duration", "2h", "--description", "Night work", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "BAPP-222", "--started-utc", "2026-04-02T10:00:00Z", "--duration", "1h", "--description", "Second route", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ZAPP-999", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "2h", "--description", "Unrouted local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-01", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	filters := payload["filters"].(map[string]any)
	raw := filters["raw"].(map[string]any)
	effective := filters["effective"].(map[string]any)
	if raw["instance"] != "main" || effective["instance"] != "main" {
		t.Fatalf("expected instance filters in output, got %s", totals.stdout)
	}
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected summary %s", totals.stdout)
	}
	days := payload["days"].([]any)
	if len(days) != 2 {
		t.Fatalf("expected 2 day rows, got %s", totals.stdout)
	}
	firstDay := days[0].(map[string]any)
	secondDay := days[1].(map[string]any)
	if firstDay["date"] != "2026-04-01" || firstDay["local_total_seconds"].(float64) != 1800 || firstDay["remote_total_seconds"].(float64) != 1800 {
		t.Fatalf("unexpected first day %s", totals.stdout)
	}
	if secondDay["date"] != "2026-04-02" || secondDay["local_total_seconds"].(float64) != 9000 || secondDay["remote_total_seconds"].(float64) != 9000 {
		t.Fatalf("unexpected second day %s", totals.stdout)
	}
}

func TestTotalsJiraDataMismatchOnlyCountsRoutedScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataTotalsTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"name":"user-1","key":"user-1"}}`,
		},
		"ZAPP-999": {
			`{"id":"wl-2","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":14400,"comment":"Unrouted remote","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL, "backup": server.URL}, map[string]string{
		"main":   "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
		"backup": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - BAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "2h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ZAPP-999", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "8h", "--description", "Unrouted local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	missingInstance := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if missingInstance.code != 2 {
		t.Fatalf("expected instance validation failure, got code=%d stdout=%s stderr=%s", missingInstance.code, missingInstance.stdout, missingInstance.stderr)
	}

	unknownInstance := runCLI(t, "totals", "--adapter", "jira-data-center", "--instance", "missing", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if unknownInstance.code != 2 {
		t.Fatalf("expected unknown instance validation failure, got code=%d stdout=%s stderr=%s", unknownInstance.code, unknownInstance.stdout, unknownInstance.stderr)
	}

	mismatch := runCLI(t, "totals", "--adapter", "jira-data-center", "--instance", "main", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if mismatch.code != 0 {
		t.Fatalf("mismatch totals failed: code=%d stdout=%s stderr=%s", mismatch.code, mismatch.stdout, mismatch.stderr)
	}
	payload := decodeJSONMap(t, []byte(mismatch.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "mismatch" || summary["delta_seconds"].(float64) != 3600 {
		t.Fatalf("unexpected mismatch summary %s", mismatch.stdout)
	}
}

func TestTotalsJiraDataTableCompactFooterIncludesInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataTotalsTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL}, map[string]string{
		"main": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-02", "--to", "2026-04-02")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	if strings.Contains(totals.stdout, "\n2026-04-02") {
		t.Fatalf("expected compact jira data output without day rows, stdout=%q", totals.stdout)
	}
	if !strings.Contains(totals.stdout, "instance=main") {
		t.Fatalf("expected instance in footer, stdout=%q", totals.stdout)
	}
}

func TestTotalsJiraDataValidatesRoutedScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataTotalsTestServer(t, map[string][]string{})
	defer server.Close()

	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL}, nil)
	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	missingRouting := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if missingRouting.code != 2 {
		t.Fatalf("expected missing routing validation, got code=%d stdout=%s stderr=%s", missingRouting.code, missingRouting.stdout, missingRouting.stderr)
	}
	if !strings.Contains(missingRouting.stdout, "routing is required for totals") && !strings.Contains(missingRouting.stderr, "routing is required for totals") {
		t.Fatalf("expected missing routing message, got stdout=%s stderr=%s", missingRouting.stdout, missingRouting.stderr)
	}

	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL}, map[string]string{
		"main": "      routing:\n        profiles:\n          default:\n            issue_prefixes: []\n",
	})
	noPrefixes := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if noPrefixes.code != 2 {
		t.Fatalf("expected missing issue_prefixes validation, got code=%d stdout=%s stderr=%s", noPrefixes.code, noPrefixes.stdout, noPrefixes.stderr)
	}
	if !strings.Contains(noPrefixes.stdout, "routed issue_prefixes are required for totals") && !strings.Contains(noPrefixes.stderr, "routed issue_prefixes are required for totals") {
		t.Fatalf("expected issue_prefixes message, got stdout=%s stderr=%s", noPrefixes.stdout, noPrefixes.stderr)
	}
}

func TestTotalsJiraDataExcludesReportingTargetsAndManualExcludedIssues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataTotalsTestServer(t, map[string][]string{
		"ACIU-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"name":"user-1","key":"user-1"}}`,
		},
		"ACIU-999": {
			`{"id":"wl-2","started":"2026-04-02T10:00:00.000+0000","timeSpentSeconds":7200,"comment":"Reporting target remote","author":{"name":"user-1","key":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraData(t, map[string]string{"main": server.URL}, map[string]string{
		"main": "      pull:\n        exclude_issues:\n          - MANUAL-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - ACIU\n          reporting-only:\n            reporting_targets:\n              RAPP: ACIU-999\n",
	})

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ACIU-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ACIU-999", "--started-utc", "2026-04-02T10:00:00Z", "--duration", "2h", "--description", "Reporting target local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-data-center", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["local_total_seconds"].(float64) != 3600 || summary["remote_total_seconds"].(float64) != 3600 || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected summary %s", totals.stdout)
	}
}

func TestTotalsJiraCloudMatchAcrossMultiDayWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-01T23:30:00.000+0000","timeSpentSeconds":7200,"comment":"Night work","author":{"accountId":"user-1"}}`,
		},
		"BAPP-222": {
			`{"id":"wl-2","started":"2026-04-02T10:00:00.000+0000","timeSpentSeconds":3600,"comment":"Second route","author":{"accountId":"user-1"}}`,
		},
		"ZAPP-999": {
			`{"id":"wl-3","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":7200,"comment":"Unrouted remote","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      pull:\n        exclude_issues:\n          - REPORT-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          secondary:\n            issue_prefixes:\n              - BAPP\n          reporting-only:\n            reporting_targets:\n              RAPP: REPORT-1\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-01T23:30:00Z", "--duration", "2h", "--description", "Night work", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "BAPP-222", "--started-utc", "2026-04-02T10:00:00Z", "--duration", "1h", "--description", "Second route", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ZAPP-999", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "2h", "--description", "Unrouted local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-01", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	filters := payload["filters"].(map[string]any)
	raw := filters["raw"].(map[string]any)
	effective := filters["effective"].(map[string]any)
	if raw["instance"] != "product" || effective["instance"] != "product" {
		t.Fatalf("expected instance filters in output, got %s", totals.stdout)
	}
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected summary %s", totals.stdout)
	}
	days := payload["days"].([]any)
	if len(days) != 2 {
		t.Fatalf("expected 2 day rows, got %s", totals.stdout)
	}
	firstDay := days[0].(map[string]any)
	secondDay := days[1].(map[string]any)
	if firstDay["date"] != "2026-04-01" || firstDay["local_total_seconds"].(float64) != 1800 || firstDay["remote_total_seconds"].(float64) != 1800 {
		t.Fatalf("unexpected first day %s", totals.stdout)
	}
	if secondDay["date"] != "2026-04-02" || firstDay["state"] != "match" || secondDay["local_total_seconds"].(float64) != 9000 || secondDay["remote_total_seconds"].(float64) != 9000 {
		t.Fatalf("unexpected second day %s", totals.stdout)
	}
}

func TestTotalsJiraCloudMismatchOnlyCountsRoutedScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"accountId":"user-1"}}`,
		},
		"ZAPP-999": {
			`{"id":"wl-2","started":"2026-04-02T12:00:00.000+0000","timeSpentSeconds":14400,"comment":"Unrouted remote","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL, "backup": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
		"backup":  "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - BAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "2h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ZAPP-999", "--started-utc", "2026-04-02T12:00:00Z", "--duration", "8h", "--description", "Unrouted local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	missingInstance := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if missingInstance.code != 2 {
		t.Fatalf("expected instance validation failure, got code=%d stdout=%s stderr=%s", missingInstance.code, missingInstance.stdout, missingInstance.stderr)
	}

	unknownInstance := runCLI(t, "totals", "--adapter", "jira-cloud", "--instance", "missing", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if unknownInstance.code != 2 {
		t.Fatalf("expected unknown instance validation failure, got code=%d stdout=%s stderr=%s", unknownInstance.code, unknownInstance.stdout, unknownInstance.stderr)
	}

	mismatch := runCLI(t, "totals", "--adapter", "jira-cloud", "--instance", "product", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if mismatch.code != 0 {
		t.Fatalf("mismatch totals failed: code=%d stdout=%s stderr=%s", mismatch.code, mismatch.stdout, mismatch.stderr)
	}
	payload := decodeJSONMap(t, []byte(mismatch.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "mismatch" || summary["delta_seconds"].(float64) != 3600 {
		t.Fatalf("unexpected mismatch summary %s", mismatch.stdout)
	}
}

func TestTotalsJiraCloudTableCompactFooterIncludesInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-02", "--to", "2026-04-02")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}
	if strings.Contains(totals.stdout, "\n2026-04-02") {
		t.Fatalf("expected compact jira cloud output without day rows, stdout=%q", totals.stdout)
	}
	if !strings.Contains(totals.stdout, "instance=product") {
		t.Fatalf("expected instance in footer, stdout=%q", totals.stdout)
	}
}

func TestTotalsJiraCloudValidatesRoutedScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{})
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, nil)
	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	missingRouting := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if missingRouting.code != 2 {
		t.Fatalf("expected missing routing validation, got code=%d stdout=%s stderr=%s", missingRouting.code, missingRouting.stdout, missingRouting.stderr)
	}
	if !strings.Contains(missingRouting.stdout, "routing is required for totals") && !strings.Contains(missingRouting.stderr, "routing is required for totals") {
		t.Fatalf("expected missing routing message, got stdout=%s stderr=%s", missingRouting.stdout, missingRouting.stderr)
	}

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes: []\n",
	})
	noPrefixes := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if noPrefixes.code != 2 {
		t.Fatalf("expected missing issue_prefixes validation, got code=%d stdout=%s stderr=%s", noPrefixes.code, noPrefixes.stdout, noPrefixes.stderr)
	}
	if !strings.Contains(noPrefixes.stdout, "routed issue_prefixes are required for totals") && !strings.Contains(noPrefixes.stderr, "routed issue_prefixes are required for totals") {
		t.Fatalf("expected issue_prefixes message, got stdout=%s stderr=%s", noPrefixes.stdout, noPrefixes.stderr)
	}
}

func TestTotalsJiraCloudExcludesReportingTargetsAndManualExcludedIssues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"ACIU-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Routed remote","author":{"accountId":"user-1"}}`,
		},
		"ACIU-999": {
			`{"id":"wl-2","started":"2026-04-02T10:00:00.000+0000","timeSpentSeconds":7200,"comment":"Reporting target remote","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      pull:\n        exclude_issues:\n          - MANUAL-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - ACIU\n          reporting-only:\n            reporting_targets:\n              RAPP: ACIU-999\n",
	})

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ACIU-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Routed local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ACIU-999", "--started-utc", "2026-04-02T10:00:00Z", "--duration", "2h", "--description", "Reporting target local", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["local_total_seconds"].(float64) != 3600 || summary["remote_total_seconds"].(float64) != 3600 || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected summary %s", totals.stdout)
	}
}

func TestTotalsJiraCloudCurrentMonthWorks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	now := time.Now().UTC()
	startedAt := time.Date(now.Year(), now.Month(), 3, 8, 0, 0, 0, time.UTC)
	issueKey := "AAPP-123"
	server := newJiraCloudTestServer(t, map[string][]string{
		issueKey: {
			`{"id":"wl-1","started":"` + startedAt.Format("2006-01-02T15:04:05.000-0700") + `","timeSpentSeconds":3600,"comment":"Current month","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", issueKey, "--started-utc", startedAt.Format(time.RFC3339), "--duration", "1h", "--description", "Current month", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--current-month", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected current month summary %s", totals.stdout)
	}
}

func TestTotalsJiraCloudResolvesIdOnlySearchResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	now := time.Now().UTC()
	startedAt := time.Date(now.Year(), now.Month(), 3, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"user-1","displayName":"User One"}`))
		case r.URL.Path == "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"isLast":true,"issues":[{"id":"48218"}]}`))
		case r.URL.Path == "/rest/api/3/issue/48218":
			_, _ = w.Write([]byte(`{"id":"48218","key":"AAPP-123"}`))
		case r.URL.Path == "/rest/api/3/issue/48218/worklog":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":1,"worklogs":[{"id":"wl-1","started":"` + startedAt.Format("2006-01-02T15:04:05.000-0700") + `","timeSpentSeconds":3600,"comment":"Current month","author":{"accountId":"user-1"}}]}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", startedAt.Format(time.RFC3339), "--duration", "1h", "--description", "Current month", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--current-month", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected id-only search summary %s", totals.stdout)
	}
}

func TestTotalsJiraCloudResolvesAndExcludesIdOnlyReportingTargetSearchResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	now := time.Now().UTC()
	startedAt := time.Date(now.Year(), now.Month(), 3, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"user-1","displayName":"User One"}`))
		case r.URL.Path == "/rest/api/3/search/jql":
			_, _ = w.Write([]byte(`{"isLast":true,"issues":[{"id":"48218"},{"key":"ACIU-123"}]}`))
		case r.URL.Path == "/rest/api/3/issue/48218":
			_, _ = w.Write([]byte(`{"id":"48218","key":"ACIU-999"}`))
		case r.URL.Path == "/rest/api/3/issue/ACIU-123/worklog":
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":1,"worklogs":[{"id":"wl-1","started":"` + startedAt.Format("2006-01-02T15:04:05.000-0700") + `","timeSpentSeconds":3600,"comment":"Current month","author":{"accountId":"user-1"}}]}`))
		case r.URL.Path == "/rest/api/3/issue/48218/worklog":
			t.Fatalf("excluded reporting target worklogs should not be loaded")
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - ACIU\n          reporting:\n            reporting_targets:\n              RAPP: ACIU-999\n",
	})

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ACIU-123", "--started-utc", startedAt.Format(time.RFC3339), "--duration", "1h", "--description", "Current month", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	add = runCLI(t, "worklogs", "add", "--issue", "ACIU-999", "--started-utc", startedAt.Add(2*time.Hour).Format(time.RFC3339), "--duration", "2h", "--description", "Excluded reporting target", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	totals := runCLI(t, "totals", "--adapter", "jira-cloud", "--current-month", "--output", "json")
	if totals.code != 0 {
		t.Fatalf("totals failed: code=%d stdout=%s stderr=%s", totals.code, totals.stdout, totals.stderr)
	}

	payload := decodeJSONMap(t, []byte(totals.stdout))
	summary := payload["summary"].(map[string]any)
	if summary["state"] != "match" || summary["local_total_seconds"].(float64) != 3600 || summary["remote_total_seconds"].(float64) != 3600 || summary["delta_seconds"].(float64) != 0 {
		t.Fatalf("unexpected id-only exclusion summary %s", totals.stdout)
	}
}

func TestClockifyPlanReconcileAndApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if reconcileResult.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	plan := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if plan["plan_direction"].(string) != "pull" {
		t.Fatalf("unexpected plan direction %s", reconcileResult.stdout)
	}
	summary := plan["summary"].(map[string]any)
	if summary["ready_items"].(float64) != 1 {
		t.Fatalf("expected one ready item, got %s", reconcileResult.stdout)
	}
	if summary["invalid_findings"].(float64) != 2 {
		t.Fatalf("expected two invalid findings, got %s", reconcileResult.stdout)
	}
	planID := plan["plan_id"].(string)

	apply := runCLI(t, "plan", "apply", planID, "--output", "json")
	if apply.code != 0 {
		t.Fatalf("apply failed: code=%d stdout=%s stderr=%s", apply.code, apply.stdout, apply.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if list.code != 0 {
		t.Fatalf("worklogs list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	listPayload := decodeJSONMap(t, []byte(list.stdout))
	if listPayload["total"].(float64) != 1 {
		t.Fatalf("expected one imported worklog, got %s", list.stdout)
	}

	second := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second reconcile failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}
	secondPayload := decodeJSONMap(t, []byte(second.stdout))
	secondSummary := secondPayload["summary"].(map[string]any)
	if secondSummary["ready_items"].(float64) != 0 {
		t.Fatalf("expected zero ready items after apply, got %s", second.stdout)
	}
}

func TestPlanReconcilePullJiraCloudAcrossMultipleInstancesCreatesSinglePlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
		"BAPP-456": {
			`{"id":"wl-2","started":"2026-05-12T10:00:00.000+0000","timeSpentSeconds":1800,"comment":"Review PR","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{
		"maxima_lt_jira": server.URL,
		"ito_jira":       server.URL,
	}, map[string]string{
		"maxima_lt_jira": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
		"ito_jira":       "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - BAPP\n",
	})

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "jira-cloud", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "jira-cloud" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 1 || got[0] != "jira-cloud" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 2 {
		t.Fatalf("expected two target instances, got %s", result.stdout)
	}
	if count := countSavedPlans(t); count != 1 {
		t.Fatalf("expected one saved plan, got %d", count)
	}
}

func TestPlanReconcilePullInfersJiraCloudAdapterFromInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"maxima_lt_jira": server.URL}, map[string]string{
		"maxima_lt_jira": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--instance", "maxima_lt_jira", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "jira-cloud" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 1 || got[0] != "jira-cloud" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 1 || got[0] != "maxima_lt_jira" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcilePullInfersClockifyFromInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--instance", "clockify", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "clockify" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 1 || got[0] != "clockify" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 1 || got[0] != "clockify" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcilePullInfersMixedJiraFamiliesFromInstances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{
		"BAPP-456": {
			`{"id":"wl-2","started":"2026-05-12T10:00:00.000+0000","timeSpentSeconds":1800,"comment":"Review PR","author":{"name":"user-1"}}`,
		},
	})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		map[string]string{"ito_jira": jiraDataServer.URL},
		map[string]string{"maxima_lt_jira": "AAPP"},
		map[string]string{"ito_jira": "BAPP"},
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--instance", "maxima_lt_jira", "--instance", "ito_jira", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "multiple" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 2 || got[0] != "jira-cloud" || got[1] != "jira-data-center" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 2 || got[0] != "ito_jira" || got[1] != "maxima_lt_jira" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcilePullUnionsExplicitAdapterAndInstanceInferredFamily(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudServer.Close()
	jiraDataServer := newJiraDataTotalsTestServer(t, map[string][]string{
		"BAPP-456": {
			`{"id":"wl-2","started":"2026-05-12T10:00:00.000+0000","timeSpentSeconds":1800,"comment":"Review PR","author":{"name":"user-1"}}`,
		},
	})
	defer jiraDataServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		map[string]string{"ito_jira": jiraDataServer.URL},
		map[string]string{"maxima_lt_jira": "AAPP"},
		map[string]string{"ito_jira": "BAPP"},
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "jira-cloud", "--instance", "maxima_lt_jira", "--instance", "ito_jira", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "multiple" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 2 || got[0] != "jira-cloud" || got[1] != "jira-data-center" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 2 || got[0] != "ito_jira" || got[1] != "maxima_lt_jira" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcileRequiresAdapterOrInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--current-week", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "at least one --adapter or --instance is required") &&
		!strings.Contains(result.stderr, "at least one --adapter or --instance is required") {
		t.Fatalf("unexpected validation message stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestPlanReconcilePullHonorsClockifyOnlyAllowlistWithExplicitJiraAdapter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := newClockifyTestServer(t)
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		nil,
		map[string]string{"maxima_lt_jira": "AAPP"},
		nil,
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "jira-cloud", "--instance", "clockify", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "clockify" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 1 || got[0] != "clockify" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 1 || got[0] != "clockify" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcilePullSkipsClockifyWhenInstanceAllowlistExcludesIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := newClockifyTestServer(t)
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		nil,
		map[string]string{"maxima_lt_jira": "AAPP"},
		nil,
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--instance", "maxima_lt_jira", "--current-week", "--output", "json")
	if result.code != 0 {
		t.Fatalf("expected success, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["adapter_family"] != "jira-cloud" {
		t.Fatalf("unexpected adapter summary %s", result.stdout)
	}
	if got := payload["adapter_families"].([]any); len(got) != 1 || got[0] != "jira-cloud" {
		t.Fatalf("unexpected adapter families %s", result.stdout)
	}
	if got := payload["target_instances"].([]any); len(got) != 1 || got[0] != "maxima_lt_jira" {
		t.Fatalf("unexpected target instances %s", result.stdout)
	}
}

func TestPlanReconcilePullMixedInstanceClockifyAuthFailurePersistsCheckFailedPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	jiraCloudServer := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-05-12T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer jiraCloudServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		nil,
		map[string]string{"maxima_lt_jira": "AAPP"},
		nil,
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--instance", "maxima_lt_jira", "--instance", "clockify", "--current-week")
	if result.code != 6 {
		t.Fatalf("expected partial-plan exit 6, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "check_failed") && !strings.Contains(result.stderr, "check_failed") {
		t.Fatalf("expected saved check_failed plan output, got stdout=%s stderr=%s", result.stdout, result.stderr)
	}
}

func TestPlanReconcilePullMixedInstancesPersistsPartialPlanOnAdapterFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	clockifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer clockifyServer.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = clockifyServer.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	jiraCloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer jiraCloudServer.Close()

	writeConfigWithUTCAndBareTotalsAdapters(
		t,
		map[string]string{"maxima_lt_jira": jiraCloudServer.URL},
		nil,
		map[string]string{"maxima_lt_jira": "AAPP"},
		nil,
	)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--instance", "maxima_lt_jira", "--instance", "clockify", "--current-week", "--output", "json")
	if result.code != 6 {
		t.Fatalf("expected partial-plan exit 6, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["aggregate_status"] != "check_failed" {
		t.Fatalf("expected check_failed plan, got %s", result.stdout)
	}
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two failed adapter-scope items, got %s", result.stdout)
	}

	got := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		got[item["target_adapter_instance"].(string)] = item
	}

	if got["clockify"]["plan_status"] != "check_failed" || got["clockify"]["reason_code"] != "auth_error" {
		t.Fatalf("unexpected clockify failed item %#v", got["clockify"])
	}
	if got["maxima_lt_jira"]["plan_status"] != "check_failed" || got["maxima_lt_jira"]["reason_code"] != "auth_error" {
		t.Fatalf("unexpected jira failed item %#v", got["maxima_lt_jira"])
	}
	if countSavedPlans(t) != 1 {
		t.Fatalf("expected one saved plan, got %d", countSavedPlans(t))
	}
}

func TestIssueMetadataRefreshJiraDataCenterFiltersToSelectedInstancePrefixes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraDataIssueMetadataTestServer(t, map[string]*int64{
		"AAPP-123": int64Ptr(7200),
	})
	defer server.Close()

	writeConfigWithUTCAndJiraData(t, map[string]string{
		"main":   server.URL,
		"backup": server.URL,
	}, map[string]string{
		"main":   "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
		"backup": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - BAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started", "todayT09:00", "--duration", "1h", "--description", "Main issue", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	second := runCLI(t, "worklogs", "add", "--issue", "BAPP-123", "--started", "todayT10:00", "--duration", "1h", "--description", "Backup issue", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	refresh := runCLI(t, "issue-metadata", "refresh", "--adapter", "jira-data-center", "--instance", "main", "--field", "max-estimate", "--current-month", "--output", "json")
	if refresh.code != 0 {
		t.Fatalf("refresh failed: code=%d stdout=%s stderr=%s", refresh.code, refresh.stdout, refresh.stderr)
	}

	payload := decodeJSONMap(t, []byte(refresh.stdout))
	if payload["updated"].(float64) != 1 {
		t.Fatalf("expected one refreshed issue, got %s", refresh.stdout)
	}
	issues := payload["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("expected one issue payload, got %s", refresh.stdout)
	}
	item := issues[0].(map[string]any)
	if item["issue_key"] != "AAPP-123" {
		t.Fatalf("expected AAPP-123 refresh payload, got %s", refresh.stdout)
	}
	if item["max_estimate_seconds"].(float64) != 7200 {
		t.Fatalf("expected 7200-second estimate, got %s", refresh.stdout)
	}
}

func TestIssueMetadataRefreshJiraCloudFiltersToSelectedInstancePrefixes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudIssueMetadataTestServer(t, map[string]*int64{
		"AAPP-123": int64Ptr(7200),
		"AAPP-456": nil,
	})
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{
		"product": server.URL,
		"backup":  server.URL,
	}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
		"backup":  "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - BAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	for _, args := range [][]string{
		{"worklogs", "add", "--issue", "AAPP-123", "--started", "todayT09:00", "--duration", "1h", "--description", "Main issue", "--output", "json"},
		{"worklogs", "add", "--issue", "AAPP-456", "--started", "todayT10:00", "--duration", "1h", "--description", "No estimate issue", "--output", "json"},
		{"worklogs", "add", "--issue", "BAPP-123", "--started", "todayT11:00", "--duration", "1h", "--description", "Backup issue", "--output", "json"},
	} {
		add := runCLI(t, args...)
		if add.code != 0 {
			t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
		}
	}

	refresh := runCLI(t, "issue-metadata", "refresh", "--adapter", "jira-cloud", "--instance", "product", "--field", "max-estimate", "--current-month", "--output", "json")
	if refresh.code != 0 {
		t.Fatalf("refresh failed: code=%d stdout=%s stderr=%s", refresh.code, refresh.stdout, refresh.stderr)
	}

	payload := decodeJSONMap(t, []byte(refresh.stdout))
	if payload["updated"].(float64) != 2 {
		t.Fatalf("expected two refreshed issues, got %s", refresh.stdout)
	}
	issues := payload["issues"].([]any)
	if len(issues) != 2 {
		t.Fatalf("expected two issue payloads, got %s", refresh.stdout)
	}
	got := map[string]any{}
	for _, raw := range issues {
		item := raw.(map[string]any)
		got[item["issue_key"].(string)] = item["max_estimate_seconds"]
	}
	if got["AAPP-123"].(float64) != 7200 {
		t.Fatalf("expected 7200-second estimate, got %s", refresh.stdout)
	}
	if got["AAPP-456"] != nil {
		t.Fatalf("expected null estimate, got %s", refresh.stdout)
	}

}

func TestJiraCloudPlanReconcileAndApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"AAPP-123": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      pull:\n        exclude_issues:\n          - REPORT-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "jira-cloud", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if reconcileResult.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	plan := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if plan["adapter_family"] != "jira-cloud" || plan["plan_direction"] != "pull" {
		t.Fatalf("unexpected plan payload %s", reconcileResult.stdout)
	}
	planID := plan["plan_id"].(string)

	apply := runCLI(t, "plan", "apply", planID, "--output", "json")
	if apply.code != 0 {
		t.Fatalf("apply failed: code=%d stdout=%s stderr=%s", apply.code, apply.stdout, apply.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if list.code != 0 {
		t.Fatalf("worklogs list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	listPayload := decodeJSONMap(t, []byte(list.stdout))
	if listPayload["total"].(float64) != 1 {
		t.Fatalf("expected one imported worklog, got %s", list.stdout)
	}
}

func TestJiraCloudPushPlanAndApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startedAt := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)

	server := newJiraCloudTestServer(t, map[string][]string{})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      pull:\n        exclude_issues:\n          - REPORT-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", startedAt, "--duration", "1h", "--description", "Build feature", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--push", "--adapter", "jira-cloud", "--last-week", "--output", "json")
	if reconcileResult.code != 0 {
		t.Fatalf("push reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	payload := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if payload["adapter_family"] != "jira-cloud" || payload["plan_direction"] != "push" {
		t.Fatalf("unexpected plan payload %s", reconcileResult.stdout)
	}
	planID := payload["plan_id"].(string)

	apply := runCLI(t, "plan", "apply", planID, "--output", "json")
	if apply.code != 0 {
		t.Fatalf("apply failed: code=%d stdout=%s stderr=%s", apply.code, apply.stdout, apply.stderr)
	}
	applyPayload := decodeJSONMap(t, []byte(apply.stdout))
	if applyPayload["applied_count"].(float64) != 1 {
		t.Fatalf("expected one applied item, got %s", apply.stdout)
	}

	show := runCLI(t, "plan", "show", planID, "--output", "json")
	if show.code != 0 {
		t.Fatalf("show failed: code=%d stdout=%s stderr=%s", show.code, show.stdout, show.stderr)
	}
	showPayload := decodeJSONMap(t, []byte(show.stdout))
	items := showPayload["items"].([]any)
	if items[0].(map[string]any)["apply_message"] != "applied saved push payload to jira-cloud" {
		t.Fatalf("unexpected apply message %s", show.stdout)
	}
}

func TestJiraCloudPushReconcileCheckFailedPlanUsesExitSixAndNormalJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	startedAt := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      pull:\n        exclude_issues:\n          - REPORT-1\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n",
	})

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", startedAt, "--duration", "1h", "--description", "Build feature", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--push", "--adapter", "jira-cloud", "--last-week", "--progress", "plain", "--output", "json")
	if reconcileResult.code != 6 {
		t.Fatalf("expected partial-success exit 6, got code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}

	payload := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if payload["aggregate_status"] != "check_failed" {
		t.Fatalf("expected check_failed aggregate status, got %s", reconcileResult.stdout)
	}
	if payload["error"] != nil {
		t.Fatalf("expected normal plan payload, got %s", reconcileResult.stdout)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one planned scope, got %s", reconcileResult.stdout)
	}
	item := items[0].(map[string]any)
	if item["plan_status"] != "check_failed" || item["comparison_status"] != "check_failed" || item["reason_code"] != "auth_error" {
		t.Fatalf("unexpected failed item %s", reconcileResult.stdout)
	}
	if countSavedPlans(t) != 1 {
		t.Fatalf("expected one saved plan, got %d", countSavedPlans(t))
	}
	if !strings.Contains(reconcileResult.stderr, "[progress]") {
		t.Fatalf("expected progress on stderr, got %q", reconcileResult.stderr)
	}
	if strings.Contains(reconcileResult.stdout, "[progress]") {
		t.Fatalf("expected stdout to remain pure json, got %q", reconcileResult.stdout)
	}
}

func TestJiraCloudReportingReconcileAlreadyInSyncReturnsNoPlanJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{
		"REPORT-1": {
			`{"id":"wl-1","started":"2026-04-02T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"AAPP-123 | Build feature","author":{"accountId":"user-1"}}`,
		},
	})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          reporting:\n            reporting_targets:\n              AAPP: REPORT-1\n",
	})

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	add := runCLI(t, "worklogs", "add", "--issue", "AAPP-123", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Build feature", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--push", "--adapter", "jira-cloud", "--route-profile", "reporting", "--from", "2026-04-02", "--to", "2026-04-02", "--output", "json")
	if reconcileResult.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	payload := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if payload["plan_created"] != false || payload["reason"] != "already_in_sync" {
		t.Fatalf("unexpected no-plan payload %s", reconcileResult.stdout)
	}
	if payload["plan_id"] != nil {
		t.Fatalf("expected no plan_id, got %s", reconcileResult.stdout)
	}
	if payload["route_profile"] != "reporting" || payload["matched_scope_count"].(float64) != 1 || payload["actionable_scope_count"].(float64) != 0 {
		t.Fatalf("unexpected no-plan metadata %s", reconcileResult.stdout)
	}
	instances := payload["resolved_target_instances"].([]any)
	if len(instances) != 1 || instances[0] != "product" {
		t.Fatalf("unexpected resolved instances %s", reconcileResult.stdout)
	}
	if count := countSavedPlans(t); count != 0 {
		t.Fatalf("expected no saved plans, got %d", count)
	}
}

func TestJiraCloudReportingReconcileNoMatchingRoutesReturnsNoPlanTable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := newJiraCloudTestServer(t, map[string][]string{})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{"product": server.URL}, map[string]string{
		"product": "      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - AAPP\n          reporting:\n            reporting_targets:\n              AAPP: REPORT-1\n",
	})

	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	add := runCLI(t, "worklogs", "add", "--issue", "ZAPP-999", "--started-utc", "2026-04-02T08:00:00Z", "--duration", "1h", "--description", "Unmapped", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--push", "--adapter", "jira-cloud", "--route-profile", "reporting", "--from", "2026-04-02", "--to", "2026-04-02")
	if reconcileResult.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	if !strings.Contains(reconcileResult.stdout, "PLAN_CREATED") || !strings.Contains(reconcileResult.stdout, "no_matching_routes") || !strings.Contains(reconcileResult.stdout, "false") {
		t.Fatalf("unexpected no-plan table %q", reconcileResult.stdout)
	}
	if count := countSavedPlans(t); count != 0 {
		t.Fatalf("expected no saved plans, got %d", count)
	}
}

func TestPlanReconcileRejectsJiraCloudSnakeCaseAndUnsupportedCommandsStayDeterministic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := newJiraCloudTestServer(t, map[string][]string{})
	defer server.Close()
	writeConfigWithUTCAndJiraCloud(t, map[string]string{
		"product": server.URL,
		"backup":  server.URL,
	}, nil)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	invalid := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "jira_cloud", "--today", "--output", "json")
	if invalid.code != 2 {
		t.Fatalf("expected validation error, got code=%d stdout=%s stderr=%s", invalid.code, invalid.stdout, invalid.stderr)
	}
	if !strings.Contains(invalid.stdout, "supported adapters are clockify, jira-cloud, and jira-data-center") && !strings.Contains(invalid.stderr, "supported adapters are clockify, jira-cloud, and jira-data-center") {
		t.Fatalf("expected adapter validation message, got stdout=%s stderr=%s", invalid.stdout, invalid.stderr)
	}

	status := runCLI(t, "status", "--adapter", "jira-cloud", "--output", "json")
	if status.code != 0 {
		t.Fatalf("expected jira-cloud status success, got code=%d stdout=%s stderr=%s", status.code, status.stdout, status.stderr)
	}
	items := statusItems(t, status.stdout)
	if len(items) != 2 {
		t.Fatalf("expected two jira-cloud status rows, got %s", status.stdout)
	}

	missingInstance := runCLI(t, "issue-metadata", "refresh", "--adapter", "jira-cloud", "--field", "max-estimate", "--today", "--output", "json")
	if missingInstance.code != 2 {
		t.Fatalf("expected jira-cloud issue-metadata missing-instance validation, got code=%d stdout=%s stderr=%s", missingInstance.code, missingInstance.stdout, missingInstance.stderr)
	}
	if !strings.Contains(missingInstance.stdout, "--instance is required when more than one jira_cloud instance is configured") && !strings.Contains(missingInstance.stderr, "--instance is required when more than one jira_cloud instance is configured") {
		t.Fatalf("unexpected missing-instance message stdout=%s stderr=%s", missingInstance.stdout, missingInstance.stderr)
	}

	unknownInstance := runCLI(t, "issue-metadata", "refresh", "--adapter", "jira-cloud", "--instance", "missing", "--field", "max-estimate", "--today", "--output", "json")
	if unknownInstance.code != 2 {
		t.Fatalf("expected unknown jira-cloud instance validation, got code=%d stdout=%s stderr=%s", unknownInstance.code, unknownInstance.stdout, unknownInstance.stderr)
	}
	if !(strings.Contains(unknownInstance.stdout, "jira_cloud instance") && strings.Contains(unknownInstance.stdout, "is not configured")) &&
		!(strings.Contains(unknownInstance.stderr, "jira_cloud instance") && strings.Contains(unknownInstance.stderr, "is not configured")) {
		t.Fatalf("unexpected unknown-instance message stdout=%s stderr=%s", unknownInstance.stdout, unknownInstance.stderr)
	}

	unsupported := runCLI(t, "issue-metadata", "refresh", "--adapter", "clockify", "--field", "max-estimate", "--today", "--output", "json")
	if unsupported.code != 2 {
		t.Fatalf("expected unsupported adapter validation, got code=%d stdout=%s stderr=%s", unsupported.code, unsupported.stdout, unsupported.stderr)
	}
	if !strings.Contains(unsupported.stdout, "supported adapters are jira-cloud and jira-data-center") && !strings.Contains(unsupported.stderr, "supported adapters are jira-cloud and jira-data-center") {
		t.Fatalf("unexpected issue-metadata adapter message stdout=%s stderr=%s", unsupported.stdout, unsupported.stderr)
	}
}

func TestPlanTableOutputIsAligned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	reconcile := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30")
	if reconcile.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcile.code, reconcile.stdout, reconcile.stderr)
	}
	if strings.Contains(reconcile.stdout, "\t") {
		t.Fatalf("expected aligned table output without raw tabs, got %q", reconcile.stdout)
	}
	if !strings.Contains(reconcile.stdout, "PLAN_ID") || !strings.Contains(reconcile.stdout, "INVALID_FINDINGS") {
		t.Fatalf("expected reconcile headers, got %q", reconcile.stdout)
	}
	if !strings.Contains(reconcile.stdout, "\nNext:\n") || !strings.Contains(reconcile.stdout, "workledger plan show ") {
		t.Fatalf("expected reconcile next-step footer, got %q", reconcile.stdout)
	}
	if !strings.Contains(reconcile.stdout, "workledger plan apply ") {
		t.Fatalf("expected reconcile apply next step when ready items exist, got %q", reconcile.stdout)
	}

	show := runCLI(t, "plan", "show")
	if show.code != 0 {
		t.Fatalf("show failed: code=%d stdout=%s stderr=%s", show.code, show.stdout, show.stderr)
	}
	if strings.Contains(show.stdout, "\t") {
		t.Fatalf("expected aligned plan show output without raw tabs, got %q", show.stdout)
	}
	if !strings.Contains(show.stdout, "TARGET_INSTANCE") || !strings.Contains(show.stdout, "REMOTE_ROWS") {
		t.Fatalf("expected plan show headers, got %q", show.stdout)
	}
	if !strings.Contains(show.stdout, "  -  ") && !strings.Contains(show.stdout, "\nclockify") {
		t.Fatalf("expected plan show target instance placeholder, got %q", show.stdout)
	}
	if !strings.Contains(show.stdout, "2026-04-01T00:00:00Z...2026-04-30T23:59:59Z") {
		t.Fatalf("expected plan show window value, got %q", show.stdout)
	}
}

func TestPlanReconcileTableOutputOmitsApplyNextStepWhenNoReadyItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	first := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first reconcile failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	planID := decodeJSONMap(t, []byte(first.stdout))["plan_id"].(string)

	apply := runCLI(t, "plan", "apply", planID, "--output", "json")
	if apply.code != 0 {
		t.Fatalf("apply failed: code=%d stdout=%s stderr=%s", apply.code, apply.stdout, apply.stderr)
	}

	second := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30")
	if second.code != 0 {
		t.Fatalf("second reconcile failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}
	if !strings.Contains(second.stdout, "\nNext:\n") || !strings.Contains(second.stdout, "workledger plan show ") {
		t.Fatalf("expected reconcile show next step, got %q", second.stdout)
	}
	if strings.Contains(second.stdout, "workledger plan apply ") {
		t.Fatalf("expected reconcile output without apply next step when no ready items exist, got %q", second.stdout)
	}
}

func TestPlanReconcileJSONOutputDoesNotIncludeNextStepFooter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30", "--output", "json")
	if result.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if strings.Contains(result.stdout, "Next:") || strings.Contains(result.stdout, "workledger plan show ") || strings.Contains(result.stdout, "workledger plan apply ") {
		t.Fatalf("expected reconcile json output without next-step footer, got %q", result.stdout)
	}
}

func TestPlanListTableIncludesWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	seedSavedPlan(t, savedPlanSeed{
		planID:      "plan-1",
		fingerprint: "fp",
		itemID:      "item-1",
		direction:   "pull",
		adapter:     "clockify",
		target:      "AAPP-1",
		action:      "merge",
		payloadJSON: "[]",
	})

	result := runCLI(t, "plan", "list")
	if result.code != 0 {
		t.Fatalf("plan list failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if strings.Contains(result.stdout, "\t") {
		t.Fatalf("expected aligned plan list output without raw tabs, got %q", result.stdout)
	}
	if !strings.Contains(result.stdout, "WINDOW") || !strings.Contains(result.stdout, "2026-05-01T00:00:00Z...2026-05-01T23:59:59Z") {
		t.Fatalf("expected plan list window column, got %q", result.stdout)
	}
}

func TestPlanShowDeleteOnlyScopeReportsZeroLocalRowsAndMatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockifyPush(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	seedTombstone(t, "row-1", "AAPP-123", "2026-05-01T08:00:00Z", 3600, "Deleted locally", "2026-05-02T08:00:00Z")

	reconcile := runCLI(t, "plan", "reconcile", "--push", "--adapter", "clockify", "--from", "2026-05-01", "--to", "2026-05-31", "--output", "json")
	if reconcile.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", reconcile.code, reconcile.stdout, reconcile.stderr)
	}

	show := runCLI(t, "plan", "show", "--output", "json")
	if show.code != 0 {
		t.Fatalf("show failed: code=%d stdout=%s stderr=%s", show.code, show.stdout, show.stderr)
	}
	payload := decodeJSONMap(t, []byte(show.stdout))
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one saved plan item, got %#v", payload["items"])
	}
	item := items[0].(map[string]any)
	if item["local_row_count"].(float64) != 0 {
		t.Fatalf("expected tombstone scope local_row_count=0, got %#v", item["local_row_count"])
	}
	if item["comparison_status"] != "match" || item["reason_code"] != "exact_match" {
		t.Fatalf("expected tombstone empty remote scope to render as match, got %#v", item)
	}
}

func TestPlanShowOnlyReadyFiltersItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	seedSavedPlan(t, savedPlanSeed{
		planID:      "plan-ready-filter",
		fingerprint: "fp",
		items: []savedPlanItemSeed{
			{
				itemID:      "item-ready",
				direction:   "push",
				adapter:     "clockify",
				target:      "AAPP-1",
				action:      "create",
				payloadJSON: "[]",
			},
			{
				itemID:      "item-failed",
				direction:   "push",
				adapter:     "clockify",
				target:      "BAPP-1",
				action:      "none",
				payloadJSON: "[]",
			},
		},
	})

	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`UPDATE saved_plan_items SET plan_status = 'check_failed', comparison_status = 'check_failed', reason_code = 'auth_error', reason_detail = 'seeded failed' WHERE id = ?`, "item-failed"); err != nil {
		t.Fatalf("mark saved plan item failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE saved_plans SET aggregate_status = 'check_failed' WHERE id = ?`, "plan-ready-filter"); err != nil {
		t.Fatalf("mark saved plan failed: %v", err)
	}

	result := runCLI(t, "plan", "show", "plan-ready-filter", "--only-ready", "--output", "json")
	if result.code != 0 {
		t.Fatalf("plan show failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one ready item, got %s", result.stdout)
	}
	item := items[0].(map[string]any)
	if item["id"] != "item-ready" || item["plan_status"] != "ready" {
		t.Fatalf("expected only ready item, got %#v", item)
	}
	summary := payload["summary"].(map[string]any)
	if summary["total_items"].(float64) != 1 || summary["ready_items"].(float64) != 1 {
		t.Fatalf("expected filtered summary counts, got %#v", summary)
	}
}

func TestPlanRetryValidatesOnlyAndNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	missing := runCLI(t, "plan", "retry", "--output", "json")
	if missing.code != 2 {
		t.Fatalf("expected missing --only validation, got code=%d stdout=%s stderr=%s", missing.code, missing.stdout, missing.stderr)
	}

	invalid := runCLI(t, "plan", "retry", "--only", "later", "--output", "json")
	if invalid.code != 2 {
		t.Fatalf("expected invalid --only validation, got code=%d stdout=%s stderr=%s", invalid.code, invalid.stdout, invalid.stderr)
	}

	writeConfigWithUTC(t)
	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	notFound := runCLI(t, "plan", "retry", "missing-plan", "--only", "failed", "--output", "json")
	if notFound.code != 3 {
		t.Fatalf("expected not-found exit 3, got code=%d stdout=%s stderr=%s", notFound.code, notFound.stdout, notFound.stderr)
	}
}

func TestPlanRetryExitCodesMatchApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	effective, err := config.LoadEffective()
	if err != nil {
		t.Fatalf("load effective config: %v", err)
	}
	fingerprint, err := config.FingerprintEffective(effective)
	if err != nil {
		t.Fatalf("fingerprint config: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		seedSavedPlan(t, savedPlanSeed{
			planID:      "plan-success",
			fingerprint: fingerprint,
			itemID:      "item-success",
			direction:   "pull",
			adapter:     "clockify",
			target:      "AAPP-1",
			action:      "merge",
			payloadJSON: `[{"issue_key":"AAPP-1","started_at_utc":"2026-05-01T08:00:00Z","duration_seconds":3600,"description":"Sync docs"}]`,
			attempts: []savedAttemptSeed{
				{state: "failed", createdAt: "2026-05-02T10:00:00Z"},
			},
		})

		result := runCLI(t, "plan", "retry", "plan-success", "--only", "failed", "--output", "json")
		if result.code != 0 {
			t.Fatalf("expected success exit 0, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		payload := decodeJSONMap(t, []byte(result.stdout))
		if payload["retry_scope"] != "failed" || payload["applied_count"].(float64) != 1 {
			t.Fatalf("unexpected success payload %#v", payload)
		}
	})

	t.Run("failed", func(t *testing.T) {
		seedSavedPlan(t, savedPlanSeed{
			planID:      "plan-failed",
			fingerprint: fingerprint,
			itemID:      "item-failed",
			direction:   "push",
			adapter:     "unsupported",
			target:      "AAPP-2",
			action:      "create",
			payloadJSON: `[{"issue_key":"AAPP-2","started_at_utc":"2026-05-01T09:00:00Z","duration_seconds":3600,"description":"Will fail"}]`,
			attempts: []savedAttemptSeed{
				{state: "failed", createdAt: "2026-05-02T09:00:00Z"},
			},
		})

		result := runCLI(t, "plan", "retry", "plan-failed", "--only", "failed", "--output", "json")
		if result.code != 1 {
			t.Fatalf("expected failed exit 1, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		payload := decodeJSONMap(t, []byte(result.stdout))
		if payload["failed_count"].(float64) != 1 {
			t.Fatalf("unexpected failed payload %#v", payload)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		seedSavedPlan(t, savedPlanSeed{
			planID:      "plan-mixed",
			fingerprint: fingerprint,
			items: []savedPlanItemSeed{
				{
					itemID:      "item-mixed-pull",
					direction:   "pull",
					adapter:     "clockify",
					target:      "AAPP-3",
					action:      "merge",
					payloadJSON: `[{"issue_key":"AAPP-3","started_at_utc":"2026-05-01T10:00:00Z","duration_seconds":3600,"description":"Pull ok"}]`,
					attempts: []savedAttemptSeed{
						{state: "failed", createdAt: "2026-05-02T10:00:00Z"},
					},
				},
				{
					itemID:      "item-mixed-push",
					direction:   "push",
					adapter:     "unsupported",
					target:      "AAPP-4",
					action:      "create",
					payloadJSON: `[{"issue_key":"AAPP-4","started_at_utc":"2026-05-01T11:00:00Z","duration_seconds":3600,"description":"Push bad"}]`,
					attempts: []savedAttemptSeed{
						{state: "failed", createdAt: "2026-05-02T10:00:00Z"},
					},
				},
			},
		})

		result := runCLI(t, "plan", "retry", "plan-mixed", "--only", "failed", "--output", "json")
		if result.code != 6 {
			t.Fatalf("expected mixed exit 6, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		payload := decodeJSONMap(t, []byte(result.stdout))
		if payload["mixed_result"] != true {
			t.Fatalf("unexpected mixed payload %#v", payload)
		}
	})
}

func TestPlanProgressModesKeepStdoutStable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	effective, err := config.LoadEffective()
	if err != nil {
		t.Fatalf("load effective config: %v", err)
	}
	fingerprint, err := config.FingerprintEffective(effective)
	if err != nil {
		t.Fatalf("fingerprint config: %v", err)
	}

	t.Run("apply plain writes stderr only", func(t *testing.T) {
		seedSavedPlan(t, savedPlanSeed{
			planID:      "plan-progress-apply",
			fingerprint: fingerprint,
			itemID:      "item-progress-apply",
			direction:   "pull",
			adapter:     "clockify",
			target:      "AAPP-1",
			action:      "merge",
			payloadJSON: `[{"issue_key":"AAPP-1","started_at_utc":"2026-05-01T08:00:00Z","duration_seconds":3600,"description":"Sync docs"}]`,
		})

		result := runCLI(t, "plan", "apply", "plan-progress-apply", "--progress", "plain", "--output", "json")
		if result.code != 0 {
			t.Fatalf("apply failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		decodeJSONMap(t, []byte(result.stdout))
		if !strings.Contains(result.stderr, "[progress]") {
			t.Fatalf("expected progress on stderr, got %q", result.stderr)
		}
		if strings.Contains(result.stdout, "[progress]") {
			t.Fatalf("expected stdout to remain pure json, got %q", result.stdout)
		}
	})

	t.Run("retry auto stays silent on non tty", func(t *testing.T) {
		seedSavedPlan(t, savedPlanSeed{
			planID:      "plan-progress-retry",
			fingerprint: fingerprint,
			itemID:      "item-progress-retry",
			direction:   "pull",
			adapter:     "clockify",
			target:      "AAPP-2",
			action:      "merge",
			payloadJSON: `[{"issue_key":"AAPP-2","started_at_utc":"2026-05-01T09:00:00Z","duration_seconds":3600,"description":"Retry me"}]`,
			attempts: []savedAttemptSeed{
				{state: "failed", createdAt: "2026-05-02T10:00:00Z"},
			},
		})

		result := runCLI(t, "plan", "retry", "plan-progress-retry", "--only", "failed", "--progress", "auto", "--output", "json")
		if result.code != 0 {
			t.Fatalf("retry failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
		decodeJSONMap(t, []byte(result.stdout))
		if result.stderr != "" {
			t.Fatalf("expected auto progress to stay silent on non-tty stderr, got %q", result.stderr)
		}
	})
}

func TestPlanReconcilePlainProgressWritesStderrOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--from", "2026-04-01", "--to", "2026-04-30", "--progress", "plain", "--output", "json")
	if result.code != 0 {
		t.Fatalf("reconcile failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	decodeJSONMap(t, []byte(result.stdout))
	if !strings.Contains(result.stderr, "[progress]") {
		t.Fatalf("expected progress on stderr, got %q", result.stderr)
	}
	if strings.Contains(result.stdout, "[progress]") {
		t.Fatalf("expected stdout to remain pure json, got %q", result.stdout)
	}
}

func TestTotalsProgressWritesStderrOnlyAndRespectsModes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockify(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	plain := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--progress", "plain", "--output", "json")
	if plain.code != 0 {
		t.Fatalf("plain totals failed: code=%d stdout=%s stderr=%s", plain.code, plain.stdout, plain.stderr)
	}
	decodeJSONMap(t, []byte(plain.stdout))
	if !strings.Contains(plain.stderr, "[progress]") {
		t.Fatalf("expected plain progress on stderr, got %q", plain.stderr)
	}
	if strings.Contains(plain.stdout, "[progress]") {
		t.Fatalf("expected stdout to remain pure json, got %q", plain.stdout)
	}

	bar := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--progress", "bar", "--output", "json")
	if bar.code != 0 {
		t.Fatalf("bar totals failed: code=%d stdout=%s stderr=%s", bar.code, bar.stdout, bar.stderr)
	}
	decodeJSONMap(t, []byte(bar.stdout))
	if !strings.Contains(bar.stderr, "[progress]") {
		t.Fatalf("expected bar to fall back to plain progress on non-tty stderr, got %q", bar.stderr)
	}
	if strings.Contains(bar.stdout, "[progress]") {
		t.Fatalf("expected stdout to remain pure json, got %q", bar.stdout)
	}

	off := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--progress", "off", "--output", "json")
	if off.code != 0 {
		t.Fatalf("off totals failed: code=%d stdout=%s stderr=%s", off.code, off.stdout, off.stderr)
	}
	decodeJSONMap(t, []byte(off.stdout))
	if off.stderr != "" {
		t.Fatalf("expected off progress to stay silent, got %q", off.stderr)
	}

	auto := runCLI(t, "totals", "--adapter", "clockify", "--from", "2026-04-02", "--to", "2026-04-02", "--progress", "auto", "--output", "json")
	if auto.code != 0 {
		t.Fatalf("auto totals failed: code=%d stdout=%s stderr=%s", auto.code, auto.stdout, auto.stderr)
	}
	decodeJSONMap(t, []byte(auto.stdout))
	if auto.stderr != "" {
		t.Fatalf("expected auto progress to stay silent on non-tty stderr, got %q", auto.stderr)
	}
}

func TestProgressReporterForMode(t *testing.T) {
	t.Run("bar on tty uses tty reporter", func(t *testing.T) {
		reporter, err := progressReporterForMode(progress.ModeBar, true, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := fmt.Sprintf("%T", reporter); got != "*progress.ttyReporter" {
			t.Fatalf("expected tty reporter, got %s", got)
		}
	})

	t.Run("bar on non tty falls back to plain reporter", func(t *testing.T) {
		reporter, err := progressReporterForMode(progress.ModeBar, false, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := fmt.Sprintf("%T", reporter); got != "*progress.plainReporter" {
			t.Fatalf("expected plain reporter, got %s", got)
		}
	})

	t.Run("auto on non tty stays noop", func(t *testing.T) {
		reporter, err := progressReporterForMode(progress.ModeAuto, false, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := fmt.Sprintf("%T", reporter); got != "progress.noopReporter" {
			t.Fatalf("expected noop reporter, got %s", got)
		}
	})
}

func TestParsePlanWindowAtWeekShortcutsAndRelativeDates(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	todayFrom, todayTo, err := parsePlanWindowAt(cfg, true, false, false, false, false, false, false, false, false, false, false, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("today parse failed: %v", err)
	}
	if got := todayFrom.Format(time.RFC3339); got != "2026-05-06T00:00:00Z" {
		t.Fatalf("unexpected today from %s", got)
	}
	if got := todayTo.Format(time.RFC3339); got != "2026-05-06T23:59:59Z" {
		t.Fatalf("unexpected today to %s", got)
	}

	yesterdayFrom, yesterdayTo, err := parsePlanWindowAt(cfg, false, true, false, false, false, false, false, false, false, false, false, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("yesterday parse failed: %v", err)
	}
	if got := yesterdayFrom.Format(time.RFC3339); got != "2026-05-05T00:00:00Z" {
		t.Fatalf("unexpected yesterday from %s", got)
	}
	if got := yesterdayTo.Format(time.RFC3339); got != "2026-05-05T23:59:59Z" {
		t.Fatalf("unexpected yesterday to %s", got)
	}

	currentFrom, currentTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, true, false, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("current week parse failed: %v", err)
	}
	if got := currentFrom.Format(time.RFC3339); got != "2026-05-04T00:00:00Z" {
		t.Fatalf("unexpected current week from %s", got)
	}
	if got := currentTo.Format(time.RFC3339); got != "2026-05-10T23:59:59Z" {
		t.Fatalf("unexpected current week to %s", got)
	}

	lastFrom, lastTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, true, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("last week parse failed: %v", err)
	}
	if got := lastFrom.Format(time.RFC3339); got != "2026-04-27T00:00:00Z" {
		t.Fatalf("unexpected last week from %s", got)
	}
	if got := lastTo.Format(time.RFC3339); got != "2026-05-03T23:59:59Z" {
		t.Fatalf("unexpected last week to %s", got)
	}

	currentMonthFrom, currentMonthTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, false, true, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("current month parse failed: %v", err)
	}
	if got := currentMonthFrom.Format(time.RFC3339); got != "2026-05-01T00:00:00Z" {
		t.Fatalf("unexpected current month from %s", got)
	}
	if got := currentMonthTo.Format(time.RFC3339); got != "2026-05-31T23:59:59Z" {
		t.Fatalf("unexpected current month to %s", got)
	}

	lastMonthFrom, lastMonthTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, false, false, true, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("last month parse failed: %v", err)
	}
	if got := lastMonthFrom.Format(time.RFC3339); got != "2026-04-01T00:00:00Z" {
		t.Fatalf("unexpected last month from %s", got)
	}
	if got := lastMonthTo.Format(time.RFC3339); got != "2026-04-30T23:59:59Z" {
		t.Fatalf("unexpected last month to %s", got)
	}

	januaryNow := func() time.Time {
		return time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)
	}
	lastMonthFrom, lastMonthTo, err = parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, false, false, true, "", "", 0, false, januaryNow)
	if err != nil {
		t.Fatalf("year-boundary last month parse failed: %v", err)
	}
	if got := lastMonthFrom.Format(time.RFC3339); got != "2025-12-01T00:00:00Z" {
		t.Fatalf("unexpected year-boundary last month from %s", got)
	}
	if got := lastMonthTo.Format(time.RFC3339); got != "2025-12-31T23:59:59Z" {
		t.Fatalf("unexpected year-boundary last month to %s", got)
	}

	relativeFrom, relativeTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, false, false, false, "-2d", "today", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("relative parse failed: %v", err)
	}
	if got := relativeFrom.Format(time.RFC3339); got != "2026-05-04T00:00:00Z" {
		t.Fatalf("unexpected relative from %s", got)
	}
	if got := relativeTo.Format(time.RFC3339); got != "2026-05-06T23:59:59Z" {
		t.Fatalf("unexpected relative to %s", got)
	}

	if _, _, err := parsePlanWindowAt(cfg, true, false, false, false, false, false, false, false, false, true, false, false, false, "", "", 0, false, fixedNow); err == nil {
		t.Fatal("expected mutually exclusive shortcut validation error")
	}
}

func TestParsePlanWindowAtWeekdayShortcuts(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	mondayFrom, mondayTo, err := parsePlanWindowAt(cfg, false, false, true, false, false, false, false, false, false, false, false, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("monday parse failed: %v", err)
	}
	if got := mondayFrom.Format(time.RFC3339); got != "2026-05-04T00:00:00Z" {
		t.Fatalf("unexpected monday from %s", got)
	}
	if got := mondayTo.Format(time.RFC3339); got != "2026-05-04T23:59:59Z" {
		t.Fatalf("unexpected monday to %s", got)
	}

	sundayFrom, sundayTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, true, false, false, false, false, "", "", 0, false, fixedNow)
	if err != nil {
		t.Fatalf("sunday parse failed: %v", err)
	}
	if got := sundayFrom.Format(time.RFC3339); got != "2026-05-10T00:00:00Z" {
		t.Fatalf("unexpected sunday from %s", got)
	}
	if got := sundayTo.Format(time.RFC3339); got != "2026-05-10T23:59:59Z" {
		t.Fatalf("unexpected sunday to %s", got)
	}
}

func TestParsePlanWindowAtWeekdayShortcutsWithWeekOffset(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	previousMondayFrom, previousMondayTo, err := parsePlanWindowAt(cfg, false, false, true, false, false, false, false, false, false, false, false, false, false, "", "", -1, true, fixedNow)
	if err != nil {
		t.Fatalf("previous monday parse failed: %v", err)
	}
	if got := previousMondayFrom.Format(time.RFC3339); got != "2026-04-27T00:00:00Z" {
		t.Fatalf("unexpected previous monday from %s", got)
	}
	if got := previousMondayTo.Format(time.RFC3339); got != "2026-04-27T23:59:59Z" {
		t.Fatalf("unexpected previous monday to %s", got)
	}

	nextFridayFrom, nextFridayTo, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, true, false, false, false, false, false, false, "", "", 1, true, fixedNow)
	if err != nil {
		t.Fatalf("next friday parse failed: %v", err)
	}
	if got := nextFridayFrom.Format(time.RFC3339); got != "2026-05-15T00:00:00Z" {
		t.Fatalf("unexpected next friday from %s", got)
	}
	if got := nextFridayTo.Format(time.RFC3339); got != "2026-05-15T23:59:59Z" {
		t.Fatalf("unexpected next friday to %s", got)
	}
}

func TestParsePlanWindowAtRejectsInvalidWeekOffsetUsage(t *testing.T) {
	cfg := config.EffectiveConfig{Location: time.UTC}
	fixedNow := func() time.Time {
		return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	}

	if _, _, err := parsePlanWindowAt(cfg, false, false, false, false, false, false, false, false, false, false, false, false, false, "", "", -1, true, fixedNow); err == nil || err.Error() != worklogs.WeekOffsetRequiresWeekdayMessage() {
		t.Fatalf("expected exact week-offset weekday error, got %v", err)
	}
	if _, _, err := parsePlanWindowAt(cfg, true, false, false, false, false, false, false, false, false, false, false, false, false, "", "", -1, true, fixedNow); err == nil || !strings.Contains(err.Error(), "only modifies weekday filters") {
		t.Fatalf("expected standalone-selector conflict, got %v", err)
	}
}

func TestPlanReconcileAcceptsShortcutFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	today := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--today", "--output", "json")
	if today.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", today.code, today.stdout, today.stderr)
	}
	if strings.Contains(today.stderr, "unknown flag: --today") || strings.Contains(today.stdout, "unknown flag: --today") {
		t.Fatalf("expected --today to be recognized, got stdout=%s stderr=%s", today.stdout, today.stderr)
	}

	yesterday := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--yesterday", "--output", "json")
	if yesterday.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", yesterday.code, yesterday.stdout, yesterday.stderr)
	}
	if strings.Contains(yesterday.stderr, "unknown flag: --yesterday") || strings.Contains(yesterday.stdout, "unknown flag: --yesterday") {
		t.Fatalf("expected --yesterday to be recognized, got stdout=%s stderr=%s", yesterday.stdout, yesterday.stderr)
	}

	monday := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--mon", "--output", "json")
	if monday.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", monday.code, monday.stdout, monday.stderr)
	}
	if strings.Contains(monday.stderr, "unknown flag: --mon") || strings.Contains(monday.stdout, "unknown flag: --mon") {
		t.Fatalf("expected --mon to be recognized, got stdout=%s stderr=%s", monday.stdout, monday.stderr)
	}

	currentMonth := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--current-month", "--output", "json")
	if currentMonth.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", currentMonth.code, currentMonth.stdout, currentMonth.stderr)
	}
	if strings.Contains(currentMonth.stderr, "unknown flag: --current-month") || strings.Contains(currentMonth.stdout, "unknown flag: --current-month") {
		t.Fatalf("expected --current-month to be recognized, got stdout=%s stderr=%s", currentMonth.stdout, currentMonth.stderr)
	}

	lastMonth := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--last-month", "--output", "json")
	if lastMonth.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", lastMonth.code, lastMonth.stdout, lastMonth.stderr)
	}
	if strings.Contains(lastMonth.stderr, "unknown flag: --last-month") || strings.Contains(lastMonth.stdout, "unknown flag: --last-month") {
		t.Fatalf("expected --last-month to be recognized, got stdout=%s stderr=%s", lastMonth.stdout, lastMonth.stderr)
	}

	previousMonday := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--mon", "--week-offset", "-1", "--output", "json")
	if previousMonday.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", previousMonday.code, previousMonday.stdout, previousMonday.stderr)
	}
	if strings.Contains(previousMonday.stderr, "unknown flag: --week-offset") || strings.Contains(previousMonday.stdout, "unknown flag: --week-offset") {
		t.Fatalf("expected --week-offset to be recognized, got stdout=%s stderr=%s", previousMonday.stdout, previousMonday.stderr)
	}
}

func TestPlanListSupportsDateWindowSelectors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	if init := runCLI(t, "init", "--output", "json"); init.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", init.code, init.stdout, init.stderr)
	}

	seedSavedPlan(t, savedPlanSeed{
		planID:      "plan-old",
		fingerprint: "fp",
		itemID:      "item-old",
		direction:   "pull",
		adapter:     "clockify",
		target:      "AAPP-1",
		action:      "merge",
		payloadJSON: "[]",
		createdAt:   time.Now().UTC().AddDate(0, 0, -2).Format(time.RFC3339),
	})
	seedSavedPlan(t, savedPlanSeed{
		planID:      "plan-today",
		fingerprint: "fp",
		itemID:      "item-today",
		direction:   "pull",
		adapter:     "clockify",
		target:      "AAPP-2",
		action:      "merge",
		payloadJSON: "[]",
		createdAt:   time.Now().UTC().Format(time.RFC3339),
	})

	result := runCLI(t, "plan", "list", "--today", "--output", "json")
	if result.code != 0 {
		t.Fatalf("plan list failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := decodeJSONMap(t, []byte(result.stdout))
	items, ok := payload["plans"].([]any)
	if !ok {
		t.Fatalf("expected plans array, got %s", result.stdout)
	}
	if len(items) != 1 {
		t.Fatalf("expected one plan for today, got %s", result.stdout)
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected plan object, got %T", items[0])
	}
	if item["plan_id"] != "plan-today" {
		t.Fatalf("expected only today's plan, got %s", result.stdout)
	}
}

func TestTotalsAcceptsMonthShortcutFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	currentMonth := runCLI(t, "totals", "--adapter", "clockify", "--current-month", "--output", "json")
	if currentMonth.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", currentMonth.code, currentMonth.stdout, currentMonth.stderr)
	}
	if strings.Contains(currentMonth.stderr, "unknown flag: --current-month") || strings.Contains(currentMonth.stdout, "unknown flag: --current-month") {
		t.Fatalf("expected --current-month to be recognized, got stdout=%s stderr=%s", currentMonth.stdout, currentMonth.stderr)
	}

	friday := runCLI(t, "totals", "--adapter", "clockify", "--fri", "--output", "json")
	if friday.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", friday.code, friday.stdout, friday.stderr)
	}
	if strings.Contains(friday.stderr, "unknown flag: --fri") || strings.Contains(friday.stdout, "unknown flag: --fri") {
		t.Fatalf("expected --fri to be recognized, got stdout=%s stderr=%s", friday.stdout, friday.stderr)
	}

	lastMonth := runCLI(t, "totals", "--adapter", "clockify", "--last-month", "--output", "json")
	if lastMonth.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", lastMonth.code, lastMonth.stdout, lastMonth.stderr)
	}
	if strings.Contains(lastMonth.stderr, "unknown flag: --last-month") || strings.Contains(lastMonth.stdout, "unknown flag: --last-month") {
		t.Fatalf("expected --last-month to be recognized, got stdout=%s stderr=%s", lastMonth.stdout, lastMonth.stderr)
	}

	nextFriday := runCLI(t, "totals", "--adapter", "clockify", "--fri", "--week-offset", "1", "--output", "json")
	if nextFriday.code != 2 {
		t.Fatalf("expected validation error for missing clockify config, got code=%d stdout=%s stderr=%s", nextFriday.code, nextFriday.stdout, nextFriday.stderr)
	}
	if strings.Contains(nextFriday.stderr, "unknown flag: --week-offset") || strings.Contains(nextFriday.stdout, "unknown flag: --week-offset") {
		t.Fatalf("expected --week-offset to be recognized, got stdout=%s stderr=%s", nextFriday.stdout, nextFriday.stderr)
	}
}

func TestPlanReconcilePushTodayPersistsEmptyPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockifyPush(t)

	server := newClockifyTestServer(t)
	defer server.Close()
	originalBaseURL := clockifyadapter.BaseURL
	clockifyadapter.BaseURL = server.URL
	defer func() { clockifyadapter.BaseURL = originalBaseURL }()

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	reconcileResult := runCLI(t, "plan", "reconcile", "--push", "--adapter", "clockify", "--today", "--output", "json")
	if reconcileResult.code != 0 {
		t.Fatalf("push reconcile failed: code=%d stdout=%s stderr=%s", reconcileResult.code, reconcileResult.stdout, reconcileResult.stderr)
	}
	payload := decodeJSONMap(t, []byte(reconcileResult.stdout))
	if payload["plan_direction"].(string) != "push" {
		t.Fatalf("expected push plan, got %s", reconcileResult.stdout)
	}
	summary := payload["summary"].(map[string]any)
	if summary["total_items"].(float64) != 0 {
		t.Fatalf("expected empty push plan, got %s", reconcileResult.stdout)
	}
}

func TestPlanReconcileRejectsOnlyDeletedWithPull(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTCAndClockifyPush(t)

	result := runCLI(t, "plan", "reconcile", "--pull", "--adapter", "clockify", "--today", "--only-deleted", "--output", "json")
	if result.code != 2 {
		t.Fatalf("expected validation failure, got code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

func TestAddUpdateDeleteAndTombstoneFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "  Investigated   bug  ", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	added := decodeJSONMap(t, []byte(add.stdout))
	id := added["id"].(string)

	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "id", "--output", "json")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	listPayload := decodeJSONMap(t, []byte(list.stdout))
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one active worklog, got %#v", listPayload["items"])
	}
	listItem := items[0].(map[string]any)
	if listItem["id"].(string) != id {
		t.Fatalf("expected listed worklog id %s, got %s", id, listItem["id"].(string))
	}

	update := runCLI(t, "worklogs", "update", id, "--duration", "2h", "--output", "json")
	if update.code != 0 {
		t.Fatalf("update failed: code=%d stdout=%s stderr=%s", update.code, update.stdout, update.stderr)
	}

	del := runCLI(t, "worklogs", "delete", id, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	afterDelete := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "id", "--output", "json")
	if afterDelete.code != 0 {
		t.Fatalf("list after delete failed: code=%d stdout=%s stderr=%s", afterDelete.code, afterDelete.stdout, afterDelete.stderr)
	}
	afterDeletePayload := decodeJSONMap(t, []byte(afterDelete.stdout))
	afterDeleteItems := afterDeletePayload["items"].([]any)
	if len(afterDeleteItems) != 0 {
		t.Fatalf("expected no active worklogs after delete, got %#v", afterDeletePayload["items"])
	}

	deleted := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--output", "json")
	if deleted.code != 0 {
		t.Fatalf("deleted list failed: code=%d stdout=%s stderr=%s", deleted.code, deleted.stdout, deleted.stderr)
	}
	payload := decodeJSONMap(t, []byte(deleted.stdout))
	deletedItems := payload["items"].([]any)
	if len(deletedItems) != 1 {
		t.Fatalf("expected one deleted tombstone, got %#v", payload["items"])
	}
	if got := readSingleTombstoneDescription(t); got != "Investigated bug" {
		t.Fatalf("expected tombstone description to be preserved, got %q", got)
	}
}

func TestHardDeleteSkipsTombstone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "Investigated bug", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	id := decodeJSONMap(t, []byte(add.stdout))["id"].(string)

	del := runCLI(t, "worklogs", "delete", id, "--hard", "--output", "json")
	if del.code != 0 {
		t.Fatalf("hard delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}
	deletedPayload := decodeJSONMap(t, []byte(del.stdout))
	if deletedPayload["hard_delete"] != true {
		t.Fatalf("expected hard_delete=true, got %#v", deletedPayload["hard_delete"])
	}

	afterDelete := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "id", "--output", "json")
	if afterDelete.code != 0 {
		t.Fatalf("list after hard delete failed: code=%d stdout=%s stderr=%s", afterDelete.code, afterDelete.stdout, afterDelete.stderr)
	}
	afterDeletePayload := decodeJSONMap(t, []byte(afterDelete.stdout))
	afterDeleteItems := afterDeletePayload["items"].([]any)
	if len(afterDeleteItems) != 0 {
		t.Fatalf("expected no active worklogs after hard delete, got %#v", afterDeletePayload["items"])
	}

	deleted := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--output", "json")
	if deleted.code != 0 {
		t.Fatalf("deleted list failed: code=%d stdout=%s stderr=%s", deleted.code, deleted.stdout, deleted.stderr)
	}
	payload := decodeJSONMap(t, []byte(deleted.stdout))
	items := payload["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected no deleted tombstones after hard delete, got %#v", payload["items"])
	}
}

func TestOverlapConflictAndBatchDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Investigated bug", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	conflict := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "1h", "--description", "Second task", "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected overlap conflict exit 2, got %d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}

	dry := runCLI(t, "worklogs", "delete", "--issue", "ABC-123", "--dry", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("dry batch delete failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}

	exec := runCLI(t, "worklogs", "delete", "--issue", "ABC-123", "--yes", "--output", "json")
	if exec.code != 0 {
		t.Fatalf("batch delete failed: code=%d stdout=%s stderr=%s", exec.code, exec.stdout, exec.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	payload := decodeJSONMap(t, []byte(list.stdout))
	if payload["total"].(float64) != 0 {
		t.Fatalf("expected zero active rows, got %#v", payload["total"])
	}
	if got := readSingleTombstoneDescription(t); got != "Investigated bug" {
		t.Fatalf("expected batch tombstone description to be preserved, got %q", got)
	}
}

func TestBatchHardDeleteSkipsTombstones(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Investigated bug", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	dry := runCLI(t, "worklogs", "delete", "--issue", "ABC-123", "--dry", "--hard", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("dry batch hard delete failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	dryPayload := decodeJSONMap(t, []byte(dry.stdout))
	if dryPayload["hard_delete"] != true {
		t.Fatalf("expected dry hard_delete=true, got %#v", dryPayload["hard_delete"])
	}

	exec := runCLI(t, "worklogs", "delete", "--issue", "ABC-123", "--yes", "--hard", "--output", "json")
	if exec.code != 0 {
		t.Fatalf("batch hard delete failed: code=%d stdout=%s stderr=%s", exec.code, exec.stdout, exec.stderr)
	}
	execPayload := decodeJSONMap(t, []byte(exec.stdout))
	if execPayload["hard_delete"] != true {
		t.Fatalf("expected exec hard_delete=true, got %#v", execPayload["hard_delete"])
	}

	deleted := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--output", "json")
	if deleted.code != 0 {
		t.Fatalf("deleted list failed: code=%d stdout=%s stderr=%s", deleted.code, deleted.stdout, deleted.stderr)
	}
	payload := decodeJSONMap(t, []byte(deleted.stdout))
	items := payload["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected no tombstones after batch hard delete, got %#v", payload["items"])
	}
}

func TestWorklogsRestoreDryRunAndExecute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Restorable item", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	id := decodeJSONMap(t, []byte(add.stdout))["id"].(string)

	del := runCLI(t, "worklogs", "delete", id, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	dry := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--dry", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("restore dry-run failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	dryPayload := decodeJSONMap(t, []byte(dry.stdout))
	if dryPayload["dry_run"] != true {
		t.Fatalf("expected dry_run=true, got %#v", dryPayload["dry_run"])
	}
	if dryPayload["matched"].(float64) != 1 {
		t.Fatalf("expected matched=1, got %#v", dryPayload["matched"])
	}
	dryItems := dryPayload["items"].([]any)
	preview := dryItems[0].(map[string]any)
	if preview["restore_preview"] != true {
		t.Fatalf("expected restore_preview=true, got %#v", preview["restore_preview"])
	}
	if preview["description"].(string) != "Restorable item" {
		t.Fatalf("expected preserved description, got %#v", preview["description"])
	}

	before := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(before.stdout))["total"].(float64) != 0 {
		t.Fatalf("restore dry-run should not recreate active rows")
	}

	exec := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--output", "json")
	if exec.code != 0 {
		t.Fatalf("restore execute failed: code=%d stdout=%s stderr=%s", exec.code, exec.stdout, exec.stderr)
	}
	execPayload := decodeJSONMap(t, []byte(exec.stdout))
	if execPayload["dry_run"] != false {
		t.Fatalf("expected dry_run=false, got %#v", execPayload["dry_run"])
	}
	if execPayload["restored"].(float64) != 1 {
		t.Fatalf("expected restored=1, got %#v", execPayload["restored"])
	}
	if execPayload["items"].([]any)[0].(map[string]any)["id"].(string) != id {
		t.Fatalf("expected restored id %s, got %#v", id, execPayload["items"])
	}

	active := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	activePayload := decodeJSONMap(t, []byte(active.stdout))
	if activePayload["total"].(float64) != 1 {
		t.Fatalf("expected one restored active row, got %s", active.stdout)
	}
	record := activePayload["items"].([]any)[0].(map[string]any)
	if record["id"].(string) != id || record["description"].(string) != "Restorable item" {
		t.Fatalf("expected original record values restored, got %#v", record)
	}

	deleted := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--output", "json")
	if decodeJSONMap(t, []byte(deleted.stdout))["total"].(float64) != 0 {
		t.Fatalf("expected restore to consume tombstone, got %s", deleted.stdout)
	}
}

func TestWorklogsRestoreDryRunWithIssueAndZeroMatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "Restorable item", "--output", "json")
	id := decodeJSONMap(t, []byte(add.stdout))["id"].(string)
	del := runCLI(t, "worklogs", "delete", id, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	dry := runCLI(t, "worklogs", "restore", "--issue", "ABC-123", "--from", "2026-05-03", "--to", "2026-05-03", "--dry", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("restore dry-run with issue failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	if decodeJSONMap(t, []byte(dry.stdout))["matched"].(float64) != 1 {
		t.Fatalf("expected one issue-filtered match, got %s", dry.stdout)
	}

	zero := runCLI(t, "worklogs", "restore", "--issue", "ABC-999", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--output", "json")
	if zero.code != 0 {
		t.Fatalf("zero-match restore failed: code=%d stdout=%s stderr=%s", zero.code, zero.stdout, zero.stderr)
	}
	zeroPayload := decodeJSONMap(t, []byte(zero.stdout))
	if zeroPayload["restored"].(float64) != 0 {
		t.Fatalf("expected restored=0, got %#v", zeroPayload["restored"])
	}
}

func TestWorklogsRestoreRejectsConflictsUnlessForced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "To delete", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	firstID := decodeJSONMap(t, []byte(first.stdout))["id"].(string)

	del := runCLI(t, "worklogs", "delete", firstID, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "1h", "--description", "Conflicting active", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	conflict := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected restore conflict exit 2, got %d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}

	active := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	activePayload := decodeJSONMap(t, []byte(active.stdout))
	if activePayload["total"].(float64) != 1 {
		t.Fatalf("conflicting restore should be atomic, got %s", active.stdout)
	}
	if activePayload["items"].([]any)[0].(map[string]any)["id"].(string) == firstID {
		t.Fatalf("conflicting restore should not recreate deleted row, got %s", active.stdout)
	}
	deleted := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--output", "json")
	if decodeJSONMap(t, []byte(deleted.stdout))["total"].(float64) != 1 {
		t.Fatalf("conflicting restore should keep tombstone, got %s", deleted.stdout)
	}

	forced := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--force", "--output", "json")
	if forced.code != 0 {
		t.Fatalf("forced restore failed: code=%d stdout=%s stderr=%s", forced.code, forced.stdout, forced.stderr)
	}
	forcedActive := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(forcedActive.stdout))["total"].(float64) != 2 {
		t.Fatalf("forced restore should recreate deleted row, got %s", forcedActive.stdout)
	}
}

func TestWorklogsRestoreRejectsConflictsWithinRestoreSetUnlessForced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T06:30:00Z", "--duration", "1h", "--description", "Second", "--force", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	for _, id := range []string{
		decodeJSONMap(t, []byte(first.stdout))["id"].(string),
		decodeJSONMap(t, []byte(second.stdout))["id"].(string),
	} {
		deleted := runCLI(t, "worklogs", "delete", id, "--output", "json")
		if deleted.code != 0 {
			t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", deleted.code, deleted.stdout, deleted.stderr)
		}
	}

	conflict := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected restore-set conflict exit 2, got %d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}

	active := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(active.stdout))["total"].(float64) != 0 {
		t.Fatalf("restore-set conflict should be atomic, got %s", active.stdout)
	}

	forced := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--yes", "--force", "--output", "json")
	if forced.code != 0 {
		t.Fatalf("forced restore-set execution failed: code=%d stdout=%s stderr=%s", forced.code, forced.stdout, forced.stderr)
	}
	if decodeJSONMap(t, []byte(forced.stdout))["restored"].(float64) != 2 {
		t.Fatalf("expected restored=2, got %s", forced.stdout)
	}
}

func TestWorklogsRestoreValidationErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	noSelector := runCLI(t, "worklogs", "restore", "--yes", "--output", "json")
	if noSelector.code != 2 {
		t.Fatalf("expected no-selector validation error, got %d stdout=%s stderr=%s", noSelector.code, noSelector.stdout, noSelector.stderr)
	}

	both := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--dry", "--yes", "--output", "json")
	if both.code != 2 {
		t.Fatalf("expected dry/yes validation error, got %d stdout=%s stderr=%s", both.code, both.stdout, both.stderr)
	}

	issue := runCLI(t, "worklogs", "restore", "--issue", "bad", "--from", "2026-05-03", "--to", "2026-05-03", "--dry", "--output", "json")
	if issue.code != 2 {
		t.Fatalf("expected issue validation error, got %d stdout=%s stderr=%s", issue.code, issue.stdout, issue.stderr)
	}

	onlyDeleted := runCLI(t, "worklogs", "restore", "--from", "2026-05-03", "--to", "2026-05-03", "--dry", "--only-deleted", "--output", "json")
	if onlyDeleted.code != 1 {
		t.Fatalf("expected unknown-flag failure for --only-deleted, got %d stdout=%s stderr=%s", onlyDeleted.code, onlyDeleted.stdout, onlyDeleted.stderr)
	}
}

func TestWorklogOutputsExcludeMaxEstimateAndIssueMetadataOwnsIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	assertIssueMetadataTableExists(t)
	seedIssueMetadata(t, "ABC-123", int64Ptr(7200))

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "Investigated bug", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	added := decodeJSONMap(t, []byte(add.stdout))
	if _, ok := added["max_estimate_seconds"]; ok {
		t.Fatalf("expected add output without max_estimate_seconds, got %#v", added)
	}

	id := added["id"].(string)
	listByID := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "id,issue_key,started_at,duration_seconds,description", "--output", "json")
	if listByID.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", listByID.code, listByID.stdout, listByID.stderr)
	}
	listed := decodeJSONMap(t, []byte(listByID.stdout))
	listItems := listed["items"].([]any)
	if len(listItems) != 1 {
		t.Fatalf("expected one listed worklog, got %#v", listed["items"])
	}
	listItem := listItems[0].(map[string]any)
	if listItem["id"].(string) != id {
		t.Fatalf("expected listed worklog id %s, got %s", id, listItem["id"].(string))
	}
	if _, ok := listItem["max_estimate_seconds"]; ok {
		t.Fatalf("expected listed output without max_estimate_seconds, got %#v", listItem)
	}

	update := runCLI(t, "worklogs", "update", id, "--duration", "2h", "--output", "json")
	if update.code != 0 {
		t.Fatalf("update failed: code=%d stdout=%s stderr=%s", update.code, update.stdout, update.stderr)
	}
	updated := decodeJSONMap(t, []byte(update.stdout))
	if _, ok := updated["max_estimate_seconds"]; ok {
		t.Fatalf("expected update output without max_estimate_seconds, got %#v", updated)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--fields", "issue_key,max_estimate_seconds", "--output", "json")
	if list.code != 2 {
		t.Fatalf("expected fields validation failure, got code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "max_estimate_seconds") {
		t.Fatalf("expected invalid field error, stdout=%s", list.stdout)
	}

	table := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03")
	if table.code != 0 {
		t.Fatalf("table list failed: code=%d stdout=%s stderr=%s", table.code, table.stdout, table.stderr)
	}
	if bytes.Contains([]byte(table.stdout), []byte("MAX_ESTIMATE")) {
		t.Fatalf("expected no MAX_ESTIMATE column, stdout=%s", table.stdout)
	}

	metadataList := runCLI(t, "issue-metadata", "list", "--issue", "ABC-123", "--output", "json")
	if metadataList.code != 0 {
		t.Fatalf("issue-metadata list failed: code=%d stdout=%s stderr=%s", metadataList.code, metadataList.stdout, metadataList.stderr)
	}
	listPayload := decodeJSONMap(t, []byte(metadataList.stdout))
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one metadata row, got %#v", listPayload)
	}
	record := items[0].(map[string]any)
	if record["max_estimate_seconds"].(float64) != 7200 {
		t.Fatalf("expected issue metadata max_estimate_seconds, got %#v", record["max_estimate_seconds"])
	}

}

func TestWorklogsListTableTruncatesDescription(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	longDescription := strings.Repeat("x", listDescriptionMaxWidth+10)
	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", longDescription, "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}

	expectedDescription := strings.Repeat("x", listDescriptionMaxWidth-3) + "..."
	if !strings.Contains(list.stdout, expectedDescription) {
		t.Fatalf("expected truncated description %q, stdout=%q", expectedDescription, list.stdout)
	}
	if strings.Contains(list.stdout, longDescription) {
		t.Fatalf("expected full description to be truncated, stdout=%q", list.stdout)
	}
	if !strings.Contains(list.stdout, "Totals: 1 worklogs, 1h") {
		t.Fatalf("expected totals footer, stdout=%q", list.stdout)
	}
}

func TestWorklogsListCurrentWeekSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "todayT09:00", "--duration", "1h", "--description", "Current week item", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	currentWeek := runCLI(t, "worklogs", "list", "--current-week", "--output", "json")
	if currentWeek.code != 0 {
		t.Fatalf("current week list failed: code=%d stdout=%s stderr=%s", currentWeek.code, currentWeek.stdout, currentWeek.stderr)
	}
	currentWeekPayload := decodeJSONMap(t, []byte(currentWeek.stdout))
	if currentWeekPayload["total"].(float64) != 1 {
		t.Fatalf("expected one current-week row, got %s", currentWeek.stdout)
	}
	rawFilters := currentWeekPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if rawFilters["current_week"] != true {
		t.Fatalf("expected current_week raw filter, got %#v", rawFilters)
	}

	lastWeek := runCLI(t, "worklogs", "list", "--last-week", "--output", "json")
	if lastWeek.code != 0 {
		t.Fatalf("last week list failed: code=%d stdout=%s stderr=%s", lastWeek.code, lastWeek.stdout, lastWeek.stderr)
	}
	lastWeekPayload := decodeJSONMap(t, []byte(lastWeek.stdout))
	if lastWeekPayload["total"].(float64) != 0 {
		t.Fatalf("expected zero last-week rows, got %s", lastWeek.stdout)
	}
}

func TestWorklogsListMonthSelectorsAndSearchContextJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	now := time.Now().UTC()
	currentMonthDate := time.Date(now.Year(), now.Month(), 3, 9, 0, 0, 0, time.UTC)
	lastMonthDate := currentMonthDate.AddDate(0, -1, 0)
	currentMonthDay := currentMonthDate.Format("2006-01-02")
	lastMonthDay := lastMonthDate.Format("2006-01-02")

	currentAdd := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", currentMonthDay+"T09:00", "--duration", "1h", "--description", "Current month item", "--output", "json")
	if currentAdd.code != 0 {
		t.Fatalf("current month add failed: code=%d stdout=%s stderr=%s", currentAdd.code, currentAdd.stdout, currentAdd.stderr)
	}
	lastAdd := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started", lastMonthDay+"T09:00", "--duration", "30m", "--description", "Last month item", "--output", "json")
	if lastAdd.code != 0 {
		t.Fatalf("last month add failed: code=%d stdout=%s stderr=%s", lastAdd.code, lastAdd.stdout, lastAdd.stderr)
	}

	currentMonth := runCLI(t, "worklogs", "list", "--current-month", "--output", "json")
	if currentMonth.code != 0 {
		t.Fatalf("current month list failed: code=%d stdout=%s stderr=%s", currentMonth.code, currentMonth.stdout, currentMonth.stderr)
	}
	currentPayload := decodeJSONMap(t, []byte(currentMonth.stdout))
	if currentPayload["total"].(float64) != 1 {
		t.Fatalf("expected one current-month row, got %s", currentMonth.stdout)
	}
	currentRaw := currentPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if currentRaw["current_month"] != true || currentRaw["last_month"] != false {
		t.Fatalf("expected current_month raw filter state, got %#v", currentRaw)
	}

	lastMonth := runCLI(t, "worklogs", "list", "--last-month", "--output", "json")
	if lastMonth.code != 0 {
		t.Fatalf("last month list failed: code=%d stdout=%s stderr=%s", lastMonth.code, lastMonth.stdout, lastMonth.stderr)
	}
	lastPayload := decodeJSONMap(t, []byte(lastMonth.stdout))
	if lastPayload["total"].(float64) != 1 {
		t.Fatalf("expected one last-month row, got %s", lastMonth.stdout)
	}
	lastRaw := lastPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if lastRaw["last_month"] != true || lastRaw["current_month"] != false {
		t.Fatalf("expected last_month raw filter state, got %#v", lastRaw)
	}

	search := runCLI(t, "worklogs", "search", "item", "--current-month", "--output", "json")
	if search.code != 0 {
		t.Fatalf("current month search failed: code=%d stdout=%s stderr=%s", search.code, search.stdout, search.stderr)
	}
	searchPayload := decodeJSONMap(t, []byte(search.stdout))
	if searchPayload["total"].(float64) != 1 {
		t.Fatalf("expected one current-month search match, got %s", search.stdout)
	}
	searchRaw := searchPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if searchRaw["current_month"] != true {
		t.Fatalf("expected current_month search raw filter, got %#v", searchRaw)
	}

	contextResult := runCLI(t, "worklogs", "context", "--current-month", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}
	contextPayload := decodeJSONMap(t, []byte(contextResult.stdout))
	contextRaw := contextPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if contextRaw["current_month"] != true || contextRaw["last_month"] != false {
		t.Fatalf("expected current_month context raw filter, got %#v", contextRaw)
	}

	defaultContext := runCLI(t, "worklogs", "context", "--output", "json")
	if defaultContext.code != 0 {
		t.Fatalf("default context failed: code=%d stdout=%s stderr=%s", defaultContext.code, defaultContext.stdout, defaultContext.stderr)
	}
	defaultPayload := decodeJSONMap(t, []byte(defaultContext.stdout))
	defaultRaw := defaultPayload["filters"].(map[string]any)["raw"].(map[string]any)
	if defaultRaw["current_month"] != false || defaultRaw["last_month"] != false {
		t.Fatalf("expected default context raw month selectors to be false, got %#v", defaultRaw)
	}
	defaultEffective := defaultPayload["filters"].(map[string]any)["effective"].(map[string]any)
	if defaultEffective["from"] != now.Format("2006-01-02")+"T00:00:00Z" {
		t.Fatalf("expected default context to preserve today semantics, got %#v", defaultEffective)
	}
}

func TestWorklogsIssuePrefixSelectorAcrossListSearchAndDeleteDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	now := time.Now().UTC()
	currentMonthDate := time.Date(now.Year(), now.Month(), 3, 9, 0, 0, 0, time.UTC)
	currentMonthDay := currentMonthDate.Format("2006-01-02")

	for _, args := range [][]string{
		{"worklogs", "add", "--issue", "IRW-123", "--started", currentMonthDay + "T09:00", "--duration", "1h", "--description", "IRW current month item", "--output", "json"},
		{"worklogs", "add", "--issue", "IRW2-123", "--started", currentMonthDay + "T10:30", "--duration", "30m", "--description", "IRW2 current month item", "--output", "json"},
		{"worklogs", "add", "--issue", "IRW-124", "--started", currentMonthDay + "T11:30", "--duration", "45m", "--description", "IRW second item", "--output", "json"},
	} {
		add := runCLI(t, args...)
		if add.code != 0 {
			t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
		}
	}

	list := runCLI(t, "worklogs", "list", "--issue-prefix", "IRW", "--current-month", "--output", "json")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	listPayload := decodeJSONMap(t, []byte(list.stdout))
	if listPayload["total"].(float64) != 2 {
		t.Fatalf("expected two IRW rows, got %s", list.stdout)
	}
	listFilters := listPayload["filters"].(map[string]any)
	listRaw := listFilters["raw"].(map[string]any)
	listEffective := listFilters["effective"].(map[string]any)
	if listRaw["issue_prefix"] != "IRW" || listEffective["issue_prefix"] != "IRW" {
		t.Fatalf("expected issue prefix filters, got raw=%#v effective=%#v", listRaw, listEffective)
	}

	search := runCLI(t, "worklogs", "search", "item", "--issue-prefix", "IRW", "--current-month", "--output", "json")
	if search.code != 0 {
		t.Fatalf("search failed: code=%d stdout=%s stderr=%s", search.code, search.stdout, search.stderr)
	}
	searchPayload := decodeJSONMap(t, []byte(search.stdout))
	if searchPayload["total"].(float64) != 2 {
		t.Fatalf("expected two IRW search rows, got %s", search.stdout)
	}
	searchFilters := searchPayload["filters"].(map[string]any)
	searchRaw := searchFilters["raw"].(map[string]any)
	searchEffective := searchFilters["effective"].(map[string]any)
	if searchRaw["issue_prefix"] != "IRW" || searchEffective["issue_prefix"] != "IRW" {
		t.Fatalf("expected search issue prefix filters, got raw=%#v effective=%#v", searchRaw, searchEffective)
	}

	deleteDry := runCLI(t, "worklogs", "delete", "--issue-prefix", "IRW", "--current-month", "--dry", "--output", "json")
	if deleteDry.code != 0 {
		t.Fatalf("delete dry-run failed: code=%d stdout=%s stderr=%s", deleteDry.code, deleteDry.stdout, deleteDry.stderr)
	}
	deletePayload := decodeJSONMap(t, []byte(deleteDry.stdout))
	if deletePayload["matched"].(float64) != 2 {
		t.Fatalf("expected two dry-run matches, got %s", deleteDry.stdout)
	}
	deleteFilters := deletePayload["filters"].(map[string]any)
	deleteRaw := deleteFilters["raw"].(map[string]any)
	deleteEffective := deleteFilters["effective"].(map[string]any)
	if deleteRaw["issue_prefix"] != "IRW" || deleteEffective["issue_prefix"] != "IRW" {
		t.Fatalf("expected delete issue prefix filters, got raw=%#v effective=%#v", deleteRaw, deleteEffective)
	}

	invalid := runCLI(t, "worklogs", "list", "--issue-prefix", "irw", "--current-month", "--output", "json")
	if invalid.code != 2 {
		t.Fatalf("expected invalid issue-prefix to fail, got code=%d stdout=%s stderr=%s", invalid.code, invalid.stdout, invalid.stderr)
	}
	if !strings.Contains(invalid.stdout, "issue_prefix") && !strings.Contains(invalid.stderr, "issue_prefix") {
		t.Fatalf("expected issue_prefix validation message, got stdout=%s stderr=%s", invalid.stdout, invalid.stderr)
	}
}

func TestWorklogsListTableFooterIncludesCountAndHumanDuration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "todayT09:00", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started", "todayT10:30", "--duration", "30m", "--description", "Second", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--current-week")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "Totals: 2 worklogs, 1h 30m") {
		t.Fatalf("expected human-readable totals footer, stdout=%q", list.stdout)
	}
}

func TestWorklogsListTableFooterShowsZeroForEmptyResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--current-week")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "Totals: 0 worklogs, 0m") {
		t.Fatalf("expected zero totals footer, stdout=%q", list.stdout)
	}
}

func TestWorklogsSearchWithoutDateSelectorFindsAcrossDays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "1h", "--description", "API docs review", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started", "2026-05-04T09:00", "--duration", "30m", "--description", "api DOCS follow-up", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	search := runCLI(t, "worklogs", "search", "Api Docs", "--output", "json")
	if search.code != 0 {
		t.Fatalf("search failed: code=%d stdout=%s stderr=%s", search.code, search.stdout, search.stderr)
	}
	payload := decodeJSONMap(t, []byte(search.stdout))
	if payload["total"].(float64) != 2 {
		t.Fatalf("expected two matches, got %s", search.stdout)
	}
	filters := payload["filters"].(map[string]any)
	raw := filters["raw"].(map[string]any)
	effective := filters["effective"].(map[string]any)
	if raw["query"].(string) != "Api Docs" {
		t.Fatalf("expected raw query, got %#v", raw["query"])
	}
	if effective["query"].(string) != "Api Docs" {
		t.Fatalf("expected effective query, got %#v", effective["query"])
	}
	items := payload["items"].([]any)
	firstRecord := items[0].(map[string]any)
	secondRecord := items[1].(map[string]any)
	if firstRecord["started_at_utc"].(string) != "2026-05-04T09:00:00Z" || secondRecord["started_at_utc"].(string) != "2026-05-03T09:00:00Z" {
		t.Fatalf("unexpected search ordering: %#v", items)
	}
}

func TestWorklogsSearchValidationZeroMatchLiteralAndDeletedMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	blank := runCLI(t, "worklogs", "search", "   ", "--output", "json")
	if blank.code != 2 {
		t.Fatalf("expected blank-query validation error, got code=%d stdout=%s stderr=%s", blank.code, blank.stdout, blank.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "45m", "--description", "Fix 100%_done behavior", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	id := decodeJSONMap(t, []byte(add.stdout))["id"].(string)

	zero := runCLI(t, "worklogs", "search", "missing", "--issue", "ABC-123", "--today", "--output", "json")
	if zero.code != 0 {
		t.Fatalf("zero-match search failed: code=%d stdout=%s stderr=%s", zero.code, zero.stdout, zero.stderr)
	}
	if decodeJSONMap(t, []byte(zero.stdout))["total"].(float64) != 0 {
		t.Fatalf("expected zero matches, got %s", zero.stdout)
	}

	active := runCLI(t, "worklogs", "search", "%_done", "--from", "2026-05-03", "--to", "2026-05-03")
	if active.code != 0 {
		t.Fatalf("literal search failed: code=%d stdout=%s stderr=%s", active.code, active.stdout, active.stderr)
	}
	if !strings.Contains(active.stdout, "Fix 100%_done behavior") || !strings.Contains(active.stdout, "Totals: 1 worklogs, 45m") {
		t.Fatalf("expected literal active match with totals footer, stdout=%q", active.stdout)
	}

	del := runCLI(t, "worklogs", "delete", id, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	activeAfterDelete := runCLI(t, "worklogs", "search", "%_done", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if activeAfterDelete.code != 0 {
		t.Fatalf("active search after delete failed: code=%d stdout=%s stderr=%s", activeAfterDelete.code, activeAfterDelete.stdout, activeAfterDelete.stderr)
	}
	if decodeJSONMap(t, []byte(activeAfterDelete.stdout))["total"].(float64) != 0 {
		t.Fatalf("expected no active matches after delete, got %s", activeAfterDelete.stdout)
	}

	deleted := runCLI(t, "worklogs", "search", "%_done", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted", "--fields", "issue_key,deleted_at", "--output", "json")
	if deleted.code != 0 {
		t.Fatalf("deleted search failed: code=%d stdout=%s stderr=%s", deleted.code, deleted.stdout, deleted.stderr)
	}
	deletedPayload := decodeJSONMap(t, []byte(deleted.stdout))
	if deletedPayload["total"].(float64) != 1 {
		t.Fatalf("expected one tombstone match, got %s", deleted.stdout)
	}
	deletedFilters := deletedPayload["filters"].(map[string]any)
	if deletedFilters["raw"].(map[string]any)["query"].(string) != "%_done" {
		t.Fatalf("expected raw literal query, got %#v", deletedFilters)
	}
	record := deletedPayload["items"].([]any)[0].(map[string]any)
	if len(record) != 3 {
		t.Fatalf("expected deleted item shape to remain tombstone-style, got %#v", record)
	}
}

func TestWorklogsListTableFooterIgnoresSelectedFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "todayT09:00", "--duration", "1h", "--description", "Field filter", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--current-week", "--fields", "issue_key,description")
	if list.code != 0 {
		t.Fatalf("list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "ISSUE") || !strings.Contains(list.stdout, "DESCRIPTION") {
		t.Fatalf("expected selected columns, stdout=%q", list.stdout)
	}
	if strings.Contains(list.stdout, "DURATION") {
		t.Fatalf("expected duration column to be hidden, stdout=%q", list.stdout)
	}
	if !strings.Contains(list.stdout, "Totals: 1 worklogs, 1h") {
		t.Fatalf("expected totals footer independent of fields, stdout=%q", list.stdout)
	}
}

func TestWorklogsListDeletedTableFooterIncludesCountAndHumanDuration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "2026-05-03T09:00", "--duration", "45m", "--description", "Deleted item", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}
	id := decodeJSONMap(t, []byte(add.stdout))["id"].(string)

	del := runCLI(t, "worklogs", "delete", id, "--output", "json")
	if del.code != 0 {
		t.Fatalf("delete failed: code=%d stdout=%s stderr=%s", del.code, del.stdout, del.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--only-deleted")
	if list.code != 0 {
		t.Fatalf("deleted list failed: code=%d stdout=%s stderr=%s", list.code, list.stdout, list.stderr)
	}
	if !strings.Contains(list.stdout, "Totals: 1 tombstones, 45m") {
		t.Fatalf("expected deleted totals footer, stdout=%q", list.stdout)
	}
}

func TestContextIncludesPlanningMetadataAndCollisions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	seedIssueMetadata(t, "ABC-123", int64Ptr(7200))
	seedIssueMetadata(t, "ABC-124", nil)

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "todayT09:00", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}

	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started", "todayT09:30", "--duration", "1h", "--description", "Second", "--force", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--today", "--issue", "ABC-123", "--issue", "ABC-124", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	payload := decodeJSONMap(t, []byte(contextResult.stdout))
	planning := payload["planning"].(map[string]any)
	issueOrder := planning["issue_order"].([]any)
	if issueOrder[0].(string) != "ABC-123" || issueOrder[1].(string) != "ABC-124" {
		t.Fatalf("unexpected issue order %#v", issueOrder)
	}
	issues := planning["issues"].([]any)
	firstIssue := issues[0].(map[string]any)
	secondIssue := issues[1].(map[string]any)
	if firstIssue["max_estimate_seconds"].(float64) != 7200 {
		t.Fatalf("expected first planning issue max estimate, got %#v", firstIssue["max_estimate_seconds"])
	}
	if secondIssue["max_estimate_seconds"] != nil {
		t.Fatalf("expected second planning issue null max estimate, got %#v", secondIssue["max_estimate_seconds"])
	}

	summary := payload["summary"].(map[string]any)
	if summary["collision_count"].(float64) != 1 {
		t.Fatalf("expected one collision, got %#v", summary["collision_count"])
	}
	if summary["until_quota_seconds"].(float64) != 21600 {
		t.Fatalf("expected 21600 until_quota_seconds, got %#v", summary["until_quota_seconds"])
	}

	settings := payload["settings"].(map[string]any)
	if settings["daily_minimum_quota_seconds"].(float64) != 28800 {
		t.Fatalf("expected daily minimum quota in settings, got %#v", settings["daily_minimum_quota_seconds"])
	}
	lunch := settings["lunch"].(map[string]any)
	if lunch["start"] != "12:00" || lunch["end"] != "12:45" {
		t.Fatalf("expected default lunch in settings, got %#v", lunch)
	}

	days := payload["days"].([]any)
	day := days[0].(map[string]any)
	if day["until_quota_seconds"].(float64) != 21600 {
		t.Fatalf("expected day until_quota_seconds, got %#v", day["until_quota_seconds"])
	}
	worklogs := day["worklogs"].([]any)
	if len(worklogs) != 2 {
		t.Fatalf("expected two worklogs for today, got %#v", worklogs)
	}
	if _, ok := worklogs[0].(map[string]any)["max_estimate_seconds"]; ok {
		t.Fatalf("expected day worklog rows without max_estimate_seconds, got %#v", worklogs[0])
	}
	collisions := day["collisions"].([]any)
	if len(collisions) != 1 {
		t.Fatalf("expected one collision segment, got %#v", collisions)
	}
}

func TestContextTableShowsUntilQuota(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started", "todayT09:00", "--duration", "1h", "--description", "First", "--output", "json")
	if add.code != 0 {
		t.Fatalf("add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--today")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}
	if !strings.Contains(contextResult.stdout, "UNTIL_QUOTA") {
		t.Fatalf("expected UNTIL_QUOTA header, got %s", contextResult.stdout)
	}
	if !strings.Contains(contextResult.stdout, "25200") {
		t.Fatalf("expected quota delta of 25200 seconds, got %s", contextResult.stdout)
	}
}

func TestContextUsesConfiguredDailyLunchByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  daily_minimum_quota_seconds: 28800\n  daily_lunch: 11:30-12:15\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	settings := decodeJSONMap(t, []byte(contextResult.stdout))["settings"].(map[string]any)
	lunch := settings["lunch"].(map[string]any)
	if lunch["start"] != "11:30" || lunch["end"] != "12:15" {
		t.Fatalf("expected configured lunch, got %#v", lunch)
	}
}

func TestContextUsesConfiguredWorkdayByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  day_start: 09:00\n  day_end: 17:30\n  daily_lunch: 12:00-12:45\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	settings := decodeJSONMap(t, []byte(contextResult.stdout))["settings"].(map[string]any)
	if settings["day_start"] != "09:00" || settings["day_end"] != "17:30" {
		t.Fatalf("expected configured workday, got %#v", settings)
	}
}

func TestContextDayFlagsOverrideConfiguredWorkday(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  day_start: 09:00\n  day_end: 17:30\n  daily_lunch: 12:00-12:45\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--day-start", "10:00", "--day-end", "18:00", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	settings := decodeJSONMap(t, []byte(contextResult.stdout))["settings"].(map[string]any)
	if settings["day_start"] != "10:00" || settings["day_end"] != "18:00" {
		t.Fatalf("expected workday flag override, got %#v", settings)
	}
}

func TestContextLunchFlagOverridesConfiguredDailyLunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  daily_lunch: 11:30-12:15\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--lunch", "13:00-13:30", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	settings := decodeJSONMap(t, []byte(contextResult.stdout))["settings"].(map[string]any)
	lunch := settings["lunch"].(map[string]any)
	if lunch["start"] != "13:00" || lunch["end"] != "13:30" {
		t.Fatalf("expected flag lunch override, got %#v", lunch)
	}
}

func TestContextNoLunchDisablesConfiguredDailyLunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  daily_lunch: 11:30-12:15\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--no-lunch", "--output", "json")
	if contextResult.code != 0 {
		t.Fatalf("context failed: code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}

	settings := decodeJSONMap(t, []byte(contextResult.stdout))["settings"].(map[string]any)
	if settings["lunch"] != nil {
		t.Fatalf("expected lunch to be disabled, got %#v", settings["lunch"])
	}
}

func TestContextConfiguredDailyLunchMustFitEffectiveWorkday(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sqlitePath := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	writeConfigContent(t, "default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: "+sqlitePath+"\nworklogs:\n  minimum_duration_seconds: 900\n  day_start: 07:00\n  day_end: 08:00\n  daily_lunch: 07:15-07:45\n")

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	contextResult := runCLI(t, "worklogs", "context", "--from", "2026-05-03", "--to", "2026-05-03", "--day-start", "08:00", "--day-end", "17:00", "--output", "json")
	if contextResult.code != 2 {
		t.Fatalf("expected validation failure, got code=%d stdout=%s stderr=%s", contextResult.code, contextResult.stdout, contextResult.stderr)
	}
	if !strings.Contains(contextResult.stdout, "lunch must fit strictly inside the effective workday") {
		t.Fatalf("expected workday fit error, got stdout=%s stderr=%s", contextResult.stdout, contextResult.stderr)
	}
}

func TestShiftDryRunAndExecute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T08:00:00Z", "--duration", "1h", "--description", "Second", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	dry := runCLI(t, "worklogs", "shift", "--issue", "ABC-123", "--by", "15m", "--dry", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("shift dry-run failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	dryPayload := decodeJSONMap(t, []byte(dry.stdout))
	if dryPayload["dry_run"] != true {
		t.Fatalf("expected dry_run=true, got %#v", dryPayload["dry_run"])
	}
	if dryPayload["delta_seconds"].(float64) != 900 {
		t.Fatalf("expected delta_seconds=900, got %#v", dryPayload["delta_seconds"])
	}
	items := dryPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one preview item, got %#v", dryPayload["items"])
	}
	preview := items[0].(map[string]any)
	if preview["started_at_utc_before"].(string) != "2026-05-03T06:00:00Z" {
		t.Fatalf("unexpected preview before %#v", preview)
	}
	if preview["started_at_utc_after"].(string) != "2026-05-03T06:15:00Z" {
		t.Fatalf("unexpected preview after %#v", preview)
	}

	beforeExecute := runCLI(t, "worklogs", "list", "--issue", "ABC-123", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	beforePayload := decodeJSONMap(t, []byte(beforeExecute.stdout))
	beforeRecord := beforePayload["items"].([]any)[0].(map[string]any)
	if beforeRecord["started_at_utc"].(string) != "2026-05-03T06:00:00Z" {
		t.Fatalf("dry-run should not mutate record, got %#v", beforeRecord)
	}

	exec := runCLI(t, "worklogs", "shift", "--issue", "ABC-123", "--by", "15m", "--output", "json")
	if exec.code != 0 {
		t.Fatalf("shift execute failed: code=%d stdout=%s stderr=%s", exec.code, exec.stdout, exec.stderr)
	}
	execPayload := decodeJSONMap(t, []byte(exec.stdout))
	if execPayload["dry_run"] != false {
		t.Fatalf("expected dry_run=false, got %#v", execPayload["dry_run"])
	}
	execItems := execPayload["items"].([]any)
	execRecord := execItems[0].(map[string]any)
	if execRecord["started_at_utc"].(string) != "2026-05-03T06:15:00Z" {
		t.Fatalf("expected shifted started_at_utc, got %#v", execRecord)
	}
}

func TestShiftRejectsConflictAndNoMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	first := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "1h", "--description", "First", "--output", "json")
	if first.code != 0 {
		t.Fatalf("first add failed: code=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	second := runCLI(t, "worklogs", "add", "--issue", "ABC-124", "--started-utc", "2026-05-03T07:30:00Z", "--duration", "1h", "--description", "Second", "--output", "json")
	if second.code != 0 {
		t.Fatalf("second add failed: code=%d stdout=%s stderr=%s", second.code, second.stdout, second.stderr)
	}

	conflict := runCLI(t, "worklogs", "shift", "--issue", "ABC-123", "--by", "90m", "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected conflict exit 2, got %d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}

	list := runCLI(t, "worklogs", "list", "--issue", "ABC-123", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	payload := decodeJSONMap(t, []byte(list.stdout))
	record := payload["items"].([]any)[0].(map[string]any)
	if record["started_at_utc"].(string) != "2026-05-03T06:00:00Z" {
		t.Fatalf("conflict should not mutate record, got %#v", record)
	}

	notFound := runCLI(t, "worklogs", "shift", "--issue", "ZZZ-999", "--by", "15m", "--output", "json")
	if notFound.code != 3 {
		t.Fatalf("expected no-match exit 3, got %d stdout=%s stderr=%s", notFound.code, notFound.stdout, notFound.stderr)
	}
}

func TestApplyDryRunFromStdinAndExecuteFromFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	payload := `{"adds":[{"issue_key":"ABC-123","started_at_utc":"2026-05-03T06:00:00Z","duration_seconds":1800,"description":"First"},{"issue_key":"ABC-124","started_at_utc":"2026-05-03T06:30:00Z","duration_seconds":1800,"description":"Second"}]}`
	dry := runCLIWithInput(t, payload, "worklogs", "apply", "--stdin", "--dry", "--output", "json")
	if dry.code != 0 {
		t.Fatalf("apply dry-run failed: code=%d stdout=%s stderr=%s", dry.code, dry.stdout, dry.stderr)
	}
	dryPayload := decodeJSONMap(t, []byte(dry.stdout))
	if dryPayload["dry_run"] != true {
		t.Fatalf("expected dry_run=true, got %#v", dryPayload["dry_run"])
	}
	summary := dryPayload["summary"].(map[string]any)
	if summary["add_count"].(float64) != 2 {
		t.Fatalf("expected add_count=2, got %#v", summary)
	}
	items := dryPayload["items"].([]any)
	first := items[0].(map[string]any)
	if first["index"].(float64) != 0 {
		t.Fatalf("expected first index 0, got %#v", first)
	}
	if first["id"] != nil {
		t.Fatalf("expected dry-run id to be null, got %#v", first["id"])
	}

	listBefore := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(listBefore.stdout))["total"].(float64) != 0 {
		t.Fatalf("dry-run should not create rows")
	}

	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	exec := runCLI(t, "worklogs", "apply", "--file", payloadPath, "--output", "json")
	if exec.code != 0 {
		t.Fatalf("apply execute failed: code=%d stdout=%s stderr=%s", exec.code, exec.stdout, exec.stderr)
	}
	execPayload := decodeJSONMap(t, []byte(exec.stdout))
	if execPayload["dry_run"] != false {
		t.Fatalf("expected dry_run=false, got %#v", execPayload["dry_run"])
	}
	execItems := execPayload["items"].([]any)
	if execItems[0].(map[string]any)["id"] == nil {
		t.Fatalf("expected persisted id in execute response, got %#v", execItems[0])
	}

	listAfter := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(listAfter.stdout))["total"].(float64) != 2 {
		t.Fatalf("expected two rows after apply, got %s", listAfter.stdout)
	}
}

func TestApplyRejectsConflictWithoutForceAndAllowsForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)

	result := runCLI(t, "init", "--output", "json")
	if result.code != 0 {
		t.Fatalf("init failed: code=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}

	add := runCLI(t, "worklogs", "add", "--issue", "ABC-123", "--started-utc", "2026-05-03T06:00:00Z", "--duration", "30m", "--description", "Existing", "--output", "json")
	if add.code != 0 {
		t.Fatalf("seed add failed: code=%d stdout=%s stderr=%s", add.code, add.stdout, add.stderr)
	}

	conflictPayload := `{"adds":[{"issue_key":"ABC-124","started_at_utc":"2026-05-03T06:15:00Z","duration_seconds":1800,"description":"Conflict"}]}`
	conflictPath := filepath.Join(t.TempDir(), "conflict.json")
	if err := os.WriteFile(conflictPath, []byte(conflictPayload), 0o600); err != nil {
		t.Fatalf("write conflict payload: %v", err)
	}

	conflict := runCLI(t, "worklogs", "apply", "--file", conflictPath, "--output", "json")
	if conflict.code != 2 {
		t.Fatalf("expected conflict exit 2, got %d stdout=%s stderr=%s", conflict.code, conflict.stdout, conflict.stderr)
	}
	list := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(list.stdout))["total"].(float64) != 1 {
		t.Fatalf("conflicting apply should not partially write, got %s", list.stdout)
	}

	forced := runCLI(t, "worklogs", "apply", "--file", conflictPath, "--force", "--output", "json")
	if forced.code != 0 {
		t.Fatalf("forced apply failed: code=%d stdout=%s stderr=%s", forced.code, forced.stdout, forced.stderr)
	}
	listAfter := runCLI(t, "worklogs", "list", "--from", "2026-05-03", "--to", "2026-05-03", "--output", "json")
	if decodeJSONMap(t, []byte(listAfter.stdout))["total"].(float64) != 2 {
		t.Fatalf("forced apply should create second row, got %s", listAfter.stdout)
	}
}

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), args, stdout, stderr)

	return cliResult{
		code:   code,
		stdout: stdout.String(),
		stderr: stderr.String(),
	}
}

func runCLIWithInput(t *testing.T, input string, args ...string) cliResult {
	t.Helper()

	originalStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	if _, err := io.WriteString(writer, input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = originalStdin
		_ = reader.Close()
	}()

	return runCLI(t, args...)
}

func decodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to decode json: %v\n%s", err, string(data))
	}

	return payload
}

func statusItems(t *testing.T, raw string) []map[string]any {
	t.Helper()

	payload := decodeJSONMap(t, []byte(raw))
	itemsRaw, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected items array, got %s", raw)
	}

	items := make([]map[string]any, 0, len(itemsRaw))
	for _, item := range itemsRaw {
		record, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected item object, got %T in %s", item, raw)
		}
		items = append(items, record)
	}

	return items
}

func totalsItems(t *testing.T, raw string) []map[string]any {
	t.Helper()

	payload := decodeJSONMap(t, []byte(raw))
	itemsRaw, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected items array, got %s", raw)
	}

	items := make([]map[string]any, 0, len(itemsRaw))
	for _, item := range itemsRaw {
		record, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected item object, got %T in %s", item, raw)
		}
		items = append(items, record)
	}

	return items
}

func writeConfigWithUTC(t *testing.T) {
	t.Helper()

	home := os.Getenv("HOME")
	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	writeConfigAtPath(t, sqlitePath)
}

func writeConfigAtPath(t *testing.T, sqlitePath string) {
	t.Helper()

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\n  daily_minimum_quota_seconds: 28800\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigContent(t *testing.T, content string) {
	t.Helper()

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func setClockifyTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("WORKLEDGER_TEST_CLOCKIFY_API_KEY", "clockify-key")
}

func setJiraCloudTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("WORKLEDGER_TEST_JIRA_CLOUD_TOKEN", "jira-token")
}

func setJiraDataTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("WORKLEDGER_TEST_JIRA_DATA_TOKEN", "jira-token")
}

func writeConfigWithUTCAndClockify(t *testing.T) {
	t.Helper()
	setClockifyTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndMissingClockifySecretEnv(t *testing.T) {
	t.Helper()

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY_MISSING\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndClockifyPush(t *testing.T) {
	t.Helper()
	setClockifyTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n  project_mapping:\n    issue_prefixes:\n      AAPP: App Project\n    default_project: App Project\n    create_issue_tag_if_missing: true\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndStatusAdapters(t *testing.T, includeClockify bool, jiraCloudInstances map[string]string, jiraDataInstances map[string]string) {
	t.Helper()

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	var b strings.Builder
	b.WriteString("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: ")
	b.WriteString(sqlitePath)
	b.WriteString("\nworklogs:\n  minimum_duration_seconds: 900\n")

	if includeClockify {
		setClockifyTestEnv(t)
		b.WriteString("clockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n\n")
	}
	if len(jiraCloudInstances) > 0 {
		setJiraCloudTestEnv(t)
		b.WriteString("jira_cloud:\n  instances:\n")
		for name, baseURL := range jiraCloudInstances {
			b.WriteString("    ")
			b.WriteString(name)
			b.WriteString(":\n      base_url: ")
			b.WriteString(baseURL)
			b.WriteString("\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n")
		}
		b.WriteString("\n")
	}
	if len(jiraDataInstances) > 0 {
		setJiraDataTestEnv(t)
		b.WriteString("jira_data_center:\n  instances:\n")
		for name, baseURL := range jiraDataInstances {
			b.WriteString("    ")
			b.WriteString(name)
			b.WriteString(":\n      base_url: ")
			b.WriteString(baseURL)
			b.WriteString("\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n")
		}
	}

	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndAllStatusFamilies(t *testing.T, jiraCloudURL, jiraDataURL string) {
	t.Helper()
	setClockifyTestEnv(t)
	setJiraCloudTestEnv(t)
	setJiraDataTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n\njira_cloud:\n  instances:\n    product:\n      base_url: " + jiraCloudURL + "\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n\njira_data_center:\n  instances:\n    internal:\n      base_url: " + jiraDataURL + "\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndBareTotalsAdapters(t *testing.T, jiraCloudInstances map[string]string, jiraDataInstances map[string]string, jiraCloudPrefixes map[string]string, jiraDataPrefixes map[string]string) {
	t.Helper()
	setClockifyTestEnv(t)
	if len(jiraCloudInstances) > 0 {
		setJiraCloudTestEnv(t)
	}
	if len(jiraDataInstances) > 0 {
		setJiraDataTestEnv(t)
	}

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	var b strings.Builder
	b.WriteString("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: ")
	b.WriteString(sqlitePath)
	b.WriteString("\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n")
	if len(jiraCloudInstances) > 0 {
		b.WriteString("\njira_cloud:\n  instances:\n")
		for name, baseURL := range jiraCloudInstances {
			b.WriteString("    ")
			b.WriteString(name)
			b.WriteString(":\n      base_url: ")
			b.WriteString(baseURL)
			b.WriteString("\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - ")
			b.WriteString(jiraCloudPrefixes[name])
			b.WriteString("\n")
		}
	}
	if len(jiraDataInstances) > 0 {
		b.WriteString("\njira_data_center:\n  instances:\n")
		for name, baseURL := range jiraDataInstances {
			b.WriteString("    ")
			b.WriteString(name)
			b.WriteString(":\n      base_url: ")
			b.WriteString(baseURL)
			b.WriteString("\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n      routing:\n        profiles:\n          default:\n            issue_prefixes:\n              - ")
			b.WriteString(jiraDataPrefixes[name])
			b.WriteString("\n")
		}
	}

	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndJiraData(t *testing.T, instances map[string]string, routing map[string]string) {
	t.Helper()
	setJiraDataTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	var b strings.Builder
	b.WriteString("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: ")
	b.WriteString(sqlitePath)
	b.WriteString("\nworklogs:\n  minimum_duration_seconds: 900\njira_data_center:\n  instances:\n")
	for name, baseURL := range instances {
		b.WriteString("    ")
		b.WriteString(name)
		b.WriteString(":\n      base_url: ")
		b.WriteString(baseURL)
		b.WriteString("\n      auth:\n        bearer:\n          token_env: WORKLEDGER_TEST_JIRA_DATA_TOKEN\n")
		if block, ok := routing[name]; ok {
			b.WriteString(block)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeConfigWithUTCAndJiraCloud(t *testing.T, instances map[string]string, routing map[string]string) {
	t.Helper()
	setJiraCloudTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	var b strings.Builder
	b.WriteString("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: ")
	b.WriteString(sqlitePath)
	b.WriteString("\nworklogs:\n  minimum_duration_seconds: 900\njira_cloud:\n  instances:\n")
	for name, baseURL := range instances {
		b.WriteString("    ")
		b.WriteString(name)
		b.WriteString(":\n      base_url: ")
		b.WriteString(baseURL)
		b.WriteString("\n      auth:\n        email: user@example.com\n        token_env: WORKLEDGER_TEST_JIRA_CLOUD_TOKEN\n")
		if block, ok := routing[name]; ok {
			b.WriteString(block)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeBrokenClockifyConfig(t *testing.T) {
	t.Helper()
	setClockifyTestEnv(t)

	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "workledger")
	sqlitePath := filepath.Join(home, ".local", "share", "workledger", "worklogs.db")
	content := []byte("default_output: table\nlocal_timezone: UTC\nstorage:\n  sqlite_path: " + sqlitePath + "\nworklogs:\n  minimum_duration_seconds: 900\nclockify:\n  workspace_id: ws-active\n  auth:\n    api_key_env: WORKLEDGER_TEST_CLOCKIFY_API_KEY\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
}

func newClockifyTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/user":
			_, _ = w.Write([]byte(`{"id":"user-1","activeWorkspace":"ws-active","defaultWorkspace":"ws-default"}`))
		case r.URL.Path == "/v1/workspaces/ws-active/user/user-1/time-entries":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"valid-1","description":"  Sync   docs ","tagIds":["tag-123"],"timeInterval":{"start":"2026-04-02T08:00:00Z","end":"2026-04-02T09:00:00Z"}},
				{"id":"missing-tag","description":"No issue","tagIds":["tag-misc"],"timeInterval":{"start":"2026-04-03T08:00:00Z","end":"2026-04-03T09:00:00Z"}},
				{"id":"ambiguous-tag","description":"Two issues","tagIds":["tag-123","tag-124"],"timeInterval":{"start":"2026-04-04T08:00:00Z","end":"2026-04-04T09:00:00Z"}},
				{"id":"running","description":"Running","tagIds":["tag-999"],"timeInterval":{"start":"2026-04-05T08:00:00Z","end":""}}
			]`))
		case r.URL.Path == "/v1/workspaces/ws-active/tags":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[
				{"id":"tag-123","name":"AAPP-123"},
				{"id":"tag-124","name":"AAPP-124"},
				{"id":"tag-999","name":"AAPP-999"},
				{"id":"tag-misc","name":"misc"}
			]`))
		case r.URL.Path == "/v1/workspaces/ws-active/projects":
			w.Header().Set("Last-Page", "true")
			_, _ = w.Write([]byte(`[{"id":"proj-1","name":"App Project"}]`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
}

func newJiraDataTotalsTestServer(t *testing.T, issues map[string][]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"name":"user-1","key":"user-1","displayName":"User One"}`))
		case r.URL.Path == "/rest/api/2/search":
			items := make([]string, 0, len(issues))
			for issueKey := range issues {
				items = append(items, `{"id":"`+issueKey+`","key":"`+issueKey+`"}`)
			}
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":` + strconv.Itoa(len(items)) + `,"issues":[` + strings.Join(items, ",") + `]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/") && strings.HasSuffix(r.URL.Path, "/worklog"):
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 7 {
				t.Fatalf("unexpected jira worklog path %s", r.URL.Path)
			}
			issueKey := parts[5]
			worklogs, ok := issues[issueKey]
			if !ok {
				t.Fatalf("unexpected issue key %s", issueKey)
			}
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":` + strconv.Itoa(len(worklogs)) + `,"worklogs":[` + strings.Join(worklogs, ",") + `]}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
}

func newJiraDataIssueMetadataTestServer(t *testing.T, estimates map[string]*int64) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/") && r.Method == http.MethodGet:
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 6 {
				t.Fatalf("unexpected jira issue path %s", r.URL.Path)
			}
			issueKey := parts[5]
			estimate, ok := estimates[issueKey]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if estimate == nil {
				_, _ = w.Write([]byte(`{"id":"` + issueKey + `","key":"` + issueKey + `","fields":{"timetracking":null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"` + issueKey + `","key":"` + issueKey + `","fields":{"timetracking":{"originalEstimateSeconds":` + strconv.FormatInt(*estimate, 10) + `}}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
}

func newJiraCloudIssueMetadataTestServer(t *testing.T, estimates map[string]*int64) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && r.Method == http.MethodGet:
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 6 {
				t.Fatalf("unexpected jira cloud issue path %s", r.URL.Path)
			}
			issueKey := parts[5]
			estimate, ok := estimates[issueKey]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if estimate == nil {
				_, _ = w.Write([]byte(`{"id":"` + issueKey + `","key":"` + issueKey + `","fields":{"timetracking":null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"` + issueKey + `","key":"` + issueKey + `","fields":{"timetracking":{"originalEstimateSeconds":` + strconv.FormatInt(*estimate, 10) + `}}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
}

func newJiraCloudTestServer(t *testing.T, issues map[string][]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"user-1","displayName":"User One"}`))
		case r.URL.Path == "/rest/api/3/search/jql":
			items := make([]string, 0, len(issues))
			for issueKey := range issues {
				items = append(items, `{"id":"`+issueKey+`","key":"`+issueKey+`"}`)
			}
			_, _ = w.Write([]byte(`{"isLast":true,"issues":[` + strings.Join(items, ",") + `]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/worklog"):
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 6 {
				t.Fatalf("unexpected jira cloud issue path %s", r.URL.Path)
			}
			issueRef := parts[5]
			_, _ = w.Write([]byte(`{"id":"` + issueRef + `","key":"` + issueRef + `"}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodGet:
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) < 7 {
				t.Fatalf("unexpected jira cloud worklog path %s", r.URL.Path)
			}
			issueKey := parts[5]
			worklogs, ok := issues[issueKey]
			if !ok {
				worklogs = []string{}
			}
			_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":` + strconv.Itoa(len(worklogs)) + `,"worklogs":[` + strings.Join(worklogs, ",") + `]}`))
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/") && strings.HasSuffix(r.URL.Path, "/worklog") && r.Method == http.MethodPost:
			parts := strings.Split(r.URL.Path, "/")
			issueKey := parts[5]
			issues[issueKey] = append(issues[issueKey], `{"id":"created-1","started":"2026-05-01T08:00:00.000+0000","timeSpentSeconds":3600,"comment":"Build feature","author":{"accountId":"user-1"}}`)
			_, _ = w.Write([]byte(`{"id":"created-1"}`))
		case strings.Contains(r.URL.Path, "/worklog/") && r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
}

func assertIssueMetadataTableExists(t *testing.T) {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='issue_metadata'`).Scan(&name); err != nil {
		t.Fatalf("issue_metadata table missing: %v", err)
	}
}

func readSingleTombstoneDescription(t *testing.T) string {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	var description string
	if err := db.QueryRow(`SELECT description FROM worklog_tombstones LIMIT 1`).Scan(&description); err != nil {
		t.Fatalf("read tombstone description: %v", err)
	}
	return description
}

func seedIssueMetadata(t *testing.T, issueKey string, maxEstimateSeconds *int64) {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(
		`INSERT INTO issue_metadata(issue_key, max_estimate_seconds, source_adapter_family, source_adapter_instance, refreshed_at) VALUES(?, ?, ?, ?, ?)`,
		issueKey,
		maxEstimateSeconds,
		"jira-cloud",
		"product",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed issue_metadata: %v", err)
	}
}

func seedTombstone(t *testing.T, id, issueKey, startedAt string, durationSeconds int, description, deletedAt string) {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(
		`INSERT INTO worklog_tombstones(worklog_id, issue_key, started_at_utc, duration_seconds, description, deleted_at) VALUES(?, ?, ?, ?, ?, ?)`,
		id, issueKey, startedAt, durationSeconds, description, deletedAt,
	); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
}

type savedAttemptSeed struct {
	state     string
	createdAt string
}

type savedPlanItemSeed struct {
	itemID      string
	direction   string
	adapter     string
	target      string
	action      string
	payloadJSON string
	attempts    []savedAttemptSeed
}

type savedPlanSeed struct {
	planID      string
	fingerprint string
	itemID      string
	direction   string
	adapter     string
	target      string
	action      string
	payloadJSON string
	attempts    []savedAttemptSeed
	items       []savedPlanItemSeed
	createdAt   string
}

func seedSavedPlan(t *testing.T, seed savedPlanSeed) {
	t.Helper()

	items := seed.items
	if len(items) == 0 {
		items = []savedPlanItemSeed{{
			itemID:      seed.itemID,
			direction:   seed.direction,
			adapter:     seed.adapter,
			target:      seed.target,
			action:      seed.action,
			payloadJSON: seed.payloadJSON,
			attempts:    seed.attempts,
		}}
	}

	db := openTestDB(t)
	defer db.Close()

	createdAt := seed.createdAt
	if createdAt == "" {
		createdAt = "2026-05-02T12:00:00Z"
	}

	if _, err := db.Exec(
		`INSERT INTO saved_plans(id, plan_direction, adapter_family, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status, applied_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		seed.planID, items[0].direction, items[0].adapter, seed.fingerprint, "2026-05-01T00:00:00Z", "2026-05-01T23:59:59Z", createdAt, "ready",
	); err != nil {
		t.Fatalf("seed saved plan: %v", err)
	}

	for _, item := range items {
		if _, err := db.Exec(
			`INSERT INTO saved_plan_items(
				id, plan_id, issue_key, plan_direction, target_adapter_family, target_adapter_instance, target_issue, route_profile,
				window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail,
				payload_json, inspection_summary_json, delivery_key, content_hash, local_row_count, local_total_seconds,
				remote_row_count, remote_total_seconds, applied_state, applied_at, apply_message
			) VALUES(?, ?, ?, ?, ?, '', ?, NULL, ?, ?, 'ready', ?, 'remote_diff', 'remote_diff', 'seeded', ?, '{}', ?, 'hash', 1, 3600, 0, 0, 'not_attempted', NULL, '')`,
			item.itemID, seed.planID, item.target, item.direction, item.adapter, item.target,
			"2026-05-01T00:00:00Z", "2026-05-01T23:59:59Z", item.action, item.payloadJSON, item.itemID+"-delivery",
		); err != nil {
			t.Fatalf("seed saved plan item: %v", err)
		}
		for i, attempt := range item.attempts {
			if _, err := db.Exec(
				`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
				item.itemID+"-attempt-"+strconv.Itoa(i), seed.planID, item.itemID, attempt.state, attempt.state, attempt.createdAt,
			); err != nil {
				t.Fatalf("seed delivery attempt: %v", err)
			}
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(os.Getenv("HOME"), ".local", "share", "workledger", "worklogs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func countActiveCLIWorklogs(t *testing.T) int {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worklogs`).Scan(&count); err != nil {
		t.Fatalf("count worklogs: %v", err)
	}
	return count
}

func countSavedPlans(t *testing.T) int {
	t.Helper()

	db := openTestDB(t)
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM saved_plans`).Scan(&count); err != nil {
		t.Fatalf("count saved plans: %v", err)
	}
	return count
}

func int64Ptr(value int64) *int64 {
	return &value
}
