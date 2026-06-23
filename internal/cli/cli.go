package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradcadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile"
	reconcilemodel "github.com/solitus0/workledger/internal/reconcile/model"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/totals"
	"github.com/solitus0/workledger/internal/worklogs"
)

const listDescriptionMaxWidth = 80

var Version = "dev"

const (
	dateSelectorHelp       = "Date selector: YYYY-MM-DD, today, yesterday, tomorrow, +Nd, or -Nd"
	fromDateHelp           = "From " + dateSelectorHelp + ", e.g. 2026-05-14 or -7d"
	toDateHelp             = "To " + dateSelectorHelp + ", e.g. 2026-05-14 or today"
	localTimestampHelp     = "Local started timestamp: YYYY-MM-DDTHH:MM, todayTHH:MM, yesterdayTHH:MM, tomorrowTHH:MM, +NdTHH:MM, or -NdTHH:MM"
	utcTimestampHelp       = "UTC started timestamp in RFC3339, e.g. 2026-05-14T09:00:00Z"
	clockHelp              = "Clock time in HH:MM, e.g. 09:00"
	lunchWindowHelp        = "Lunch exclusion window in HH:MM-HH:MM, e.g. 12:00-13:00"
	worklogsAddExample     = "  workledger worklogs add --issue PROJ-123 --started todayT09:00 --duration 2h --description \"Implement reconciliation\"\n  workledger worklogs add --issue PROJ-123 --started-utc 2026-05-14T09:00:00Z --duration 2h --description \"Implement reconciliation\"\n  workledger worklogs add --issue PROJ-123 --snap --today --duration 2h --description \"Implement reconciliation\""
	worklogsUpdateExample  = "  workledger worklogs update <id> --started 2026-05-14T09:00 --duration 1h30m\n  workledger worklogs update <id> --started-utc 2026-05-14T09:00:00Z"
	worklogsContextExample = "  workledger worklogs context --today --day-start 09:00 --day-end 17:30\n  workledger worklogs context --from 2026-05-14 --to 2026-05-14 --lunch 12:00-13:00"
	worklogsApplyExample   = "  workledger worklogs apply --file payload.json\n  workledger worklogs apply --stdin\n\nPayload timestamps:\n  started_at uses the same local timestamp grammar as --started\n  started_at_utc uses RFC3339 UTC, e.g. 2026-05-14T09:00:00Z"
	totalsExample          = "  workledger totals --today\n  workledger totals --from 2026-05-14 --to 2026-05-16 --adapter clockify"
	planReconcileExample   = "  workledger plan reconcile --push --adapter clockify --today\n  workledger plan reconcile --pull --adapter jira-cloud --from 2026-05-14 --to 2026-05-16"
	planListExample        = "  workledger plan list --today\n  workledger plan list --from 2026-05-14 --to 2026-05-16"
	weekOffsetHelp         = "Shift weekday filters by N weeks; only valid with --mon..--sun"
)

const groupedDateFlagUsageTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}{{end}}

Usage:
  {{if .Runnable}}{{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}{{.CommandPath}} [command]{{end}}

{{if .Example}}Examples:
{{.Example}}

{{end}}{{if otherFlagUsages .}}Flags:
{{otherFlagUsages .}}
{{end}}{{if dateFilterFlagUsages .}}Date filters:
{{dateFilterFlagUsages .}}
{{end}}{{if weekdayFilterFlagUsages .}}Weekday filters:
{{weekdayFilterFlagUsages .}}
{{end}}{{if dateModifierFlagUsages .}}Date filter modifiers:
{{dateModifierFlagUsages .}}
{{end}}{{if .HasInheritedFlags}}Global Flags:
{{.InheritedFlags.FlagUsagesWrapped 80}}{{end}}`

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}

type app struct {
	stdout io.Writer
	stderr io.Writer
}

type dateWindowFlagValues struct {
	Today        *bool
	Yesterday    *bool
	Monday       *bool
	Tuesday      *bool
	Wednesday    *bool
	Thursday     *bool
	Friday       *bool
	Saturday     *bool
	Sunday       *bool
	CurrentWeek  *bool
	LastWeek     *bool
	CurrentMonth *bool
	LastMonth    *bool
	From         *string
	To           *string
	WeekOffset   *int
}

type dateWindowHelpSet struct {
	Today           string
	Yesterday       string
	WeekdayTemplate string
	CurrentWeek     string
	LastWeek        string
	CurrentMonth    string
	LastMonth       string
}

var (
	dateFilterFlagNames    = []string{"today", "yesterday", "current-week", "last-week", "current-month", "last-month", "from", "to"}
	weekdayFilterFlagNames = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	dateModifierFlagNames  = []string{"week-offset"}

	filterDateWindowHelp = dateWindowHelpSet{
		Today:           "Filter to today",
		Yesterday:       "Filter to yesterday",
		WeekdayTemplate: "Filter to %s in the selected week",
		CurrentWeek:     "Filter to the current week",
		LastWeek:        "Filter to the previous week",
		CurrentMonth:    "Filter to the current month",
		LastMonth:       "Filter to the previous month",
	}
	useDateWindowHelp = dateWindowHelpSet{
		Today:           "Use today",
		Yesterday:       "Use yesterday",
		WeekdayTemplate: "Use %s in the selected week",
		CurrentWeek:     "Use the current week",
		LastWeek:        "Use the previous week",
		CurrentMonth:    "Use the current month",
		LastMonth:       "Use the previous month",
	}
	addDateWindowHelp = dateWindowHelpSet{
		Today:           "Use the current local day",
		Yesterday:       "Use the previous local day",
		WeekdayTemplate: "Use %s in the selected week",
		CurrentWeek:     "Use the current local week",
		LastWeek:        "Use the previous local week",
		CurrentMonth:    "Use the current local month",
		LastMonth:       "Use the previous local month",
	}
)

func init() {
	cobra.AddTemplateFunc("otherFlagUsages", otherFlagUsages)
	cobra.AddTemplateFunc("dateFilterFlagUsages", func(cmd *cobra.Command) string {
		return namedFlagUsages(cmd, dateFilterFlagNames)
	})
	cobra.AddTemplateFunc("weekdayFilterFlagUsages", func(cmd *cobra.Command) string {
		return namedFlagUsages(cmd, weekdayFilterFlagNames)
	})
	cobra.AddTemplateFunc("dateModifierFlagUsages", func(cmd *cobra.Command) string {
		return namedFlagUsages(cmd, dateModifierFlagNames)
	})
}

func applyGroupedDateFlagUsage(cmd *cobra.Command) {
	cmd.LocalFlags().SortFlags = false
	cmd.SetUsageTemplate(groupedDateFlagUsageTemplate)
}

func addDateWindowFlags(cmd *cobra.Command, values dateWindowFlagValues, help dateWindowHelpSet) {
	cmd.Flags().BoolVar(values.Today, "today", false, help.Today)
	cmd.Flags().BoolVar(values.Yesterday, "yesterday", false, help.Yesterday)
	cmd.Flags().BoolVar(values.Monday, "mon", false, fmt.Sprintf(help.WeekdayTemplate, "Monday"))
	cmd.Flags().BoolVar(values.Tuesday, "tue", false, fmt.Sprintf(help.WeekdayTemplate, "Tuesday"))
	cmd.Flags().BoolVar(values.Wednesday, "wed", false, fmt.Sprintf(help.WeekdayTemplate, "Wednesday"))
	cmd.Flags().BoolVar(values.Thursday, "thu", false, fmt.Sprintf(help.WeekdayTemplate, "Thursday"))
	cmd.Flags().BoolVar(values.Friday, "fri", false, fmt.Sprintf(help.WeekdayTemplate, "Friday"))
	cmd.Flags().BoolVar(values.Saturday, "sat", false, fmt.Sprintf(help.WeekdayTemplate, "Saturday"))
	cmd.Flags().BoolVar(values.Sunday, "sun", false, fmt.Sprintf(help.WeekdayTemplate, "Sunday"))
	cmd.Flags().BoolVar(values.CurrentWeek, "current-week", false, help.CurrentWeek)
	cmd.Flags().BoolVar(values.LastWeek, "last-week", false, help.LastWeek)
	cmd.Flags().BoolVar(values.CurrentMonth, "current-month", false, help.CurrentMonth)
	cmd.Flags().BoolVar(values.LastMonth, "last-month", false, help.LastMonth)
	cmd.Flags().StringVar(values.From, "from", "", fromDateHelp)
	cmd.Flags().StringVar(values.To, "to", "", toDateHelp)
	cmd.Flags().IntVar(values.WeekOffset, "week-offset", 0, weekOffsetHelp)
	applyGroupedDateFlagUsage(cmd)
}

func otherFlagUsages(cmd *cobra.Command) string {
	excluded := map[string]struct{}{}
	for _, name := range dateFilterFlagNames {
		excluded[name] = struct{}{}
	}
	for _, name := range weekdayFilterFlagNames {
		excluded[name] = struct{}{}
	}
	for _, name := range dateModifierFlagNames {
		excluded[name] = struct{}{}
	}

	flags := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	flags.SortFlags = false
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if _, skip := excluded[flag.Name]; skip {
			return
		}
		flags.AddFlag(flag)
	})
	return trimmedUsages(flags)
}

func namedFlagUsages(cmd *cobra.Command, names []string) string {
	flags := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	flags.SortFlags = false
	for _, name := range names {
		flag := cmd.LocalFlags().Lookup(name)
		if flag == nil {
			continue
		}
		flags.AddFlag(flag)
	}
	return trimmedUsages(flags)
}

func trimmedUsages(flags *pflag.FlagSet) string {
	usage := strings.TrimRight(flags.FlagUsagesWrapped(80), "\n")
	if usage == "" {
		return ""
	}
	return usage + "\n"
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr}
	cmd := a.newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	if err := cmd.ExecuteContext(ctx); err != nil {
		var exitErr exitError
		if errors.As(err, &exitErr) {
			return exitErr.code
		}

		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	return 0
}

func (a *app) newRootCommand() *cobra.Command {
	var showVersion bool

	root := &cobra.Command{
		Use:           "workledger",
		Short:         "Manage canonical local worklogs",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return a.runVersion(cmd)
			}
			return cmd.Help()
		},
	}

	root.PersistentFlags().String("output", "", "Output mode: table or json")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version")
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddCommand(a.newVersionCommand())
	root.AddCommand(a.newInitCommand())
	root.AddCommand(a.newConfigCommand())
	root.AddCommand(a.newSetupCommand())
	root.AddCommand(a.newWorklogsCommand())
	root.AddCommand(a.newTombstonesCommand())
	root.AddCommand(a.newTrashCommand())
	root.AddCommand(a.newTotalsCommand())
	root.AddCommand(a.newStatusCommand())
	root.AddCommand(a.newDoctorCommand())
	root.AddCommand(a.newRoutingCommand())
	root.AddCommand(a.newRouteCommand())
	root.AddCommand(a.newClockifyCommand())
	root.AddCommand(a.newIssueMetadataCommand())
	root.AddCommand(a.newPlanCommand())

	return root
}

func (a *app) newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runVersion(cmd)
		},
	}
}

func (a *app) runVersion(cmd *cobra.Command) error {
	mode := outputMode(cmd)
	if mode == "json" {
		return a.writeJSON(map[string]string{"version": Version})
	}
	_, _ = fmt.Fprintln(a.stdout, Version)
	return nil
}

func (a *app) newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Bootstrap local config and SQLite storage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			configPath, err := config.ConfigPath()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", "resolve config path", nil)
			}

			if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if err := config.TightenDir(filepath.Dir(configPath)); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			configStatus := "reused"
			if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
				clockifyCfg, err := a.bootstrapClockifyConfig(cmd.Context())
				if err != nil {
					return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
				}
				if err := os.WriteFile(configPath, config.DefaultConfigBytes(clockifyCfg), 0o600); err != nil {
					return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
				}
				configStatus = "created"
			}
			if err := config.TightenFile(configPath); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			effective, validationIssues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(validationIssues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", validationIssues)
			}

			if err := os.MkdirAll(filepath.Dir(effective.SQLitePath), 0o700); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if err := config.TightenDir(filepath.Dir(effective.SQLitePath)); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			store, sqliteStatus, err := sqlitestore.Bootstrap(effective.SQLitePath)
			if err != nil {
				var bootstrapErr *sqlitestore.BootstrapError
				if errors.As(err, &bootstrapErr) {
					return a.failUnrecoverableSQLite(mode, bootstrapErr.Path)
				}
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			defer store.Close()

			if err := config.TightenFile(effective.SQLitePath); err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			payload := map[string]any{
				"config":      configStatus,
				"sqlite":      string(sqliteStatus),
				"config_path": effective.ConfigPath,
				"sqlite_path": effective.SQLitePath,
			}
			if mode == "json" {
				return a.writeJSON(payload)
			}

			_, _ = fmt.Fprintf(
				a.stdout,
				"Local worklogs are ready.\n\nConfig:\n  Status: %s\n  Path: %s\n\nClockify:\n  Status: %s\n\nDatabase:\n  Path: %s\n\nNext:\n  workledger worklogs add\n\nOptional adapter setup:\n  workledger setup jira-cloud --instance <name>\n  workledger setup jira-data-center --instance <name>\n  workledger setup clockify\n\nValidate anytime:\n  workledger config validate\n",
				initConfigStatusLabel(configStatus),
				effective.ConfigPath,
				initClockifyStatusLabel(configStatus, effective.File.Clockify),
				effective.SQLitePath,
			)
			return nil
		},
	}
}

func initConfigStatusLabel(status string) string {
	switch status {
	case "created":
		return "created new config"
	case "reused":
		return "reused existing valid config"
	default:
		return status
	}
}

func initClockifyStatusLabel(configStatus string, clockifyCfg *config.ClockifyConfig) string {
	if configStatus == "reused" {
		return "kept existing config"
	}
	if clockifyCfg != nil && strings.TrimSpace(clockifyCfg.Auth.APIKeyEnv) == "CLOCKIFY_API_KEY" {
		return "auto-configured from discovered CLOCKIFY_API_KEY"
	}
	return "not auto-configured from CLOCKIFY_API_KEY"
}

func (a *app) newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Config commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, issues, err := config.ValidateExisting()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if len(issues) > 0 {
				return a.fail(mode, 2, "validation_error", "config validation failed", issues)
			}

			payload := map[string]any{
				"valid":       true,
				"config_path": effective.ConfigPath,
				"effective": map[string]any{
					"default_output": effective.DefaultOutput,
					"storage": map[string]any{
						"sqlite_path": effective.SQLitePath,
					},
					"worklogs": map[string]any{
						"minimum_duration_seconds":    effective.MinimumDurationSeconds,
						"daily_minimum_quota_seconds": effective.DailyMinimumQuotaSeconds,
						"day_start":                   effective.DayStart,
						"day_end":                     effective.DayEnd,
						"daily_lunch":                 effective.DailyLunch,
					},
				},
			}
			if effective.LocalTimezoneConfig != nil {
				payload["effective"].(map[string]any)["local_timezone"] = *effective.LocalTimezoneConfig
			}

			if mode == "json" {
				return a.writeJSON(payload)
			}
			_, _ = fmt.Fprintln(a.stdout, "config is valid")
			return nil
		},
	})

	cmd.AddCommand(a.newConfigEnvCommand())
	cmd.AddCommand(a.newConfigSummaryCommand())

	return cmd
}

func (a *app) newWorklogsCommand() *cobra.Command {
	worklogsCmd := &cobra.Command{
		Use:   "worklogs",
		Short: "Manage local worklogs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	worklogsCmd.AddCommand(a.newWorklogsListCommand())
	worklogsCmd.AddCommand(a.newWorklogsSearchCommand())
	worklogsCmd.AddCommand(a.newWorklogsContextCommand())
	worklogsCmd.AddCommand(a.newWorklogsShiftCommand())
	worklogsCmd.AddCommand(a.newWorklogsApplyCommand())
	worklogsCmd.AddCommand(a.newWorklogsAddCommand())
	worklogsCmd.AddCommand(a.newWorklogsUpdateCommand())
	worklogsCmd.AddCommand(a.newWorklogsDeleteCommand())

	return worklogsCmd
}

func (a *app) newWorklogsListCommand() *cobra.Command {
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
	var fields string

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List local worklogs",
		Example: "  workledger worklogs list --today\n  workledger worklogs list --from 2026-05-14 --to 2026-05-16\n  workledger worklogs list --mon --week-offset -1",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			fieldList := splitFields(fields)
			weekOffsetSet := cmd.Flags().Changed("week-offset")
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
				WeekOffsetSet: weekOffsetSet,
				Fields:        fieldList,
			}
			active, _, effectiveFilters, err := service.List(effective, raw)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderListJSON(effective, raw, effectiveFilters, active, nil)
			}

			columns := []string{"id", "issue_key", "started_at", "duration_seconds", "description"}
			if len(effectiveFilters.Fields) > 0 {
				columns = effectiveFilters.Fields
			}
			if err := renderTable(a.stdout, tableHeaders(columns), activeRows(active, effective.Location, columns, listDescriptionMaxWidth)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(active), sumActiveDurationSeconds(active), "worklogs")
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
	cmd.Flags().StringVar(&fields, "fields", "", "Comma-separated field list")
	return cmd
}

func (a *app) newWorklogsSearchCommand() *cobra.Command {
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
	var fields string

	cmd := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search local worklogs by description",
		Args:    cobra.ExactArgs(1),
		Example: "  workledger worklogs search review --today\n  workledger worklogs search docs --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			fieldList := splitFields(fields)
			weekOffsetSet := cmd.Flags().Changed("week-offset")
			rawFilters := worklogs.ListFilters{
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
				WeekOffsetSet: weekOffsetSet,
				Fields:        fieldList,
			}
			active, _, effectiveFilters, normalizedQuery, err := service.Search(effective, worklogs.SearchInput{
				Query:       args[0],
				ListFilters: rawFilters,
			})
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderSearchJSON(effective, args[0], rawFilters, effectiveFilters, normalizedQuery, active, nil)
			}

			columns := []string{"id", "issue_key", "started_at", "duration_seconds", "description"}
			if len(effectiveFilters.Fields) > 0 {
				columns = effectiveFilters.Fields
			}
			if err := renderTable(a.stdout, tableHeaders(columns), activeRows(active, effective.Location, columns, listDescriptionMaxWidth)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(active), sumActiveDurationSeconds(active), "worklogs")
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
	cmd.Flags().StringVar(&fields, "fields", "", "Comma-separated field list")
	return cmd
}

func (a *app) newWorklogsContextCommand() *cobra.Command {
	var issues []string
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
	var dayStart string
	var dayEnd string
	var lunch string
	var noLunch bool

	cmd := &cobra.Command{
		Use:     "context",
		Short:   "Inspect planning context for local worklogs",
		Example: worklogsContextExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			result, err := service.Context(effective, worklogs.ContextInput{
				Issues:        issues,
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
				WeekOffsetSet: weekOffsetSet,
				DayStart:      dayStart,
				DayEnd:        dayEnd,
				Lunch:         lunch,
				NoLunch:       noLunch,
			})
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderContextJSON(worklogs.ContextInput{
					Issues:        issues,
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
					WeekOffsetSet: weekOffsetSet,
					DayStart:      dayStart,
					DayEnd:        dayEnd,
					Lunch:         lunch,
					NoLunch:       noLunch,
				}, result, effective.Location)
			}

			return renderTable(a.stdout, []string{"DATE", "WORKLOGS", "BOOKED", "UNTIL_QUOTA", "COLLISIONS"}, contextRows(result))
		},
	}

	cmd.Flags().StringArrayVar(&issues, "issue", nil, "Planning issue key")
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
	cmd.Flags().StringVar(&dayStart, "day-start", "", "Workday start "+clockHelp)
	cmd.Flags().StringVar(&dayEnd, "day-end", "", "Workday end "+clockHelp)
	cmd.Flags().StringVar(&lunch, "lunch", "", lunchWindowHelp)
	cmd.Flags().BoolVar(&noLunch, "no-lunch", false, "Disable lunch exclusion")
	return cmd
}

func (a *app) newWorklogsShiftCommand() *cobra.Command {
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
	var by string
	var dry bool

	cmd := &cobra.Command{
		Use:     "shift",
		Short:   "Shift local worklog timestamps",
		Example: "  workledger worklogs shift --issue PROJ-123 --today --by 15m\n  workledger worklogs shift --from 2026-05-14 --to 2026-05-16 --by -30m --dry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, !dry, "worklogs shift")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			result, err := service.Shift(effective, worklogs.ListFilters{
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
				WeekOffsetSet: weekOffsetSet,
			}, by, dry)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderShiftJSON(worklogs.ListFilters{
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
					WeekOffsetSet: weekOffsetSet,
				}, result, effective.Location)
			}

			if dry {
				return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW_BEFORE", "WINDOW_AFTER", "DURATION", "DESCRIPTION"}, shiftPreviewRows(result.PreviewItems, effective.Location))
			}

			return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Items, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
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
	cmd.Flags().StringVar(&by, "by", "", "Signed Go duration, e.g. 15m, 1h30m, -45m")
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview shifted worklogs")
	return cmd
}

func (a *app) newWorklogsApplyCommand() *cobra.Command {
	var filePath string
	var stdin bool
	var dry bool
	var force bool

	cmd := &cobra.Command{
		Use:     "apply",
		Short:   "Apply raw batch worklog additions",
		Example: worklogsApplyExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, !dry, "worklogs apply")
			if err != nil {
				return err
			}
			defer cleanup()

			if (filePath == "" && !stdin) || (filePath != "" && stdin) {
				return a.fail(mode, 2, "validation_error", "apply requires exactly one of file or stdin", nil)
			}

			var data []byte
			if stdin {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(filePath)
			}
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			payload, err := worklogs.ParseRawApplyPayload(data)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			result, err := service.Apply(effective, payload, force, dry)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderApplyJSON(result, effective.Location)
			}

			return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to raw apply payload JSON")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read raw apply payload JSON from stdin")
	cmd.Flags().BoolVar(&dry, "dry", false, "Validate without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass duplicate and overlap validation")
	return cmd
}

func (a *app) newWorklogsAddCommand() *cobra.Command {
	var issue string
	var started string
	var startedUTC string
	var snap bool
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
	var dayStart string
	var dayEnd string
	var lunch string
	var noLunch bool
	var duration string
	var description string
	var dry bool
	var force bool

	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Add a local worklog",
		Example: worklogsAddExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, !dry, "worklogs add")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			input := worklogs.AddInput{
				IssueKey:      issue,
				Started:       started,
				StartedUTC:    startedUTC,
				Snap:          snap,
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
				WeekOffsetSet: weekOffsetSet,
				DayStart:      dayStart,
				DayEnd:        dayEnd,
				Lunch:         lunch,
				NoLunch:       noLunch,
				Duration:      duration,
				Description:   description,
				Force:         force,
			}

			var result worklogs.AddResult
			if dry {
				result, err = service.PreviewAdd(effective, input)
			} else {
				result, err = service.Add(effective, input)
			}
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if snap {
				if mode == "json" {
					payload := map[string]any{
						"records": worklogRecordsJSON(result.Records, effective.Location, dry),
					}
					if dry {
						payload["dry_run"] = true
					}
					if len(result.Warnings) > 0 {
						payload["warnings"] = result.Warnings
					}
					return a.writeJSON(payload)
				}
				writeAddWarnings(a.stderr, result.Warnings)
				if dry {
					return renderTable(a.stdout, []string{"ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, effective.Location, []string{"issue_key", "started_at", "duration_seconds", "description"}, 0))
				}
				return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Records, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
			}

			record := result.Records[0]
			if mode == "json" {
				if dry {
					return a.writeJSON(map[string]any{
						"dry_run": true,
						"record":  worklogPreviewJSON(record, effective.Location),
					})
				}
				return a.writeJSON(worklogRecordJSON(record, effective.Location))
			}
			if dry {
				return renderTable(a.stdout, []string{"ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows([]worklogs.LocalWorklog{record}, effective.Location, []string{"issue_key", "started_at", "duration_seconds", "description"}, 0))
			}
			return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows([]worklogs.LocalWorklog{record}, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&started, "started", "", localTimestampHelp)
	cmd.Flags().StringVar(&startedUTC, "started-utc", "", utcTimestampHelp)
	cmd.Flags().BoolVar(&snap, "snap", false, "Snap to the earliest fitting time in the selected local date window")
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
	}, addDateWindowHelp)
	cmd.Flags().StringVar(&dayStart, "day-start", "", clockHelp)
	cmd.Flags().StringVar(&dayEnd, "day-end", "", clockHelp)
	cmd.Flags().StringVar(&lunch, "lunch", "", lunchWindowHelp)
	cmd.Flags().BoolVar(&noLunch, "no-lunch", false, "Disable lunch exclusion for snap placement")
	cmd.Flags().StringVar(&duration, "duration", "", "Worklog duration")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().BoolVar(&dry, "dry", false, "Validate without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass duplicate and overlap validation")
	return cmd
}

func (a *app) newWorklogsUpdateCommand() *cobra.Command {
	var issue string
	var started string
	var startedUTC string
	var duration string
	var description string
	var force bool

	cmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update a local worklog",
		Args:    cobra.ExactArgs(1),
		Example: worklogsUpdateExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, true, "worklogs update")
			if err != nil {
				return err
			}
			defer cleanup()

			patch := worklogs.PatchInput{Force: force}
			if cmd.Flags().Changed("issue") {
				patch.IssueKey = &issue
			}
			if cmd.Flags().Changed("started") {
				patch.Started = &started
			}
			if cmd.Flags().Changed("started-utc") {
				patch.StartedUTC = &startedUTC
			}
			if cmd.Flags().Changed("duration") {
				patch.Duration = &duration
			}
			if cmd.Flags().Changed("description") {
				patch.Description = &description
			}

			record, err := service.Update(effective, args[0], patch)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.writeJSON(worklogRecordJSON(record, effective.Location))
			}
			return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows([]worklogs.LocalWorklog{record}, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&started, "started", "", localTimestampHelp)
	cmd.Flags().StringVar(&startedUTC, "started-utc", "", utcTimestampHelp)
	cmd.Flags().StringVar(&duration, "duration", "", "Worklog duration")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass duplicate and overlap validation")
	return cmd
}

func (a *app) newWorklogsDeleteCommand() *cobra.Command {
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
	var dry bool
	var yes bool
	var hardDelete bool

	cmd := &cobra.Command{
		Use:     "delete [id]",
		Short:   "Delete local worklogs",
		Args:    cobra.MaximumNArgs(1),
		Example: "  workledger worklogs delete <id>\n  workledger worklogs delete --from 2026-05-14 --to 2026-05-16 --dry\n  workledger worklogs delete --today --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, true, "worklogs delete")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			if len(args) == 1 {
				if dry || yes || issue != "" || issuePrefix != "" || today || yesterday || monday || tuesday || wednesday || thursday || friday || saturday || sunday || currentWeek || lastWeek || currentMonth || lastMonth || from != "" || to != "" || weekOffsetSet {
					return a.fail(mode, 2, "validation_error", "single delete cannot be combined with batch delete flags", nil)
				}
				record, err := service.Delete(args[0], hardDelete)
				if err != nil {
					return a.handleWorklogError(mode, effective, err)
				}
				if mode == "json" {
					return a.writeJSON(map[string]any{
						"id":          record.ID,
						"issue_key":   record.IssueKey,
						"deleted_at":  record.DeletedAt.Format(time.RFC3339),
						"hard_delete": record.HardDelete,
					})
				}
				return renderTable(a.stdout, []string{"ID", "ISSUE", "DELETED", "HARD"}, deleteResultRows([]worklogs.DeleteResult{record}))
			}

			if dry && yes {
				return a.fail(mode, 2, "validation_error", "--dry and --yes are mutually exclusive", nil)
			}
			if !dry && !yes {
				return a.fail(mode, 2, "validation_error", "filtered batch delete requires --yes or --dry", nil)
			}

			result, err := service.DeleteBatch(effective, worklogs.ListFilters{
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
				WeekOffsetSet: weekOffsetSet,
			}, dry, hardDelete)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderDeleteBatchJSON(worklogs.ListFilters{
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
					WeekOffsetSet: weekOffsetSet,
				}, result, effective.Location)
			}

			if dry {
				return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION"}, activeRows(result.Items, effective.Location, []string{"id", "issue_key", "started_at", "duration_seconds", "description"}, 0))
			}
			rows := make([][]string, 0, len(result.Deleted))
			for _, id := range result.Deleted {
				rows = append(rows, []string{id, strconv.FormatBool(result.HardDelete)})
			}
			return renderTable(a.stdout, []string{"ID", "HARD"}, rows)
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Issue prefix")
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
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview matching deletes")
	cmd.Flags().BoolVar(&yes, "yes", false, "Execute filtered batch delete")
	cmd.Flags().BoolVar(&hardDelete, "hard", false, "Delete without creating tombstones")
	return cmd
}

func (a *app) newWorklogsRestoreCommand() *cobra.Command {
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
	var dry bool
	var yes bool
	var force bool

	cmd := &cobra.Command{
		Use:     "restore",
		Short:   "Restore deleted local worklogs",
		Args:    cobra.NoArgs,
		Example: "  workledger worklogs restore --from 2026-05-14 --to 2026-05-16 --dry\n  workledger worklogs restore --today --yes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, !dry, "worklogs restore")
			if err != nil {
				return err
			}
			defer cleanup()

			if dry && yes {
				return a.fail(mode, 2, "validation_error", "--dry and --yes are mutually exclusive", nil)
			}
			if !dry && !yes {
				return a.fail(mode, 2, "validation_error", "worklogs restore requires --yes or --dry", nil)
			}

			weekOffsetSet := cmd.Flags().Changed("week-offset")
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
				WeekOffsetSet: weekOffsetSet,
			}
			result, err := service.RestoreBatch(effective, raw, dry, force)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderRestoreBatchJSON(raw, result, effective.Location)
			}

			if dry {
				return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "DELETED"}, restorePreviewRows(result.Items, effective.Location))
			}
			rows := make([][]string, 0, len(result.Restored))
			for _, item := range result.Items {
				rows = append(rows, []string{item.Record.ID, item.Record.IssueKey})
			}
			return renderTable(a.stdout, []string{"ID", "ISSUE"}, rows)
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Issue prefix")
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
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview matching restores")
	cmd.Flags().BoolVar(&yes, "yes", false, "Execute filtered batch restore")
	cmd.Flags().BoolVar(&force, "force", false, "Restore even when duplicate or overlap conflicts exist")
	return cmd
}

func (a *app) newTombstonesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tombstones",
		Short: "Manage deleted worklog tombstones",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(a.newTombstonesListCommand())
	cmd.AddCommand(a.newTombstonesSearchCommand())
	cmd.AddCommand(a.newTombstonesDeleteCommand())
	cmd.AddCommand(a.newTombstonesRestoreCommand())
	return cmd
}

func (a *app) newTombstonesListCommand() *cobra.Command {
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
		Short:   "List deleted worklog tombstones",
		Example: "  workledger tombstones list --today\n  workledger tombstones list --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
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
				WeekOffsetSet: weekOffsetSet,
				OnlyDeleted:   true,
			}
			_, tombstones, effectiveFilters, err := service.List(effective, raw)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTombstonesListJSON(effective, raw, effectiveFilters, tombstones)
			}

			if err := renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "DELETED"}, tombstoneFullRows(tombstones, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(tombstones), sumDeletedDurationSeconds(tombstones), "tombstones")
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

func (a *app) newTombstonesSearchCommand() *cobra.Command {
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
		Short:   "Search deleted worklog tombstones by description",
		Args:    cobra.ExactArgs(1),
		Example: "  workledger tombstones search review --today\n  workledger tombstones search docs --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			rawFilters := worklogs.ListFilters{
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
				WeekOffsetSet: weekOffsetSet,
				OnlyDeleted:   true,
			}
			_, tombstones, effectiveFilters, normalizedQuery, err := service.Search(effective, worklogs.SearchInput{
				Query:       args[0],
				ListFilters: rawFilters,
			})
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderTombstonesSearchJSON(effective, args[0], rawFilters, effectiveFilters, normalizedQuery, tombstones)
			}

			if err := renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "DELETED"}, tombstoneFullRows(tombstones, effective.Location)); err != nil {
				return err
			}
			return renderListTotalsFooter(a.stdout, len(tombstones), sumDeletedDurationSeconds(tombstones), "tombstones")
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

func (a *app) newTombstonesDeleteCommand() *cobra.Command {
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
	var dry bool
	var yes bool

	cmd := &cobra.Command{
		Use:     "delete [id]",
		Short:   "Permanently delete tombstones",
		Args:    cobra.MaximumNArgs(1),
		Example: "  workledger tombstones delete <id>\n  workledger tombstones delete --from 2026-05-14 --to 2026-05-16 --dry\n  workledger tombstones delete --today --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, true, "tombstones delete")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			if len(args) == 1 {
				if dry || yes || issue != "" || issuePrefix != "" || today || yesterday || monday || tuesday || wednesday || thursday || friday || saturday || sunday || currentWeek || lastWeek || currentMonth || lastMonth || from != "" || to != "" || weekOffsetSet {
					return a.fail(mode, 2, "validation_error", "single delete cannot be combined with batch delete flags", nil)
				}
				record, err := service.DeleteTombstone(args[0])
				if err != nil {
					return a.handleWorklogError(mode, effective, err)
				}
				if mode == "json" {
					return a.writeJSON(map[string]any{
						"id":         record.ID,
						"issue_key":  record.IssueKey,
						"deleted_at": record.DeletedAt.UTC().Format(time.RFC3339),
					})
				}
				return renderTable(a.stdout, []string{"ID", "ISSUE", "DELETED"}, deletedRows([]worklogs.Tombstone{{
					ID:        record.ID,
					IssueKey:  record.IssueKey,
					DeletedAt: record.DeletedAt,
				}}))
			}

			if dry && yes {
				return a.fail(mode, 2, "validation_error", "--dry and --yes are mutually exclusive", nil)
			}
			if !dry && !yes {
				return a.fail(mode, 2, "validation_error", "filtered batch delete requires --yes or --dry", nil)
			}

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
				WeekOffsetSet: weekOffsetSet,
			}
			result, err := service.DeleteTombstoneBatch(effective, raw, dry)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderDeleteTombstoneBatchJSON(raw, result, effective.Location)
			}

			if dry {
				return renderTable(a.stdout, []string{"ID", "ISSUE", "DELETED"}, deletedRows(result.Items))
			}
			rows := make([][]string, 0, len(result.Deleted))
			for _, id := range result.Deleted {
				rows = append(rows, []string{id})
			}
			return renderTable(a.stdout, []string{"ID"}, rows)
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Issue prefix")
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
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview matching deletes")
	cmd.Flags().BoolVar(&yes, "yes", false, "Execute filtered batch delete")
	return cmd
}

func (a *app) newTombstonesRestoreCommand() *cobra.Command {
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
	var dry bool
	var yes bool
	var force bool

	cmd := &cobra.Command{
		Use:     "restore",
		Short:   "Restore deleted local worklogs",
		Args:    cobra.NoArgs,
		Example: "  workledger tombstones restore --from 2026-05-14 --to 2026-05-16 --dry\n  workledger tombstones restore --today --yes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, !dry, "tombstones restore")
			if err != nil {
				return err
			}
			defer cleanup()

			if dry && yes {
				return a.fail(mode, 2, "validation_error", "--dry and --yes are mutually exclusive", nil)
			}
			if !dry && !yes {
				return a.fail(mode, 2, "validation_error", "tombstones restore requires --yes or --dry", nil)
			}

			weekOffsetSet := cmd.Flags().Changed("week-offset")
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
				WeekOffsetSet: weekOffsetSet,
			}
			result, err := service.RestoreBatch(effective, raw, dry, force)
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}

			if mode == "json" {
				return a.renderRestoreBatchJSON(raw, result, effective.Location)
			}

			if dry {
				return renderTable(a.stdout, []string{"ID", "ISSUE", "WINDOW", "DURATION", "DESCRIPTION", "DELETED"}, restorePreviewRows(result.Items, effective.Location))
			}
			rows := make([][]string, 0, len(result.Restored))
			for _, item := range result.Items {
				rows = append(rows, []string{item.Record.ID, item.Record.IssueKey})
			}
			return renderTable(a.stdout, []string{"ID", "ISSUE"}, rows)
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Issue key")
	cmd.Flags().StringVar(&issuePrefix, "issue-prefix", "", "Issue prefix")
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
	cmd.Flags().BoolVar(&dry, "dry", false, "Preview matching restores")
	cmd.Flags().BoolVar(&yes, "yes", false, "Execute filtered batch restore")
	cmd.Flags().BoolVar(&force, "force", false, "Restore even when duplicate or overlap conflicts exist")
	return cmd
}

func (a *app) newStatusCommand() *cobra.Command {
	var adapter string
	var instance string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check adapter status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, err := config.LoadEffective()
			if err != nil {
				if errors.Is(err, config.ErrConfigNotFound) {
					return a.fail(mode, 2, "validation_error", "config file does not exist; run workledger init", nil)
				}
				var validationErr config.ValidationErrors
				if errors.As(err, &validationErr) {
					return a.fail(mode, 2, "validation_error", "config validation failed", validationErr.Issues)
				}
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}

			var rows []statusRow
			exitCode := 0
			switch adapter {
			case "":
				rows, exitCode = a.collectAllStatusRows(cmd.Context(), effective)
			case "clockify":
				var err error
				rows, err = a.collectClockifyStatusRows(cmd.Context(), effective, instance)
				if err != nil {
					var requestErr *clockifyadapter.RequestError
					if errors.As(err, &requestErr) {
						return a.handleClockifyError(mode, err)
					}
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
			case "jira-cloud":
				var err error
				rows, err = a.collectJiraCloudStatusRows(cmd.Context(), effective, instance)
				if err != nil {
					return a.handleJiraCloudError(mode, err)
				}
			case "jira-data-center":
				var err error
				rows, err = a.collectJiraDataStatusRows(cmd.Context(), effective, instance)
				if err != nil {
					return a.handleJiraDataError(mode, err)
				}
			default:
				return a.fail(mode, 2, "validation_error", "supported adapters are clockify, jira-cloud, and jira-data-center", nil)
			}

			if mode == "json" {
				if err := a.renderStatusJSON(rows); err != nil {
					return err
				}
			} else {
				if err := renderTable(a.stdout, statusTableHeaders(), statusTableRows(rows)); err != nil {
					return err
				}
			}
			if exitCode != 0 {
				return exitError{code: exitCode}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&adapter, "adapter", "", "Adapter family")
	cmd.Flags().StringVar(&instance, "instance", "", "Adapter instance")
	return cmd
}

type statusRow struct {
	Adapter     string
	Instance    string
	Status      string
	BaseURL     string
	WorkspaceID string
	UserID      string
	User        string
}

func (a *app) collectAllStatusRows(ctx context.Context, effective config.EffectiveConfig) ([]statusRow, int) {
	rows := make([]statusRow, 0)
	exitCode := 0

	clockifyRows, err := a.collectClockifyStatusRows(ctx, effective, "")
	if err != nil {
		rows = append(rows, failedClockifyStatusRow(effective, err))
		exitCode = firstStatusExitCode(exitCode, statusExitCodeForClockify(err))
	} else {
		rows = append(rows, clockifyRows...)
	}

	jiraCloudRows, jiraCloudExitCode := a.collectJiraCloudStatusRowsTolerant(ctx, effective)
	rows = append(rows, jiraCloudRows...)
	exitCode = firstStatusExitCode(exitCode, jiraCloudExitCode)

	jiraDataRows, jiraDataExitCode := a.collectJiraDataStatusRowsTolerant(ctx, effective)
	rows = append(rows, jiraDataRows...)
	exitCode = firstStatusExitCode(exitCode, jiraDataExitCode)

	return rows, exitCode
}

func (a *app) collectClockifyStatusRows(ctx context.Context, effective config.EffectiveConfig, instanceName string) ([]statusRow, error) {
	if effective.File.Clockify == nil {
		return nil, nil
	}

	resolvedInstance, clockifyCfg, err := config.ResolveClockifyInstance(effective, instanceName)
	if err != nil {
		return nil, err
	}

	client := clockifyadapter.NewClient(clockifyCfg.Auth.APIKey)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user.ID != clockifyCfg.UserID {
		return nil, errors.New("configured clockify.user_id does not match authenticated user")
	}
	if clockifyCfg.WorkspaceID != user.ActiveWorkspace && clockifyCfg.WorkspaceID != user.DefaultWorkspace {
		return nil, errors.New("configured clockify.workspace_id is not visible for the authenticated user")
	}
	windowFrom, windowTo, err := parsePlanWindow(effective, false, false, false, false, false, false, false, false, false, true, false, false, false, "", "", 0, false)
	if err != nil {
		return nil, err
	}
	_, err = client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, windowFrom, windowTo)
	if err != nil {
		return nil, err
	}
	_, err = client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return nil, err
	}

	return []statusRow{{
		Adapter:     "clockify",
		Instance:    resolvedInstance,
		Status:      "OK",
		WorkspaceID: clockifyCfg.WorkspaceID,
		UserID:      clockifyCfg.UserID,
	}}, nil
}

func (a *app) collectJiraCloudStatusRows(ctx context.Context, effective config.EffectiveConfig, instanceName string) ([]statusRow, error) {
	if effective.File.JiraCloud == nil || len(effective.File.JiraCloud.Instances) == 0 {
		return nil, nil
	}

	names, err := resolveJiraCloudStatusInstanceNames(effective, instanceName)
	if err != nil {
		return nil, err
	}

	rows := make([]statusRow, 0, len(names))
	for _, name := range names {
		_, instance, err := config.ResolveJiraCloudInstance(effective, name)
		if err != nil {
			return nil, err
		}
		client := jiracloudadapter.NewClient(instance.BaseURL, instance.Auth.Email, instance.Auth.Token)
		user, err := client.CurrentUser(ctx)
		if err != nil {
			return nil, err
		}
		rows = append(rows, statusRow{
			Adapter:  "jira-cloud",
			Instance: name,
			Status:   "OK",
			BaseURL:  instance.BaseURL,
			User:     firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID),
		})
	}

	return rows, nil
}

func (a *app) collectJiraCloudStatusRowsTolerant(ctx context.Context, effective config.EffectiveConfig) ([]statusRow, int) {
	if effective.File.JiraCloud == nil || len(effective.File.JiraCloud.Instances) == 0 {
		return nil, 0
	}

	names := sortedJiraCloudInstanceNames(effective.File.JiraCloud.Instances)
	rows := make([]statusRow, 0, len(names))
	exitCode := 0
	for _, name := range names {
		_, instance, err := config.ResolveJiraCloudInstance(effective, name)
		if err != nil {
			rows = append(rows, statusRow{
				Adapter:  "jira-cloud",
				Instance: name,
				Status:   err.Error(),
			})
			exitCode = firstStatusExitCode(exitCode, 2)
			continue
		}
		client := jiracloudadapter.NewClient(instance.BaseURL, instance.Auth.Email, instance.Auth.Token)
		user, err := client.CurrentUser(ctx)
		if err != nil {
			rows = append(rows, statusRow{
				Adapter:  "jira-cloud",
				Instance: name,
				Status:   err.Error(),
				BaseURL:  instance.BaseURL,
			})
			exitCode = firstStatusExitCode(exitCode, statusExitCodeForJiraCloud(err))
			continue
		}
		rows = append(rows, statusRow{
			Adapter:  "jira-cloud",
			Instance: name,
			Status:   "OK",
			BaseURL:  instance.BaseURL,
			User:     firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID),
		})
	}

	return rows, exitCode
}

func (a *app) collectJiraDataStatusRows(ctx context.Context, effective config.EffectiveConfig, instanceName string) ([]statusRow, error) {
	if effective.File.JiraData == nil || len(effective.File.JiraData.Instances) == 0 {
		return nil, nil
	}

	names, err := resolveJiraDataStatusInstanceNames(effective, instanceName)
	if err != nil {
		return nil, err
	}

	rows := make([]statusRow, 0, len(names))
	for _, name := range names {
		_, instance, err := config.ResolveJiraDataInstance(effective, name)
		if err != nil {
			return nil, err
		}
		client := jiradcadapter.NewClient(instance.BaseURL, instance.Auth.Bearer.Token)
		user, err := client.CurrentUser(ctx)
		if err != nil {
			return nil, err
		}
		rows = append(rows, statusRow{
			Adapter:  "jira-data-center",
			Instance: name,
			Status:   "OK",
			BaseURL:  instance.BaseURL,
			User:     firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID),
		})
	}

	return rows, nil
}

func (a *app) collectJiraDataStatusRowsTolerant(ctx context.Context, effective config.EffectiveConfig) ([]statusRow, int) {
	if effective.File.JiraData == nil || len(effective.File.JiraData.Instances) == 0 {
		return nil, 0
	}

	names := sortedJiraDataInstanceNames(effective.File.JiraData.Instances)
	rows := make([]statusRow, 0, len(names))
	exitCode := 0
	for _, name := range names {
		_, instance, err := config.ResolveJiraDataInstance(effective, name)
		if err != nil {
			rows = append(rows, statusRow{
				Adapter:  "jira-data-center",
				Instance: name,
				Status:   err.Error(),
			})
			exitCode = firstStatusExitCode(exitCode, 2)
			continue
		}
		client := jiradcadapter.NewClient(instance.BaseURL, instance.Auth.Bearer.Token)
		user, err := client.CurrentUser(ctx)
		if err != nil {
			rows = append(rows, statusRow{
				Adapter:  "jira-data-center",
				Instance: name,
				Status:   err.Error(),
				BaseURL:  instance.BaseURL,
			})
			exitCode = firstStatusExitCode(exitCode, statusExitCodeForJiraData(err))
			continue
		}
		rows = append(rows, statusRow{
			Adapter:  "jira-data-center",
			Instance: name,
			Status:   "OK",
			BaseURL:  instance.BaseURL,
			User:     firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID),
		})
	}

	return rows, exitCode
}

func (a *app) renderStatusJSON(rows []statusRow) error {
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"adapter":      row.Adapter,
			"instance":     emptyToNil(row.Instance),
			"status":       row.Status,
			"base_url":     emptyToNil(row.BaseURL),
			"workspace_id": emptyToNil(row.WorkspaceID),
			"user_id":      emptyToNil(row.UserID),
			"user":         emptyToNil(row.User),
		})
	}

	return a.writeJSON(map[string]any{"items": items})
}

func statusTableHeaders() []string {
	return []string{"ADAPTER", "INSTANCE", "BASE_URL", "USER", "STATUS"}
}

func statusTableRows(rows []statusRow) [][]string {
	items := make([][]string, 0, len(rows))
	for _, row := range rows {
		instance := row.Instance
		user := row.User
		if row.Adapter == "clockify" {
			user = row.UserID
		}
		items = append(items, []string{
			row.Adapter,
			instance,
			row.BaseURL,
			user,
			row.Status,
		})
	}
	return items
}

func sortedJiraCloudInstanceNames(instances map[string]config.JiraCloudInstance) []string {
	names := make([]string, 0, len(instances))
	for name := range instances {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedJiraDataInstanceNames(instances map[string]config.JiraDataCenterInstance) []string {
	names := make([]string, 0, len(instances))
	for name := range instances {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveJiraCloudStatusInstanceNames(effective config.EffectiveConfig, instanceName string) ([]string, error) {
	if instanceName == "" {
		return sortedJiraCloudInstanceNames(effective.File.JiraCloud.Instances), nil
	}

	resolved, _, err := config.ResolveJiraCloudInstance(effective, instanceName)
	if err != nil {
		return nil, err
	}
	return []string{resolved}, nil
}

func resolveJiraDataStatusInstanceNames(effective config.EffectiveConfig, instanceName string) ([]string, error) {
	if instanceName == "" {
		return sortedJiraDataInstanceNames(effective.File.JiraData.Instances), nil
	}

	resolved, _, err := config.ResolveJiraDataInstance(effective, instanceName)
	if err != nil {
		return nil, err
	}
	return []string{resolved}, nil
}

func failedClockifyStatusRow(effective config.EffectiveConfig, err error) statusRow {
	row := statusRow{
		Adapter:  "clockify",
		Instance: config.ClockifyInstanceName,
		Status:   err.Error(),
	}
	if effective.File.Clockify != nil {
		row.WorkspaceID = effective.File.Clockify.WorkspaceID
		row.UserID = effective.File.Clockify.UserID
	}
	return row
}

func firstStatusExitCode(current, next int) int {
	if current != 0 {
		return current
	}
	return next
}

func statusExitCodeForClockify(err error) int {
	var requestErr *clockifyadapter.RequestError
	if errors.As(err, &requestErr) {
		if requestErr.StatusCode == http.StatusUnauthorized || requestErr.StatusCode == http.StatusForbidden {
			return 4
		}
		return 5
	}
	return 2
}

func statusExitCodeForJiraData(err error) int {
	var requestErr *jiradcadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return 4
		case http.StatusNotFound:
			return 3
		default:
			return 5
		}
	}
	return 1
}

func statusExitCodeForJiraCloud(err error) int {
	var requestErr *jiracloudadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return 4
		case http.StatusNotFound:
			return 3
		default:
			return 5
		}
	}
	return 1
}

func totalsStatusForClockifyError(err error) string {
	var requestErr *clockifyadapter.RequestError
	if errors.As(err, &requestErr) {
		if requestErr.StatusCode == http.StatusUnauthorized || requestErr.StatusCode == http.StatusForbidden {
			return "auth_error"
		}
		return "external_error"
	}
	return "unexpected_error"
}

func totalsExitCodeForJiraData(err error) int {
	var requestErr *jiradcadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return 4
		case http.StatusNotFound:
			return 3
		default:
			return 5
		}
	}
	return 1
}

func totalsStatusForJiraDataError(err error) string {
	var requestErr *jiradcadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "auth_error"
		case http.StatusNotFound:
			return "not_found"
		default:
			return "remote_error"
		}
	}
	return "unexpected_error"
}

func totalsExitCodeForJiraCloud(err error) int {
	var requestErr *jiracloudadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return 4
		case http.StatusNotFound:
			return 3
		default:
			return 5
		}
	}
	return 1
}

func totalsStatusForJiraCloudError(err error) string {
	var requestErr *jiracloudadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "auth_error"
		case http.StatusNotFound:
			return "not_found"
		default:
			return "remote_error"
		}
	}
	return "unexpected_error"
}

func (a *app) newIssueMetadataCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue-metadata",
		Short: "Manage issue metadata",
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(a.newIssueMetadataListCommand())
	cmd.AddCommand(a.newIssueMetadataRefreshCommand())
	return cmd
}

func (a *app) newIssueMetadataListCommand() *cobra.Command {
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
		Short:   "List cached issue metadata",
		Example: "  workledger issue-metadata list --issue PROJ-123\n  workledger issue-metadata list --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, service, cleanup, err := a.loadService(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
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
				WeekOffsetSet: weekOffsetSet,
			}

			var (
				items   []worklogs.IssueMetadata
				filters worklogs.EffectiveFilters
			)
			if issue != "" && !hasSelector(raw) && !worklogs.IsValidIssueKey(issue) {
				return a.fail(mode, 2, "validation_error", "issue must match <PROJECTKEY>-<NUMBER>", nil)
			}
			if worklogs.IsValidIssueKey(issue) && !hasSelector(raw) {
				filters.Timezone = effective.TimezoneName
				filters.IssueKey = &issue
				item, err := service.ShowIssueMetadata(issue)
				if err != nil {
					return a.handleIssueMetadataError(mode, err)
				}
				items = []worklogs.IssueMetadata{item}
			} else {
				active, _, effectiveFilters, err := service.List(effective, raw)
				if err != nil {
					return a.handleWorklogError(mode, effective, err)
				}
				items, err = service.ListIssueMetadata(issueKeys(active))
				if err != nil {
					return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
				}
				filters = effectiveFilters
			}

			if mode == "json" {
				return a.renderIssueMetadataListJSON(raw, filters, items, effective.Location)
			}
			return renderTable(a.stdout, []string{"ISSUE", "MAX_ESTIMATE_SECONDS", "SOURCE_ADAPTER", "SOURCE_INSTANCE", "REFRESHED_AT"}, issueMetadataRows(items))
		},
	}

	cmd.Flags().StringVar(&issue, "issue", "", "Filter to one issue")
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
	}, useDateWindowHelp)
	return cmd
}

func (a *app) newIssueMetadataRefreshCommand() *cobra.Command {
	var adapter string
	var field string
	var instance string
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
		Use:     "refresh",
		Short:   "Refresh local issue metadata from an adapter",
		Example: "  workledger issue-metadata refresh --adapter jira-cloud --field max-estimate --today\n  workledger issue-metadata refresh --adapter jira-data-center --field max-estimate --from 2026-05-14 --to 2026-05-16",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if field != "max-estimate" {
				return a.fail(mode, 2, "validation_error", "only --field=max-estimate is supported in this slice", nil)
			}
			effective, service, cleanup, err := a.loadService(mode, true, "issue-metadata refresh")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			items, _, _, err := service.List(effective, worklogs.ListFilters{
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
				WeekOffsetSet: weekOffsetSet,
			})
			if err != nil {
				return a.handleWorklogError(mode, effective, err)
			}
			var resolvedInstance string
			var refreshed []map[string]any
			issueKeys := issueKeys(items)
			switch adapter {
			case "jira-data-center":
				resolvedInstance, err = resolveJiraDataInstanceName(effective, instance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				if len(issueKeys) == 0 {
					if mode == "json" {
						return a.writeJSON(map[string]any{"adapter": adapter, "instance": resolvedInstance, "field": field, "updated": 0, "issues": []any{}})
					}
					_, _ = fmt.Fprintln(a.stdout, "updated=0")
					return nil
				}
				if effective.File.JiraData != nil {
					if cfg, ok := effective.File.JiraData.Instances[resolvedInstance]; ok && cfg.Routing != nil {
						issuePrefixes, err := config.JiraDataIssuePrefixes(effective, resolvedInstance)
						if err != nil {
							return a.fail(mode, 2, "validation_error", err.Error(), nil)
						}
						issueKeys = filterIssueKeysByPrefixes(issueKeys, issuePrefixes)
					}
				}
				if len(issueKeys) == 0 {
					if mode == "json" {
						return a.writeJSON(map[string]any{"adapter": adapter, "instance": resolvedInstance, "field": field, "updated": 0, "issues": []any{}})
					}
					_, _ = fmt.Fprintln(a.stdout, "updated=0")
					return nil
				}
				_, cfg, err := config.ResolveJiraDataInstance(effective, resolvedInstance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				client := jiradcadapter.NewClient(cfg.BaseURL, cfg.Auth.Bearer.Token)
				refreshed = make([]map[string]any, 0, len(issueKeys))
				for _, key := range issueKeys {
					issueItem, err := client.GetIssue(cmd.Context(), key, []string{"timetracking"})
					if err != nil {
						return a.handleJiraDataError(mode, err)
					}
					var estimate *int64
					if issueItem.Fields.Timetracking != nil {
						estimate = issueItem.Fields.Timetracking.OriginalEstimateSeconds
					}
					if err := service.UpsertIssueMetadata(key, estimate, "jira-data-center", resolvedInstance, time.Now().UTC()); err != nil {
						return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
					}
					refreshed = append(refreshed, map[string]any{"issue_key": key, "max_estimate_seconds": int64PtrToAny(estimate)})
				}
			case "jira-cloud":
				resolvedInstance, err = resolveJiraCloudInstanceName(effective, instance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				if len(issueKeys) == 0 {
					if mode == "json" {
						return a.writeJSON(map[string]any{"adapter": adapter, "instance": resolvedInstance, "field": field, "updated": 0, "issues": []any{}})
					}
					_, _ = fmt.Fprintln(a.stdout, "updated=0")
					return nil
				}
				if effective.File.JiraCloud != nil {
					if cfg, ok := effective.File.JiraCloud.Instances[resolvedInstance]; ok && cfg.Routing != nil {
						issuePrefixes, err := config.JiraCloudIssuePrefixes(effective, resolvedInstance)
						if err != nil {
							return a.fail(mode, 2, "validation_error", err.Error(), nil)
						}
						issueKeys = filterIssueKeysByPrefixes(issueKeys, issuePrefixes)
					}
				}
				if len(issueKeys) == 0 {
					if mode == "json" {
						return a.writeJSON(map[string]any{"adapter": adapter, "instance": resolvedInstance, "field": field, "updated": 0, "issues": []any{}})
					}
					_, _ = fmt.Fprintln(a.stdout, "updated=0")
					return nil
				}
				_, cfg, err := config.ResolveJiraCloudInstance(effective, resolvedInstance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				client := jiracloudadapter.NewClient(cfg.BaseURL, cfg.Auth.Email, cfg.Auth.Token)
				refreshed = make([]map[string]any, 0, len(issueKeys))
				for _, key := range issueKeys {
					issueItem, err := client.GetIssue(cmd.Context(), key, []string{"timetracking"})
					if err != nil {
						return a.handleJiraCloudError(mode, err)
					}
					var estimate *int64
					if issueItem.Fields.Timetracking != nil {
						estimate = issueItem.Fields.Timetracking.OriginalEstimateSeconds
					}
					if err := service.UpsertIssueMetadata(key, estimate, "jira-cloud", resolvedInstance, time.Now().UTC()); err != nil {
						return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
					}
					refreshed = append(refreshed, map[string]any{"issue_key": key, "max_estimate_seconds": int64PtrToAny(estimate)})
				}
			default:
				return a.fail(mode, 2, "validation_error", "supported adapters are jira-cloud and jira-data-center", nil)
			}
			if mode == "json" {
				return a.writeJSON(map[string]any{"adapter": adapter, "instance": resolvedInstance, "field": field, "updated": len(refreshed), "issues": refreshed})
			}
			return renderTable(a.stdout, []string{"ISSUE", "MAX_ESTIMATE_SECONDS"}, issueMetadataRefreshRows(refreshed))
		},
	}
	cmd.Flags().StringVar(&adapter, "adapter", "", "Adapter family")
	cmd.Flags().StringVar(&field, "field", "", "Metadata field")
	cmd.Flags().StringVar(&instance, "instance", "", "Jira instance")
	cmd.Flags().StringVar(&issue, "issue", "", "Filter to one issue")
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
	}, useDateWindowHelp)
	return cmd
}

func (a *app) newTotalsCommand() *cobra.Command {
	var adapter string
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
	var instance string
	var routeProfile string
	var details bool
	var progressMode string

	cmd := &cobra.Command{
		Use:     "totals",
		Short:   "Summarize local totals or compare with remote adapter totals",
		Example: totalsExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)

			effective, store, cleanup, err := a.loadStore(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			windowFrom, windowTo, err := parsePlanWindow(effective, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to, weekOffset, weekOffsetSet)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			service := totals.NewService(store)
			var result totals.Result
			resolvedInstance := ""
			reporter, err := a.progressReporter(progressMode)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			selectedAdapter := adapter
			if routeProfile != "" {
				selectedAdapter, err = resolveTotalsRouteProfileAdapter(effective, adapter, instance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
			}

			opts := totals.Options{Reporter: reporter, Instance: instance, RouteProfile: routeProfile}
			switch selectedAdapter {
			case "":
				items, exitCode := service.CollectAll(cmd.Context(), effective, windowFrom, windowTo, opts)
				if instance != "" && len(items) == 0 {
					return a.fail(mode, 2, "validation_error", fmt.Sprintf("instance %q not found", instance), nil)
				}
				if mode == "json" {
					if err := a.renderAllTotalsJSON(effective, windowFrom, windowTo, items); err != nil {
						return err
					}
				} else {
					if err := a.renderAllTotalsTable(effective, windowFrom, windowTo, items); err != nil {
						return err
					}
				}
				if exitCode != 0 {
					return exitError{code: exitCode}
				}
				return nil
			case "clockify":
				resolvedInstance, _, err = config.ResolveClockifyInstance(effective, instance)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				result, err = service.CompareClockifyRemote(cmd.Context(), effective, windowFrom, windowTo, opts)
				if err != nil {
					if totals.IsValidationError(err) {
						return a.fail(mode, 2, "validation_error", err.Error(), nil)
					}
					return a.handleClockifyError(mode, err)
				}
			case "jira-data-center":
				resolvedInstance, result, err = service.CompareJiraDataRemote(cmd.Context(), effective, instance, windowFrom, windowTo, opts)
				if err != nil {
					if totals.IsValidationError(err) {
						return a.fail(mode, 2, "validation_error", err.Error(), nil)
					}
					return a.handleJiraDataError(mode, err)
				}
			case "jira-cloud":
				resolvedInstance, result, err = service.CompareJiraCloudRemote(cmd.Context(), effective, instance, windowFrom, windowTo, opts)
				if err != nil {
					if totals.IsValidationError(err) {
						return a.fail(mode, 2, "validation_error", err.Error(), nil)
					}
					return a.handleJiraCloudError(mode, err)
				}
			default:
				return a.fail(mode, 2, "validation_error", "supported adapters are clockify, jira-cloud, and jira-data-center", nil)
			}

			if mode == "json" {
				return a.renderTotalsJSON(selectedAdapter, resolvedInstance, routeProfile, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to, weekOffset, weekOffsetSet, effective, windowFrom, windowTo, result)
			}
			return a.renderTotalsTable(selectedAdapter, resolvedInstance, routeProfile, details, effective, windowFrom, windowTo, result)
		},
	}

	cmd.Flags().StringVar(&adapter, "adapter", "", "Adapter family")
	cmd.Flags().StringVar(&instance, "instance", "", "Adapter instance")
	cmd.Flags().StringVar(&routeProfile, "route-profile", "", "Jira route profile")
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
	}, useDateWindowHelp)
	cmd.Flags().BoolVar(&details, "details", false, "Show per-day totals")
	cmd.Flags().StringVar(&progressMode, "progress", string(progress.ModeAuto), "Progress mode: auto, bar, plain, or off")
	return cmd
}

type totalsCollectionItem = totals.CollectionItem

func (a *app) collectAllTotalsItems(ctx context.Context, effective config.EffectiveConfig, service *totals.Service, windowFrom, windowTo time.Time) ([]totalsCollectionItem, int) {
	items := make([]totalsCollectionItem, 0)
	exitCode := 0

	clockifyItem := a.collectClockifyTotalsItem(ctx, effective, service, windowFrom, windowTo)
	if clockifyItem != nil {
		items = append(items, *clockifyItem)
		exitCode = firstStatusExitCode(exitCode, clockifyItem.Code)
	}

	jiraCloudItems := a.collectJiraCloudTotalsItems(ctx, effective, service, windowFrom, windowTo)
	for _, item := range jiraCloudItems {
		items = append(items, item)
		exitCode = firstStatusExitCode(exitCode, item.Code)
	}

	jiraDataItems := a.collectJiraDataTotalsItems(ctx, effective, service, windowFrom, windowTo)
	for _, item := range jiraDataItems {
		items = append(items, item)
		exitCode = firstStatusExitCode(exitCode, item.Code)
	}

	return items, exitCode
}

func (a *app) collectClockifyTotalsItem(ctx context.Context, effective config.EffectiveConfig, service *totals.Service, windowFrom, windowTo time.Time) *totalsCollectionItem {
	if effective.File.Clockify == nil {
		return nil
	}

	item := totalsCollectionItem{Adapter: "clockify", Instance: config.ClockifyInstanceName}
	_, clockifyCfg, err := config.ResolveClockifyInstance(effective, "")
	if err != nil {
		if effective.File.Clockify != nil {
			item.Display = effective.File.Clockify.WorkspaceID
		}
		item.Code = 2
		item.Status = "validation_error"
		item.Message = err.Error()
		return &item
	}
	item.Display = clockifyCfg.WorkspaceID
	client := clockifyadapter.NewClient(clockifyCfg.Auth.APIKey)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		item.Code = statusExitCodeForClockify(err)
		item.Status = totalsStatusForClockifyError(err)
		item.Message = err.Error()
		return &item
	}
	if user.ID != clockifyCfg.UserID {
		item.Code = 2
		item.Status = "validation_error"
		item.Message = "configured clockify.user_id does not match authenticated user"
		return &item
	}
	if clockifyCfg.WorkspaceID != user.ActiveWorkspace && clockifyCfg.WorkspaceID != user.DefaultWorkspace {
		item.Code = 2
		item.Status = "validation_error"
		item.Message = "configured clockify.workspace_id is not visible for the authenticated user"
		return &item
	}
	entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, windowFrom, windowTo)
	if err != nil {
		item.Code = statusExitCodeForClockify(err)
		item.Status = totalsStatusForClockifyError(err)
		item.Message = err.Error()
		return &item
	}
	result, err := service.CompareClockify(ctx, effective, windowFrom, windowTo, entries)
	if err != nil {
		item.Code = 1
		item.Status = "unexpected_error"
		item.Message = err.Error()
		return &item
	}
	item.Result = &result
	return &item
}

func (a *app) collectJiraCloudTotalsItems(ctx context.Context, effective config.EffectiveConfig, service *totals.Service, windowFrom, windowTo time.Time) []totalsCollectionItem {
	if effective.File.JiraCloud == nil || len(effective.File.JiraCloud.Instances) == 0 {
		return nil
	}

	names := sortedJiraCloudInstanceNames(effective.File.JiraCloud.Instances)
	items := make([]totalsCollectionItem, 0, len(names))
	for _, name := range names {
		item := totalsCollectionItem{Adapter: "jira-cloud", Instance: name}
		issuePrefixes, err := config.JiraCloudIssuePrefixesForTotals(effective, name)
		if err != nil {
			item.Code = 2
			item.Status = "validation_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		excludedIssues, err := config.JiraExcludedIssuesForInstance(effective, "jira-cloud", name)
		if err != nil {
			item.Code = 2
			item.Status = "validation_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		rows, err := a.loadJiraCloudTotalsRows(ctx, effective, name, windowFrom, windowTo, issuePrefixes, excludedIssues)
		if err != nil {
			item.Code = totalsExitCodeForJiraCloud(err)
			item.Status = totalsStatusForJiraCloudError(err)
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		result, err := service.CompareJiraDataWithExclusions(ctx, effective, windowFrom, windowTo, rows, issuePrefixes, toExactSet(excludedIssues))
		if err != nil {
			item.Code = 1
			item.Status = "unexpected_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		item.Result = &result
		items = append(items, item)
	}
	return items
}

func (a *app) collectJiraDataTotalsItems(ctx context.Context, effective config.EffectiveConfig, service *totals.Service, windowFrom, windowTo time.Time) []totalsCollectionItem {
	if effective.File.JiraData == nil || len(effective.File.JiraData.Instances) == 0 {
		return nil
	}

	names := sortedJiraDataInstanceNames(effective.File.JiraData.Instances)
	items := make([]totalsCollectionItem, 0, len(names))
	for _, name := range names {
		item := totalsCollectionItem{Adapter: "jira-data-center", Instance: name}
		issuePrefixes, err := config.JiraDataIssuePrefixesForTotals(effective, name)
		if err != nil {
			item.Code = 2
			item.Status = "validation_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		excludedIssues, err := config.JiraExcludedIssuesForInstance(effective, "jira-data-center", name)
		if err != nil {
			item.Code = 2
			item.Status = "validation_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		rows, err := a.loadJiraDataTotalsRows(ctx, effective, name, windowFrom, windowTo, issuePrefixes, excludedIssues)
		if err != nil {
			item.Code = totalsExitCodeForJiraData(err)
			item.Status = totalsStatusForJiraDataError(err)
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		result, err := service.CompareJiraDataWithExclusions(ctx, effective, windowFrom, windowTo, rows, issuePrefixes, toExactSet(excludedIssues))
		if err != nil {
			item.Code = 1
			item.Status = "unexpected_error"
			item.Message = err.Error()
			items = append(items, item)
			continue
		}
		item.Result = &result
		items = append(items, item)
	}
	return items
}

func (a *app) newPlanCommand() *cobra.Command {
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Review and apply saved reconcile plans",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	planCmd.AddCommand(a.newPlanReconcileCommand())
	planCmd.AddCommand(a.newPlanShowCommand())
	planCmd.AddCommand(a.newPlanListCommand())
	planCmd.AddCommand(a.newPlanApplyCommand())
	planCmd.AddCommand(a.newPlanRetryCommand())

	return planCmd
}

func (a *app) newPlanReconcileCommand() *cobra.Command {
	var pull bool
	var push bool
	var adapters []string
	var instances []string
	var routeProfile string
	var onlyDeleted bool
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
	var progressMode string

	cmd := &cobra.Command{
		Use:     "reconcile",
		Short:   "Create a saved reconcile plan",
		Example: planReconcileExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if pull == push {
				return a.fail(mode, 2, "validation_error", "reconcile requires exactly one of --pull or --push", nil)
			}
			if pull && onlyDeleted {
				return a.fail(mode, 2, "validation_error", "--only-deleted can only be used with --push", nil)
			}

			effective, store, cleanup, err := a.loadStore(mode, true, "plan reconcile")
			if err != nil {
				return err
			}
			defer cleanup()

			weekOffsetSet := cmd.Flags().Changed("week-offset")
			windowFrom, windowTo, err := parsePlanWindow(effective, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to, weekOffset, weekOffsetSet)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			reporter, err := a.progressReporter(progressMode)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			selectedAdapters, selectedInstances, err := validateReconcileScope(effective, adapters, instances)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}

			plans := reconcile.NewService(store)
			var plan reconcile.Plan
			var result reconcile.ReconcileResult
			scope := reconcile.ReconcileScope{AdapterFamilies: selectedAdapters, Instances: selectedInstances}
			if pull {
				plan, err = plans.CreateMultiPullPlan(cmd.Context(), effective, scope, windowFrom, windowTo, reconcile.PlanOptions{Reporter: reporter})
			} else {
				result, err = plans.ReconcileMultiPushPlan(cmd.Context(), effective, scope, routeProfile, windowFrom, windowTo, onlyDeleted, reconcile.PlanOptions{Reporter: reporter})
				if err == nil && result.Plan != nil {
					plan = *result.Plan
				}
			}
			if err != nil {
				return a.handleReconcileAdapterError(mode, strings.Join(selectedAdapters, ","), err)
			}

			if result.NoPlan != nil {
				if mode == "json" {
					return a.writeJSON(reconcileNoPlanJSON(*result.NoPlan))
				}
				return renderReconcileNoPlanTable(a.stdout, *result.NoPlan, effective.Location)
			}

			if mode == "json" {
				if err := a.writeJSON(planJSON(plan)); err != nil {
					return err
				}
			} else {
				if err := renderTable(a.stdout, []string{"PLAN_ID", "STATUS", "ACTIONABLE", "INVALID_FINDINGS"}, [][]string{{plan.ID, plan.AggregateStatus, fmt.Sprint(countPlanItemsByStatus(plan.Items, "ready")), fmt.Sprint(len(plan.Findings))}}); err != nil {
					return err
				}
				if err := renderReconcilePlanNextSteps(a.stdout, plan); err != nil {
					return err
				}
			}
			if hasPlanItemsWithStatus(plan.Items, "check_failed") {
				return exitError{code: 6}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&pull, "pull", false, "Create a pull plan")
	cmd.Flags().BoolVar(&push, "push", false, "Create a push plan")
	cmd.Flags().StringArrayVar(&adapters, "adapter", nil, "Adapter family")
	cmd.Flags().StringArrayVar(&instances, "instance", nil, "Adapter instance allowlist")
	cmd.Flags().StringVar(&routeProfile, "route-profile", "", "Push route profile")
	cmd.Flags().BoolVar(&onlyDeleted, "only-deleted", false, "Push tombstoned local rows only")
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
	}, useDateWindowHelp)
	cmd.Flags().StringVar(&progressMode, "progress", string(progress.ModeAuto), "Progress mode: auto, bar, plain, or off")
	return cmd
}

func (a *app) newPlanShowCommand() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "show [plan-id]",
		Short: "Show a saved plan",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, store, cleanup, err := a.loadStore(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			planID := ""
			if len(args) == 1 {
				planID = args[0]
			}

			plan, err := reconcile.NewService(store).LoadPlan(planID)
			if err != nil {
				if errors.Is(err, reconcile.ErrPlanNotFound) {
					return a.fail(mode, 3, "not_found", "saved plan not found", nil)
				}
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if !showAll {
				plan.Items = filterPlanItemsByStatus(plan.Items, "ready")
			}
			if mode == "json" {
				return a.writeJSON(planJSON(plan))
			}

			return renderPlanShowTable(a.stdout, effective.Location, plan.Items)
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all plan items including non-ready")
	return cmd
}

func (a *app) newPlanListCommand() *cobra.Command {
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
		Short:   "List saved plans",
		Example: planListExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			effective, store, cleanup, err := a.loadStore(mode, false, "")
			if err != nil {
				return err
			}
			defer cleanup()

			items, err := reconcile.NewService(store).ListPlans()
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			weekOffsetSet := cmd.Flags().Changed("week-offset")
			if hasPlanWindowSelection(today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to) {
				windowFrom, windowTo, err := parsePlanWindow(effective, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to, weekOffset, weekOffsetSet)
				if err != nil {
					return a.fail(mode, 2, "validation_error", err.Error(), nil)
				}
				items = filterPlanListEntriesByCreatedAt(items, windowFrom, windowTo)
			}
			if mode == "json" {
				return a.writeJSON(map[string]any{"plans": planListJSON(items)})
			}

			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{
					item.ID,
					item.Direction,
					joinOrDash(item.AdapterFamilies),
					joinOrDash(item.TargetInstances),
					formatSavedPlanWindow(item.WindowFromUTC, item.WindowToUTC, effective.Location),
					item.CreatedAt.Format(time.RFC3339),
					fmt.Sprint(item.TotalItems),
					fmt.Sprint(item.ReadyItems),
					fmt.Sprint(item.SucceededItems),
				})
			}
			return renderTable(a.stdout, []string{"PLAN_ID", "DIRECTION", "ADAPTERS", "INSTANCES", "WINDOW", "CREATED_AT", "ITEMS", "READY", "SUCCEEDED"}, rows)
		},
	}

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
	}, useDateWindowHelp)
	return cmd
}

func (a *app) newPlanApplyCommand() *cobra.Command {
	var progressMode string

	cmd := &cobra.Command{
		Use:   "apply [plan-id]",
		Short: "Apply a saved plan",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			effective, store, cleanup, err := a.loadStore(mode, true, "plan apply")
			if err != nil {
				return err
			}
			defer cleanup()

			planID := ""
			if len(args) == 1 {
				planID = args[0]
			}
			reporter, err := a.progressReporter(progressMode)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			result, err := reconcile.NewService(store).ApplyPlan(effective, planID, reconcile.ApplyOptions{Reporter: reporter})
			if err != nil {
				if errors.Is(err, reconcile.ErrPlanNotFound) {
					return a.fail(mode, 3, "not_found", "saved plan not found", nil)
				}
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.renderPlanExecutionResult(mode, result)
		},
	}

	cmd.Flags().StringVar(&progressMode, "progress", string(progress.ModeAuto), "Progress mode: auto, bar, plain, or off")
	return cmd
}

func (a *app) newPlanRetryCommand() *cobra.Command {
	var only string
	var progressMode string

	cmd := &cobra.Command{
		Use:   "retry [plan-id]",
		Short: "Retry saved ready plan items",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := outputMode(cmd)
			if only != "failed" && only != "uncertain" {
				return a.fail(mode, 2, "validation_error", "plan retry requires --only failed or --only uncertain", nil)
			}

			effective, store, cleanup, err := a.loadStore(mode, true, "plan retry")
			if err != nil {
				return err
			}
			defer cleanup()

			planID := ""
			if len(args) == 1 {
				planID = args[0]
			}

			reporter, err := a.progressReporter(progressMode)
			if err != nil {
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			result, err := reconcile.NewService(store).RetryPlan(effective, planID, only, reconcile.ApplyOptions{Reporter: reporter})
			if err != nil {
				if errors.Is(err, reconcile.ErrPlanNotFound) {
					return a.fail(mode, 3, "not_found", "saved plan not found", nil)
				}
				return a.fail(mode, 2, "validation_error", err.Error(), nil)
			}
			return a.renderPlanExecutionResult(mode, result)
		},
	}

	cmd.Flags().StringVar(&only, "only", "", "Retry scope: failed or uncertain")
	cmd.Flags().StringVar(&progressMode, "progress", string(progress.ModeAuto), "Progress mode: auto, bar, plain, or off")
	return cmd
}

func (a *app) bootstrapClockifyConfig(ctx context.Context) (*config.ClockifyConfig, error) {
	return a.bootstrapClockifyConfigFromEnv(ctx, "CLOCKIFY_API_KEY")
}

func (a *app) currentClockifyUserFromEnv(ctx context.Context, apiKeyEnv string) (*clockifyadapter.User, error) {
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return nil, nil
	}

	client := clockifyadapter.NewClient(apiKey)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, nil
	}
	return &user, nil
}

func (a *app) bootstrapClockifyConfigFromEnv(ctx context.Context, apiKeyEnv string) (*config.ClockifyConfig, error) {
	user, err := a.currentClockifyUserFromEnv(ctx, apiKeyEnv)
	if err != nil || user == nil {
		return nil, nil
	}

	workspaceID := user.ActiveWorkspace
	if workspaceID == "" {
		workspaceID = user.DefaultWorkspace
	}
	if workspaceID == "" || user.ID == "" {
		return nil, nil
	}

	return &config.ClockifyConfig{
		WorkspaceID: workspaceID,
		UserID:      user.ID,
		Auth:        config.ClockifyAuthConfig{APIKeyEnv: apiKeyEnv},
	}, nil
}

func resolveJiraDataInstanceName(effective config.EffectiveConfig, name string) (string, error) {
	resolved, _, err := config.ResolveJiraDataInstance(effective, name)
	return resolved, err
}

func resolveJiraCloudInstanceName(effective config.EffectiveConfig, name string) (string, error) {
	resolved, _, err := config.ResolveJiraCloudInstance(effective, name)
	return resolved, err
}

func resolveTotalsRouteProfileAdapter(effective config.EffectiveConfig, adapter, instance string) (string, error) {
	switch adapter {
	case "jira-cloud", "jira-data-center":
		return adapter, nil
	case "":
	case "clockify":
		return "", errors.New("--route-profile is supported only for jira-cloud and jira-data-center totals")
	default:
		return "", errors.New("--route-profile is supported only for jira-cloud and jira-data-center totals")
	}

	if strings.TrimSpace(instance) == "" {
		return "", errors.New("--route-profile requires --adapter jira-cloud or jira-data-center, or --instance <name> for a configured Jira target")
	}

	hasJiraCloud := effective.File.JiraCloud != nil
	if hasJiraCloud {
		_, hasJiraCloud = effective.File.JiraCloud.Instances[instance]
	}
	hasJiraData := effective.File.JiraData != nil
	if hasJiraData {
		_, hasJiraData = effective.File.JiraData.Instances[instance]
	}

	switch {
	case hasJiraCloud && hasJiraData:
		return "", fmt.Errorf("instance %q exists in both jira_cloud and jira_data_center; use --adapter jira-cloud or --adapter jira-data-center", instance)
	case hasJiraCloud:
		return "jira-cloud", nil
	case hasJiraData:
		return "jira-data-center", nil
	case instance == config.ClockifyInstanceName:
		return "", errors.New("--route-profile is supported only for jira-cloud and jira-data-center totals")
	default:
		return "", fmt.Errorf("adapter instance %q is not configured", instance)
	}
}

func sortedSetKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func validateReconcileScope(effective config.EffectiveConfig, adapters []string, instances []string) ([]string, []string, error) {
	if len(adapters) == 0 && len(instances) == 0 {
		return nil, nil, errors.New("at least one --adapter or --instance is required")
	}
	adapterSet := map[string]struct{}{}
	for _, adapter := range adapters {
		switch adapter {
		case "clockify", "jira-cloud", "jira-data-center":
			adapterSet[adapter] = struct{}{}
		default:
			return nil, nil, errors.New("supported adapters are clockify, jira-cloud, and jira-data-center")
		}
	}
	instanceOwners := map[string]string{}
	if effective.File.JiraCloud != nil {
		for name := range effective.File.JiraCloud.Instances {
			instanceOwners[name] = "jira-cloud"
		}
	}
	if effective.File.JiraData != nil {
		for name := range effective.File.JiraData.Instances {
			instanceOwners[name] = "jira-data-center"
		}
	}
	instanceSet := map[string]struct{}{}
	for _, instance := range instances {
		owner, ok := instanceOwners[instance]
		if !ok && instance == config.ClockifyInstanceName {
			if _, _, err := config.ResolveClockifyInstance(effective, instance); err != nil {
				return nil, nil, err
			}
			owner = "clockify"
			ok = true
		}
		if !ok {
			return nil, nil, fmt.Errorf("adapter instance %q is not configured", instance)
		}
		adapterSet[owner] = struct{}{}
		instanceSet[instance] = struct{}{}
	}
	selectedAdapters := sortedSetKeys(adapterSet)
	selectedInstances := sortedSetKeys(instanceSet)
	if _, ok := adapterSet["clockify"]; ok {
		if _, err := config.ResolveClockifyConfig(effective); err != nil {
			return nil, nil, err
		}
	}
	if _, ok := adapterSet["jira-cloud"]; ok && effective.File.JiraCloud == nil {
		return nil, nil, errors.New("jira_cloud config is required")
	}
	if _, ok := adapterSet["jira-data-center"]; ok && effective.File.JiraData == nil {
		return nil, nil, errors.New("jira_data_center config is required")
	}
	return selectedAdapters, selectedInstances, nil
}

func (a *app) loadJiraDataTotalsRows(ctx context.Context, effective config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, issuePrefixes []string, excludedIssues []string) ([]reconcilemodel.Row, error) {
	_, instance, err := config.ResolveJiraDataInstance(effective, instanceName)
	if err != nil {
		return nil, err
	}
	client := jiradcadapter.NewClient(instance.BaseURL, instance.Auth.Bearer.Token)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}

	jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", windowFrom.Format("2006-01-02"), windowTo.Format("2006-01-02"))
	issues, err := client.SearchIssues(ctx, jql, nil)
	if err != nil {
		return nil, err
	}

	excluded := toExactSet(excludedIssues)
	rows := make([]reconcilemodel.Row, 0)
	for _, issue := range issues {
		if _, skip := excluded[issue.Key]; skip {
			continue
		}
		worklogItems, err := client.ListIssueWorklogs(ctx, issue.Key)
		if err != nil {
			return nil, err
		}
		valid, _ := jiradcadapter.NormalizeIssueWorklogs(issue.Key, worklogItems, user, windowFrom, windowTo)
		for _, row := range valid {
			if matchesAnyIssuePrefix(row.IssueKey, issuePrefixes) {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func (a *app) loadJiraCloudTotalsRows(ctx context.Context, effective config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, issuePrefixes []string, excludedIssues []string) ([]reconcilemodel.Row, error) {
	_, instance, err := config.ResolveJiraCloudInstance(effective, instanceName)
	if err != nil {
		return nil, err
	}
	client := jiracloudadapter.NewClient(instance.BaseURL, instance.Auth.Email, instance.Auth.Token)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}

	jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", windowFrom.Format("2006-01-02"), windowTo.Format("2006-01-02"))
	issues, err := client.SearchIssues(ctx, jql, nil)
	if err != nil {
		return nil, err
	}

	excluded := toExactSet(excludedIssues)
	rows := make([]reconcilemodel.Row, 0)
	for _, issue := range issues {
		issueKey, issueRef, err := resolveJiraCloudTotalsIssueReference(ctx, client, issue)
		if err != nil {
			return nil, err
		}
		if _, skip := excluded[issueKey]; skip {
			continue
		}
		worklogItems, err := client.ListIssueWorklogs(ctx, issueRef)
		if err != nil {
			return nil, err
		}
		valid, _ := jiracloudadapter.NormalizeIssueWorklogs(issueKey, worklogItems, user, windowFrom, windowTo)
		for _, row := range valid {
			if matchesAnyIssuePrefix(row.IssueKey, issuePrefixes) {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func toExactSet(values []string) map[string]struct{} {
	items := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		items[value] = struct{}{}
	}
	return items
}

func resolveJiraCloudTotalsIssueReference(ctx context.Context, client *jiracloudadapter.Client, issue jiracloudadapter.IssueBrief) (issueKey string, issueRef string, err error) {
	if issue.Key != "" {
		return issue.Key, issue.Key, nil
	}
	if issue.ID == "" {
		return "", "", errors.New("jira cloud search result is missing both issue key and issue id")
	}
	resolved, err := client.GetIssue(ctx, issue.ID, nil)
	if err != nil {
		return "", "", err
	}
	if resolved.Key == "" {
		return "", "", fmt.Errorf("jira cloud issue lookup returned no key for issue id %s", issue.ID)
	}
	return resolved.Key, issue.ID, nil
}

func matchesAnyIssuePrefix(issueKey string, issuePrefixes []string) bool {
	for _, prefix := range issuePrefixes {
		if strings.HasPrefix(issueKey, prefix) {
			return true
		}
	}
	return false
}

func parsePlanWindow(cfg config.EffectiveConfig, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth bool, from, to string, weekOffset int, weekOffsetSet bool) (time.Time, time.Time, error) {
	return parsePlanWindowAt(cfg, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth, from, to, weekOffset, weekOffsetSet, time.Now)
}

func hasPlanWindowSelection(today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth bool, from, to string) bool {
	return today || yesterday || monday || tuesday || wednesday || thursday || friday || saturday || sunday || currentWeek || lastWeek || currentMonth || lastMonth || from != "" || to != ""
}

func parsePlanWindowAt(cfg config.EffectiveConfig, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth bool, from, to string, weekOffset int, weekOffsetSet bool, now func() time.Time) (time.Time, time.Time, error) {
	windowStart, windowEnd, err := worklogs.ResolveDateWindowSelectionAt(cfg, worklogs.DateWindowSelection{
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
		WeekOffsetSet: weekOffsetSet,
	}, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if windowStart == nil || windowEnd == nil {
		return time.Time{}, time.Time{}, errors.New("either --from and --to or exactly one of --today/--yesterday/--mon/--tue/--wed/--thu/--fri/--sat/--sun/--current-week/--last-week/--current-month/--last-month is required")
	}
	return windowStart.UTC(), windowEnd.UTC(), nil
}

func filterPlanListEntriesByCreatedAt(items []reconcile.ListEntry, windowFrom, windowTo time.Time) []reconcile.ListEntry {
	filtered := make([]reconcile.ListEntry, 0, len(items))
	for _, item := range items {
		if item.CreatedAt.Before(windowFrom) || item.CreatedAt.After(windowTo) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterPlanItemsByStatus(items []reconcile.PlanItem, status string) []reconcile.PlanItem {
	filtered := make([]reconcile.PlanItem, 0, len(items))
	for _, item := range items {
		if item.PlanStatus != status {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func renderPlanShowTable(w io.Writer, location *time.Location, items []reconcile.PlanItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join([]string{"TARGET", "ISSUE", "WINDOW", "STATUS", "ACTION", "COMPARE", "LOCAL", "REMOTE", "MATCH", "CREATE", "DELETE", "EXECUTION"}, "\t"))
	for _, item := range items {
		_, _ = fmt.Fprintln(tw, strings.Join([]string{
			planShowTargetValue(item),
			item.TargetIssue,
			formatSavedPlanWindow(item.WindowFromUTC, item.WindowToUTC, location),
			item.PlanStatus,
			item.PlannedAction,
			item.ComparisonStatus,
			fmt.Sprint(item.LocalRowCount),
			fmt.Sprint(item.RemoteRowCount),
			planShowDiffMetricValue(item, item.InspectionSummary.MatchedRowCount),
			planShowDiffMetricValue(item, item.InspectionSummary.CreateRowCount),
			planShowDiffMetricValue(item, item.InspectionSummary.DeleteRowCount),
			item.ExecutionState,
		}, "\t"))
	}
	return tw.Flush()
}

func planShowTargetValue(item reconcile.PlanItem) string {
	if item.TargetAdapterInstance == "" {
		return item.TargetAdapterFamily
	}
	return item.TargetAdapterFamily + "/" + item.TargetAdapterInstance
}

func planShowDiffMetricValue(item reconcile.PlanItem, value int) string {
	if !planShowHasDiffCounts(item) {
		return "-"
	}
	if item.ComparisonStatus == "not_checked" || item.ComparisonStatus == "check_failed" {
		return "-"
	}
	return fmt.Sprint(value)
}

func planShowHasDiffCounts(item reconcile.PlanItem) bool {
	matched := item.InspectionSummary.MatchedRowCount
	create := item.InspectionSummary.CreateRowCount
	deleteCount := item.InspectionSummary.DeleteRowCount
	switch item.PlanDirection {
	case "pull":
		return matched+create == item.RemoteRowCount && matched+deleteCount == item.LocalRowCount
	default:
		return matched+create == item.LocalRowCount && matched+deleteCount == item.RemoteRowCount
	}
}

func planJSON(plan reconcile.Plan) map[string]any {
	items := make([]map[string]any, 0, len(plan.Items))
	for _, item := range plan.Items {
		payload := make([]map[string]any, 0, len(item.Payload))
		for _, row := range item.Payload {
			payload = append(payload, map[string]any{
				"issue_key":        row.IssueKey,
				"started_at_utc":   row.StartedAtUTC.Format(time.RFC3339),
				"duration_seconds": row.DurationSeconds,
				"description":      row.Description,
				"source_row_id":    emptyToNil(row.SourceRowID),
			})
		}
		items = append(items, map[string]any{
			"id":                      item.ID,
			"issue_key":               item.IssueKey,
			"plan_direction":          item.PlanDirection,
			"target_adapter_family":   item.TargetAdapterFamily,
			"target_adapter_instance": emptyToNil(item.TargetAdapterInstance),
			"target_issue":            item.TargetIssue,
			"plan_status":             item.PlanStatus,
			"planned_action":          item.PlannedAction,
			"comparison_status":       item.ComparisonStatus,
			"reason_code":             item.ReasonCode,
			"reason_detail":           item.ReasonDetail,
			"local_row_count":         item.LocalRowCount,
			"local_total":             item.LocalTotal,
			"remote_row_count":        item.RemoteRowCount,
			"remote_total":            item.RemoteTotal,
			"inspection_summary":      item.InspectionSummary,
			"delivery_key":            item.DeliveryKey,
			"applied_state":           item.AppliedState,
			"execution_state":         item.ExecutionState,
			"apply_message":           emptyToNil(item.ApplyMessage),
			"payload":                 payload,
		})
	}

	findings := make([]map[string]any, 0, len(plan.Findings))
	for _, finding := range plan.Findings {
		findings = append(findings, map[string]any{
			"id":            finding.ID,
			"source_row_id": finding.SourceRowID,
			"reason_code":   finding.ReasonCode,
			"reason_detail": finding.ReasonDetail,
			"payload":       finding.Payload,
		})
	}

	return map[string]any{
		"plan_id":            plan.ID,
		"plan_direction":     plan.Direction,
		"adapter_family":     plan.AdapterFamily,
		"adapter_families":   plan.AdapterFamilies,
		"target_instances":   plan.TargetInstances,
		"config_fingerprint": plan.ConfigFingerprint,
		"window_from_utc":    plan.WindowFromUTC.Format(time.RFC3339),
		"window_to_utc":      plan.WindowToUTC.Format(time.RFC3339),
		"created_at":         plan.CreatedAt.Format(time.RFC3339),
		"aggregate_status":   plan.AggregateStatus,
		"applied_at":         plan.AppliedAt,
		"summary":            map[string]any{"total_items": len(plan.Items), "ready_items": countPlanItemsByStatus(plan.Items, "ready"), "skipped_items": countPlanItemsByStatus(plan.Items, "skipped"), "invalid_findings": len(plan.Findings)},
		"items":              items,
		"findings":           findings,
	}
}

func planListJSON(items []reconcile.ListEntry) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"plan_id":          item.ID,
			"plan_direction":   item.Direction,
			"adapter_family":   item.AdapterFamily,
			"adapter_families": item.AdapterFamilies,
			"target_instances": item.TargetInstances,
			"created_at":       item.CreatedAt.Format(time.RFC3339),
			"aggregate_status": item.AggregateStatus,
			"total_items":      item.TotalItems,
			"ready_items":      item.ReadyItems,
			"succeeded_items":  item.SucceededItems,
		})
	}
	return rows
}

func reconcileNoPlanJSON(result reconcile.ReconcileNoPlanResult) map[string]any {
	return map[string]any{
		"plan_created":              false,
		"reason":                    result.Reason,
		"adapter_family":            result.AdapterFamily,
		"adapter_families":          result.AdapterFamilies,
		"route_profile":             emptyToNil(result.RouteProfile),
		"window_from_utc":           result.WindowFromUTC.Format(time.RFC3339),
		"window_to_utc":             result.WindowToUTC.Format(time.RFC3339),
		"resolved_target_instances": result.ResolvedTargetInstances,
		"matched_scope_count":       result.MatchedScopeCount,
		"actionable_scope_count":    result.ActionableScopeCount,
	}
}

func renderReconcileNoPlanTable(w io.Writer, result reconcile.ReconcileNoPlanResult, location *time.Location) error {
	return renderTable(w,
		[]string{"PLAN_CREATED", "REASON", "ADAPTERS", "ROUTE_PROFILE", "WINDOW", "RESOLVED_TARGET_INSTANCES", "MATCHED_SCOPES", "ACTIONABLE_SCOPES"},
		[][]string{{
			"false",
			result.Reason,
			joinOrDash(result.AdapterFamilies),
			displayOrDash(result.RouteProfile),
			formatSavedPlanWindow(result.WindowFromUTC, result.WindowToUTC, location),
			joinOrDash(result.ResolvedTargetInstances),
			fmt.Sprint(result.MatchedScopeCount),
			fmt.Sprint(result.ActionableScopeCount),
		}},
	)
}

func renderReconcilePlanNextSteps(w io.Writer, plan reconcile.Plan) error {
	if countPlanItemsByStatus(plan.Items, "ready") == 0 {
		_, err := fmt.Fprintf(w, "\nNext:\n  workledger plan show %s --all\n", plan.ID)
		return err
	}
	_, err := fmt.Fprintf(w, "\nNext:\n  workledger plan show %s\n", plan.ID)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "  workledger plan apply %s\n", plan.ID)
	return err
}

func formatSavedPlanWindow(fromUTC, toUTC time.Time, location *time.Location) string {
	return fmt.Sprintf("%s - %s", fromUTC.In(location).Format("2006-01-02"), toUTC.In(location).Format("2006-01-02"))
}

func countPlanItemsByStatus(items []reconcile.PlanItem, status string) int {
	count := 0
	for _, item := range items {
		if item.PlanStatus == status {
			count++
		}
	}
	return count
}

func hasPlanItemsWithStatus(items []reconcile.PlanItem, status string) bool {
	for _, item := range items {
		if item.PlanStatus == status {
			return true
		}
	}
	return false
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func displayOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (a *app) loadStore(mode string, requireWrite bool, operation string) (config.EffectiveConfig, *sqlitestore.Store, func(), error) {
	effective, err := config.LoadEffective()
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			return config.EffectiveConfig{}, nil, nil, a.fail(mode, 2, "validation_error", "config file does not exist; run workledger init", nil)
		}
		var validationErr config.ValidationErrors
		if errors.As(err, &validationErr) {
			return config.EffectiveConfig{}, nil, nil, a.fail(mode, 2, "validation_error", "config validation failed", validationErr.Issues)
		}
		return config.EffectiveConfig{}, nil, nil, a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}

	if requireWrite {
		if err := checkLocalStorageWritable(effective.SQLitePath, operation); err != nil {
			var storageErr *localStorageError
			if errors.As(err, &storageErr) {
				return config.EffectiveConfig{}, nil, nil, a.failLocalStorageNotWritable(mode, storageErr)
			}
			return config.EffectiveConfig{}, nil, nil, a.fail(mode, 1, "unexpected_error", err.Error(), nil)
		}
	}

	store, err := sqlitestore.OpenExisting(effective.SQLitePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.EffectiveConfig{}, nil, nil, a.failSQLiteStoreNotReady(mode)
		}
		var storeErr *sqlitestore.OpenExistingError
		if errors.As(err, &storeErr) {
			switch storeErr.Kind {
			case sqlitestore.OpenExistingErrorSchemaMismatch:
				return config.EffectiveConfig{}, nil, nil, a.failSQLiteStoreSchemaMismatch(mode)
			case sqlitestore.OpenExistingErrorCorrupt, sqlitestore.OpenExistingErrorIncompatible:
				return config.EffectiveConfig{}, nil, nil, a.failUnrecoverableSQLite(mode, storeErr.Path)
			}
		}
		return config.EffectiveConfig{}, nil, nil, a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}

	return effective, store, func() { _ = store.Close() }, nil
}

func (a *app) loadService(mode string, requireWrite bool, operation string) (config.EffectiveConfig, *worklogs.Service, func(), error) {
	effective, store, cleanup, err := a.loadStore(mode, requireWrite, operation)
	if err != nil {
		return config.EffectiveConfig{}, nil, nil, err
	}
	return effective, worklogs.NewService(store), cleanup, nil
}

func (a *app) handleWorklogError(mode string, cfg config.EffectiveConfig, err error) error {
	switch {
	case errors.Is(err, worklogs.ErrNotFound):
		return a.fail(mode, 3, "not_found", "worklog not found", nil)
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

func (a *app) handleIssueMetadataError(mode string, err error) error {
	switch {
	case errors.Is(err, worklogs.ErrIssueMetadataNotFound):
		return a.fail(mode, 3, "not_found", "issue metadata not found", nil)
	case errors.Is(err, worklogs.ErrValidation):
		var validationErr worklogs.ValidationError
		if errors.As(err, &validationErr) {
			return a.fail(mode, 2, "validation_error", err.Error(), validationErr.Issues)
		}
		return a.fail(mode, 2, "validation_error", err.Error(), nil)
	default:
		return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
	}
}

func (a *app) handleClockifyError(mode string, err error) error {
	var requestErr *clockifyadapter.RequestError
	if errors.As(err, &requestErr) {
		if requestErr.StatusCode == 401 || requestErr.StatusCode == 403 {
			return a.fail(mode, 4, "auth_error", err.Error(), nil)
		}
		return a.fail(mode, 5, "external_error", err.Error(), nil)
	}
	return a.fail(mode, 5, "external_error", err.Error(), nil)
}

func (a *app) handleJiraDataError(mode string, err error) error {
	var requestErr *jiradcadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return a.fail(mode, 4, "auth_error", "jira data center authentication failed", nil)
		case http.StatusNotFound:
			return a.fail(mode, 3, "not_found", err.Error(), nil)
		default:
			return a.fail(mode, 5, "remote_error", requestErr.Error(), nil)
		}
	}
	return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
}

func (a *app) handleJiraCloudError(mode string, err error) error {
	var requestErr *jiracloudadapter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return a.fail(mode, 4, "auth_error", "jira cloud authentication failed", nil)
		case http.StatusNotFound:
			return a.fail(mode, 3, "not_found", "jira cloud resource not found", nil)
		default:
			return a.fail(mode, 5, "remote_error", requestErr.Error(), nil)
		}
	}
	return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
}

func (a *app) handleReconcileAdapterError(mode, adapter string, err error) error {
	var clockifyErr *clockifyadapter.RequestError
	if errors.As(err, &clockifyErr) {
		return a.handleClockifyError(mode, err)
	}
	var jiraDataErr *jiradcadapter.RequestError
	if errors.As(err, &jiraDataErr) {
		return a.handleJiraDataError(mode, err)
	}
	var jiraCloudErr *jiracloudadapter.RequestError
	if errors.As(err, &jiraCloudErr) {
		return a.handleJiraCloudError(mode, err)
	}
	_ = adapter
	return a.fail(mode, 2, "validation_error", err.Error(), nil)
}

func (a *app) renderTotalsJSON(adapter, instance, routeProfile string, today, yesterday, monday, tuesday, wednesday, thursday, friday, saturday, sunday, currentWeek, lastWeek, currentMonth, lastMonth bool, from, to string, weekOffset int, weekOffsetSet bool, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, result totals.Result) error {
	rawFilters := map[string]any{
		"adapter":       emptyToNil(adapter),
		"today":         today,
		"yesterday":     yesterday,
		"mon":           monday,
		"tue":           tuesday,
		"wed":           wednesday,
		"thu":           thursday,
		"fri":           friday,
		"sat":           saturday,
		"sun":           sunday,
		"current_week":  currentWeek,
		"last_week":     lastWeek,
		"current_month": currentMonth,
		"last_month":    lastMonth,
		"from":          emptyToNil(from),
		"to":            emptyToNil(to),
	}
	if weekOffsetSet {
		rawFilters["week_offset"] = weekOffset
	} else {
		rawFilters["week_offset"] = nil
	}
	effectiveFilters := map[string]any{
		"adapter":  emptyToNil(adapter),
		"from":     windowFromUTC.In(cfg.Location).Format(time.RFC3339),
		"to":       windowToUTC.In(cfg.Location).Format(time.RFC3339),
		"timezone": cfg.TimezoneName,
	}
	if adapter == "jira-data-center" || adapter == "jira-cloud" || adapter == "clockify" {
		rawFilters["instance"] = emptyToNil(instance)
		effectiveFilters["instance"] = instance
	}
	if routeProfile != "" {
		rawFilters["route_profile"] = routeProfile
		effectiveFilters["route_profile"] = routeProfile
	}

	days := make([]map[string]any, 0, len(result.Days))
	for _, day := range result.Days {
		days = append(days, map[string]any{
			"date":                 day.Date,
			"state":                day.State,
			"local_total_seconds":  day.LocalTotalSeconds,
			"remote_total_seconds": day.RemoteTotalSeconds,
			"delta_seconds":        day.DeltaSeconds,
		})
	}

	return a.writeJSON(map[string]any{
		"filters": map[string]any{
			"raw":       rawFilters,
			"effective": effectiveFilters,
		},
		"summary": totalsSummaryJSON(result),
		"days":    days,
	})
}

func (a *app) renderAllTotalsJSON(cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, items []totalsCollectionItem) error {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"adapter":  item.Adapter,
			"instance": emptyToNil(item.Instance),
			"from":     windowFromUTC.In(cfg.Location).Format(time.RFC3339),
			"to":       windowToUTC.In(cfg.Location).Format(time.RFC3339),
			"timezone": cfg.TimezoneName,
		}
		if item.Result != nil {
			entry["summary"] = totalsSummaryJSON(*item.Result)
			entry["days"] = totalsDaysJSON(item.Result.Days)
		} else {
			entry["status"] = item.Status
			entry["message"] = item.Message
		}
		rows = append(rows, entry)
	}
	return a.writeJSON(map[string]any{"items": rows})
}

func (a *app) renderTotalsTable(adapter, instance, routeProfile string, details bool, cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, result totals.Result) error {
	rows := make([][]string, 0, len(result.Days)+1)
	if details {
		for _, day := range result.Days {
			rows = append(rows, []string{
				day.Date,
				humanDuration(day.LocalTotalSeconds),
				humanDuration(day.RemoteTotalSeconds),
				signedHumanDuration(day.DeltaSeconds),
				day.State,
			})
		}
	}
	rows = append(rows, []string{
		"TOTAL",
		humanDuration(result.LocalTotalSeconds),
		humanDuration(result.RemoteTotalSeconds),
		signedHumanDuration(result.DeltaSeconds),
		result.State,
	})
	if err := renderTable(a.stdout, []string{"DATE", "LOCAL", "REMOTE", "DELTA", "STATE"}, rows); err != nil {
		return err
	}
	footer := fmt.Sprintf(
		"\nfrom=%s to=%s timezone=%s",
		windowFromUTC.In(cfg.Location).Format(time.RFC3339),
		windowToUTC.In(cfg.Location).Format(time.RFC3339),
		cfg.TimezoneName,
	)
	if adapter != "" {
		footer = fmt.Sprintf(
			"\nadapter=%s from=%s to=%s timezone=%s",
			adapter,
			windowFromUTC.In(cfg.Location).Format(time.RFC3339),
			windowToUTC.In(cfg.Location).Format(time.RFC3339),
			cfg.TimezoneName,
		)
		if adapter == "jira-data-center" || adapter == "jira-cloud" || adapter == "clockify" {
			footer += fmt.Sprintf(" instance=%s", instance)
		}
		if routeProfile != "" {
			footer += fmt.Sprintf(" route_profile=%s", routeProfile)
		}
	}
	footer += fmt.Sprintf(" state=%s\n", result.State)
	_, err := fmt.Fprint(a.stdout, footer)
	return err
}

func (a *app) renderAllTotalsTable(cfg config.EffectiveConfig, windowFromUTC, windowToUTC time.Time, items []totalsCollectionItem) error {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		instance := firstNonEmpty(item.Display, item.Instance)
		if item.Adapter == "clockify" {
			instance = item.Instance
		}
		localTotal := ""
		remoteTotal := ""
		delta := ""
		state := ""
		message := ""
		if item.Result != nil {
			localTotal = humanDuration(item.Result.LocalTotalSeconds)
			remoteTotal = humanDuration(item.Result.RemoteTotalSeconds)
			delta = signedHumanDuration(item.Result.DeltaSeconds)
			state = item.Result.State
		} else if item.LocalResult != nil {
			localTotal = humanDuration(item.LocalResult.LocalTotalSeconds)
			state = item.Status
		} else {
			state = item.Status
			message = singleLineTableCell(item.Message)
		}
		if item.Result == nil {
			message = singleLineTableCell(item.Message)
		}
		rows = append(rows, []string{
			item.Adapter,
			displayOrDash(instance),
			windowFromUTC.In(cfg.Location).Format(time.RFC3339),
			windowToUTC.In(cfg.Location).Format(time.RFC3339),
			cfg.TimezoneName,
			localTotal,
			remoteTotal,
			delta,
			state,
			message,
		})
	}
	return renderTable(a.stdout, []string{"ADAPTER", "INSTANCE", "FROM", "TO", "TIMEZONE", "LOCAL", "REMOTE", "DELTA", "STATE", "ERROR"}, rows)
}

func singleLineTableCell(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func totalsSummaryJSON(result totals.Result) map[string]any {
	return map[string]any{
		"state":                         result.State,
		"local_total_seconds":           result.LocalTotalSeconds,
		"remote_total_seconds":          result.RemoteTotalSeconds,
		"delta_seconds":                 result.DeltaSeconds,
		"running_remote_entry_detected": result.RunningRemoteEntryDetected,
	}
}

func totalsDaysJSON(days []totals.DayResult) []map[string]any {
	items := make([]map[string]any, 0, len(days))
	for _, day := range days {
		items = append(items, map[string]any{
			"date":                 day.Date,
			"state":                day.State,
			"local_total_seconds":  day.LocalTotalSeconds,
			"remote_total_seconds": day.RemoteTotalSeconds,
			"delta_seconds":        day.DeltaSeconds,
		})
	}
	return items
}

func (a *app) renderIssueMetadataListJSON(raw worklogs.ListFilters, effective worklogs.EffectiveFilters, items []worklogs.IssueMetadata, location *time.Location) error {
	payload := map[string]any{
		"filters": selectorFiltersJSON(raw, effective, location),
	}

	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, issueMetadataRecordJSON(item))
	}
	payload["items"] = records
	payload["total"] = len(records)
	return a.writeJSON(payload)
}

func (a *app) renderListJSON(cfg config.EffectiveConfig, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, active []worklogs.LocalWorklog, deleted []worklogs.Tombstone) error {
	payload := map[string]any{
		"filters": map[string]any{
			"raw": map[string]any{
				"issue":         emptyToNil(raw.Issue),
				"issue_prefix":  emptyToNil(raw.IssuePrefix),
				"today":         raw.Today,
				"yesterday":     raw.Yesterday,
				"mon":           raw.Monday,
				"tue":           raw.Tuesday,
				"wed":           raw.Wednesday,
				"thu":           raw.Thursday,
				"fri":           raw.Friday,
				"sat":           raw.Saturday,
				"sun":           raw.Sunday,
				"current_week":  raw.CurrentWeek,
				"last_week":     raw.LastWeek,
				"current_month": raw.CurrentMonth,
				"last_month":    raw.LastMonth,
				"from":          emptyToNil(raw.From),
				"to":            emptyToNil(raw.To),
				"week_offset":   rawWeekOffsetJSON(raw.WeekOffset, raw.WeekOffsetSet),
				"only_deleted":  raw.OnlyDeleted,
				"fields":        raw.Fields,
			},
			"effective": map[string]any{
				"only_deleted": effective.OnlyDeleted,
				"fields":       nilIfEmpty(effective.Fields),
				"timezone":     effective.Timezone,
			},
		},
	}
	if effective.IssueKey != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["issue_key"] = *effective.IssueKey
	}
	if effective.IssuePrefix != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["issue_prefix"] = *effective.IssuePrefix
	}
	if effective.From != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["from"] = effective.From.In(cfg.Location).Format(time.RFC3339)
	}
	if effective.To != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["to"] = effective.To.In(cfg.Location).Format(time.RFC3339)
	}
	if raw.OnlyDeleted {
		items := make([]map[string]any, 0, len(deleted))
		for _, item := range deleted {
			items = append(items, tombstoneRecordJSON(item, cfg.Location))
		}
		payload["items"] = items
		payload["total"] = len(items)
		return a.writeJSON(payload)
	}

	items := make([]map[string]any, 0, len(active))
	for _, item := range active {
		record := worklogRecordJSON(item, cfg.Location)
		if len(effective.Fields) > 0 {
			record = filterRecord(record, effective.Fields)
		}
		items = append(items, record)
	}
	payload["items"] = items
	payload["total"] = len(items)
	return a.writeJSON(payload)
}

func (a *app) renderSearchJSON(cfg config.EffectiveConfig, rawQuery string, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, normalizedQuery string, active []worklogs.LocalWorklog, deleted []worklogs.Tombstone) error {
	payload := map[string]any{
		"filters": map[string]any{
			"raw": map[string]any{
				"query":         rawQuery,
				"issue":         emptyToNil(raw.Issue),
				"issue_prefix":  emptyToNil(raw.IssuePrefix),
				"today":         raw.Today,
				"yesterday":     raw.Yesterday,
				"mon":           raw.Monday,
				"tue":           raw.Tuesday,
				"wed":           raw.Wednesday,
				"thu":           raw.Thursday,
				"fri":           raw.Friday,
				"sat":           raw.Saturday,
				"sun":           raw.Sunday,
				"current_week":  raw.CurrentWeek,
				"last_week":     raw.LastWeek,
				"current_month": raw.CurrentMonth,
				"last_month":    raw.LastMonth,
				"from":          emptyToNil(raw.From),
				"to":            emptyToNil(raw.To),
				"week_offset":   rawWeekOffsetJSON(raw.WeekOffset, raw.WeekOffsetSet),
				"only_deleted":  raw.OnlyDeleted,
				"fields":        raw.Fields,
			},
			"effective": map[string]any{
				"query":        normalizedQuery,
				"only_deleted": effective.OnlyDeleted,
				"fields":       nilIfEmpty(effective.Fields),
				"timezone":     effective.Timezone,
			},
		},
	}
	if effective.IssueKey != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["issue_key"] = *effective.IssueKey
	}
	if effective.IssuePrefix != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["issue_prefix"] = *effective.IssuePrefix
	}
	if effective.From != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["from"] = effective.From.In(cfg.Location).Format(time.RFC3339)
	}
	if effective.To != nil {
		payload["filters"].(map[string]any)["effective"].(map[string]any)["to"] = effective.To.In(cfg.Location).Format(time.RFC3339)
	}
	if raw.OnlyDeleted {
		items := make([]map[string]any, 0, len(deleted))
		for _, item := range deleted {
			items = append(items, tombstoneRecordJSON(item, cfg.Location))
		}
		payload["items"] = items
		payload["total"] = len(items)
		return a.writeJSON(payload)
	}

	items := make([]map[string]any, 0, len(active))
	for _, item := range active {
		record := worklogRecordJSON(item, cfg.Location)
		if len(effective.Fields) > 0 {
			record = filterRecord(record, effective.Fields)
		}
		items = append(items, record)
	}
	payload["items"] = items
	payload["total"] = len(items)
	return a.writeJSON(payload)
}

func (a *app) renderContextJSON(raw worklogs.ContextInput, result worklogs.ContextResult, location *time.Location) error {
	filters := map[string]any{
		"raw": map[string]any{
			"issue":         nilIfEmpty(raw.Issues),
			"today":         raw.Today,
			"yesterday":     raw.Yesterday,
			"mon":           raw.Monday,
			"tue":           raw.Tuesday,
			"wed":           raw.Wednesday,
			"thu":           raw.Thursday,
			"fri":           raw.Friday,
			"sat":           raw.Saturday,
			"sun":           raw.Sunday,
			"current_week":  raw.CurrentWeek,
			"last_week":     raw.LastWeek,
			"current_month": raw.CurrentMonth,
			"last_month":    raw.LastMonth,
			"from":          emptyToNil(raw.From),
			"to":            emptyToNil(raw.To),
			"week_offset":   rawWeekOffsetJSON(raw.WeekOffset, raw.WeekOffsetSet),
			"day_start":     emptyToNil(raw.DayStart),
			"day_end":       emptyToNil(raw.DayEnd),
			"lunch":         emptyToNil(raw.Lunch),
			"no_lunch":      raw.NoLunch,
		},
		"effective": map[string]any{
			"timezone": result.Filters.Timezone,
		},
	}
	if result.Filters.From != nil {
		filters["effective"].(map[string]any)["from"] = result.Filters.From.In(location).Format(time.RFC3339)
	}
	if result.Filters.To != nil {
		filters["effective"].(map[string]any)["to"] = result.Filters.To.In(location).Format(time.RFC3339)
	}

	planningIssues := make([]map[string]any, 0, len(result.Planning.Issues))
	for _, item := range result.Planning.Issues {
		planningIssues = append(planningIssues, map[string]any{
			"issue_key":            item.IssueKey,
			"max_estimate_seconds": int64PtrToAny(item.MaxEstimateSeconds),
		})
	}

	days := make([]map[string]any, 0, len(result.Days))
	for _, day := range result.Days {
		dayWorklogs := make([]map[string]any, 0, len(day.Worklogs))
		for _, worklog := range day.Worklogs {
			dayWorklogs = append(dayWorklogs, worklogRecordJSON(worklog, location))
		}
		freeSlots := make([]map[string]any, 0, len(day.FreeSlots))
		for _, slot := range day.FreeSlots {
			freeSlots = append(freeSlots, map[string]any{
				"start":            slot.Start.Format(time.RFC3339),
				"end":              slot.End.Format(time.RFC3339),
				"duration_seconds": slot.DurationSeconds,
			})
		}
		collisions := make([]map[string]any, 0, len(day.Collisions))
		for _, collision := range day.Collisions {
			collisions = append(collisions, map[string]any{
				"start":       collision.Start.Format(time.RFC3339),
				"end":         collision.End.Format(time.RFC3339),
				"worklog_ids": collision.WorklogIDs,
			})
		}
		days = append(days, map[string]any{
			"date":                day.Date,
			"worklogs":            dayWorklogs,
			"booked_seconds":      day.BookedSeconds,
			"until_quota_seconds": day.UntilQuotaSeconds,
			"free_slots":          freeSlots,
			"collisions":          collisions,
		})
	}

	settings := map[string]any{
		"timezone":                    result.Settings.Timezone,
		"day_start":                   result.Settings.DayStart,
		"day_end":                     result.Settings.DayEnd,
		"daily_minimum_quota_seconds": result.Settings.DailyMinimumQuotaSeconds,
		"lunch":                       nil,
	}
	if result.Settings.Lunch != nil {
		settings["lunch"] = map[string]any{
			"start": result.Settings.Lunch.Start,
			"end":   result.Settings.Lunch.End,
		}
	}

	return a.writeJSON(map[string]any{
		"filters":  filters,
		"settings": settings,
		"planning": map[string]any{
			"issue_order":              result.Planning.IssueOrder,
			"issues":                   planningIssues,
			"minimum_duration_seconds": result.Planning.MinimumDurationSeconds,
			"payload_contract":         result.Planning.PayloadContract,
			"slot_order":               result.Planning.SlotOrder,
		},
		"summary": map[string]any{
			"day_count":           result.Summary.DayCount,
			"worklog_count":       result.Summary.WorklogCount,
			"booked_seconds":      result.Summary.BookedSeconds,
			"until_quota_seconds": result.Summary.UntilQuotaSeconds,
			"collision_count":     result.Summary.CollisionCount,
		},
		"days": days,
	})
}

func (a *app) renderDeleteBatchJSON(raw worklogs.ListFilters, result worklogs.DeleteBatchResult, location *time.Location) error {
	filters := selectorFiltersJSON(raw, result.Filters, location)
	if result.DryRun {
		items := make([]map[string]any, 0, len(result.Items))
		for _, item := range result.Items {
			record := worklogRecordJSON(item, location)
			record["delete_preview"] = true
			items = append(items, record)
		}
		return a.writeJSON(map[string]any{
			"filters":     filters,
			"dry_run":     true,
			"hard_delete": result.HardDelete,
			"matched":     len(items),
			"items":       items,
		})
	}

	items := make([]map[string]any, 0, len(result.Deleted))
	for _, id := range result.Deleted {
		items = append(items, map[string]any{"id": id})
	}
	return a.writeJSON(map[string]any{
		"filters":     filters,
		"dry_run":     false,
		"hard_delete": result.HardDelete,
		"deleted":     len(result.Deleted),
		"items":       items,
	})
}

func (a *app) renderRestoreBatchJSON(raw worklogs.ListFilters, result worklogs.RestoreBatchResult, location *time.Location) error {
	filters := selectorFiltersJSON(raw, result.Filters, location)
	if result.DryRun {
		items := make([]map[string]any, 0, len(result.Items))
		for _, item := range result.Items {
			record := worklogRecordJSON(item.Record, location)
			record["deleted_at"] = item.Tombstone.DeletedAt.UTC().Format(time.RFC3339)
			record["restore_preview"] = true
			items = append(items, record)
		}
		return a.writeJSON(map[string]any{
			"filters": filters,
			"dry_run": true,
			"matched": len(items),
			"items":   items,
		})
	}

	items := make([]map[string]any, 0, len(result.Restored))
	for _, id := range result.Restored {
		items = append(items, map[string]any{"id": id})
	}
	return a.writeJSON(map[string]any{
		"filters":  filters,
		"dry_run":  false,
		"restored": len(result.Restored),
		"items":    items,
	})
}

func (a *app) renderTombstonesListJSON(cfg config.EffectiveConfig, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, items []worklogs.Tombstone) error {
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, tombstoneRecordJSON(item, cfg.Location))
	}
	return a.writeJSON(map[string]any{
		"filters": selectorFiltersJSON(raw, effective, cfg.Location),
		"items":   records,
		"total":   len(records),
	})
}

func (a *app) renderTombstonesSearchJSON(cfg config.EffectiveConfig, rawQuery string, raw worklogs.ListFilters, effective worklogs.EffectiveFilters, normalizedQuery string, items []worklogs.Tombstone) error {
	filters := selectorFiltersJSON(raw, effective, cfg.Location)
	filters["raw"].(map[string]any)["query"] = rawQuery
	filters["effective"].(map[string]any)["query"] = normalizedQuery
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		records = append(records, tombstoneRecordJSON(item, cfg.Location))
	}
	return a.writeJSON(map[string]any{
		"filters": filters,
		"items":   records,
		"total":   len(records),
	})
}

func (a *app) renderDeleteTombstoneBatchJSON(raw worklogs.ListFilters, result worklogs.DeleteTombstoneBatchResult, location *time.Location) error {
	filters := selectorFiltersJSON(raw, result.Filters, location)
	if result.DryRun {
		items := make([]map[string]any, 0, len(result.Items))
		for _, item := range result.Items {
			record := tombstoneRecordJSON(item, location)
			record["delete_preview"] = true
			items = append(items, record)
		}
		return a.writeJSON(map[string]any{
			"filters": filters,
			"dry_run": true,
			"matched": len(items),
			"items":   items,
		})
	}
	items := make([]map[string]any, 0, len(result.Deleted))
	for _, id := range result.Deleted {
		items = append(items, map[string]any{"id": id})
	}
	return a.writeJSON(map[string]any{
		"filters": filters,
		"dry_run": false,
		"deleted": len(result.Deleted),
		"items":   items,
	})
}

func selectorFiltersJSON(raw worklogs.ListFilters, effective worklogs.EffectiveFilters, location *time.Location) map[string]any {
	filters := map[string]any{
		"raw": map[string]any{
			"issue":         emptyToNil(raw.Issue),
			"issue_prefix":  emptyToNil(raw.IssuePrefix),
			"today":         raw.Today,
			"yesterday":     raw.Yesterday,
			"mon":           raw.Monday,
			"tue":           raw.Tuesday,
			"wed":           raw.Wednesday,
			"thu":           raw.Thursday,
			"fri":           raw.Friday,
			"sat":           raw.Saturday,
			"sun":           raw.Sunday,
			"current_week":  raw.CurrentWeek,
			"last_week":     raw.LastWeek,
			"current_month": raw.CurrentMonth,
			"last_month":    raw.LastMonth,
			"from":          emptyToNil(raw.From),
			"to":            emptyToNil(raw.To),
			"week_offset":   rawWeekOffsetJSON(raw.WeekOffset, raw.WeekOffsetSet),
		},
		"effective": map[string]any{},
	}
	if effective.IssueKey != nil {
		filters["effective"].(map[string]any)["issue_key"] = *effective.IssueKey
	}
	if effective.IssuePrefix != nil {
		filters["effective"].(map[string]any)["issue_prefix"] = *effective.IssuePrefix
	}
	if effective.From != nil {
		filters["effective"].(map[string]any)["from"] = effective.From.In(location).Format(time.RFC3339)
	}
	if effective.To != nil {
		filters["effective"].(map[string]any)["to"] = effective.To.In(location).Format(time.RFC3339)
	}
	filters["effective"].(map[string]any)["timezone"] = effective.Timezone
	return filters
}

func (a *app) renderShiftJSON(raw worklogs.ListFilters, result worklogs.ShiftResult, location *time.Location) error {
	filters := map[string]any{
		"raw": map[string]any{
			"issue":         emptyToNil(raw.Issue),
			"today":         raw.Today,
			"yesterday":     raw.Yesterday,
			"mon":           raw.Monday,
			"tue":           raw.Tuesday,
			"wed":           raw.Wednesday,
			"thu":           raw.Thursday,
			"fri":           raw.Friday,
			"sat":           raw.Saturday,
			"sun":           raw.Sunday,
			"current_week":  raw.CurrentWeek,
			"last_week":     raw.LastWeek,
			"current_month": raw.CurrentMonth,
			"last_month":    raw.LastMonth,
			"from":          emptyToNil(raw.From),
			"to":            emptyToNil(raw.To),
			"week_offset":   rawWeekOffsetJSON(raw.WeekOffset, raw.WeekOffsetSet),
		},
		"effective": map[string]any{
			"timezone": result.Filters.Timezone,
		},
	}
	if result.Filters.IssueKey != nil {
		filters["effective"].(map[string]any)["issue_key"] = *result.Filters.IssueKey
	}
	if result.Filters.From != nil {
		filters["effective"].(map[string]any)["from"] = result.Filters.From.In(location).Format(time.RFC3339)
	}
	if result.Filters.To != nil {
		filters["effective"].(map[string]any)["to"] = result.Filters.To.In(location).Format(time.RFC3339)
	}

	payload := map[string]any{
		"filters":       filters,
		"dry_run":       result.DryRun,
		"delta_seconds": result.DeltaSeconds,
		"matched":       result.Matched,
	}
	if result.DryRun {
		items := make([]map[string]any, 0, len(result.PreviewItems))
		for _, item := range result.PreviewItems {
			items = append(items, map[string]any{
				"id":                    item.ID,
				"issue_key":             item.IssueKey,
				"started_at_before":     item.StartedAtBefore.Format(time.RFC3339),
				"started_at_after":      item.StartedAtAfter.Format(time.RFC3339),
				"started_at_utc_before": item.StartedAtUTCBefore.Format(time.RFC3339),
				"started_at_utc_after":  item.StartedAtUTCAfter.Format(time.RFC3339),
				"duration_seconds":      item.DurationSeconds,
				"description":           item.Description,
			})
		}
		payload["items"] = items
		return a.writeJSON(payload)
	}

	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, worklogRecordJSON(item, location))
	}
	payload["items"] = items
	return a.writeJSON(payload)
}

func (a *app) renderApplyJSON(result worklogs.ApplyResult, location *time.Location) error {
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		entry := map[string]any{
			"op":     item.Op,
			"index":  item.Index,
			"id":     nil,
			"record": worklogRecordJSON(item.Record, location),
		}
		if item.ID != nil {
			entry["id"] = *item.ID
		}
		items = append(items, entry)
	}

	return a.writeJSON(map[string]any{
		"dry_run": result.DryRun,
		"summary": map[string]any{
			"add_count": result.Adds,
		},
		"items": items,
	})
}

func (a *app) renderPlanExecutionResult(mode string, result reconcile.ApplyResult) error {
	if mode == "json" {
		payload := map[string]any{
			"plan_id":              result.PlanID,
			"applied_count":        result.AppliedCount,
			"skipped_count":        result.SkippedCount,
			"failed_count":         result.FailedCount,
			"mixed_result":         result.MixedResult,
			"noop":                 result.NoOp,
			"trash_archived_count": result.TrashArchivedCount,
		}
		if result.RetryScope != "" {
			payload["retry_scope"] = result.RetryScope
		}
		if len(result.ScopeResults) > 0 {
			scopeResults := make([]map[string]any, 0, len(result.ScopeResults))
			for _, item := range result.ScopeResults {
				scopeResults = append(scopeResults, map[string]any{
					"plan_item_id":         item.PlanItemID,
					"scope_label":          item.ScopeLabel,
					"plan_direction":       item.PlanDirection,
					"planned_action":       item.PlannedAction,
					"trash_archived_count": item.TrashArchivedCount,
					"warnings":             item.Warnings,
				})
			}
			payload["scope_results"] = scopeResults
		}
		if err := a.writeJSON(payload); err != nil {
			return err
		}
	} else {
		if result.RetryScope != "" {
			_, _ = fmt.Fprintf(a.stdout, "plan_id=%s retry_scope=%s applied=%d failed=%d skipped=%d mixed_result=%t noop=%t trashed=%d\n", result.PlanID, result.RetryScope, result.AppliedCount, result.FailedCount, result.SkippedCount, result.MixedResult, result.NoOp, result.TrashArchivedCount)
		} else {
			_, _ = fmt.Fprintf(a.stdout, "plan_id=%s applied=%d failed=%d skipped=%d mixed_result=%t noop=%t trashed=%d\n", result.PlanID, result.AppliedCount, result.FailedCount, result.SkippedCount, result.MixedResult, result.NoOp, result.TrashArchivedCount)
		}
		for _, item := range result.ScopeResults {
			for _, warning := range item.Warnings {
				_, _ = fmt.Fprintf(a.stdout, "scope=%s warning=%s\n", item.ScopeLabel, warning)
			}
		}
	}

	switch {
	case result.MixedResult:
		return exitError{code: 6}
	case result.FailedCount > 0:
		return exitError{code: 1}
	default:
		return nil
	}
}

func (a *app) fail(mode string, code int, errorCode, message string, details any) error {
	if mode == "json" {
		_ = a.writeJSON(map[string]any{
			"error": map[string]any{
				"code":    errorCode,
				"message": message,
				"details": detailsOrEmpty(details),
			},
		})
	} else {
		_, _ = fmt.Fprintln(a.stdout, message)
	}
	return exitError{code: code}
}

func (a *app) failSQLiteStoreNotReady(mode string) error {
	return a.fail(mode, 2, "sqlite_store_not_ready", "SQLite store is not ready; run workledger init.", nil)
}

func (a *app) failSQLiteStoreSchemaMismatch(mode string) error {
	return a.fail(mode, 2, "sqlite_store_schema_mismatch", "SQLite store schema is outdated or mismatched; run workledger init to repair it.", nil)
}

func (a *app) failUnrecoverableSQLite(mode, sqlitePath string) error {
	message := "Local SQLite store is corrupt or incompatible and cannot be repaired additively."
	next := "Next step: inspect, replace, or restore the SQLite file, then rerun workledger init."

	_, _ = fmt.Fprintf(a.stderr, "%s\nsqlite_path: %s\n%s\n", message, sqlitePath, next)
	if mode == "json" {
		_ = a.writeJSON(map[string]any{
			"reason":      "sqlite_unrecoverable",
			"message":     message,
			"sqlite_path": sqlitePath,
		})
	}
	return exitError{code: 1}
}

func (a *app) failLocalStorageNotWritable(mode string, storageErr *localStorageError) error {
	message := storageErr.Error()
	next := "Next step: run outside sandbox, move storage.sqlite_path to a writable location, or fix filesystem permissions."

	if mode == "json" {
		_ = a.writeJSON(map[string]any{
			"reason":      "local_storage_not_writable",
			"message":     message,
			"sqlite_path": storageErr.SQLitePath,
			"parent_dir":  storageErr.ParentDir,
			"operation":   storageErr.Operation,
		})
	} else {
		_, _ = fmt.Fprintf(a.stdout, "%s\n%s\n", message, next)
	}
	return exitError{code: 1}
}

func (a *app) writeJSON(value any) error {
	encoder := json.NewEncoder(a.stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func outputMode(cmd *cobra.Command) string {
	value, _ := cmd.Flags().GetString("output")
	return config.DetectOutputMode(value)
}

func (a *app) progressReporter(raw string) (progress.Reporter, error) {
	return progressReporterForMode(progress.Mode(raw), isTTYWriter(a.stderr), a.stderr)
}

func progressReporterForMode(mode progress.Mode, isTTY bool, w io.Writer) (progress.Reporter, error) {
	switch mode {
	case progress.ModeAuto:
		if isTTY {
			return progress.NewTTYReporter(w), nil
		}
		return progress.NewNoopReporter(), nil
	case progress.ModeBar:
		if isTTY {
			return progress.NewTTYReporter(w), nil
		}
		return progress.NewPlainReporter(w), nil
	case progress.ModePlain:
		return progress.NewPlainReporter(w), nil
	case progress.ModeOff:
		return progress.NewNoopReporter(), nil
	default:
		return nil, fmt.Errorf("progress must be one of auto, bar, plain, or off")
	}
}

type fdWriter interface {
	Fd() uintptr
}

func isTTYWriter(w io.Writer) bool {
	fd, ok := w.(fdWriter)
	if !ok {
		return false
	}
	return isatty.IsTerminal(fd.Fd()) || isatty.IsCygwinTerminal(fd.Fd())
}

func splitFields(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		fields = append(fields, strings.TrimSpace(part))
	}
	return fields
}

func hasSelector(raw worklogs.ListFilters) bool {
	return raw.Today || raw.Yesterday || raw.Monday || raw.Tuesday || raw.Wednesday || raw.Thursday || raw.Friday || raw.Saturday || raw.Sunday || raw.CurrentWeek || raw.LastWeek || raw.CurrentMonth || raw.LastMonth || raw.From != "" || raw.To != ""
}

func worklogRecordJSON(item worklogs.LocalWorklog, location *time.Location) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"issue_key":        item.IssueKey,
		"started_at":       item.StartedAtUTC.In(location).Format(time.RFC3339),
		"started_at_utc":   item.StartedAtUTC.UTC().Format(time.RFC3339),
		"duration_seconds": item.DurationSeconds,
		"description":      item.Description,
	}
}

func worklogTableRecord(item worklogs.LocalWorklog, location *time.Location) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"issue_key":        item.IssueKey,
		"started_at":       localizedWorklogWindow(item.StartedAtUTC, item.DurationSeconds, location),
		"started_at_utc":   item.StartedAtUTC.UTC().Format(time.RFC3339),
		"duration_seconds": tableDurationMinutes(item.DurationSeconds),
		"description":      item.Description,
	}
}

func localizedWorklogWindow(startedAtUTC time.Time, durationSeconds int, location *time.Location) string {
	start := startedAtUTC.In(location)
	end := start.Add(time.Duration(durationSeconds) * time.Second)
	return fmt.Sprintf("%s - %s - %s", start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"))
}

func tableDurationMinutes(durationSeconds int) string {
	sign := ""
	if durationSeconds < 0 {
		sign = "-"
		durationSeconds = -durationSeconds
	}

	minutes := durationSeconds / 60
	if durationSeconds%60 != 0 {
		minutes++
	}
	return fmt.Sprintf("%s%dm", sign, minutes)
}

func worklogPreviewJSON(item worklogs.LocalWorklog, location *time.Location) map[string]any {
	return map[string]any{
		"issue_key":        item.IssueKey,
		"started_at":       item.StartedAtUTC.In(location).Format(time.RFC3339),
		"started_at_utc":   item.StartedAtUTC.UTC().Format(time.RFC3339),
		"duration_seconds": item.DurationSeconds,
		"description":      item.Description,
	}
}

func worklogRecordsJSON(items []worklogs.LocalWorklog, location *time.Location, preview bool) []map[string]any {
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if preview {
			records = append(records, worklogPreviewJSON(item, location))
			continue
		}
		records = append(records, worklogRecordJSON(item, location))
	}
	return records
}

func writeAddWarnings(w io.Writer, warnings []worklogs.AddWarning) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(w, warning.Message)
	}
}

func tombstoneRecordJSON(item worklogs.Tombstone, location *time.Location) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"issue_key":        item.IssueKey,
		"started_at":       item.StartedAtUTC.In(location).Format(time.RFC3339),
		"started_at_utc":   item.StartedAtUTC.UTC().Format(time.RFC3339),
		"duration_seconds": item.DurationSeconds,
		"description":      item.Description,
		"deleted_at":       item.DeletedAt.UTC().Format(time.RFC3339),
	}
}

func filterRecord(record map[string]any, fields []string) map[string]any {
	filtered := make(map[string]any, len(fields))
	for _, field := range fields {
		filtered[field] = record[field]
	}
	return filtered
}

func issueMetadataRecordJSON(item worklogs.IssueMetadata) map[string]any {
	return map[string]any{
		"issue_key":               item.IssueKey,
		"max_estimate_seconds":    int64PtrToAny(item.MaxEstimateSeconds),
		"source_adapter_family":   item.SourceAdapterFamily,
		"source_adapter_instance": item.SourceAdapterInst,
		"refreshed_at":            item.RefreshedAt.UTC().Format(time.RFC3339),
	}
}

func activeRows(items []worklogs.LocalWorklog, location *time.Location, fields []string, descriptionMaxWidth int) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		record := worklogTableRecord(item, location)
		row := make([]string, 0, len(fields))
		for _, field := range fields {
			row = append(row, formatActiveRowValue(field, record[field], descriptionMaxWidth))
		}
		rows = append(rows, row)
	}
	return rows
}

func formatActiveRowValue(field string, value any, descriptionMaxWidth int) string {
	if value == nil {
		return ""
	}

	formatted := fmt.Sprint(value)
	if field == "description" && descriptionMaxWidth > 0 && len(formatted) > descriptionMaxWidth {
		return formatted[:descriptionMaxWidth-3] + "..."
	}

	return formatted
}

func shiftPreviewRows(items []worklogs.ShiftPreviewItem, location *time.Location) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID,
			item.IssueKey,
			localizedWorklogWindow(item.StartedAtUTCBefore, item.DurationSeconds, location),
			localizedWorklogWindow(item.StartedAtUTCAfter, item.DurationSeconds, location),
			tableDurationMinutes(item.DurationSeconds),
			item.Description,
		})
	}
	return rows
}

func deletedRows(items []worklogs.Tombstone) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.ID, item.IssueKey, item.DeletedAt.UTC().Format(time.RFC3339)})
	}
	return rows
}

func tombstoneFullRows(items []worklogs.Tombstone, location *time.Location) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID,
			item.IssueKey,
			localizedWorklogWindow(item.StartedAtUTC, item.DurationSeconds, location),
			tableDurationMinutes(item.DurationSeconds),
			item.Description,
			item.DeletedAt.UTC().Format(time.RFC3339),
		})
	}
	return rows
}

func deleteResultRows(items []worklogs.DeleteResult) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID,
			item.IssueKey,
			item.DeletedAt.UTC().Format(time.RFC3339),
			strconv.FormatBool(item.HardDelete),
		})
	}
	return rows
}

func restorePreviewRows(items []worklogs.RestorePreviewItem, location *time.Location) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		record := worklogTableRecord(item.Record, location)
		rows = append(rows, []string{
			record["id"].(string),
			record["issue_key"].(string),
			record["started_at"].(string),
			record["duration_seconds"].(string),
			record["description"].(string),
			item.Tombstone.DeletedAt.UTC().Format(time.RFC3339),
		})
	}
	return rows
}

func tableHeaders(fields []string) []string {
	headers := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "id":
			headers = append(headers, "ID")
		case "issue_key":
			headers = append(headers, "ISSUE")
		case "started_at":
			headers = append(headers, "WINDOW")
		case "started_at_utc":
			headers = append(headers, "STARTED_AT_UTC")
		case "duration_seconds":
			headers = append(headers, "DURATION")
		case "description":
			headers = append(headers, "DESCRIPTION")
		case "deleted_at":
			headers = append(headers, "DELETED")
		}
	}
	return headers
}

func renderTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func renderListTotalsFooter(w io.Writer, count int, durationSeconds int, label string) error {
	_, err := fmt.Fprintf(w, "\nTotals: %d %s, %s\n", count, label, humanDuration(durationSeconds))
	return err
}

func sumActiveDurationSeconds(items []worklogs.LocalWorklog) int {
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	return total
}

func sumDeletedDurationSeconds(items []worklogs.Tombstone) int {
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	return total
}

func humanDuration(durationSeconds int) string {
	if durationSeconds == 0 {
		return "0m"
	}

	remaining := durationSeconds
	parts := make([]string, 0, 3)

	hours := remaining / 3600
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		remaining %= 3600
	}

	minutes := remaining / 60
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
		remaining %= 60
	}

	if remaining > 0 {
		parts = append(parts, fmt.Sprintf("%ds", remaining))
	}

	return strings.Join(parts, " ")
}

func signedHumanDuration(durationSeconds int) string {
	if durationSeconds < 0 {
		return "-" + humanDuration(-durationSeconds)
	}
	return humanDuration(durationSeconds)
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func rawWeekOffsetJSON(value int, set bool) any {
	if !set {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nilIfEmpty(items []string) any {
	if len(items) == 0 {
		return nil
	}
	return items
}

func detailsOrEmpty(value any) any {
	if value == nil {
		return []any{}
	}
	return value
}

func issueKeys(items []worklogs.LocalWorklog) []string {
	keys := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.IssueKey]; ok {
			continue
		}
		seen[item.IssueKey] = struct{}{}
		keys = append(keys, item.IssueKey)
	}
	return keys
}

func filterIssueKeysByPrefixes(issueKeys, issuePrefixes []string) []string {
	if len(issuePrefixes) == 0 {
		return issueKeys
	}
	filtered := make([]string, 0, len(issueKeys))
	for _, issueKey := range issueKeys {
		if matchesAnyIssuePrefix(issueKey, issuePrefixes) {
			filtered = append(filtered, issueKey)
		}
	}
	return filtered
}

func issueMetadataRefreshRows(items []map[string]any) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		value := ""
		if item["max_estimate_seconds"] != nil {
			value = fmt.Sprint(item["max_estimate_seconds"])
		}
		rows = append(rows, []string{fmt.Sprint(item["issue_key"]), value})
	}
	return rows
}

func issueMetadataRows(items []worklogs.IssueMetadata) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		value := ""
		if item.MaxEstimateSeconds != nil {
			value = fmt.Sprint(*item.MaxEstimateSeconds)
		}
		rows = append(rows, []string{
			item.IssueKey,
			value,
			item.SourceAdapterFamily,
			item.SourceAdapterInst,
			item.RefreshedAt.UTC().Format(time.RFC3339),
		})
	}
	return rows
}

func int64PtrToAny(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func contextRows(result worklogs.ContextResult) [][]string {
	rows := make([][]string, 0, len(result.Days))
	for _, day := range result.Days {
		rows = append(rows, []string{
			day.Date,
			fmt.Sprint(len(day.Worklogs)),
			fmt.Sprint(day.BookedSeconds),
			fmt.Sprint(day.UntilQuotaSeconds),
			fmt.Sprint(len(day.Collisions)),
		})
	}
	return rows
}
