package cli

import (
	"strings"
	"testing"
)

func TestWorklogsListEpicCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	for _, args := range [][]string{
		{"init"},
		{"worklogs", "add", "--issue", "SBAH-1", "--started", "2026-09-01T09:00", "--duration", "1h", "--description", "September work"},
		{"worklogs", "add", "--issue", "SBAH-2", "--started", "2026-10-01T09:00", "--duration", "2h", "--description", "October work"},
		{"worklogs", "add", "--issue", "OTHER-1", "--started", "2026-10-01T11:00", "--duration", "1h", "--description", "Outside epic"},
		{"issues", "set", "--issue", "SBAH-1", "--epic", "SBA-october-work"},
		{"issues", "set", "--issue", "SBAH-2", "--epic", "SBA-october-work"},
	} {
		if result := runCLI(t, args...); result.code != 0 {
			t.Fatal(result)
		}
	}

	result := runCLI(t, "worklogs", "list", "--epic", "  SBA-october-work  ", "--fields", "issue_key,description", "--output", "json")
	if result.code != 0 {
		t.Fatal(result)
	}
	payload := decodeJSONMap(t, []byte(result.stdout))
	if payload["total"] != float64(2) {
		t.Fatalf("all-date epic list: %s", result.stdout)
	}
	filters := payload["filters"].(map[string]any)
	raw, effective := filters["raw"].(map[string]any), filters["effective"].(map[string]any)
	if raw["epic"] != "  SBA-october-work  " || effective["epic"] != "SBA-october-work" {
		t.Fatalf("epic filter metadata: %#v", filters)
	}
	if _, ok := effective["from"]; ok {
		t.Fatalf("all-date epic read has a from bound: %#v", effective)
	}
	if _, ok := effective["to"]; ok {
		t.Fatalf("all-date epic read has a to bound: %#v", effective)
	}
	items := payload["items"].([]any)
	for i, want := range []string{"SBAH-1", "SBAH-2"} {
		item := items[i].(map[string]any)
		if len(item) != 2 || item["issue_key"] != want || item["description"] == nil {
			t.Fatalf("field selection or sorting: %#v", item)
		}
	}

	for _, tc := range []struct {
		name string
		args []string
		want float64
	}{
		{name: "date window", args: []string{"--from", "2026-10-01", "--to", "2026-10-31"}, want: 1},
		{name: "issue", args: []string{"--issue", "SBAH-1"}, want: 1},
		{name: "prefix", args: []string{"--issue-prefix", "SBAH"}, want: 2},
		{name: "nonmember", args: []string{"--issue", "OTHER-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"worklogs", "list", "--epic", "SBA-october-work", "--output", "json"}, tc.args...)
			result := runCLI(t, args...)
			if result.code != 0 || decodeJSONMap(t, []byte(result.stdout))["total"] != tc.want {
				t.Fatal(result)
			}
		})
	}

	table := runCLI(t, "worklogs", "list", "--epic", "SBA-october-work")
	for _, want := range []string{"September work", "October work", "3h", "2 worklogs"} {
		if table.code != 0 || !strings.Contains(table.stdout, want) {
			t.Fatalf("table missing %q: %#v", want, table)
		}
	}
	unknown := runCLI(t, "worklogs", "list", "--epic", "Missing epic", "--output", "json")
	if unknown.code != 0 || decodeJSONMap(t, []byte(unknown.stdout))["total"] != float64(0) {
		t.Fatalf("unknown epic: %#v", unknown)
	}
	completion := runCompletion(t, "worklogs", "list", "--epic", "SBA-")
	assertCompletionContains(t, completion, "SBA-october-work")
}

func TestWorklogsListRejectsInvalidEpicCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigWithUTC(t)
	if result := runCLI(t, "init"); result.code != 0 {
		t.Fatal(result)
	}
	for _, epic := range []string{"", "  ", "Two\nlines"} {
		result := runCLI(t, "worklogs", "list", "--epic", epic, "--output", "json")
		if result.code != 2 || !strings.Contains(result.stdout, "epic") {
			t.Fatalf("invalid epic %q: %#v", epic, result)
		}
	}
}
