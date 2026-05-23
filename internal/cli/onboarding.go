package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mattn/go-isatty"
	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
	"github.com/spf13/cobra"
)

var isTTYReader = func(r io.Reader) bool {
	fd, ok := r.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return isatty.IsTerminal(fd.Fd()) || isatty.IsCygwinTerminal(fd.Fd())
}

type promptInput struct {
	reader io.Reader
	lines  *bufio.Reader
}

func newPromptInput(r io.Reader) *promptInput {
	return &promptInput{reader: r, lines: bufio.NewReader(r)}
}

func (p *promptInput) require(value, label string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if !isTTYReader(p.reader) {
		return "", fmt.Errorf("%s is required", label)
	}
	if _, err := fmt.Fprintf(os.Stderr, "%s: ", label); err != nil {
		return "", err
	}
	line, err := p.lines.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return line, nil
}

func (p *promptInput) requireList(values []string, label string) ([]string, error) {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	if len(items) > 0 {
		return items, nil
	}
	value, err := p.require("", label)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(value, ",")
	items = items[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s is required", label)
	}
	return items, nil
}

func (a *app) newSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Add adapter config blocks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(a.newSetupJiraCloudCommand())
	cmd.AddCommand(a.newSetupJiraDataCommand())
	cmd.AddCommand(a.newSetupClockifyCommand())
	return cmd
}

func (a *app) newSetupJiraCloudCommand() *cobra.Command {
	var params config.SetupJiraCloudParams
	cmd := &cobra.Command{
		Use:   "jira-cloud",
		Short: "Add a Jira Cloud instance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if _, issues, err := config.ValidateExisting(); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			} else if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}

			prompts := newPromptInput(cmd.InOrStdin())
			var err error
			if params.Instance, err = prompts.require(params.Instance, "instance"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.BaseURL, err = prompts.require(params.BaseURL, "base-url"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.Email, err = prompts.require(params.Email, "email"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.TokenEnv, err = prompts.require(params.TokenEnv, "token-env"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.IssuePrefixes, err = prompts.requireList(params.IssuePrefixes, "issue-prefix"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if err := config.AddJiraCloudInstance(params); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.renderSetupResult(mode, map[string]any{
				"created":        true,
				"adapter":        "jira-cloud",
				"instance":       params.Instance,
				"token_env":      params.TokenEnv,
				"issue_prefixes": params.IssuePrefixes,
			})
		},
	}
	cmd.Flags().StringVar(&params.Instance, "instance", "", "Instance name")
	cmd.Flags().StringVar(&params.BaseURL, "base-url", "", "Base URL")
	cmd.Flags().StringVar(&params.Email, "email", "", "Account email")
	cmd.Flags().StringVar(&params.TokenEnv, "token-env", "", "Token env var name")
	cmd.Flags().StringArrayVar(&params.IssuePrefixes, "issue-prefix", nil, "Owned issue prefix")
	return cmd
}

func (a *app) newSetupJiraDataCommand() *cobra.Command {
	var params config.SetupJiraDataParams
	cmd := &cobra.Command{
		Use:   "jira-data-center",
		Short: "Add a Jira Data Center instance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if _, issues, err := config.ValidateExisting(); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			} else if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}

			prompts := newPromptInput(cmd.InOrStdin())
			var err error
			if params.Instance, err = prompts.require(params.Instance, "instance"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.BaseURL, err = prompts.require(params.BaseURL, "base-url"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.TokenEnv, err = prompts.require(params.TokenEnv, "token-env"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.IssuePrefixes, err = prompts.requireList(params.IssuePrefixes, "issue-prefix"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if err := config.AddJiraDataInstance(params); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.renderSetupResult(mode, map[string]any{
				"created":        true,
				"adapter":        "jira-data-center",
				"instance":       params.Instance,
				"token_env":      params.TokenEnv,
				"issue_prefixes": params.IssuePrefixes,
			})
		},
	}
	cmd.Flags().StringVar(&params.Instance, "instance", "", "Instance name")
	cmd.Flags().StringVar(&params.BaseURL, "base-url", "", "Base URL")
	cmd.Flags().StringVar(&params.TokenEnv, "token-env", "", "Token env var name")
	cmd.Flags().StringArrayVar(&params.IssuePrefixes, "issue-prefix", nil, "Owned issue prefix")
	return cmd
}

func (a *app) newSetupClockifyCommand() *cobra.Command {
	var projectMaps []string
	params := config.SetupClockifyParams{APIKeyEnv: "CLOCKIFY_API_KEY"}
	cmd := &cobra.Command{
		Use:   "clockify",
		Short: "Add Clockify config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if _, issues, err := config.ValidateExisting(); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			} else if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}

			prompts := newPromptInput(cmd.InOrStdin())
			var err error
			params.APIKeyEnv = strings.TrimSpace(params.APIKeyEnv)
			if params.APIKeyEnv == "" {
				params.APIKeyEnv = "CLOCKIFY_API_KEY"
			}
			if params.WorkspaceID == "" || params.UserID == "" {
				user, err := a.currentClockifyUserFromEnv(cmd.Context(), params.APIKeyEnv)
				if err != nil {
					return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
				}
				if user != nil {
					if params.WorkspaceID == "" {
						params.WorkspaceID = strings.TrimSpace(user.ActiveWorkspace)
						if params.WorkspaceID == "" {
							params.WorkspaceID = strings.TrimSpace(user.DefaultWorkspace)
						}
					}
					if params.UserID == "" {
						params.UserID = strings.TrimSpace(user.ID)
					}
				}
			}
			if params.WorkspaceID, err = prompts.require(params.WorkspaceID, "workspace-id"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if params.UserID, err = prompts.require(params.UserID, "user-id"); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			params.ProjectMap, err = parseProjectMappings(projectMaps)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			if err := config.UpsertClockifyConfig(params); err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.renderSetupResult(mode, map[string]any{
				"created":      true,
				"adapter":      "clockify",
				"api_key_env":  params.APIKeyEnv,
				"workspace_id": params.WorkspaceID,
				"user_id":      params.UserID,
			})
		},
	}
	cmd.Flags().StringVar(&params.WorkspaceID, "workspace-id", "", "Workspace ID")
	cmd.Flags().StringVar(&params.UserID, "user-id", "", "User ID")
	cmd.Flags().StringVar(&params.APIKeyEnv, "api-key-env", "", "API key env var name")
	cmd.Flags().StringArrayVar(&projectMaps, "project-map", nil, "Clockify project mapping PREFIX=PROJECT")
	return cmd
}

func (a *app) renderSetupResult(mode string, payload map[string]any) error {
	if mode == "json" {
		return a.writeJSON(payload)
	}
	adapter, _ := payload["adapter"].(string)
	switch adapter {
	case "jira-cloud", "jira-data-center":
		instance, _ := payload["instance"].(string)
		tokenEnv, _ := payload["token_env"].(string)
		_, _ = fmt.Fprintf(
			a.stdout,
			"Add this environment variable before running adapter commands:\n\nexport %s=...\n\nThen verify:\n  workledger status --adapter %s --instance %s --explain\n",
			tokenEnv,
			adapter,
			instance,
		)
	case "clockify":
		apiKeyEnv, _ := payload["api_key_env"].(string)
		_, _ = fmt.Fprintf(
			a.stdout,
			"Add this environment variable before running Clockify commands:\n\nexport %s=...\n\nThen verify:\n  workledger status --adapter clockify --explain\n",
			apiKeyEnv,
		)
	default:
		_, _ = fmt.Fprintf(a.stdout, "created %s\n", adapter)
	}
	return nil
}

func (a *app) newConfigEnvCommand() *cobra.Command {
	var printExportTemplate bool
	var dotenvTemplate bool
	cmd := &cobra.Command{
		Use:   "env",
		Short: "List referenced env vars",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if printExportTemplate && dotenvTemplate {
				return a.fail(mode, 2, "validation_error", "--print-export-template and --dotenv-template are mutually exclusive", nil)
			}
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}
			refs := config.EnvReferences(effective)
			if printExportTemplate {
				return a.renderEnvTemplate(mode, "export", refs)
			}
			if dotenvTemplate {
				return a.renderEnvTemplate(mode, "dotenv", refs)
			}
			if mode == "json" {
				items := make([]map[string]any, 0, len(refs))
				for _, ref := range refs {
					items = append(items, map[string]any{"name": ref.Name, "is_set": ref.IsSet})
				}
				return a.writeJSON(map[string]any{"items": items})
			}
			rows := make([][]string, 0, len(refs))
			for _, ref := range refs {
				rows = append(rows, []string{ref.Name, yesNo(ref.IsSet)})
			}
			return renderTable(a.stdout, []string{"NAME", "SET"}, rows)
		},
	}
	cmd.Flags().BoolVar(&printExportTemplate, "print-export-template", false, "Print export shell template")
	cmd.Flags().BoolVar(&dotenvTemplate, "dotenv-template", false, "Print dotenv template")
	return cmd
}

func (a *app) newConfigSummaryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Show effective config summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}
			summary := config.Summary(effective)
			if mode == "json" {
				return a.writeJSON(map[string]any{
					"config_path":                 summary.ConfigPath,
					"default_output":              summary.DefaultOutput,
					"sqlite_path":                 summary.SQLitePath,
					"local_timezone":              emptyToNil(summary.LocalTimezone),
					"minimum_duration_seconds":    summary.MinimumDurationSeconds,
					"daily_minimum_quota_seconds": summary.DailyMinimumQuotaSeconds,
					"day_start":                   summary.DayStart,
					"day_end":                     summary.DayEnd,
					"daily_lunch":                 summary.DailyLunch,
					"jira_instance_count":         summary.JiraInstanceCount,
					"unique_env_var_count":        summary.UniqueEnvVarCount,
					"missing_env_var_count":       summary.MissingEnvVarCount,
					"unique_routed_prefix_count":  summary.UniqueRoutedPrefixCount,
					"reporting_target_count":      summary.ReportingTargetCount,
					"clockify_mapping_count":      summary.ClockifyMappingCount,
				})
			}
			rows := [][]string{
				{"config_path", summary.ConfigPath},
				{"default_output", summary.DefaultOutput},
				{"sqlite_path", summary.SQLitePath},
				{"local_timezone", displayOrDash(summary.LocalTimezone)},
				{"minimum_duration_seconds", fmt.Sprint(summary.MinimumDurationSeconds)},
				{"daily_minimum_quota_seconds", fmt.Sprint(summary.DailyMinimumQuotaSeconds)},
				{"day_start", summary.DayStart},
				{"day_end", summary.DayEnd},
				{"daily_lunch", summary.DailyLunch},
				{"jira_instance_count", fmt.Sprint(summary.JiraInstanceCount)},
				{"unique_env_var_count", fmt.Sprint(summary.UniqueEnvVarCount)},
				{"missing_env_var_count", fmt.Sprint(summary.MissingEnvVarCount)},
				{"unique_routed_prefix_count", fmt.Sprint(summary.UniqueRoutedPrefixCount)},
				{"reporting_target_count", fmt.Sprint(summary.ReportingTargetCount)},
				{"clockify_mapping_count", fmt.Sprint(summary.ClockifyMappingCount)},
			}
			return renderTable(a.stdout, []string{"FIELD", "VALUE"}, rows)
		},
	}
}

func (a *app) newDoctorCommand() *cobra.Command {
	var local, env, routing, connectivity, all bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run local onboarding diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if all {
				local, env, routing, connectivity = true, true, true, true
			}
			if !local && !env && !routing && !connectivity {
				local, env, routing = true, true, true
			}

			items := make([]doctorItem, 0)
			exitCode := 0
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			configValid := len(issues) == 0
			if local {
				if configValid {
					items = append(items, doctorItem{Category: "local", Target: "config", Status: "ok", Message: "config is valid"})
					if err := checkLocalStorageWritable(effective.SQLitePath, "doctor"); err != nil {
						items = append(items, doctorItem{Category: "local", Target: "storage", Status: "error", Message: err.Error()})
						exitCode = firstStatusExitCode(exitCode, 1)
					} else {
						items = append(items, doctorItem{Category: "local", Target: "storage", Status: "ok", Message: "local SQLite storage is writable"})
					}
				} else {
					items = append(items, doctorItem{Category: "local", Target: "config", Status: "error", Message: "config validation failed"})
					exitCode = 2
				}
			}

			if env {
				if !configValid {
					items = append(items, doctorItem{Category: "env", Target: "config", Status: "skipped", Message: "config validation failed"})
				} else {
					refs := config.EnvReferences(effective)
					missing := 0
					for _, ref := range refs {
						status := "ok"
						message := "set"
						if !ref.IsSet {
							status = "error"
							message = "missing"
							missing++
						}
						items = append(items, doctorItem{Category: "env", Target: ref.Name, Status: status, Message: message})
					}
					if missing > 0 && exitCode == 0 {
						exitCode = 2
					}
				}
			}

			if routing {
				if !configValid {
					items = append(items, doctorItem{Category: "routing", Target: "config", Status: "skipped", Message: "config validation failed"})
				} else {
					rules := config.RouteRules(effective)
					if len(rules) == 0 {
						items = append(items, doctorItem{Category: "routing", Target: "rules", Status: "ok", Message: "no routing rules configured"})
					} else {
						items = append(items, doctorItem{Category: "routing", Target: "rules", Status: "ok", Message: fmt.Sprintf("%d routing rules configured", len(rules))})
					}
					audit := config.AuditClockifyMappings(effective)
					for _, prefix := range audit.MissingPrefixes {
						items = append(items, doctorItem{Category: "routing", Target: prefix, Status: "warning", Message: "routed prefix has no Clockify project mapping"})
					}
					for _, prefix := range audit.OrphanedPrefixes {
						items = append(items, doctorItem{Category: "routing", Target: prefix, Status: "warning", Message: "Clockify project mapping is not referenced by Jira routing"})
					}
				}
			}

			if connectivity {
				if !configValid {
					items = append(items, doctorItem{Category: "connectivity", Target: "config", Status: "skipped", Message: "config validation failed"})
				} else {
					rows, code := a.collectAllStatusRows(cmd.Context(), effective)
					for _, row := range rows {
						target := row.Adapter
						if row.Instance != "" {
							target += ":" + row.Instance
						}
						if row.Adapter == "clockify" && row.WorkspaceID != "" {
							target = row.Adapter + ":" + row.WorkspaceID
						}
						status := "ok"
						if row.Status != "OK" {
							status = "error"
						}
						items = append(items, doctorItem{Category: "connectivity", Target: target, Status: status, Message: row.Status})
					}
					exitCode = firstStatusExitCode(exitCode, code)
				}
			}

			if mode == "json" {
				rows := make([]map[string]any, 0, len(items))
				for _, item := range items {
					rows = append(rows, map[string]any{
						"category": item.Category,
						"target":   item.Target,
						"status":   item.Status,
						"message":  item.Message,
					})
				}
				if err := a.writeJSON(map[string]any{"items": rows}); err != nil {
					return err
				}
			} else if err := renderTable(a.stdout, []string{"CATEGORY", "TARGET", "STATUS", "MESSAGE"}, doctorRows(items)); err != nil {
				return err
			}

			if !configValid && exitCode == 0 {
				exitCode = 2
			}
			if exitCode != 0 {
				return exitError{code: exitCode}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "Run local config checks")
	cmd.Flags().BoolVar(&env, "env", false, "Run env var checks")
	cmd.Flags().BoolVar(&routing, "routing", false, "Run routing checks")
	cmd.Flags().BoolVar(&connectivity, "connectivity", false, "Run live adapter checks")
	cmd.Flags().BoolVar(&all, "all", false, "Run all checks")
	return cmd
}

func (a *app) newRoutingCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Routing diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(a.newRoutingListCommand())
	return cmd
}

func (a *app) newRoutingListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List routing rules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}
			rules := config.RouteRules(effective)
			if mode == "json" {
				items := make([]map[string]any, 0, len(rules))
				for _, rule := range rules {
					items = append(items, map[string]any{
						"adapter_family": rule.AdapterFamily,
						"instance":       rule.Instance,
						"profile":        rule.Profile,
						"mode":           rule.Mode,
						"source_prefix":  rule.SourcePrefix,
						"target_issue":   emptyToNil(rule.TargetIssue),
					})
				}
				return a.writeJSON(map[string]any{"items": items})
			}
			rows := make([][]string, 0, len(rules))
			for _, rule := range rules {
				rows = append(rows, []string{rule.AdapterFamily, rule.Instance, rule.Profile, rule.Mode, rule.SourcePrefix, rule.TargetIssue})
			}
			return renderTable(a.stdout, []string{"ADAPTER", "INSTANCE", "PROFILE", "MODE", "PREFIX", "TARGET_ISSUE"}, rows)
		},
	}
}

func (a *app) newRouteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Inspect one issue route",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(a.newRouteExplainCommand())
	return cmd
}

func (a *app) newRouteExplainCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <issue-key>",
		Short: "Explain route ownership and reporting matches",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			issueKey := strings.TrimSpace(args[0])
			if !worklogs.IsValidIssueKey(issueKey) {
				return a.fail(mode, 2, "validation_error", "issue must match <PROJECTKEY>-<NUMBER>", nil)
			}
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}
			explanation := config.ExplainRoute(effective, issueKey)
			if mode == "json" {
				return a.writeJSON(routeExplanationJSON(explanation))
			}
			if _, err := fmt.Fprintf(a.stdout, "result=%s issue=%s clockify_project=%s\n", explanation.Result, explanation.IssueKey, displayOrDash(clockifyProjectName(explanation.ClockifyProject))); err != nil {
				return err
			}
			return renderTable(a.stdout, []string{"TYPE", "ADAPTER", "INSTANCE", "PROFILE", "PREFIX", "TARGET_ISSUE"}, routeExplanationRows(explanation))
		},
	}
}

func (a *app) newClockifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clockify",
		Short: "Clockify diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	mappings := &cobra.Command{
		Use:   "mappings",
		Short: "Clockify project mapping diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	mappings.AddCommand(a.newClockifyMappingsValidateCommand())
	cmd.AddCommand(mappings)
	return cmd
}

func (a *app) newClockifyMappingsValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate Clockify project mappings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}
			clockifyCfg, err := config.ResolveClockifyConfig(effective)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			client := clockifyadapter.NewClient(clockifyCfg.Auth.APIKey)
			projects, err := client.ListProjects(cmd.Context(), clockifyCfg.WorkspaceID)
			if err != nil {
				return a.handleClockifyError(mode, err)
			}

			byName := map[string]int{}
			for _, project := range projects {
				byName[project.Name]++
			}

			audit := config.AuditClockifyMappings(effective)
			rows := make([]clockifyMappingRow, 0)
			exitCode := 0
			if effective.File.Clockify != nil && effective.File.Clockify.ProjectMapping != nil {
				prefixes := make([]string, 0, len(effective.File.Clockify.ProjectMapping.IssuePrefixes))
				for prefix := range effective.File.Clockify.ProjectMapping.IssuePrefixes {
					prefixes = append(prefixes, prefix)
				}
				sort.Strings(prefixes)
				for _, prefix := range prefixes {
					projectName := effective.File.Clockify.ProjectMapping.IssuePrefixes[prefix]
					status := "ok"
					message := "resolved"
					switch byName[projectName] {
					case 0:
						status = "error"
						message = "project not found"
						if exitCode == 0 {
							exitCode = 3
						}
					case 1:
					default:
						status = "error"
						message = "project name is ambiguous"
						if exitCode == 0 {
							exitCode = 5
						}
					}
					rows = append(rows, clockifyMappingRow{Prefix: prefix, Project: projectName, Status: status, Message: message})
				}
			}
			for _, prefix := range audit.MissingPrefixes {
				rows = append(rows, clockifyMappingRow{Prefix: prefix, Status: "warning", Message: "routed prefix has no mapping"})
			}
			for _, prefix := range audit.OrphanedPrefixes {
				project := ""
				if effective.File.Clockify != nil && effective.File.Clockify.ProjectMapping != nil {
					project = effective.File.Clockify.ProjectMapping.IssuePrefixes[prefix]
				}
				rows = append(rows, clockifyMappingRow{Prefix: prefix, Project: project, Status: "warning", Message: "mapping prefix is not referenced by Jira routing"})
			}

			if mode == "json" {
				items := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					items = append(items, map[string]any{
						"prefix":  row.Prefix,
						"project": emptyToNil(row.Project),
						"status":  row.Status,
						"message": row.Message,
					})
				}
				if err := a.writeJSON(map[string]any{"items": items}); err != nil {
					return err
				}
			} else if err := renderTable(a.stdout, []string{"PREFIX", "PROJECT", "STATUS", "MESSAGE"}, clockifyMappingRows(rows)); err != nil {
				return err
			}

			if exitCode != 0 {
				return exitError{code: exitCode}
			}
			return nil
		},
	}
}

type doctorItem struct {
	Category string
	Target   string
	Status   string
	Message  string
}

type clockifyMappingRow struct {
	Prefix  string
	Project string
	Status  string
	Message string
}

func doctorRows(items []doctorItem) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Category, item.Target, item.Status, item.Message})
	}
	return rows
}

func routeExplanationJSON(explanation config.RouteExplanation) map[string]any {
	return map[string]any{
		"issue_key":         explanation.IssueKey,
		"result":            explanation.Result,
		"ownership_matches": routeRulesJSON(explanation.OwnershipMatches),
		"reporting_matches": routeRulesJSON(explanation.ReportingMatches),
		"clockify_project":  clockifyProjectJSON(explanation.ClockifyProject),
	}
}

func routeRulesJSON(rules []config.RouteRule) []map[string]any {
	items := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		items = append(items, map[string]any{
			"adapter_family": rule.AdapterFamily,
			"instance":       rule.Instance,
			"profile":        rule.Profile,
			"mode":           rule.Mode,
			"source_prefix":  rule.SourcePrefix,
			"target_issue":   emptyToNil(rule.TargetIssue),
		})
	}
	return items
}

func clockifyProjectJSON(item *config.ClockifyProjectResolution) any {
	if item == nil {
		return nil
	}
	return map[string]any{
		"project_name":  item.ProjectName,
		"source_prefix": emptyToNil(item.SourcePrefix),
		"used_default":  item.UsedDefault,
	}
}

func routeExplanationRows(explanation config.RouteExplanation) [][]string {
	rows := make([][]string, 0, len(explanation.OwnershipMatches)+len(explanation.ReportingMatches))
	for _, rule := range explanation.OwnershipMatches {
		rows = append(rows, []string{"ownership", rule.AdapterFamily, rule.Instance, rule.Profile, rule.SourcePrefix, rule.TargetIssue})
	}
	for _, rule := range explanation.ReportingMatches {
		rows = append(rows, []string{"reporting", rule.AdapterFamily, rule.Instance, rule.Profile, rule.SourcePrefix, rule.TargetIssue})
	}
	return rows
}

func clockifyProjectName(item *config.ClockifyProjectResolution) string {
	if item == nil {
		return ""
	}
	return item.ProjectName
}

func clockifyMappingRows(items []clockifyMappingRow) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Prefix, item.Project, item.Status, item.Message})
	}
	return rows
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (a *app) renderEnvTemplate(mode, format string, refs []config.EnvVarRef) error {
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		if format == "export" {
			lines = append(lines, fmt.Sprintf("export %s=", ref.Name))
		} else {
			lines = append(lines, ref.Name+"=")
		}
	}
	if mode == "json" {
		return a.writeJSON(map[string]any{"format": format, "lines": lines})
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func parseProjectMappings(values []string) (map[string]string, error) {
	items := make(map[string]string, len(values))
	for _, value := range values {
		prefix, project, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(prefix) == "" || strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("invalid project-map %q: expected PREFIX=PROJECT", value)
		}
		items[strings.TrimSpace(prefix)] = strings.TrimSpace(project)
	}
	return items, nil
}
