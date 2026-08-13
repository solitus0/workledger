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
	cmd.Flags().StringArrayVar(&params.IssuePrefixes, "issue-prefix", nil, "Jira project key used to route issues to this instance (for example, PROJ for PROJ-123)")
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
	cmd.Flags().StringArrayVar(&params.IssuePrefixes, "issue-prefix", nil, "Jira project key used to route issues to this instance (for example, OPS for OPS-42)")
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
		tokenEnv, _ := payload["token_env"].(string)
		_, _ = fmt.Fprintf(
			a.stdout,
			"Add this environment variable before running adapter commands:\n\nexport %s=...\n\nThen verify:\n  workledger status\n",
			tokenEnv,
		)
	case "clockify":
		apiKeyEnv, _ := payload["api_key_env"].(string)
		_, _ = fmt.Fprintf(
			a.stdout,
			"Add this environment variable before running Clockify commands:\n\nexport %s=...\n\nThen verify:\n  workledger status\n",
			apiKeyEnv,
		)
	default:
		_, _ = fmt.Fprintf(a.stdout, "created %s\n", adapter)
	}
	return nil
}

func (a *app) newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Run local status diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)

			items := make([]statusItem, 0)
			exitCode := 0
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			configValid := len(issues) == 0
			if configValid {
				items = append(items, statusItem{Category: "local", Target: "config", Status: "ok", Message: "config is valid"})
				if err := checkLocalStorageWritable(effective.SQLitePath, "status"); err != nil {
					items = append(items, statusItem{Category: "local", Target: "storage", Status: "error", Message: err.Error()})
					exitCode = firstNonZeroExitCode(exitCode, 1)
				} else {
					items = append(items, statusItem{Category: "local", Target: "storage", Status: "ok", Message: "local SQLite storage is writable"})
				}
			} else {
				items = append(items, statusItem{Category: "local", Target: "config", Status: "error", Message: "config validation failed"})
				exitCode = 2
			}

			if !configValid {
				items = append(items, statusItem{Category: "env", Target: "config", Status: "skipped", Message: "config validation failed"})
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
					items = append(items, statusItem{Category: "env", Target: ref.Name, Status: status, Message: message})
				}
				if missing > 0 && exitCode == 0 {
					exitCode = 2
				}
			}

			if !configValid {
				items = append(items, statusItem{Category: "routing", Target: "config", Status: "skipped", Message: "config validation failed"})
			} else {
				rules := config.RouteRules(effective)
				if len(rules) == 0 {
					items = append(items, statusItem{Category: "routing", Target: "rules", Status: "ok", Message: "no routing rules configured"})
				} else {
					items = append(items, statusItem{Category: "routing", Target: "rules", Status: "ok", Message: fmt.Sprintf("%d routing rules configured", len(rules))})
				}
				audit := config.AuditClockifyMappings(effective)
				for _, prefix := range audit.MissingPrefixes {
					items = append(items, statusItem{Category: "routing", Target: prefix, Status: "warning", Message: "routed prefix has no Clockify project mapping"})
				}
				for _, prefix := range audit.OrphanedPrefixes {
					items = append(items, statusItem{Category: "routing", Target: prefix, Status: "warning", Message: "Clockify project mapping is not referenced by Jira routing"})
				}
			}

			if !configValid {
				items = append(items, statusItem{Category: "connectivity", Target: "config", Status: "skipped", Message: "config validation failed"})
			} else {
				rows, code := a.collectAllConnectivityRows(cmd.Context(), effective)
				for _, row := range rows {
					target := row.Adapter
					if row.Instance != "" {
						target += ":" + row.Instance
					}
					if row.Adapter == "clockify" && row.WorkspaceID != "" {
						target = row.Adapter + ":" + row.WorkspaceID
					}
					status := "ok"
					message := row.User
					if row.Adapter == "clockify" {
						message = row.UserID
					}
					if row.Status != "OK" {
						status = "error"
						message = row.Status
					}
					items = append(items, statusItem{Category: "connectivity", Target: target, Status: status, Message: message})
				}
				exitCode = firstNonZeroExitCode(exitCode, code)
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
			} else if err := renderTable(a.stdout, []string{"CATEGORY", "TARGET", "STATUS", "MESSAGE"}, statusRows(items)); err != nil {
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

type statusItem struct {
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

func statusRows(items []statusItem) [][]string {
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
