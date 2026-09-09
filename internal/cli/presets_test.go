package cli

import (
	"strings"
	"testing"
)

func TestPresetCLIWorkflowAndCompletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init failed: %+v", result)
	}

	created := runCLI(t, "presets", "add", "daily-standup", "--issue", "APP-1", "--start", "09:00", "--duration", "15m", "--description", "Daily standup", "--output", "json")
	if created.code != 0 {
		t.Fatalf("create failed: %+v", created)
	}
	payload := decodeJSONMap(t, []byte(created.stdout))
	if payload["name"] != "daily-standup" || payload["duration_seconds"] != float64(900) {
		t.Fatalf("unexpected create payload: %#v", payload)
	}
	if duplicate := runCLI(t, "presets", "add", "daily-standup", "--issue", "APP-1", "--start", "09:00", "--duration", "15m", "--description", "Duplicate"); duplicate.code != 2 {
		t.Fatalf("duplicate code=%d stdout=%s stderr=%s", duplicate.code, duplicate.stdout, duplicate.stderr)
	}

	completion := runCompletion(t, "presets", "apply", "daily-")
	assertCompletionContains(t, completion, "daily-standup")

	dry := runCLI(t, "presets", "apply", "daily-standup", "--date", "tomorrow", "--description", "Standup override", "--dry", "--output", "json")
	if dry.code != 0 || !strings.Contains(dry.stdout, `"dry_run": true`) || !strings.Contains(dry.stdout, "Standup override") {
		t.Fatalf("dry apply failed: %+v", dry)
	}
	shown := runCLI(t, "presets", "show", "daily-standup", "--output", "json")
	if shown.code != 0 || !strings.Contains(shown.stdout, `"last_used_at": null`) {
		t.Fatalf("dry apply changed usage: %+v", shown)
	}

	applied := runCLI(t, "presets", "apply", "daily-standup", "--date", "2026-09-07", "--output", "json")
	if applied.code != 0 || !strings.Contains(applied.stdout, `"started_at": "2026-09-07T09:00:00Z"`) {
		t.Fatalf("apply failed: %+v", applied)
	}
	updated := runCLI(t, "presets", "update", "daily-standup", "--name", "team-standup", "--duration", "30m", "--output", "json")
	if updated.code != 0 || !strings.Contains(updated.stdout, `"name": "team-standup"`) || !strings.Contains(updated.stdout, `"duration_seconds": 1800`) {
		t.Fatalf("update failed: %+v", updated)
	}
	if old := runCLI(t, "presets", "show", "daily-standup"); old.code != 3 {
		t.Fatalf("old name code=%d", old.code)
	}
	listed := runCLI(t, "presets", "list", "--output", "json")
	if listed.code != 0 || !strings.Contains(listed.stdout, "team-standup") {
		t.Fatalf("list failed: %+v", listed)
	}
	deleted := runCLI(t, "presets", "delete", "team-standup", "--output", "json")
	if deleted.code != 0 || !strings.Contains(deleted.stdout, "team-standup") {
		t.Fatalf("delete failed: %+v", deleted)
	}
}

func TestPresetCLIValidatesDateSlugAndUpdateFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init"); result.code != 0 {
		t.Fatalf("init failed: %+v", result)
	}
	invalid := runCLI(t, "presets", "add", "Daily Standup", "--issue", "APP-1", "--start", "09:00", "--duration", "15m", "--description", "Daily")
	if invalid.code != 2 {
		t.Fatalf("invalid slug code=%d stderr=%s", invalid.code, invalid.stderr)
	}
	if result := runCLI(t, "presets", "add", "daily", "--issue", "APP-1", "--start", "09:00", "--duration", "15m", "--description", "Daily"); result.code != 0 {
		t.Fatalf("seed failed: %+v", result)
	}
	if result := runCLI(t, "presets", "update", "daily"); result.code != 2 {
		t.Fatalf("empty update code=%d stderr=%s", result.code, result.stderr)
	}
	if result := runCLI(t, "presets", "apply", "daily", "--date", "nonday"); result.code != 2 {
		t.Fatalf("invalid date code=%d stderr=%s", result.code, result.stderr)
	}
}
