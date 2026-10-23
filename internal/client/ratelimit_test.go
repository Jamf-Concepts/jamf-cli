// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// gatewayResponse builds a response carrying the gateway's rate-limit headers
// as observed on the wire: Date and x-ratelimit-reset both in whole seconds,
// reset resetIn after Date.
func gatewayResponse(status int, remaining int, date time.Time, resetIn time.Duration) *http.Response {
	date = date.Truncate(time.Second)
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Date":                  []string{date.UTC().Format(http.TimeFormat)},
			"X-Ratelimit-Limit":     []string{"100"},
			"X-Ratelimit-Remaining": []string{strconv.Itoa(remaining)},
			"X-Ratelimit-Reset":     []string{strconv.FormatInt(date.Add(resetIn).Unix(), 10)},
		},
	}
}

// observed returns a throttle whose first request, started at now, was
// answered with resp at now.
func observed(now time.Time, resp *http.Response) *requestThrottle {
	g := &requestThrottle{}
	_, seq := g.reserve(now)
	g.observe(resp, seq, now)
	return g
}

// TestBudget_NoHeadersNoPacing pins that a server sending no rate-limit
// headers sees no pacing at all.
func TestBudget_NoHeadersNoPacing(t *testing.T) {
	now := time.Now()
	g := observed(now, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}})
	for i := range 50 {
		if start, _ := g.reserve(now); !start.Equal(now) {
			t.Fatalf("start %d delayed by %v with no rate-limit headers", i, start.Sub(now))
		}
	}
}

// TestBudget_PlentyLeftBarelySpaces pins that the budget adds nothing to the
// limit's ceiling while plenty is left: with 99 of 100 left, ten requests
// arriving together start one ceiling gap apart, not further.
func TestBudget_PlentyLeftBarelySpaces(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := observed(now, gatewayResponse(http.StatusOK, 99, now, time.Second))
	for i := range 10 {
		want := time.Duration(i+1)*limitSpacing(100) + time.Millisecond
		if start, _ := g.reserve(now); start.Sub(now) > want {
			t.Fatalf("start %d delayed %v with 99 left; want <= %v", i, start.Sub(now), want)
		}
	}
}

// TestBudget_SpreadsRemainingThenStops pins the slope and the stop: with 19
// left in a one-second window the next 19 starts are spread across it, and the
// 20th waits for the window to end.
func TestBudget_SpreadsRemainingThenStops(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	end := now.Add(time.Second)
	g := observed(now, gatewayResponse(http.StatusOK, 19, now, time.Second))

	var prev time.Time
	for i := range 19 {
		start, _ := g.reserve(now)
		if start.After(end) {
			t.Fatalf("start %d at +%v, past the window end; the budget had room", i, start.Sub(now))
		}
		if i > 0 && start.Sub(prev) < 30*time.Millisecond {
			t.Fatalf("starts %d and %d only %v apart; want the budget spread across the window", i-1, i, start.Sub(prev))
		}
		prev = start
	}
	if start, _ := g.reserve(now); start.Before(end.Add(time.Second)) {
		t.Errorf("start with the budget spent at +%v; want >= +2s (window end, padded a second)", start.Sub(now))
	}
}

// TestBudget_CountsInFlightRequests pins that the estimate subtracts requests
// started after the observed one: three left the client before the first
// answered "3 left", so the fifth must wait.
func TestBudget_CountsInFlightRequests(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := &requestThrottle{}
	for range 4 {
		g.reserve(now)
	}
	g.observe(gatewayResponse(http.StatusOK, 3, now, time.Second), 1, now)
	if start, _ := g.reserve(now); start.Before(now.Add(2 * time.Second)) {
		t.Errorf("fifth start at +%v; want held to the window end", start.Sub(now))
	}
}

// TestBudget_IgnoresOlderResponses pins that a late response from an older
// request, carrying a higher count taken earlier, does not restore the budget.
func TestBudget_IgnoresOlderResponses(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := &requestThrottle{}
	g.reserve(now)
	g.reserve(now)
	g.observe(gatewayResponse(http.StatusOK, 1, now, time.Second), 2, now)
	g.observe(gatewayResponse(http.StatusOK, 90, now, time.Second), 1, now)

	g.reserve(now) // the one left
	if start, _ := g.reserve(now); start.Before(now.Add(2 * time.Second)) {
		t.Errorf("start at +%v; want held", start.Sub(now))
	}
}

// TestBudget_GatewayRefusalHoldsEveryone pins the hold: after one gateway 429
// every start waits for the window, and a later success does not release it.
func TestBudget_GatewayRefusalHoldsEveryone(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := &requestThrottle{}
	g.reserve(now)
	g.reserve(now)
	g.observe(gatewayResponse(http.StatusTooManyRequests, 0, now, 0), 1, now)
	g.observe(gatewayResponse(http.StatusOK, 90, now, time.Second), 2, now)

	for i := range 5 {
		start, _ := g.reserve(now)
		if start.Before(now.Add(time.Second)) {
			t.Fatalf("start %d at +%v after a gateway 429; want held to the window end, padded a second", i, start.Sub(now))
		}
		if start.After(now.Add(2*time.Second + time.Duration(i+1)*limitSpacing(100))) {
			t.Fatalf("start %d at +%v; want released within a second of jitter after the hold, plus the limit's ceiling", i, start.Sub(now))
		}
	}
}

// TestBudget_ServiceRefusalSetsNoHold replays the blueprints report's own 429:
// the gateway still has budget, so nothing is held.
func TestBudget_ServiceRefusalSetsNoHold(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := observed(now, gatewayResponse(http.StatusTooManyRequests, 18, now, -9*time.Second))
	if start, _ := g.reserve(now); start.Sub(now) > 100*time.Millisecond {
		t.Errorf("start delayed %v after a service 429; want no hold", start.Sub(now))
	}
}

// TestBudget_SuccessReportingZeroIgnored guards against a limiter that reports
// remaining 0 on every response.
func TestBudget_SuccessReportingZeroIgnored(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := observed(now, gatewayResponse(http.StatusOK, 0, now, time.Second))
	for i := range 10 {
		ceiling := time.Duration(i+1) * limitSpacing(100)
		if start, _ := g.reserve(now); start.Sub(now) > ceiling {
			t.Fatalf("start %d delayed %v by a success reporting 0; want <= %v (the limit's ceiling)", i, start.Sub(now), ceiling)
		}
	}
}

// TestBudget_ExpiredWindowIgnored pins that an observation stops applying once
// its window is over: nothing beyond the limit's ceiling applies.
func TestBudget_ExpiredWindowIgnored(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	past := now.Add(-5 * time.Second)
	g := &requestThrottle{}
	_, seq := g.reserve(past)
	g.observe(gatewayResponse(http.StatusOK, 1, past, time.Second), seq, past)
	for i := range 10 {
		ceiling := time.Duration(i) * limitSpacing(100)
		if start, _ := g.reserve(now); start.Sub(now) > ceiling {
			t.Fatalf("start %d delayed %v by a window that ended seconds ago; want <= %v", i, start.Sub(now), ceiling)
		}
	}
}

// TestBudget_HoldClamped pins the ceiling on a hostile reset an hour out.
func TestBudget_HoldClamped(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := observed(now, gatewayResponse(http.StatusTooManyRequests, 0, now, time.Hour))
	if start, _ := g.reserve(now); start.Sub(now) > rateLimitWaitMax+2*time.Second {
		t.Errorf("held %v for a reset an hour out; want <= %v", start.Sub(now), rateLimitWaitMax+2*time.Second)
	}
}

func TestRateLimitResetWait(t *testing.T) {
	now := time.Now()
	retryAfter := gatewayResponse(http.StatusTooManyRequests, 0, now, 3*time.Second)
	retryAfter.Header.Set("Retry-After", "1")
	millis := gatewayResponse(http.StatusTooManyRequests, 0, now, 0)
	millis.Header.Set("X-Ratelimit-Reset", strconv.FormatInt(now.UnixMilli(), 10))

	tests := []struct {
		name     string
		resp     *http.Response
		ok       bool
		min, max time.Duration
	}{
		{"gateway refusal waits for the window", gatewayResponse(http.StatusTooManyRequests, 0, now, 3*time.Second), true, 4 * time.Second, 5 * time.Second},
		{"reset in the same second still pads", gatewayResponse(http.StatusTooManyRequests, 0, now, 0), true, time.Second, 2 * time.Second},
		{"reset in epoch millis is clamped", millis, true, rateLimitWaitMax, rateLimitWaitMax},
		{"Retry-After wins", retryAfter, false, 0, 0},
		{"service 429 with budget left", gatewayResponse(http.StatusTooManyRequests, 18, now, time.Second), false, 0, 0},
		{"not a 429", gatewayResponse(http.StatusOK, 0, now, time.Second), false, 0, 0},
		{"no headers", &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}, false, 0, 0},
		{"nil response", nil, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rateLimitResetWait(tt.resp, rateLimitWaitMax)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (wait %v)", ok, tt.ok, got)
			}
			if ok && (got < tt.min || got > tt.max) {
				t.Errorf("wait = %v, want in [%v, %v]", got, tt.min, tt.max)
			}
		})
	}
}

// writeGatewayRefusal answers with the gateway's 429 as captured on the wire:
// no Retry-After, remaining 0, reset in the current second.
func writeGatewayRefusal(w http.ResponseWriter) {
	now := time.Now()
	w.Header().Set("Date", now.UTC().Format(http.TimeFormat))
	w.Header().Set("X-Ratelimit-Limit", "100")
	w.Header().Set("X-Ratelimit-Remaining", "0")
	w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(now.Unix(), 10))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error": "Rate Limit Exceeded"}`))
}

// TestDo_GatewayRefusalWaitsForTheWindow is the end-to-end form: a gateway 429
// is retried once the window has ended, and a POST is retried too, because the
// refusal never reached the service.
func TestDo_GatewayRefusalWaitsForTheWindow(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeGatewayRefusal(w)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tid")))
	start := time.Now()
	resp, err := c.Do(context.Background(), http.MethodPost, "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2 (refusal, then retry)", got)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("retried after %v; want the window end, padded a second", elapsed)
	}
}

// TestDo_GatewayRefusalHoldsOtherRequests pins that the hold reaches a
// different request: issued straight after a gateway 429, it waits for the
// window rather than drawing a second refusal.
func TestDo_GatewayRefusalHoldsOtherRequests(t *testing.T) {
	var refusedAt atomic.Int64
	var buildingsAt atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pro/v1/refused":
			refusedAt.CompareAndSwap(0, time.Now().UnixNano()) // the first refusal; its retries follow
			writeGatewayRefusal(w)
		default:
			buildingsAt.Store(time.Now().UnixNano())
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tid")))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, _ = c.Do(ctx, http.MethodGet, "/v1/refused", nil)
	}()
	defer cancel()
	for refusedAt.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let observe record the refusal

	resp, err := c.Do(context.Background(), http.MethodGet, "/v1/buildings", nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = resp.Body.Close()
	if gap := time.Duration(buildingsAt.Load() - refusedAt.Load()); gap < 900*time.Millisecond {
		t.Errorf("second request reached the server %v after the refusal; want held to the window end", gap)
	}
}

// TestDo_InstanceIgnoresRateLimitHeaders pins that pacing is gateway-only: an
// instance answering remaining 1 is not slowed.
func TestDo_InstanceIgnoresRateLimitHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		w.Header().Set("Date", now.UTC().Format(http.TimeFormat))
		w.Header().Set("X-Ratelimit-Remaining", "1")
		w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(now.Add(5*time.Second).Unix(), 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	if c.throttle != nil {
		t.Fatal("instance client has a throttle; pacing must be gateway-only")
	}
	start := time.Now()
	for range 5 {
		resp, err := c.Do(context.Background(), http.MethodGet, "/v1/buildings", nil)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		_ = resp.Body.Close()
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("five instance requests took %v; want no pacing", elapsed)
	}
}

// TestDo_RateLimitedDoesNotSleepAfterTheLastAttempt pins that a request
// refused on every attempt fails as soon as the last refusal arrives, rather
// than sleeping first for a retry that will never be sent.
func TestDo_RateLimitedDoesNotSleepAfterTheLastAttempt(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"))
	start := time.Now()
	_, err := c.Do(context.Background(), http.MethodGet, "/v1/buildings", nil)
	if code := exitcode.CodeFrom(err); code != exitcode.RateLimited {
		t.Fatalf("exit code = %d, want %d (RateLimited); err = %v", code, exitcode.RateLimited, err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	if elapsed := time.Since(start); elapsed >= 2900*time.Millisecond {
		t.Errorf("took %v; want two 1s waits, not three", elapsed)
	}
}

// TestDo_VerboseShowsTheRefusalsRateLimitHeaders pins that a 429 the retry
// loop consumes is still logged: Do logs only the response that survives the
// loop, so without this the refusal's x-ratelimit-* headers never appear.
func TestDo_VerboseShowsTheRefusalsRateLimitHeaders(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeGatewayRefusal(w)
			return
		}
		w.Header().Set("X-Ratelimit-Remaining", "19")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, auth.NewTokenProvider("test-token"), WithGatewayScope(auth.TenantScope("tid")), WithVerbose(3))
	out := captureStderrConcurrently(t, func() {
		resp, err := c.Do(context.Background(), http.MethodGet, "/v1/jamf-pro-version", nil)
		if err != nil {
			t.Errorf("Do() error = %v", err)
			return
		}
		_ = resp.Body.Close()
	})
	for _, want := range []string{
		"429 Too Many Requests, retrying in",
		"X-Ratelimit-Remaining: 0",
		"X-Ratelimit-Reset: ",
		"Rate Limit Exceeded",
		"X-Ratelimit-Remaining: 19",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output lacks %q:\n%s", want, out)
		}
	}
}

// TestBudget_LimitSetsACeiling pins the ceiling the budget gate cannot give
// against a one-second window: with the reset reading as this second, so there
// is nothing to spread, 100 starts arriving together are still spaced to 80 a
// second.
func TestBudget_LimitSetsACeiling(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := observed(now, gatewayResponse(http.StatusOK, 19, now, 0))
	var last time.Time
	for range 100 {
		last, _ = g.reserve(now)
	}
	if spread := last.Sub(now); spread < 1200*time.Millisecond {
		t.Errorf("100 starts spread over %v; want >= 1.2s (80 a second)", spread)
	}
}

// TestBudget_DisabledLimitSetsNoCeiling pins that Tyk's limit 0 ("disabled")
// paces nothing.
func TestBudget_DisabledLimitSetsNoCeiling(t *testing.T) {
	now := time.Now()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Ratelimit-Limit": []string{"0"}}}
	g := observed(now, resp)
	for i := range 20 {
		if start, _ := g.reserve(now); !start.Equal(now) {
			t.Fatalf("start %d delayed %v with the limit disabled", i, start.Sub(now))
		}
	}
}
