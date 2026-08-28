package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigBytesIncludesFullCommentedAdapterReference(t *testing.T) {
	content := string(DefaultConfigBytes(nil))

	fragments := []string{
		"default_output: table\n",
		"local_timezone: Europe/Vilnius\n",
		"worklogs:\n  minimum_duration_seconds: 900\n  # daily_minimum_quota_seconds is for workledger worklogs context\n  daily_minimum_quota_seconds: 28800\n  # day_start and day_end are used for context analysis and automatic worklog placement\n  day_start: 08:00\n  day_end: 17:00\n  # daily_lunch is used for context analysis and automatic worklog placement\n  daily_lunch: 12:00-12:45\n",
		"# clockify:\n#   workspace_id: your-workspace-id\n#   user_id: your-user-id\n#   auth:\n#     api_key_env: CLOCKIFY_API_KEY\n#   project_mapping:\n#     issue_prefixes:\n#       WEB: App Project\n#     default_project: Default Project # fallback project when no issue prefix matches\n#     create_issue_tag_if_missing: true # automation default; create missing issue tags on push\n",
		"# jira_cloud:\n#   instances:\n#     product:\n#       base_url: https://example.atlassian.net\n#       auth:\n#         email: user@example.com\n#         token_env: WORKLEDGER_JIRA_CLOUD_PRODUCT_TOKEN\n#       pull:\n#         exclude_issues: # issue keys that pull must never import into local storage; reporting issues are excluded by default\n#           - REPORT-2\n#       routing:\n#         profiles:\n#           default:\n#             issue_prefixes:\n#               - WEB\n#           # Reconcile this reporting profile with:\n#           # workledger plan reconcile --push --adapter=jira-cloud --instance product --route-profile reporting --today\n#           reporting: # non-default profile for fixed reporting issue routing\n#             reporting_targets: # canonical prefix -> fixed reporting issue key; OPS matches jira_data_center.instances.internal.routing.profiles.default.issue_prefixes\n#               OPS: REPORT-1\n",
		"# jira_data_center:\n#   instances:\n#     internal:\n#       base_url: https://jira.example.com\n#       auth:\n#         bearer:\n#           token_env: WORKLEDGER_JIRA_DC_INTERNAL_TOKEN\n#       routing:\n#         profiles:\n#           default:\n#             issue_prefixes:\n#               - OPS\n",
	}

	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected scaffold fragment %q in config:\n%s", fragment, content)
		}
	}
}

func TestDefaultConfigBytesWithClockifyBootstrapIncludesCommentedProjectMappingExample(t *testing.T) {
	content := string(DefaultConfigBytes(&ClockifyConfig{
		WorkspaceID: "ws-active",
		UserID:      "user-1",
		Auth: ClockifyAuthConfig{
			APIKeyEnv: "CLOCKIFY_API_KEY",
		},
	}))

	fragments := []string{
		"worklogs:\n  minimum_duration_seconds: 900\n  # daily_minimum_quota_seconds is for workledger worklogs context\n  daily_minimum_quota_seconds: 28800\n  # day_start and day_end are used for context analysis and automatic worklog placement\n  day_start: 08:00\n  day_end: 17:00\n  # daily_lunch is used for context analysis and automatic worklog placement\n  daily_lunch: 12:00-12:45\n",
		"clockify:\n  workspace_id: ws-active\n  user_id: user-1\n  auth:\n    api_key_env: CLOCKIFY_API_KEY\n  # project_mapping:\n  #   issue_prefixes:\n  #     WEB: App Project\n  #   default_project: Default Project # fallback project when no issue prefix matches\n  #   create_issue_tag_if_missing: true # automation default; create missing issue tags on push\n",
		"# jira_cloud:\n#   instances:\n#     product:\n#       base_url: https://example.atlassian.net\n",
		"# jira_data_center:\n#   instances:\n#     internal:\n#       base_url: https://jira.example.com\n",
	}

	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected scaffold fragment %q in config:\n%s", fragment, content)
		}
	}
}

func TestValidateConfigBytesAcceptsLocalTimezone(t *testing.T) {
	data := []byte(`
local_timezone: Europe/Vilnius
storage:
  sqlite_path: /tmp/workledger-test.db
worklogs:
  daily_minimum_quota_seconds: 28800
`)

	issues, cfg := validateConfigBytes(data)
	if len(issues) != 0 {
		t.Fatalf("expected valid config, got %#v", issues)
	}
	if cfg.LocalTimezone != "Europe/Vilnius" {
		t.Fatalf("expected local timezone to decode, got %#v", cfg.LocalTimezone)
	}
}

func TestValidateConfigBytesRejectsInvalidLocalTimezone(t *testing.T) {
	cases := []string{
		"local_timezone: \"\"",
		"local_timezone: Mars/Olympus",
	}

	for _, fragment := range cases {
		data := []byte(fragment + "\nstorage:\n  sqlite_path: /tmp/workledger-test.db\nworklogs:\n  daily_minimum_quota_seconds: 28800\n")

		issues, _ := validateConfigBytes(data)
		if len(issues) == 0 {
			t.Fatalf("expected validation issues for %q", fragment)
		}
		if issues[0].Field != "local_timezone" {
			t.Fatalf("expected local_timezone issue, got %#v", issues)
		}
	}
}

func TestValidateConfigBytesRejectsInvalidDailyMinimumQuota(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
worklogs:
  daily_minimum_quota_seconds: 0
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
	found := false
	for _, issue := range issues {
		if issue.Field == "worklogs.daily_minimum_quota_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected daily minimum quota issue, got %#v", issues)
	}
}

func TestValidateConfigBytesAcceptsDailyLunch(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
worklogs:
  daily_lunch: 12:00-13:00
`)

	issues, cfg := validateConfigBytes(data)
	if len(issues) != 0 {
		t.Fatalf("expected valid config, got %#v", issues)
	}
	if cfg.Worklogs == nil || cfg.Worklogs.DailyLunch != "12:00-13:00" {
		t.Fatalf("expected daily_lunch to decode, got %#v", cfg.Worklogs)
	}
}

func TestValidateConfigBytesAcceptsConfiguredWorkday(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
worklogs:
  day_start: 09:00
  day_end: 17:30
  daily_lunch: 12:00-12:45
`)

	issues, cfg := validateConfigBytes(data)
	if len(issues) != 0 {
		t.Fatalf("expected valid config, got %#v", issues)
	}
	if cfg.Worklogs == nil || cfg.Worklogs.DayStart != "09:00" || cfg.Worklogs.DayEnd != "17:30" {
		t.Fatalf("expected configured workday to decode, got %#v", cfg.Worklogs)
	}
}

func TestValidateConfigBytesRejectsInvalidConfiguredWorkday(t *testing.T) {
	cases := []struct {
		name          string
		fragment      string
		expectedField string
	}{
		{name: "invalid day_start format", fragment: "day_start: 9am", expectedField: "worklogs.day_start"},
		{name: "invalid day_end format", fragment: "day_end: 5pm", expectedField: "worklogs.day_end"},
		{name: "day_start after day_end", fragment: "day_start: 17:00\n  day_end: 08:00", expectedField: "worklogs"},
	}

	for _, tc := range cases {
		data := []byte("storage:\n  sqlite_path: /tmp/workledger-test.db\nworklogs:\n  " + tc.fragment + "\n")

		issues, _ := validateConfigBytes(data)
		if len(issues) == 0 {
			t.Fatalf("expected validation issues for %q", tc.name)
		}
		found := false
		for _, issue := range issues {
			if issue.Field == tc.expectedField {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s issue for %q, got %#v", tc.expectedField, tc.name, issues)
		}
	}
}

func TestValidateConfigBytesRejectsInvalidDailyLunch(t *testing.T) {
	cases := []string{
		"daily_lunch: noon",
		"daily_lunch: 25:00-26:00",
		"daily_lunch: 13:00-12:00",
	}

	for _, fragment := range cases {
		data := []byte("storage:\n  sqlite_path: /tmp/workledger-test.db\nworklogs:\n  " + fragment + "\n")

		issues, _ := validateConfigBytes(data)
		if len(issues) == 0 {
			t.Fatalf("expected validation issues for %q", fragment)
		}
		found := false
		for _, issue := range issues {
			if issue.Field == "worklogs.daily_lunch" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected daily_lunch issue for %q, got %#v", fragment, issues)
		}
	}
}

func TestValidateConfigBytesRejectsConfiguredLunchOutsideConfiguredWorkday(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
worklogs:
  day_start: 09:00
  day_end: 17:00
  daily_lunch: 08:30-09:15
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
	found := false
	for _, issue := range issues {
		if issue.Field == "worklogs.daily_lunch" && strings.Contains(issue.Message, "fit strictly inside the configured workday") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected configured workday fit issue, got %#v", issues)
	}
}

func TestValidateConfigBytesRejectsSelectionBlock(t *testing.T) {
	data := []byte(`
selection:
  timezone: UTC
storage:
  sqlite_path: /tmp/workledger-test.db
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
	found := false
	for _, issue := range issues {
		if issue.Field == "selection" && strings.Contains(issue.Message, "not supported") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unsupported selection issue, got %#v", issues)
	}
}

func TestValidateConfigBytesAcceptsClockifyProjectMapping(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_CLOCKIFY_API_KEY
  project_mapping:
    issue_prefixes:
      AAPP: App Project
    default_project: Default Project
    create_issue_tag_if_missing: true
`)

	issues, cfg := validateConfigBytes(data)
	if len(issues) != 0 {
		t.Fatalf("expected valid config, got %#v", issues)
	}
	if cfg.Clockify == nil || cfg.Clockify.ProjectMapping == nil {
		t.Fatal("expected clockify project mapping to be decoded")
	}
	if cfg.Clockify.ProjectMapping.IssuePrefixes["AAPP"] != "App Project" {
		t.Fatalf("unexpected issue prefix mapping %#v", cfg.Clockify.ProjectMapping.IssuePrefixes)
	}
}

func TestValidateConfigBytesRejectsInvalidClockifyProjectMapping(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_CLOCKIFY_API_KEY
  project_mapping:
    issue_prefixes:
      AAPP: ""
    unexpected: true
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) < 2 {
		t.Fatalf("expected multiple issues, got %#v", issues)
	}
}

func TestValidateConfigBytesAcceptsJiraDataRouting(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com/
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_TOKEN
      routing:
        profiles:
          default:
            issue_prefixes:
              - AAPP
          reporting:
            reporting_targets:
              AAPP: REPORT-1
`)

	issues, cfg := validateConfigBytes(data)
	if len(issues) != 0 {
		t.Fatalf("expected valid config, got %#v", issues)
	}
	if cfg.JiraData == nil || cfg.JiraData.Instances["internal"].Routing == nil {
		t.Fatal("expected jira data center routing")
	}
	if cfg.JiraData.Instances["internal"].BaseURL != "https://jira.example.com" {
		t.Fatalf("expected trimmed base URL, got %q", cfg.JiraData.Instances["internal"].BaseURL)
	}
}

func TestValidateConfigBytesRejectsMixedJiraDataRouteModes(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_TOKEN
      routing:
        profiles:
          default:
            issue_prefixes:
              - AAPP
            reporting_targets:
              AAPP: REPORT-1
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
}

func TestValidateConfigBytesRejectsReportingDefaultProfile(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_TOKEN
      routing:
        profiles:
          default:
            reporting_targets:
              AAPP: REPORT-1
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatalf("expected validation issues, got %#v", issues)
	}
}

func TestValidateConfigBytesRejectsDeprecatedTopLevelRouting(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
routing:
  jira_data_center:
    profiles:
      default:
        issue_prefixes:
          AAPP: internal
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
}

func TestValidateConfigBytesRejectsDuplicateOwnedPrefix(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
jira_data_center:
  instances:
    internal:
      base_url: https://jira-internal.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_INTERNAL_TOKEN
      routing:
        profiles:
          default:
            issue_prefixes:
              - AAPP
    reporting:
      base_url: https://jira-reporting.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_REPORTING_TOKEN
      routing:
        profiles:
          default:
            issue_prefixes:
              - AAPP
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
}

func TestValidateConfigBytesRejectsInlineSecretKeys(t *testing.T) {
	data := []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token: secret
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token: secret
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key: secret
`)

	issues, _ := validateConfigBytes(data)
	if len(issues) < 6 {
		t.Fatalf("expected inline-secret and required-env issues, got %#v", issues)
	}
}

func TestLoadEffectiveLeavesEnvBackedSecretsDeferred(t *testing.T) {
	effective, err := loadEffective("test.yaml", []byte(`
local_timezone: Europe/Vilnius
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_CLOCKIFY_API_KEY
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token_env: WORKLEDGER_JIRA_CLOUD_TOKEN
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_TOKEN
`))
	if err != nil {
		t.Fatalf("loadEffective failed: %v", err)
	}
	if effective.LocalTimezoneConfig == nil || *effective.LocalTimezoneConfig != "Europe/Vilnius" {
		t.Fatalf("expected effective local timezone, got %#v", effective.LocalTimezoneConfig)
	}
	if effective.Location.String() != "Europe/Vilnius" {
		t.Fatalf("expected loaded location, got %q", effective.Location.String())
	}
	if effective.File.Clockify == nil || effective.File.Clockify.Auth.APIKey != "" {
		t.Fatalf("expected deferred clockify API key, got %#v", effective.File.Clockify)
	}
	if token := effective.File.JiraCloud.Instances["product"].Auth.Token; token != "" {
		t.Fatalf("expected deferred jira cloud token, got %q", token)
	}
	if token := effective.File.JiraData.Instances["internal"].Auth.Bearer.Token; token != "" {
		t.Fatalf("expected deferred jira data token, got %q", token)
	}
}

func TestLoadEffectiveAllowsMissingOrBlankSecretEnv(t *testing.T) {
	t.Setenv("WORKLEDGER_BLANK_SECRET", "   ")

	_, err := loadEffective("test.yaml", []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_MISSING_SECRET
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token_env: WORKLEDGER_BLANK_SECRET
`))
	if err != nil {
		t.Fatalf("expected env resolution to stay deferred, got %v", err)
	}
}

func TestAdapterResolversResolveEnvBackedSecrets(t *testing.T) {
	t.Setenv("WORKLEDGER_CLOCKIFY_API_KEY", "clockify-secret")
	t.Setenv("WORKLEDGER_JIRA_CLOUD_TOKEN", "jira-cloud-secret")
	t.Setenv("WORKLEDGER_JIRA_DC_TOKEN", "jira-dc-secret")

	effective, err := loadEffective("test.yaml", []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_CLOCKIFY_API_KEY
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token_env: WORKLEDGER_JIRA_CLOUD_TOKEN
jira_data_center:
  instances:
    internal:
      base_url: https://jira.example.com
      auth:
        bearer:
          token_env: WORKLEDGER_JIRA_DC_TOKEN
`))
	if err != nil {
		t.Fatalf("loadEffective failed: %v", err)
	}

	clockifyCfg, err := ResolveClockifyConfig(effective)
	if err != nil || clockifyCfg.Auth.APIKey != "clockify-secret" {
		t.Fatalf("expected resolved clockify API key, got cfg=%#v err=%v", clockifyCfg, err)
	}
	resolvedClockifyName, resolvedClockifyCfg, err := ResolveClockifyInstance(effective, "")
	if err != nil || resolvedClockifyName != ClockifyInstanceName || resolvedClockifyCfg.Auth.APIKey != "clockify-secret" {
		t.Fatalf("expected resolved clockify instance, got name=%q cfg=%#v err=%v", resolvedClockifyName, resolvedClockifyCfg, err)
	}
	_, jiraCloudCfg, err := ResolveJiraCloudInstance(effective, "product")
	if err != nil || jiraCloudCfg.Auth.Token != "jira-cloud-secret" {
		t.Fatalf("expected resolved jira cloud token, got cfg=%#v err=%v", jiraCloudCfg, err)
	}
	_, jiraDataCfg, err := ResolveJiraDataInstance(effective, "internal")
	if err != nil || jiraDataCfg.Auth.Bearer.Token != "jira-dc-secret" {
		t.Fatalf("expected resolved jira data token, got cfg=%#v err=%v", jiraDataCfg, err)
	}
}

func TestAdapterResolversRejectMissingOrBlankSecretEnv(t *testing.T) {
	t.Setenv("WORKLEDGER_BLANK_SECRET", "   ")

	effective, err := loadEffective("test.yaml", []byte(`
storage:
  sqlite_path: /tmp/workledger-test.db
clockify:
  workspace_id: ws-1
  user_id: user-1
  auth:
    api_key_env: WORKLEDGER_MISSING_SECRET
jira_cloud:
  instances:
    product:
      base_url: https://example.atlassian.net
      auth:
        email: user@example.com
        token_env: WORKLEDGER_BLANK_SECRET
`))
	if err != nil {
		t.Fatalf("loadEffective failed: %v", err)
	}

	_, clockifyErr := ResolveClockifyConfig(effective)
	if clockifyErr == nil {
		t.Fatal("expected clockify env validation error")
	}
	var clockifyValidationErr ValidationErrors
	if !errors.As(clockifyErr, &clockifyValidationErr) || len(clockifyValidationErr.Issues) != 1 {
		t.Fatalf("expected one clockify env issue, got %v", clockifyErr)
	}

	_, _, jiraCloudErr := ResolveJiraCloudInstance(effective, "product")
	if jiraCloudErr == nil {
		t.Fatal("expected jira cloud env validation error")
	}
	var jiraCloudValidationErr ValidationErrors
	if !errors.As(jiraCloudErr, &jiraCloudValidationErr) || len(jiraCloudValidationErr.Issues) != 1 {
		t.Fatalf("expected one jira cloud env issue, got %v", jiraCloudErr)
	}
}

func TestResolveClockifyInstanceRejectsUnknownName(t *testing.T) {
	effective := EffectiveConfig{
		File: FileConfig{
			Clockify: &ClockifyConfig{
				WorkspaceID: "ws-1",
				UserID:      "user-1",
				Auth:        ClockifyAuthConfig{APIKey: "secret"},
			},
		},
	}

	_, _, err := ResolveClockifyInstance(effective, "missing")
	if err == nil || !strings.Contains(err.Error(), `clockify instance "missing" is not configured`) {
		t.Fatalf("expected unknown clockify instance error, got %v", err)
	}
}

func TestFingerprintEffectiveIgnoresResolvedSecretValues(t *testing.T) {
	base := EffectiveConfig{
		DefaultOutput:          "table",
		SQLitePath:             "/tmp/workledger-test.db",
		MinimumDurationSeconds: 900,
		TimezoneName:           "UTC",
		Location:               time.UTC,
		File: FileConfig{
			Clockify: &ClockifyConfig{
				WorkspaceID: "ws-1",
				UserID:      "user-1",
				Auth: ClockifyAuthConfig{
					APIKeyEnv: "WORKLEDGER_CLOCKIFY_API_KEY",
					APIKey:    "secret-a",
				},
			},
		},
	}
	changedSecret := base
	changedSecret.File.Clockify = &ClockifyConfig{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		Auth: ClockifyAuthConfig{
			APIKeyEnv: "WORKLEDGER_CLOCKIFY_API_KEY",
			APIKey:    "secret-b",
		},
	}

	left, err := FingerprintEffective(base)
	if err != nil {
		t.Fatalf("FingerprintEffective failed: %v", err)
	}
	right, err := FingerprintEffective(changedSecret)
	if err != nil {
		t.Fatalf("FingerprintEffective failed: %v", err)
	}
	if left != right {
		t.Fatalf("expected identical fingerprints, got %q and %q", left, right)
	}
}

func TestJiraExcludedIssuesForInstanceIncludesReportingTargets(t *testing.T) {
	cfg := EffectiveConfig{
		File: FileConfig{
			JiraCloud: &JiraCloudConfig{
				Instances: map[string]JiraCloudInstance{
					"product": {
						Pull: JiraPullConfig{ExcludeIssues: []string{"MANUAL-1"}},
						Routing: &JiraInstanceRoutes{
							Profiles: map[string]JiraRouteProfile{
								"default":     {IssuePrefixes: []string{"AAPP"}},
								"reporting-a": {ReportingTargets: map[string]string{"RAPP": "REPORT-1"}},
								"reporting-b": {ReportingTargets: map[string]string{"BAPP": "REPORT-2"}},
							},
						},
					},
				},
			},
		},
	}

	exclusions, err := JiraExcludedIssuesForInstance(cfg, "jira-cloud", "product")
	if err != nil {
		t.Fatalf("JiraExcludedIssuesForInstance failed: %v", err)
	}

	expected := []string{"MANUAL-1", "REPORT-1", "REPORT-2"}
	if len(exclusions) != len(expected) {
		t.Fatalf("unexpected exclusions %#v", exclusions)
	}
	for i := range expected {
		if exclusions[i] != expected[i] {
			t.Fatalf("unexpected exclusions %#v", exclusions)
		}
	}
}
