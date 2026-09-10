package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
)

type cliActivityRun struct {
	service *activity.Service
	store   *sqlitestore.Store
	entry   activity.Entry
}

func (a *app) newActivityCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "activity", Short: "Inspect diagnostic activity history", RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	cmd.AddCommand(a.newActivityListCommand())
	return cmd
}

func (a *app) newActivityListCommand() *cobra.Command {
	var limit int
	var source, state string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent CLI and TUI activity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := outputMode(cmd)
			if limit < 1 || limit > activity.RetentionLimit {
				return a.fail(mode, 2, "validation_error", fmt.Sprintf("activity limit must be between 1 and %d", activity.RetentionLimit), nil)
			}
			if source != "" && source != string(activity.SourceCLI) && source != string(activity.SourceTUI) {
				return a.fail(mode, 2, "validation_error", "activity source must be cli or tui", nil)
			}
			if state != "" && !isActivityState(state) {
				return a.fail(mode, 2, "validation_error", "activity state must be running, succeeded, failed, partial, or canceled", nil)
			}
			_, store, cleanup, err := a.loadStore(mode, false, "activity list")
			if err != nil {
				return err
			}
			defer cleanup()
			items, err := activity.NewService(store).List(cmd.Context(), activity.ListFilters{Limit: limit, Source: activity.Source(source), State: activity.State(state), ExcludeID: a.activeActivityID})
			if err != nil {
				return a.fail(mode, 1, "unexpected_error", err.Error(), nil)
			}
			if mode == "json" {
				return a.writeJSON(map[string]any{"items": items})
			}
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{item.StartedAt.Local().Format("2006-01-02 15:04:05"), string(item.Source), item.Operation, string(item.State), activityDuration(item), displayOrDash(item.Summary)})
			}
			return renderTable(a.stdout, []string{"STARTED", "SOURCE", "OPERATION", "STATE", "DURATION", "SUMMARY"}, rows)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to return (1-500)")
	cmd.Flags().StringVar(&source, "source", "", "Filter by source: cli or tui")
	cmd.Flags().StringVar(&state, "state", "", "Filter by state")
	return cmd
}

func (a *app) startCLIActivity(root *cobra.Command, args []string) *cliActivityRun {
	operation, ok := activityOperation(root, args)
	if !ok {
		return nil
	}
	startedAt := time.Now().UTC()
	summary := safeCLIActivitySummary(args)
	attributes := safeCLIActivityAttributes(args)
	service, store, ok := openActivityService()
	if !ok {
		return &cliActivityRun{entry: activity.Entry{Operation: operation, Summary: summary, Attributes: attributes, StartedAt: startedAt}}
	}
	entry, err := service.Start(context.Background(), activity.StartInput{Source: activity.SourceCLI, Operation: operation, Summary: summary, Attributes: attributes, StartedAt: startedAt})
	if err != nil {
		_ = store.Close()
		return &cliActivityRun{entry: activity.Entry{Operation: operation, Summary: summary, Attributes: attributes, StartedAt: startedAt}}
	}
	a.activeActivityID = entry.ID
	return &cliActivityRun{service: service, store: store, entry: entry}
}

func (a *app) finishCLIActivity(run *cliActivityRun, exitCode int) {
	if run == nil {
		return
	}
	if run.service == nil && run.entry.Operation == "init" {
		service, store, ok := openActivityService()
		if ok {
			entry, err := service.Start(context.Background(), activity.StartInput{Source: activity.SourceCLI, Operation: run.entry.Operation, Summary: run.entry.Summary, Attributes: run.entry.Attributes, StartedAt: run.entry.StartedAt})
			if err == nil {
				run.service, run.store, run.entry = service, store, entry
			} else {
				_ = store.Close()
			}
		}
	}
	if run.service == nil {
		return
	}
	defer run.store.Close()
	state := activity.StateSucceeded
	switch exitCode {
	case 0:
	case 6:
		state = activity.StatePartial
	case 130:
		state = activity.StateCanceled
	default:
		state = activity.StateFailed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = run.service.Finish(ctx, run.entry.ID, activity.FinishInput{State: state, ExitCode: &exitCode, ErrorCode: a.activityErrorCode, ErrorMessage: a.activityErrorMessage})
}

func openActivityService() (*activity.Service, *sqlitestore.Store, bool) {
	cfg, err := config.LoadEffective()
	if err != nil {
		return nil, nil, false
	}
	store, err := sqlitestore.OpenExisting(cfg.SQLitePath)
	if err != nil {
		return nil, nil, false
	}
	return activity.NewService(store), store, true
}

func activityOperation(root *cobra.Command, args []string) (string, bool) {
	if len(args) == 0 || hasHelpArg(args) || strings.HasPrefix(args[0], "__complete") {
		return "", false
	}
	if slicesContain(args, "--version") || slicesContain(args, "-v") {
		return "version", true
	}
	cmd, _, err := root.Find(args)
	if err != nil || cmd == root || !cmd.Runnable() || cmd.HasSubCommands() {
		return "", false
	}
	path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
	return strings.ReplaceAll(path, " ", "."), true
}

var safeValueActivityFlags = map[string]struct{}{
	"adapter": {}, "created-within": {}, "duration": {}, "field": {}, "from": {}, "instance": {}, "issue": {}, "issue-prefix": {}, "limit": {}, "only": {}, "route-profile": {}, "scope": {}, "source": {}, "state": {}, "to": {}, "week-offset": {},
}

var safeBoolActivityFlags = map[string]struct{}{
	"all": {}, "current-month": {}, "current-week": {}, "details": {}, "dry": {}, "fill": {}, "fit": {}, "force": {}, "fri": {}, "last-month": {}, "last-week": {}, "mon": {}, "no-lunch": {}, "overtime": {}, "pull": {}, "push": {}, "sat": {}, "sun": {}, "thu": {}, "today": {}, "tomorrow": {}, "tue": {}, "wed": {}, "yes": {}, "yesterday": {},
}

func safeCLIActivityAttributes(args []string) map[string]string {
	result := map[string]string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		nameValue := strings.TrimPrefix(arg, "--")
		name, value, hasValue := strings.Cut(nameValue, "=")
		if _, ok := safeBoolActivityFlags[name]; ok {
			result[name] = "true"
			continue
		}
		if _, ok := safeValueActivityFlags[name]; !ok {
			continue
		}
		if !hasValue && index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			index++
			value = args[index]
		}
		if value != "" {
			if previous := result[name]; previous != "" {
				result[name] = previous + "," + value
			} else {
				result[name] = value
			}
		}
	}
	return result
}

func safeCLIActivitySummary(args []string) string {
	attributes := safeCLIActivityAttributes(args)
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+attributes[key])
	}
	return strings.Join(parts, " ")
}

func safeActivityErrorMessage(code string) string {
	switch code {
	case "validation_error":
		return "validation failed"
	case "not_found":
		return "requested item was not found"
	case "authentication_error":
		return "authentication failed"
	case "external_error":
		return "external operation failed"
	case "conflict":
		return "operation conflicted with current state"
	case "":
		return ""
	default:
		return strings.ReplaceAll(code, "_", " ")
	}
}

func activityDuration(item activity.Entry) string {
	if item.DurationMS == nil {
		return "-"
	}
	return (time.Duration(*item.DurationMS) * time.Millisecond).Round(time.Millisecond).String()
}

func isActivityState(value string) bool {
	switch activity.State(value) {
	case activity.StateRunning, activity.StateSucceeded, activity.StateFailed, activity.StatePartial, activity.StateCanceled:
		return true
	default:
		return false
	}
}

func hasHelpArg(args []string) bool {
	return slicesContain(args, "--help") || slicesContain(args, "-h")
}

func slicesContain(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
