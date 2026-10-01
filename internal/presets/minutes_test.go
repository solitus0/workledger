package presets

import (
	"context"
	"testing"
)

func TestPresetCreationUpdateAndApplyRoundMinutes(t *testing.T) {
	store, service, cfg := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	input := validInput("rounded")
	input.Duration = "15m30s"
	preset, err := service.Create(ctx, cfg, input)
	if err != nil || preset.DurationSeconds != 960 {
		t.Fatalf("create = %#v, %v", preset, err)
	}
	duration := "16m30s"
	preset, err = service.Update(ctx, cfg, preset.Name, PatchInput{Duration: &duration, ExpectedRevision: preset.Revision})
	if err != nil || preset.DurationSeconds != 1020 {
		t.Fatalf("update = %#v, %v", preset, err)
	}
	result, err := service.Apply(ctx, cfg, preset.Name, ApplyInput{Date: "2026-05-01", DryRun: true})
	if err != nil || result.Records[0].DurationSeconds != 1020 {
		t.Fatalf("apply = %#v, %v", result, err)
	}
}
