package cli

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/solitus0/workledger/internal/worklogs"
	"github.com/spf13/cobra"
)

func (a *app) newIssuesSetCommand() *cobra.Command {
	var issue, title, estimate, epic string
	var clearTitle, clearEstimate, clearEpic bool
	cmd := &cobra.Command{
		Use: "set", Short: "Create an issue or change its optional local metadata", Args: cobra.NoArgs,
		Example: "  workledger issues set --issue SBAH-1\n  workledger issues set --issue SBAH-2 --title \"Product data lifecycle\" --max-estimate 16h --epic \"Product documents\"\n  workledger issues set --issue SBAH-2 --clear-max-estimate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if (cmd.Flags().Changed("title") && clearTitle) || (cmd.Flags().Changed("max-estimate") && clearEstimate) || (cmd.Flags().Changed("epic") && clearEpic) {
				return a.fail(mode, 2, "validation_error", "cannot set and clear the same field", nil)
			}
			patch := worklogs.IssueMetadataPatch{IssueKey: issue, TitleSet: clearTitle, MaxEstimateSet: clearEstimate, EpicSet: clearEpic}
			if cmd.Flags().Changed("title") {
				patch.Title, patch.TitleSet = &title, true
			}
			if cmd.Flags().Changed("epic") {
				patch.Epic, patch.EpicSet = &epic, true
			}
			if cmd.Flags().Changed("max-estimate") {
				value, err := worklogs.ParseMaxEstimate(estimate)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				patch.MaxEstimateSeconds, patch.MaxEstimateSet = &value, true
			}
			return a.applyIssues(cmd, []worklogs.IssueMetadataPatch{patch}, false)
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Canonical issue key")
	cmd.Flags().StringVar(&epic, "epic", "", "Local epic group name")
	cmd.Flags().BoolVar(&clearEpic, "clear-epic", false, "Remove local epic membership")
	cmd.Flags().StringVar(&title, "title", "", "Local issue title")
	cmd.Flags().StringVar(&estimate, "max-estimate", "", "Positive whole-second maximum estimate, e.g. 16h")
	cmd.Flags().BoolVar(&clearTitle, "clear-title", false, "Clear the local title")
	cmd.Flags().BoolVar(&clearEstimate, "clear-max-estimate", false, "Clear the manual estimate override")
	return cmd
}

func (a *app) newIssuesApplyCommand() *cobra.Command {
	var file string
	var stdin, dry bool
	cmd := &cobra.Command{
		Use: "apply", Short: "Apply a JSON array of local issue patches", Args: cobra.NoArgs,
		Example: "  workledger issues apply --file issues.json --dry --output json\n  workledger issues apply --file issues.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if (file == "") == !stdin {
				return a.fail(mode, 2, "validation_error", "apply requires exactly one of file or stdin", nil)
			}
			var data []byte
			var err error
			if stdin {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(file)
			}
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			patches, err := worklogs.ParseIssueMetadataPatches(data)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.applyIssues(cmd, patches, dry)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Path to issue patch JSON")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read issue patch JSON from stdin")
	cmd.Flags().BoolVar(&dry, "dry", false, "Validate without writing")
	return cmd
}

func (a *app) applyIssues(cmd *cobra.Command, patches []worklogs.IssueMetadataPatch, dry bool) error {
	mode := outputMode(cmd)
	if err := worklogs.ValidateIssueMetadataPatches(patches); err != nil {
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	}
	for i := range patches {
		if patches[i].Epic != nil {
			epic := strings.TrimSpace(*patches[i].Epic)
			patches[i].Epic = &epic
		}
		if patches[i].Title != nil {
			title := strings.TrimSpace(*patches[i].Title)
			patches[i].Title = &title
		}
	}
	cfg, service, cleanup, err := a.loadService(mode, !dry, "issues "+cmd.Name())
	if err != nil {
		return err
	}
	defer cleanup()
	if err := service.ApplyIssueMetadata(cmd.Context(), patches, dry); err != nil {
		return a.handleWorklogError(mode, cfg, err)
	}
	if mode == "json" {
		return a.writeJSON(map[string]any{"dry_run": dry, "items": patches, "total": len(patches)})
	}
	rows := make([][]string, 0, len(patches))
	for _, patch := range patches {
		title, estimate, epic := "unchanged", "unchanged", "unchanged"
		if patch.TitleSet {
			title = "cleared"
			if patch.Title != nil {
				title = *patch.Title
			}
		}
		if patch.MaxEstimateSet {
			estimate = "cleared"
			if patch.MaxEstimateSeconds != nil {
				estimate = fmt.Sprint(*patch.MaxEstimateSeconds)
			}
		}
		if patch.EpicSet {
			epic = "cleared"
			if patch.Epic != nil {
				epic = *patch.Epic
			}
		}
		rows = append(rows, []string{patch.IssueKey, title, epic, estimate})
	}
	if dry {
		fmt.Fprintln(a.stdout, "Dry run: no issues written")
	}
	return renderTable(a.stdout, []string{"ISSUE", "TITLE", "EPIC", "MANUAL_MAX_ESTIMATE_SECONDS"}, rows)
}

func (a *app) newIssuesEpicsCommand() *cobra.Command {
	var issuePrefix, search string
	cmd := &cobra.Command{
		Use: "epics", Short: "List local epic groups and lifetime estimate budgets", Args: cobra.NoArgs,
		Example: "  workledger issues epics --issue-prefix SBAH\n  workledger issues epics --search documents --output json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			search = strings.TrimSpace(search)
			cfg, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()
			items, err := service.SearchIssues(cmd.Context(), issuePrefix, "", "")
			if err != nil {
				return a.handleWorklogError(mode, cfg, err)
			}
			if search != "" {
				filtered := make([]worklogs.IssueMetadata, 0, len(items))
				for _, item := range items {
					if strings.Contains(strings.ToLower(epicLabel(item.Epic)), strings.ToLower(search)) {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			groups := worklogs.GroupIssueEpics(items)
			if mode == "json" {
				records := make([]map[string]any, 0, len(groups))
				for _, group := range groups {
					record := issueSummaryJSON(group.IssueSummary)
					record["epic"] = emptyToNil(group.Epic)
					records = append(records, record)
				}
				return a.writeJSON(map[string]any{
					"filters": map[string]any{"issue_prefix": emptyToNil(issuePrefix), "search": emptyToNil(search)},
					"items":   records, "total": len(records), "summary": issueSummaryJSON(worklogs.SummarizeIssues(items)),
				})
			}
			rows := make([][]string, 0, len(groups))
			for _, group := range groups {
				rows = append(rows, []string{
					epicLabel(group.Epic), fmt.Sprint(group.IssueCount), fmt.Sprint(group.EstimatedIssueCount),
					fmt.Sprint(group.UnestimatedIssueCount), issueSummaryEstimate(group.IssueSummary),
					humanDuration(int(group.LoggedSeconds)), issueSummaryRemaining(group.IssueSummary), humanDuration(int(group.OverrunSeconds)),
				})
			}
			if err := renderTable(a.stdout, []string{"EPIC", "ISSUES", "ESTIMATED", "UNESTIMATED", "ESTIMATE", "LOGGED", "REMAINING", "OVERRUN"}, rows); err != nil {
				return err
			}
			return renderIssueSummary(a.stdout, worklogs.SummarizeIssues(items))
		},
	}
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter members by issue project prefix")
	cmd.Flags().StringVar(&search, "search", "", "Filter by case-insensitive epic name substring")
	return cmd
}

func issueMatchesSearch(item worklogs.IssueMetadata, search string) bool {
	search = strings.ToLower(search)
	return search == "" || strings.Contains(strings.ToLower(item.IssueKey), search) ||
		strings.Contains(strings.ToLower(item.Title), search) || strings.Contains(strings.ToLower(item.Epic), search)
}

func epicLabel(epic string) string {
	if epic == "" {
		return "Ungrouped"
	}
	return epic
}

func optionalIssueDuration(seconds *int64) string {
	if seconds == nil {
		return "unknown"
	}
	return humanDuration(int(*seconds))
}

func issueSummaryJSON(summary worklogs.IssueSummary) map[string]any {
	return map[string]any{
		"issue_count": summary.IssueCount, "estimated_issue_count": summary.EstimatedIssueCount,
		"unestimated_issue_count": summary.UnestimatedIssueCount, "logged_seconds": summary.LoggedSeconds,
		"estimated_seconds": summary.EstimatedSeconds, "remaining_seconds": summary.RemainingSeconds,
		"overrun_seconds": summary.OverrunSeconds,
	}
}

func issueSummaryEstimate(summary worklogs.IssueSummary) string {
	if summary.EstimatedIssueCount == 0 {
		return "unknown"
	}
	return humanDuration(int(summary.EstimatedSeconds))
}

func issueSummaryRemaining(summary worklogs.IssueSummary) string {
	if summary.EstimatedIssueCount == 0 {
		return "unknown"
	}
	value := humanDuration(int(summary.RemainingSeconds))
	if summary.UnestimatedIssueCount > 0 {
		return value + " known"
	}
	return value
}

func renderIssueSummary(w io.Writer, summary worklogs.IssueSummary) error {
	if summary.IssueCount == 0 {
		_, err := fmt.Fprintln(w, "\nTotals: 0 issues, 0m logged; no estimates")
		return err
	}
	_, err := fmt.Fprintf(w, "\nTotals: %d issues, %s logged, %s estimated, %s remaining, %s overrun; %d unestimated\n",
		summary.IssueCount, humanDuration(int(summary.LoggedSeconds)), issueSummaryEstimate(summary),
		issueSummaryRemaining(summary), humanDuration(int(summary.OverrunSeconds)), summary.UnestimatedIssueCount)
	return err
}

func (a *app) renderIssuesTable(items []worklogs.IssueMetadata) error {
	if err := renderTable(a.stdout, []string{"ISSUE", "TITLE", "EPIC", "ESTIMATE", "LOGGED", "REMAINING", "OVERRUN", "ESTIMATE_SOURCE"}, issueRows(items)); err != nil {
		return err
	}
	return renderIssueSummary(a.stdout, worklogs.SummarizeIssues(items))
}

func sortIssueRows(items []worklogs.IssueMetadata) {
	slices.SortFunc(items, func(a, b worklogs.IssueMetadata) int {
		aPrefix, aNumber, _ := strings.Cut(a.IssueKey, "-")
		bPrefix, bNumber, _ := strings.Cut(b.IssueKey, "-")
		if prefix := strings.Compare(aPrefix, bPrefix); prefix != 0 {
			return prefix
		}
		if length := cmp.Compare(len(aNumber), len(bNumber)); length != 0 {
			return length
		}
		return strings.Compare(aNumber, bNumber)
	})
}
