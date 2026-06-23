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
func (s *Service) applyClockifyPushItemWithDeps(ctx context.Context, item PlanItem, deps *clockifyPushDeps) (pushApplyResult, error) {
	projectMatches := matchingProjects(deps.projects, item.InspectionSummary.ProjectName)
	if len(projectMatches) != 1 {
		return pushApplyResult{}, fmt.Errorf("saved target project %q is no longer resolvable", item.InspectionSummary.ProjectName)
	}
	project := projectMatches[0]

	tag, ok := deps.tagsByIssue[item.TargetIssue]
	if !ok {
		return pushApplyResult{}, fmt.Errorf("issue tag %q was not pre-resolved", item.TargetIssue)
	}

	result := pushApplyResult{}
	switch item.PlannedAction {
	case "create":
		for _, row := range item.Payload {
			if _, err := deps.client.CreateTimeEntry(ctx, deps.cfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
				return result, err
			}
		}
	case "replace":
		entries, err := deps.client.ListUserTimeEntries(ctx, deps.cfg.WorkspaceID, deps.cfg.UserID, item.WindowFromUTC, item.WindowToUTC)
		if err != nil {
			return result, err
		}
		scopeEntries := filterEntriesByProject(filterEntriesByIssue(entries, deps.tagsByID, item.TargetIssue), project.ID)
		deleteRows, createRows := diffScopedRemoteRows(buildClockifyScopedRows(scopeEntries, deps.tagsByID, item.TargetIssue), item.Payload)
		if len(deleteRows) > 0 {
			if err := deleteRemoteClockifyEntries(ctx, deps.client, deps.cfg.WorkspaceID, scopedRemoteRawValues(deleteRows)); err != nil {
				return result, err
			}
		}
		deletedRows := scopedRemoteRowValues(deleteRows)
		if archivedCount, err := s.archiveRemoteTrashRows(item, deletedRows); err != nil {
			result.warnings = append(result.warnings, archiveWarning(item.TargetAdapterFamily, len(deletedRows), err))
		} else {
			result.trashArchivedCount += archivedCount
		}
		for _, row := range createRows {
			if _, err := deps.client.CreateTimeEntry(ctx, deps.cfg.WorkspaceID, row, project.ID, []string{tag.ID}); err != nil {
				return result, err
			}
		}
	case "delete":
		entries, err := deps.client.ListUserTimeEntries(ctx, deps.cfg.WorkspaceID, deps.cfg.UserID, item.WindowFromUTC, item.WindowToUTC)
		if err != nil {
			return result, err
		}
		scopeEntries := filterEntriesByProject(filterEntriesByIssue(entries, deps.tagsByID, item.TargetIssue), project.ID)
		if len(scopeEntries) == 0 {
			scopeEntries = findEntriesByTimeIntervals(entries, item.Payload)
		}
		deletedRows := normalizeDeletedClockifyRows(scopeEntries, deps.tagsByID, item.TargetIssue)
		if err := deleteRemoteClockifyEntries(ctx, deps.client, deps.cfg.WorkspaceID, scopeEntries); err != nil {
			return result, err
		}
		if archivedCount, err := s.archiveRemoteTrashRows(item, deletedRows); err != nil {
			result.warnings = append(result.warnings, archiveWarning(item.TargetAdapterFamily, len(deletedRows), err))
		} else {
			result.trashArchivedCount += archivedCount
		}
	default:
		return pushApplyResult{}, fmt.Errorf("unsupported push action %q", item.PlannedAction)
	}

	return result, nil
}
