package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLIActivityPersistsSilentlyAndRedactsFreeText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	initialized := runCLI(t, "init", "--output", "json")
	if initialized.code != 0 {
		t.Fatalf("init failed: %+v", initialized)
	}
	added := runCLI(t, "worklogs", "add", "--issue", "APP-123", "--started", "2026-09-06T10:00", "--duration", "1h", "--description", "private customer detail", "--output", "json")
	if added.code != 0 || added.stderr != "" {
		t.Fatalf("add failed or logged to stderr: %+v", added)
	}
	listed := runCLI(t, "activity", "list", "--source", "cli", "--output", "json")
	if listed.code != 0 || listed.stderr != "" {
		t.Fatalf("activity list failed: %+v", listed)
	}
	if strings.Contains(listed.stdout, "private customer detail") || strings.Contains(listed.stdout, "--description") {
		t.Fatalf("activity leaked free text: %s", listed.stdout)
	}
	var payload struct {
		Items []struct {
			Operation  string            `json:"operation"`
			State      string            `json:"state"`
			Attributes map[string]string `json:"attributes"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &payload); err != nil {
		t.Fatal(err)
	}
	foundInit, foundAdd := false, false
	for _, item := range payload.Items {
		switch item.Operation {
		case "init":
			foundInit = item.State == "succeeded"
		case "worklogs.add":
			foundAdd = item.State == "succeeded" && item.Attributes["issue"] == "APP-123" && item.Attributes["duration"] == "1h"
		}
	}
	if !foundInit || !foundAdd {
		t.Fatalf("missing expected activities: %+v", payload.Items)
	}
}

func TestCLIActivityRecordsValidationFailureAndListExcludesItself(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init: %+v", result)
	}
	failed := runCLI(t, "activity", "list", "--limit", "0", "--output", "json")
	if failed.code != 2 {
		t.Fatalf("invalid list code=%d output=%s", failed.code, failed.stdout)
	}
	listed := runCLI(t, "activity", "list", "--output", "json")
	if listed.code != 0 {
		t.Fatalf("list: %+v", listed)
	}
	if strings.Contains(listed.stdout, `"state": "running"`) {
		t.Fatalf("list exposed its own running entry: %s", listed.stdout)
	}
	if !strings.Contains(listed.stdout, `"operation": "activity.list"`) || !strings.Contains(listed.stdout, `"state": "failed"`) {
		t.Fatalf("validation failure missing: %s", listed.stdout)
	}
}

func TestCLIActivityRecordsCreatedWithinSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if result := runCLI(t, "init", "--output", "json"); result.code != 0 {
		t.Fatalf("init: %+v", result)
	}
	deleted := runCLI(t, "worklogs", "delete", "--created-within", "15m", "--dry", "--output", "json")
	if deleted.code != 0 {
		t.Fatalf("delete preview: %+v", deleted)
	}
	listed := runCLI(t, "activity", "list", "--source", "cli", "--output", "json")
	if listed.code != 0 {
		t.Fatalf("activity list: %+v", listed)
	}
	var payload struct {
		Items []struct {
			Operation  string            `json:"operation"`
			Attributes map[string]string `json:"attributes"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &payload); err != nil {
		t.Fatal(err)
	}
	for _, item := range payload.Items {
		if item.Operation == "worklogs.delete" && item.Attributes["created-within"] == "15m" && item.Attributes["dry"] == "true" {
			return
		}
	}
	t.Fatalf("created-within activity missing: %+v", payload.Items)
}

func TestCLIActivityUnavailableStoreDoesNotChangeVersionOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	result := runCLI(t, "version", "--output", "json")
	if result.code != 0 || result.stderr != "" || !strings.Contains(result.stdout, `"version"`) {
		t.Fatalf("version changed without store: %+v", result)
	}
}
