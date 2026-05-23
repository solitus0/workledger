package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultOutputMode            = "table"
	DefaultMinimumDurationSecond = 900
	DefaultDailyMinimumQuota     = 28800
	DefaultDailyLunch            = "12:00-12:45"
	DefaultSQLitePath            = "~/.local/share/workledger/worklogs.db"
	ClockifyInstanceName         = "clockify"
)

var ErrConfigNotFound = errors.New("config not found")

type FileConfig struct {
	DefaultOutput string                `yaml:"default_output"`
	LocalTimezone string                `yaml:"local_timezone"`
	Storage       StorageConfig         `yaml:"storage"`
	Worklogs      *WorklogConfig        `yaml:"worklogs"`
	JiraCloud     *JiraCloudConfig      `yaml:"jira_cloud"`
	JiraData      *JiraDataCenterConfig `yaml:"jira_data_center"`
	Clockify      *ClockifyConfig       `yaml:"clockify"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

type WorklogConfig struct {
	MinimumDurationSeconds   int    `yaml:"minimum_duration_seconds"`
	DailyMinimumQuotaSeconds int    `yaml:"daily_minimum_quota_seconds"`
	DailyLunch               string `yaml:"daily_lunch"`
}

type JiraCloudConfig struct {
	Instances map[string]JiraCloudInstance `yaml:"instances"`
}

type JiraCloudInstance struct {
	BaseURL string              `yaml:"base_url"`
	Auth    JiraCloudAuthBlock  `yaml:"auth"`
	Pull    JiraPullConfig      `yaml:"pull"`
	Routing *JiraInstanceRoutes `yaml:"routing"`
}

type JiraCloudAuthBlock struct {
	Email    string `yaml:"email" json:"email"`
	TokenEnv string `yaml:"token_env" json:"token_env"`
	Token    string `yaml:"-" json:"-"`
}

type JiraDataCenterConfig struct {
	Instances map[string]JiraDataCenterInstance `yaml:"instances"`
}

type JiraDataCenterInstance struct {
	BaseURL string                 `yaml:"base_url"`
	Auth    JiraDataCenterAuthWrap `yaml:"auth"`
	Pull    JiraPullConfig         `yaml:"pull"`
	Routing *JiraInstanceRoutes    `yaml:"routing"`
}

type JiraDataCenterAuthWrap struct {
	Bearer JiraDataCenterBearer `yaml:"bearer"`
}

type JiraDataCenterBearer struct {
	TokenEnv string `yaml:"token_env" json:"token_env"`
	Token    string `yaml:"-" json:"-"`
}

type JiraPullConfig struct {
	ExcludeIssues []string `yaml:"exclude_issues"`
}

type JiraInstanceRoutes struct {
	Profiles map[string]JiraRouteProfile `yaml:"profiles"`
}

type JiraRouteProfile struct {
	IssuePrefixes    []string          `yaml:"issue_prefixes"`
	ReportingTargets map[string]string `yaml:"reporting_targets"`
}

type ClockifyConfig struct {
	WorkspaceID    string                 `yaml:"workspace_id"`
	UserID         string                 `yaml:"user_id"`
	Auth           ClockifyAuthConfig     `yaml:"auth"`
	ProjectMapping *ClockifyProjectConfig `yaml:"project_mapping"`
}

type ClockifyAuthConfig struct {
	APIKeyEnv string `yaml:"api_key_env" json:"api_key_env"`
	APIKey    string `yaml:"-" json:"-"`
}

type ClockifyProjectConfig struct {
	IssuePrefixes           map[string]string `yaml:"issue_prefixes"`
	DefaultProject          string            `yaml:"default_project"`
	CreateIssueTagIfMissing *bool             `yaml:"create_issue_tag_if_missing"`
}

type EffectiveConfig struct {
	ConfigPath               string
	DefaultOutput            string
	SQLitePath               string
	MinimumDurationSeconds   int
	DailyMinimumQuotaSeconds int
	DailyLunch               string
	TimezoneName             string
	Location                 *time.Location
	LocalTimezoneConfig      *string
	File                     FileConfig
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationErrors struct {
	Issues []ValidationIssue
}

func (e ValidationErrors) Error() string {
	if len(e.Issues) == 0 {
		return "validation failed"
	}

	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if issue.Field == "" {
			parts = append(parts, issue.Message)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", issue.Field, issue.Message))
	}

	return strings.Join(parts, "; ")
}

func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", "workledger", "config.yaml"), nil
}

func DetectOutputMode(requested string) string {
	if requested == "table" || requested == "json" {
		return requested
	}

	configPath, err := ConfigPath()
	if err != nil {
		return DefaultOutputMode
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return DefaultOutputMode
	}

	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return DefaultOutputMode
	}

	mode, _ := raw["default_output"].(string)
	if mode == "table" || mode == "json" {
		return mode
	}

	return DefaultOutputMode
}

func JiraDataIssuePrefixes(cfg EffectiveConfig, instanceName string) ([]string, error) {
	if cfg.File.JiraData == nil || len(cfg.File.JiraData.Instances) == 0 {
		return nil, errors.New("jira_data_center config is required")
	}

	instance, ok := cfg.File.JiraData.Instances[instanceName]
	if !ok {
		return nil, fmt.Errorf("jira_data_center instance %q is not configured", instanceName)
	}
	if instance.Routing == nil || len(instance.Routing.Profiles) == 0 {
		return nil, errors.New("jira_data_center routing is required for totals")
	}

	prefixes := make([]string, 0)
	for _, profile := range instance.Routing.Profiles {
		prefixes = append(prefixes, profile.IssuePrefixes...)
	}

	for i := range prefixes {
		prefixes[i] = strings.TrimSpace(prefixes[i])
	}
	prefixes = slices.DeleteFunc(prefixes, func(value string) bool {
		return value == ""
	})
	slices.Sort(prefixes)
	prefixes = slices.Compact(prefixes)

	return prefixes, nil
}

func JiraDataIssuePrefixesForTotals(cfg EffectiveConfig, instanceName string) ([]string, error) {
	prefixes, err := JiraDataIssuePrefixes(cfg, instanceName)
	if err != nil {
		return nil, err
	}
	if len(prefixes) == 0 {
		return nil, errors.New("jira_data_center routed issue_prefixes are required for totals")
	}
	return prefixes, nil
}

func JiraExcludedIssuesForInstance(cfg EffectiveConfig, family, instanceName string) ([]string, error) {
	excluded := map[string]struct{}{}
	add := func(issue string) {
		issue = strings.TrimSpace(issue)
		if issue != "" {
			excluded[issue] = struct{}{}
		}
	}

	switch family {
	case "jira-cloud":
		if cfg.File.JiraCloud == nil || len(cfg.File.JiraCloud.Instances) == 0 {
			return nil, errors.New("jira_cloud config is required")
		}
		instance, ok := cfg.File.JiraCloud.Instances[instanceName]
		if !ok {
			return nil, fmt.Errorf("jira_cloud instance %q is not configured", instanceName)
		}
		for _, issue := range instance.Pull.ExcludeIssues {
			add(issue)
		}
		if instance.Routing != nil {
			for _, profile := range instance.Routing.Profiles {
				for _, targetIssue := range profile.ReportingTargets {
					add(targetIssue)
				}
			}
		}
	case "jira-data-center":
		if cfg.File.JiraData == nil || len(cfg.File.JiraData.Instances) == 0 {
			return nil, errors.New("jira_data_center config is required")
		}
		instance, ok := cfg.File.JiraData.Instances[instanceName]
		if !ok {
			return nil, fmt.Errorf("jira_data_center instance %q is not configured", instanceName)
		}
		for _, issue := range instance.Pull.ExcludeIssues {
			add(issue)
		}
		if instance.Routing != nil {
			for _, profile := range instance.Routing.Profiles {
				for _, targetIssue := range profile.ReportingTargets {
					add(targetIssue)
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported jira family %q", family)
	}

	items := make([]string, 0, len(excluded))
	for issue := range excluded {
		items = append(items, issue)
	}
	slices.Sort(items)
	return items, nil
}

func JiraCloudIssuePrefixes(cfg EffectiveConfig, instanceName string) ([]string, error) {
	if cfg.File.JiraCloud == nil || len(cfg.File.JiraCloud.Instances) == 0 {
		return nil, errors.New("jira_cloud config is required")
	}

	instance, ok := cfg.File.JiraCloud.Instances[instanceName]
	if !ok {
		return nil, fmt.Errorf("jira_cloud instance %q is not configured", instanceName)
	}
	if instance.Routing == nil || len(instance.Routing.Profiles) == 0 {
		return nil, errors.New("jira_cloud routing is required for totals")
	}

	prefixes := make([]string, 0)
	for _, profile := range instance.Routing.Profiles {
		prefixes = append(prefixes, profile.IssuePrefixes...)
	}

	for i := range prefixes {
		prefixes[i] = strings.TrimSpace(prefixes[i])
	}
	prefixes = slices.DeleteFunc(prefixes, func(value string) bool {
		return value == ""
	})
	slices.Sort(prefixes)
	prefixes = slices.Compact(prefixes)

	return prefixes, nil
}

func JiraCloudIssuePrefixesForTotals(cfg EffectiveConfig, instanceName string) ([]string, error) {
	prefixes, err := JiraCloudIssuePrefixes(cfg, instanceName)
	if err != nil {
		return nil, err
	}
	if len(prefixes) == 0 {
		return nil, errors.New("jira_cloud routed issue_prefixes are required for totals")
	}

	return prefixes, nil
}

func DefaultConfigBytes(clockifyConfig *ClockifyConfig) []byte {
	lines := []string{
		"default_output: table",
		"# local_timezone affects local input and display only; persisted timestamps remain in UTC",
		"local_timezone: Europe/Vilnius",
		"storage:",
		"  sqlite_path: ~/.local/share/workledger/worklogs.db",
		"worklogs:",
		"  minimum_duration_seconds: 900",
		"  # daily_minimum_quota_seconds is for workledger worklogs context",
		"  daily_minimum_quota_seconds: 28800",
		"  # daily_lunch is only used for workledger worklogs context",
		"  daily_lunch: 12:00-12:45",
		"",
	}
	if clockifyConfig == nil {
		lines = append(lines,
			"# clockify:",
			"#   workspace_id: your-workspace-id",
			"#   user_id: your-user-id",
			"#   auth:",
			"#     api_key_env: CLOCKIFY_API_KEY",
			"#   project_mapping:",
			"#     issue_prefixes:",
			"#       WEB: App Project",
			"#     default_project: Default Project # fallback project when no issue prefix matches",
			"#     create_issue_tag_if_missing: true # automation default; create missing issue tags on push",
			"",
		)
	} else {
		lines = append(lines,
			"clockify:",
			"  workspace_id: "+clockifyConfig.WorkspaceID,
			"  user_id: "+clockifyConfig.UserID,
			"  auth:",
			"    api_key_env: "+clockifyConfig.Auth.APIKeyEnv,
			"  # project_mapping:",
			"  #   issue_prefixes:",
			"  #     WEB: App Project",
			"  #   default_project: Default Project # fallback project when no issue prefix matches",
			"  #   create_issue_tag_if_missing: true # automation default; create missing issue tags on push",
			"",
			"# jira_cloud:",
			"#   instances:",
			"#     product:",
			"#       base_url: https://example.atlassian.net",
			"#       auth:",
			"#         email: user@example.com",
			"#         token_env: WORKLEDGER_JIRA_CLOUD_PRODUCT_TOKEN",
			"#       pull:",
			"#         exclude_issues: # issue keys that pull must never import into local storage; reporting issues are excluded by default",
			"#           - REPORT-2",
			"#       routing:",
			"#         profiles:",
			"#           default:",
			"#             issue_prefixes:",
			"#               - WEB",
			"#           # Reconcile this reporting profile with:",
			"#           # workledger plan reconcile --push --adapter=jira-cloud --instance product --route-profile reporting --today",
			"#           reporting: # non-default profile for fixed reporting issue routing",
			"#             reporting_targets: # canonical prefix -> fixed reporting issue key; OPS matches jira_data_center.instances.internal.routing.profiles.default.issue_prefixes",
			"#               OPS: REPORT-1",
			"",
			"# jira_data_center:",
			"#   instances:",
			"#     internal:",
			"#       base_url: https://jira.example.com",
			"#       auth:",
			"#         bearer:",
			"#           token_env: WORKLEDGER_JIRA_DC_INTERNAL_TOKEN",
			"#       routing:",
			"#         profiles:",
			"#           default:",
			"#             issue_prefixes:",
			"#               - OPS",
			"",
		)
		return []byte(strings.Join(lines, "\n"))
	}
	lines = append(lines,
		"# jira_cloud:",
		"#   instances:",
		"#     product:",
		"#       base_url: https://example.atlassian.net",
		"#       auth:",
		"#         email: user@example.com",
		"#         token_env: WORKLEDGER_JIRA_CLOUD_PRODUCT_TOKEN",
		"#       pull:",
		"#         exclude_issues: # issue keys that pull must never import into local storage; reporting issues are excluded by default",
		"#           - REPORT-2",
		"#       routing:",
		"#         profiles:",
		"#           default:",
		"#             issue_prefixes:",
		"#               - WEB",
		"#           # Reconcile this reporting profile with:",
		"#           # workledger plan reconcile --push --adapter=jira-cloud --instance product --route-profile reporting --today",
		"#           reporting: # non-default profile for fixed reporting issue routing",
		"#             reporting_targets: # canonical prefix -> fixed reporting issue key; OPS matches jira_data_center.instances.internal.routing.profiles.default.issue_prefixes",
		"#               OPS: REPORT-1",
		"",
		"# jira_data_center:",
		"#   instances:",
		"#     internal:",
		"#       base_url: https://jira.example.com",
		"#       auth:",
		"#         bearer:",
		"#           token_env: WORKLEDGER_JIRA_DC_INTERNAL_TOKEN",
		"#       routing:",
		"#         profiles:",
		"#           default:",
		"#             issue_prefixes:",
		"#               - OPS",
		"#           # Reconcile this reporting profile with:",
		"#           # workledger plan reconcile --push --adapter=jira-data-center --instance internal --route-profile reporting --today",
		"",
	)
	return []byte(strings.Join(lines, "\n"))
}

func LoadEffective() (EffectiveConfig, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return EffectiveConfig{}, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EffectiveConfig{}, ErrConfigNotFound
		}
		return EffectiveConfig{}, err
	}

	return loadEffective(configPath, data)
}

func ValidateExisting() (EffectiveConfig, []ValidationIssue, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return EffectiveConfig{}, nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EffectiveConfig{}, []ValidationIssue{{Field: "config", Message: "config file does not exist"}}, nil
		}
		return EffectiveConfig{}, nil, err
	}

	effective, err := loadEffective(configPath, data)
	if err == nil {
		return effective, nil, nil
	}

	var validationErr ValidationErrors
	if errors.As(err, &validationErr) {
		return EffectiveConfig{}, validationErr.Issues, nil
	}

	return EffectiveConfig{}, nil, err
}

func loadEffective(configPath string, data []byte) (EffectiveConfig, error) {
	issues, fileConfig := validateConfigBytes(data)
	if len(issues) > 0 {
		return EffectiveConfig{}, ValidationErrors{Issues: issues}
	}

	location := time.Local
	var timezoneName string
	var timezonePtr *string
	if fileConfig.LocalTimezone != "" {
		location, _ = time.LoadLocation(fileConfig.LocalTimezone)
		timezoneName = fileConfig.LocalTimezone
		timezoneCopy := timezoneName
		timezonePtr = &timezoneCopy
	}

	sqlitePath, err := ExpandHome(fileConfig.Storage.SQLitePath)
	if err != nil {
		return EffectiveConfig{}, err
	}

	minimumDuration := DefaultMinimumDurationSecond
	dailyMinimumQuota := DefaultDailyMinimumQuota
	dailyLunch := DefaultDailyLunch
	if fileConfig.Worklogs != nil && fileConfig.Worklogs.MinimumDurationSeconds > 0 {
		minimumDuration = fileConfig.Worklogs.MinimumDurationSeconds
	}
	if fileConfig.Worklogs != nil && fileConfig.Worklogs.DailyMinimumQuotaSeconds > 0 {
		dailyMinimumQuota = fileConfig.Worklogs.DailyMinimumQuotaSeconds
	}
	if fileConfig.Worklogs != nil && strings.TrimSpace(fileConfig.Worklogs.DailyLunch) != "" {
		dailyLunch = strings.TrimSpace(fileConfig.Worklogs.DailyLunch)
	}

	defaultOutput := DefaultOutputMode
	if fileConfig.DefaultOutput != "" {
		defaultOutput = fileConfig.DefaultOutput
	}

	return EffectiveConfig{
		ConfigPath:               configPath,
		DefaultOutput:            defaultOutput,
		SQLitePath:               sqlitePath,
		MinimumDurationSeconds:   minimumDuration,
		DailyMinimumQuotaSeconds: dailyMinimumQuota,
		DailyLunch:               dailyLunch,
		TimezoneName:             timezoneName,
		Location:                 location,
		LocalTimezoneConfig:      timezonePtr,
		File:                     fileConfig,
	}, nil
}

func ExpandHome(path string) (string, error) {
	switch {
	case path == "":
		return "", nil
	case path == "~":
		return os.UserHomeDir()
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	default:
		return filepath.Abs(path)
	}
}

func TightenDir(path string) error {
	return ensureMode(path, 0o700)
}

func TightenFile(path string) error {
	return ensureMode(path, 0o600)
}

func FingerprintEffective(effective EffectiveConfig) (string, error) {
	payload := struct {
		DefaultOutput            string     `json:"default_output"`
		SQLitePath               string     `json:"sqlite_path"`
		MinimumDurationSeconds   int        `json:"minimum_duration_seconds"`
		DailyMinimumQuotaSeconds int        `json:"daily_minimum_quota_seconds"`
		DailyLunch               string     `json:"daily_lunch"`
		Timezone                 string     `json:"timezone"`
		File                     FileConfig `json:"file"`
	}{
		DefaultOutput:            effective.DefaultOutput,
		SQLitePath:               effective.SQLitePath,
		MinimumDurationSeconds:   effective.MinimumDurationSeconds,
		DailyMinimumQuotaSeconds: effective.DailyMinimumQuotaSeconds,
		DailyLunch:               effective.DailyLunch,
		Timezone:                 effective.TimezoneName,
		File:                     effective.File,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func ensureMode(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	current := info.Mode().Perm()
	if current == mode {
		return nil
	}

	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("enforce private permissions on %s: %w", path, err)
	}

	return nil
}

func validateConfigBytes(data []byte) ([]ValidationIssue, FileConfig) {
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return []ValidationIssue{{Field: "config", Message: err.Error()}}, FileConfig{}
	}

	issues := make([]ValidationIssue, 0)
	topLevelKeys := []string{"default_output", "local_timezone", "storage", "worklogs", "jira_cloud", "jira_data_center", "clockify"}
	issues = append(issues, unknownKeyIssues("config", raw, topLevelKeys)...)

	if value, ok := raw["default_output"]; ok {
		str, ok := value.(string)
		if !ok || (str != "table" && str != "json") {
			issues = append(issues, ValidationIssue{Field: "default_output", Message: "must be table or json"})
		}
	}

	issues = append(issues, validateLocalTimezone(raw["local_timezone"])...)
	if _, ok := raw["selection"]; ok {
		issues = append(issues, ValidationIssue{Field: "selection", Message: "is not supported"})
	}
	issues = append(issues, validateStorage(raw["storage"])...)
	issues = append(issues, validateWorklogs(raw["worklogs"])...)
	issues = append(issues, validateDeprecatedRouting(raw["routing"])...)
	issues = append(issues, validateJiraCloud(raw["jira_cloud"])...)
	issues = append(issues, validateJiraData(raw["jira_data_center"])...)
	issues = append(issues, validateClockify(raw["clockify"])...)

	if len(issues) > 0 {
		return issues, FileConfig{}
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var cfg FileConfig
	if err := decoder.Decode(&cfg); err != nil {
		return []ValidationIssue{{Field: "config", Message: err.Error()}}, FileConfig{}
	}

	return nil, normalizeConfig(cfg)
}

func validateLocalTimezone(value any) []ValidationIssue {
	if value == nil {
		return nil
	}

	timezone, ok := value.(string)
	if !ok || timezone == "" {
		return []ValidationIssue{{Field: "local_timezone", Message: "must be a non-empty string"}}
	}

	if _, err := time.LoadLocation(timezone); err != nil {
		return []ValidationIssue{{Field: "local_timezone", Message: "must be a valid timezone"}}
	}

	return nil
}

func validateStorage(value any) []ValidationIssue {
	if value == nil {
		return []ValidationIssue{{Field: "storage", Message: "section is required"}}
	}

	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "storage", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("storage", m, []string{"sqlite_path"})
	pathValue, ok := m["sqlite_path"]
	if !ok {
		issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: "is required"})
		return issues
	}

	path, ok := pathValue.(string)
	if !ok || strings.TrimSpace(path) == "" {
		issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: "must be a non-empty string"})
		return issues
	}

	resolved, err := ExpandHome(path)
	if err != nil {
		issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: err.Error()})
		return issues
	}

	parent := filepath.Dir(resolved)
	if parent == "." {
		issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: "must resolve to an absolute path"})
		return issues
	}

	if info, err := os.Stat(parent); err == nil {
		if !info.IsDir() {
			issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: "parent path must be a directory"})
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		issues = append(issues, ValidationIssue{Field: "storage.sqlite_path", Message: err.Error()})
	}

	return issues
}

func validateWorklogs(value any) []ValidationIssue {
	if value == nil {
		return nil
	}

	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "worklogs", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("worklogs", m, []string{"minimum_duration_seconds", "daily_minimum_quota_seconds", "daily_lunch"})
	if minimum, ok := m["minimum_duration_seconds"]; ok {
		intValue, ok := yamlInt(minimum)
		if !ok || intValue <= 0 {
			issues = append(issues, ValidationIssue{Field: "worklogs.minimum_duration_seconds", Message: "must be a positive whole number of seconds"})
		}
	}
	if quota, ok := m["daily_minimum_quota_seconds"]; ok {
		intValue, ok := yamlInt(quota)
		if !ok || intValue <= 0 {
			issues = append(issues, ValidationIssue{Field: "worklogs.daily_minimum_quota_seconds", Message: "must be a positive whole number of seconds"})
		}
	}
	if lunch, ok := m["daily_lunch"]; ok {
		str, ok := lunch.(string)
		if !ok {
			issues = append(issues, ValidationIssue{Field: "worklogs.daily_lunch", Message: "must use HH:MM-HH:MM"})
		} else if err := ValidateDailyLunchWindow(strings.TrimSpace(str)); err != nil {
			issues = append(issues, ValidationIssue{Field: "worklogs.daily_lunch", Message: err.Error()})
		}
	}

	return issues
}

func ValidateDailyLunchWindow(value string) error {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return fmt.Errorf("must use HH:MM-HH:MM")
	}

	start, err := parseClockMinutes(strings.TrimSpace(parts[0]))
	if err != nil {
		return err
	}
	end, err := parseClockMinutes(strings.TrimSpace(parts[1]))
	if err != nil {
		return err
	}
	if start >= end {
		return fmt.Errorf("must define a positive interval")
	}

	return nil
}

func parseClockMinutes(value string) (int, error) {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%02d:%02d", &hour, &minute); err != nil {
		return 0, fmt.Errorf("must use HH:MM-HH:MM")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("must use HH:MM-HH:MM")
	}
	return hour*60 + minute, nil
}

func validateJiraCloud(value any) []ValidationIssue {
	if value == nil {
		return nil
	}

	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "jira_cloud", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("jira_cloud", m, []string{"instances"})
	instances, ok := m["instances"]
	if !ok {
		return append(issues, ValidationIssue{Field: "jira_cloud.instances", Message: "is required"})
	}

	instanceMap, ok := instances.(map[string]any)
	if !ok {
		return append(issues, ValidationIssue{Field: "jira_cloud.instances", Message: "must be a mapping"})
	}

	for name, entry := range instanceMap {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("jira_cloud.instances.%s", name), Message: "must be a mapping"})
			continue
		}
		prefix := fmt.Sprintf("jira_cloud.instances.%s", name)
		issues = append(issues, unknownKeyIssues(prefix, entryMap, []string{"base_url", "auth", "pull", "routing"})...)
		issues = append(issues, requiredString(entryMap, prefix+".base_url", "base_url")...)
		authMap, ok := entryMap["auth"].(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: prefix + ".auth", Message: "must be a mapping"})
			continue
		}
		issues = append(issues, rejectInlineSecretKey(authMap, prefix+".auth.token", "token", "token_env")...)
		issues = append(issues, unknownKeyIssues(prefix+".auth", authMap, []string{"email", "token", "token_env"})...)
		issues = append(issues, requiredString(authMap, prefix+".auth.email", "email")...)
		issues = append(issues, requiredString(authMap, prefix+".auth.token_env", "token_env")...)
		if pullValue, ok := entryMap["pull"]; ok {
			issues = append(issues, validateJiraPull(pullValue, prefix+".pull")...)
		}
		if routingValue, ok := entryMap["routing"]; ok {
			issues = append(issues, validateJiraInstanceRouting(routingValue, prefix+".routing", issuePrefixOwners(instanceMap))...)
		}
	}

	return issues
}

func validateJiraData(value any) []ValidationIssue {
	if value == nil {
		return nil
	}

	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "jira_data_center", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("jira_data_center", m, []string{"instances"})
	instances, ok := m["instances"]
	if !ok {
		return append(issues, ValidationIssue{Field: "jira_data_center.instances", Message: "is required"})
	}

	instanceMap, ok := instances.(map[string]any)
	if !ok {
		return append(issues, ValidationIssue{Field: "jira_data_center.instances", Message: "must be a mapping"})
	}

	for name, entry := range instanceMap {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("jira_data_center.instances.%s", name), Message: "must be a mapping"})
			continue
		}
		prefix := fmt.Sprintf("jira_data_center.instances.%s", name)
		issues = append(issues, unknownKeyIssues(prefix, entryMap, []string{"base_url", "auth", "pull", "routing"})...)
		issues = append(issues, requiredString(entryMap, prefix+".base_url", "base_url")...)
		authMap, ok := entryMap["auth"].(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: prefix + ".auth", Message: "must be a mapping"})
			continue
		}
		issues = append(issues, unknownKeyIssues(prefix+".auth", authMap, []string{"bearer"})...)
		bearerMap, ok := authMap["bearer"].(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: prefix + ".auth.bearer", Message: "must be a mapping"})
			continue
		}
		issues = append(issues, rejectInlineSecretKey(bearerMap, prefix+".auth.bearer.token", "token", "token_env")...)
		issues = append(issues, unknownKeyIssues(prefix+".auth.bearer", bearerMap, []string{"token", "token_env"})...)
		issues = append(issues, requiredString(bearerMap, prefix+".auth.bearer.token_env", "token_env")...)
		if pullValue, ok := entryMap["pull"]; ok {
			issues = append(issues, validateJiraPull(pullValue, prefix+".pull")...)
		}
		if routingValue, ok := entryMap["routing"]; ok {
			issues = append(issues, validateJiraInstanceRouting(routingValue, prefix+".routing", issuePrefixOwners(instanceMap))...)
		}
	}

	return issues
}

func validateJiraPull(value any, prefix string) []ValidationIssue {
	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: prefix, Message: "must be a mapping"}}
	}
	issues := unknownKeyIssues(prefix, m, []string{"exclude_issues"})
	if raw, ok := m["exclude_issues"]; ok {
		items, ok := raw.([]any)
		if !ok {
			return append(issues, ValidationIssue{Field: prefix + ".exclude_issues", Message: "must be a list"})
		}
		for i, item := range items {
			str, ok := item.(string)
			if !ok || strings.TrimSpace(str) == "" {
				issues = append(issues, ValidationIssue{Field: fmt.Sprintf("%s.exclude_issues[%d]", prefix, i), Message: "must be a non-empty string"})
			}
		}
	}
	return issues
}

func validateDeprecatedRouting(value any) []ValidationIssue {
	if value == nil {
		return nil
	}
	return []ValidationIssue{{Field: "routing", Message: "is not supported in MVP; move Jira routes under jira_* .instances.<name>.routing"}}
}

func excludeIssuesByInstance(instances map[string]any) map[string]map[string]struct{} {
	result := map[string]map[string]struct{}{}
	for name, raw := range instances {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pull, ok := entry["pull"].(map[string]any)
		if !ok {
			continue
		}
		list, ok := pull["exclude_issues"].([]any)
		if !ok {
			continue
		}
		result[name] = map[string]struct{}{}
		for _, item := range list {
			str, ok := item.(string)
			if ok && strings.TrimSpace(str) != "" {
				result[name][str] = struct{}{}
			}
		}
	}
	return result
}

func issuePrefixOwners(instances map[string]any) map[string]map[string][]string {
	owners := map[string]map[string][]string{}
	for instanceName, raw := range instances {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		routing, ok := entry["routing"].(map[string]any)
		if !ok {
			continue
		}
		profiles, ok := routing["profiles"].(map[string]any)
		if !ok {
			continue
		}
		for profileName, rawProfile := range profiles {
			profile, ok := rawProfile.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"issue_prefixes", "reporting_targets"} {
				value, ok := profile[key]
				if !ok {
					continue
				}
				prefixes := routePrefixes(value)
				for _, prefix := range prefixes {
					if _, ok := owners[profileName]; !ok {
						owners[profileName] = map[string][]string{}
					}
					if slices.Contains(owners[profileName][prefix], instanceName) {
						continue
					}
					owners[profileName][prefix] = append(owners[profileName][prefix], instanceName)
				}
			}
		}
	}
	for _, byPrefix := range owners {
		for prefix := range byPrefix {
			slices.Sort(byPrefix[prefix])
		}
	}
	return owners
}

func routePrefixes(value any) []string {
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, raw := range typed {
			if str, ok := raw.(string); ok && strings.TrimSpace(str) != "" {
				items = append(items, str)
			}
		}
		return items
	case map[string]any:
		items := make([]string, 0, len(typed))
		for prefix := range typed {
			if strings.TrimSpace(prefix) != "" {
				items = append(items, prefix)
			}
		}
		return items
	default:
		return nil
	}
}

func validateStringList(value any, field string) []ValidationIssue {
	list, ok := value.([]any)
	if !ok {
		return []ValidationIssue{{Field: field, Message: "must be a list"}}
	}
	issues := make([]ValidationIssue, 0)
	for i, raw := range list {
		str, ok := raw.(string)
		if !ok || strings.TrimSpace(str) == "" {
			issues = append(issues, ValidationIssue{Field: fmt.Sprintf("%s[%d]", field, i), Message: "must be a non-empty string"})
		}
	}
	return issues
}

func validateStringMap(value any, field string) []ValidationIssue {
	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: field, Message: "must be a mapping"}}
	}
	issues := make([]ValidationIssue, 0)
	for key, raw := range m {
		str, ok := raw.(string)
		if strings.TrimSpace(key) == "" || !ok || strings.TrimSpace(str) == "" {
			issues = append(issues, ValidationIssue{Field: field + "." + key, Message: "must map a non-empty prefix to a non-empty string"})
		}
	}
	return issues
}

func validateJiraInstanceRouting(value any, field string, owners map[string]map[string][]string) []ValidationIssue {
	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: field, Message: "must be a mapping"}}
	}
	issues := unknownKeyIssues(field, m, []string{"profiles"})
	profiles, ok := m["profiles"]
	if !ok {
		return append(issues, ValidationIssue{Field: field + ".profiles", Message: "is required"})
	}
	profileMap, ok := profiles.(map[string]any)
	if !ok {
		return append(issues, ValidationIssue{Field: field + ".profiles", Message: "must be a mapping"})
	}
	instanceName := pathBase(strings.TrimSuffix(field, ".routing"))
	for name, rawProfile := range profileMap {
		prefix := field + ".profiles." + name
		profile, ok := rawProfile.(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: prefix, Message: "must be a mapping"})
			continue
		}
		issues = append(issues, unknownKeyIssues(prefix, profile, []string{"issue_prefixes", "reporting_targets"})...)
		_, hasIssuePrefixes := profile["issue_prefixes"]
		_, hasReportingTargets := profile["reporting_targets"]
		if hasIssuePrefixes && hasReportingTargets {
			issues = append(issues, ValidationIssue{Field: prefix, Message: "may not mix issue_prefixes and reporting_targets"})
		}
		if !hasIssuePrefixes && !hasReportingTargets {
			issues = append(issues, ValidationIssue{Field: prefix, Message: "must define issue_prefixes or reporting_targets"})
		}
		if hasIssuePrefixes {
			issues = append(issues, validateStringList(profile["issue_prefixes"], prefix+".issue_prefixes")...)
			for _, owned := range routePrefixes(profile["issue_prefixes"]) {
				if owner := conflictingPrefixOwner(owners, name, owned, instanceName); owner != "" {
					issues = append(issues, ValidationIssue{Field: prefix + ".issue_prefixes", Message: fmt.Sprintf("prefix %s is already owned by %s in profile %s", owned, owner, name)})
				}
			}
		}
		if hasReportingTargets {
			if name == "default" {
				issues = append(issues, ValidationIssue{Field: prefix + ".reporting_targets", Message: "default profile may not use reporting_targets"})
			}
			targets := validateReportingTargets(profile["reporting_targets"], prefix+".reporting_targets")
			issues = append(issues, targets...)
			for owned, targetIssue := range reportingTargetMap(profile["reporting_targets"]) {
				if owner := conflictingPrefixOwner(owners, name, owned, instanceName); owner != "" {
					issues = append(issues, ValidationIssue{Field: prefix + ".reporting_targets", Message: fmt.Sprintf("prefix %s is already owned by %s in profile %s", owned, owner, name)})
				}
				_ = targetIssue
			}
		}
	}
	return issues
}

func conflictingPrefixOwner(owners map[string]map[string][]string, profileName, prefix, instanceName string) string {
	profileOwners, ok := owners[profileName]
	if !ok {
		return ""
	}
	for _, owner := range profileOwners[prefix] {
		if owner != instanceName {
			return owner
		}
	}
	return ""
}

func pathBase(field string) string {
	parts := strings.Split(field, ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func reportingTargetMap(value any) map[string]string {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	items := map[string]string{}
	for prefix, raw := range m {
		targetIssue, ok := raw.(string)
		if ok && strings.TrimSpace(prefix) != "" && strings.TrimSpace(targetIssue) != "" {
			items[prefix] = targetIssue
		}
	}
	return items
}

func validateReportingTargets(value any, field string) []ValidationIssue {
	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: field, Message: "must be a mapping"}}
	}
	issues := make([]ValidationIssue, 0)
	for prefix, raw := range m {
		targetIssue, ok := raw.(string)
		if strings.TrimSpace(prefix) == "" || !ok || strings.TrimSpace(targetIssue) == "" {
			issues = append(issues, ValidationIssue{Field: field + "." + prefix, Message: "must map a non-empty prefix to a non-empty target issue"})
		}
	}
	return issues
}

func validateClockify(value any) []ValidationIssue {
	if value == nil {
		return nil
	}

	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "clockify", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("clockify", m, []string{"workspace_id", "user_id", "auth", "project_mapping"})
	issues = append(issues, requiredString(m, "clockify.workspace_id", "workspace_id")...)
	issues = append(issues, requiredString(m, "clockify.user_id", "user_id")...)
	authMap, ok := m["auth"].(map[string]any)
	if !ok {
		return append(issues, ValidationIssue{Field: "clockify.auth", Message: "must be a mapping"})
	}
	issues = append(issues, rejectInlineSecretKey(authMap, "clockify.auth.api_key", "api_key", "api_key_env")...)
	issues = append(issues, unknownKeyIssues("clockify.auth", authMap, []string{"api_key", "api_key_env"})...)
	issues = append(issues, requiredString(authMap, "clockify.auth.api_key_env", "api_key_env")...)
	if projectMapping, ok := m["project_mapping"]; ok {
		issues = append(issues, validateClockifyProjectMapping(projectMapping)...)
	}

	return issues
}

func validateClockifyProjectMapping(value any) []ValidationIssue {
	m, ok := value.(map[string]any)
	if !ok {
		return []ValidationIssue{{Field: "clockify.project_mapping", Message: "must be a mapping"}}
	}

	issues := unknownKeyIssues("clockify.project_mapping", m, []string{"issue_prefixes", "default_project", "create_issue_tag_if_missing"})
	if prefixes, ok := m["issue_prefixes"]; ok {
		prefixMap, ok := prefixes.(map[string]any)
		if !ok {
			issues = append(issues, ValidationIssue{Field: "clockify.project_mapping.issue_prefixes", Message: "must be a mapping"})
		} else {
			for prefix, rawProject := range prefixMap {
				projectName, ok := rawProject.(string)
				if strings.TrimSpace(prefix) == "" || !ok || strings.TrimSpace(projectName) == "" {
					issues = append(issues, ValidationIssue{Field: "clockify.project_mapping.issue_prefixes." + prefix, Message: "must map a non-empty prefix to a non-empty project name"})
				}
			}
		}
	}
	if defaultProject, ok := m["default_project"]; ok {
		str, ok := defaultProject.(string)
		if !ok || strings.TrimSpace(str) == "" {
			issues = append(issues, ValidationIssue{Field: "clockify.project_mapping.default_project", Message: "must be a non-empty string"})
		}
	}
	if rawCreate, ok := m["create_issue_tag_if_missing"]; ok {
		if _, ok := rawCreate.(bool); !ok {
			issues = append(issues, ValidationIssue{Field: "clockify.project_mapping.create_issue_tag_if_missing", Message: "must be a boolean"})
		}
	}

	return issues
}

func requiredString(m map[string]any, field, key string) []ValidationIssue {
	value, ok := m[key]
	if !ok {
		return []ValidationIssue{{Field: field, Message: "is required"}}
	}

	str, ok := value.(string)
	if !ok || strings.TrimSpace(str) == "" {
		return []ValidationIssue{{Field: field, Message: "must be a non-empty string"}}
	}

	return nil
}

func rejectInlineSecretKey(m map[string]any, field, key, replacement string) []ValidationIssue {
	if _, ok := m[key]; !ok {
		return nil
	}

	return []ValidationIssue{{
		Field:   field,
		Message: fmt.Sprintf("inline secret is not supported; use %s", replacement),
	}}
}

func unknownKeyIssues(prefix string, m map[string]any, allowed []string) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	for key := range m {
		if slices.Contains(allowed, key) {
			continue
		}
		field := key
		if prefix != "config" {
			field = prefix + "." + key
		}
		issues = append(issues, ValidationIssue{Field: field, Message: "is not supported in MVP"})
	}
	return issues
}

func yamlInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if float64(int(typed)) == typed {
			return int(typed), true
		}
	}

	return 0, false
}

func normalizeConfig(cfg FileConfig) FileConfig {
	if cfg.JiraCloud != nil {
		for name, instance := range cfg.JiraCloud.Instances {
			instance.BaseURL = strings.TrimRight(instance.BaseURL, "/")
			cfg.JiraCloud.Instances[name] = instance
		}
	}
	if cfg.JiraData != nil {
		for name, instance := range cfg.JiraData.Instances {
			instance.BaseURL = strings.TrimRight(instance.BaseURL, "/")
			cfg.JiraData.Instances[name] = instance
		}
	}

	return cfg
}

func ResolveClockifyConfig(cfg EffectiveConfig) (*ClockifyConfig, error) {
	_, item, err := ResolveClockifyInstance(cfg, "")
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func ResolveClockifyInstance(cfg EffectiveConfig, name string) (string, ClockifyConfig, error) {
	if cfg.File.Clockify == nil {
		return "", ClockifyConfig{}, errors.New("clockify config is required")
	}

	instanceName := strings.TrimSpace(name)
	if instanceName == "" {
		instanceName = ClockifyInstanceName
	}
	if instanceName != ClockifyInstanceName {
		return "", ClockifyConfig{}, fmt.Errorf("clockify instance %q is not configured", instanceName)
	}
	item := *cfg.File.Clockify
	if item.WorkspaceID == "" || item.UserID == "" {
		return "", ClockifyConfig{}, errors.New("clockify.workspace_id, clockify.user_id, and clockify.auth.api_key_env are required")
	}
	if strings.TrimSpace(item.Auth.APIKey) == "" {
		if item.Auth.APIKeyEnv == "" {
			return "", ClockifyConfig{}, errors.New("clockify.workspace_id, clockify.user_id, and clockify.auth.api_key_env are required")
		}
		apiKey, err := resolveSecretEnv(item.Auth.APIKeyEnv, "clockify.auth.api_key_env")
		if err != nil {
			return "", ClockifyConfig{}, err
		}
		item.Auth.APIKey = apiKey
	} else {
		item.Auth.APIKey = strings.TrimSpace(item.Auth.APIKey)
	}
	return ClockifyInstanceName, item, nil
}

func ResolveJiraCloudInstance(cfg EffectiveConfig, name string) (string, JiraCloudInstance, error) {
	if cfg.File.JiraCloud == nil || len(cfg.File.JiraCloud.Instances) == 0 {
		return "", JiraCloudInstance{}, errors.New("jira_cloud config is required")
	}

	instanceName := name
	if instanceName == "" {
		if len(cfg.File.JiraCloud.Instances) != 1 {
			return "", JiraCloudInstance{}, errors.New("--instance is required when more than one jira_cloud instance is configured")
		}
		for key := range cfg.File.JiraCloud.Instances {
			instanceName = key
		}
	}

	instance, ok := cfg.File.JiraCloud.Instances[instanceName]
	if !ok {
		return "", JiraCloudInstance{}, fmt.Errorf("jira_cloud instance %q is not configured", instanceName)
	}

	if strings.TrimSpace(instance.Auth.Token) == "" {
		if instance.Auth.TokenEnv == "" {
			return "", JiraCloudInstance{}, fmt.Errorf("jira_cloud.instances.%s.auth.token_env is required", instanceName)
		}
		token, err := resolveSecretEnv(instance.Auth.TokenEnv, fmt.Sprintf("jira_cloud.instances.%s.auth.token_env", instanceName))
		if err != nil {
			return "", JiraCloudInstance{}, err
		}
		instance.Auth.Token = token
	} else {
		instance.Auth.Token = strings.TrimSpace(instance.Auth.Token)
	}
	return instanceName, instance, nil
}

func ResolveJiraDataInstance(cfg EffectiveConfig, name string) (string, JiraDataCenterInstance, error) {
	if cfg.File.JiraData == nil || len(cfg.File.JiraData.Instances) == 0 {
		return "", JiraDataCenterInstance{}, errors.New("jira_data_center config is required")
	}

	instanceName := name
	if instanceName == "" {
		if len(cfg.File.JiraData.Instances) != 1 {
			return "", JiraDataCenterInstance{}, errors.New("--instance is required when more than one jira_data_center instance is configured")
		}
		for key := range cfg.File.JiraData.Instances {
			instanceName = key
		}
	}

	instance, ok := cfg.File.JiraData.Instances[instanceName]
	if !ok {
		return "", JiraDataCenterInstance{}, fmt.Errorf("jira_data_center instance %q is not configured", instanceName)
	}

	if strings.TrimSpace(instance.Auth.Bearer.Token) == "" {
		if instance.Auth.Bearer.TokenEnv == "" {
			return "", JiraDataCenterInstance{}, fmt.Errorf("jira_data_center.instances.%s.auth.bearer.token_env is required", instanceName)
		}
		token, err := resolveSecretEnv(instance.Auth.Bearer.TokenEnv, fmt.Sprintf("jira_data_center.instances.%s.auth.bearer.token_env", instanceName))
		if err != nil {
			return "", JiraDataCenterInstance{}, err
		}
		instance.Auth.Bearer.Token = token
	} else {
		instance.Auth.Bearer.Token = strings.TrimSpace(instance.Auth.Bearer.Token)
	}
	return instanceName, instance, nil
}

func resolveSecretEnv(envName string, field string) (string, error) {
	value, ok := os.LookupEnv(envName)
	if !ok || strings.TrimSpace(value) == "" {
		return "", ValidationErrors{Issues: []ValidationIssue{{
			Field:   field,
			Message: fmt.Sprintf("environment variable %s must be set to a non-empty value", envName),
		}}}
	}

	return strings.TrimSpace(value), nil
}
