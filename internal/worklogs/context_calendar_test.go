package worklogs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

func TestContextSlicesUseLocalMidnightsAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Vilnius")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		month time.Month
		day   int
		hours int
	}{
		{time.March, 29, 23}, {time.October, 25, 25},
	} {
		t.Run(tc.month.String(), func(t *testing.T) {
			store, service := newTestService(t)
			defer store.Close()
			cfg := config.EffectiveConfig{Location: location, DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800}
			start := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, location)
			end := start.AddDate(0, 0, 1)
			mustAddWorklog(t, service, cfg, AddInput{
				IssueKey: "APP-1", StartedUTC: start.UTC().Format(time.RFC3339), Duration: fmt.Sprintf("%dh", tc.hours), Description: "DST day",
			})
			mustAddWorklog(t, service, cfg, AddInput{
				IssueKey: "APP-2", StartedUTC: end.UTC().Format(time.RFC3339), Duration: "1h", Description: "Following day",
			})
			result, err := service.Context(context.Background(), cfg, ContextInput{From: start.Format("2006-01-02"), To: end.Format("2006-01-02"), NoLunch: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Days) != 2 || result.Days[0].BookedSeconds != tc.hours*3600 || result.Days[1].BookedSeconds != 3600 || result.Summary.BookedSeconds != (tc.hours+1)*3600 {
				t.Fatalf("DST context omitted or double-counted occupancy: %#v", result)
			}
		})
	}
}

func TestContextMonthTotalClipsCarryInAndCarryOut(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()
	cfg := config.EffectiveConfig{Location: time.UTC, DayStart: "08:00", DayEnd: "17:00", DailyMinimumQuotaSeconds: 28800}
	for _, start := range []string{"2026-04-30T23:30:00Z", "2026-05-31T23:30:00Z"} {
		mustAddWorklog(t, service, cfg, AddInput{IssueKey: "APP-1", StartedUTC: start, Duration: "2h", Description: "Month boundary"})
	}
	result, err := service.Context(context.Background(), cfg, ContextInput{From: "2026-05-01", To: "2026-05-31", NoLunch: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Days) != 31 || result.Summary.BookedSeconds != 2*3600 || result.Summary.WorklogCount != 1 || result.Days[0].BookedSeconds != 90*60 || result.Days[30].BookedSeconds != 30*60 {
		t.Fatalf("month context did not clip boundary entries: %#v", result)
	}
	if len(result.Days[0].Worklogs) != 0 || len(result.Days[30].Worklogs) != 1 {
		t.Fatal("boundary occupancy changed start-date worklog membership")
	}
}
