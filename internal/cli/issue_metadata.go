package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/solitus0/workledger/internal/worklogs"
	"github.com/spf13/cobra"
)

func (a *app) newIssueMetadataSetCommand() *cobra.Command {
	var issue, title, estimate string
	var clearTitle, clearEstimate bool
	cmd := &cobra.Command{
		Use: "set", Short: "Set or clear local issue decorations", Args: cobra.NoArgs,
		Example: "  workledger issue-metadata set --issue SBAH-2 --title \"Product data lifecycle\" --max-estimate 16h\n  workledger issue-metadata set --issue SBAH-2 --clear-max-estimate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if (cmd.Flags().Changed("title") && clearTitle) || (cmd.Flags().Changed("max-estimate") && clearEstimate) {
				return a.fail(mode, 2, "validation_error", "cannot set and clear the same field", nil)
			}
			patch := worklogs.IssueMetadataPatch{IssueKey: issue, TitleSet: clearTitle, MaxEstimateSet: clearEstimate}
			if cmd.Flags().Changed("title") {
				patch.Title, patch.TitleSet = &title, true
			}
			if cmd.Flags().Changed("max-estimate") {
				value, err := worklogs.ParseMaxEstimate(estimate)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				patch.MaxEstimateSeconds, patch.MaxEstimateSet = &value, true
			}
			return a.applyIssueMetadata(cmd, []worklogs.IssueMetadataPatch{patch}, false)
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Canonical issue key")
	cmd.Flags().StringVar(&title, "title", "", "Local issue title")
	cmd.Flags().StringVar(&estimate, "max-estimate", "", "Positive whole-second maximum estimate, e.g. 16h")
	cmd.Flags().BoolVar(&clearTitle, "clear-title", false, "Clear the local title")
	cmd.Flags().BoolVar(&clearEstimate, "clear-max-estimate", false, "Clear the manual estimate override")
	return cmd
}

func (a *app) newIssueMetadataApplyCommand() *cobra.Command {
	var file string
	var stdin, dry bool
	cmd := &cobra.Command{
		Use: "apply", Short: "Apply a JSON array of local issue metadata patches", Args: cobra.NoArgs,
		Example: "  workledger issue-metadata apply --file issue-metadata.json --dry --output json\n  workledger issue-metadata apply --file issue-metadata.json",
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
			return a.applyIssueMetadata(cmd, patches, dry)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Path to metadata patch JSON")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read metadata patch JSON from stdin")
	cmd.Flags().BoolVar(&dry, "dry", false, "Validate without writing")
	return cmd
}

func (a *app) applyIssueMetadata(cmd *cobra.Command, patches []worklogs.IssueMetadataPatch, dry bool) error {
	mode := outputMode(cmd)
	if err := worklogs.ValidateIssueMetadataPatches(patches); err != nil {
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	}
	for i := range patches {
		if patches[i].Title != nil {
			title := strings.TrimSpace(*patches[i].Title)
			patches[i].Title = &title
		}
	}
	cfg, service, cleanup, err := a.loadService(mode, !dry, "issue-metadata "+cmd.Name())
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
		title, estimate := "unchanged", "unchanged"
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
		rows = append(rows, []string{patch.IssueKey, title, estimate})
	}
	if dry {
		fmt.Fprintln(a.stdout, "Dry run: no metadata written")
	}
	return renderTable(a.stdout, []string{"ISSUE", "TITLE", "MANUAL_MAX_ESTIMATE_SECONDS"}, rows)
}
