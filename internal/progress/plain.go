package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type plainReporter struct {
	w  io.Writer
	mu sync.Mutex
}

func NewPlainReporter(w io.Writer) Reporter {
	if w == nil {
		return NewNoopReporter()
	}
	return &plainReporter{w: w}
}

func (r *plainReporter) Start(event Event) {
	r.write("start", event)
}

func (r *plainReporter) Event(event Event) {
	r.write("event", event)
}

func (r *plainReporter) Finish(event Event) {
	r.write("finish", event)
}

func (r *plainReporter) write(kind string, event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	parts := []string{"[progress]", kind}
	if event.Phase != "" {
		parts = append(parts, "phase="+event.Phase)
	}
	if event.ScopeTotal > 0 || event.ScopeDone > 0 {
		parts = append(parts, fmt.Sprintf("scopes=%d/%d", event.ScopeDone, event.ScopeTotal))
	}
	if event.WorkTotal > 0 || event.WorkDone > 0 {
		parts = append(parts, fmt.Sprintf("worklogs=%d/%d", event.WorkDone, event.WorkTotal))
	}
	if event.Active > 0 {
		parts = append(parts, fmt.Sprintf("active=%d", event.Active))
	}
	if event.Failed > 0 {
		parts = append(parts, fmt.Sprintf("failed=%d", event.Failed))
	}
	if event.ScopeLabel != "" {
		parts = append(parts, "scope="+event.ScopeLabel)
	}
	if event.PlannedAction != "" {
		parts = append(parts, "action="+event.PlannedAction)
	}
	if event.Message != "" {
		parts = append(parts, "message="+quoteIfNeeded(event.Message))
	}
	_, _ = fmt.Fprintln(r.w, strings.Join(parts, " "))
}

func quoteIfNeeded(value string) string {
	if strings.IndexFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n'
	}) == -1 {
		return value
	}
	return fmt.Sprintf("%q", value)
}
