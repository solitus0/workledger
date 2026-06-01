package reconcile

import "github.com/solitus0/workledger/internal/config"

func validateReportingTargetOwnership(cfg config.EffectiveConfig, selectedFamily, routeProfile, targetIssue string) error {
	return config.ValidateReportingTargetOwnership(cfg, selectedFamily, routeProfile, targetIssue)
}
