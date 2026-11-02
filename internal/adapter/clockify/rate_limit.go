package clockify

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pace API-key traffic below Clockify's lowest paid-plan limit (20 requests/s).
// Free workspace hourly quotas cannot be solved by per-second pacing.
const requestInterval = 100 * time.Millisecond
const maxRetryWait = 5 * time.Second
const exhaustedCooldown = time.Minute

var requestGates sync.Map

type requestGate struct {
	mu       sync.Mutex
	next     time.Time
	cooldown time.Time
	rejected *RequestError
}

func (c *Client) requestGate(route string) *requestGate {
	key := c.baseURL + "\x00" + c.apiKey
	parts := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "workspaces" {
		// Workspace limits apply across client instances and API keys in this process.
		key = c.baseURL + "/workspaces/" + parts[2]
	}
	gate, _ := requestGates.LoadOrStore(key, &requestGate{})
	return gate.(*requestGate)
}

func (g *requestGate) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		now := time.Now()
		if g.rejected != nil && now.Before(g.cooldown) {
			err := g.rejected
			g.mu.Unlock()
			return err
		}
		next := g.next
		if g.cooldown.After(next) {
			next = g.cooldown
		}
		delay := next.Sub(now)
		if delay <= 0 {
			g.next = now.Add(requestInterval)
			g.rejected = nil
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()
		if err := waitRequest(ctx, delay); err != nil {
			return err
		}
	}
}

func (g *requestGate) throttle(delay time.Duration, failure *RequestError, terminal bool) *RequestError {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(g.cooldown) {
		g.cooldown = until
	}
	if terminal {
		copy := *failure
		copy.RetryAt = g.cooldown
		g.rejected = &copy
		return g.rejected
	}
	return failure
}

func retryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((365*24*time.Hour)/time.Second) {
			return time.Time{}, false
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if date, err := http.ParseTime(value); err == nil {
		if date.Before(now) {
			date = now
		}
		return date, true
	}
	return time.Time{}, false
}

func retryBackoff(attempt int) time.Duration {
	return time.Second<<attempt + time.Duration(rand.Int64N(int64(250*time.Millisecond)))
}

func waitRequest(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
