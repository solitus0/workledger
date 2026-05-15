package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type ttyReporter struct {
	w         io.Writer
	now       func() time.Time
	mu        sync.Mutex
	startedAt time.Time
	lastWidth int
	finished  bool
}

func NewTTYReporter(w io.Writer) Reporter {
	if w == nil {
		return NewNoopReporter()
	}
	return &ttyReporter{
		w:   w,
		now: time.Now,
	}
}

func (r *ttyReporter) Start(event Event) {
	r.render(event, false)
}

func (r *ttyReporter) Event(event Event) {
	r.render(event, false)
}

func (r *ttyReporter) Finish(event Event) {
	r.render(event, true)
}

func (r *ttyReporter) render(event Event, final bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finished {
		return
	}
	if r.startedAt.IsZero() {
		r.startedAt = r.now()
	}

	line := r.formatLine(event)
	padding := ""
	if extra := r.lastWidth - len(line); extra > 0 {
		padding = strings.Repeat(" ", extra)
	}
	_, _ = fmt.Fprintf(r.w, "\r%s%s", line, padding)
	r.lastWidth = len(line)

	if final {
		_, _ = fmt.Fprint(r.w, "\n")
		r.finished = true
	}
}

func (r *ttyReporter) formatLine(event Event) string {
	parts := make([]string, 0, 6)
	if event.Phase != "" {
		parts = append(parts, event.Phase)
	}
	if units := formatTTYUnits(event); units != "" {
		parts = append(parts, units)
	}
	if event.Failed > 0 {
		parts = append(parts, fmt.Sprintf("failed=%d", event.Failed))
	}
	if event.ScopeLabel != "" {
		parts = append(parts, event.ScopeLabel)
	}
	if event.Message != "" {
		parts = append(parts, event.Message)
	}
	parts = append(parts, fmt.Sprintf("elapsed=%s", r.now().Sub(r.startedAt).Round(time.Millisecond)))
	return strings.Join(parts, " | ")
}

func formatTTYUnits(event Event) string {
	if event.ScopeTotal > 0 {
		return fmt.Sprintf("%s %d/%d", renderTTYBar(event.ScopeDone, event.ScopeTotal, 10), event.ScopeDone, event.ScopeTotal)
	}
	if event.ScopeDone > 0 {
		return fmt.Sprintf("scopes=%d", event.ScopeDone)
	}
	if event.WorkTotal > 0 {
		return fmt.Sprintf("worklogs=%d/%d", event.WorkDone, event.WorkTotal)
	}
	if event.WorkDone > 0 {
		return fmt.Sprintf("worklogs=%d", event.WorkDone)
	}
	return ""
}

func renderTTYBar(done, total, width int) string {
	if total <= 0 {
		return ""
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := 0
	if width > 0 {
		filled = done * width / total
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}
