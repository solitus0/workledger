package reconcile

import (
	"context"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
)

type pushExecutionSummary struct {
	appliedCount       int
	failedCount        int
	scopeDone          int
	workDone           int
	trashArchivedCount int
	scopeResults       []ApplyScopeResult
}

type pushExecutionGroup struct {
	key   string
	items []PlanItem
}

type pushExecutionOutcome struct {
	item               PlanItem
	executed           bool
	failed             bool
	finalState         string
	attemptMessage     string
	applyMessage       string
	workDone           int
	trashArchivedCount int
	warnings           []string
}

func splitPlanExecutionItems(items []PlanItem) (pullItems []PlanItem, pushItems []PlanItem) {
	for _, item := range items {
		if item.PlanDirection == "push" {
			pushItems = append(pushItems, item)
			continue
		}
		pullItems = append(pullItems, item)
	}
	return pullItems, pushItems
}

func totalApplyWorkUnits(items []PlanItem) int {
	total := 0
	for _, item := range items {
		total += applyWorkUnits(item)
	}
	return total
}

func applyWorkUnits(item PlanItem) int {
	switch item.PlannedAction {
	case "create", "merge":
		return len(item.Payload)
	case "replace":
		return item.RemoteRowCount + len(item.Payload)
	case "delete":
		return item.RemoteRowCount
	default:
		return 0
	}
}

func planItemScopeLabel(item PlanItem) string {
	if item.TargetIssue != "" {
		return item.TargetIssue
	}
	return item.IssueKey
}

func (s *Service) executeSavedPushGroups(ctx context.Context, cfg config.EffectiveConfig, retryScope string, items []PlanItem, initialScopeDone, initialWorkDone, scopeTotal, workTotal int, reporter progress.Reporter) (pushExecutionSummary, error) {
	groups := groupPushExecutionItems(items)
	for _, group := range groups {
		for _, item := range group.items {
			if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "pending", "push delivery started"); err != nil {
				return pushExecutionSummary{}, err
			}
		}
	}

	// Pre-resolve Clockify projects and tags sequentially before any goroutine starts.
	var clockifyItems []PlanItem
	for _, g := range groups {
		if len(g.items) > 0 && g.items[0].TargetAdapterFamily == "clockify" {
			clockifyItems = append(clockifyItems, g.items...)
		}
	}
	clockifyCtx := ctx
	if len(clockifyItems) > 0 {
		deps, err := s.prepareClockifyPushDeps(ctx, cfg, clockifyItems)
		if err != nil {
			return pushExecutionSummary{}, err
		}
		clockifyCtx = withClockifyDeps(ctx, deps)
	}
	numClockifyGroups := len(clockifyItems)

	outcomes := make(chan pushExecutionOutcome, len(items))
	var wg sync.WaitGroup
	for _, group := range groups {
		group := group
		isClockify := len(group.items) > 0 && group.items[0].TargetAdapterFamily == "clockify"
		itemCtx := ctx
		if isClockify {
			itemCtx = clockifyCtx
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if isClockify && numClockifyGroups > 1 {
				jitter := time.Duration(rand.Int63n(int64(time.Duration(numClockifyGroups) * 200 * time.Millisecond)))
				select {
				case <-itemCtx.Done():
					return
				case <-time.After(jitter):
				}
			}
			for _, item := range group.items {
				outcomes <- s.performPushItem(itemCtx, cfg, item, retryScope)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(outcomes)
	}()

	summary := pushExecutionSummary{scopeDone: initialScopeDone, workDone: initialWorkDone}
	for outcome := range outcomes {
		appliedAt := s.now().UTC()
		if err := s.recordDeliveryAttempt(outcome.item.PlanID, outcome.item.ID, outcome.finalState, outcome.attemptMessage); err != nil {
			return pushExecutionSummary{}, err
		}
		if err := s.markItemApplied(outcome.item.ID, appliedAt, outcome.finalState, outcome.applyMessage); err != nil {
			return pushExecutionSummary{}, err
		}
		if outcome.executed {
			summary.appliedCount++
		}
		if outcome.failed {
			summary.failedCount++
		}
		summary.trashArchivedCount += outcome.trashArchivedCount
		if outcome.trashArchivedCount > 0 || len(outcome.warnings) > 0 {
			summary.scopeResults = append(summary.scopeResults, ApplyScopeResult{
				PlanItemID:         outcome.item.ID,
				ScopeLabel:         planItemScopeLabel(outcome.item),
				PlanDirection:      outcome.item.PlanDirection,
				PlannedAction:      outcome.item.PlannedAction,
				TrashArchivedCount: outcome.trashArchivedCount,
				Warnings:           append([]string(nil), outcome.warnings...),
			})
		}
		summary.scopeDone++
		summary.workDone += outcome.workDone
		reporter.Event(progress.Event{
			Phase:         "applying",
			ScopeLabel:    planItemScopeLabel(outcome.item),
			PlannedAction: outcome.item.PlannedAction,
			ScopeDone:     summary.scopeDone,
			ScopeTotal:    scopeTotal,
			WorkDone:      summary.workDone,
			WorkTotal:     workTotal,
			Failed:        summary.failedCount,
			Message:       outcome.applyMessage,
		})
	}
	return summary, nil
}

func groupPushExecutionItems(items []PlanItem) []pushExecutionGroup {
	grouped := map[string][]PlanItem{}
	keys := make([]string, 0)
	for _, item := range items {
		key := item.TargetAdapterFamily + "\x00" + item.TargetAdapterInstance + "\x00" + item.TargetIssue
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], item)
	}
	sort.Strings(keys)
	groups := make([]pushExecutionGroup, 0, len(keys))
	for _, key := range keys {
		groupItems := grouped[key]
		sort.Slice(groupItems, func(i, j int) bool {
			if !groupItems[i].WindowFromUTC.Equal(groupItems[j].WindowFromUTC) {
				return groupItems[i].WindowFromUTC.Before(groupItems[j].WindowFromUTC)
			}
			return groupItems[i].ID < groupItems[j].ID
		})
		groups = append(groups, pushExecutionGroup{key: key, items: groupItems})
	}
	return groups
}

func (s *Service) performPushItem(ctx context.Context, cfg config.EffectiveConfig, item PlanItem, retryScope string) pushExecutionOutcome {
	outcome := pushExecutionOutcome{
		item:           item,
		failed:         false,
		finalState:     "succeeded",
		attemptMessage: "push delivery succeeded",
		applyMessage:   pushApplySuccessMessage(item.TargetAdapterFamily),
		workDone:       applyWorkUnits(item),
	}

	if retryScope == "uncertain" {
		reconciledState, message, err := s.reconcileUncertainPushItem(ctx, cfg, item)
		if err == nil {
			switch reconciledState {
			case "succeeded":
				outcome.executed = true
				outcome.attemptMessage = message
				outcome.applyMessage = message
				return outcome
			case "uncertain":
				outcome.failed = true
				outcome.finalState = "uncertain"
				outcome.attemptMessage = message
				outcome.applyMessage = message
				return outcome
			}
		}
		if err != nil {
			outcome.failed = true
			outcome.finalState = "uncertain"
			outcome.attemptMessage = err.Error()
			outcome.applyMessage = err.Error()
			return outcome
		}
	}

	pushResult, err := s.applyPushItem(ctx, cfg, item)
	if err != nil {
		outcome.failed = true
		outcome.finalState = "failed"
		if retryScope == "uncertain" {
			outcome.finalState = "uncertain"
		}
		outcome.attemptMessage = err.Error()
		outcome.applyMessage = err.Error()
		outcome.trashArchivedCount = pushResult.trashArchivedCount
		outcome.warnings = append([]string(nil), pushResult.warnings...)
		return outcome
	}
	if err := s.clearDeleteTombstones(item); err != nil {
		outcome.failed = true
		outcome.finalState = "failed"
		outcome.attemptMessage = err.Error()
		outcome.applyMessage = err.Error()
		outcome.trashArchivedCount = pushResult.trashArchivedCount
		outcome.warnings = append([]string(nil), pushResult.warnings...)
		return outcome
	}
	if len(pushResult.warnings) > 0 {
		outcome.applyMessage = outcome.applyMessage + " with warnings"
	}
	outcome.executed = true
	outcome.trashArchivedCount = pushResult.trashArchivedCount
	outcome.warnings = append([]string(nil), pushResult.warnings...)
	return outcome
}
