package cli

import (
	"fmt"

	"github.com/solitus0/workledger/internal/worklogs"
	"github.com/spf13/cobra"
)

func (a *app) newWorklogsAssignCommand() *cobra.Command {
	var issue string
	var dry bool
	cmd := &cobra.Command{
		Use:     "assign <id>... --issue <KEY>",
		Short:   "Assign an issue key to explicitly selected unassigned worklogs",
		Args:    cobra.MinimumNArgs(1),
		Example: "  workledger worklogs assign <id> <id> --issue PROJ-123 --dry",
		RunE: func(cmd *cobra.Command, ids []string) error {
			mode := outputMode(cmd)
			cfg, service, cleanup, err := a.loadService(mode, !dry, "worklogs assign")
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := service.Assign(cmd.Context(), worklogs.AssignInput{IDs: ids, IssueKey: issue}, dry)
			if err != nil {
				if mode != "json" {
					fmt.Fprintln(a.stderr, "Resolve conflicts with worklogs show, update, or delete, then retry the selected batch.")
				}
				return a.handleWorklogError(mode, cfg, err)
			}
			if mode == "json" {
				return a.writeJSON(map[string]any{"dry_run": dry, "records": worklogRecordsJSON(result.Records, cfg.Location, false), "total": len(result.Records)})
			}
			if err := renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, cfg.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(result.Records), sumActiveDurationSeconds(result.Records), "worklogs")
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Destination issue key (required)")
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview assignment without writing")
	return cmd
}
