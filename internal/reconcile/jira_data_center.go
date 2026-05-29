package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

func (s *Service) CreateJiraDataPullPlan(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildJiraDataPullPlan(ctx, cfg, instanceName, windowFrom, windowTo, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildJiraDataPullPlan(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	instance, err := resolvePullJiraDataInstance(cfg, instanceName)
	if err != nil {
		return Plan{}, err
	}
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	client := s.newJiraDataClient(instance.cfg)
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-data-center pull"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-data-center pull complete"})
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
		AdapterFamily:     "jira-data-center",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}

	exclusions, err := config.JiraExcludedIssuesForInstance(cfg, "jira-data-center", instance.name)
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
			if _, ok := excluded[issue.Key]; ok {
				results <- pullResult{}
				return
			}
			worklogItems, err := client.ListIssueWorklogs(ctx, issue.Key)
			if err != nil {
				results <- pullResult{err: fmt.Errorf("issue %s: %w", issue.Key, err)}
				return
			}
			valid, invalid := jiradatacenter.NormalizeIssueWorklogs(issue.Key, worklogItems, user, windowFrom, windowTo)
			results <- pullResult{issueKey: issue.Key, valid: valid, invalid: invalid}
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
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: fetched, ScopeTotal: len(issues), Message: "fetched jira-data-center issue worklogs"})

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
		item.ReasonDetail = "Jira Data Center rows differ from the local ledger"
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
		if tombstoneProtected(remoteRows, s.store.DB()) {
			item.PlanStatus = "invalid"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "tombstone_protected"
			item.ReasonDetail = "Saved payload would recreate a tombstoned local allocation"
		} else if rowsEqual(remoteRows, localPayload) {
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Data Center rows already match the local ledger"
		}
		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}
	plan.Items = items
	plan.Findings = findings
	plan.AggregateStatus = deriveAggregateStatus(items, findings)
	normalizePlanSummary(&plan)
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "built jira-data-center pull plan"})
	return plan, nil
}

func (s *Service) ReconcileJiraDataPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (ReconcileResult, error) {
	result, err := s.reconcileJiraDataPushPlanInMemory(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
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

func (s *Service) reconcileJiraDataPushPlanInMemory(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (ReconcileResult, error) {
	routes, err := resolveJiraDataRouteProfile(cfg, routeProfile)
	if err != nil {
		return ReconcileResult{}, err
	}

	plan, err := s.buildJiraDataPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return ReconcileResult{}, err
	}
	if !routes.isReportingOnly() {
		return ReconcileResult{Plan: &plan}, nil
	}

	result := summarizeReportingNoPlan("jira-data-center", routeProfile, windowFrom, windowTo, plan.Items)
	if result != nil {
		return ReconcileResult{NoPlan: result}, nil
	}
	return ReconcileResult{Plan: &plan}, nil
}

func (s *Service) CreateJiraDataPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildJiraDataPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildJiraDataPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	routes, err := resolveJiraDataRouteProfile(cfg, routeProfile)
	if err != nil {
		return Plan{}, err
	}
	localRows, err := s.listActiveWindow(windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	tombstones, err := s.listTombstoneScope(windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}

	activeRows := worklogsToRows(localRows)
	tombstoneRows := tombstonesToRows(tombstones)
	issueKeys := unionKeys(activeRows, tombstoneRows)
	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "push",
		AdapterFamily:     "jira-data-center",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}
	if routes.isReportingOnly() {
		return s.buildJiraDataReportingPushPlan(ctx, cfg, routeProfile, windowFrom, windowTo, onlyDeleted, plan, routes, activeRows, tombstoneRows, opts)
	}
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-data-center push"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-data-center push complete"})
	}()
	planned := make([]jiraDataPlannedScope, 0, len(issueKeys))
	items := make([]PlanItem, 0, len(issueKeys))
	for _, issueKey := range issueKeys {
		activePayload := sortRows(activeRows[issueKey])
		localPayload := activePayload
		deletePayload := sortRows(tombstoneRows[issueKey])
		isDeleteOnly := len(localPayload) == 0 && len(deletePayload) > 0
		if onlyDeleted && !isDeleteOnly {
			continue
		}
		if isDeleteOnly {
			localPayload = deletePayload
		}
		route, ok, err := routes.resolve(issueKey)
		if err != nil {
			return Plan{}, err
		}
		if !ok {
			item := newPlanItem(plan, issueKey, localPayload)
			applyActiveLocalMetrics(&item, activePayload)
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "missing_route"
			item.ReasonDetail = "Jira Data Center routing is missing for this issue scope"
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}
		if route.reporting {
			if err := validateReportingTargetOwnership(cfg, "jira-data-center", routeProfile, route.targetIssue); err != nil {
				return Plan{}, err
			}
		}
		if route.reporting {
			for i := range localPayload {
				localPayload[i].IssueKey = route.targetIssue
				localPayload[i].Description = jiradatacenter.ReportingDescription(issueKey, localPayload[i].Description)
			}
		}
		planned = append(planned, jiraDataPlannedScope{issueKey: issueKey, activePayload: activePayload, localPayload: localPayload, isDeleteOnly: isDeleteOnly, route: route})
	}
	fetchedScopes, err := s.fetchJiraDataPushScopes(ctx, cfg, planned, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: len(fetchedScopes), ScopeTotal: len(planned), Message: "fetched jira-data-center remote scopes"})
	for _, scope := range planned {
		fetched := fetchedScopes[scope.issueKey]
		item := newPlanItem(plan, scope.issueKey, scope.localPayload)
		item.RouteProfile = routeProfile
		item.TargetAdapterFamily = "jira-data-center"
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
			applyJiraDataCheckFailed(&item, *fetched.failure)
		case scope.isDeleteOnly && len(fetched.remoteScope) == 0:
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Data Center scope already matches the local empty state"
		case scope.isDeleteOnly && fetched.foreignPresent:
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "foreign_authored_rows_present"
			item.ReasonDetail = "Cleanup scope contains foreign-authored Jira worklogs"
		case scope.isDeleteOnly:
			item.PlanStatus = "ready"
			item.PlannedAction = "delete"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "remote_present"
			item.ReasonDetail = "Jira Data Center scope contains rows that should be deleted"
		case rowsEqual(scope.localPayload, fetched.targetRows):
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Data Center rows already match the local ledger"
		case fetched.foreignPresent:
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "foreign_authored_rows_present"
			item.ReasonDetail = "Cleanup scope contains foreign-authored Jira worklogs"
		case len(fetched.remoteScope) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "create"
			item.ComparisonStatus = "remote_missing"
			item.ReasonCode = "remote_missing"
			item.ReasonDetail = "Jira Data Center scope is empty and will be created"
		default:
			item.PlanStatus = "ready"
			item.PlannedAction = "replace"
			item.ComparisonStatus = "remote_diff"
			item.ReasonCode = "remote_diff"
			item.ReasonDetail = "Jira Data Center scope differs from the saved local payload"
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
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "saved jira-data-center push plan"})
	return plan, nil
}

func (s *Service) buildJiraDataReportingPushPlan(ctx context.Context, cfg config.EffectiveConfig, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, plan Plan, routes jiraRouteProfile, activeRows map[string][]model.Row, tombstoneRows map[string][]model.Row, opts PlanOptions) (Plan, error) {
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile jira-data-center push"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile jira-data-center push complete"})
	}()
	issueKeys := unionKeys(activeRows, tombstoneRows)
	items := make([]PlanItem, 0, len(issueKeys))
	groupOrder := make([]string, 0)
	groups := map[string]*reportingScopeGroup{}

	for _, issueKey := range issueKeys {
		activePayload := sortRows(activeRows[issueKey])
		deletePayload := sortRows(tombstoneRows[issueKey])
		isDeleteOnly := len(activePayload) == 0 && len(deletePayload) > 0
		if onlyDeleted && !isDeleteOnly {
			continue
		}

		localPayload := activePayload
		if isDeleteOnly {
			localPayload = deletePayload
		}

		route, ok, err := routes.resolve(issueKey)
		if err != nil {
			return Plan{}, err
		}
		if !ok {
			item := newPlanItem(plan, issueKey, localPayload)
			applyActiveLocalMetrics(&item, activePayload)
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "missing_route"
			item.ReasonDetail = "Jira Data Center routing is missing for this issue scope"
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}
		if err := validateReportingTargetOwnership(cfg, "jira-data-center", routeProfile, route.targetIssue); err != nil {
			return Plan{}, err
		}

		groupKey := "jira-data-center\x00" + route.targetInstance + "\x00" + route.targetIssue
		group, ok := groups[groupKey]
		if !ok {
			group = newReportingScopeGroup("jira-data-center", route.targetInstance, route.targetIssue)
			groups[groupKey] = group
			groupOrder = append(groupOrder, groupKey)
		}
		group.addSource(issueKey, activePayload, deletePayload, jiradatacenter.ReportingDescription)
	}

	fetchedGroups, err := s.fetchJiraDataReportingGroups(ctx, cfg, groups, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: len(fetchedGroups), ScopeTotal: len(groupOrder), Message: "fetched jira-data-center reporting scopes"})
	for _, groupKey := range groupOrder {
		group := groups[groupKey]
		fetched := fetchedGroups[groupKey]
		desiredRows := sortRows(group.DesiredRows)
		isDeleteOnly := len(desiredRows) == 0 && group.HasTombstones

		item := newPlanItem(plan, group.TargetIssue, desiredRows)
		item.RouteProfile = routeProfile
		item.TargetAdapterFamily = group.TargetAdapterFamily
		item.TargetAdapterInstance = group.TargetAdapterInstance
		item.TargetIssue = group.TargetIssue
		item.RemoteRowCount = len(fetched.targetRows)
		item.RemoteTotal = sumRows(fetched.targetRows)
		item.InspectionSummary = InspectionSummary{
			LocalRowCount:          len(desiredRows),
			LocalTotalSeconds:      sumRows(desiredRows),
			RemoteRowCount:         len(fetched.targetRows),
			RemoteTotalSeconds:     sumRows(fetched.targetRows),
			SourceIssueKeys:        group.sortedSourceIssueKeys(),
			PerSourceTotals:        clonePerSourceTotals(group.PerSourceTotals),
			ResolvedTargetInstance: group.TargetAdapterInstance,
			ResolvedTargetIssue:    group.TargetIssue,
			ForeignAuthorPresent:   fetched.foreignPresent,
		}

		switch {
		case fetched.failure != nil:
			applyJiraDataCheckFailed(&item, *fetched.failure)
		case isDeleteOnly && len(fetched.remoteScope) == 0:
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Data Center scope already matches the local empty state"
		case isDeleteOnly && fetched.foreignPresent:
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "foreign_authored_rows_present"
			item.ReasonDetail = "Cleanup scope contains foreign-authored Jira worklogs"
		case isDeleteOnly:
			item.PlanStatus = "ready"
			item.PlannedAction = "delete"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "remote_present"
			item.ReasonDetail = "Jira Data Center scope contains rows that should be deleted"
		case rowsEqual(desiredRows, fetched.targetRows):
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Jira Data Center rows already match the local ledger"
		case fetched.foreignPresent:
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "remote_present"
			item.ReasonCode = "foreign_authored_rows_present"
			item.ReasonDetail = "Cleanup scope contains foreign-authored Jira worklogs"
		case len(fetched.remoteScope) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "create"
			item.ComparisonStatus = "remote_missing"
			item.ReasonCode = "remote_missing"
			item.ReasonDetail = "Jira Data Center scope is empty and will be created"
		default:
			item.PlanStatus = "ready"
			item.PlannedAction = "replace"
			item.ComparisonStatus = "remote_diff"
			item.ReasonCode = "remote_diff"
			item.ReasonDetail = "Jira Data Center scope differs from the saved local payload"
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
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "saved jira-data-center push plan"})
	return plan, nil
}

type jiraDataPlannedScope struct {
	issueKey      string
	activePayload []model.Row
	localPayload  []model.Row
	isDeleteOnly  bool
	route         jiraResolvedRoute
}

type jiraDataFetchedScope struct {
	targetRows     []model.Row
	remoteScope    []jiradatacenter.Worklog
	foreignPresent bool
	failure        *jiraDataFetchFailure
}

type jiraDataFetchFailure struct {
	reasonCode   string
	reasonDetail string
}

func classifyJiraDataFetchFailure(err error) jiraDataFetchFailure {
	var requestErr *jiradatacenter.RequestError
	if errors.As(err, &requestErr) {
		switch requestErr.StatusCode {
		case 401, 403:
			return jiraDataFetchFailure{reasonCode: "auth_error", reasonDetail: "jira data center authentication failed"}
		case 404:
			return jiraDataFetchFailure{reasonCode: "not_found", reasonDetail: "jira data center resource not found"}
		default:
			return jiraDataFetchFailure{reasonCode: "remote_error", reasonDetail: requestErr.Error()}
		}
	}
	return jiraDataFetchFailure{reasonCode: "unexpected_error", reasonDetail: err.Error()}
}

func ptrJiraDataFetchFailure(f jiraDataFetchFailure) *jiraDataFetchFailure { return &f }

func applyJiraDataCheckFailed(item *PlanItem, failure jiraDataFetchFailure) {
	item.PlanStatus = "check_failed"
	item.PlannedAction = "none"
	item.ComparisonStatus = "check_failed"
	item.ReasonCode = failure.reasonCode
	item.ReasonDetail = failure.reasonDetail
}

func (s *Service) fetchJiraDataPushScopes(ctx context.Context, cfg config.EffectiveConfig, planned []jiraDataPlannedScope, windowFrom, windowTo time.Time) (map[string]jiraDataFetchedScope, error) {
	grouped := map[string][]jiraDataPlannedScope{}
	for _, scope := range planned {
		grouped[scope.route.targetInstance] = append(grouped[scope.route.targetInstance], scope)
	}
	results := make(chan struct {
		key   string
		scope jiraDataFetchedScope
	}, len(planned))
	var wg sync.WaitGroup
	for instanceName, scopes := range grouped {
		instanceName, scopes := instanceName, scopes
		instance, err := requireJiraDataInstance(cfg, instanceName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := s.newJiraDataClient(instance)
			user, err := client.CurrentUser(ctx)
			if err != nil {
				failure := classifyJiraDataFetchFailure(err)
				for _, scope := range scopes {
					results <- struct {
						key   string
						scope jiraDataFetchedScope
					}{key: scope.issueKey, scope: jiraDataFetchedScope{failure: &failure}}
				}
				return
			}
			for _, scope := range scopes {
				worklogItems, err := client.ListIssueWorklogs(ctx, scope.route.targetIssue)
				if err != nil {
					results <- struct {
						key   string
						scope jiraDataFetchedScope
					}{
						key:   scope.issueKey,
						scope: jiraDataFetchedScope{failure: ptrJiraDataFetchFailure(classifyJiraDataFetchFailure(err))},
					}
					continue
				}
				userRows, _ := jiradatacenter.NormalizeIssueWorklogs(scope.route.targetIssue, worklogItems, user, windowFrom, windowTo)
				results <- struct {
					key   string
					scope jiraDataFetchedScope
				}{
					key: scope.issueKey,
					scope: jiraDataFetchedScope{
						targetRows:     sortRows(userRows),
						remoteScope:    filterJiraWorklogsByUserAndWindow(worklogItems, user, windowFrom, windowTo),
						foreignPresent: hasForeignJiraWorklogs(worklogItems, user, windowFrom, windowTo),
					},
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	fetched := make(map[string]jiraDataFetchedScope, len(planned))
	for result := range results {
		fetched[result.key] = result.scope
	}
	return fetched, nil
}

func (s *Service) fetchJiraDataReportingGroups(ctx context.Context, cfg config.EffectiveConfig, groups map[string]*reportingScopeGroup, windowFrom, windowTo time.Time) (map[string]jiraDataFetchedScope, error) {
	groupedByInstance := map[string][]string{}
	for key, group := range groups {
		groupedByInstance[group.TargetAdapterInstance] = append(groupedByInstance[group.TargetAdapterInstance], key)
	}
	results := make(chan struct {
		key   string
		scope jiraDataFetchedScope
	}, len(groups))
	var wg sync.WaitGroup
	for instanceName, keys := range groupedByInstance {
		instanceName, keys := instanceName, keys
		instance, err := requireJiraDataInstance(cfg, instanceName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := s.newJiraDataClient(instance)
			user, err := client.CurrentUser(ctx)
			if err != nil {
				failure := classifyJiraDataFetchFailure(err)
				for _, key := range keys {
					results <- struct {
						key   string
						scope jiraDataFetchedScope
					}{key: key, scope: jiraDataFetchedScope{failure: &failure}}
				}
				return
			}
			for _, key := range keys {
				group := groups[key]
				worklogItems, err := client.ListIssueWorklogs(ctx, group.TargetIssue)
				if err != nil {
					results <- struct {
						key   string
						scope jiraDataFetchedScope
					}{
						key:   key,
						scope: jiraDataFetchedScope{failure: ptrJiraDataFetchFailure(classifyJiraDataFetchFailure(err))},
					}
					continue
				}
				userRows, _ := jiradatacenter.NormalizeIssueWorklogs(group.TargetIssue, worklogItems, user, windowFrom, windowTo)
				results <- struct {
					key   string
					scope jiraDataFetchedScope
				}{
					key: key,
					scope: jiraDataFetchedScope{
						targetRows:     sortRows(userRows),
						remoteScope:    filterJiraWorklogsByUserAndWindow(worklogItems, user, windowFrom, windowTo),
						foreignPresent: hasForeignJiraWorklogs(worklogItems, user, windowFrom, windowTo),
					},
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	fetched := make(map[string]jiraDataFetchedScope, len(groups))
	for result := range results {
		fetched[result.key] = result.scope
	}
	return fetched, nil
}

type resolvedJiraPullInstance struct {
	name string
	cfg  config.JiraDataCenterInstance
}

func resolvePullJiraDataInstance(cfg config.EffectiveConfig, instanceName string) (resolvedJiraPullInstance, error) {
	name, item, err := config.ResolveJiraDataInstance(cfg, instanceName)
	if err != nil {
		return resolvedJiraPullInstance{}, err
	}
	return resolvedJiraPullInstance{name: name, cfg: item}, nil
}

func requireJiraDataInstance(cfg config.EffectiveConfig, instanceName string) (config.JiraDataCenterInstance, error) {
	_, resolved, err := config.ResolveJiraDataInstance(cfg, instanceName)
	if err != nil {
		return config.JiraDataCenterInstance{}, err
	}
	return resolved, nil
}

type jiraResolvedRoute struct {
	targetInstance string
	targetIssue    string
	reporting      bool
}

type jiraRouteProfile struct {
	routes []jiraResolvedRouteRule
}

type jiraResolvedRouteRule struct {
	prefix         string
	targetInstance string
	targetIssue    string
	reporting      bool
}

func resolveJiraDataRouteProfile(cfg config.EffectiveConfig, name string) (jiraRouteProfile, error) {
	if cfg.File.JiraData == nil || len(cfg.File.JiraData.Instances) == 0 {
		return jiraRouteProfile{}, errors.New("jira_data_center routing is required for push")
	}
	profileName := name
	if profileName == "" {
		profileName = "default"
	}
	routes := make([]jiraResolvedRouteRule, 0)
	hasRouting := false
	found := false
	for instanceName, instance := range cfg.File.JiraData.Instances {
		if instance.Routing == nil {
			continue
		}
		hasRouting = true
		profile, ok := instance.Routing.Profiles[profileName]
		if !ok {
			continue
		}
		found = true
		for _, prefix := range profile.IssuePrefixes {
			routes = append(routes, jiraResolvedRouteRule{
				prefix:         prefix,
				targetInstance: instanceName,
			})
		}
		for prefix, targetIssue := range profile.ReportingTargets {
			routes = append(routes, jiraResolvedRouteRule{
				prefix:         prefix,
				targetInstance: instanceName,
				targetIssue:    targetIssue,
				reporting:      true,
			})
		}
	}
	if !hasRouting {
		return jiraRouteProfile{}, errors.New("jira_data_center routing is required for push")
	}
	if !found {
		return jiraRouteProfile{}, fmt.Errorf("jira_data_center route profile %q is not configured", profileName)
	}
	return jiraRouteProfile{routes: routes}, nil
}

func (p jiraRouteProfile) resolve(issueKey string) (jiraResolvedRoute, bool, error) {
	matches := make([]jiraResolvedRoute, 0, 1)
	for _, route := range p.routes {
		if !strings.HasPrefix(issueKey, route.prefix+"-") {
			continue
		}
		targetIssue := issueKey
		if route.reporting {
			targetIssue = route.targetIssue
		}
		matches = append(matches, jiraResolvedRoute{
			targetInstance: route.targetInstance,
			targetIssue:    targetIssue,
			reporting:      route.reporting,
		})
	}
	if len(matches) == 0 {
		return jiraResolvedRoute{}, false, nil
	}
	if len(matches) > 1 {
		return jiraResolvedRoute{}, false, fmt.Errorf("jira_data_center routing is ambiguous for issue %q", issueKey)
	}
	return matches[0], true, nil
}

func filterJiraWorklogsByUserAndWindow(items []jiradatacenter.Worklog, user jiradatacenter.User, from, to time.Time) []jiradatacenter.Worklog {
	filtered := make([]jiradatacenter.Worklog, 0)
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		startedAt = startedAt.UTC()
		if startedAt.Before(from.UTC()) || startedAt.After(to.UTC()) {
			continue
		}
		if item.Author.AccountID == user.AccountID || (user.AccountID == "" && (item.Author.Key == user.Key || item.Author.Name == user.Name)) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func hasForeignJiraWorklogs(items []jiradatacenter.Worklog, user jiradatacenter.User, from, to time.Time) bool {
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		startedAt = startedAt.UTC()
		if startedAt.Before(from.UTC()) || startedAt.After(to.UTC()) {
			continue
		}
		if !(item.Author.AccountID == user.AccountID || (user.AccountID == "" && (item.Author.Key == user.Key || item.Author.Name == user.Name))) {
			return true
		}
	}
	return false
}
