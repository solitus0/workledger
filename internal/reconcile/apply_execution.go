package reconcile

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
	"github.com/solitus0/workledger/internal/reconcile/model"
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

type groupedPushOutcomeDetails struct {
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

func allReadyPushExecutionItems(items []PlanItem) []PlanItem {
	ready := make([]PlanItem, 0)
	for _, item := range items {
		if item.PlanDirection == "push" && item.PlanStatus == "ready" {
			ready = append(ready, item)
		}
	}
	return ready
}

func planItemScopeLabel(item PlanItem) string {
	if item.TargetIssue != "" {
		return item.TargetIssue
	}
	return item.IssueKey
}

func (s *Service) executeSavedPushGroups(ctx context.Context, cfg config.EffectiveConfig, retryScope string, items []PlanItem, allReadyItems []PlanItem, initialScopeDone, initialWorkDone, scopeTotal, workTotal int, reporter progress.Reporter) (pushExecutionSummary, error) {
	selectedGroups := groupPushExecutionItems(items)
	allReadyGroups := groupPushExecutionItems(allReadyItems)
	fullGroupByKey := make(map[string][]PlanItem, len(allReadyGroups))
	for _, group := range allReadyGroups {
		fullGroupByKey[group.key] = group.items
	}

	precomputedOutcomes := make([]pushExecutionOutcome, 0)
	groups := make([]pushExecutionGroup, 0, len(selectedGroups))
	for _, group := range selectedGroups {
		fullGroup := fullGroupByKey[group.key]
		if len(fullGroup) == 0 {
			fullGroup = group.items
		}
		if retryScope != "" && len(group.items) < len(fullGroup) {
			outcomes, err := s.resolvePartialPushRetryGroup(ctx, cfg, group.items, fullGroup)
			if err != nil {
				return pushExecutionSummary{}, err
			}
			precomputedOutcomes = append(precomputedOutcomes, outcomes...)
			continue
		}
		groups = append(groups, group)
	}

	// Pre-resolve Clockify projects and tags sequentially before any goroutine starts.
	var clockifyItems []PlanItem
	numClockifyGroups := 0
	for _, group := range groups {
		if len(group.items) > 0 && group.items[0].TargetAdapterFamily == "clockify" {
			clockifyItems = append(clockifyItems, group.items...)
			numClockifyGroups++
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

	for _, group := range groups {
		for _, item := range group.items {
			if err := s.recordDeliveryAttempt(item.PlanID, item.ID, "pending", "push delivery started"); err != nil {
				return pushExecutionSummary{}, err
			}
		}
	}

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
			if len(group.items) > 1 {
				for _, outcome := range s.performPushGroup(itemCtx, cfg, group.items, retryScope) {
					outcomes <- outcome
				}
				return
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
	for _, outcome := range precomputedOutcomes {
		if err := s.recordPushExecutionOutcome(&summary, outcome, reporter, scopeTotal, workTotal); err != nil {
			return pushExecutionSummary{}, err
		}
	}
	for outcome := range outcomes {
		if err := s.recordPushExecutionOutcome(&summary, outcome, reporter, scopeTotal, workTotal); err != nil {
			return pushExecutionSummary{}, err
		}
	}
	return summary, nil
}

func (s *Service) recordPushExecutionOutcome(summary *pushExecutionSummary, outcome pushExecutionOutcome, reporter progress.Reporter, scopeTotal, workTotal int) error {
	appliedAt := s.now().UTC()
	if err := s.recordDeliveryAttempt(outcome.item.PlanID, outcome.item.ID, outcome.finalState, outcome.attemptMessage); err != nil {
		return err
	}
	if err := s.markItemApplied(outcome.item.ID, appliedAt, outcome.finalState, outcome.applyMessage); err != nil {
		return err
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
	return nil
}

func groupPushExecutionItems(items []PlanItem) []pushExecutionGroup {
	grouped := map[string][]PlanItem{}
	keys := make([]string, 0)
	for _, item := range items {
		key := pushExecutionGroupKey(item)
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

func pushExecutionGroupKey(item PlanItem) string {
	return item.TargetAdapterFamily + "\x00" + item.TargetAdapterInstance + "\x00" + item.TargetIssue
}

func (s *Service) performPushGroup(ctx context.Context, cfg config.EffectiveConfig, items []PlanItem, retryScope string) []pushExecutionOutcome {
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		return []pushExecutionOutcome{s.performPushItem(ctx, cfg, items[0], retryScope)}
	}

	merged := mergePushGroupItem(items)
	if retryScope == "uncertain" {
		reconciledState, message, err := s.reconcileUncertainPushItem(ctx, cfg, merged)
		if err == nil {
			switch reconciledState {
			case "succeeded":
				return buildPushGroupOutcomes(items, pushExecutionOutcome{
					item:           merged,
					executed:       true,
					finalState:     "succeeded",
					attemptMessage: message,
					applyMessage:   message,
				})
			case "uncertain":
				return buildPushGroupOutcomes(items, pushExecutionOutcome{
					item:           merged,
					failed:         true,
					finalState:     "uncertain",
					attemptMessage: message,
					applyMessage:   message,
				})
			}
		}
		if err != nil {
			return buildPushGroupOutcomes(items, pushExecutionOutcome{
				item:           merged,
				failed:         true,
				finalState:     "uncertain",
				attemptMessage: err.Error(),
				applyMessage:   err.Error(),
			})
		}
	}

	pushResult, err := s.applyPushItem(ctx, cfg, merged)
	if err != nil {
		finalState := "failed"
		if retryScope == "uncertain" {
			finalState = "uncertain"
		}
		return buildPushGroupOutcomes(items, pushExecutionOutcome{
			item:               merged,
			failed:             true,
			finalState:         finalState,
			attemptMessage:     err.Error(),
			applyMessage:       err.Error(),
			trashArchivedCount: pushResult.trashArchivedCount,
			warnings:           append([]string(nil), pushResult.warnings...),
		})
	}
	return finalizeSuccessfulPushGroup(items, merged.TargetAdapterFamily, pushResult)
}

func (s *Service) resolvePartialPushRetryGroup(ctx context.Context, cfg config.EffectiveConfig, selectedItems []PlanItem, fullGroupItems []PlanItem) ([]pushExecutionOutcome, error) {
	merged := mergePushGroupItem(fullGroupItems)
	reconciledState, message, err := s.reconcileUncertainPushItem(ctx, cfg, merged)
	if err != nil {
		return nil, err
	}
	if reconciledState == "succeeded" {
		return finalizePartialPushRetrySuccess(selectedItems, message), nil
	}

	detail := strings.TrimSpace(message)
	if detail == "" {
		detail = "shared push scope no longer matches the saved full-group target state"
	}
	return nil, errors.New(sharedPushScopeLabel(merged) + ": " + detail + "; rerun 'workledger plan reconcile' instead of retrying a subset")
}

func mergePushGroupItem(items []PlanItem) PlanItem {
	merged := items[0]
	payload := make([]model.Row, 0)
	for _, item := range items {
		if item.PlannedAction == "delete" {
			continue
		}
		payload = append(payload, item.Payload...)
	}
	merged.Payload = sortRows(payload)
	if len(merged.Payload) == 0 {
		merged.PlannedAction = "delete"
	} else {
		merged.PlannedAction = "replace"
	}
	return merged
}

func sharedPushScopeLabel(item PlanItem) string {
	scope := item.TargetAdapterFamily
	if item.TargetAdapterInstance != "" {
		scope += "/" + item.TargetAdapterInstance
	}
	if item.TargetIssue != "" {
		scope += "/" + item.TargetIssue
	}
	return scope
}

func buildPushGroupOutcomes(items []PlanItem, base pushExecutionOutcome) []pushExecutionOutcome {
	outcomes := make([]pushExecutionOutcome, 0, len(items))
	for i, item := range items {
		outcome := pushExecutionOutcome{
			item:           item,
			executed:       base.executed,
			failed:         base.failed,
			finalState:     base.finalState,
			attemptMessage: base.attemptMessage,
			applyMessage:   base.applyMessage,
			workDone:       applyWorkUnits(item),
		}
		if i == 0 {
			outcome.trashArchivedCount = base.trashArchivedCount
			outcome.warnings = append([]string(nil), base.warnings...)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func finalizeSuccessfulPushGroup(items []PlanItem, adapterFamily string, pushResult pushApplyResult) []pushExecutionOutcome {
	message := pushApplySuccessMessage(adapterFamily)
	if len(pushResult.warnings) > 0 {
		message += " with warnings"
	}

	detailsByItem := allocateGroupedPushOutcomeDetails(items, pushResult)
	outcomes := make([]pushExecutionOutcome, 0, len(items))
	for _, item := range items {
		details := detailsByItem[item.ID]
		outcome := pushExecutionOutcome{
			item:               item,
			executed:           true,
			finalState:         "succeeded",
			attemptMessage:     "push delivery succeeded",
			applyMessage:       message,
			workDone:           applyWorkUnits(item),
			trashArchivedCount: details.trashArchivedCount,
			warnings:           append([]string(nil), details.warnings...),
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func finalizePartialPushRetrySuccess(items []PlanItem, message string) []pushExecutionOutcome {
	outcomes := make([]pushExecutionOutcome, 0, len(items))
	for _, item := range items {
		outcome := pushExecutionOutcome{
			item:           item,
			executed:       true,
			finalState:     "succeeded",
			attemptMessage: message,
			applyMessage:   message,
			workDone:       applyWorkUnits(item),
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func allocateGroupedPushOutcomeDetails(items []PlanItem, pushResult pushApplyResult) map[string]groupedPushOutcomeDetails {
	detailsByItem := make(map[string]groupedPushOutcomeDetails, len(items))
	if len(items) == 0 {
		return detailsByItem
	}

	affectedIDs := make([]string, 0, len(items))
	affectedSet := make(map[string]struct{}, len(items))
	for _, deletedRow := range pushResult.deletedRows {
		itemID := groupedDeletedRowOwner(items, deletedRow)
		details := detailsByItem[itemID]
		details.trashArchivedCount++
		detailsByItem[itemID] = details
		if _, ok := affectedSet[itemID]; !ok {
			affectedSet[itemID] = struct{}{}
			affectedIDs = append(affectedIDs, itemID)
		}
	}

	if len(affectedIDs) == 0 && len(pushResult.warnings) > 0 {
		affectedIDs = append(affectedIDs, items[0].ID)
	}
	for _, itemID := range affectedIDs {
		details := detailsByItem[itemID]
		details.warnings = append([]string(nil), pushResult.warnings...)
		detailsByItem[itemID] = details
	}

	return detailsByItem
}

func groupedDeletedRowOwner(items []PlanItem, deletedRow model.Row) string {
	for _, item := range items {
		if deletedRowMatchesPlanItem(item, deletedRow) {
			return item.ID
		}
	}
	return items[0].ID
}

func deletedRowMatchesPlanItem(item PlanItem, deletedRow model.Row) bool {
	sourceIssueKeys := item.InspectionSummary.SourceIssueKeys
	if len(sourceIssueKeys) == 0 && item.IssueKey != "" {
		sourceIssueKeys = []string{item.IssueKey}
	}
	for _, sourceIssueKey := range sourceIssueKeys {
		if deletedRow.IssueKey == sourceIssueKey {
			return true
		}
		if strings.HasPrefix(deletedRow.Description, sourceIssueKey+" | ") {
			return true
		}
	}
	return false
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
	if len(pushResult.warnings) > 0 {
		outcome.applyMessage = outcome.applyMessage + " with warnings"
	}
	outcome.executed = true
	outcome.trashArchivedCount = pushResult.trashArchivedCount
	outcome.warnings = append([]string(nil), pushResult.warnings...)
	return outcome
}
