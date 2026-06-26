package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

func (s *Service) CreateJiraCloudPullPlan(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildJiraCloudPullPlan(ctx, cfg, instanceName, windowFrom, windowTo, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildJiraCloudPullPlan(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	instance, err := resolvePullJiraCloudInstance(cfg, instanceName)
	if err != nil {
		return Plan{}, err
	}
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	client := s.newJiraCloudClient(instance.cfg)
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-cloud pull"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-cloud pull complete"})
	}()
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return Plan{}, err
	}
	jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", windowFrom.Format("2006-01-02"), windowTo.Format("2006-01-02"))
	issues, err := client.SearchIssues(ctx, jql, nil)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "pull",
		AdapterFamily:     "jira-cloud",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}

	exclusions, err := config.JiraExcludedIssuesForInstance(cfg, "jira-cloud", instance.name)
	if err != nil {
		return Plan{}, err
	}
	excluded := map[string]struct{}{}
	for _, issue := range exclusions {
		excluded[issue] = struct{}{}
	}
	if instance.cfg.Routing != nil {
		for _, profile := range instance.cfg.Routing.Profiles {
			for _, targetIssue := range profile.ReportingTargets {
				excluded[targetIssue] = struct{}{}
			}
		}
	}

	grouped := map[string][]model.Row{}
	findings := make([]PlanFinding, 0)
	type pullResult struct {
		issueKey string
		valid    []model.Row
		invalid  []model.InvalidRow
		err      error
	}
	results := make(chan pullResult, len(issues))
	var wg sync.WaitGroup
	for _, issue := range issues {
		issue := issue
		wg.Add(1)
		go func() {
			defer wg.Done()
			issueKey, issueRef, err := resolveJiraCloudIssueReference(ctx, client, issue)
			if err != nil {
				results <- pullResult{err: err}
				return
			}
			if _, ok := excluded[issueKey]; ok {
				results <- pullResult{}
				return
			}
			worklogItems, err := client.ListIssueWorklogs(ctx, issueRef)
			if err != nil {
				results <- pullResult{err: err}
				return
			}
			valid, invalid := jiracloud.NormalizeIssueWorklogs(issueKey, worklogItems, user, windowFrom, windowTo)
			results <- pullResult{issueKey: issueKey, valid: valid, invalid: invalid}
		}()
	}
	wg.Wait()
	close(results)
	fetched := 0
	for result := range results {
		if result.err != nil {
			return Plan{}, result.err
		}
		if result.issueKey == "" {
			continue
		}
		fetched++
		grouped[result.issueKey] = append(grouped[result.issueKey], result.valid...)
		for _, invalidRow := range result.invalid {
			findings = append(findings, PlanFinding{
				ID:           uuid.NewString(),
				PlanID:       plan.ID,
				SourceRowID:  invalidRow.SourceRowID,
				ReasonCode:   invalidRow.ReasonCode,
				ReasonDetail: invalidRow.ReasonDetail,
				Payload:      invalidRow,
			})
		}
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: fetched, ScopeTotal: len(issues), Message: "fetched jira-cloud issue worklogs"})

	issueKeys := sortedKeys(grouped)
	items := make([]PlanItem, 0, len(issueKeys))
	for _, issueKey := range issueKeys {
		remoteRows := sortRows(grouped[issueKey])
		localRows, err := s.listLocalScope(issueKey, windowFrom, windowTo)
		if err != nil {
			return Plan{}, err
		}
		localPayload := localToRows(localRows)
		item := newPlanItem(plan, issueKey, localPayload)
		item.TargetAdapterInstance = instance.name
		item.TargetIssue = issueKey
		item.Payload = remoteRows
		item.RemoteRowCount = len(remoteRows)
		item.RemoteTotal = sumRows(remoteRows)
		item.PlanStatus = "ready"
		item.PlannedAction = "merge"
		item.ComparisonStatus = "merge_needed"
		item.ReasonCode = "remote_diff"
		item.ReasonDetail = "Jira Cloud rows differ from the local ledger"
		item.InspectionSummary = InspectionSummary{
			LocalRowCount:          len(localPayload),
			LocalTotalSeconds:      sumRows(localPayload),
			RemoteRowCount:         len(remoteRows),
			RemoteTotalSeconds:     sumRows(remoteRows),
			SourceIssueKeys:        []string{issueKey},
			PerSourceTotals:        map[string]int{issueKey: sumRows(remoteRows)},
			ResolvedTargetInstance: instance.name,
			ResolvedTargetIssue:    issueKey,
		}
		applyInspectionRowDiffSummary(&item, remoteRows, localPayload)
		if rowsEqual(remoteRows, localPayload) {
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Cloud rows already match the local ledger"
		}
		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}
	plan.Items = items
	plan.Findings = findings
	plan.AggregateStatus = deriveAggregateStatus(items, findings)
	normalizePlanSummary(&plan)
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "built jira-cloud pull plan"})
	return plan, nil
}

func resolveJiraCloudIssueReference(ctx context.Context, client jiraCloudClient, issue jiracloud.IssueBrief) (issueKey string, issueRef string, err error) {
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

func (s *Service) ReconcileJiraCloudPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (ReconcileResult, error) {
	result, err := s.reconcileJiraCloudPushPlanInMemory(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return ReconcileResult{}, err
	}
	if result.NoPlan != nil {
		return result, nil
	}
	if result.Plan != nil {
		if err := s.insertPlan(*result.Plan); err != nil {
			return ReconcileResult{}, err
		}
	}
	return result, nil
}

func (s *Service) reconcileJiraCloudPushPlanInMemory(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (ReconcileResult, error) {
	opts := resolvePlanOptions(options)
	routes, err := resolveJiraCloudRouteProfile(cfg, routeProfile)
	if err != nil {
		return ReconcileResult{}, err
	}

	plan, err := s.buildJiraCloudPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return ReconcileResult{}, err
	}
	if !routes.isReportingOnly() {
		return ReconcileResult{Plan: &plan}, nil
	}

	result := summarizeReportingNoPlan("jira-cloud", routeProfile, windowFrom, windowTo, plan.Items)
	if result != nil {
		if opts.PreserveNonActionableReportingPlan && result.Reason == "exact_match" {
			return ReconcileResult{Plan: &plan, NoPlan: result, PreserveNonActionableReportingPlan: true}, nil
		}
		return ReconcileResult{NoPlan: result}, nil
	}
	return ReconcileResult{Plan: &plan}, nil
}

func (s *Service) CreateJiraCloudPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildJiraCloudPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildJiraCloudPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	routes, err := resolveJiraCloudRouteProfile(cfg, routeProfile)
	if err != nil {
		return Plan{}, err
	}
	localRows, err := s.listActiveWindow(windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}

	activeRows := worklogsToRows(localRows)
	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "push",
		AdapterFamily:     "jira-cloud",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}
	if routes.isReportingOnly() {
		return s.buildJiraCloudReportingPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, plan, routes, activeRows, opts)
	}
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-cloud push"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-cloud push complete"})
	}()
	remoteOwnedIssues, failedInstances, err := s.fetchJiraCloudRemoteOwnedIssues(ctx, cfg, routes, windowFrom, windowTo, opts.ExcludedRemoteOwnedIssueKeys)
	if err != nil {
		return Plan{}, err
	}
	issueKeys := unionKeys(activeRows, remoteOwnedIssues)
	planned := make([]jiraCloudPlannedScope, 0, len(issueKeys))
	items := make([]PlanItem, 0, len(issueKeys))
	failedScopeInstances := map[string]struct{}{}
	for _, issueKey := range issueKeys {
		activePayload := sortRows(activeRows[issueKey])
		localPayload := activePayload
		route, ok, err := routes.resolve(issueKey)
		if err != nil {
			return Plan{}, err
		}
		if !ok {
			if opts.SuppressMissingRoutes {
				continue
			}
			item := newPlanItem(plan, issueKey, activePayload)
			item.RouteProfile = routeProfile
			applyActiveLocalMetrics(&item, activePayload)
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "missing_route"
			item.ReasonDetail = "Jira Cloud routing is missing for this issue scope"
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}
		if route.reporting {
			if err := validateReportingTargetOwnership(cfg, "jira-cloud", routeProfile, route.targetIssue); err != nil {
				return Plan{}, err
			}
		}
		if failure, ok := failedInstances[route.targetInstance]; ok {
			item := newPlanItem(plan, issueKey, localPayload)
			item.RouteProfile = routeProfile
			item.TargetAdapterFamily = "jira-cloud"
			item.TargetAdapterInstance = route.targetInstance
			item.TargetIssue = route.targetIssue
			item.InspectionSummary = InspectionSummary{
				LocalRowCount:          len(activePayload),
				LocalTotalSeconds:      sumRows(activePayload),
				SourceIssueKeys:        []string{issueKey},
				PerSourceTotals:        map[string]int{issueKey: sumRows(activePayload)},
				ResolvedTargetInstance: route.targetInstance,
				ResolvedTargetIssue:    route.targetIssue,
			}
			applyActiveLocalMetrics(&item, activePayload)
			applyJiraCloudCheckFailed(&item, failure)
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			failedScopeInstances[route.targetInstance] = struct{}{}
			continue
		}
		if route.reporting {
			for i := range localPayload {
				localPayload[i].IssueKey = route.targetIssue
				localPayload[i].Description = jiracloud.ReportingDescription(issueKey, localPayload[i].Description)
			}
		}
		planned = append(planned, jiraCloudPlannedScope{issueKey: issueKey, activePayload: activePayload, localPayload: localPayload, route: route})
	}
	fetchedScopes, err := s.fetchJiraCloudPushScopes(ctx, cfg, planned, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: len(fetchedScopes), ScopeTotal: len(planned), Message: "fetched jira-cloud remote scopes"})
	for _, scope := range planned {
		fetched := fetchedScopes[scope.issueKey]
		item := newPlanItem(plan, scope.issueKey, scope.localPayload)
		item.RouteProfile = routeProfile
		item.TargetAdapterFamily = "jira-cloud"
		item.TargetAdapterInstance = scope.route.targetInstance
		item.TargetIssue = scope.route.targetIssue
		item.RemoteRowCount = len(fetched.targetRows)
		item.RemoteTotal = sumRows(fetched.targetRows)
		item.InspectionSummary = InspectionSummary{
			LocalRowCount:          len(scope.activePayload),
			LocalTotalSeconds:      sumRows(scope.activePayload),
			RemoteRowCount:         len(fetched.targetRows),
			RemoteTotalSeconds:     sumRows(fetched.targetRows),
			SourceIssueKeys:        []string{scope.issueKey},
			PerSourceTotals:        map[string]int{scope.issueKey: sumRows(scope.activePayload)},
			ResolvedTargetInstance: scope.route.targetInstance,
			ResolvedTargetIssue:    scope.route.targetIssue,
			ForeignAuthorPresent:   fetched.foreignPresent,
		}
		applyActiveLocalMetrics(&item, scope.activePayload)
		switch {
		case fetched.failure != nil:
			applyJiraCloudCheckFailed(&item, *fetched.failure)
		case rowsEqual(scope.localPayload, fetched.targetRows):
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Cloud rows already match the local ledger"
			applyInspectionRowDiffSummary(&item, scope.localPayload, fetched.targetRows)
		case len(fetched.targetRows) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "create"
			item.ComparisonStatus = "remote_missing"
			item.ReasonCode = "remote_missing"
			item.ReasonDetail = "Jira Cloud scope has no normalized remote rows and will be created"
			applyInspectionRowDiffSummary(&item, scope.localPayload, fetched.targetRows)
		default:
			item.PlanStatus = "ready"
			item.PlannedAction = "replace"
			item.ComparisonStatus = "remote_diff"
			item.ReasonCode = "remote_diff"
			item.ReasonDetail = "Jira Cloud scope differs from the saved local payload"
			applyInspectionRowDiffSummary(&item, scope.localPayload, fetched.targetRows)
		}
		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}
	for instanceName, failure := range failedInstances {
		if _, ok := failedScopeInstances[instanceName]; ok {
			continue
		}
		item := newPlanItem(plan, instanceName, nil)
		item.RouteProfile = routeProfile
		item.TargetAdapterFamily = "jira-cloud"
		item.TargetAdapterInstance = instanceName
		item.TargetIssue = instanceName
		item.InspectionSummary = InspectionSummary{
			ResolvedTargetInstance: instanceName,
			ResolvedTargetIssue:    instanceName,
		}
		applyJiraCloudCheckFailed(&item, failure)
		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].TargetIssue != items[j].TargetIssue {
			return items[i].TargetIssue < items[j].TargetIssue
		}
		return items[i].IssueKey < items[j].IssueKey
	})
	plan.Items = items
	plan.AggregateStatus = deriveAggregateStatus(items, nil)
	normalizePlanSummary(&plan)
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "saved jira-cloud push plan"})
	return plan, nil
}

func (s *Service) buildJiraCloudReportingPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, plan Plan, routes jiraRouteProfile, activeRows map[string][]model.Row, opts PlanOptions) (Plan, error) {
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-cloud push"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-cloud push complete"})
	}()
	issueKeys := sortedKeys(activeRows)
	items := make([]PlanItem, 0, len(issueKeys))
	groupOrder := make([]string, 0)
	groups := map[string]*reportingScopeGroup{}

	for _, route := range routes.uniqueReportingTargets() {
		groupKey := "jira-cloud\x00" + route.targetInstance + "\x00" + route.targetIssue
		if _, ok := groups[groupKey]; ok {
			continue
		}
		groups[groupKey] = newReportingScopeGroup("jira-cloud", route.targetInstance, route.targetIssue)
		groupOrder = append(groupOrder, groupKey)
	}

	for _, issueKey := range issueKeys {
		activePayload := sortRows(activeRows[issueKey])
		route, ok, err := routes.resolve(issueKey)
		if err != nil {
			return Plan{}, err
		}
		if !ok {
			if opts.SuppressMissingRoutes {
				continue
			}
			item := newPlanItem(plan, issueKey, activePayload)
			item.RouteProfile = routeProfile
			applyActiveLocalMetrics(&item, activePayload)
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "missing_route"
			item.ReasonDetail = "Jira Cloud routing is missing for this issue scope"
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}
		if err := validateReportingTargetOwnership(cfg, "jira-cloud", routeProfile, route.targetIssue); err != nil {
			return Plan{}, err
		}

		groupKey := "jira-cloud\x00" + route.targetInstance + "\x00" + route.targetIssue
		group := groups[groupKey]
		group.addSource(issueKey, activePayload, jiracloud.ReportingDescription)
	}

	fetchedGroups, err := s.fetchJiraCloudReportingGroups(ctx, cfg, groups, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: len(fetchedGroups), ScopeTotal: len(groupOrder), Message: "fetched jira-cloud reporting scopes"})
	for _, groupKey := range groupOrder {
		group := groups[groupKey]
		fetched := fetchedGroups[groupKey]
		desiredRows := sortRows(group.DesiredRows)
		cleanupRows := normalizeDeletedJiraCloudRows(group.TargetIssue, fetched.remoteScope)

		item := newPlanItem(plan, group.TargetIssue, desiredRows)
		item.RouteProfile = routeProfile
		item.TargetAdapterFamily = group.TargetAdapterFamily
		item.TargetAdapterInstance = group.TargetAdapterInstance
		item.TargetIssue = group.TargetIssue
		remoteRows := fetched.targetRows
		if len(desiredRows) == 0 && len(cleanupRows) > 0 {
			remoteRows = cleanupRows
		}
		item.RemoteRowCount = len(remoteRows)
		item.RemoteTotal = sumRows(remoteRows)
		item.InspectionSummary = InspectionSummary{
			LocalRowCount:          len(desiredRows),
			LocalTotalSeconds:      sumRows(desiredRows),
			RemoteRowCount:         len(remoteRows),
			RemoteTotalSeconds:     sumRows(remoteRows),
			SourceIssueKeys:        group.sortedSourceIssueKeys(),
			PerSourceTotals:        clonePerSourceTotals(group.PerSourceTotals),
			ResolvedTargetInstance: group.TargetAdapterInstance,
			ResolvedTargetIssue:    group.TargetIssue,
			ForeignAuthorPresent:   fetched.foreignPresent,
		}

		switch {
		case fetched.failure != nil:
			applyJiraCloudCheckFailed(&item, *fetched.failure)
		case len(desiredRows) == 0 && len(fetched.remoteScope) == 0:
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Cloud scope already matches the local empty state"
			applyInspectionRowDiffCounts(&item, rowDiffCounts{})
		case len(desiredRows) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "delete"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "remote_present"
			item.ReasonDetail = "Jira Cloud scope contains rows that should be deleted"
			applyInspectionRowDiffSummary(&item, nil, remoteRows)
		case rowsEqual(desiredRows, fetched.targetRows):
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Cloud rows already match the local ledger"
			applyInspectionRowDiffSummary(&item, desiredRows, fetched.targetRows)
		case len(fetched.targetRows) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "create"
			item.ComparisonStatus = "remote_missing"
			item.ReasonCode = "remote_missing"
			item.ReasonDetail = "Jira Cloud scope has no normalized remote rows and will be created"
			applyInspectionRowDiffSummary(&item, desiredRows, fetched.targetRows)
		default:
			item.PlanStatus = "ready"
			item.PlannedAction = "replace"
			item.ComparisonStatus = "remote_diff"
			item.ReasonCode = "remote_diff"
			item.ReasonDetail = "Jira Cloud scope differs from the saved local payload"
			applyInspectionRowDiffSummary(&item, desiredRows, fetched.targetRows)
		}

		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].TargetIssue != items[j].TargetIssue {
			return items[i].TargetIssue < items[j].TargetIssue
		}
		return items[i].IssueKey < items[j].IssueKey
	})
	plan.Items = items
	plan.AggregateStatus = deriveAggregateStatus(items, nil)
	normalizePlanSummary(&plan)
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "saved jira-cloud push plan"})
	return plan, nil
}

type jiraCloudPlannedScope struct {
	issueKey      string
	activePayload []model.Row
	localPayload  []model.Row
	route         jiraResolvedRoute
}

type jiraCloudFetchedScope struct {
	targetRows     []model.Row
	remoteScope    []jiracloud.Worklog
	foreignPresent bool
	failure        *jiraCloudFetchFailure
}

type jiraCloudFetchFailure struct {
	reasonCode   string
	reasonDetail string
}

func (s *Service) fetchJiraCloudRemoteOwnedIssues(ctx context.Context, cfg config.EffectiveConfig, routes jiraRouteProfile, windowFrom, windowTo time.Time, excludedIssueKeys []string) (map[string]bool, map[string]jiraCloudFetchFailure, error) {
	owned := make(map[string]bool)
	failed := make(map[string]jiraCloudFetchFailure)
	excluded := stringSet(excludedIssueKeys)
	grouped := routes.groupedTargetInstances()
	for instanceName := range grouped {
		instance, err := requireJiraCloudInstance(cfg, instanceName)
		if err != nil {
			return nil, nil, err
		}
		client := s.newJiraCloudClient(instance)
		jql := fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", windowFrom.Format("2006-01-02"), windowTo.Format("2006-01-02"))
		issues, err := client.SearchIssues(ctx, jql, nil)
		if err != nil {
			failed[instanceName] = classifyJiraCloudFetchFailure(err)
			continue
		}
		for _, issue := range issues {
			issueKey, _, err := resolveJiraCloudIssueReference(ctx, client, issue)
			if err != nil {
				return nil, nil, err
			}
			if _, ok := excluded[issueKey]; ok {
				continue
			}
			route, ok, err := routes.resolve(issueKey)
			if err != nil {
				return nil, nil, err
			}
			if !ok || route.reporting || route.targetInstance != instanceName {
				continue
			}
			owned[issueKey] = true
		}
	}
	return owned, failed, nil
}

func (s *Service) fetchJiraCloudPushScopes(ctx context.Context, cfg config.EffectiveConfig, planned []jiraCloudPlannedScope, windowFrom, windowTo time.Time) (map[string]jiraCloudFetchedScope, error) {
	grouped := map[string][]jiraCloudPlannedScope{}
	for _, scope := range planned {
		grouped[scope.route.targetInstance] = append(grouped[scope.route.targetInstance], scope)
	}
	results := make(chan struct {
		key   string
		scope jiraCloudFetchedScope
	}, len(planned))
	var wg sync.WaitGroup
	for instanceName, scopes := range grouped {
		instanceName, scopes := instanceName, scopes
		instance, err := requireJiraCloudInstance(cfg, instanceName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := s.newJiraCloudClient(instance)
			user, err := client.CurrentUser(ctx)
			if err != nil {
				failure := classifyJiraCloudFetchFailure(err)
				for _, scope := range scopes {
					results <- struct {
						key   string
						scope jiraCloudFetchedScope
					}{
						key:   scope.issueKey,
						scope: jiraCloudFetchedScope{failure: &failure},
					}
				}
				return
			}
			for _, scope := range scopes {
				worklogItems, err := client.ListIssueWorklogs(ctx, scope.route.targetIssue)
				if err != nil {
					results <- struct {
						key   string
						scope jiraCloudFetchedScope
					}{
						key:   scope.issueKey,
						scope: jiraCloudFetchedScope{failure: ptrJiraCloudFetchFailure(classifyJiraCloudFetchFailure(err))},
					}
					continue
				}
				userRows, _ := jiracloud.NormalizeIssueWorklogs(scope.route.targetIssue, worklogItems, user, windowFrom, windowTo)
				results <- struct {
					key   string
					scope jiraCloudFetchedScope
				}{
					key: scope.issueKey,
					scope: jiraCloudFetchedScope{
						targetRows:     sortRows(userRows),
						remoteScope:    filterJiraCloudWorklogsByUserAndWindow(worklogItems, user, windowFrom, windowTo),
						foreignPresent: hasForeignJiraCloudWorklogs(worklogItems, user, windowFrom, windowTo),
					},
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	fetched := make(map[string]jiraCloudFetchedScope, len(planned))
	for result := range results {
		fetched[result.key] = result.scope
	}
	return fetched, nil
}

func (p jiraRouteProfile) groupedTargetInstances() map[string]struct{} {
	grouped := make(map[string]struct{})
	for _, route := range p.routes {
		grouped[route.targetInstance] = struct{}{}
	}
	return grouped
}

func (p jiraRouteProfile) uniqueReportingTargets() []jiraResolvedRoute {
	targets := make([]jiraResolvedRoute, 0)
	seen := make(map[string]struct{})
	for _, route := range p.routes {
		if !route.reporting {
			continue
		}
		key := route.targetInstance + "\x00" + route.targetIssue
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, jiraResolvedRoute{
			targetInstance: route.targetInstance,
			targetIssue:    route.targetIssue,
			reporting:      true,
		})
	}
	return targets
}

func (s *Service) fetchJiraCloudReportingGroups(ctx context.Context, cfg config.EffectiveConfig, groups map[string]*reportingScopeGroup, windowFrom, windowTo time.Time) (map[string]jiraCloudFetchedScope, error) {
	groupedByInstance := map[string][]string{}
	for key, group := range groups {
		groupedByInstance[group.TargetAdapterInstance] = append(groupedByInstance[group.TargetAdapterInstance], key)
	}
	results := make(chan struct {
		key   string
		scope jiraCloudFetchedScope
	}, len(groups))
	var wg sync.WaitGroup
	for instanceName, keys := range groupedByInstance {
		instanceName, keys := instanceName, keys
		instance, err := requireJiraCloudInstance(cfg, instanceName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := s.newJiraCloudClient(instance)
			user, err := client.CurrentUser(ctx)
			if err != nil {
				failure := classifyJiraCloudFetchFailure(err)
				for _, key := range keys {
					results <- struct {
						key   string
						scope jiraCloudFetchedScope
					}{
						key:   key,
						scope: jiraCloudFetchedScope{failure: &failure},
					}
				}
				return
			}
			for _, key := range keys {
				group := groups[key]
				worklogItems, err := client.ListIssueWorklogs(ctx, group.TargetIssue)
				if err != nil {
					results <- struct {
						key   string
						scope jiraCloudFetchedScope
					}{
						key:   key,
						scope: jiraCloudFetchedScope{failure: ptrJiraCloudFetchFailure(classifyJiraCloudFetchFailure(err))},
					}
					continue
				}
				userRows, _ := jiracloud.NormalizeIssueWorklogs(group.TargetIssue, worklogItems, user, windowFrom, windowTo)
				results <- struct {
					key   string
					scope jiraCloudFetchedScope
				}{
					key: key,
					scope: jiraCloudFetchedScope{
						targetRows:     sortRows(userRows),
						remoteScope:    filterJiraCloudWorklogsByUserAndWindow(worklogItems, user, windowFrom, windowTo),
						foreignPresent: hasForeignJiraCloudWorklogs(worklogItems, user, windowFrom, windowTo),
					},
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	fetched := make(map[string]jiraCloudFetchedScope, len(groups))
	for result := range results {
		fetched[result.key] = result.scope
	}
	return fetched, nil
}

func applyJiraCloudCheckFailed(item *PlanItem, failure jiraCloudFetchFailure) {
	item.PlanStatus = "check_failed"
	item.PlannedAction = "none"
	item.ComparisonStatus = "check_failed"
	item.ReasonCode = failure.reasonCode
	item.ReasonDetail = failure.reasonDetail
}

func classifyJiraCloudFetchFailure(err error) jiraCloudFetchFailure {
	var requestErr *jiracloud.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return jiraCloudFetchFailure{reasonCode: "auth_error", reasonDetail: "jira cloud authentication failed"}
		case http.StatusNotFound:
			return jiraCloudFetchFailure{reasonCode: "not_found", reasonDetail: "jira cloud resource not found"}
		default:
			return jiraCloudFetchFailure{reasonCode: "remote_error", reasonDetail: requestErr.Error()}
		}
	}
	return jiraCloudFetchFailure{reasonCode: "unexpected_error", reasonDetail: err.Error()}
}

func ptrJiraCloudFetchFailure(failure jiraCloudFetchFailure) *jiraCloudFetchFailure {
	return &failure
}

type resolvedJiraCloudPullInstance struct {
	name string
	cfg  config.JiraCloudInstance
}

func resolvePullJiraCloudInstance(cfg config.EffectiveConfig, instanceName string) (resolvedJiraCloudPullInstance, error) {
	name, item, err := config.ResolveJiraCloudInstance(cfg, instanceName)
	if err != nil {
		return resolvedJiraCloudPullInstance{}, err
	}
	return resolvedJiraCloudPullInstance{name: name, cfg: item}, nil
}

func requireJiraCloudInstance(cfg config.EffectiveConfig, instanceName string) (config.JiraCloudInstance, error) {
	_, resolved, err := config.ResolveJiraCloudInstance(cfg, instanceName)
	if err != nil {
		return config.JiraCloudInstance{}, err
	}
	return resolved, nil
}

func resolveJiraCloudRouteProfile(cfg config.EffectiveConfig, name string) (jiraRouteProfile, error) {
	if cfg.File.JiraCloud == nil || len(cfg.File.JiraCloud.Instances) == 0 {
		return jiraRouteProfile{}, errors.New("jira_cloud routing is required for push")
	}

	targets := make([]jiraRoutingTarget, 0)
	for instanceName, instance := range cfg.File.JiraCloud.Instances {
		if instance.Routing == nil {
			continue
		}
		targets = append(targets, jiraRoutingTarget{
			target:   ReconcileTarget{AdapterFamily: "jira-cloud", Instance: instanceName},
			profiles: instance.Routing.Profiles,
		})
	}
	return resolveJiraRouteProfileForTargets("jira-cloud", name, targets)
}

func (p jiraRouteProfile) isReportingOnly() bool {
	if len(p.routes) == 0 {
		return false
	}
	for _, route := range p.routes {
		if !route.reporting {
			return false
		}
	}
	return true
}

func filterJiraCloudWorklogsByUserAndWindow(items []jiracloud.Worklog, user jiracloud.User, from, to time.Time) []jiracloud.Worklog {
	filtered := make([]jiracloud.Worklog, 0)
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		startedAt = startedAt.UTC()
		if startedAt.Before(from.UTC()) || startedAt.After(to.UTC()) {
			continue
		}
		if jiraWorklogAuthorMatchesUser(item.Author, user) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func hasForeignJiraCloudWorklogs(items []jiracloud.Worklog, user jiracloud.User, from, to time.Time) bool {
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		startedAt = startedAt.UTC()
		if startedAt.Before(from.UTC()) || startedAt.After(to.UTC()) {
			continue
		}
		if !jiraWorklogAuthorMatchesUser(item.Author, user) {
			return true
		}
	}
	return false
}
