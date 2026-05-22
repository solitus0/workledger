package reconcile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile/model"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

var ErrPlanNotFound = errors.New("saved plan not found")

type clockifyClient interface {
	ListUserTimeEntries(ctx context.Context, workspaceID, userID string, start, end time.Time) ([]clockify.TimeEntry, error)
	ListTags(ctx context.Context, workspaceID string) (map[string]clockify.Tag, error)
	ListProjects(ctx context.Context, workspaceID string) ([]clockify.Project, error)
	CreateTag(ctx context.Context, workspaceID, name string) (clockify.Tag, error)
	CreateTimeEntry(ctx context.Context, workspaceID string, row model.Row, projectID string, tagIDs []string) (clockify.TimeEntry, error)
	DeleteTimeEntry(ctx context.Context, workspaceID, entryID string) error
}

type jiraDataClient interface {
	CurrentUser(ctx context.Context) (jiradatacenter.User, error)
	SearchIssues(ctx context.Context, jql string, fields []string) ([]jiradatacenter.IssueBrief, error)
	ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiradatacenter.Worklog, error)
	GetIssue(ctx context.Context, issueKey string, fields []string) (jiradatacenter.IssueBrief, error)
	CreateWorklog(ctx context.Context, issueKey string, row model.Row) (jiradatacenter.Worklog, error)
	DeleteWorklog(ctx context.Context, issueKey, worklogID string) error
}

type jiraCloudClient interface {
	CurrentUser(ctx context.Context) (jiracloud.User, error)
	SearchIssues(ctx context.Context, jql string, fields []string) ([]jiracloud.IssueBrief, error)
	ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiracloud.Worklog, error)
	GetIssue(ctx context.Context, issueKey string, fields []string) (jiracloud.IssueBrief, error)
	CreateWorklog(ctx context.Context, issueKey string, row model.Row) (jiracloud.Worklog, error)
	DeleteWorklog(ctx context.Context, issueKey, worklogID string) error
}

type Service struct {
	store              *sqlitestore.Store
	now                func() time.Time
	newClockifyClient  func(cfg config.ClockifyConfig) clockifyClient
	newJiraCloudClient func(cfg config.JiraCloudInstance) jiraCloudClient
	newJiraDataClient  func(cfg config.JiraDataCenterInstance) jiraDataClient
}

type Plan struct {
	ID                string
	Direction         string
	AdapterFamily     string
	AdapterFamilies   []string
	TargetInstances   []string
	ConfigFingerprint string
	WindowFromUTC     time.Time
	WindowToUTC       time.Time
	CreatedAt         time.Time
	AggregateStatus   string
	AppliedAt         *time.Time
	Items             []PlanItem
	Findings          []PlanFinding
}

type PlanItem struct {
	ID                    string
	PlanID                string
	IssueKey              string
	PlanDirection         string
	TargetAdapterFamily   string
	TargetAdapterInstance string
	TargetIssue           string
	RouteProfile          string
	WindowFromUTC         time.Time
	WindowToUTC           time.Time
	PlanStatus            string
	PlannedAction         string
	ComparisonStatus      string
	ReasonCode            string
	ReasonDetail          string
	LocalRowCount         int
	LocalTotal            int
	RemoteRowCount        int
	RemoteTotal           int
	InspectionSummary     InspectionSummary
	DeliveryKey           string
	AppliedState          string
	ExecutionState        string
	AppliedAt             *time.Time
	ApplyMessage          string
	Payload               []model.Row
}

type DeliveryAttempt struct {
	State     string
	Message   string
	CreatedAt time.Time
}

type InspectionSummary struct {
	ProjectName            string         `json:"project_name,omitempty"`
	ProjectID              string         `json:"project_id,omitempty"`
	IssueTagName           string         `json:"issue_tag_name,omitempty"`
	LocalRowCount          int            `json:"local_row_count"`
	LocalTotalSeconds      int            `json:"local_total_seconds"`
	RemoteRowCount         int            `json:"remote_row_count"`
	RemoteTotalSeconds     int            `json:"remote_total_seconds"`
	RequiresTagCreate      bool           `json:"requires_tag_create,omitempty"`
	SourceIssueKeys        []string       `json:"source_issue_keys,omitempty"`
	PerSourceTotals        map[string]int `json:"per_source_totals,omitempty"`
	ResolvedTargetInstance string         `json:"resolved_target_instance,omitempty"`
	ResolvedTargetIssue    string         `json:"resolved_target_issue,omitempty"`
	ForeignAuthorPresent   bool           `json:"foreign_author_present,omitempty"`
}

type reportingScopeGroup struct {
	TargetAdapterFamily   string
	TargetAdapterInstance string
	TargetIssue           string
	SourceIssueKeys       []string
	PerSourceTotals       map[string]int
	DesiredRows           []model.Row
	HasTombstones         bool
}

type PlanFinding struct {
	ID           string
	PlanID       string
	SourceRowID  string
	ReasonCode   string
	ReasonDetail string
	Payload      model.InvalidRow
}

type ListEntry struct {
	ID              string
	Direction       string
	AdapterFamily   string
	AdapterFamilies []string
	TargetInstances []string
	WindowFromUTC   time.Time
	WindowToUTC     time.Time
	CreatedAt       time.Time
	AggregateStatus string
	TotalItems      int
	ReadyItems      int
	SucceededItems  int
}

type ApplyResult struct {
	PlanID       string
	RetryScope   string
	AppliedCount int
	SkippedCount int
	FailedCount  int
	MixedResult  bool
	NoOp         bool
}

type ReconcileResult struct {
	Plan   *Plan
	NoPlan *ReconcileNoPlanResult
}

type ReconcileNoPlanResult struct {
	PlanCreated             bool
	AdapterFamily           string
	AdapterFamilies         []string
	RouteProfile            string
	WindowFromUTC           time.Time
	WindowToUTC             time.Time
	ResolvedTargetInstances []string
	MatchedScopeCount       int
	ActionableScopeCount    int
	Reason                  string
}

func NewService(store *sqlitestore.Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
		newClockifyClient: func(cfg config.ClockifyConfig) clockifyClient {
			return clockify.NewClient(cfg.Auth.APIKey)
		},
		newJiraCloudClient: func(cfg config.JiraCloudInstance) jiraCloudClient {
			return jiracloud.NewClient(cfg.BaseURL, cfg.Auth.Email, cfg.Auth.Token)
		},
		newJiraDataClient: func(cfg config.JiraDataCenterInstance) jiraDataClient {
			return jiradatacenter.NewClient(cfg.BaseURL, cfg.Auth.Bearer.Token)
		},
	}
}

type ReconcileScope struct {
	AdapterFamilies []string
	Instances       []string
}

func normalizePlanSummary(plan *Plan) {
	if plan == nil {
		return
	}
	if len(plan.AdapterFamilies) == 0 {
		plan.AdapterFamilies = collectPlanAdapterFamilies(plan.Items)
	}
	if len(plan.TargetInstances) == 0 {
		plan.TargetInstances = collectPlanTargetInstances(plan.Items)
	}
	switch len(plan.AdapterFamilies) {
	case 0:
		if plan.AdapterFamily == "" {
			plan.AdapterFamily = "multiple"
		}
	case 1:
		plan.AdapterFamily = plan.AdapterFamilies[0]
	default:
		plan.AdapterFamily = "multiple"
	}
}

func collectPlanAdapterFamilies(items []PlanItem) []string {
	set := map[string]struct{}{}
	for _, item := range items {
		if item.TargetAdapterFamily == "" {
			continue
		}
		set[item.TargetAdapterFamily] = struct{}{}
	}
	return sortedSetKeys(set)
}

func collectPlanTargetInstances(items []PlanItem) []string {
	set := map[string]struct{}{}
	for _, item := range items {
		if item.TargetAdapterInstance == "" {
			continue
		}
		set[item.TargetAdapterInstance] = struct{}{}
	}
	return sortedSetKeys(set)
}

func sortedSetKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func scopeSelectsClockify(instances []string) bool {
	if len(instances) == 0 {
		return true
	}
	for _, instance := range instances {
		if instance == config.ClockifyInstanceName {
			return true
		}
	}
	return false
}

func filterJiraScopeConfig(cfg config.EffectiveConfig, family string, instances []string) config.EffectiveConfig {
	filtered := cfg
	allowed := map[string]struct{}{}
	for _, instance := range instances {
		allowed[instance] = struct{}{}
	}
	switch family {
	case "jira-cloud":
		if cfg.File.JiraCloud == nil {
			return filtered
		}
		copied := *cfg.File.JiraCloud
		copied.Instances = map[string]config.JiraCloudInstance{}
		for name, instance := range cfg.File.JiraCloud.Instances {
			if len(allowed) > 0 {
				if _, ok := allowed[name]; !ok {
					continue
				}
			}
			copied.Instances[name] = instance
		}
		filtered.File.JiraCloud = &copied
	case "jira-data-center":
		if cfg.File.JiraData == nil {
			return filtered
		}
		copied := *cfg.File.JiraData
		copied.Instances = map[string]config.JiraDataCenterInstance{}
		for name, instance := range cfg.File.JiraData.Instances {
			if len(allowed) > 0 {
				if _, ok := allowed[name]; !ok {
					continue
				}
			}
			copied.Instances[name] = instance
		}
		filtered.File.JiraData = &copied
	}
	return filtered
}

func mergePlans(direction string, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, plans []Plan) (Plan, error) {
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	merged := Plan{
		ID:                uuid.NewString(),
		Direction:         direction,
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         time.Now().UTC(),
	}
	items := make([]PlanItem, 0)
	findings := make([]PlanFinding, 0)
	for _, plan := range plans {
		for _, item := range plan.Items {
			item.PlanID = merged.ID
			items = append(items, item)
		}
		for _, finding := range plan.Findings {
			finding.PlanID = merged.ID
			findings = append(findings, finding)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TargetIssue != items[j].TargetIssue {
			return items[i].TargetIssue < items[j].TargetIssue
		}
		if items[i].TargetAdapterFamily != items[j].TargetAdapterFamily {
			return items[i].TargetAdapterFamily < items[j].TargetAdapterFamily
		}
		if items[i].TargetAdapterInstance != items[j].TargetAdapterInstance {
			return items[i].TargetAdapterInstance < items[j].TargetAdapterInstance
		}
		if !items[i].WindowFromUTC.Equal(items[j].WindowFromUTC) {
			return items[i].WindowFromUTC.Before(items[j].WindowFromUTC)
		}
		return items[i].ID < items[j].ID
	})
	merged.Items = items
	merged.Findings = findings
	merged.AggregateStatus = deriveAggregateStatus(items, findings)
	normalizePlanSummary(&merged)
	return merged, nil
}

func (s *Service) CreateMultiPullPlan(ctx context.Context, cfg config.EffectiveConfig, scope ReconcileScope, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	plans := make([]Plan, 0, len(scope.AdapterFamilies))
	for _, family := range scope.AdapterFamilies {
		switch family {
		case "clockify":
			if !scopeSelectsClockify(scope.Instances) {
				continue
			}
			plan, err := s.buildClockifyPullPlan(ctx, cfg, windowFrom, windowTo, options...)
			if err != nil {
				failure, ok := classifyPullPlanFailure(err)
				if !ok {
					return Plan{}, err
				}
				plan, err = s.buildCheckFailedPullPlan(cfg, family, config.ClockifyInstanceName, windowFrom, windowTo, failure)
				if err != nil {
					return Plan{}, err
				}
			}
			plans = append(plans, plan)
		case "jira-cloud":
			filtered := filterJiraScopeConfig(cfg, family, scope.Instances)
			instances := make([]string, 0, len(filtered.File.JiraCloud.Instances))
			for instance := range filtered.File.JiraCloud.Instances {
				instances = append(instances, instance)
			}
			sort.Strings(instances)
			for _, instance := range instances {
				plan, err := s.buildJiraCloudPullPlan(ctx, filtered, instance, windowFrom, windowTo, options...)
				if err != nil {
					failure, ok := classifyPullPlanFailure(err)
					if !ok {
						return Plan{}, err
					}
					plan, err = s.buildCheckFailedPullPlan(cfg, family, instance, windowFrom, windowTo, failure)
					if err != nil {
						return Plan{}, err
					}
				}
				plans = append(plans, plan)
			}
		case "jira-data-center":
			filtered := filterJiraScopeConfig(cfg, family, scope.Instances)
			instances := make([]string, 0, len(filtered.File.JiraData.Instances))
			for instance := range filtered.File.JiraData.Instances {
				instances = append(instances, instance)
			}
			sort.Strings(instances)
			for _, instance := range instances {
				plan, err := s.buildJiraDataPullPlan(ctx, filtered, instance, windowFrom, windowTo, options...)
				if err != nil {
					failure, ok := classifyPullPlanFailure(err)
					if !ok {
						return Plan{}, err
					}
					plan, err = s.buildCheckFailedPullPlan(cfg, family, instance, windowFrom, windowTo, failure)
					if err != nil {
						return Plan{}, err
					}
				}
				plans = append(plans, plan)
			}
		default:
			return Plan{}, fmt.Errorf("unsupported adapter family %q", family)
		}
	}
	if len(plans) == 1 {
		if err := s.insertPlan(plans[0]); err != nil {
			return Plan{}, err
		}
		return plans[0], nil
	}
	merged, err := mergePlans("pull", cfg, windowFrom, windowTo, plans)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(merged); err != nil {
		return Plan{}, err
	}
	return merged, nil
}

type pullPlanFailure struct {
	reasonCode   string
	reasonDetail string
}

func classifyPullPlanFailure(err error) (pullPlanFailure, bool) {
	var clockifyErr *clockify.RequestError
	if errors.As(err, &clockifyErr) {
		switch clockifyErr.StatusCode {
		case 401, 403:
			return pullPlanFailure{reasonCode: "auth_error", reasonDetail: clockifyErr.Error()}, true
		default:
			return pullPlanFailure{reasonCode: "remote_error", reasonDetail: clockifyErr.Error()}, true
		}
	}

	var jiraCloudErr *jiracloud.RequestError
	if errors.As(err, &jiraCloudErr) {
		switch jiraCloudErr.StatusCode {
		case 401, 403:
			return pullPlanFailure{reasonCode: "auth_error", reasonDetail: "jira cloud authentication failed"}, true
		case 404:
			return pullPlanFailure{reasonCode: "not_found", reasonDetail: "jira cloud resource not found"}, true
		default:
			return pullPlanFailure{reasonCode: "remote_error", reasonDetail: jiraCloudErr.Error()}, true
		}
	}

	var jiraDataErr *jiradatacenter.RequestError
	if errors.As(err, &jiraDataErr) {
		switch jiraDataErr.StatusCode {
		case 401, 403:
			return pullPlanFailure{reasonCode: "auth_error", reasonDetail: "jira data center authentication failed"}, true
		case 404:
			return pullPlanFailure{reasonCode: "not_found", reasonDetail: "jira data center resource not found"}, true
		default:
			return pullPlanFailure{reasonCode: "remote_error", reasonDetail: jiraDataErr.Error()}, true
		}
	}

	return pullPlanFailure{}, false
}

func (s *Service) buildCheckFailedPullPlan(cfg config.EffectiveConfig, family, instance string, windowFrom, windowTo time.Time, failure pullPlanFailure) (Plan, error) {
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "pull",
		AdapterFamily:     family,
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "check_failed",
	}

	scopeID := instance
	if scopeID == "" {
		scopeID = family
	}
	item := PlanItem{
		ID:                    uuid.NewString(),
		PlanID:                plan.ID,
		IssueKey:              scopeID,
		PlanDirection:         "pull",
		TargetAdapterFamily:   family,
		TargetAdapterInstance: instance,
		TargetIssue:           scopeID,
		WindowFromUTC:         plan.WindowFromUTC,
		WindowToUTC:           plan.WindowToUTC,
		PlanStatus:            "check_failed",
		PlannedAction:         "none",
		ComparisonStatus:      "check_failed",
		ReasonCode:            failure.reasonCode,
		ReasonDetail:          failure.reasonDetail,
		AppliedState:          "not_attempted",
		InspectionSummary: InspectionSummary{
			ResolvedTargetInstance: instance,
			ResolvedTargetIssue:    scopeID,
		},
	}
	item.DeliveryKey = buildDeliveryKey(item)
	plan.Items = []PlanItem{item}
	normalizePlanSummary(&plan)
	return plan, nil
}

func (s *Service) ReconcileMultiPushPlan(ctx context.Context, cfg config.EffectiveConfig, scope ReconcileScope, routeProfile string, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (ReconcileResult, error) {
	plans := make([]Plan, 0, len(scope.AdapterFamilies))
	noPlanInstances := map[string]struct{}{}
	noPlanFamilies := map[string]struct{}{}
	noPlanReason := ""
	var lastNoPlan *ReconcileNoPlanResult
	allNoPlan := true
	for _, family := range scope.AdapterFamilies {
		switch family {
		case "clockify":
			if !scopeSelectsClockify(scope.Instances) {
				continue
			}
			plan, err := s.buildClockifyPushPlan(ctx, cfg, windowFrom, windowTo, onlyDeleted, options...)
			if err != nil {
				return ReconcileResult{}, err
			}
			plans = append(plans, plan)
			allNoPlan = false
		case "jira-cloud":
			filtered := filterJiraScopeConfig(cfg, family, scope.Instances)
			result, err := s.reconcileJiraCloudPushPlanInMemory(ctx, filtered, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
			if err != nil {
				return ReconcileResult{}, err
			}
			if result.NoPlan != nil {
				lastNoPlan = result.NoPlan
				noPlanReason = result.NoPlan.Reason
				noPlanFamilies[family] = struct{}{}
				for _, instance := range result.NoPlan.ResolvedTargetInstances {
					noPlanInstances[instance] = struct{}{}
				}
				continue
			}
			allNoPlan = false
			if result.Plan != nil {
				plans = append(plans, *result.Plan)
			}
		case "jira-data-center":
			filtered := filterJiraScopeConfig(cfg, family, scope.Instances)
			result, err := s.reconcileJiraDataPushPlanInMemory(ctx, filtered, routeProfile, windowFrom, windowTo, onlyDeleted, options...)
			if err != nil {
				return ReconcileResult{}, err
			}
			if result.NoPlan != nil {
				lastNoPlan = result.NoPlan
				noPlanReason = result.NoPlan.Reason
				noPlanFamilies[family] = struct{}{}
				for _, instance := range result.NoPlan.ResolvedTargetInstances {
					noPlanInstances[instance] = struct{}{}
				}
				continue
			}
			allNoPlan = false
			if result.Plan != nil {
				plans = append(plans, *result.Plan)
			}
		default:
			return ReconcileResult{}, fmt.Errorf("unsupported adapter family %q", family)
		}
	}
	if allNoPlan {
		if len(scope.AdapterFamilies) == 1 && lastNoPlan != nil {
			return ReconcileResult{NoPlan: lastNoPlan}, nil
		}
		families := sortedSetKeys(noPlanFamilies)
		adapterFamily := "multiple"
		if len(families) == 1 {
			adapterFamily = families[0]
		}
		return ReconcileResult{NoPlan: &ReconcileNoPlanResult{
			PlanCreated:             false,
			AdapterFamily:           adapterFamily,
			AdapterFamilies:         families,
			RouteProfile:            routeProfile,
			WindowFromUTC:           windowFrom.UTC(),
			WindowToUTC:             windowTo.UTC(),
			ResolvedTargetInstances: sortedSetKeys(noPlanInstances),
			Reason:                  noPlanReason,
		}}, nil
	}
	if len(plans) == 1 {
		if err := s.insertPlan(plans[0]); err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{Plan: &plans[0]}, nil
	}
	merged, err := mergePlans("push", cfg, windowFrom, windowTo, plans)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := s.insertPlan(merged); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Plan: &merged}, nil
}

func (s *Service) buildClockifyPullPlanFromRows(cfg config.EffectiveConfig, windowFrom, windowTo time.Time, rows []model.Row, invalidRows []model.InvalidRow) (Plan, error) {
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}

	grouped := map[string][]model.Row{}
	for _, row := range rows {
		grouped[row.IssueKey] = append(grouped[row.IssueKey], row)
	}

	issues := sortedKeys(grouped)
	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "pull",
		AdapterFamily:     "clockify",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}

	findings := make([]PlanFinding, 0, len(invalidRows))
	for _, item := range invalidRows {
		findings = append(findings, PlanFinding{
			ID:           uuid.NewString(),
			PlanID:       plan.ID,
			SourceRowID:  item.SourceRowID,
			ReasonCode:   item.ReasonCode,
			ReasonDetail: item.ReasonDetail,
			Payload:      item,
		})
	}
	plan.Findings = findings

	items := make([]PlanItem, 0, len(issues))
	for _, issueKey := range issues {
		remoteRows := sortRows(grouped[issueKey])
		localRows, err := s.listLocalScope(issueKey, windowFrom, windowTo)
		if err != nil {
			return Plan{}, err
		}

		localPayload := localToRows(localRows)
		item := newPlanItem(plan, issueKey, localPayload)
		item.TargetAdapterInstance = config.ClockifyInstanceName
		item.PlanStatus = "ready"
		item.PlannedAction = "merge"
		item.ComparisonStatus = "merge_needed"
		item.ReasonCode = "remote_diff"
		item.ReasonDetail = "Clockify rows differ from the local ledger"
		item.RemoteRowCount = len(remoteRows)
		item.RemoteTotal = sumRows(remoteRows)
		item.Payload = remoteRows

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
			item.ReasonDetail = "Clockify rows already match the local ledger"
		}
		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}

	plan.Items = items
	plan.AggregateStatus = deriveAggregateStatus(items, findings)
	normalizePlanSummary(&plan)
	return plan, nil
}

func (s *Service) CreateClockifyPushPlan(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildClockifyPushPlan(ctx, cfg, windowFrom, windowTo, onlyDeleted, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildClockifyPushPlan(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, onlyDeleted bool, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return Plan{}, err
	}
	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ID:                uuid.NewString(),
		Direction:         "push",
		AdapterFamily:     "clockify",
		ConfigFingerprint: fingerprint,
		WindowFromUTC:     windowFrom.UTC(),
		WindowToUTC:       windowTo.UTC(),
		CreatedAt:         s.now().UTC(),
		AggregateStatus:   "ready",
	}

	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile clockify push"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile clockify push complete"})
	}()

	client := s.newClockifyClient(*clockifyCfg)
	entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	tagsByID, err := client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return Plan{}, err
	}
	projects, err := client.ListProjects(ctx, clockifyCfg.WorkspaceID)
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
	if len(activeRows) == 0 && len(tombstoneRows) == 0 {
		plan.Items = nil
		plan.AggregateStatus = "ready"
		normalizePlanSummary(&plan)
		return plan, nil
	}

	issueKeys := unionKeys(activeRows, tombstoneRows)
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

		item := newPlanItem(plan, issueKey, localPayload)
		item.TargetAdapterFamily = "clockify"
		item.TargetAdapterInstance = config.ClockifyInstanceName
		item.TargetIssue = issueKey
		item.InspectionSummary = InspectionSummary{
			IssueTagName:       issueKey,
			LocalRowCount:      len(activePayload),
			LocalTotalSeconds:  sumRows(activePayload),
			RemoteRowCount:     0,
			RemoteTotalSeconds: 0,
		}
		applyActiveLocalMetrics(&item, activePayload)

		projectName := config.ResolveClockifyProjectName(clockifyCfg.ProjectMapping, issueKey)
		if projectName == "" {
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "missing_project_mapping"
			item.ReasonDetail = "Clockify project mapping is missing for this issue scope"
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}

		projectMatches := matchingProjects(projects, projectName)
		if len(projectMatches) == 0 {
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "project_not_found"
			item.ReasonDetail = "Configured Clockify project is not available in the workspace"
			item.InspectionSummary.ProjectName = projectName
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}
		if len(projectMatches) > 1 {
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "project_ambiguous"
			item.ReasonDetail = "Configured Clockify project name matches multiple workspace projects"
			item.InspectionSummary.ProjectName = projectName
			item.DeliveryKey = buildDeliveryKey(item)
			items = append(items, item)
			continue
		}

		project := projectMatches[0]
		item.InspectionSummary.ProjectName = project.Name
		item.InspectionSummary.ProjectID = project.ID

		tag, tagExists := findTagByName(tagsByID, issueKey)
		if tagExists {
			item.InspectionSummary.IssueTagName = tag.Name
		}
		item.InspectionSummary.RequiresTagCreate = !tagExists && !isDeleteOnly && clockifyCreateIssueTagIfMissing(clockifyCfg)

		issueEntries := filterEntriesByIssue(entries, tagsByID, issueKey)
		targetEntries := filterEntriesByProject(issueEntries, project.ID)
		targetRows, _ := clockify.NormalizeEntries(targetEntries, tagsByID)
		targetRows = sortRows(targetRows)

		item.RemoteRowCount = len(targetRows)
		item.RemoteTotal = sumRows(targetRows)
		item.InspectionSummary.RemoteRowCount = item.RemoteRowCount
		item.InspectionSummary.RemoteTotalSeconds = item.RemoteTotal

		switch {
		case isDeleteOnly:
			if len(targetEntries) == 0 {
				item.PlanStatus = "skipped"
				item.PlannedAction = "none"
				item.ComparisonStatus = "match"
				item.ReasonCode = "exact_match"
				item.ReasonDetail = "Clockify scope already matches the local empty state"
			} else {
				item.PlanStatus = "ready"
				item.PlannedAction = "delete"
				item.ComparisonStatus = "remote_present"
				item.ReasonCode = "remote_present"
				item.ReasonDetail = "Clockify scope contains rows that should be deleted"
			}
		case !tagExists && !clockifyCreateIssueTagIfMissing(clockifyCfg):
			item.PlanStatus = "blocked"
			item.PlannedAction = "none"
			item.ComparisonStatus = "not_checked"
			item.ReasonCode = "issue_tag_missing"
			item.ReasonDetail = "Clockify issue tag is missing and config forbids creating it"
		case rowsEqual(localPayload, targetRows) && len(issueEntries) == len(targetEntries):
			item.PlanStatus = "skipped"
			item.PlannedAction = "none"
			item.ComparisonStatus = "match"
			item.ReasonCode = "exact_match"
			item.ReasonDetail = "Clockify rows already match the local ledger in the target project"
		case len(targetEntries) == 0 && len(issueEntries) == 0:
			item.PlanStatus = "ready"
			item.PlannedAction = "create"
			item.ComparisonStatus = "remote_missing"
			item.ReasonCode = "remote_missing"
			item.ReasonDetail = "Clockify scope is empty and will be created"
		default:
			item.PlanStatus = "ready"
			item.PlannedAction = "replace"
			item.ComparisonStatus = "remote_diff"
			item.ReasonCode = "remote_diff"
			item.ReasonDetail = "Clockify scope differs from the saved local payload"
		}

		item.DeliveryKey = buildDeliveryKey(item)
		items = append(items, item)
	}

	plan.Items = items
	plan.AggregateStatus = deriveAggregateStatus(items, nil)
	normalizePlanSummary(&plan)
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(items), ScopeTotal: len(items), Message: "built clockify push plan"})
	return plan, nil
}

func (s *Service) LoadPlan(id string) (Plan, error) {
	var rowID string
	var direction string
	var adapter string
	var adapterFamiliesJSON string
	var targetInstancesJSON string
	var fingerprint string
	var windowFrom string
	var windowTo string
	var createdAt string
	var aggregate string
	var appliedAt sql.NullString

	query := `SELECT id, plan_direction, adapter_family, adapter_families_json, target_instances_json, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status, applied_at FROM saved_plans`
	args := []any{}
	if id != "" {
		query += ` WHERE id = ?`
		args = append(args, id)
	} else {
		query += ` ORDER BY created_at DESC, id DESC LIMIT 1`
	}

	err := s.store.DB().QueryRow(query, args...).Scan(&rowID, &direction, &adapter, &adapterFamiliesJSON, &targetInstancesJSON, &fingerprint, &windowFrom, &windowTo, &createdAt, &aggregate, &appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ID:                rowID,
		Direction:         direction,
		AdapterFamily:     adapter,
		ConfigFingerprint: fingerprint,
		AggregateStatus:   aggregate,
	}
	_ = json.Unmarshal([]byte(adapterFamiliesJSON), &plan.AdapterFamilies)
	_ = json.Unmarshal([]byte(targetInstancesJSON), &plan.TargetInstances)
	plan.WindowFromUTC, _ = time.Parse(time.RFC3339, windowFrom)
	plan.WindowToUTC, _ = time.Parse(time.RFC3339, windowTo)
	plan.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if appliedAt.Valid {
		t, _ := time.Parse(time.RFC3339, appliedAt.String)
		plan.AppliedAt = &t
	}

	items, err := s.loadPlanItems(plan.ID)
	if err != nil {
		return Plan{}, err
	}
	findings, err := s.loadPlanFindings(plan.ID)
	if err != nil {
		return Plan{}, err
	}
	plan.Items = items
	plan.Findings = findings
	normalizePlanSummary(&plan)
	return plan, nil
}

func (s *Service) ListPlans() ([]ListEntry, error) {
	rows, err := s.store.DB().Query(`
		SELECT
			p.id,
			p.plan_direction,
			p.adapter_family,
			p.adapter_families_json,
			p.target_instances_json,
			p.window_from_utc,
			p.window_to_utc,
			p.created_at,
			p.aggregate_status,
			COUNT(i.id),
			COALESCE(SUM(CASE WHEN i.plan_status = 'ready' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN i.applied_state = 'succeeded' THEN 1 ELSE 0 END), 0)
		FROM saved_plans p
		LEFT JOIN saved_plan_items i ON i.plan_id = p.id
		GROUP BY p.id, p.plan_direction, p.adapter_family, p.adapter_families_json, p.target_instances_json, p.created_at, p.aggregate_status
		ORDER BY p.created_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ListEntry, 0)
	for rows.Next() {
		var entry ListEntry
		var adapterFamiliesJSON string
		var targetInstancesJSON string
		var windowFromUTC string
		var windowToUTC string
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.Direction, &entry.AdapterFamily, &adapterFamiliesJSON, &targetInstancesJSON, &windowFromUTC, &windowToUTC, &createdAt, &entry.AggregateStatus, &entry.TotalItems, &entry.ReadyItems, &entry.SucceededItems); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(adapterFamiliesJSON), &entry.AdapterFamilies)
		_ = json.Unmarshal([]byte(targetInstancesJSON), &entry.TargetInstances)
		entry.WindowFromUTC, _ = time.Parse(time.RFC3339, windowFromUTC)
		entry.WindowToUTC, _ = time.Parse(time.RFC3339, windowToUTC)
		entry.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if len(items[i].AdapterFamilies) == 0 && items[i].AdapterFamily != "" {
			items[i].AdapterFamilies = []string{items[i].AdapterFamily}
		}
		if len(items[i].AdapterFamilies) > 1 {
			items[i].AdapterFamily = "multiple"
		}
	}
	return items, nil
}

func (s *Service) ApplyPlan(cfg config.EffectiveConfig, id string, options ...ApplyOptions) (ApplyResult, error) {
	return s.executeSavedPlan(cfg, id, "", func(item PlanItem) bool {
		return item.PlanStatus == "ready" && item.ExecutionState == "not_attempted"
	}, options...)
}

func (s *Service) RetryPlan(cfg config.EffectiveConfig, id string, only string, options ...ApplyOptions) (ApplyResult, error) {
	switch only {
	case "failed":
		return s.executeSavedPlan(cfg, id, only, func(item PlanItem) bool {
			return item.PlanStatus == "ready" && item.ExecutionState == "failed"
		}, options...)
	case "uncertain":
		return s.executeSavedPlan(cfg, id, only, func(item PlanItem) bool {
			return item.PlanStatus == "ready" && item.ExecutionState == "uncertain"
		}, options...)
	default:
		return ApplyResult{}, fmt.Errorf("retry scope must be failed or uncertain")
	}
}

func (s *Service) executeSavedPlan(cfg config.EffectiveConfig, id, retryScope string, selectItem func(PlanItem) bool, options ...ApplyOptions) (ApplyResult, error) {
	opts := resolveApplyOptions(options)
	plan, err := s.LoadPlan(id)
	if err != nil {
		return ApplyResult{}, err
	}

	fingerprint, err := config.FingerprintEffective(cfg)
	if err != nil {
		return ApplyResult{}, err
	}
	if fingerprint != plan.ConfigFingerprint {
		return ApplyResult{}, fmt.Errorf("saved plan config fingerprint does not match current config")
	}

	ready := make([]PlanItem, 0)
	skipped := 0
	for _, item := range plan.Items {
		if selectItem(item) {
			ready = append(ready, item)
			continue
		}
		skipped++
	}
	if len(ready) == 0 {
		return ApplyResult{PlanID: plan.ID, RetryScope: retryScope, SkippedCount: skipped, NoOp: true}, nil
	}

	sort.Slice(ready, func(i, j int) bool {
		if ready[i].TargetIssue != ready[j].TargetIssue {
			return ready[i].TargetIssue < ready[j].TargetIssue
		}
		if ready[i].TargetAdapterFamily != ready[j].TargetAdapterFamily {
			return ready[i].TargetAdapterFamily < ready[j].TargetAdapterFamily
		}
		if ready[i].TargetAdapterInstance != ready[j].TargetAdapterInstance {
			return ready[i].TargetAdapterInstance < ready[j].TargetAdapterInstance
		}
		if !ready[i].WindowFromUTC.Equal(ready[j].WindowFromUTC) {
			return ready[i].WindowFromUTC.Before(ready[j].WindowFromUTC)
		}
		return ready[i].ID < ready[j].ID
	})

	result := ApplyResult{PlanID: plan.ID, RetryScope: retryScope, SkippedCount: skipped}
	opts.Reporter.Start(progress.Event{
		Phase:      "applying",
		ScopeDone:  0,
		ScopeTotal: len(ready),
		WorkDone:   0,
		WorkTotal:  totalApplyWorkUnits(ready),
		Message:    "plan apply",
	})

	pullItems, pushItems := splitPlanExecutionItems(ready)
	var scopeDone int
	var workDone int
	for _, item := range pullItems {
		executed, failed, err := s.executePullItem(item)
		if err != nil {
			return ApplyResult{}, err
		}
		scopeDone++
		workDone += applyWorkUnits(item)
		if executed {
			result.AppliedCount++
		}
		if failed {
			result.FailedCount++
		}
		opts.Reporter.Event(progress.Event{
			Phase:         "applying",
			ScopeLabel:    planItemScopeLabel(item),
			PlannedAction: item.PlannedAction,
			ScopeDone:     scopeDone,
			ScopeTotal:    len(ready),
			WorkDone:      workDone,
			WorkTotal:     totalApplyWorkUnits(ready),
			Failed:        result.FailedCount,
			Message:       "applied local pull scope",
		})
	}
	if len(pushItems) > 0 {
		pushResult, err := s.executeSavedPushGroups(context.Background(), cfg, retryScope, pushItems, scopeDone, workDone, len(ready), totalApplyWorkUnits(ready), opts.Reporter)
		if err != nil {
			return ApplyResult{}, err
		}
		result.AppliedCount += pushResult.appliedCount
		result.FailedCount += pushResult.failedCount
		scopeDone = pushResult.scopeDone
		workDone = pushResult.workDone
	}

	appliedAt := s.now().UTC()
	if err := s.markPlanApplied(plan.ID, appliedAt); err != nil {
		return ApplyResult{}, err
	}
	result.MixedResult = result.AppliedCount > 0 && result.FailedCount > 0
	opts.Reporter.Finish(progress.Event{
		Phase:      "finalizing",
		ScopeDone:  scopeDone,
		ScopeTotal: len(ready),
		WorkDone:   workDone,
		WorkTotal:  totalApplyWorkUnits(ready),
		Failed:     result.FailedCount,
		Message:    "plan apply complete",
	})
	return result, nil
}

func (s *Service) executeSavedPlanItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem, retryScope string) (bool, bool, error) {
	switch item.PlanDirection {
	case "", "pull":
		return s.executePullItem(item)
	case "push":
		return s.executePushItem(ctx, cfg, item, retryScope)
	default:
		return false, false, fmt.Errorf("unsupported plan direction %q", item.PlanDirection)
	}
}

func (s *Service) executePullItem(item PlanItem) (bool, bool, error) {
	appliedAt := s.now().UTC()
	if err := s.applyPullItem(item); err != nil {
		return false, false, err
	}
	if err := s.markItemApplied(item.ID, appliedAt, "succeeded", "merged saved pull payload into local ledger"); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *Service) executePushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem, retryScope string) (bool, bool, error) {
	appliedAt := s.now().UTC()
	if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "pending", "push delivery started"); err != nil {
		return false, false, err
	}

	if retryScope == "uncertain" {
		reconciledState, message, err := s.reconcileUncertainPushItem(ctx, cfg, item)
		if err != nil {
			return false, false, err
		}
		switch reconciledState {
		case "succeeded":
			if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "succeeded", message); err != nil {
				return false, false, err
			}
			if err := s.markItemApplied(item.ID, appliedAt, "succeeded", message); err != nil {
				return false, false, err
			}
			return true, false, nil
		case "uncertain":
			if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "uncertain", message); err != nil {
				return false, false, err
			}
			if err := s.markItemApplied(item.ID, appliedAt, "uncertain", message); err != nil {
				return false, false, err
			}
			return false, true, nil
		}
	}

	err := s.applyPushItem(ctx, cfg, item)
	if err != nil {
		finalState := "failed"
		if retryScope == "uncertain" {
			finalState = "uncertain"
		}
		if attemptErr := s.recordDeliveryAttempt(item.PlanID, item.ID, finalState, err.Error()); attemptErr != nil {
			return false, false, attemptErr
		}
		if markErr := s.markItemApplied(item.ID, appliedAt, finalState, err.Error()); markErr != nil {
			return false, false, markErr
		}
		return false, true, nil
	}

	message := pushApplySuccessMessage(item.TargetAdapterFamily)
	if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "succeeded", "push delivery succeeded"); err != nil {
		return false, false, err
	}
	if err := s.markItemApplied(item.ID, appliedAt, "succeeded", message); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *Service) applyPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) error {
	switch item.TargetAdapterFamily {
	case "clockify":
		return s.applyClockifyPushItem(ctx, cfg, item)
	case "jira-cloud":
		return s.applyJiraCloudPushItem(ctx, cfg, item)
	case "jira-data-center":
		return s.applyJiraDataPushItem(ctx, cfg, item)
	default:
		return fmt.Errorf("unsupported target adapter family %q", item.TargetAdapterFamily)
	}
}

func (s *Service) applyPullItem(item PlanItem) error {
	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`DELETE FROM worklogs WHERE issue_key = ? AND started_at_utc >= ? AND started_at_utc <= ?`,
		item.IssueKey,
		sqlitestore.RFC3339UTC(item.WindowFromUTC),
		sqlitestore.RFC3339UTC(item.WindowToUTC),
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	now := sqlitestore.RFC3339UTC(s.now().UTC())
	for _, row := range item.Payload {
		if _, err := tx.Exec(
			`INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(),
			row.IssueKey,
			sqlitestore.RFC3339UTC(row.StartedAtUTC),
			row.DurationSeconds,
			row.Description,
			now,
			now,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (s *Service) reconcileUncertainPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) (string, string, error) {
	remoteRows, err := s.loadCurrentPushScope(ctx, cfg, item)
	if err != nil {
		return "", "", err
	}

	intended := item.Payload
	if item.PlannedAction == "delete" {
		intended = nil
	}
	if rowsEquivalent(sortRows(remoteRows), sortRows(intended)) {
		return "succeeded", "remote scope already matches the saved intended end state", nil
	}

	if len(remoteRows) == 0 && (item.PlannedAction == "create" || item.PlannedAction == "replace") {
		return "", "", nil
	}

	return "uncertain", "remote scope is ambiguous; retry did not replay automatically", nil
}

func (s *Service) loadCurrentPushScope(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) ([]model.Row, error) {
	switch item.TargetAdapterFamily {
	case "clockify":
		return s.loadCurrentClockifyScope(ctx, cfg, item)
	case "jira-cloud":
		return s.loadCurrentJiraCloudScope(ctx, cfg, item)
	case "jira-data-center":
		return s.loadCurrentJiraDataScope(ctx, cfg, item)
	default:
		return nil, fmt.Errorf("unsupported target adapter family %q", item.TargetAdapterFamily)
	}
}

func (s *Service) loadCurrentClockifyScope(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) ([]model.Row, error) {
	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return nil, err
	}
	client := s.newClockifyClient(*clockifyCfg)
	projects, err := client.ListProjects(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return nil, err
	}
	projectMatches := matchingProjects(projects, item.InspectionSummary.ProjectName)
	if len(projectMatches) != 1 {
		return nil, fmt.Errorf("saved target project %q is no longer resolvable", item.InspectionSummary.ProjectName)
	}
	project := projectMatches[0]

	tagsByID, err := client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return nil, err
	}
	entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, item.WindowFromUTC, item.WindowToUTC)
	if err != nil {
		return nil, err
	}
	issueEntries := filterEntriesByIssue(entries, tagsByID, item.TargetIssue)
	targetEntries := filterEntriesByProject(issueEntries, project.ID)
	rows, _ := clockify.NormalizeEntries(targetEntries, tagsByID)
	return sortRows(rows), nil
}

func (s *Service) loadCurrentJiraDataScope(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) ([]model.Row, error) {
	instance, err := requireJiraDataInstance(cfg, item.TargetAdapterInstance)
	if err != nil {
		return nil, err
	}
	client := s.newJiraDataClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	worklogs, err := client.ListIssueWorklogs(ctx, item.TargetIssue)
	if err != nil {
		return nil, err
	}
	return sortRows(normalizeJiraDataScope(filterJiraWorklogsByUserAndWindow(worklogs, user, item.WindowFromUTC, item.WindowToUTC))), nil
}

func (s *Service) loadCurrentJiraCloudScope(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) ([]model.Row, error) {
	instance, err := requireJiraCloudInstance(cfg, item.TargetAdapterInstance)
	if err != nil {
		return nil, err
	}
	client := s.newJiraCloudClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	worklogs, err := client.ListIssueWorklogs(ctx, item.TargetIssue)
	if err != nil {
		return nil, err
	}
	return sortRows(normalizeJiraCloudScope(filterJiraCloudWorklogsByUserAndWindow(worklogs, user, item.WindowFromUTC, item.WindowToUTC))), nil
}

func (s *Service) applyClockifyPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) error {
	if deps, ok := clockifyDepsFromCtx(ctx); ok {
		return s.applyClockifyPushItemWithDeps(ctx, item, deps)
	}

	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return err
	}

	client := s.newClockifyClient(*clockifyCfg)
	projects, err := client.ListProjects(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return err
	}
	projectMatches := matchingProjects(projects, item.InspectionSummary.ProjectName)
	if len(projectMatches) != 1 {
		return fmt.Errorf("saved target project %q is no longer resolvable", item.InspectionSummary.ProjectName)
	}
	project := projectMatches[0]

	tagsByID, err := client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return err
	}
	tag, tagExists := findTagByName(tagsByID, item.TargetIssue)
	if !tagExists {
		allowCreate := clockifyCreateIssueTagIfMissing(clockifyCfg)
		if !allowCreate {
			return fmt.Errorf("issue tag %q is missing and config forbids creating it", item.TargetIssue)
		}
		tag, err = client.CreateTag(ctx, clockifyCfg.WorkspaceID, item.TargetIssue)
		if err != nil {
			return err
		}
	}

	switch item.PlannedAction {
	case "create":
		for _, row := range item.Payload {
			if _, err := client.CreateTimeEntry(ctx, clockifyCfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
				return err
			}
		}
	case "replace", "delete":
		entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, item.WindowFromUTC, item.WindowToUTC)
		if err != nil {
			return err
		}
		scopeEntries := filterEntriesByProject(filterEntriesByIssue(entries, tagsByID, item.TargetIssue), project.ID)
		for _, entry := range scopeEntries {
			if err := client.DeleteTimeEntry(ctx, clockifyCfg.WorkspaceID, entry.ID); err != nil {
				return err
			}
		}
		if item.PlannedAction == "replace" {
			for _, row := range item.Payload {
				if _, err := client.CreateTimeEntry(ctx, clockifyCfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported push action %q", item.PlannedAction)
	}

	return nil
}

func clockifyCreateIssueTagIfMissing(clockifyCfg *config.ClockifyConfig) bool {
	if clockifyCfg == nil || clockifyCfg.ProjectMapping == nil {
		return false
	}
	if clockifyCfg.ProjectMapping.CreateIssueTagIfMissing == nil {
		return true
	}
	return *clockifyCfg.ProjectMapping.CreateIssueTagIfMissing
}

func (s *Service) applyJiraDataPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) error {
	instance, err := requireJiraDataInstance(cfg, item.TargetAdapterInstance)
	if err != nil {
		return err
	}
	client := s.newJiraDataClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return err
	}
	worklogs, err := client.ListIssueWorklogs(ctx, item.TargetIssue)
	if err != nil {
		return err
	}
	scope := filterJiraWorklogsByUserAndWindow(worklogs, user, item.WindowFromUTC, item.WindowToUTC)
	switch item.PlannedAction {
	case "create":
		for _, row := range item.Payload {
			if _, err := client.CreateWorklog(ctx, item.TargetIssue, row); err != nil {
				return err
			}
		}
	case "replace", "delete":
		for _, remote := range scope {
			if err := client.DeleteWorklog(ctx, item.TargetIssue, remote.ID); err != nil {
				return err
			}
		}
		if item.PlannedAction == "replace" {
			for _, row := range item.Payload {
				if _, err := client.CreateWorklog(ctx, item.TargetIssue, row); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported push action %q", item.PlannedAction)
	}
	return nil
}

func (s *Service) applyJiraCloudPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem) error {
	instance, err := requireJiraCloudInstance(cfg, item.TargetAdapterInstance)
	if err != nil {
		return err
	}
	client := s.newJiraCloudClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return err
	}
	worklogs, err := client.ListIssueWorklogs(ctx, item.TargetIssue)
	if err != nil {
		return err
	}
	scope := filterJiraCloudWorklogsByUserAndWindow(worklogs, user, item.WindowFromUTC, item.WindowToUTC)
	switch item.PlannedAction {
	case "create":
		for _, row := range item.Payload {
			if _, err := client.CreateWorklog(ctx, item.TargetIssue, row); err != nil {
				return err
			}
		}
	case "replace", "delete":
		for _, remote := range scope {
			if err := client.DeleteWorklog(ctx, item.TargetIssue, remote.ID); err != nil {
				return err
			}
		}
		if item.PlannedAction == "replace" {
			for _, row := range item.Payload {
				if _, err := client.CreateWorklog(ctx, item.TargetIssue, row); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported push action %q", item.PlannedAction)
	}
	return nil
}

func (s *Service) insertPlan(plan Plan) error {
	normalizePlanSummary(&plan)
	tx, err := s.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}

	adapterFamilies, err := json.Marshal(plan.AdapterFamilies)
	if err != nil {
		return err
	}
	targetInstances, err := json.Marshal(plan.TargetInstances)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO saved_plans(id, plan_direction, adapter_family, adapter_families_json, target_instances_json, config_fingerprint, window_from_utc, window_to_utc, created_at, aggregate_status, applied_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		plan.ID,
		plan.Direction,
		plan.AdapterFamily,
		string(adapterFamilies),
		string(targetInstances),
		plan.ConfigFingerprint,
		sqlitestore.RFC3339UTC(plan.WindowFromUTC),
		sqlitestore.RFC3339UTC(plan.WindowToUTC),
		sqlitestore.RFC3339UTC(plan.CreatedAt),
		plan.AggregateStatus,
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	for _, item := range plan.Items {
		payload, err := json.Marshal(item.Payload)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		inspection, err := json.Marshal(item.InspectionSummary)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO saved_plan_items(
				id, plan_id, issue_key, plan_direction, target_adapter_family, target_adapter_instance, target_issue, route_profile,
				window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail,
				payload_json, inspection_summary_json, delivery_key, content_hash, local_row_count, local_total_seconds,
				remote_row_count, remote_total_seconds, applied_state, applied_at, apply_message
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
			item.ID,
			item.PlanID,
			item.IssueKey,
			item.PlanDirection,
			item.TargetAdapterFamily,
			item.TargetAdapterInstance,
			item.TargetIssue,
			nullIfEmpty(item.RouteProfile),
			sqlitestore.RFC3339UTC(item.WindowFromUTC),
			sqlitestore.RFC3339UTC(item.WindowToUTC),
			item.PlanStatus,
			item.PlannedAction,
			item.ComparisonStatus,
			item.ReasonCode,
			item.ReasonDetail,
			string(payload),
			string(inspection),
			item.DeliveryKey,
			hashPayload(payload),
			item.LocalRowCount,
			item.LocalTotal,
			item.RemoteRowCount,
			item.RemoteTotal,
			item.AppliedState,
			item.ApplyMessage,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	for _, finding := range plan.Findings {
		payload, err := json.Marshal(finding.Payload)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO saved_plan_findings(id, plan_id, source_row_id, reason_code, reason_detail, payload_json) VALUES(?, ?, ?, ?, ?, ?)`,
			finding.ID,
			finding.PlanID,
			finding.SourceRowID,
			finding.ReasonCode,
			finding.ReasonDetail,
			string(payload),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (s *Service) loadPlanItems(planID string) ([]PlanItem, error) {
	attemptsByItem, err := s.loadDeliveryAttempts(planID)
	if err != nil {
		return nil, err
	}

	rows, err := s.store.DB().Query(`
		SELECT
			id, plan_id, issue_key, plan_direction, target_adapter_family, target_adapter_instance, target_issue, route_profile,
			window_from_utc, window_to_utc, plan_status, planned_action, comparison_status, reason_code, reason_detail,
			payload_json, inspection_summary_json, delivery_key, local_row_count, local_total_seconds, remote_row_count,
			remote_total_seconds, applied_state, applied_at, apply_message
		FROM saved_plan_items
		WHERE plan_id = ?
		ORDER BY target_issue ASC, target_adapter_family ASC, target_adapter_instance ASC, window_from_utc ASC, id ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanItem, 0)
	for rows.Next() {
		var item PlanItem
		var routeProfile sql.NullString
		var fromUTC string
		var toUTC string
		var payload string
		var inspection string
		var appliedAt sql.NullString
		if err := rows.Scan(
			&item.ID, &item.PlanID, &item.IssueKey, &item.PlanDirection, &item.TargetAdapterFamily, &item.TargetAdapterInstance,
			&item.TargetIssue, &routeProfile, &fromUTC, &toUTC, &item.PlanStatus, &item.PlannedAction, &item.ComparisonStatus,
			&item.ReasonCode, &item.ReasonDetail, &payload, &inspection, &item.DeliveryKey, &item.LocalRowCount, &item.LocalTotal,
			&item.RemoteRowCount, &item.RemoteTotal, &item.AppliedState, &appliedAt, &item.ApplyMessage,
		); err != nil {
			return nil, err
		}
		item.RouteProfile = routeProfile.String
		item.WindowFromUTC, _ = time.Parse(time.RFC3339, fromUTC)
		item.WindowToUTC, _ = time.Parse(time.RFC3339, toUTC)
		if err := json.Unmarshal([]byte(payload), &item.Payload); err != nil {
			return nil, err
		}
		if inspection != "" {
			if err := json.Unmarshal([]byte(inspection), &item.InspectionSummary); err != nil {
				return nil, err
			}
		}
		if item.PlanDirection == "" {
			item.PlanDirection = "pull"
		}
		if item.TargetAdapterFamily == "" {
			item.TargetAdapterFamily = "clockify"
		}
		if item.TargetIssue == "" {
			item.TargetIssue = item.IssueKey
		}
		if item.DeliveryKey == "" {
			item.DeliveryKey = buildDeliveryKey(item)
		}
		if appliedAt.Valid {
			t, _ := time.Parse(time.RFC3339, appliedAt.String)
			item.AppliedAt = &t
		}
		item.ExecutionState = deriveExecutionState(attemptsByItem[item.ID], s.now().UTC())
		if item.ExecutionState == "" {
			item.ExecutionState = "not_attempted"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) loadDeliveryAttempts(planID string) (map[string][]DeliveryAttempt, error) {
	rows, err := s.store.DB().Query(`
		SELECT plan_item_id, attempt_state, message, created_at
		FROM delivery_attempts
		WHERE plan_id = ?
		ORDER BY created_at ASC, id ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attempts := map[string][]DeliveryAttempt{}
	for rows.Next() {
		var itemID string
		var attempt DeliveryAttempt
		var createdAt string
		if err := rows.Scan(&itemID, &attempt.State, &attempt.Message, &createdAt); err != nil {
			return nil, err
		}
		attempt.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		attempts[itemID] = append(attempts[itemID], attempt)
	}
	return attempts, rows.Err()
}

func (s *Service) loadPlanFindings(planID string) ([]PlanFinding, error) {
	rows, err := s.store.DB().Query(`
		SELECT id, plan_id, source_row_id, reason_code, reason_detail, payload_json
		FROM saved_plan_findings
		WHERE plan_id = ?
		ORDER BY source_row_id ASC, id ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanFinding, 0)
	for rows.Next() {
		var item PlanFinding
		var payload string
		if err := rows.Scan(&item.ID, &item.PlanID, &item.SourceRowID, &item.ReasonCode, &item.ReasonDetail, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) listLocalScope(issueKey string, windowFrom, windowTo time.Time) ([]worklogs.LocalWorklog, error) {
	rows, err := s.store.DB().Query(
		`SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs WHERE issue_key = ? AND started_at_utc >= ? AND started_at_utc <= ? ORDER BY started_at_utc ASC, id ASC`,
		issueKey,
		sqlitestore.RFC3339UTC(windowFrom.UTC()),
		sqlitestore.RFC3339UTC(windowTo.UTC()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]worklogs.LocalWorklog, 0)
	for rows.Next() {
		item, err := scanWorklog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) listActiveWindow(windowFrom, windowTo time.Time) ([]worklogs.LocalWorklog, error) {
	rows, err := s.store.DB().Query(
		`SELECT id, issue_key, started_at_utc, duration_seconds, description FROM worklogs WHERE started_at_utc >= ? AND started_at_utc <= ? ORDER BY issue_key ASC, started_at_utc ASC, id ASC`,
		sqlitestore.RFC3339UTC(windowFrom.UTC()),
		sqlitestore.RFC3339UTC(windowTo.UTC()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]worklogs.LocalWorklog, 0)
	for rows.Next() {
		item, err := scanWorklog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) listTombstoneScope(windowFrom, windowTo time.Time) ([]worklogs.Tombstone, error) {
	rows, err := s.store.DB().Query(
		`SELECT worklog_id, issue_key, started_at_utc, duration_seconds, deleted_at FROM worklog_tombstones WHERE started_at_utc >= ? AND started_at_utc <= ? ORDER BY issue_key ASC, started_at_utc ASC, worklog_id ASC`,
		sqlitestore.RFC3339UTC(windowFrom.UTC()),
		sqlitestore.RFC3339UTC(windowTo.UTC()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]worklogs.Tombstone, 0)
	for rows.Next() {
		item, err := scanTombstone(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func tombstoneProtected(rows []model.Row, db *sql.DB) bool {
	for _, row := range rows {
		var count int
		err := db.QueryRow(
			`SELECT COUNT(1) FROM worklog_tombstones WHERE issue_key = ? AND started_at_utc = ? AND duration_seconds = ?`,
			row.IssueKey,
			sqlitestore.RFC3339UTC(row.StartedAtUTC),
			row.DurationSeconds,
		).Scan(&count)
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

func localToRows(items []worklogs.LocalWorklog) []model.Row {
	rows := make([]model.Row, 0, len(items))
	for _, item := range items {
		rows = append(rows, model.Row{
			IssueKey:        item.IssueKey,
			StartedAtUTC:    item.StartedAtUTC.UTC(),
			DurationSeconds: item.DurationSeconds,
			Description:     item.Description,
		})
	}
	return sortRows(rows)
}

func worklogsToRows(items []worklogs.LocalWorklog) map[string][]model.Row {
	grouped := map[string][]model.Row{}
	for _, item := range items {
		grouped[item.IssueKey] = append(grouped[item.IssueKey], model.Row{
			IssueKey:        item.IssueKey,
			StartedAtUTC:    item.StartedAtUTC.UTC(),
			DurationSeconds: item.DurationSeconds,
			Description:     item.Description,
		})
	}
	return grouped
}

func tombstonesToRows(items []worklogs.Tombstone) map[string][]model.Row {
	grouped := map[string][]model.Row{}
	for _, item := range items {
		grouped[item.IssueKey] = append(grouped[item.IssueKey], model.Row{
			IssueKey:        item.IssueKey,
			StartedAtUTC:    item.StartedAtUTC.UTC(),
			DurationSeconds: item.DurationSeconds,
		})
	}
	return grouped
}

func sortRows(items []model.Row) []model.Row {
	rows := append([]model.Row(nil), items...)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].StartedAtUTC.Equal(rows[j].StartedAtUTC) {
			return rows[i].StartedAtUTC.Before(rows[j].StartedAtUTC)
		}
		if rows[i].DurationSeconds != rows[j].DurationSeconds {
			return rows[i].DurationSeconds < rows[j].DurationSeconds
		}
		if rows[i].Description != rows[j].Description {
			return rows[i].Description < rows[j].Description
		}
		return rows[i].IssueKey < rows[j].IssueKey
	})
	return rows
}

func rowsEqual(a, b []model.Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].IssueKey != b[i].IssueKey || !a[i].StartedAtUTC.Equal(b[i].StartedAtUTC) || a[i].DurationSeconds != b[i].DurationSeconds || a[i].Description != b[i].Description {
			return false
		}
	}
	return true
}

func rowsEquivalent(a, b []model.Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].StartedAtUTC.Equal(b[i].StartedAtUTC) || a[i].DurationSeconds != b[i].DurationSeconds || a[i].Description != b[i].Description {
			return false
		}
	}
	return true
}

func sumRows(items []model.Row) int {
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	return total
}

func normalizeJiraDataScope(items []jiradatacenter.Worklog) []model.Row {
	rows := make([]model.Row, 0, len(items))
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		rows = append(rows, model.Row{
			StartedAtUTC:    startedAt.UTC(),
			DurationSeconds: item.TimeSpentSeconds,
			Description:     remoteCommentText(item.Comment),
		})
	}
	return rows
}

func normalizeJiraCloudScope(items []jiracloud.Worklog) []model.Row {
	rows := make([]model.Row, 0, len(items))
	for _, item := range items {
		startedAt, err := time.Parse("2006-01-02T15:04:05.000-0700", item.Started)
		if err != nil {
			continue
		}
		rows = append(rows, model.Row{
			StartedAtUTC:    startedAt.UTC(),
			DurationSeconds: item.TimeSpentSeconds,
			Description:     remoteCommentText(item.Comment),
		})
	}
	return rows
}

func remoteCommentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if body, ok := typed["body"].(string); ok {
			return body
		}
	}
	return ""
}

func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Service) markItemApplied(id string, appliedAt time.Time, state, message string) error {
	_, err := s.store.DB().Exec(`UPDATE saved_plan_items SET applied_state = ?, applied_at = ?, apply_message = ? WHERE id = ?`, state, sqlitestore.RFC3339UTC(appliedAt), message, id)
	return err
}

func (s *Service) markPlanApplied(id string, appliedAt time.Time) error {
	_, err := s.store.DB().Exec(`UPDATE saved_plans SET applied_at = ? WHERE id = ?`, sqlitestore.RFC3339UTC(appliedAt), id)
	return err
}

func (s *Service) recordDeliveryAttempt(planID, itemID, state, message string) error {
	_, err := s.store.DB().Exec(
		`INSERT INTO delivery_attempts(id, plan_id, plan_item_id, attempt_state, message, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		uuid.NewString(),
		planID,
		itemID,
		state,
		message,
		sqlitestore.RFC3339UTC(s.now().UTC()),
	)
	return err
}

func deriveExecutionState(attempts []DeliveryAttempt, now time.Time) string {
	if len(attempts) == 0 {
		return "not_attempted"
	}

	for _, attempt := range attempts {
		if attempt.State == "succeeded" {
			return "succeeded"
		}
	}

	latest := attempts[len(attempts)-1]
	if latest.State == "pending" {
		if state, ok := latestTerminalState(attempts, latest.CreatedAt); ok {
			return state
		}
		if now.Sub(latest.CreatedAt) < 15*time.Minute {
			return "pending"
		}
		return "uncertain"
	}
	if latest.State == "failed" || latest.State == "uncertain" {
		return latest.State
	}
	return "not_attempted"
}

func latestTerminalState(attempts []DeliveryAttempt, notBefore time.Time) (string, bool) {
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt.CreatedAt.Before(notBefore) {
			return "", false
		}
		if attempt.State == "failed" || attempt.State == "uncertain" {
			return attempt.State, true
		}
	}
	return "", false
}

func newPlanItem(plan Plan, issueKey string, payload []model.Row) PlanItem {
	return PlanItem{
		ID:                    uuid.NewString(),
		PlanID:                plan.ID,
		IssueKey:              issueKey,
		PlanDirection:         plan.Direction,
		TargetAdapterFamily:   plan.AdapterFamily,
		TargetAdapterInstance: "",
		TargetIssue:           issueKey,
		WindowFromUTC:         plan.WindowFromUTC,
		WindowToUTC:           plan.WindowToUTC,
		LocalRowCount:         len(payload),
		LocalTotal:            sumRows(payload),
		AppliedState:          "not_attempted",
		Payload:               payload,
	}
}

func applyActiveLocalMetrics(item *PlanItem, activeRows []model.Row) {
	item.LocalRowCount = len(activeRows)
	item.LocalTotal = sumRows(activeRows)
	if item.InspectionSummary.PerSourceTotals != nil {
		item.InspectionSummary.PerSourceTotals[item.IssueKey] = item.LocalTotal
	}
	item.InspectionSummary.LocalRowCount = item.LocalRowCount
	item.InspectionSummary.LocalTotalSeconds = item.LocalTotal
}

func newReportingScopeGroup(adapterFamily, targetInstance, targetIssue string) *reportingScopeGroup {
	return &reportingScopeGroup{
		TargetAdapterFamily:   adapterFamily,
		TargetAdapterInstance: targetInstance,
		TargetIssue:           targetIssue,
		PerSourceTotals:       map[string]int{},
	}
}

func (g *reportingScopeGroup) addSource(issueKey string, activeRows, tombstoneRows []model.Row, normalizeDescription func(sourceIssue, description string) string) {
	g.SourceIssueKeys = append(g.SourceIssueKeys, issueKey)
	g.PerSourceTotals[issueKey] = sumRows(activeRows)
	if len(tombstoneRows) > 0 {
		g.HasTombstones = true
	}
	for _, row := range activeRows {
		row.IssueKey = g.TargetIssue
		row.Description = normalizeDescription(issueKey, row.Description)
		g.DesiredRows = append(g.DesiredRows, row)
	}
}

func (g *reportingScopeGroup) sortedSourceIssueKeys() []string {
	keys := append([]string(nil), g.SourceIssueKeys...)
	sort.Strings(keys)
	return keys
}

func clonePerSourceTotals(items map[string]int) map[string]int {
	cloned := make(map[string]int, len(items))
	for key, value := range items {
		cloned[key] = value
	}
	return cloned
}

func pushApplySuccessMessage(adapterFamily string) string {
	switch adapterFamily {
	case "":
		return "applied saved push payload"
	default:
		return fmt.Sprintf("applied saved push payload to %s", adapterFamily)
	}
}

func deriveAggregateStatus(items []PlanItem, findings []PlanFinding) string {
	if len(items) == 0 {
		if len(findings) > 0 {
			return "invalid"
		}
		return "ready"
	}
	hasReady := false
	hasBlocked := false
	hasCheckFailed := false
	hasInvalid := false
	allSkipped := true
	for _, item := range items {
		switch item.PlanStatus {
		case "ready":
			hasReady = true
			allSkipped = false
		case "blocked":
			hasBlocked = true
			allSkipped = false
		case "check_failed":
			hasCheckFailed = true
			allSkipped = false
		case "invalid":
			hasInvalid = true
			allSkipped = false
		case "skipped":
		default:
			allSkipped = false
		}
	}
	switch {
	case hasCheckFailed:
		return "check_failed"
	case hasReady:
		return "ready"
	case hasBlocked:
		return "blocked"
	case hasInvalid || len(findings) > 0:
		return "invalid"
	case allSkipped:
		return "skipped"
	default:
		return "ready"
	}
}

func summarizeReportingNoPlan(adapterFamily, routeProfile string, windowFrom, windowTo time.Time, items []PlanItem) *ReconcileNoPlanResult {
	matched := 0
	actionable := 0
	hasBlocking := false
	resolvedInstances := map[string]struct{}{}

	for _, item := range items {
		if item.TargetAdapterInstance != "" && item.ReasonCode != "missing_route" {
			resolvedInstances[item.TargetAdapterInstance] = struct{}{}
		}
		switch item.PlanStatus {
		case "ready":
			actionable++
			matched++
		case "blocked", "invalid", "check_failed":
			hasBlocking = true
			if item.ReasonCode != "missing_route" {
				matched++
			}
		case "skipped":
			if item.ReasonCode != "missing_route" {
				matched++
			}
		default:
			if item.ReasonCode != "missing_route" {
				matched++
			}
		}
	}

	if matched != 0 && (actionable != 0 || hasBlocking) {
		return nil
	}

	instances := make([]string, 0, len(resolvedInstances))
	for instance := range resolvedInstances {
		instances = append(instances, instance)
	}
	sort.Strings(instances)

	reason := "already_in_sync"
	if matched == 0 {
		reason = "no_matching_routes"
	}

	return &ReconcileNoPlanResult{
		PlanCreated:             false,
		AdapterFamily:           adapterFamily,
		AdapterFamilies:         []string{adapterFamily},
		RouteProfile:            routeProfile,
		WindowFromUTC:           windowFrom.UTC(),
		WindowToUTC:             windowTo.UTC(),
		ResolvedTargetInstances: instances,
		MatchedScopeCount:       matched,
		ActionableScopeCount:    actionable,
		Reason:                  reason,
	}
}

func buildDeliveryKey(item PlanItem) string {
	payload, _ := json.Marshal(item.Payload)
	sum := sha256.Sum256([]byte(
		item.PlanDirection + "\x00" +
			item.TargetAdapterFamily + "\x00" +
			item.TargetAdapterInstance + "\x00" +
			item.TargetIssue + "\x00" +
			sqlitestore.RFC3339UTC(item.WindowFromUTC) + "\x00" +
			sqlitestore.RFC3339UTC(item.WindowToUTC) + "\x00" +
			item.PlannedAction + "\x00" +
			hashPayload(payload),
	))
	return hex.EncodeToString(sum[:])
}

func matchingProjects(projects []clockify.Project, name string) []clockify.Project {
	items := make([]clockify.Project, 0)
	for _, project := range projects {
		if project.Name == name {
			items = append(items, project)
		}
	}
	return items
}

func findTagByName(tagsByID map[string]clockify.Tag, name string) (clockify.Tag, bool) {
	for _, tag := range tagsByID {
		if tag.Name == name {
			return tag, true
		}
	}
	return clockify.Tag{}, false
}

func filterEntriesByIssue(entries []clockify.TimeEntry, tagsByID map[string]clockify.Tag, issueKey string) []clockify.TimeEntry {
	items := make([]clockify.TimeEntry, 0)
	for _, entry := range entries {
		issueTags := clockify.EntryIssueTags(entry, tagsByID)
		if len(issueTags) != 1 || issueTags[0] != issueKey {
			continue
		}
		items = append(items, entry)
	}
	return items
}

func filterEntriesByProject(entries []clockify.TimeEntry, projectID string) []clockify.TimeEntry {
	items := make([]clockify.TimeEntry, 0)
	for _, entry := range entries {
		if entry.ProjectID == projectID {
			items = append(items, entry)
		}
	}
	return items
}

func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func unionKeys[A any, B any](left map[string]A, right map[string]B) []string {
	seen := map[string]struct{}{}
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func scanWorklog(scanner interface{ Scan(dest ...any) error }) (worklogs.LocalWorklog, error) {
	var item worklogs.LocalWorklog
	var startedAt string
	if err := scanner.Scan(&item.ID, &item.IssueKey, &startedAt, &item.DurationSeconds, &item.Description); err != nil {
		return worklogs.LocalWorklog{}, err
	}
	parsed, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return worklogs.LocalWorklog{}, err
	}
	item.StartedAtUTC = parsed.UTC()
	return item, nil
}

func scanTombstone(scanner interface{ Scan(dest ...any) error }) (worklogs.Tombstone, error) {
	var item worklogs.Tombstone
	var startedAt string
	var deletedAt string
	if err := scanner.Scan(&item.ID, &item.IssueKey, &startedAt, &item.DurationSeconds, &deletedAt); err != nil {
		return worklogs.Tombstone{}, err
	}
	parsedStart, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return worklogs.Tombstone{}, err
	}
	parsedDeletedAt, err := time.Parse(time.RFC3339, deletedAt)
	if err != nil {
		return worklogs.Tombstone{}, err
	}
	item.StartedAtUTC = parsedStart.UTC()
	item.DeletedAt = parsedDeletedAt.UTC()
	return item, nil
}
