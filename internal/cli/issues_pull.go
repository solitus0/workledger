package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/issues"
	"github.com/solitus0/workledger/internal/worklogs"
	"github.com/spf13/cobra"
)

func (a *app) newIssuesPullCommand() *cobra.Command {
	var adapter, instance string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Import Jira issues assigned to the authenticated user",
		Long:  "Import assigned Jira issues into the local catalog, including issues without worklogs. By default, check all configured Jira instances and honor their ownership routes and pull exclusions. List only issues created or updated by the pull.",
		Example: "  workledger issues pull\n" +
			"  workledger issues pull --adapter jira-cloud --instance product --output json\n" +
			"  workledger issues pull --output plain",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				requested, _ := cmd.Flags().GetString("output")
				if !cmd.Flags().Changed("output") {
					requested = diagnosticOutputFlag(a.args)
				}
				return a.invalidAssignedPull(assignedPullOutputMode(requested), err.Error())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			requested, _ := cmd.Flags().GetString("output")
			mode := assignedPullOutputMode(requested)
			switch requested {
			case "", "table", "json", "plain":
			default:
				return a.invalidAssignedPull(mode, "output must be table, json, or plain")
			}
			if cmd.Flags().Changed("adapter") && strings.TrimSpace(adapter) == "" {
				return a.invalidAssignedPull(mode, "--adapter requires a Jira adapter name")
			}
			if cmd.Flags().Changed("instance") && strings.TrimSpace(instance) == "" {
				return a.invalidAssignedPull(mode, "--instance requires a configured Jira instance name")
			}
			a.diagnosticsOnStderr = true
			defer func() { a.diagnosticsOnStderr = false }()
			cfg, local, cleanup, err := a.loadService(mode, true, "issues pull")
			if err != nil {
				return err
			}
			defer cleanup()
			result, pullErr := issues.NewService(local).PullAssigned(cmd.Context(), cfg, adapter, instance)
			if errors.Is(pullErr, issues.ErrValidation) {
				return a.invalidAssignedPull(mode, pullErr.Error())
			}
			return a.renderAssignedPull(mode, result, pullErr != nil)
		},
	}
	cmd.Flags().StringVar(&adapter, "adapter", "", "Limit to jira-cloud or jira-data-center")
	cmd.Flags().StringVar(&instance, "instance", "", "Limit to one configured Jira instance")
	cmd.Flags().String("output", "", "Output mode: table, json, or plain (one changed issue key per line)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return a.invalidAssignedPull(assignedPullOutputMode(diagnosticOutputFlag(a.args)), err.Error())
	})
	return cmd
}

func assignedPullOutputMode(requested string) string {
	if requested == "plain" {
		return requested
	}
	return config.DetectOutputMode(requested)
}

func (a *app) invalidAssignedPull(mode, message string) error {
	return a.reportDiagnostic(mode, commandDiagnostic{
		Code: "validation_error", ExitCode: 2, Message: message, Run: "workledger issues pull --help",
	})
}

type assignedPullTargetRecord struct {
	Adapter    string `json:"adapter"`
	Instance   string `json:"instance"`
	Status     string `json:"status"`
	Imported   int    `json:"imported"`
	ReasonCode string `json:"reason_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type assignedPullTargetDiagnostic struct {
	Adapter  string `json:"adapter"`
	Instance string `json:"instance"`
	Status   string `json:"status"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Run      string `json:"run,omitempty"`
}

func (a *app) renderAssignedPull(mode string, result issues.AssignedPullResult, interrupted bool) error {
	targets := make([]assignedPullTargetRecord, 0, len(result.Targets))
	var failures []assignedPullTargetDiagnostic
	failureCodes := make(map[int]string)
	var completed bool
	for _, target := range result.Targets {
		record := assignedPullTargetRecord{Adapter: target.Adapter, Instance: target.Instance, Imported: target.Imported}
		switch {
		case target.Err != nil:
			code, reason, message := assignedPullError(target.Err)
			record.Status, record.ReasonCode = "failed", reason
			failureCodes[code] = reason
			instance := "'" + strings.ReplaceAll(target.Instance, "'", "'\\''") + "'"
			failures = append(failures, assignedPullTargetDiagnostic{
				Adapter: target.Adapter, Instance: target.Instance, Status: record.Status, Code: reason, Message: message,
				Run: "workledger issues pull --adapter " + target.Adapter + " --instance " + instance,
			})
		case target.SkipReason != "":
			record.Status, record.Message = "skipped", target.SkipReason
		default:
			record.Status = "succeeded"
			completed = true
		}
		targets = append(targets, record)
	}
	var diagnostic commandDiagnostic
	switch {
	case interrupted:
		diagnostic = commandDiagnostic{Code: "interrupted", ExitCode: 130, Message: "Jira issue pull interrupted"}
	case len(failures) != 0:
		diagnostic.Message = "Jira issue pull failed"
		if completed {
			diagnostic.Code, diagnostic.ExitCode = "partial_success", 6
			diagnostic.Message = "Jira issue pull completed with failures"
		} else {
			// Report local and configuration errors before auth, missing resources, or upstream errors.
			for _, code := range []int{1, 2, 4, 3, 5} {
				if reason, ok := failureCodes[code]; ok {
					diagnostic.Code, diagnostic.ExitCode = reason, code
					break
				}
			}
		}
	}
	if diagnostic.ExitCode == 0 || completed {
		if err := a.writeAssignedPullResult(mode, result.Items, targets); err != nil {
			return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
		}
	}
	if diagnostic.ExitCode == 0 {
		return nil
	}
	details := failures
	if !completed {
		for _, target := range targets {
			if target.Status == "skipped" {
				details = append(details, assignedPullTargetDiagnostic{
					Adapter: target.Adapter, Instance: target.Instance, Status: target.Status,
					Code: "no_ownership_routes", Message: target.Message,
				})
			}
		}
	}
	if len(details) != 0 {
		diagnostic.Details = details
		if mode != "json" {
			var message strings.Builder
			message.WriteString(diagnostic.Message)
			for _, target := range details {
				detail := strings.Join(strings.Fields(target.Message), " ")
				fmt.Fprintf(&message, "\n  %s/%s %s: %s", target.Adapter, target.Instance, target.Status, detail)
				if target.Run != "" {
					fmt.Fprintf(&message, "\n  After resolving the error, run: %s", target.Run)
				}
			}
			diagnostic.Message = message.String()
		}
	}
	return a.reportDiagnostic(mode, diagnostic)
}

func (a *app) writeAssignedPullResult(mode string, items []worklogs.IssueMetadata, targets []assignedPullTargetRecord) error {
	switch mode {
	case "json":
		records := make([]map[string]any, 0, len(items))
		for _, item := range items {
			records = append(records, map[string]any{
				"issue_key": item.IssueKey, "title": emptyToNil(item.Title), "max_estimate_seconds": int64PtrToAny(item.MaxEstimateSeconds),
				"source_adapter_family": item.SourceAdapterFamily, "source_adapter_instance": item.SourceAdapterInst,
			})
		}
		return a.writeJSON(map[string]any{"items": records, "total": len(records), "targets": targets})
	case "plain":
		for _, item := range items {
			if _, err := fmt.Fprintln(a.stdout, item.IssueKey); err != nil {
				return err
			}
		}
		return nil
	}

	if len(items) > 0 {
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			source := item.SourceAdapterFamily + "/" + item.SourceAdapterInst
			rows = append(rows, []string{item.IssueKey, item.Title, optionalIssueDuration(item.MaxEstimateSeconds), source})
		}
		if err := renderTable(a.stdout, []string{"ISSUE", "JIRA TITLE", "JIRA ESTIMATE", "SOURCE"}, rows); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(a.stdout, "Issues created or updated from Jira: %d\n", len(items)); err != nil {
		return err
	}
	rows := make([][]string, 0, len(targets))
	for _, target := range targets {
		rows = append(rows, []string{target.Adapter, target.Instance, target.Status, strconv.Itoa(target.Imported), strings.Join(strings.Fields(target.Message), " ")})
	}
	return renderTable(a.stdout, []string{"ADAPTER", "INSTANCE", "STATUS", "IMPORTED", "DETAIL"}, rows)
}

func assignedPullError(err error) (int, string, string) {
	if errors.Is(err, issues.ErrValidation) {
		return 2, "validation_error", err.Error()
	}
	var remote *issues.RemoteError
	if !errors.As(err, &remote) {
		return 1, "unexpected_error", err.Error()
	}
	code, reason, _ := classifyJiraRequestError(remote.Err)
	switch reason {
	case "auth_error":
		return code, reason, remote.Adapter + " authentication failed"
	case "not_found":
		return code, reason, remote.Adapter + " resource not found"
	default:
		return 5, "remote_error", err.Error()
	}
}
