package reconcile

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/solitus0/workledger/internal/config"
)

type SelectionRequest struct {
	Adapters     []string
	Instances    []string
	Direction    string
	RouteProfile string
}

type Selection struct {
	Targets        []ReconcileTarget
	SkippedTargets []SkippedTarget
}

type ReconcileTarget struct {
	AdapterFamily string
	Instance      string
}

type SkippedTarget struct {
	AdapterFamily string
	Instance      string
	Reason        string
}

type SelectionError struct {
	Message        string
	SkippedTargets []SkippedTarget
}

func (e SelectionError) Error() string {
	return e.Message
}

func ResolveSelection(effective config.EffectiveConfig, request SelectionRequest) (Selection, error) {
	if request.Direction == "pull" && strings.TrimSpace(request.RouteProfile) != "" {
		return Selection{}, errors.New("--route-profile can only be used with --push")
	}

	implicitAll := len(request.Adapters) == 0 && len(request.Instances) == 0
	adapterSet, err := validateSelectionAdapters(request.Adapters)
	if err != nil {
		return Selection{}, err
	}

	targets, err := collectSelectionTargets(effective, adapterSet, request.Instances, implicitAll)
	if err != nil {
		return Selection{}, err
	}
	targets = uniqueReconcileTargets(targets)
	if len(targets) == 0 {
		return Selection{}, errors.New("no configured reconcile targets found")
	}

	targets, err = filterSelectionTargetsByRouteProfile(effective, targets, request.RouteProfile)
	if err != nil {
		return Selection{}, err
	}

	validTargets, skippedTargets, err := validateSelectionTargets(effective, targets, request.Direction, request.RouteProfile, implicitAll)
	if err != nil {
		return Selection{}, err
	}
	if len(validTargets) == 0 {
		if len(skippedTargets) > 0 {
			return Selection{}, newNoValidSelectionTargetsError(skippedTargets)
		}
		return Selection{}, errors.New("no configured reconcile targets found")
	}

	return Selection{
		Targets:        uniqueReconcileTargets(validTargets),
		SkippedTargets: append([]SkippedTarget(nil), skippedTargets...),
	}, nil
}

func validateSelectionAdapters(adapters []string) (map[string]struct{}, error) {
	adapterSet := map[string]struct{}{}
	for _, adapter := range adapters {
		switch adapter {
		case "clockify", "jira-cloud", "jira-data-center":
			adapterSet[adapter] = struct{}{}
		default:
			return nil, errors.New("supported adapters are clockify, jira-cloud, and jira-data-center")
		}
	}
	return adapterSet, nil
}

func collectSelectionTargets(effective config.EffectiveConfig, adapterSet map[string]struct{}, instances []string, implicitAll bool) ([]ReconcileTarget, error) {
	if implicitAll {
		return configuredReconcileTargets(effective), nil
	}

	targets := make([]ReconcileTarget, 0)
	if len(instances) > 0 {
		for _, instance := range instances {
			family, err := resolveSelectionInstanceFamily(effective, adapterSet, instance)
			if err != nil {
				return nil, err
			}
			if len(adapterSet) > 0 {
				if _, ok := adapterSet[family]; !ok {
					return nil, fmt.Errorf("instance %q belongs to adapter %s, not selected adapters %s", instance, family, strings.Join(sortedSetKeys(adapterSet), ","))
				}
			}
			targets = append(targets, ReconcileTarget{AdapterFamily: family, Instance: instance})
		}
		return targets, nil
	}

	for _, adapter := range sortedSetKeys(adapterSet) {
		resolvedTargets, err := configuredTargetsForAdapter(effective, adapter)
		if err != nil {
			return nil, err
		}
		targets = append(targets, resolvedTargets...)
	}
	return targets, nil
}

func resolveSelectionInstanceFamily(effective config.EffectiveConfig, adapterSet map[string]struct{}, instance string) (string, error) {
	hasJiraCloud := effective.File.JiraCloud != nil
	if hasJiraCloud {
		_, hasJiraCloud = effective.File.JiraCloud.Instances[instance]
	}
	hasJiraData := effective.File.JiraData != nil
	if hasJiraData {
		_, hasJiraData = effective.File.JiraData.Instances[instance]
	}

	switch {
	case hasJiraCloud && hasJiraData:
		if len(adapterSet) == 1 {
			if _, ok := adapterSet["jira-cloud"]; ok {
				return "jira-cloud", nil
			}
			if _, ok := adapterSet["jira-data-center"]; ok {
				return "jira-data-center", nil
			}
		}
		return "", fmt.Errorf("instance %q exists in both jira_cloud and jira_data_center; use --adapter jira-cloud or --adapter jira-data-center", instance)
	case hasJiraCloud:
		return "jira-cloud", nil
	case hasJiraData:
		return "jira-data-center", nil
	case instance == config.ClockifyInstanceName:
		if _, _, err := config.ResolveClockifyInstance(effective, instance); err != nil {
			return "", err
		}
		return "clockify", nil
	default:
		return "", fmt.Errorf("adapter instance %q is not configured", instance)
	}
}

func configuredTargetsForAdapter(effective config.EffectiveConfig, adapter string) ([]ReconcileTarget, error) {
	switch adapter {
	case "clockify":
		if effective.File.Clockify == nil {
			return nil, errors.New("clockify config is required")
		}
		return []ReconcileTarget{{AdapterFamily: "clockify", Instance: config.ClockifyInstanceName}}, nil
	case "jira-cloud":
		if effective.File.JiraCloud == nil || len(effective.File.JiraCloud.Instances) == 0 {
			return nil, errors.New("jira_cloud config is required")
		}
		names := sortedJiraCloudInstanceNames(effective.File.JiraCloud.Instances)
		targets := make([]ReconcileTarget, 0, len(names))
		for _, name := range names {
			targets = append(targets, ReconcileTarget{AdapterFamily: "jira-cloud", Instance: name})
		}
		return targets, nil
	case "jira-data-center":
		if effective.File.JiraData == nil || len(effective.File.JiraData.Instances) == 0 {
			return nil, errors.New("jira_data_center config is required")
		}
		names := sortedJiraDataInstanceNames(effective.File.JiraData.Instances)
		targets := make([]ReconcileTarget, 0, len(names))
		for _, name := range names {
			targets = append(targets, ReconcileTarget{AdapterFamily: "jira-data-center", Instance: name})
		}
		return targets, nil
	default:
		return nil, fmt.Errorf("unsupported adapter family %q", adapter)
	}
}

func configuredReconcileTargets(effective config.EffectiveConfig) []ReconcileTarget {
	targets := make([]ReconcileTarget, 0)
	if effective.File.Clockify != nil {
		targets = append(targets, ReconcileTarget{AdapterFamily: "clockify", Instance: config.ClockifyInstanceName})
	}
	if effective.File.JiraCloud != nil {
		for _, name := range sortedJiraCloudInstanceNames(effective.File.JiraCloud.Instances) {
			targets = append(targets, ReconcileTarget{AdapterFamily: "jira-cloud", Instance: name})
		}
	}
	if effective.File.JiraData != nil {
		for _, name := range sortedJiraDataInstanceNames(effective.File.JiraData.Instances) {
			targets = append(targets, ReconcileTarget{AdapterFamily: "jira-data-center", Instance: name})
		}
	}
	return targets
}

func filterSelectionTargetsByRouteProfile(effective config.EffectiveConfig, targets []ReconcileTarget, routeProfile string) ([]ReconcileTarget, error) {
	profileName := strings.TrimSpace(routeProfile)
	if profileName == "" {
		return uniqueReconcileTargets(targets), nil
	}

	for _, target := range targets {
		if target.AdapterFamily == "clockify" {
			return nil, errors.New("--route-profile can only be used when all selected reconcile targets are jira-cloud or jira-data-center")
		}
	}

	filtered := make([]ReconcileTarget, 0, len(targets))
	for _, target := range targets {
		if jiraTargetHasRouteProfile(effective, target.AdapterFamily, target.Instance, profileName) {
			filtered = append(filtered, target)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("route profile %q is not configured for any selected jira-cloud or jira-data-center target", profileName)
	}
	return uniqueReconcileTargets(filtered), nil
}

func validateSelectionTargets(effective config.EffectiveConfig, targets []ReconcileTarget, direction, routeProfile string, skipInvalid bool) ([]ReconcileTarget, []SkippedTarget, error) {
	validTargets := make([]ReconcileTarget, 0, len(targets))
	skippedTargets := make([]SkippedTarget, 0)
	for _, target := range targets {
		if err := validateSelectionTarget(effective, target, direction, routeProfile); err != nil {
			if !skipInvalid {
				return nil, nil, err
			}
			skippedTargets = append(skippedTargets, SkippedTarget{
				AdapterFamily: target.AdapterFamily,
				Instance:      target.Instance,
				Reason:        err.Error(),
			})
			continue
		}
		validTargets = append(validTargets, target)
	}
	return validTargets, skippedTargets, nil
}

func validateSelectionTarget(effective config.EffectiveConfig, target ReconcileTarget, direction, routeProfile string) error {
	switch target.AdapterFamily {
	case "clockify":
		_, _, err := config.ResolveClockifyInstance(effective, target.Instance)
		return err
	case "jira-cloud":
		if _, _, err := config.ResolveJiraCloudInstance(effective, target.Instance); err != nil {
			return err
		}
		if direction == "push" {
			if strings.TrimSpace(routeProfile) == "" {
				if !jiraTargetHasImplicitPushSelection(effective, target.AdapterFamily, target.Instance) {
					return fmt.Errorf("jira_cloud instance %q does not define an implicit push route profile (default or reporting_targets)", target.Instance)
				}
			} else if !jiraTargetHasRouteProfile(effective, target.AdapterFamily, target.Instance, routeProfile) {
				return fmt.Errorf("jira_cloud instance %q does not define route profile %q", target.Instance, routeProfile)
			}
		}
		return nil
	case "jira-data-center":
		if _, _, err := config.ResolveJiraDataInstance(effective, target.Instance); err != nil {
			return err
		}
		if direction == "push" {
			if strings.TrimSpace(routeProfile) == "" {
				if !jiraTargetHasImplicitPushSelection(effective, target.AdapterFamily, target.Instance) {
					return fmt.Errorf("jira_data_center instance %q does not define an implicit push route profile (default or reporting_targets)", target.Instance)
				}
			} else if !jiraTargetHasRouteProfile(effective, target.AdapterFamily, target.Instance, routeProfile) {
				return fmt.Errorf("jira_data_center instance %q does not define route profile %q", target.Instance, routeProfile)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported adapter family %q", target.AdapterFamily)
	}
}

func newNoValidSelectionTargetsError(skippedTargets []SkippedTarget) error {
	reasons := make([]string, 0, len(skippedTargets))
	for _, target := range skippedTargets {
		scope := target.AdapterFamily
		if strings.TrimSpace(target.Instance) != "" {
			scope += "/" + target.Instance
		}
		reasons = append(reasons, scope+": "+target.Reason)
	}
	return SelectionError{
		Message:        "no valid reconcile targets remained after filtering invalid targets: " + strings.Join(reasons, "; "),
		SkippedTargets: append([]SkippedTarget(nil), skippedTargets...),
	}
}

func jiraTargetHasRouteProfile(effective config.EffectiveConfig, family, instance, routeProfile string) bool {
	profileName := strings.TrimSpace(routeProfile)
	if profileName == "" {
		profileName = "default"
	}
	switch family {
	case "jira-cloud":
		if effective.File.JiraCloud == nil {
			return false
		}
		cfg, ok := effective.File.JiraCloud.Instances[instance]
		if !ok || cfg.Routing == nil {
			return false
		}
		_, ok = cfg.Routing.Profiles[profileName]
		return ok
	case "jira-data-center":
		if effective.File.JiraData == nil {
			return false
		}
		cfg, ok := effective.File.JiraData.Instances[instance]
		if !ok || cfg.Routing == nil {
			return false
		}
		_, ok = cfg.Routing.Profiles[profileName]
		return ok
	default:
		return false
	}
}

func jiraTargetHasImplicitPushSelection(effective config.EffectiveConfig, family, instance string) bool {
	profiles := jiraTargetProfiles(effective, ReconcileTarget{AdapterFamily: family, Instance: instance})
	if len(profiles) == 0 {
		return false
	}
	if _, ok := profiles["default"]; ok {
		return true
	}
	for profileName, profile := range profiles {
		if profileName == "default" {
			continue
		}
		if len(profile.ReportingTargets) > 0 {
			return true
		}
	}
	return false
}

func jiraTargetProfiles(effective config.EffectiveConfig, target ReconcileTarget) map[string]config.JiraRouteProfile {
	switch target.AdapterFamily {
	case "jira-cloud":
		if effective.File.JiraCloud == nil {
			return nil
		}
		instance, ok := effective.File.JiraCloud.Instances[target.Instance]
		if !ok || instance.Routing == nil {
			return nil
		}
		return instance.Routing.Profiles
	case "jira-data-center":
		if effective.File.JiraData == nil {
			return nil
		}
		instance, ok := effective.File.JiraData.Instances[target.Instance]
		if !ok || instance.Routing == nil {
			return nil
		}
		return instance.Routing.Profiles
	default:
		return nil
	}
}

func uniqueReconcileTargets(targets []ReconcileTarget) []ReconcileTarget {
	seen := map[string]ReconcileTarget{}
	for _, target := range targets {
		seen[target.AdapterFamily+"\x00"+target.Instance] = target
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	unique := make([]ReconcileTarget, 0, len(keys))
	for _, key := range keys {
		unique = append(unique, seen[key])
	}
	return unique
}
