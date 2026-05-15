package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTTYReporterFinishEndsLine(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := &ttyReporter{
		w:         buf,
		now:       fixedTTYNow(),
		startedAt: fixedTTYNow()().Add(-3 * time.Second),
	}

	reporter.Start(Event{Phase: "discovering", ScopeDone: 1, ScopeTotal: 5, Message: "totals collection"})
	reporter.Finish(Event{Phase: "finalizing", ScopeDone: 5, ScopeTotal: 5, Failed: 1, Message: "done"})

	output := buf.String()
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("expected trailing newline, got %q", output)
	}
	if !strings.Contains(output, "\r") {
		t.Fatalf("expected carriage return rendering, got %q", output)
	}
	if strings.Contains(output, "[progress]") {
		t.Fatalf("did not expect plain progress format, got %q", output)
	}
}

func TestTTYReporterRendersBarWhenTotalsKnown(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	reporter := &ttyReporter{
		w:         buf,
		now: func() time.Time {
			return now
		},
		startedAt: now.Add(-(4 * time.Second) - (250 * time.Millisecond)),
	}

	reporter.Event(Event{Phase: "fetching", ScopeDone: 3, ScopeTotal: 8, Failed: 1, Message: "fetching totals target"})

	output := buf.String()
	if !strings.Contains(output, "[###.......] 3/8") {
		t.Fatalf("expected progress bar, got %q", output)
	}
	if !strings.Contains(output, "failed=1") {
		t.Fatalf("expected failed count, got %q", output)
	}
	if !strings.Contains(output, "elapsed=4.25s") {
		t.Fatalf("expected elapsed time, got %q", output)
	}
}

func TestTTYReporterOmitsBarWhenTotalsUnknown(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := &ttyReporter{
		w:         buf,
		now:       fixedTTYNow(),
		startedAt: fixedTTYNow()().Add(-2 * time.Second),
	}

	reporter.Event(Event{Phase: "fetching", ScopeDone: 3, Message: "fetched jira issues"})

	output := buf.String()
	if !strings.Contains(output, "scopes=3") {
		t.Fatalf("expected count-only progress, got %q", output)
	}
	if strings.Contains(output, "[") {
		t.Fatalf("did not expect bar output, got %q", output)
	}
}

func fixedTTYNow() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	}
}
