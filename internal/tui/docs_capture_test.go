//go:build docs

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

// Opt-in production rendering with fictional data and isolated local storage.
func TestDocumentationCapture(t *testing.T) {
	output := os.Getenv("WORKLEDGER_CAPTURE_DIR")
	if output == "" {
		t.Skip("set WORKLEDGER_CAPTURE_DIR to generate documentation frames")
	}
	data, err := os.ReadFile("../../docs/examples/showcase.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Date, Timezone string
		Issues         []worklogs.IssueMetadataPatch
		Adds           []worklogs.RawApplyAdd
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation(fixture.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	date, err := time.ParseInLocation("2006-01-02", fixture.Date, location)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := sqlitestore.Bootstrap(filepath.Join(t.TempDir(), "showcase.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.EffectiveConfig{Location: location, TimezoneName: fixture.Timezone, MinimumDurationSeconds: 900, DailyMinimumQuotaSeconds: 28800, DayStart: "08:00", DayEnd: "17:00", DailyLunch: "12:00-12:45"}
	ctx := context.Background()
	service := worklogs.NewService(store)
	if err := service.ApplyIssueMetadata(ctx, fixture.Issues, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(ctx, cfg, worklogs.RawApplyPayload{Adds: fixture.Adds}, false, false); err != nil {
		t.Fatal(err)
	}
	presetService := presets.NewService(store)
	for i := 0; i < 24; i++ {
		if _, err := presetService.Create(ctx, cfg, presets.CreateInput{Name: fmt.Sprintf("atlas-review-%02d", i+1), IssueKey: "APP-214", StartTime: "16:00", Duration: "30m", Description: "Review the Atlas activity feed"}); err != nil {
			t.Fatal(err)
		}
	}
	deps := dependencies{now: func() time.Time { return date.Add(16 * time.Hour) }, theme: DefaultTheme()}
	ws := &workspace{cfg: cfg, worklogs: service, presets: presetService, plans: &fakePlans{}}
	base := func(width, height int, colors Palette) model {
		m := newModel(ctx, deps, ws, nil)
		t.Cleanup(m.statusCancel)
		m.width, m.height = width, height
		m.colors = colors
		m.statusLoading, m.activityLoading, m.planLoading = false, false, false
		next, _ := m.Update(m.worklogCmd(m.worklogGen, date)())
		m = next.(model)
		m.worklogSelected = 4
		m.issueTotalIssue = m.worklogs[4].IssueKey
		m.issueTotal, err = service.ActiveIssueTotalSeconds(ctx, m.issueTotalIssue)
		if err != nil {
			t.Fatal(err)
		}
		m.issueTotalLoading = false
		m.presets, err = presetService.List(ctx, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		m.presetLoading = false
		return m
	}
	capture := func(name string, width, height int, prepare func(*model)) {
		for _, theme := range []struct {
			name   string
			colors Palette
		}{{"dark", deps.theme.Dark}, {"light", deps.theme.Light}} {
			m := base(width, height, theme.colors)
			prepare(&m)
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m = next.(model)
			if err := os.WriteFile(filepath.Join(output, name+"-"+theme.name+".ansi"), []byte(m.View().Content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	capture("worklogs", 110, 34, func(*model) {})
	capture("worklogs-wide", 140, 36, func(*model) {})
	capture("worklogs-minimum", 100, 28, func(*model) {})
	capture("worklogs-spacious", 180, 48, func(*model) {})
	capture("add-fill-minimum", 100, 28, func(m *model) {
		m.openAddForm()
		m.form.inputs[issueInput].SetValue("APP-214")
		m.form.inputs[durationInput].SetValue("1h")
		m.form.inputs[descriptionInput].SetValue("Review activity feed keyboard navigation and focus states")
		preparePlacementCapture(t, m, service)
	})
	capture("preset-picker-minimum", 100, 28, func(m *model) {
		m.openPresetPicker()
		for i := 0; i < 15; i++ {
			next, _ := m.Update(specialKey(tea.KeyDown))
			*m = next.(model)
		}
	})
	capture("plan-dates", 110, 34, func(m *model) {
		m.activeTab = plansTab
		m.openPlanForm()
		m.planForm.targets = []reconcile.ReconcileTarget{{AdapterFamily: "clockify", Instance: "clockify"}}
		m.planForm.window = planCustomWindow
		m.planForm.dateInputs[0].SetValue("2026-11-02")
		m.planForm.dateInputs[1].SetValue("2026-11-06")
		m.focusPlanDateInput()
	})
	capture("preset-delete", 100, 28, func(m *model) {
		m.activeTab = presetsTab
		m.presetDelete = &m.presets[0]
		m.overlay = deletePresetOverlay
	})
	capture("draft-discard", 100, 28, func(m *model) {
		m.openEditForm(m.worklogs[4])
		m.form.inputs[descriptionInput].SetValue("Review retry behavior and document failure recovery")
		next, _ := m.Update(specialKey(tea.KeyEsc))
		*m = next.(model)
	})
	capture("worklogs-activity-minimum", 100, 28, func(m *model) {
		m.activityOpen = true
		m.focus = focusActivity
		finished := date.Add(15 * time.Hour)
		duration := int64(1250)
		m.activities = []activity.Entry{{Source: activity.SourceTUI, Operation: "worklogs.add", State: activity.StateSucceeded, StartedAt: finished.Add(-time.Second), FinishedAt: &finished, DurationMS: &duration, Summary: "Added an Atlas activity-feed review worklog"}}
	})
	capture("status-failure", 100, 28, func(m *model) {
		m.activeTab = statusTab
		m.statusErr = "Local storage check failed. Check permissions, then refresh diagnostics."
	})
}

func preparePlacementCapture(t *testing.T, m *model, service *worklogs.Service) {
	t.Helper()
	result, err := service.PreviewAdd(m.ctx, m.workspace.cfg, automaticAddInput(m.selectedDate, m.form.placement, m.form.overtime, m.form.noLunch, m.form.inputs[issueInput].Value(), m.form.inputs[durationInput].Value(), m.form.inputs[descriptionInput].Value()))
	if err != nil {
		t.Fatal(err)
	}
	m.form.previewRecords = result.Records
	m.form.previewLoading = false
	metadata, err := service.LookupIssueMetadata(m.ctx, []string{"APP-214"})
	if err != nil {
		t.Fatal(err)
	}
	m.issueMetadata = metadata
}
