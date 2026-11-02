package clockify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func testHTTPClient(t *testing.T, f roundTripFunc) *Client {
	t.Helper()
	c := NewClient("test-key")
	c.baseURL = "https://clockify.test/" + t.Name()
	c.httpClient = &http.Client{Transport: f}
	return c
}

func Test429HonorsRetryAfterAndReplaysRejectedBody(t *testing.T) {
	for _, form := range []string{"seconds", "date"} {
		t.Run(form, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				calls := 0
				var bodies []string
				c := testHTTPClient(t, func(r *http.Request) (*http.Response, error) {
					calls++
					body, _ := io.ReadAll(r.Body)
					bodies = append(bodies, string(body))
					if calls == 1 {
						resp := testResponse(429, "")
						value := "2"
						if form == "date" {
							value = start.Add(2 * time.Second).UTC().Format(http.TimeFormat)
						}
						resp.Header.Set("Retry-After", value)
						return resp, nil
					}
					if time.Since(start) < 2*time.Second {
						t.Fatal("retried before Retry-After")
					}
					return testResponse(200, `{"id":"tag"}`), nil
				})
				if _, err := c.CreateTag(t.Context(), "workspace", "APP-1"); err != nil || calls != 2 || bodies[0] != bodies[1] {
					t.Fatalf("rejected POST replay: calls=%d bodies=%v err=%v", calls, bodies, err)
				}
			})
		})
	}
}

func TestWorkspacePacingSharedAcrossClientsAndKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var starts []time.Time
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			return testResponse(200, "[]"), nil
		})
		first := testHTTPClient(t, transport)
		second := testHTTPClient(t, transport)
		second.apiKey = "another-key"
		var wg sync.WaitGroup
		for i := range 6 {
			wg.Go(func() {
				c := first
				if i%2 != 0 {
					c = second
				}
				if _, err := c.ListProjects(t.Context(), "workspace"); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if len(starts) != 6 {
			t.Fatalf("unexpected requests: %d", len(starts))
		}
		for i := 1; i < len(starts); i++ {
			if starts[i].Sub(starts[i-1]) < requestInterval {
				t.Fatalf("requests burst: %v", starts)
			}
		}
	})
}

func Test429ExhaustionAndLongCooldownStopOtherRequests(t *testing.T) {
	for _, header := range []string{"", "3600", "malformed"} {
		t.Run("header="+header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					resp := testResponse(429, "")
					resp.Header.Set("Retry-After", header)
					return resp, nil
				})
				c := testHTTPClient(t, transport)
				start := time.Now()
				_, err := c.ListProjects(t.Context(), "workspace")
				var failure *RequestError
				if !errors.As(err, &failure) || failure.RetryAt.IsZero() || time.Since(start) > maxRetryWait {
					t.Fatalf("unbounded or hidden throttling: %v, elapsed=%s", err, time.Since(start))
				}
				wantCalls := 3
				if header == "3600" {
					wantCalls = 1
				}
				other := testHTTPClient(t, transport)
				if _, err := other.ListTags(t.Context(), "workspace"); !errors.As(err, &failure) || calls != wantCalls {
					t.Fatalf("cooldown did not protect other clients: calls=%d err=%v", calls, err)
				}
				// Another workspace has an independent quota and remains usable.
				if _, err := other.ListTags(t.Context(), "other-workspace"); err == nil || calls <= wantCalls {
					t.Fatal("workspace failures were coupled")
				}
			})
		})
	}
}

func TestPacingAndRetryWaitsHonorCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		c := testHTTPClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			cancel()
			resp := testResponse(429, "")
			resp.Header.Set("Retry-After", "2")
			return resp, nil
		})
		if _, err := c.ListProjects(ctx, "workspace"); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("canceled retry continued: calls=%d err=%v", calls, err)
		}
	})
}

func TestMutationFailuresAreNeverReplayedWithoutReconciliation(t *testing.T) {
	for _, outcome := range []string{"transport", "server", "decode"} {
		t.Run(outcome, func(t *testing.T) {
			calls := 0
			c := testHTTPClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if outcome == "transport" {
					return nil, io.ErrUnexpectedEOF
				}
				if outcome == "server" {
					return testResponse(503, ""), nil
				}
				return testResponse(200, "invalid JSON"), nil
			})
			_, err := c.CreateTag(t.Context(), "workspace", "APP-1")
			var unconfirmed *MutationError
			if !errors.As(err, &unconfirmed) || calls != 1 {
				t.Fatalf("unsafe retry: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestSafeReadRetriesTransientFailures(t *testing.T) {
	for _, outcome := range []string{"transport", "server"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				c := testHTTPClient(t, func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						if outcome == "transport" {
							return nil, io.ErrUnexpectedEOF
						}
						return testResponse(503, ""), nil
					}
					return testResponse(200, "[]"), nil
				})
				if _, err := c.ListProjects(t.Context(), "workspace"); err != nil || calls != 2 {
					t.Fatalf("read retry: calls=%d err=%v", calls, err)
				}
			})
		})
	}
}

func TestPaginationStopsWithoutLastPageHeader(t *testing.T) {
	for _, resource := range []string{"entries", "projects", "tags"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			c := testHTTPClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return testResponse(200, "[]"), nil
			})
			var err error
			switch resource {
			case "entries":
				_, err = c.ListUserTimeEntries(t.Context(), "workspace", "user", time.Now(), time.Now())
			case "projects":
				_, err = c.ListProjects(t.Context(), "workspace")
			case "tags":
				_, err = c.ListTags(t.Context(), "workspace")
			}
			if err != nil || calls != 1 {
				t.Fatalf("pagination did not stop: calls=%d err=%v", calls, err)
			}
		})
	}
}
