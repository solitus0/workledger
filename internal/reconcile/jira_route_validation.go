package reconcile

import (
	"fmt"
	"strings"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

type jiraIssueOwner struct {
	family   string
	instance string
}

func validateReportingTargetOwnership(cfg config.EffectiveConfig, selectedFamily, routeProfile, targetIssue string) error {
	if !worklogs.IsValidIssueKey(targetIssue) {
		return nil
	}

	prefix, _, ok := strings.Cut(targetIssue, "-")
	if !ok || prefix == "" {
		return nil
	}

	owner, ok := findCrossFamilyIssueOwner(cfg, prefix)
	if !ok || owner.family == selectedFamily {
		return nil
	}

	return fmt.Errorf(
		"reporting target issue %q in %s route profile %q is owned by configured %s instance %q; move this reporting_targets rule there or run with --adapter=%s",
		targetIssue,
		selectedFamily,
		routeProfile,
		owner.family,
		owner.instance,
		owner.family,
	)
}

func findCrossFamilyIssueOwner(cfg config.EffectiveConfig, prefix string) (jiraIssueOwner, bool) {
	if owner, ok := findIssueOwnerInJiraCloud(cfg, prefix); ok {
		return owner, true
	}
	if owner, ok := findIssueOwnerInJiraData(cfg, prefix); ok {
		return owner, true
	}
	return jiraIssueOwner{}, false
}

func findIssueOwnerInJiraCloud(cfg config.EffectiveConfig, prefix string) (jiraIssueOwner, bool) {
	if cfg.File.JiraCloud == nil {
		return jiraIssueOwner{}, false
	}
	for instanceName, instance := range cfg.File.JiraCloud.Instances {
		if routeFamilyOwnsPrefix(instance.Routing, prefix) {
			return jiraIssueOwner{family: "jira-cloud", instance: instanceName}, true
		}
	}
	return jiraIssueOwner{}, false
}

func findIssueOwnerInJiraData(cfg config.EffectiveConfig, prefix string) (jiraIssueOwner, bool) {
	if cfg.File.JiraData == nil {
		return jiraIssueOwner{}, false
	}
	for instanceName, instance := range cfg.File.JiraData.Instances {
		if routeFamilyOwnsPrefix(instance.Routing, prefix) {
			return jiraIssueOwner{family: "jira-data-center", instance: instanceName}, true
		}
	}
	return jiraIssueOwner{}, false
}

func routeFamilyOwnsPrefix(routes *config.JiraInstanceRoutes, prefix string) bool {
	if routes == nil {
		return false
	}
	for _, profile := range routes.Profiles {
		for _, owned := range profile.IssuePrefixes {
			if owned == prefix {
				return true
			}
		}
	}
	return false
}
