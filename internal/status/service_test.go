package status

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestCheckBuildsFrontendNeutralReport(t *testing.T) {
	cfg := config.EffectiveConfig{SQLitePath: "/tmp/workledger.db", Location: time.UTC}
	service := NewServiceWith(Dependencies{
		ValidateConfig: func() (config.EffectiveConfig, []config.ValidationIssue, error) {
			return cfg, nil, nil
		},
		CheckStorage: func(path, operation string) error {
			if path != cfg.SQLitePath || operation != "status" {
				t.Fatalf("unexpected storage check %q %q", path, operation)
			}
			return nil
		},
		CheckConnectivity: func(context.Context, config.EffectiveConfig) []Item {
			return []Item{{Category: "connectivity", Target: "jira-cloud:product", Status: "error", Message: "unauthorized", FailureKind: FailureAuth}}
		},
	})

	report, err := service.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if len(report.Items) != 4 {
		t.Fatalf("unexpected report %#v", report.Items)
	}
	if report.Items[0].Category != "local" || report.Items[1].Category != "local" || report.Items[2].Category != "routing" || report.Items[3].FailureKind != FailureAuth {
		t.Fatalf("unexpected item ordering %#v", report.Items)
	}
}

func TestCheckSkipsDependentChecksForInvalidConfig(t *testing.T) {
	connectivityCalled := false
	service := NewServiceWith(Dependencies{
		ValidateConfig: func() (config.EffectiveConfig, []config.ValidationIssue, error) {
			return config.EffectiveConfig{}, []config.ValidationIssue{{Field: "timezone", Message: "required"}}, nil
		},
		CheckConnectivity: func(context.Context, config.EffectiveConfig) []Item {
			connectivityCalled = true
			return nil
		},
	})
	report, err := service.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if connectivityCalled {
		t.Fatal("connectivity should be skipped")
	}
	if len(report.Items) != 4 || report.Items[0].FailureKind != FailureValidation {
		t.Fatalf("unexpected report %#v", report.Items)
	}
}

func TestCheckHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService().Check(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
