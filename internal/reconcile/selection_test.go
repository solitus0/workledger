package reconcile

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestResolveSelectionBareTargetsAllConfiguredTargets(t *testing.T) {
	selection, err := ResolveSelection(selectionConfig(), SelectionRequest{Direction: "pull"})
	if err != nil {
		t.Fatalf("ResolveSelection failed: %v", err)
	}

	want := []ReconcileTarget{
		{AdapterFamily: "clockify", Instance: config.ClockifyInstanceName},
		{AdapterFamily: "jira-cloud", Instance: "ops"},
		{AdapterFamily: "jira-cloud", Instance: "product"},
		{AdapterFamily: "jira-data-center", Instance: "internal"},
	}
	if !sameTargets(selection.Targets, want) {
		t.Fatalf("unexpected targets %#v", selection.Targets)
	}
}

func TestResolveSelectionDedupesRepeatedInputs(t *testing.T) {
	selection, err := ResolveSelection(selectionConfig(), SelectionRequest{
		Direction: "push",
		Adapters:  []string{"jira-cloud", "jira-cloud"},
		Instances: []string{"product", "product"},
	})
	if err != nil {
		t.Fatalf("ResolveSelection failed: %v", err)
	}

	want := []ReconcileTarget{{AdapterFamily: "jira-cloud", Instance: "product"}}
	if !sameTargets(selection.Targets, want) {
		t.Fatalf("unexpected targets %#v", selection.Targets)
	}
}

func TestResolveSelectionRequiresAdapterForAmbiguousJiraInstance(t *testing.T) {
	cfg := selectionConfig()
	cfg.File.JiraCloud.Instances["shared"] = cfg.File.JiraCloud.Instances["product"]
	cfg.File.JiraData.Instances["shared"] = cfg.File.JiraData.Instances["internal"]

	_, err := ResolveSelection(cfg, SelectionRequest{Direction: "push", Instances: []string{"shared"}})
	if err == nil || !strings.Contains(err.Error(), `instance "shared" exists in both jira_cloud and jira_data_center`) {
		t.Fatalf("expected ambiguous instance error, got %v", err)
	}
}

func TestResolveSelectionImplicitAllSkipsInvalidTargets(t *testing.T) {
	cfg := selectionConfig()
	cfg.File.Clockify.Auth.APIKey = ""
	cfg.File.Clockify.Auth.APIKeyEnv = ""

	selection, err := ResolveSelection(cfg, SelectionRequest{Direction: "push"})
	if err != nil {
		t.Fatalf("ResolveSelection failed: %v", err)
	}
	if len(selection.SkippedTargets) != 1 || selection.SkippedTargets[0].AdapterFamily != "clockify" {
		t.Fatalf("expected skipped clockify target, got %#v", selection.SkippedTargets)
	}
	for _, target := range selection.Targets {
		if target.AdapterFamily == "clockify" {
			t.Fatalf("clockify should not remain valid, got %#v", selection.Targets)
		}
	}
}

func TestResolveSelectionImplicitAllFailsWhenNoValidTargetRemains(t *testing.T) {
	cfg := selectionConfig()
	cfg.File.Clockify.Auth.APIKey = ""
	cfg.File.Clockify.Auth.APIKeyEnv = ""
	cfg.File.JiraCloud = nil
	cfg.File.JiraData = nil

	_, err := ResolveSelection(cfg, SelectionRequest{Direction: "push"})
	var selectionErr SelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("expected SelectionError, got %T: %v", err, err)
	}
	if len(selectionErr.SkippedTargets) != 1 || selectionErr.SkippedTargets[0].AdapterFamily != "clockify" {
		t.Fatalf("unexpected skipped targets %#v", selectionErr.SkippedTargets)
	}
}

func TestResolveSelectionRouteProfileRejectsClockifyScopes(t *testing.T) {
	_, err := ResolveSelection(selectionConfig(), SelectionRequest{
		Direction:    "push",
		Adapters:     []string{"clockify", "jira-cloud"},
		RouteProfile: "reporting",
	})
	if err == nil || !strings.Contains(err.Error(), "--route-profile can only be used when all selected reconcile targets are jira-cloud or jira-data-center") {
		t.Fatalf("expected strict route-profile error, got %v", err)
	}
}

func TestResolveSelectionRouteProfileKeepsOnlyMatchingJiraTargets(t *testing.T) {
	cfg := selectionConfig()
	delete(cfg.File.JiraCloud.Instances["ops"].Routing.Profiles, "reporting")

	selection, err := ResolveSelection(cfg, SelectionRequest{
		Direction:    "push",
		Adapters:     []string{"jira-cloud"},
		RouteProfile: "reporting",
	})
	if err != nil {
		t.Fatalf("ResolveSelection failed: %v", err)
	}

	want := []ReconcileTarget{{AdapterFamily: "jira-cloud", Instance: "product"}}
	if !sameTargets(selection.Targets, want) {
		t.Fatalf("unexpected targets %#v", selection.Targets)
	}
}

func selectionConfig() config.EffectiveConfig {
	return config.EffectiveConfig{
		SQLitePath:             "/tmp/workledger.db",
		MinimumDurationSeconds: 900,
		Location:               time.UTC,
		File: config.FileConfig{
			Clockify: &config.ClockifyConfig{
				WorkspaceID: "workspace",
				UserID:      "user",
				Auth:        config.ClockifyAuthConfig{APIKey: "clockify-token"},
			},
			JiraCloud: &config.JiraCloudConfig{
				Instances: map[string]config.JiraCloudInstance{
					"ops": {
						BaseURL: "https://ops.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "ops-token"},
						Routing: selectionRoutes("OPS"),
					},
					"product": {
						BaseURL: "https://product.atlassian.net",
						Auth:    config.JiraCloudAuthBlock{Email: "user@example.com", Token: "product-token"},
						Routing: selectionRoutes("AAPP"),
					},
				},
			},
			JiraData: &config.JiraDataCenterConfig{
				Instances: map[string]config.JiraDataCenterInstance{
					"internal": {
						BaseURL: "https://jira.example.com",
						Auth:    config.JiraDataCenterAuthWrap{Bearer: config.JiraDataCenterBearer{Token: "data-token"}},
						Routing: selectionRoutes("DAPP"),
					},
				},
			},
		},
	}
}

func selectionRoutes(prefix string) *config.JiraInstanceRoutes {
	return &config.JiraInstanceRoutes{
		Profiles: map[string]config.JiraRouteProfile{
			"default":   {IssuePrefixes: []string{prefix}},
			"reporting": {ReportingTargets: map[string]string{prefix: "REPORT-1"}},
		},
	}
}

func sameTargets(got, want []ReconcileTarget) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
