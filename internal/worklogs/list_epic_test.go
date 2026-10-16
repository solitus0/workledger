package worklogs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestListEpicAcrossDatesAndSelectors(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	ctx := context.Background()
	cfg := config.EffectiveConfig{Location: time.UTC}
	epic := "October_%'"

	add := func(key, started string) LocalWorklog {
		return mustAddWorklog(t, service, cfg, AddInput{IssueKey: key, StartedUTC: started, Duration: "1h", Description: key})
	}
	late := add("APP-1", "2027-01-01T09:00:00Z")
	early := add("APP-1", "2025-01-01T09:00:00Z")
	neighbor := add("APP2-1", "2026-05-21T09:00:00Z")
	otherProject := add("OPS-1", "2026-06-01T09:00:00Z")
	add("APP-3", "2026-05-21T10:00:00Z")
	add("APP-4", "2026-05-21T11:00:00Z")
	add("APP-5", "2026-05-21T12:00:00Z")
	trashed := add("APP-1", "2024-01-01T09:00:00Z")
	if _, err := service.Delete(ctx, trashed.ID, trashed.Revision); err != nil {
		t.Fatal(err)
	}
	otherEpic, lowerEpic, unusedEpic := "October-ab'", "october_%'", "Unused"
	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{
		{IssueKey: "APP-1", Epic: &epic, EpicSet: true},
		{IssueKey: "APP2-1", Epic: &epic, EpicSet: true},
		{IssueKey: "OPS-1", Epic: &epic, EpicSet: true},
		{IssueKey: "APP-3", Epic: &otherEpic, EpicSet: true},
		{IssueKey: "APP-4", Epic: &lowerEpic, EpicSet: true},
		{IssueKey: "APP-6", Epic: &unusedEpic, EpicSet: true},
	}, false); err != nil {
		t.Fatal(err)
	}

	paddedEpic, missingEpic := "  "+epic+"  ", "October"
	for _, tc := range []struct {
		name    string
		filters ListFilters
		want    []string
	}{
		{name: "all dates and exact literal membership", filters: ListFilters{Epic: &paddedEpic}, want: []string{early.ID, neighbor.ID, otherProject.ID, late.ID}},
		{name: "date window", filters: ListFilters{Epic: &epic, From: "2026-05-01", To: "2026-05-31"}, want: []string{neighbor.ID}},
		{name: "issue", filters: ListFilters{Epic: &epic, Issue: "APP-1"}, want: []string{early.ID, late.ID}},
		{name: "project boundary", filters: ListFilters{Epic: &epic, IssuePrefix: "APP"}, want: []string{early.ID, late.ID}},
		{name: "combined selectors", filters: ListFilters{Epic: &epic, IssuePrefix: "APP2", From: "2026-05-21", To: "2026-05-21"}, want: []string{neighbor.ID}},
		{name: "issue outside epic", filters: ListFilters{Epic: &epic, Issue: "APP-3"}},
		{name: "unknown epic", filters: ListFilters{Epic: &missingEpic}},
		{name: "epic without worklogs", filters: ListFilters{Epic: &unusedEpic}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, effective, err := service.List(ctx, cfg, tc.filters)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(items))
			for i, item := range items {
				ids[i] = item.ID
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("worklogs = %v, want %v", ids, tc.want)
			}
			if effective.Epic == nil || *effective.Epic != strings.TrimSpace(*tc.filters.Epic) {
				t.Fatalf("effective epic = %v", effective.Epic)
			}
			if tc.filters.From == "" && (effective.From != nil || effective.To != nil) {
				t.Fatalf("all-date read has bounds: %#v", effective)
			}
		})
	}

	if err := service.ApplyIssueMetadata(ctx, []IssueMetadataPatch{{IssueKey: "APP-1", EpicSet: true}}, false); err != nil {
		t.Fatal(err)
	}
	items, _, err := service.List(ctx, cfg, ListFilters{Epic: &epic})
	if err != nil || len(items) != 2 || items[0].ID != neighbor.ID || items[1].ID != otherProject.ID {
		t.Fatalf("list did not follow current membership: %#v, %v", items, err)
	}
}

func TestListEpicValidatesNameAndDateSelectors(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC}
	for _, epic := range []string{"", "  ", "Two\nlines", "Two\twords", "Two\u2028lines", "Two\u2029lines"} {
		for _, withDate := range []bool{false, true} {
			_, _, err := service.List(context.Background(), cfg, ListFilters{Epic: &epic, Today: withDate})
			var validation ValidationError
			if !errors.As(err, &validation) || len(validation.Issues) != 1 || validation.Issues[0].Field != "epic" {
				t.Errorf("epic %q with date=%t: expected epic validation, got %v", epic, withDate, err)
			}
		}
	}
	epic := "October"
	_, _, err := service.List(context.Background(), cfg, ListFilters{Epic: &epic, Today: true, CurrentMonth: true})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("epic bypassed conflicting date validation: %v", err)
	}
}
