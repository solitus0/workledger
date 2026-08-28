package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/reconcile"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

const completionCandidateLimit = 100

type completionCandidate struct {
	value       string
	description string
}

func (a *app) newCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate a shell completion script",
		Long:  "Generate a Workledger completion script for Bash, Zsh, or Fish.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		ValidArgsFunction: cobra.NoFileCompletions,
	}

	cmd.AddCommand(a.newShellCompletionCommand("bash"))
	cmd.AddCommand(a.newShellCompletionCommand("zsh"))
	cmd.AddCommand(a.newShellCompletionCommand("fish"))
	return cmd
}

func (a *app) newShellCompletionCommand(shell string) *cobra.Command {
	return &cobra.Command{
		Use:   shell,
		Short: "Generate the completion script for " + shell,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()
			out := cmd.OutOrStdout()
			switch shell {
			case "bash":
				return root.GenBashCompletionV2(out, true)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			default:
				return fmt.Errorf("unsupported completion shell %q", shell)
			}
		},
		ValidArgsFunction: cobra.NoFileCompletions,
	}
}

func (a *app) configureCompletions(root *cobra.Command) {
	mustRegisterFlagCompletion(root, "output", fixedCompletion(
		completionCandidate{value: "table", description: "Human-readable table output"},
		completionCandidate{value: "json", description: "Machine-readable JSON output"},
	))

	visitCommands(root, func(cmd *cobra.Command) {
		if cmd.LocalNonPersistentFlags().Lookup("issue") != nil {
			mustRegisterFlagCompletion(cmd, "issue", a.completeIssueKeys)
		}
		if cmd.LocalNonPersistentFlags().Lookup("progress") != nil {
			mustRegisterFlagCompletion(cmd, "progress", fixedCompletion(
				completionCandidate{value: "auto", description: "Select progress mode automatically"},
				completionCandidate{value: "bar", description: "Interactive progress bar"},
				completionCandidate{value: "plain", description: "Plain progress events"},
				completionCandidate{value: "off", description: "Disable progress output"},
			))
		}
		if cmd.LocalNonPersistentFlags().Lookup("only") != nil {
			mustRegisterFlagCompletion(cmd, "only", fixedCompletion(
				completionCandidate{value: "failed", description: "Retry failed items"},
				completionCandidate{value: "uncertain", description: "Retry uncertain items"},
			))
		}

		switch cmd.CommandPath() {
		case "workledger worklogs update", "workledger worklogs delete":
			cmd.ValidArgsFunction = a.completeWorklogIDs
		case "workledger plan show", "workledger plan apply", "workledger plan retry":
			cmd.ValidArgsFunction = a.completePlanIDs
		case "workledger route explain":
			cmd.ValidArgsFunction = a.completeIssueKeyArgs
		case "workledger issue-metadata refresh":
			mustRegisterFlagCompletion(cmd, "adapter", fixedCompletion(
				completionCandidate{value: "jira-cloud", description: "Jira Cloud"},
				completionCandidate{value: "jira-data-center", description: "Jira Data Center"},
			))
			mustRegisterFlagCompletion(cmd, "field", fixedCompletion(
				completionCandidate{value: "max-estimate", description: "Original estimate in seconds"},
			))
			mustRegisterFlagCompletion(cmd, "instance", a.completeConfiguredInstances)
		case "workledger totals":
			mustRegisterFlagCompletion(cmd, "adapter", fixedCompletion(adapterCompletionCandidates()...))
			mustRegisterFlagCompletion(cmd, "instance", a.completeConfiguredInstances)
			mustRegisterFlagCompletion(cmd, "route-profile", a.completeRouteProfiles)
		case "workledger plan reconcile":
			mustRegisterFlagCompletion(cmd, "adapter", fixedCompletion(adapterCompletionCandidates()...))
			mustRegisterFlagCompletion(cmd, "instance", a.completeConfiguredInstances)
			mustRegisterFlagCompletion(cmd, "route-profile", a.completeRouteProfiles)
		}
	})
}

func visitCommands(command *cobra.Command, visit func(*cobra.Command)) {
	visit(command)
	for _, child := range command.Commands() {
		visitCommands(child, visit)
	}
}

func mustRegisterFlagCompletion(cmd *cobra.Command, name string, completion cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(name, completion); err != nil {
		panic(fmt.Sprintf("register completion for %s --%s: %v", cmd.CommandPath(), name, err))
	}
}

func fixedCompletion(candidates ...completionCandidate) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return renderCompletionCandidates(candidates, toComplete, selectedFlagValues(cmd)), cobra.ShellCompDirectiveNoFileComp
	}
}

func adapterCompletionCandidates() []completionCandidate {
	return []completionCandidate{
		{value: "clockify", description: "Clockify"},
		{value: "jira-cloud", description: "Jira Cloud"},
		{value: "jira-data-center", description: "Jira Data Center"},
	}
}

func selectedFlagValues(cmd *cobra.Command) map[string]struct{} {
	selected := map[string]struct{}{}
	flag := cmd.Flag("adapter")
	if flag == nil || !flag.Changed {
		return selected
	}
	if flag.Value.Type() == "stringArray" {
		values, _ := cmd.Flags().GetStringArray("adapter")
		for _, value := range values {
			selected[value] = struct{}{}
		}
	}
	return selected
}

func renderCompletionCandidates(candidates []completionCandidate, prefix string, excluded map[string]struct{}) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, skip := excluded[candidate.value]; skip {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(candidate.value), prefix) {
			continue
		}
		value := sanitizeCompletionText(candidate.value)
		description := sanitizeCompletionText(candidate.description)
		if value == "" {
			continue
		}
		if description == "" {
			items = append(items, value)
		} else {
			items = append(items, value+"\t"+description)
		}
	}
	return items
}

func sanitizeCompletionText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (a *app) completeWorklogIDs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	effective, service, cleanup, ok := completionWorklogService()
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer cleanup()

	items, err := service.ListActiveByIDPrefix(toComplete, completionCandidateLimit)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates := make([]completionCandidate, 0, len(items))
	for _, item := range items {
		description := fmt.Sprintf("%s · %s · %s", item.IssueKey, item.StartedAtUTC.In(effective.Location).Format("2006-01-02 15:04"), item.Description)
		candidates = append(candidates, completionCandidate{value: item.ID, description: description})
	}
	return renderCompletionCandidates(candidates, toComplete, nil), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completePlanIDs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	effective, service, cleanup, ok := completionPlanService()
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer cleanup()

	items, err := service.ListPlansByIDPrefix(toComplete, completionCandidateLimit)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates := make([]completionCandidate, 0, len(items))
	for _, item := range items {
		description := fmt.Sprintf("%s · %s · %s", item.Direction, item.AggregateStatus, item.CreatedAt.In(effective.Location).Format("2006-01-02 15:04"))
		candidates = append(candidates, completionCandidate{value: item.ID, description: description})
	}
	return renderCompletionCandidates(candidates, toComplete, nil), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeIssueKeys(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	_, service, cleanup, ok := completionWorklogService()
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer cleanup()

	items, err := service.ListKnownIssueKeys(toComplete, completionCandidateLimit)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates := make([]completionCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, completionCandidate{value: item, description: "Known local issue"})
	}
	return renderCompletionCandidates(candidates, toComplete, nil), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeIssueKeyArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.completeIssueKeys(cmd, args, toComplete)
}

func (a *app) completeConfiguredInstances(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	effective, err := config.LoadEffective()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	allowed := supportedAdapterFamilies(cmd)
	selected := selectedStringArrayFlagValues(cmd, "instance")
	byName := map[string][]string{}
	if _, ok := allowed["clockify"]; ok && effective.File.Clockify != nil {
		byName[config.ClockifyInstanceName] = append(byName[config.ClockifyInstanceName], "clockify")
	}
	if _, ok := allowed["jira-cloud"]; ok && effective.File.JiraCloud != nil {
		for name := range effective.File.JiraCloud.Instances {
			byName[name] = append(byName[name], "jira-cloud")
		}
	}
	if _, ok := allowed["jira-data-center"]; ok && effective.File.JiraData != nil {
		for name := range effective.File.JiraData.Instances {
			byName[name] = append(byName[name], "jira-data-center")
		}
	}
	return renderNamedScopes(byName, toComplete, selected), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeRouteProfiles(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	effective, err := config.LoadEffective()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	allowed := supportedAdapterFamilies(cmd)
	selectedInstances := selectedFlagValueSet(cmd, "instance")
	byName := map[string][]string{}
	if _, ok := allowed["jira-cloud"]; ok && effective.File.JiraCloud != nil {
		collectRouteProfiles(byName, "jira-cloud", effective.File.JiraCloud.Instances, selectedInstances)
	}
	if _, ok := allowed["jira-data-center"]; ok && effective.File.JiraData != nil {
		collectRouteProfiles(byName, "jira-data-center", effective.File.JiraData.Instances, selectedInstances)
	}
	return renderNamedScopes(byName, toComplete, nil), cobra.ShellCompDirectiveNoFileComp
}

func supportedAdapterFamilies(cmd *cobra.Command) map[string]struct{} {
	allowed := map[string]struct{}{}
	values := selectedFlagValueSet(cmd, "adapter")
	if len(values) > 0 {
		return values
	}
	switch cmd.CommandPath() {
	case "workledger issue-metadata refresh":
		allowed["jira-cloud"] = struct{}{}
		allowed["jira-data-center"] = struct{}{}
	default:
		allowed["clockify"] = struct{}{}
		allowed["jira-cloud"] = struct{}{}
		allowed["jira-data-center"] = struct{}{}
	}
	return allowed
}

func selectedFlagValueSet(cmd *cobra.Command, name string) map[string]struct{} {
	values := map[string]struct{}{}
	flag := cmd.Flag(name)
	if flag == nil || !flag.Changed {
		return values
	}
	if flag.Value.Type() == "stringArray" {
		items, _ := cmd.Flags().GetStringArray(name)
		for _, item := range items {
			values[item] = struct{}{}
		}
		return values
	}
	value, _ := cmd.Flags().GetString(name)
	if value != "" {
		values[value] = struct{}{}
	}
	return values
}

func selectedStringArrayFlagValues(cmd *cobra.Command, name string) map[string]struct{} {
	flag := cmd.Flag(name)
	if flag == nil || !flag.Changed || flag.Value.Type() != "stringArray" {
		return nil
	}
	return selectedFlagValueSet(cmd, name)
}

func renderNamedScopes(byName map[string][]string, prefix string, excluded map[string]struct{}) []string {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	candidates := make([]completionCandidate, 0, len(names))
	for _, name := range names {
		scopes := byName[name]
		sort.Strings(scopes)
		scopes = compactStrings(scopes)
		candidates = append(candidates, completionCandidate{value: name, description: strings.Join(scopes, ", ")})
	}
	return renderCompletionCandidates(candidates, prefix, excluded)
}

func compactStrings(items []string) []string {
	if len(items) < 2 {
		return items
	}
	result := items[:1]
	for _, item := range items[1:] {
		if item != result[len(result)-1] {
			result = append(result, item)
		}
	}
	return result
}

func collectRouteProfiles[T config.JiraCloudInstance | config.JiraDataCenterInstance](byName map[string][]string, family string, instances map[string]T, selected map[string]struct{}) {
	for name, raw := range instances {
		if len(selected) > 0 {
			if _, ok := selected[name]; !ok {
				continue
			}
		}
		var routes *config.JiraInstanceRoutes
		switch instance := any(raw).(type) {
		case config.JiraCloudInstance:
			routes = instance.Routing
		case config.JiraDataCenterInstance:
			routes = instance.Routing
		}
		if routes == nil {
			continue
		}
		for profile := range routes.Profiles {
			byName[profile] = append(byName[profile], family+"/"+name)
		}
	}
}

func completionWorklogService() (config.EffectiveConfig, *worklogs.Service, func(), bool) {
	effective, store, cleanup, ok := completionStore()
	if !ok {
		return config.EffectiveConfig{}, nil, nil, false
	}
	return effective, worklogs.NewService(store), cleanup, true
}

func completionPlanService() (config.EffectiveConfig, *reconcile.Service, func(), bool) {
	effective, store, cleanup, ok := completionStore()
	if !ok {
		return config.EffectiveConfig{}, nil, nil, false
	}
	return effective, reconcile.NewService(store), cleanup, true
}

func completionStore() (config.EffectiveConfig, *sqlitestore.Store, func(), bool) {
	effective, err := config.LoadEffective()
	if err != nil {
		return config.EffectiveConfig{}, nil, nil, false
	}
	store, err := sqlitestore.OpenExistingReadOnly(effective.SQLitePath)
	if err != nil {
		return config.EffectiveConfig{}, nil, nil, false
	}
	return effective, store, func() { _ = store.Close() }, true
}
