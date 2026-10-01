package worklogs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestMinuteDurationAndShiftBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input         string
		minimum, want int
		invalid       bool
	}{
		{"15m29s", 900, 900, false}, {"15m30s", 900, 960, false},
		{"14m30s", 900, 900, false}, {"14m29s", 900, 0, true},
		{"15m29s", 901, 0, true}, {"15m30s", 901, 960, false},
		{"29.999s", 0, 0, true}, {"30s", 0, 60, false},
		{"-30s", 0, 0, true}, {"15m29.999s", 900, 900, false},
	} {
		t.Run(tc.input+time.Duration(tc.minimum).String(), func(t *testing.T) {
			got, err := parseDuration(tc.input, tc.minimum)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("parseDuration(%q, %d) = %d, %v", tc.input, tc.minimum, got, err)
			}
		})
	}
	for _, tc := range []struct {
		input   string
		want    int
		invalid bool
	}{
		{"29s", 0, true}, {"-29s", 0, true}, {"30s", 60, false}, {"-30s", -60, false}, {"89s", 60, false}, {"-90s", -120, false},
	} {
		got, err := parseShiftDelta(tc.input)
		if (err != nil) != tc.invalid || got != tc.want {
			t.Fatalf("shift(%s) = %d, %v", tc.input, got, err)
		}
	}
}

func TestMinuteAddUpdateAndBatchPreview(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 900}
	input := AddInput{IssueKey: "APP-1", StartedUTC: "2026-05-01T23:59:30.001Z", Duration: "15m30s", Description: "rounded"}
	preview, err := service.PreviewAdd(ctx, cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	added, err := service.Add(ctx, cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	row := added.Records[0]
	if row.StartedAtUTC.Format(time.RFC3339) != "2026-05-02T00:00:00Z" || row.DurationSeconds != 960 || !row.StartedAtUTC.Equal(preview.Records[0].StartedAtUTC) || row.DurationSeconds != preview.Records[0].DurationSeconds {
		t.Fatalf("preview/persisted timing = %#v / %#v", preview.Records, added.Records)
	}
	duration := "16m30s"
	updated, err := service.Update(ctx, cfg, row.ID, PatchInput{Duration: &duration, ExpectedRevision: row.Revision})
	if err != nil || updated.DurationSeconds != 1020 {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	started, seconds := "2026-05-02T00:16:30Z", 931
	payload := RawApplyPayload{Adds: []RawApplyAdd{{IssueKey: "APP-2", StartedAtUTC: &started, DurationSeconds: &seconds, Description: "batch"}}}
	dry, err := service.Apply(ctx, cfg, payload, false, true)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := service.Apply(ctx, cfg, payload, false, false)
	if err != nil || !actual.Records[0].StartedAtUTC.Equal(dry.Records[0].StartedAtUTC) || actual.Records[0].DurationSeconds != dry.Records[0].DurationSeconds || actual.Records[0].DurationSeconds != 960 {
		t.Fatalf("batch = %#v, %v", actual, err)
	}
	// A raw non-overlapping start rounds into an existing interval.
	_, err = service.PreviewAdd(ctx, cfg, AddInput{IssueKey: "APP-3", StartedUTC: "2026-05-01T23:44:30Z", Duration: "15m30s", Description: "conflict"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("rounded conflict = %v", err)
	}
	forced, err := service.PreviewAdd(ctx, cfg, AddInput{IssueKey: "APP-3", StartedUTC: "2026-05-01T23:44:30Z", Duration: "15m30s", Description: "conflict", Force: true})
	if err != nil || forced.Records[0].DurationSeconds != 960 {
		t.Fatalf("explicit force = %#v, %v", forced, err)
	}
}

func seedLegacyMinuteTest(t *testing.T, service *Service, id, start string, seconds int) {
	t.Helper()
	var guard string
	if err := service.store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name='trg_worklogs_minutes_insert'`).Scan(&guard); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB().Exec(`DROP TRIGGER trg_worklogs_minutes_insert`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB().Exec(`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES(?,'APP-1',?,?,'legacy','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z')`, id, start, seconds); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB().Exec(guard); err != nil {
		t.Fatal(err)
	}
}

func TestMinuteContextAndLegacyShiftPreview(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	seedLegacyMinuteTest(t, service, "legacy", "2026-05-01T08:20:46Z", 928)
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 901, DayStart: "08:00", DayEnd: "10:00"}
	ctx := context.Background()
	result, err := service.Context(ctx, cfg, ContextInput{From: "2026-05-01", To: "2026-05-01", NoLunch: true})
	if err != nil || result.Planning.MinimumDurationSeconds != 960 || len(result.Days[0].FreeSlots) != 2 || result.Days[0].FreeSlots[1].Start.Format("15:04") != "08:37" {
		t.Fatalf("context = %#v, %v", result, err)
	}
	input := AddInput{IssueKey: "APP-2", Duration: "40m29s", Description: "fill", Fill: true, From: "2026-05-01", To: "2026-05-01", NoLunch: true}
	preview, err := service.PreviewAdd(ctx, cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := service.Add(ctx, cfg, input)
	if err != nil || len(actual.Records) != 2 || actual.Records[0].DurationSeconds+actual.Records[1].DurationSeconds != 2400 {
		t.Fatalf("automatic add = %#v, %v", actual, err)
	}
	for i, row := range actual.Records {
		if !row.StartedAtUTC.Equal(preview.Records[i].StartedAtUTC) || row.DurationSeconds != preview.Records[i].DurationSeconds {
			t.Fatal("automatic preview differs from persisted timing")
		}
	}
	filters := ListFilters{From: "2026-05-01", To: "2026-05-01", Issue: "APP-1"}
	dry, err := service.Shift(ctx, cfg, filters, "-30s", true)
	if err != nil {
		t.Fatal(err)
	}
	shifted, err := service.Shift(ctx, cfg, filters, "-30s", false)
	if err != nil || shifted.Items[0].DurationSeconds != 900 || !shifted.Items[0].StartedAtUTC.Equal(dry.Items[0].StartedAtUTC) || dry.PreviewItems[0].DurationSeconds != shifted.Items[0].DurationSeconds {
		t.Fatalf("legacy shift = %#v, %v", shifted, err)
	}
}

func TestMinuteStartFractionalBoundaries(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"2026-05-01T08:00:29.999Z", "2026-05-01T08:00:00Z"},
		{"2026-05-01T08:00:30Z", "2026-05-01T08:01:00Z"},
		{"2026-12-31T23:59:30Z", "2027-01-01T00:00:00Z"},
	} {
		got, err := parseStartedAt(config.EffectiveConfig{Location: time.UTC}, "", tc.input)
		if err != nil || got.Format(time.RFC3339) != tc.want {
			t.Fatalf("start(%s) = %s, %v", tc.input, got, err)
		}
	}
}

func TestMinuteBatchRestorationCollisionIsAtomic(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	_, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id,storage_scope,source_worklog_id,issue_key,started_at_utc,duration_seconds,description,trashed_at,reason_code,reason_detail,plan_direction) VALUES
	('one','local','one','APP-1','2026-05-21T08:00:30Z',930,'first','2026-05-22T00:00:00Z','deleted','','local'),
	('two','local','two','APP-2','2026-05-21T08:16:00Z',900,'second','2026-05-22T00:00:00Z','deleted','','local')`)
	if err != nil {
		t.Fatal(err)
	}
	filters := TrashFilters{ListFilters: ListFilters{From: "2026-05-21", To: "2026-05-21"}, StorageScope: TrashScopeLocal}
	_, err = service.RestoreTrashBatch(context.Background(), config.EffectiveConfig{Location: time.UTC}, filters, false)
	if !errors.Is(err, ErrConflict) || countActiveWorklogs(t, store) != 0 {
		t.Fatalf("batch collision = %v", err)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := service.ShowTrash(id); err != nil {
			t.Fatal("failed batch restore consumed history")
		}
	}
}

func TestMinuteSlotsAndFillPreserveRoundedTotal(t *testing.T) {
	date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	window := workdayWindow{dayStartMinutes: 8 * 60, dayEndMinutes: 10 * 60}
	active := []LocalWorklog{{StartedAtUTC: date.Add(8*time.Hour + 20*time.Minute + 46*time.Second), DurationSeconds: 15*60 + 28}}
	occupied := []localInterval{{Start: active[0].StartedAtUTC, End: active[0].StartedAtUTC.Add(time.Duration(active[0].DurationSeconds) * time.Second)}}
	slots := buildFreeSlots(date, time.UTC, window, occupied)
	if len(slots) != 2 || slots[0].DurationSeconds != 1200 || slots[1].Start.Format("15:04:05") != "08:37:00" {
		t.Fatalf("inward slots = %#v", slots)
	}
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 901}
	base := LocalWorklog{IssueKey: "APP-1", DurationSeconds: 30 * 60, Description: "fill"}
	plan := buildFillPlanAcrossDates(cfg, []time.Time{date}, window, active, base, false)
	if len(plan) != 0 {
		t.Fatalf("undersized total must be impossible: %#v", plan)
	}
	base.DurationSeconds = 40 * 60
	plan = buildFillPlanAcrossDates(cfg, []time.Time{date}, window, active, base, false)
	if len(plan) != 2 || plan[0].DurationSeconds+plan[1].DurationSeconds != base.DurationSeconds {
		t.Fatalf("fill = %#v", plan)
	}
	for _, row := range plan {
		if !HasMinutePrecision(row.StartedAtUTC, row.DurationSeconds) || row.DurationSeconds < minuteMinimum(cfg.MinimumDurationSeconds) {
			t.Fatalf("invalid fragment %#v", row)
		}
	}
	if fit := buildFitPlanForDate(cfg, date, window, active, base, false); len(fit) != 1 || fit[0].StartedAtUTC.Format("15:04") != "08:37" {
		t.Fatalf("fit = %#v", fit)
	}
}

func TestMinutePlacementAroundLunchAndMidnight(t *testing.T) {
	date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	lunchStart, lunchEnd := 12*60, 12*60+45
	window := workdayWindow{dayStartMinutes: 0, dayEndMinutes: 24 * 60, lunchStart: &lunchStart, lunchEnd: &lunchEnd}
	active := []LocalWorklog{
		{StartedAtUTC: date.Add(-15*time.Minute + 30*time.Second), DurationSeconds: 29*60 + 25},
		{StartedAtUTC: date.Add(11*time.Hour + 59*time.Minute + 30*time.Second), DurationSeconds: 46},
		{StartedAtUTC: date.Add(23*time.Hour + 44*time.Minute + 30*time.Second), DurationSeconds: 17*60 + 30},
	}
	occupied := make([]localInterval, 0, len(active))
	for _, row := range active {
		occupied = append(occupied, localInterval{Start: row.StartedAtUTC, End: row.StartedAtUTC.Add(time.Duration(row.DurationSeconds) * time.Second)})
	}
	cfg := config.EffectiveConfig{Location: time.UTC, MinimumDurationSeconds: 901}
	contextSlots := buildFreeSlots(date, time.UTC, window, occupied)
	placementSlots := buildAddPlacementFreeSlots(cfg, date, window, active, false)
	if len(contextSlots) != 2 || len(placementSlots) != 2 {
		t.Fatalf("slots=%#v / %#v", contextSlots, placementSlots)
	}
	for i, want := range []struct{ start, end string }{{"00:15", "11:59"}, {"12:45", "23:44"}} {
		if contextSlots[i] != placementSlots[i] || contextSlots[i].Start.Format("15:04") != want.start || contextSlots[i].End.Format("15:04") != want.end {
			t.Fatalf("slot=%#v / %#v", contextSlots[i], placementSlots[i])
		}
	}
	plan := buildFillPlanAcrossDates(cfg, []time.Time{date}, window, active, LocalWorklog{IssueKey: "APP-1", DurationSeconds: 12 * 3600, Description: "fill"}, false)
	if len(plan) != 2 || plan[0].DurationSeconds+plan[1].DurationSeconds != 12*3600 || plan[1].DurationSeconds != 960 || plan[1].StartedAtUTC.Format("15:04") != "12:45" {
		t.Fatalf("fill=%#v", plan)
	}
}

func TestMinuteRestorePreviewAndCollision(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	_, err := store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES ('rounded','local','original','APP-1','2026-05-21T09:00:30Z',931,'legacy','2026-05-22T09:00:00Z','deleted','','local')`)
	if err != nil {
		t.Fatal(err)
	}
	filters := TrashFilters{ListFilters: ListFilters{From: "2026-05-21", To: "2026-05-21"}, StorageScope: TrashScopeLocal}
	dry, err := service.RestoreTrashBatch(ctx, cfg, filters, true)
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.RestoreTrash(ctx, cfg, "rounded")
	if err != nil || len(dry.Items) != 1 || item.Record.DurationSeconds != 960 || !item.Record.StartedAtUTC.Equal(dry.Items[0].Record.StartedAtUTC) {
		t.Fatalf("restore = %#v, %v", item, err)
	}
	_, err = store.DB().Exec(`INSERT INTO trashed_worklogs(id, storage_scope, source_worklog_id, issue_key, started_at_utc, duration_seconds, description, trashed_at, reason_code, reason_detail, plan_direction) VALUES ('conflict','local','another','APP-2','2026-05-21T08:45:30Z',930,'legacy','2026-05-22T09:00:00Z','deleted','','local')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RestoreTrash(ctx, cfg, "conflict"); !errors.Is(err, ErrConflict) {
		t.Fatalf("collision = %v", err)
	}
	if _, err := service.ShowTrash("conflict"); err != nil {
		t.Fatal("failed restore consumed audit row")
	}
}
