package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/solitus0/workledger/internal/presets"
)

func TestPresetTabAndPickerPopulateEditableManualAdd(t *testing.T) {
	ws, deps := testWorkspace(t)
	presetService := ws.presets.(*fakePresets)
	presetService.items = []presets.Preset{{
		ID: "preset-1", Name: "daily-standup", IssueKey: "APP-1", StartTime: "09:15", DurationSeconds: 900,
		Description: "Daily standup", Revision: 3, CreatedAt: deps.now(), UpdatedAt: deps.now(),
	}}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.presets = append([]presets.Preset(nil), presetService.items...)

	next, _ := m.Update(textKey("4"))
	m = next.(model)
	if m.activeTab != presetsTab || !strings.Contains(m.render(), "daily-standup") {
		t.Fatalf("preset tab did not render selected preset: %s", m.render())
	}
	next, _ = m.Update(textKey("2"))
	m = next.(model)
	next, _ = m.Update(textKey("a"))
	m = next.(model)
	if m.form == nil || m.presetPicker != nil || m.form.sourcePresetID != "" {
		t.Fatal("lowercase add did not open a blank worklog form directly")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	next, _ = m.Update(textKey("A"))
	m = next.(model)
	if m.presetPicker == nil || strings.Contains(m.render(), "Blank worklog") {
		t.Fatal("uppercase add did not open the preset-only picker")
	}
	next, cmd := m.Update(specialKey(tea.KeyEnter))
	m = next.(model)
	if m.presetPicker != nil || m.form == nil || m.form.placement != manualPlacement || m.form.sourcePresetID != "preset-1" {
		t.Fatalf("preset selection did not open manual add: %+v", m.form)
	}
	if m.form.inputs[issueInput].Value() != "APP-1" || m.form.inputs[startInput].Value() != "09:15" || m.form.inputs[descriptionInput].Value() != "Daily standup" {
		t.Fatalf("preset fields not populated: %+v", m.form.inputs)
	}
	if cmd == nil {
		t.Fatal("preset-backed form did not schedule suggestions and preview")
	}
}

func TestPresetPickerShowsShortcutsOnlyInActionBar(t *testing.T) {
	ws, deps := testWorkspace(t)
	deps.noColor = true
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.presets = []presets.Preset{
		{Name: "daily-standup", IssueKey: "APP-1", StartTime: "09:15", DurationSeconds: 900},
		{Name: "weekly-review", IssueKey: "APP-2", StartTime: "15:00", DurationSeconds: 1800},
	}
	m.openPresetPicker()

	body := m.renderPresetPicker(m.width, m.height)
	if m.presetPicker.searching || strings.Contains(body, "Find preset") || strings.Contains(body, "Type preset name") {
		t.Fatalf("preset picker should open in browse mode:\n%s", body)
	}
	for _, hint := range []string{"Ctrl+F Find", "↑/↓ Select", "Enter Choose", "Esc Cancel"} {
		if strings.Contains(body, hint) {
			t.Fatalf("preset picker body duplicates action-bar hint %q:\n%s", hint, body)
		}
	}
	rendered := m.render()
	for _, shortcut := range []string{"Ctrl+F Find", "↑/↓ Select", "Enter Choose", "Esc Cancel"} {
		if strings.Count(rendered, shortcut) != 1 {
			t.Fatalf("shortcut %q should appear once in the action bar:\n%s", shortcut, rendered)
		}
	}

	next, _ := m.Update(textKey("d"))
	m = next.(model)
	if len(m.presetPicker.items) != 2 {
		t.Fatal("typing in browse mode unexpectedly filtered presets")
	}
	next, _ = m.Update(ctrlKey('f'))
	m = next.(model)
	if !m.presetPicker.searching || !strings.Contains(m.renderPresetPicker(m.width, m.height), "Find preset") {
		t.Fatal("ctrl+f did not reveal the preset search field")
	}
	next, _ = m.Update(textKey("d"))
	m = next.(model)
	if len(m.presetPicker.items) != 1 || m.presetPicker.items[0].Name != "daily-standup" {
		t.Fatalf("find mode did not filter presets: %#v", m.presetPicker.items)
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	if m.presetPicker == nil || m.presetPicker.searching || len(m.presetPicker.items) != 2 || m.presetPicker.input.Value() != "" {
		t.Fatal("escape did not return the preset picker to unfiltered browse mode")
	}
}

func TestWeekAddShortcutsAndEmptyPresetNotice(t *testing.T) {
	ws, deps := testWorkspace(t)
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 140, 32
	m.worklogView = weekWorklogView

	next, _ := m.Update(textKey("a"))
	m = next.(model)
	if m.form == nil || m.presetPicker != nil {
		t.Fatal("week lowercase add did not open a blank worklog form directly")
	}
	next, _ = m.Update(specialKey(tea.KeyEsc))
	m = next.(model)
	next, cmd := m.Update(textKey("A"))
	m = next.(model)
	if cmd != nil || m.form != nil || m.presetPicker != nil || m.notice != "no presets available; create one in Presets" {
		t.Fatalf("empty uppercase preset add = form:%v picker:%v notice:%q cmd:%v", m.form != nil, m.presetPicker != nil, m.notice, cmd)
	}
	if footer := m.renderFooter(m.width); !strings.Contains(footer, "a Add") || !strings.Contains(footer, "A Preset") {
		t.Fatalf("worklogs footer does not expose both add shortcuts: %q", footer)
	}
	m.overlay = helpOverlay
	help, _ := m.overlayContent()
	if strings.Contains(help.body, "a add") || !strings.Contains(help.body, "action bar is the authoritative shortcut reference") {
		t.Fatalf("help should defer shortcut definitions to the action bar: %q", help.body)
	}
}

func TestPresetTabCreateAndConfirmedDelete(t *testing.T) {
	ws, deps := testWorkspace(t)
	presetService := ws.presets.(*fakePresets)
	item := presets.Preset{ID: "preset-1", Name: "daily", IssueKey: "APP-1", StartTime: "09:00", DurationSeconds: 900, Description: "Daily", Revision: 2}
	m := newModel(context.Background(), deps, ws, nil)
	m.width, m.height = 120, 32
	m.activeTab = presetsTab
	m.presets = []presets.Preset{item}

	next, _ := m.Update(textKey("a"))
	m = next.(model)
	if m.presetForm == nil || m.presetForm.editing {
		t.Fatal("preset add form did not open")
	}
	m.presetForm.inputs[0].SetValue("planning")
	m.presetForm.inputs[1].SetValue("APP-2")
	m.presetForm.inputs[2].SetValue("10:00")
	m.presetForm.inputs[3].SetValue("30m")
	m.presetForm.inputs[4].SetValue("Planning")
	next, cmd := m.Update(ctrlKey('s'))
	m = next.(model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(model)
	if len(presetService.created) != 1 || presetService.created[0].Name != "planning" {
		t.Fatalf("preset create input = %+v", presetService.created)
	}

	m.presets = []presets.Preset{item}
	m.presetSelected = 0
	next, _ = m.Update(textKey("D"))
	m = next.(model)
	if m.overlay != deletePresetOverlay {
		t.Fatal("preset delete confirmation did not open")
	}
	next, cmd = m.Update(textKey("y"))
	m = next.(model)
	result = cmd()
	next, _ = m.Update(result)
	if len(presetService.deleted) != 1 || presetService.deleted[0] != "daily" {
		t.Fatalf("preset deletes = %+v", presetService.deleted)
	}
}
