package reconcile

import "github.com/solitus0/workledger/internal/progress"

type PlanOptions struct {
	Reporter                           progress.Reporter
	SuppressMissingRoutes              bool
	ExcludedRemoteOwnedIssueKeys       []string
	PreserveNonActionableReportingPlan bool
}

type ApplyOptions struct {
	Reporter progress.Reporter
}

func resolvePlanOptions(options []PlanOptions) PlanOptions {
	if len(options) == 0 {
		return PlanOptions{Reporter: progress.NewNoopReporter()}
	}
	resolved := options[0]
	if resolved.Reporter == nil {
		resolved.Reporter = progress.NewNoopReporter()
	}
	return resolved
}

func resolveApplyOptions(options []ApplyOptions) ApplyOptions {
	if len(options) == 0 {
		return ApplyOptions{Reporter: progress.NewNoopReporter()}
	}
	resolved := options[0]
	if resolved.Reporter == nil {
		resolved.Reporter = progress.NewNoopReporter()
	}
	return resolved
}
