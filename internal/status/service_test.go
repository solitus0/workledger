package status

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
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
	if report.ConfigSummary == nil || report.ConfigSummary.SQLitePath != cfg.SQLitePath || len(report.ConfigIssues) != 0 {
		t.Fatalf("unexpected config snapshot %#v issues=%#v", report.ConfigSummary, report.ConfigIssues)
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
	if report.ConfigSummary != nil || len(report.ConfigIssues) != 1 || report.ConfigIssues[0].Field != "timezone" {
		t.Fatalf("unexpected invalid config snapshot summary=%#v issues=%#v", report.ConfigSummary, report.ConfigIssues)
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

func TestStorageCheckValidatesExistingSchemaWithoutRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workledger.db")
	store, _, err := sqlitestore.Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	_ = store.Close()

	if err := checkStorage(path, "status"); err != nil {
		t.Fatalf("valid storage rejected: %v", err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(invalid, []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write invalid storage: %v", err)
	}
	if err := checkStorage(invalid, "status"); err == nil {
		t.Fatal("existing invalid storage should fail schema validation")
	}
}
