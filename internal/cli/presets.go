package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
)

func (a *app) newPresetsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "presets", Short: "Manage reusable worklog presets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	cmd.AddCommand(a.newPresetAddCommand(), a.newPresetListCommand(), a.newPresetShowCommand(), a.newPresetUpdateCommand(), a.newPresetDeleteCommand(), a.newPresetApplyCommand())
	return cmd
}

func (a *app) newPresetAddCommand() *cobra.Command {
	var issue, start, duration, description string
	cmd := &cobra.Command{
		Use: "add <slug>", Short: "Add a worklog preset", Args: cobra.ExactArgs(1),
		Example: `  workledger presets add daily-standup --issue PROJ-123 --start 09:45 --duration 15m --description "Daily standup"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			cfg, service, cleanup, err := a.loadPresetService(mode, true, "presets add")
			if err != nil {
				return err
			}
			defer cleanup()
			item, err := service.Create(cmd.Context(), cfg, presets.CreateInput{Name: args[0], IssueKey: issue, StartTime: start, Duration: duration, Description: description})
			if err != nil {
				return a.handlePresetError(mode, err)
			}
			return a.renderPreset(mode, item)
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&start, "start", "", "Local start time in HH:MM form, e.g. 09:00")
	cmd.Flags().StringVar(&duration, "duration", "", "Worklog duration")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	return cmd
}

func (a *app) newPresetListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List worklog presets", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			_, service, cleanup, err := a.loadPresetService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()
			items, err := service.List(cmd.Context(), "", 0)
			if err != nil {
				return a.handlePresetError(mode, err)
			}
			if mode == "json" {
				rows := make([]map[string]any, 0, len(items))
				for _, item := range items {
					rows = append(rows, presetJSON(item))
				}
				return a.writeJSON(map[string]any{"items": rows})
			}
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, presetRow(item))
			}
			return renderTable(a.stdout, []string{"NAME", "ISSUE", "START", "DURATION", "DESCRIPTION", "LAST USED"}, rows)
		},
	}
}

func (a *app) newPresetShowCommand() *cobra.Command {
	return &cobra.Command{
		Use: "show <slug>", Short: "Show a worklog preset", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			_, service, cleanup, err := a.loadPresetService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()
			item, err := service.Show(cmd.Context(), args[0])
			if err != nil {
				return a.handlePresetError(mode, err)
			}
			return a.renderPreset(mode, item)
		},
	}
}

func (a *app) newPresetUpdateCommand() *cobra.Command {
	var name, issue, start, duration, description string
	cmd := &cobra.Command{
		Use: "update <slug>", Short: "Update a worklog preset", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			cfg, service, cleanup, err := a.loadPresetService(mode, true, "presets update")
			if err != nil {
				return err
			}
			defer cleanup()
			patch := presets.PatchInput{
				Name: changedString(cmd, "name", name), IssueKey: changedString(cmd, "issue", issue), StartTime: changedString(cmd, "start", start),
				Duration: changedString(cmd, "duration", duration), Description: changedString(cmd, "description", description),
			}
			item, err := service.Update(cmd.Context(), cfg, args[0], patch)
			if err != nil {
				return a.handlePresetError(mode, err)
			}
			return a.renderPreset(mode, item)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New preset slug")
	cmd.Flags().StringVar(&issue, "issue", "", "New issue key")
	cmd.Flags().StringVar(&start, "start", "", "New local start time in HH:MM form")
	cmd.Flags().StringVar(&duration, "duration", "", "New worklog duration")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	return cmd
}

func (a *app) newPresetDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use: "delete <slug>", Short: "Delete a worklog preset", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			_, service, cleanup, err := a.loadPresetService(mode, true, "presets delete")
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := service.Delete(cmd.Context(), args[0], 0)
			if err != nil {
				return a.handlePresetError(mode, err)
			}
			if mode == "json" {
				return a.writeJSON(map[string]any{"name": result.Name, "deleted_at": result.DeletedAt.Format(time.RFC3339)})
			}
			return renderTable(a.stdout, []string{"NAME", "DELETED"}, [][]string{{result.Name, result.DeletedAt.Format(time.RFC3339)}})
		},
	}
}

func (a *app) newPresetApplyCommand() *cobra.Command {
	var date, issue, start, duration, description string
	var dry, force bool
	cmd := &cobra.Command{
		Use: "apply <slug>", Short: "Apply a preset to one local day", Args: cobra.ExactArgs(1),
		Example: "  workledger presets apply daily-standup --date tomorrow\n  workledger presets apply daily-standup --date 2026-09-07 --start 10:00 --dry",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			cfg, service, cleanup, err := a.loadPresetService(mode, !dry, "presets apply")
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := service.Apply(cmd.Context(), cfg, args[0], presets.ApplyInput{
				Date: date, IssueKey: changedString(cmd, "issue", issue), StartTime: changedString(cmd, "start", start),
				Duration: changedString(cmd, "duration", duration), Description: changedString(cmd, "description", description), DryRun: dry, Force: force,
			})
			if err != nil {
				if errors.Is(err, presets.ErrNotFound) || errors.Is(err, presets.ErrValidation) || errors.Is(err, presets.ErrConflict) {
					return a.handlePresetError(mode, err)
				}
				return a.handleWorklogError(mode, cfg, err)
			}
			record := result.Records[0]
			if mode == "json" {
				if dry {
					return a.writeJSON(map[string]any{"dry_run": true, "record": worklogPreviewJSON(record, cfg.Location)})
				}
				return a.writeJSON(worklogRecordJSON(record, cfg.Location))
			}
			if dry {
				return renderTable(a.stdout, []string{"ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, cfg.Location, []string{"issue_key", "started_at", "duration_seconds", "description"}, 0))
			}
			return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, cfg.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "Target local date: YYYY-MM-DD, today, yesterday, tomorrow, mon..sun, +Nd, or -Nd")
	cmd.Flags().StringVar(&issue, "issue", "", "Override issue key")
	cmd.Flags().StringVar(&start, "start", "", "Override local start time in HH:MM form")
	cmd.Flags().StringVar(&duration, "duration", "", "Override worklog duration")
	cmd.Flags().StringVar(&description, "description", "", "Override description")
	cmd.Flags().BoolVar(&dry, "dry", false, "Validate without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass duplicate and overlap validation")
	return cmd
}

func changedString(cmd *cobra.Command, name, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

func (a *app) loadPresetService(mode string, requireWrite bool, operation string) (config.EffectiveConfig, *presets.Service, func(), error) {
	cfg, store, cleanup, err := a.loadStore(mode, requireWrite, operation)
	if err != nil {
		return config.EffectiveConfig{}, nil, nil, err
	}
	return cfg, presets.NewService(store), cleanup, nil
}

func (a *app) handlePresetError(mode string, err error) error {
	switch {
	case errors.Is(err, presets.ErrNotFound):
		return a.fail(mode, 3, "not_found", "worklog preset not found", nil)
	case errors.Is(err, presets.ErrConflict):
		return a.fail(mode, 2, "conflict", "worklog preset changed since it was selected", nil)
	case errors.Is(err, presets.ErrValidation):
		var validation presets.ValidationError
		if errors.As(err, &validation) {
			return a.fail(mode, 2, "validation_error", err.Error(), validation.Issues)
		}
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	default:
		return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}
}

func (a *app) renderPreset(mode string, item presets.Preset) error {
	if mode == "json" {
		return a.writeJSON(presetJSON(item))
	}
	return renderTable(a.stdout, []string{"NAME", "ISSUE", "START", "DURATION", "DESCRIPTION", "LAST USED"}, [][]string{presetRow(item)})
}

func presetRow(item presets.Preset) []string {
	lastUsed := "-"
	if item.LastUsedAt != nil {
		lastUsed = item.LastUsedAt.Format(time.RFC3339)
	}
	return []string{item.Name, item.IssueKey, item.StartTime, humanDuration(item.DurationSeconds), item.Description, lastUsed}
}

func presetJSON(item presets.Preset) map[string]any {
	var lastUsed any
	if item.LastUsedAt != nil {
		lastUsed = item.LastUsedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"name": item.Name, "issue_key": item.IssueKey, "start_time": item.StartTime, "duration_seconds": item.DurationSeconds,
		"description": item.Description, "created_at": item.CreatedAt.Format(time.RFC3339), "updated_at": item.UpdatedAt.Format(time.RFC3339), "last_used_at": lastUsed,
	}
}

func formatPresetSummary(item presets.Preset) string {
	return fmt.Sprintf("%s · %s · %s · %s", item.IssueKey, item.StartTime, humanDuration(item.DurationSeconds), item.Description)
}
