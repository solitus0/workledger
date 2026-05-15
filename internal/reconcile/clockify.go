package reconcile

import (
	"context"
	"errors"
	"time"

	"github.com/solitus0/workledger/internal/adapter/clockify"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/progress"
)

func (s *Service) CreateClockifyPullPlan(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	plan, err := s.buildClockifyPullPlan(ctx, cfg, windowFrom, windowTo, options...)
	if err != nil {
		return Plan{}, err
	}
	if err := s.insertPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) buildClockifyPullPlan(ctx context.Context, cfg config.EffectiveConfig, windowFrom, windowTo time.Time, options ...PlanOptions) (Plan, error) {
	opts := resolvePlanOptions(options)
	opts.Reporter.Start(progress.Event{Phase: "fetching", Message: "plan reconcile clockify pull"})
	defer func() {
		opts.Reporter.Finish(progress.Event{Phase: "finalizing", Message: "plan reconcile clockify pull complete"})
	}()

	if cfg.File.Clockify == nil {
		return Plan{}, errors.New("clockify config is required")
	}

	clockifyCfg, err := config.ResolveClockifyConfig(cfg)
	if err != nil {
		return Plan{}, err
	}

	client := s.newClockifyClient(*clockifyCfg)
	entries, err := client.ListUserTimeEntries(ctx, clockifyCfg.WorkspaceID, clockifyCfg.UserID, windowFrom, windowTo)
	if err != nil {
		return Plan{}, err
	}
	tagsByID, err := client.ListTags(ctx, clockifyCfg.WorkspaceID)
	if err != nil {
		return Plan{}, err
	}

	opts.Reporter.Event(progress.Event{Phase: "fetching", ScopeDone: 2, ScopeTotal: 2, Message: "fetched clockify entries and tags"})
	valid, invalid := clockify.NormalizeEntries(entries, tagsByID)
	plan, err := s.buildClockifyPullPlanFromRows(cfg, windowFrom, windowTo, valid, invalid)
	if err != nil {
		return Plan{}, err
	}
	opts.Reporter.Event(progress.Event{Phase: "finalizing", ScopeDone: len(plan.Items), ScopeTotal: len(plan.Items), Message: "built clockify pull plan"})
	return plan, nil
}
