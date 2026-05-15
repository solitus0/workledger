package totals

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradataadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile/model"
)

type clockifyClient interface {
	CurrentUser(ctx context.Context) (clockifyadapter.User, error)
	ListUserTimeEntries(ctx context.Context, workspaceID, userID string, start, end time.Time) ([]clockifyadapter.TimeEntry, error)
}

type jiraDataClient interface {
	CurrentUser(ctx context.Context) (jiradataadapter.User, error)
	SearchIssues(ctx context.Context, jql string, fields []string) ([]jiradataadapter.IssueBrief, error)
	ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiradataadapter.Worklog, error)
	GetIssue(ctx context.Context, issueKey string, fields []string) (jiradataadapter.IssueBrief, error)
}

type jiraCloudClient interface {
	CurrentUser(ctx context.Context) (jiracloudadapter.User, error)
	SearchIssues(ctx context.Context, jql string, fields []string) ([]jiracloudadapter.IssueBrief, error)
	ListIssueWorklogs(ctx context.Context, issueKey string) ([]jiracloudadapter.Worklog, error)
	GetIssue(ctx context.Context, issueKey string, fields []string) (jiracloudadapter.IssueBrief, error)
}

type CollectionItem struct {
	Adapter     string
	Instance    string
	Display     string
	Result      *Result
	LocalResult *Result
	Code        int
	Status      string
	Message     string
}

func (s *Service) CollectAll(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, options ...Options) ([]CollectionItem, int) {
	opts := resolveOptions(options)
	targets := s.buildCollectionTargets(cfg)
	if len(targets) == 0 {
		return nil, 0
	}

	opts.Reporter.Start(progress.Event{
		Phase:      "discovering",
		ScopeTotal: len(targets),
		Message:    "totals collection",
	})
	defer func() {
		// final completion event is emitted after ordered assembly below
	}()

	type fetchResult struct {
		index int
		item  CollectionItem
	}

	results := make(chan fetchResult, len(targets))
	for i, target := range targets {
		i := i
		target := target
		go func() {
			opts.Reporter.Event(progress.Event{
				Phase:      "fetching",
				ScopeLabel: target.label(),
				Message:    "fetching totals target",
			})
			item := target.fetch(ctx, s, cfg, windowFrom, windowTo, opts)
			results <- fetchResult{index: i, item: item}
		}()
	}

	fetched := make([]CollectionItem, len(targets))
	for range targets {
		result := <-results
		fetched[result.index] = result.item
	}

	items := make([]CollectionItem, 0, len(targets))
	exitCode := 0
	failed := 0
	for _, item := range fetched {
		items = append(items, item)
		if item.Code != 0 {
			failed++
		}
		exitCode = firstStatusExitCode(exitCode, item.Code)
	}
	opts.Reporter.Finish(progress.Event{
		Phase:      "finalizing",
		ScopeDone:  len(items),
		ScopeTotal: len(targets),
		Failed:     failed,
		Message:    "totals collection complete",
	})
	return items, exitCode
}

func (s *Service) CompareClockifyRemote(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, options ...Options) (Result, error) {
	opts := resolveOptions(options)
	opts.Reporter.Start(progress.Event{Phase: "validating", Message: "totals clockify"})
	defer opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "totals clockify complete"})

	entries, err := s.fetchClockifyEntries(ctx, cfg, windowFrom, windowTo, opts)
	if err != nil {
		return Result{}, err
	}

	opts.Reporter.Event(progress.Event{Phase: "comparing", Message: "comparing local and clockify totals"})
	return s.CompareClockify(ctx, cfg, windowFrom, windowTo, entries)
}

func (s *Service) CompareJiraCloudRemote(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...Options) (string, Result, error) {
	opts := resolveOptions(options)
	opts.Reporter.Start(progress.Event{Phase: "validating", Message: "totals jira-cloud"})
	defer opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "totals jira-cloud complete"})

	resolvedInstance, _, rows, issuePrefixes, excludedSet, err := s.fetchJiraCloudRows(ctx, cfg, instanceName, windowFrom, windowTo, opts)
	if err != nil {
		return "", Result{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "comparing", Message: "comparing local and jira-cloud totals"})
	result, err := s.CompareJiraDataWithExclusions(ctx, cfg, windowFrom, windowTo, rows, issuePrefixes, excludedSet)
	return resolvedInstance, result, err
}

func (s *Service) CompareJiraDataRemote(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, options ...Options) (string, Result, error) {
	opts := resolveOptions(options)
	opts.Reporter.Start(progress.Event{Phase: "validating", Message: "totals jira-data-center"})
	defer opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "totals jira-data-center complete"})

	resolvedInstance, _, rows, issuePrefixes, excludedSet, err := s.fetchJiraDataRows(ctx, cfg, instanceName, windowFrom, windowTo, opts)
	if err != nil {
		return "", Result{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "comparing", Message: "comparing local and jira-data-center totals"})
	result, err := s.CompareJiraDataWithExclusions(ctx, cfg, windowFrom, windowTo, rows, issuePrefixes, excludedSet)
	return resolvedInstance, result, err
}

type collectionTarget interface {
	label() string
	fetch(ctx context.Context, service *Service, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, opts Options) CollectionItem
}

type clockifyTarget struct{}

func (clockifyTarget) label() string { return "clockify" }

func (clockifyTarget) fetch(ctx context.Context, service *Service, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, opts Options) CollectionItem {
	item := CollectionItem{Adapter: "clockify", Instance: config.ClockifyInstanceName}
	if cfg.File.Clockify != nil {
		item.Display = cfg.File.Clockify.WorkspaceID
	}
	localResult, err := service.SummarizeLocal(ctx, cfg, windowFrom, windowTo)
	if err != nil {
		item.Code = 1
		item.Status = "unexpected_error"
		item.Message = err.Error()
		return item
	}
	entries, err := service.fetchClockifyEntries(ctx, cfg, windowFrom, windowTo, opts)
	if err != nil {
		item.LocalResult = &localResult
		item.Code = statusExitCodeForClockify(err)
		if errors.Is(err, errValidation) {
			item.Code = 2
		}
		item.Status = totalsStatusForClockifyError(err)
		if item.Code == 2 {
			item.Status = "validation_error"
		}
		item.Message = err.Error()
		return item
	}
	result, err := service.CompareClockify(ctx, cfg, windowFrom, windowTo, entries)
	if err != nil {
		item.Code = 1
		item.Status = "unexpected_error"
		item.Message = err.Error()
		return item
	}
	item.Result = &result
	return item
}

type jiraCloudTarget struct{ instance string }

func (t jiraCloudTarget) label() string { return "jira-cloud/" + t.instance }

func (t jiraCloudTarget) fetch(ctx context.Context, service *Service, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, opts Options) CollectionItem {
	item := CollectionItem{Adapter: "jira-cloud", Instance: t.instance}
	resolvedInstance, localResult, rows, issuePrefixes, excludedSet, err := service.fetchJiraCloudRows(ctx, cfg, t.instance, windowFrom, windowTo, opts)
	if resolvedInstance != "" {
		item.Instance = resolvedInstance
	}
	if localResult != nil {
		item.LocalResult = localResult
	}
	if err != nil {
		item.Code = totalsExitCodeForJiraCloud(err)
		if errors.Is(err, errValidation) {
			item.Code = 2
		}
		item.Status = totalsStatusForJiraCloudError(err)
		if item.Code == 2 {
			item.Status = "validation_error"
		}
		item.Message = err.Error()
		return item
	}
	result, err := service.CompareJiraDataWithExclusions(ctx, cfg, windowFrom, windowTo, rows, issuePrefixes, excludedSet)
	if err != nil {
		item.Code = 1
		item.Status = "unexpected_error"
		item.Message = err.Error()
		return item
	}
	item.Result = &result
	return item
}

type jiraDataTarget struct{ instance string }

func (t jiraDataTarget) label() string { return "jira-data-center/" + t.instance }

func (t jiraDataTarget) fetch(ctx context.Context, service *Service, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, opts Options) CollectionItem {
	item := CollectionItem{Adapter: "jira-data-center", Instance: t.instance}
	resolvedInstance, localResult, rows, issuePrefixes, excludedSet, err := service.fetchJiraDataRows(ctx, cfg, t.instance, windowFrom, windowTo, opts)
	if resolvedInstance != "" {
		item.Instance = resolvedInstance
	}
	if localResult != nil {
		item.LocalResult = localResult
	}
	if err != nil {
		item.Code = totalsExitCodeForJiraData(err)
		if errors.Is(err, errValidation) {
			item.Code = 2
		}
		item.Status = totalsStatusForJiraDataError(err)
		if item.Code == 2 {
			item.Status = "validation_error"
		}
		item.Message = err.Error()
		return item
	}
	result, err := service.CompareJiraDataWithExclusions(ctx, cfg, windowFrom, windowTo, rows, issuePrefixes, excludedSet)
	if err != nil {
		item.Code = 1
		item.Status = "unexpected_error"
		item.Message = err.Error()
		return item
	}
	item.Result = &result
	return item
}

var errValidation = errors.New("validation")

func withValidation(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", errValidation, err)
}

func IsValidationError(err error) bool {
	return errors.Is(err, errValidation)
}

func (s *Service) buildCollectionTargets(cfg config.EffectiveConfig) []collectionTarget {
	targets := make([]collectionTarget, 0)
	if cfg.File.Clockify != nil {
		targets = append(targets, clockifyTarget{})
	}
	if cfg.File.JiraCloud != nil {
		names := sortedJiraCloudInstanceNames(cfg.File.JiraCloud.Instances)
		for _, name := range names {
			targets = append(targets, jiraCloudTarget{instance: name})
		}
	}
	if cfg.File.JiraData != nil {
		names := sortedJiraDataInstanceNames(cfg.File.JiraData.Instances)
		for _, name := range names {
			targets = append(targets, jiraDataTarget{instance: name})
		}
	}
	return targets
}

func (s *Service) fetchClockifyEntries(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, opts Options) ([]clockifyadapter.TimeEntry, error) {
	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return nil, withValidation(err)
	}

	client := s.newClockifyClient(*clockifyCfg)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user.ID != clockifyCfg.UserID {
		return nil, withValidation(errors.New("configured clockify.user_id does not match authenticated user"))
	}
	if clockifyCfg.WorkspaceID != user.ActiveWorkspace && clockifyCfg.WorkspaceID != user.DefaultWorkspace {
		return nil, withValidation(errors.New("configured clockify.workspace_id is not visible for the authenticated user"))
	}

	opts.Reporter.Event(progress.Event{Phase: "fetching", Message: "fetched clockify user context"})
	entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, windowFrom, windowTo)
	if err != nil {
		return nil, err
	}
	opts.Reporter.Event(progress.Event{Phase: "fetching", Message: "fetched clockify time entries"})
	return entries, nil
}

func (s *Service) fetchJiraDataRows(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, opts Options) (string, *Result, []model.Row, []string, map[string]struct{}, error) {
	resolvedInstance, instance, err := config.ResolveJiraDataInstance(cfg, instanceName)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	issuePrefixes, err := config.JiraDataIssuePrefixesForTotals(cfg, resolvedInstance)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	excludedIssues, err := config.JiraExcludedIssuesForInstance(cfg, "jira-data-center", resolvedInstance)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	excludedSet := toExactSet(excludedIssues)
	localResult, err := s.SummarizeLocalFiltered(ctx, cfg, windowFrom, windowTo, issuePrefixes, excludedSet)
	if err != nil {
		return resolvedInstance, nil, nil, nil, nil, err
	}

	client := s.newJiraDataClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}

	jql := totalsJQL(windowFrom, windowTo)
	opts.Reporter.Event(progress.Event{Phase: "discovering", Message: "discovering jira-data-center issues"})
	issues, err := client.SearchIssues(ctx, jql, nil)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}
	rows, err := fetchJiraDataIssueRowsConcurrently(ctx, client, user, issues, windowFrom, windowTo, issuePrefixes, excludedSet, opts)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}
	return resolvedInstance, &localResult, rows, issuePrefixes, excludedSet, nil
}

func (s *Service) fetchJiraCloudRows(ctx context.Context, cfg config.EffectiveConfig, instanceName string, windowFrom, windowTo time.Time, opts Options) (string, *Result, []model.Row, []string, map[string]struct{}, error) {
	resolvedInstance, instance, err := config.ResolveJiraCloudInstance(cfg, instanceName)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	issuePrefixes, err := config.JiraCloudIssuePrefixesForTotals(cfg, resolvedInstance)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	excludedIssues, err := config.JiraExcludedIssuesForInstance(cfg, "jira-cloud", resolvedInstance)
	if err != nil {
		return "", nil, nil, nil, nil, withValidation(err)
	}
	excludedSet := toExactSet(excludedIssues)
	localResult, err := s.SummarizeLocalFiltered(ctx, cfg, windowFrom, windowTo, issuePrefixes, excludedSet)
	if err != nil {
		return resolvedInstance, nil, nil, nil, nil, err
	}

	client := s.newJiraCloudClient(instance)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}

	jql := totalsJQL(windowFrom, windowTo)
	opts.Reporter.Event(progress.Event{Phase: "discovering", Message: "discovering jira-cloud issues"})
	issues, err := client.SearchIssues(ctx, jql, nil)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}
	rows, err := fetchJiraCloudIssueRowsConcurrently(ctx, client, user, issues, windowFrom, windowTo, issuePrefixes, excludedSet, opts)
	if err != nil {
		return resolvedInstance, &localResult, nil, issuePrefixes, excludedSet, err
	}
	return resolvedInstance, &localResult, rows, issuePrefixes, excludedSet, nil
}

func fetchJiraDataIssueRowsConcurrently(ctx context.Context, client jiraDataClient, user jiradataadapter.User, issues []jiradataadapter.IssueBrief, windowFrom, windowTo time.Time, issuePrefixes []string, excluded map[string]struct{}, opts Options) ([]model.Row, error) {
	type issueResult struct {
		rows []model.Row
		err  error
	}
	results := make(chan issueResult, len(issues))
	var wg sync.WaitGroup
	for _, issue := range issues {
		issue := issue
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, skip := excluded[issue.Key]; skip {
				results <- issueResult{}
				return
			}
			worklogItems, err := client.ListIssueWorklogs(ctx, issue.Key)
			if err != nil {
				results <- issueResult{err: err}
				return
			}
			valid, _ := jiradataadapter.NormalizeIssueWorklogs(issue.Key, worklogItems, user, windowFrom, windowTo)
			filtered := make([]model.Row, 0, len(valid))
			for _, row := range valid {
				if matchesAnyIssuePrefix(row.IssueKey, issuePrefixes) {
					filtered = append(filtered, row)
				}
			}
			results <- issueResult{rows: filtered}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	rows := make([]model.Row, 0)
	fetched := 0
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		fetched++
		opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: fetched, ScopeTotal: len(issues), Message: "fetched jira-data-center issue worklogs"})
		rows = append(rows, result.rows...)
	}
	return rows, nil
}

func fetchJiraCloudIssueRowsConcurrently(ctx context.Context, client jiraCloudClient, user jiracloudadapter.User, issues []jiracloudadapter.IssueBrief, windowFrom, windowTo time.Time, issuePrefixes []string, excluded map[string]struct{}, opts Options) ([]model.Row, error) {
	type issueResult struct {
		rows []model.Row
		err  error
	}
	results := make(chan issueResult, len(issues))
	var wg sync.WaitGroup
	for _, issue := range issues {
		issue := issue
		wg.Add(1)
		go func() {
			defer wg.Done()
			issueKey, issueRef, err := resolveJiraCloudIssueReference(ctx, client, issue)
			if err != nil {
				results <- issueResult{err: err}
				return
			}
			if _, skip := excluded[issueKey]; skip {
				results <- issueResult{}
				return
			}
			worklogItems, err := client.ListIssueWorklogs(ctx, issueRef)
			if err != nil {
				results <- issueResult{err: err}
				return
			}
			valid, _ := jiracloudadapter.NormalizeIssueWorklogs(issueKey, worklogItems, user, windowFrom, windowTo)
			filtered := make([]model.Row, 0, len(valid))
			for _, row := range valid {
				if matchesAnyIssuePrefix(row.IssueKey, issuePrefixes) {
					filtered = append(filtered, row)
				}
			}
			results <- issueResult{rows: filtered}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	rows := make([]model.Row, 0)
	fetched := 0
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		fetched++
		opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: fetched, ScopeTotal: len(issues), Message: "fetched jira-cloud issue worklogs"})
		rows = append(rows, result.rows...)
	}
	return rows, nil
}

func totalsJQL(windowFrom, windowTo time.Time) string {
	return fmt.Sprintf("worklogAuthor = currentUser() AND worklogDate >= \"%s\" AND worklogDate <= \"%s\"", windowFrom.Format("2006-01-02"), windowTo.Format("2006-01-02"))
}

func resolveJiraCloudIssueReference(ctx context.Context, client jiraCloudClient, issue jiracloudadapter.IssueBrief) (issueKey string, issueRef string, err error) {
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

func matchesAnyIssuePrefix(issueKey string, issuePrefixes []string) bool {
	for _, prefix := range issuePrefixes {
		if strings.HasPrefix(issueKey, prefix) {
			return true
		}
	}
	return false
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

func firstStatusExitCode(current, next int) int {
	if current != 0 {
		return current
	}
	return next
}

func statusExitCodeForClockify(err error) int {
	var requestErr *clockifyadapter.RequestError
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
	var requestErr *jiradataadapter.RequestError
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
	var requestErr *jiradataadapter.RequestError
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
