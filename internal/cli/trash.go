package cli

import (
	"errors"
	"fmt"
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
	cmd.AddCommand(a.newTrashRestoreCommand())
	cmd.AddCommand(a.newTrashDeleteCommand())
	cmd.AddCommand(a.newTrashClearCommand())
	return cmd
}

func (a *app) newTrashDeleteCommand() *cobra.Command {
	var issue, issuePrefix, from, to, scope, trashedWithin string
	var today, yesterday, tomorrow, monday, tuesday, wednesday, thursday, friday, saturday, sunday bool
	var currentWeek, lastWeek, currentMonth, lastMonth bool
	var weekOffset int
	var dry, yes bool
	cmd := &cobra.Command{
		Use: "delete [id]", Short: "Permanently delete trashed worklogs", Args: cobra.MaximumNArgs(1),
		Example: "  workledger trash delete <id> --dry\n  workledger trash delete <id> --yes\n  workledger trash delete --issue PROJ-123 --today --dry\n  workledger trash delete --scope local --trashed-within 15m --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			if dry == yes {
				return a.fail(mode, 2, "validation_error", "permanent trash deletion requires exactly one of --dry or --yes", nil)
			}
			weekOffsetSet := cmd.Flags().Changed("week-offset")
			raw := worklogs.TrashDeleteFilters{
				ListFilters: worklogs.ListFilters{
					Issue: issue, IssuePrefix: issuePrefix, Today: today, Yesterday: yesterday, Tomorrow: tomorrow,
					Monday: monday, Tuesday: tuesday, Wednesday: wednesday, Thursday: thursday, Friday: friday,
					Saturday: saturday, Sunday: sunday, CurrentWeek: currentWeek, LastWeek: lastWeek,
					CurrentMonth: currentMonth, LastMonth: lastMonth, From: from, To: to,
					WeekOffset: weekOffset, WeekOffsetSet: weekOffsetSet,
				},
				StorageScope: scope, TrashedWithin: trashedWithin,
			}
			if len(args) == 1 && hasAnyTrashDeleteSelector(raw) {
				return a.fail(mode, 2, "validation_error", "single trash delete cannot be combined with filtered delete selectors", nil)
			}
			cfg, service, cleanup, err := a.loadService(mode, yes, "trash delete")
			if err != nil {
				return err
			}
			defer cleanup()

			var result worklogs.TrashDeleteResult
			var outputFilters *worklogs.TrashDeleteFilters
			if len(args) == 1 {
				result, err = service.DeleteTrash(cmd.Context(), args[0], dry)
			} else {
				result, err = service.DeleteTrashBatch(cmd.Context(), cfg, raw, dry)
				outputFilters = &raw
			}
			if err != nil {
				return a.handleTrashError(mode, cfg, err)
			}
			if mode == "json" {
				return a.renderTrashDeleteJSON(outputFilters, result, cfg.Location)
			}
			return a.renderTrashDeleteTable(result, cfg.Location)
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	cmd.Flags().StringVar(&scope, "scope", "", "Filter by storage scope (local or remote)")
	cmd.Flags().StringVar(&trashedWithin, "trashed-within", "", "Filter by trash age (Go duration)")
	addDateWindowFlags(cmd, dateWindowFlagValues{Today: &today, Yesterday: &yesterday, Tomorrow: &tomorrow, Monday: &monday, Tuesday: &tuesday, Wednesday: &wednesday, Thursday: &thursday, Friday: &friday, Saturday: &saturday, Sunday: &sunday, CurrentWeek: &currentWeek, LastWeek: &lastWeek, CurrentMonth: &currentMonth, LastMonth: &lastMonth, From: &from, To: &to, WeekOffset: &weekOffset}, filterDateWindowHelp)
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview permanent deletion")
	cmd.Flags().BoolVar(&yes, "yes", false, "Permanently delete the matching trash")
	return cmd
}

func (a *app) newTrashClearCommand() *cobra.Command {
	var dry, yes bool
	cmd := &cobra.Command{
		Use: "clear", Short: "Permanently delete all trash", Args: cobra.NoArgs,
		Example: "  workledger trash clear --dry\n  workledger trash clear --yes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if dry == yes {
				return a.fail(mode, 2, "validation_error", "trash clear requires exactly one of --dry or --yes", nil)
			}
			cfg, service, cleanup, err := a.loadService(mode, yes, "trash clear")
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := service.ClearTrash(cmd.Context(), dry)
			if err != nil {
				return a.handleTrashError(mode, cfg, err)
			}
			if mode == "json" {
				return a.renderTrashDeleteJSON(nil, result, cfg.Location)
			}
			return a.renderTrashDeleteTable(result, cfg.Location)
		},
	}
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview clearing all trash")
	cmd.Flags().BoolVar(&yes, "yes", false, "Permanently delete all trash")
	return cmd
}

func hasAnyTrashDeleteSelector(filters worklogs.TrashDeleteFilters) bool {
	return hasAnyTrashRestoreSelector(filters.ListFilters) || filters.StorageScope != "" || filters.TrashedWithin != ""
}

func (a *app) newTrashRestoreCommand() *cobra.Command {
	var issue, issuePrefix, from, to string
	var today, yesterday, tomorrow, monday, tuesday, wednesday, thursday, friday, saturday, sunday bool
	var currentWeek, lastWeek, currentMonth, lastMonth bool
	var weekOffset int
	var dry, yes bool
	cmd := &cobra.Command{
		Use: "restore [id]", Short: "Restore local trashed worklogs", Args: cobra.MaximumNArgs(1),
		Example: "  workledger trash restore <id>\n  workledger trash restore --today --dry\n  workledger trash restore --from 2026-05-14 --to 2026-05-16 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			cfg, service, cleanup, err := a.loadService(mode, true, "trash restore")
			if err != nil {
				return err
			}
			defer cleanup()
			weekOffsetSet := cmd.Flags().Changed("week-offset")
			raw := worklogs.ListFilters{Issue: issue, IssuePrefix: issuePrefix, Today: today, Yesterday: yesterday, Tomorrow: tomorrow, Monday: monday, Tuesday: tuesday, Wednesday: wednesday, Thursday: thursday, Friday: friday, Saturday: saturday, Sunday: sunday, CurrentWeek: currentWeek, LastWeek: lastWeek, CurrentMonth: currentMonth, LastMonth: lastMonth, From: from, To: to, WeekOffset: weekOffset, WeekOffsetSet: weekOffsetSet}
			if len(args) == 1 {
				if dry || yes || hasAnyTrashRestoreSelector(raw) {
					return a.fail(mode, 2, "validation_error", "single restore cannot be combined with batch restore flags", nil)
				}
				item, err := service.RestoreTrash(cmd.Context(), cfg, args[0])
				if err != nil {
					return a.handleTrashError(mode, cfg, err)
				}
				if mode == "json" {
					return a.writeJSON(map[string]any{"trash_id": item.TrashID, "record": worklogRecordJSON(item.Record, cfg.Location)})
				}
				return renderTable(a.stdout, []string{"TRASH ID", "RESTORED ID", "ISSUE", "WINDOW"}, trashRestoreRows([]worklogs.TrashRestoreItem{item}, cfg.Location))
			}
			if dry == yes {
				return a.fail(mode, 2, "validation_error", "filtered batch restore requires exactly one of --dry or --yes", nil)
			}
			result, err := service.RestoreTrashBatch(cmd.Context(), cfg, worklogs.TrashFilters{ListFilters: raw, StorageScope: worklogs.TrashScopeLocal}, dry)
			if err != nil {
				return a.handleTrashError(mode, cfg, err)
			}
			if mode == "json" {
				return a.renderTrashRestoreBatchJSON(raw, result, cfg.Location)
			}
			return renderTable(a.stdout, []string{"TRASH ID", "RESTORED ID", "ISSUE", "WINDOW"}, trashRestoreRows(result.Items, cfg.Location))
		},
	}
	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	addDateWindowFlags(cmd, dateWindowFlagValues{Today: &today, Yesterday: &yesterday, Tomorrow: &tomorrow, Monday: &monday, Tuesday: &tuesday, Wednesday: &wednesday, Thursday: &thursday, Friday: &friday, Saturday: &saturday, Sunday: &sunday, CurrentWeek: &currentWeek, LastWeek: &lastWeek, CurrentMonth: &currentMonth, LastMonth: &lastMonth, From: &from, To: &to, WeekOffset: &weekOffset}, filterDateWindowHelp)
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview the complete restore")
	cmd.Flags().BoolVar(&yes, "yes", false, "Restore the complete matching set")
	return cmd
}

func hasAnyTrashRestoreSelector(filters worklogs.ListFilters) bool {
	return filters.Issue != "" || filters.IssuePrefix != "" || filters.Today || filters.Yesterday || filters.Tomorrow || filters.Monday || filters.Tuesday || filters.Wednesday || filters.Thursday || filters.Friday || filters.Saturday || filters.Sunday || filters.CurrentWeek || filters.LastWeek || filters.CurrentMonth || filters.LastMonth || filters.From != "" || filters.To != "" || filters.WeekOffsetSet
}

func (a *app) renderTrashRestoreBatchJSON(raw worklogs.ListFilters, result worklogs.TrashRestoreResult, location *time.Location) error {
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{"trash_id": item.TrashID, "record": worklogRecordJSON(item.Record, location)})
	}
	filters := selectorFiltersJSON(raw, result.Filters, location)
	addTrashScopeJSON(filters, worklogs.TrashScopeLocal)
	restored := 0
	if !result.DryRun {
		restored = len(items)
	}
	return a.writeJSON(map[string]any{"filters": filters, "dry_run": result.DryRun, "matched_count": len(items), "restored_count": restored, "items": items})
}

func trashRestoreRows(items []worklogs.TrashRestoreItem, location *time.Location) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.TrashID, item.Record.ID, item.Record.IssueKey, localizedWorklogWindow(item.Record.StartedAtUTC, item.Record.DurationSeconds, location)})
	}
	return rows
}

func (a *app) renderTrashDeleteJSON(raw *worklogs.TrashDeleteFilters, result worklogs.TrashDeleteResult, location *time.Location) error {
	items := make([]map[string]any, 0, len(result.Items))
	if result.DryRun {
		for _, item := range result.Items {
			items = append(items, trashRecordJSON(item, location))
		}
	} else {
		for _, id := range result.DeletedIDs {
			items = append(items, map[string]any{"id": id})
		}
	}
	payload := map[string]any{
		"dry_run":       result.DryRun,
		"matched_count": len(result.Items),
		"deleted_count": len(result.DeletedIDs),
		"scope_counts":  trashScopeCounts(result.Items),
		"items":         items,
	}
	if raw != nil {
		filters := selectorFiltersJSON(raw.ListFilters, result.Filters.EffectiveFilters, location)
		addTrashScopeJSON(filters, raw.StorageScope)
		filters["raw"].(map[string]any)["trashed_within"] = emptyToNil(raw.TrashedWithin)
		if result.Filters.TrashedFrom != nil {
			filters["effective"].(map[string]any)["trashed_from"] = result.Filters.TrashedFrom.UTC().Format(time.RFC3339)
		}
		if result.Filters.TrashedTo != nil {
			filters["effective"].(map[string]any)["trashed_to"] = result.Filters.TrashedTo.UTC().Format(time.RFC3339)
		}
		payload["filters"] = filters
	}
	return a.writeJSON(payload)
}

func (a *app) renderTrashDeleteTable(result worklogs.TrashDeleteResult, location *time.Location) error {
	if result.DryRun {
		if err := renderTable(a.stdout, []string{"ID", "SCOPE", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "REASON", "TRASHED"}, trashRows(result.Items, location)); err != nil {
			return err
		}
		counts := trashScopeCounts(result.Items)
		_, err := fmt.Fprintf(a.stdout, "\nMatched: %d trash records (%d local, %d remote)\n", len(result.Items), counts[worklogs.TrashScopeLocal], counts[worklogs.TrashScopeRemote])
		return err
	}
	rows := make([][]string, 0, len(result.DeletedIDs))
	for _, id := range result.DeletedIDs {
		rows = append(rows, []string{id})
	}
	if err := renderTable(a.stdout, []string{"ID"}, rows); err != nil {
		return err
	}
	counts := trashScopeCounts(result.Items)
	_, err := fmt.Fprintf(a.stdout, "\nDeleted: %d trash records (%d local, %d remote)\n", len(result.DeletedIDs), counts[worklogs.TrashScopeLocal], counts[worklogs.TrashScopeRemote])
	return err
}

func trashScopeCounts(items []worklogs.TrashRecord) map[string]int {
	counts := map[string]int{worklogs.TrashScopeLocal: 0, worklogs.TrashScopeRemote: 0}
	for _, item := range items {
		counts[item.StorageScope]++
	}
	return counts
}

func (a *app) newTrashListCommand() *cobra.Command {
	var issue string
	var issuePrefix string
	var today bool
	var yesterday bool
	var tomorrow bool
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
	var scope string

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
				Tomorrow:      tomorrow,
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
			items, effectiveFilters, err := service.ListTrash(effective, worklogs.TrashFilters{ListFilters: raw, StorageScope: scope})
			if err != nil {
				return a.handleTrashError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTrashListJSON(effective, raw, scope, effectiveFilters, items)
			}
			if err := renderTable(a.stdout, []string{"ID", "SCOPE", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "REASON", "TRASHED"}, trashRows(items, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(items), sumTrashDurationSeconds(items), "trashed worklogs")
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	cmd.Flags().StringVar(&scope, "scope", "", "Filter by storage scope (local or remote)")
	addDateWindowFlags(cmd, dateWindowFlagValues{
		Today:        &today,
		Yesterday:    &yesterday,
		Tomorrow:     &tomorrow,
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
	var tomorrow bool
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
	var scope string

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
				Tomorrow:      tomorrow,
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
			items, effectiveFilters, normalizedQuery, err := service.SearchTrash(effective, worklogs.TrashSearchInput{
				Query:        args[0],
				TrashFilters: worklogs.TrashFilters{ListFilters: raw, StorageScope: scope},
			})
			if err != nil {
				return a.handleTrashError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTrashSearchJSON(effective, args[0], raw, scope, effectiveFilters, normalizedQuery, items)
			}
			if err := renderTable(a.stdout, []string{"ID", "SCOPE", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "REASON", "TRASHED"}, trashRows(items, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(items), sumTrashDurationSeconds(items), "trashed worklogs")
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Filter by issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Filter by issue prefix")
	cmd.Flags().StringVar(&scope, "scope", "", "Filter by storage scope (local or remote)")
	addDateWindowFlags(cmd, dateWindowFlagValues{
		Today:        &today,
		Yesterday:    &yesterday,
		Tomorrow:     &tomorrow,
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
			details := any(validationErr.Issues)
			if validationErr.Conflict != nil {
				details = validationErr.Conflict
			}
			return a.fail(mode, 2, "validation_error", err.Error(), details)
		}
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	default:
		return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}
}

func (a *app) renderTrashListJSON(cfg config.EffectiveConfig, raw worklogs.ListFilters, scope string, effective worklogs.EffectiveFilters, items []worklogs.TrashRecord) error {
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, trashRecordJSON(item, cfg.Location))
	}
	filters := selectorFiltersJSON(raw, effective, cfg.Location)
	addTrashScopeJSON(filters, scope)
	return a.writeJSON(map[string]any{
		"filters": filters,
		"items":   records,
		"total":   len(records),
	})
}

func (a *app) renderTrashSearchJSON(cfg config.EffectiveConfig, rawQuery string, raw worklogs.ListFilters, scope string, effective worklogs.EffectiveFilters, normalizedQuery string, items []worklogs.TrashRecord) error {
	filters := selectorFiltersJSON(raw, effective, cfg.Location)
	addTrashScopeJSON(filters, scope)
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

func addTrashScopeJSON(filters map[string]any, scope string) {
	value := any(nil)
	if scope != "" {
		value = scope
	}
	filters["raw"].(map[string]any)["scope"] = value
	filters["effective"].(map[string]any)["scope"] = value
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
		"source_created_at": nil,
		"source_updated_at": nil,
		"source_revision":   nil,
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
	if item.SourceCreatedAt != nil {
		record["source_created_at"] = item.SourceCreatedAt.UTC().Format(time.RFC3339)
	}
	if item.SourceUpdatedAt != nil {
		record["source_updated_at"] = item.SourceUpdatedAt.UTC().Format(time.RFC3339)
	}
	if item.SourceRevision != nil {
		record["source_revision"] = *item.SourceRevision
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
	if item.SourceCreatedAt != nil {
		rows = append(rows, []string{"SOURCE_CREATED_AT", item.SourceCreatedAt.UTC().Format(time.RFC3339)})
	}
	if item.SourceUpdatedAt != nil {
		rows = append(rows, []string{"SOURCE_UPDATED_AT", item.SourceUpdatedAt.UTC().Format(time.RFC3339)})
	}
	if item.SourceRevision != nil {
		rows = append(rows, []string{"SOURCE_REVISION", fmt.Sprint(*item.SourceRevision)})
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
