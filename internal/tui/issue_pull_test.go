package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/issues"
	"github.com/solitus0/workledger/internal/worklogs"
)

type testIssuePuller func(context.Context, config.EffectiveConfig, []string) issues.PullResult

func (f testIssuePuller) Pull(ctx context.Context, cfg config.EffectiveConfig, keys []string) issues.PullResult {
	return f(ctx, cfg, keys)
}

func TestIssuePullFrozenHiddenBatchAndRetry(t *testing.T) {
	m, service := issueFixture(t)
	var calls []string
	fail := true
	m.workspace.issuePuller = testIssuePuller(func(ctx context.Context, _ config.EffectiveConfig, keys []string) issues.PullResult {
		calls = append(calls, keys...)
		key := keys[0]
		if fail && key == "SBAH-2" {
			return issues.PullResult{Failures: []issues.PullFailure{{IssueKey: key, Err: errors.New("Jira unavailable")}}}
		}
		estimate := int64(7200)
		item := worklogs.IssueMetadata{IssueKey: key, Title: "Jira title", MaxEstimateSeconds: &estimate, SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main", RefreshedAt: time.Now()}
		if err := service.UpsertIssueMetadataBatch(ctx, []worklogs.IssueMetadata{item}); err != nil {
			t.Fatal(err)
		}
		return issues.PullResult{Items: []worklogs.IssueMetadata{item}}
	})
	m.issueMarks = map[string]bool{"SBAH-1": true, "SBAH-2": true}
	m = sendIssueKey(m, "/")
	m.issueSearch.input.SetValue("SBAH-2")
	m = sendIssueKey(m, "enter")
	if m.hiddenIssueMarks() != 1 {
		t.Fatal("fixture did not hide selected issue")
	}
	before, _ := service.ShowIssueMetadata(t.Context(), "SBAH-2")
	next, cmd := m.Update(actionKey("R"))
	m = next.(model)
	if cmd == nil || m.issuePull == nil || !strings.Contains(m.issuePullLabel(), "0/2") {
		t.Fatal("pull not started")
	}
	duplicate, duplicateCmd := m.Update(actionKey("R"))
	m = duplicate.(model)
	if duplicateCmd != nil {
		t.Fatal("duplicate pull started")
	}
	// Changing the view and marks never changes the submitted scope.
	m.issueSearch = nil
	m.issueMarks["SBAH-11"] = true
	next, cmd = m.Update(cmd())
	m = next.(model)
	if cmd == nil || m.issueMarks["SBAH-1"] || m.issuePull.completed != 1 {
		t.Fatal("success not cleared or progress not delivered")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.issuePull != nil || !m.issueMarks["SBAH-2"] || !m.issueMarks["SBAH-11"] || !strings.Contains(m.notice, "1 updated · 1 failed") {
		t.Fatalf("retry marks lost: %v %s", m.issueMarks, m.notice)
	}
	if !reflect.DeepEqual(calls, []string{"SBAH-1", "SBAH-2"}) {
		t.Fatalf("batch changed: %v", calls)
	}
	next, _ = m.Update(m.issueListCmd(m.issuesGen)())
	m = next.(model)
	after, _ := service.ShowIssueMetadata(t.Context(), "SBAH-2")
	if !reflect.DeepEqual(before, after) || m.selectedIssueKey() != "SBAH-2" {
		t.Fatalf("failed issue or focus changed: %+v %+v", before, after)
	}
	fail = false
	delete(m.issueMarks, "SBAH-11")
	next, cmd = m.Update(actionKey("R"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("retry unavailable")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(m.issueMarks) != 0 || len(calls) != 3 || calls[2] != "SBAH-2" {
		t.Fatalf("wrong retry: %v %v", calls, m.issueMarks)
	}
}

func TestIssuePullFocusedFallbackCancellationAndStaleResult(t *testing.T) {
	m, _ := issueFixture(t)
	var calls []string
	m.workspace.issuePuller = testIssuePuller(func(ctx context.Context, _ config.EffectiveConfig, keys []string) issues.PullResult {
		calls = append(calls, keys...)
		if err := ctx.Err(); err != nil {
			return issues.PullResult{Failures: []issues.PullFailure{{IssueKey: keys[0], Err: err}}}
		}
		return issues.PullResult{Items: []worklogs.IssueMetadata{{IssueKey: keys[0]}}}
	})
	next, cmd := m.Update(actionKey("R"))
	m = next.(model)
	if cmd == nil || !reflect.DeepEqual(m.issuePull.keys, []string{"SBAH-2"}) {
		t.Fatal("focused fallback missing")
	}
	m = sendIssueKey(m, "esc")
	next, _ = m.Update(cmd())
	m = next.(model)
	if !m.issueMarks["SBAH-2"] || m.issuePull != nil {
		t.Fatal("cancellation lost focused retry")
	}
	// A canceled batch keeps all unrequested keys marked and never starts them.
	m.issuesLoading = false
	m.issueMarks = map[string]bool{"SBAH-1": true, "SBAH-2": true, "SBAH-11": true}
	next, cmd = m.Update(actionKey("R"))
	m = next.(model)
	m = sendIssueKey(m, "esc")
	next, _ = m.Update(cmd())
	m = next.(model)
	if len(calls) != 2 || len(m.issueMarks) != 3 || !strings.Contains(m.notice, "2 pending") {
		t.Fatalf("cancellation: %v %v %s", calls, m.issueMarks, m.notice)
	}
	next, _ = m.Update(issuePullResultMsg{generation: m.issuePullGen - 1, result: issues.PullResult{Items: []worklogs.IssueMetadata{{IssueKey: "SBAH-2"}}}})
	m = next.(model)
	if !m.issueMarks["SBAH-2"] {
		t.Fatal("stale result cleared retry")
	}
}

func TestIssueEditDoesNotPromoteJiraTitleToManualOverride(t *testing.T) {
	m, service := issueFixture(t)
	if err := service.UpsertIssueMetadataBatch(t.Context(), []worklogs.IssueMetadata{{IssueKey: "SBAH-11", Title: "Remote title", SourceAdapterFamily: "jira-cloud", SourceAdapterInst: "main", RefreshedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(m.issueListCmd(m.issuesGen)())
	m = next.(model)
	m.restoreIssueSelection("SBAH-11")
	if m.selectedIssue().Title != "Remote title" {
		t.Fatal("remote title not displayed")
	}
	m = sendIssueKey(m, "e")
	if m.issueForm.inputs[1].Value() != "" {
		t.Fatal("edit promoted cached title")
	}
	next, cmd := m.Update(actionKey("ctrl+s"))
	m = next.(model)
	if cmd == nil {
		t.Fatal("save unavailable")
	}
	next, _ = m.Update(cmd())
	item, err := service.ShowIssueMetadata(t.Context(), "SBAH-11")
	if err != nil || item.Title != "Remote title" || item.ManualTitle != "" {
		t.Fatalf("save changed title ownership: %+v %v", item, err)
	}
}
