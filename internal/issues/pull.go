package issues

import (
	"context"
	"fmt"
	"strings"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

type PullFailure struct {
	IssueKey string
	Err      error
}

type PullResult struct {
	Items    []worklogs.IssueMetadata
	Failures []PullFailure
}

// Pull caches metadata for explicit catalog keys, including unused issues.
// Each issue commits independently so failures and cancellation can be retried.
func (s *Service) Pull(ctx context.Context, cfg config.EffectiveConfig, keys []string) PullResult {
	result := PullResult{}
	clients := map[string]IssueClient{}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		family, instance, err := pullRoute(cfg, key)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		client := clients[family+"/"+instance]
		if err == nil && client == nil {
			switch family {
			case "jira-cloud":
				_, resolved, resolveErr := config.ResolveJiraCloudInstance(cfg, instance)
				err = resolveErr
				if err == nil {
					client = s.deps.JiraCloudClient(resolved)
				}
			case "jira-data-center":
				_, resolved, resolveErr := config.ResolveJiraDataInstance(cfg, instance)
				err = resolveErr
				if err == nil {
					client = s.deps.JiraDataClient(resolved)
				}
			}
			clients[family+"/"+instance] = client
		}
		if err == nil {
			remote, fetchErr := client.GetIssue(ctx, key, []string{"summary", "timetracking"})
			err = fetchErr
			if err == nil {
				item := worklogs.IssueMetadata{
					IssueKey: key, Title: strings.Join(strings.Fields(remote.Fields.Summary), " "),
					MaxEstimateSeconds: originalEstimate(remote), SourceAdapterFamily: family,
					SourceAdapterInst: instance, RefreshedAt: s.deps.Now().UTC(),
				}
				err = s.worklogs.UpsertIssueMetadataBatch(ctx, []worklogs.IssueMetadata{item})
				if err == nil {
					result.Items = append(result.Items, item)
				}
			}
		}
		if err != nil {
			result.Failures = append(result.Failures, PullFailure{IssueKey: key, Err: err})
		}
	}
	return result
}

func pullRoute(cfg config.EffectiveConfig, key string) (string, string, error) {
	if !worklogs.IsValidIssueKey(key) {
		return "", "", validationError("issue must match <PROJECTKEY>-<NUMBER>")
	}
	owners := map[string]config.RouteRule{}
	for _, rule := range config.RouteRules(cfg) {
		if rule.Mode == "ownership" && strings.HasPrefix(key, rule.SourcePrefix+"-") {
			owners[rule.AdapterFamily+"/"+rule.Instance] = rule
		}
	}
	if len(owners) == 1 {
		for _, owner := range owners {
			return owner.AdapterFamily, owner.Instance, nil
		}
	}
	if len(owners) > 1 {
		return "", "", validationError(fmt.Sprintf("%s has ambiguous Jira ownership routes; configure one owner", key))
	}
	// A sole instance without routing is an unambiguous default. Reporting
	// destinations never remap the original issue or establish its owner.
	var defaults []config.RouteRule
	if cfg.File.JiraCloud != nil {
		for name, instance := range cfg.File.JiraCloud.Instances {
			if instance.Routing == nil {
				defaults = append(defaults, config.RouteRule{AdapterFamily: "jira-cloud", Instance: name})
			}
		}
	}
	if cfg.File.JiraData != nil {
		for name, instance := range cfg.File.JiraData.Instances {
			if instance.Routing == nil {
				defaults = append(defaults, config.RouteRule{AdapterFamily: "jira-data-center", Instance: name})
			}
		}
	}
	if len(defaults) == 1 {
		return defaults[0].AdapterFamily, defaults[0].Instance, nil
	}
	return "", "", validationError(fmt.Sprintf("%s has no unique Jira owner; configure an ownership issue prefix", key))
}
