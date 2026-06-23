package cli

import (
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

func (a *app) newTrashCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "Inspect archived trashed worklogs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(a.newTrashListCommand())
	cmd.AddCommand(a.newTrashSearchCommand())
	cmd.AddCommand(a.newTrashShowCommand())
	return cmd
}

func (a *app) newTrashListCommand() *cobra.Command {
	var issue string
	var issuePrefix string
	var today bool
	var yesterday bool
	var monday bool
	var tuesday bool
	var wednesday bool
	var thursday bool
	var friday bool
	var saturday bool
	var sunday bool
	var currentWeek bool
	var lastWeek bool
	var currentMonth bool
	var lastMonth bool
	var from string
	var to string
	var weekOffset int

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List trashed worklogs",
		Example: "  workledger trash list --today\n  workledger trash list --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			raw := worklogs.ListFilters{
				Issue:         issue,
				IssuePrefix:   issuePrefix,
				Today:         today,
				Yesterday:     yesterday,
				Monday:        monday,
				Tuesday:       tuesday,
				Wednesday:     wednesday,
				Thursday:      thursday,
				Friday:        friday,
				Saturday:      saturday,
				Sunday:        sunday,
				CurrentWeek:   currentWeek,
				LastWeek:      lastWeek,
				CurrentMonth:  currentMonth,
				LastMonth:     lastMonth,
				From:          from,
				To:            to,
				WeekOffset:    weekOffset,
				WeekOffsetSet: cmd.Flags().Changed("week-offset"),
			}
			items, effectiveFilters, err := service.ListTrash(effective, raw)
			if err != nil {
				return a.handleTrashError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTrashListJSON(effective, raw, effectiveFilters, items)
			}
			if err := renderTable(a.stdout, []string{"ID", "SCOPE", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "REASON", "TRASHED"}, trashRows(items, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(items), sumTrashDurationSeconds(items), "trashed worklogs")
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	addDateWindowFlags(cmd, dateWindowFlagValues{
		Today:        &today,
		Yesterday:    &yesterday,
		Monday:       &monday,
		Tuesday:      &tuesday,
		Wednesday:    &wednesday,
		Thursday:     &thursday,
		Friday:       &friday,
		Saturday:     &saturday,
		Sunday:       &sunday,
		CurrentWeek:  &currentWeek,
		LastWeek:     &lastWeek,
		CurrentMonth: &currentMonth,
		LastMonth:    &lastMonth,
		From:         &from,
		To:           &to,
		WeekOffset:   &weekOffset,
	}, filterDateWindowHelp)
	return cmd
}

func (a *app) newTrashSearchCommand() *cobra.Command {
	var issue string
	var issuePrefix string
	var today bool
	var yesterday bool
	var monday bool
	var tuesday bool
	var wednesday bool
	var thursday bool
	var friday bool
	var saturday bool
	var sunday bool
	var currentWeek bool
	var lastWeek bool
	var currentMonth bool
	var lastMonth bool
	var from string
	var to string
	var weekOffset int

	cmd := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search trashed worklogs by description",
		Args:    cobra.ExactArgs(1),
		Example: "  workledger trash search review --today\n  workledger trash search docs --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			raw := worklogs.ListFilters{
				Issue:         issue,
				IssuePrefix:   issuePrefix,
				Today:         today,
				Yesterday:     yesterday,
				Monday:        monday,
				Tuesday:       tuesday,
				Wednesday:     wednesday,
				Thursday:      thursday,
				Friday:        friday,
				Saturday:      saturday,
				Sunday:        sunday,
				CurrentWeek:   currentWeek,
				LastWeek:      lastWeek,
				CurrentMonth:  currentMonth,
				LastMonth:     lastMonth,
				From:          from,
				To:            to,
				WeekOffset:    weekOffset,
				WeekOffsetSet: cmd.Flags().Changed("week-offset"),
			}
			items, effectiveFilters, normalizedQuery, err := service.SearchTrash(effective, worklogs.SearchInput{
				Query:       args[0],
				ListFilters: raw,
			})
			if err != nil {
				return a.handleTrashError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTrashSearchJSON(effective, args[0], raw, effectiveFilters, normalizedQuery, items)
			}
			if err := renderTable(a.stdout, []string{"ID", "SCOPE", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "REASON", "TRASHED"}, trashRows(items, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(items), sumTrashDurationSeconds(items), "trashed worklogs")
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	addDateWindowFlags(cmd, dateWindowFlagValues{
		Today:        &today,
		Yesterday:    &yesterday,
		Monday:       &monday,
		Tuesday:      &tuesday,
		Wednesday:    &wednesday,
		Thursday:     &thursday,
		Friday:       &friday,
		Saturday:     &saturday,
		Sunday:       &sunday,
		CurrentWeek:  &currentWeek,
		LastWeek:     &lastWeek,
		CurrentMonth: &currentMonth,
		LastMonth:    &lastMonth,
		From:         &from,
		To:           &to,
		WeekOffset:   &weekOffset,
	}, filterDateWindowHelp)
	return cmd
}

func (a *app) newTrashShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show <id>",
		Short:   "Show one trashed worklog",
		Args:    cobra.ExactArgs(1),
		Example: "  workledger trash show <id>",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			item, err := service.ShowTrash(args[0])
			if err != nil {
				return a.handleTrashError(mode, effective, err)
			}
			if mode == "json" {
				return a.writeJSON(trashRecordJSON(item, effective.Location))
			}
			return renderTable(a.stdout, []string{"FIELD", "VALUE"}, trashShowRows(item, effective.Location))
		},
	}
	return cmd
}

func (a *app) handleTrashError(mode string, cfg config.EffectiveConfig, err error) error {
	switch {
	case errors.Is(err, worklogs.ErrTrashNotFound):
		return a.fail(mode, 3, "not_found", "trash record not found", nil)
	case errors.Is(err, worklogs.ErrValidation), errors.Is(err, worklogs.ErrConflict):
		var validationErr worklogs.ValidationError
		if errors.As(err, &validationErr) {
			return a.fail(mode, 2, "validation_error", err.Error(), validationErr.Issues)
		}
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	default:
		return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}
}

func (a *app) renderTrashListJSON(cfg config.EffectiveConfig, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, items []worklogs.TrashRecord) error {
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, trashRecordJSON(item, cfg.Location))
	}
	return a.writeJSON(map[string]any{
		"filters": selectorFiltersJSON(raw, effective, cfg.Location),
		"items":   records,
		"total":   len(records),
	})
}

func (a *app) renderTrashSearchJSON(cfg config.EffectiveConfig, rawQuery string, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, normalizedQuery string, items []worklogs.TrashRecord) error {
	filters := selectorFiltersJSON(raw, effective, cfg.Location)
	filters["raw"].(map[string]any)["query"] = rawQuery
	filters["effective"].(map[string]any)["query"] = normalizedQuery
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, trashRecordJSON(item, cfg.Location))
	}
	return a.writeJSON(map[string]any{
		"filters": filters,
		"items":   records,
		"total":   len(records),
	})
}

func trashRecordJSON(item worklogs.TrashRecord, location *time.Location) map[string]any {
	origin := map[string]any{
		"plan_direction":   item.Origin.PlanDirection,
		"plan_id":          nil,
		"plan_item_id":     nil,
		"adapter_family":   nil,
		"adapter_instance": nil,
	}
	if item.Origin.PlanID != nil {
		origin["plan_id"] = *item.Origin.PlanID
	}
	if item.Origin.PlanItemID != nil {
		origin["plan_item_id"] = *item.Origin.PlanItemID
	}
	if item.Origin.AdapterFamily != nil {
		origin["adapter_family"] = *item.Origin.AdapterFamily
	}
	if item.Origin.AdapterInstance != nil {
		origin["adapter_instance"] = *item.Origin.AdapterInstance
	}
	record := map[string]any{
		"id":                item.ID,
		"storage_scope":     item.StorageScope,
		"source_worklog_id": nil,
		"issue_key":         item.IssueKey,
		"started_at":        item.StartedAtUTC.In(location).Format(time.RFC3339),
		"started_at_utc":    item.StartedAtUTC.UTC().Format(time.RFC3339),
		"duration_seconds":  item.DurationSeconds,
		"description":       item.Description,
		"trashed_at":        item.TrashedAt.UTC().Format(time.RFC3339),
		"reason_code":       item.ReasonCode,
		"reason_detail":     item.ReasonDetail,
		"origin":            origin,
	}
	if item.SourceWorklogID != nil {
		record["source_worklog_id"] = *item.SourceWorklogID
	}
	return record
}

func trashRows(items []worklogs.TrashRecord, location *time.Location) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID,
			item.StorageScope,
			item.IssueKey,
			localizedWorklogWindow(item.StartedAtUTC, item.DurationSeconds, location),
			tableDurationMinutes(item.DurationSeconds),
			formatActiveRowValue("description", item.Description, listDescriptionMaxWidth),
			item.ReasonCode,
			item.TrashedAt.UTC().Format(time.RFC3339),
		})
	}
	return rows
}

func trashShowRows(item worklogs.TrashRecord, location *time.Location) [][]string {
	rows := [][]string{
		{"ID", item.ID},
		{"SCOPE", item.StorageScope},
		{"ISSUE", item.IssueKey},
		{"WINDOW", localizedWorklogWindow(item.StartedAtUTC, item.DurationSeconds, location)},
		{"DURATION", tableDurationMinutes(item.DurationSeconds)},
		{"DESCRIPTION", item.Description},
		{"REASON_CODE", item.ReasonCode},
		{"REASON_DETAIL", item.ReasonDetail},
		{"TRASHED_AT", item.TrashedAt.UTC().Format(time.RFC3339)},
		{"PLAN_DIRECTION", item.Origin.PlanDirection},
	}
	if item.SourceWorklogID != nil {
		rows = append(rows, []string{"SOURCE_WORKLOG_ID", *item.SourceWorklogID})
	}
	if item.Origin.PlanID != nil {
		rows = append(rows, []string{"PLAN_ID", *item.Origin.PlanID})
	}
	if item.Origin.PlanItemID != nil {
		rows = append(rows, []string{"PLAN_ITEM_ID", *item.Origin.PlanItemID})
	}
	if item.Origin.AdapterFamily != nil {
		rows = append(rows, []string{"ADAPTER_FAMILY", *item.Origin.AdapterFamily})
	}
	if item.Origin.AdapterInstance != nil {
		rows = append(rows, []string{"ADAPTER_INSTANCE", *item.Origin.AdapterInstance})
	}
	return rows
}

func sumTrashDurationSeconds(items []worklogs.TrashRecord) int {
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	return total
}
