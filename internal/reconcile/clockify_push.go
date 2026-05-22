package reconcile

import (
	"context"
	"fmt"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/config"
)

type clockifyPushDeps struct {
	client      clockifyClient
	cfg         *config.ClockifyConfig
	projects    []clockify.Project
	tagsByID    map[string]clockify.Tag
	tagsByIssue map[string]clockify.Tag
}

type clockifyCtxKey int

const clockifyDepsCtxKey clockifyCtxKey = iota

func withClockifyDeps(ctx context.Context, deps *clockifyPushDeps) context.Context {
	return context.WithValue(ctx, clockifyDepsCtxKey, deps)
}

func clockifyDepsFromCtx(ctx context.Context) (*clockifyPushDeps, bool) {
	d, ok := ctx.Value(clockifyDepsCtxKey).(*clockifyPushDeps)
	return d, ok
}

// prepareClockifyPushDeps fetches projects and tags once, then creates any missing issue-key
// tags sequentially. All API calls happen before any time-entry goroutine is launched.
func (s *Service) prepareClockifyPushDeps(ctx context.Context, cfg config.EffectiveConfig, items []PlanItem) (*clockifyPushDeps, error) {
	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return nil, err
	}

	client := s.newClockifyClient(*clockifyCfg)

	projects, err := client.ListProjects(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return nil, err
	}

	tagsByID, err := client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return nil, err
	}

	deps := &clockifyPushDeps{
		client:      client,
		cfg:         clockifyCfg,
		projects:    projects,
		tagsByID:    tagsByID,
		tagsByIssue: make(map[string]clockify.Tag),
	}

	for _, item := range items {
		issueKey := item.TargetIssue
		if _, ok := deps.tagsByIssue[issueKey]; ok {
			continue
		}
		tag, tagExists := findTagByName(tagsByID, issueKey)
		if !tagExists {
			if !clockifyCreateIssueTagIfMissing(clockifyCfg) {
				return nil, fmt.Errorf("issue tag %q is missing and config forbids creating it", issueKey)
			}
			tag, err = client.CreateTag(ctx, clockifyCfg.WorkspaceID, issueKey)
			if err != nil {
				return nil, err
			}
			tagsByID[tag.ID] = tag
		}
		deps.tagsByIssue[issueKey] = tag
	}

	return deps, nil
}

// applyClockifyPushItemWithDeps applies one plan item using pre-resolved projects and tags,
// skipping the ListProjects and ListTags calls that would otherwise run per-item.
func (s *Service) applyClockifyPushItemWithDeps(ctx context.Context, item PlanItem, deps *clockifyPushDeps) error {
	projectMatches := matchingProjects(deps.projects, item.InspectionSummary.ProjectName)
	if len(projectMatches) != 1 {
		return fmt.Errorf("saved target project %q is no longer resolvable", item.InspectionSummary.ProjectName)
	}
	project := projectMatches[0]

	tag, ok := deps.tagsByIssue[item.TargetIssue]
	if !ok {
		return fmt.Errorf("issue tag %q was not pre-resolved", item.TargetIssue)
	}

	switch item.PlannedAction {
	case "create":
		for _, row := range item.Payload {
			if _, err := deps.client.CreateTimeEntry(ctx, deps.cfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
				return err
			}
		}
	case "replace", "delete":
		entries, err := deps.client.ListUserTimeEntries(ctx, deps.cfg.WorkspaceID, deps.cfg.UserID, item.WindowFromUTC, item.WindowToUTC)
		if err != nil {
			return err
		}
		scopeEntries := filterEntriesByProject(filterEntriesByIssue(entries, deps.tagsByID, item.TargetIssue), project.ID)
		for _, entry := range scopeEntries {
			if err := deps.client.DeleteTimeEntry(ctx, deps.cfg.WorkspaceID, entry.ID); err != nil {
				return err
			}
		}
		if item.PlannedAction == "replace" {
			for _, row := range item.Payload {
				if _, err := deps.client.CreateTimeEntry(ctx, deps.cfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("unsupported push action %q", item.PlannedAction)
	}

	return nil
}
