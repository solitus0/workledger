package issues

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/solitus0/workledger/internal/adapter/jiracloud"
	"github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

type AssignedPullResult struct {
	Items   []worklogs.IssueMetadata
	Targets []AssignedPullTarget
}

type AssignedPullTarget struct {
	Adapter    string
	Instance   string
	Imported   int
	SkipReason string
	Err        error
}

type assignedSource struct {
	adapter  string
	instance string
}

type assignedIssueClient interface {
	IssueClient
	CurrentUser(context.Context) (jiradatacenter.User, error)
	SearchIssues(context.Context, string, []string) ([]jiradatacenter.IssueBrief, error)
}

// PullAssigned discovers issues independently of the local catalog and worklogs.
func (s *Service) PullAssigned(ctx context.Context, cfg config.EffectiveConfig, adapter, instance string) (AssignedPullResult, error) {
	sources, err := assignedSources(cfg, adapter, instance)
	if err != nil {
		return AssignedPullResult{}, err
	}
	owners := make(map[string]map[assignedSource]struct{})
	for _, rule := range config.RouteRules(cfg) {
		if rule.Mode != "ownership" {
			continue
		}
		prefix := strings.TrimSpace(rule.SourcePrefix)
		if owners[prefix] == nil {
			owners[prefix] = make(map[assignedSource]struct{})
		}
		owners[prefix][assignedSource{adapter: rule.AdapterFamily, instance: rule.Instance}] = struct{}{}
	}
	result := AssignedPullResult{Targets: make([]AssignedPullTarget, 0, len(sources))}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		items, skipReason, err := s.pullAssignedSource(ctx, cfg, source, owners)
		if err != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Items = append(result.Items, items...)
		result.Targets = append(result.Targets, AssignedPullTarget{
			Adapter: source.adapter, Instance: source.instance, Imported: len(items), SkipReason: skipReason, Err: err,
		})
	}
	return result, ctx.Err()
}

func assignedSources(cfg config.EffectiveConfig, adapter, instance string) ([]assignedSource, error) {
	if adapter != "" && adapter != "jira-cloud" && adapter != "jira-data-center" {
		return nil, validationError("supported adapters are jira-cloud and jira-data-center")
	}
	var sources []assignedSource
	if cfg.File.JiraCloud != nil && (adapter == "" || adapter == "jira-cloud") {
		for _, name := range slices.Sorted(maps.Keys(cfg.File.JiraCloud.Instances)) {
			if instance == "" || name == instance {
				sources = append(sources, assignedSource{adapter: "jira-cloud", instance: name})
			}
		}
	}
	if cfg.File.JiraData != nil && (adapter == "" || adapter == "jira-data-center") {
		for _, name := range slices.Sorted(maps.Keys(cfg.File.JiraData.Instances)) {
			if instance == "" || name == instance {
				sources = append(sources, assignedSource{adapter: "jira-data-center", instance: name})
			}
		}
	}
	if instance != "" && len(sources) > 1 {
		return nil, validationError(fmt.Sprintf("Jira instance %q exists in both adapter families, specify --adapter", instance))
	}
	if len(sources) == 0 {
		if instance != "" {
			return nil, validationError(fmt.Sprintf("Jira instance %q is not configured for the selected adapters", instance))
		}
		return nil, validationError("no Jira instances configured for issues pull")
	}
	return sources, nil
}

func (s *Service) pullAssignedSource(ctx context.Context, cfg config.EffectiveConfig, source assignedSource, owners map[string]map[assignedSource]struct{}) ([]worklogs.IssueMetadata, string, error) {
	prefixes := make(map[string]struct{})
	for _, prefix := range slices.Sorted(maps.Keys(owners)) {
		if _, ok := owners[prefix][source]; !ok {
			continue
		}
		if len(owners[prefix]) > 1 {
			return nil, "", validationError(fmt.Sprintf("project %q has ambiguous Jira ownership routes, configure one owner", prefix))
		}
		prefixes[prefix] = struct{}{}
	}
	if len(prefixes) == 0 {
		return nil, "no Jira ownership routes configured", nil
	}
	client, err := source.client(cfg)
	if err != nil {
		return nil, "", validationError(err.Error())
	}
	excluded, err := config.JiraExcludedIssuesForInstance(cfg, source.adapter, source.instance)
	if err != nil {
		return nil, "", validationError(err.Error())
	}
	items, err := s.fetchAssignedIssues(ctx, client, source, prefixes, excluded)
	if err != nil {
		return nil, "", &RemoteError{Adapter: source.adapter, Err: err}
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	changed, err := s.worklogs.ImportIssueMetadataBatch(ctx, items)
	return changed, "", err
}

func (source assignedSource) client(cfg config.EffectiveConfig) (assignedIssueClient, error) {
	if source.adapter == "jira-cloud" {
		_, resolved, err := config.ResolveJiraCloudInstance(cfg, source.instance)
		if err != nil {
			return nil, err
		}
		return jiracloud.NewClient(resolved.BaseURL, resolved.Auth.Email, resolved.Auth.Token), nil
	}
	_, resolved, err := config.ResolveJiraDataInstance(cfg, source.instance)
	if err != nil {
		return nil, err
	}
	return jiradatacenter.NewClient(resolved.BaseURL, resolved.Auth.Bearer.Token), nil
}

func (s *Service) fetchAssignedIssues(ctx context.Context, client assignedIssueClient, source assignedSource, prefixes map[string]struct{}, excluded []string) ([]worklogs.IssueMetadata, error) {
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("identify authenticated user: %w", err)
	}
	cloud := source.adapter == "jira-cloud"
	if (cloud && user.AccountID == "") || (!cloud && user.Key == "" && user.Name == "") {
		return nil, fmt.Errorf("Jira did not return an identifier for the authenticated user")
	}
	fields := []string{"summary", "timetracking", "assignee"}
	remoteIssues, err := client.SearchIssues(ctx, "assignee = currentUser() ORDER BY key ASC", fields)
	if err != nil {
		return nil, fmt.Errorf("search assigned issues: %w", err)
	}
	refreshedAt := s.deps.Now().UTC()
	items := make([]worklogs.IssueMetadata, 0, len(remoteIssues))
	seen := make(map[string]struct{})
	for _, remote := range remoteIssues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cloud && remote.Key == "" {
			if id, err := strconv.ParseUint(remote.ID, 10, 64); err != nil || id == 0 {
				return nil, fmt.Errorf("Jira Cloud search result has no issue key or valid issue ID")
			}
			reference := remote.ID
			remote, err = client.GetIssue(ctx, reference, fields)
			if err != nil {
				return nil, fmt.Errorf("look up Jira issue ID %s: %w", reference, err)
			}
		}
		if !matchesAssignee(cloud, remote.Fields.Assignee, user) {
			continue
		}
		if !worklogs.IsValidIssueKey(remote.Key) {
			return nil, fmt.Errorf("Jira returned an invalid issue key %q", remote.Key)
		}
		prefix, _, _ := strings.Cut(remote.Key, "-")
		_, excludedIssue := slices.BinarySearch(excluded, remote.Key)
		if _, owned := prefixes[prefix]; !owned || excludedIssue {
			continue
		}
		if _, duplicate := seen[remote.Key]; duplicate {
			continue
		}
		seen[remote.Key] = struct{}{}
		items = append(items, worklogs.IssueMetadata{
			IssueKey: remote.Key, Title: strings.Join(strings.Fields(remote.Fields.Summary), " "),
			MaxEstimateSeconds: originalEstimate(remote), SourceAdapterFamily: source.adapter,
			SourceAdapterInst: source.instance, RefreshedAt: refreshedAt,
		})
	}
	return items, nil
}

func matchesAssignee(cloud bool, assignee *jiradatacenter.User, user jiradatacenter.User) bool {
	switch {
	case assignee == nil:
		return false
	case cloud:
		return user.AccountID != "" && user.AccountID == assignee.AccountID
	case user.Key != "" && assignee.Key != "":
		return user.Key == assignee.Key
	default:
		return user.Name != "" && user.Name == assignee.Name
	}
}
