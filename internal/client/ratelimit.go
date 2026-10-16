// Copyright 2026, Jamf Software LLC

package client

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Gateway rate limiting, ported from jamfplatform-go-sdk (internal/client/
// throttle.go and retry.go; the wire evidence is that repo's
// docs/WIRE-FACTS.md#rate-limiting). In step with v1.6.1, which took the limit
// ceiling and the stricter-gap rule below back upstream. The SDK's limiter is internal, so the
// requests this client sends on a gateway profile pace themselves here. It is a
// copy, not a shared budget: SDK traffic in the same process paces on its own
// throttle. Keep the two in step until this client's gateway traffic moves onto
// the SDK's transport.
//
// The gateway limits per client ID and answers an exhausted budget with a 429
// that carries no Retry-After. Its only timing signal is x-ratelimit-reset, the
// epoch second the window ends, beside x-ratelimit-remaining. Nothing here
// encodes the limit or the window length: both are gateway configuration that
// changes without a release, and the headers carry whatever they currently are.
//
// The SDK's fixed minimum interval between requests is deliberately not ported,
// and the SDK client runs with it at 0 too (newPlatformSDKClient): it is a
// read-after-write cushion, and on a serial walk of thousands of requests it
// would add minutes. What keeps a client under the limit is a ceiling read from
// x-ratelimit-limit (see limitSpacing), which the budget gate alone does not
// provide: a live burst of 60 concurrent workers against the EU gateway on
// 2026-10-16, budget gate only, put 149 requests into one second and drew nine
// 429s. Two wire facts defeat the budget gate against a one-second window.
// x-ratelimit-reset and Date are whole seconds, so the time until reset reads
// as 0 for most of the window, and at 0 there is nothing to spread the budget
// across. And x-ratelimit-remaining is not the global budget: across one
// window of 100, each value from 19 down to 0 appears five times.

// rateLimitWaitMax caps every wait taken from the server's headers, so a
// misconfigured or hostile reset cannot stall a request indefinitely.
const rateLimitWaitMax = 60 * time.Second

// requestThrottle decides when each outbound gateway request may start. It is
// shared by every goroutine sending through the client; each caller reserves
// its start under one lock, in arrival order, then sleeps outside it.
//
// It spreads the requests the gateway says are left across the time left in
// its window: each start comes at least (time until reset) / (estimated
// remaining) after the previous one. With plenty left that is a few
// milliseconds, which leaves concurrency alone; as the budget drains the
// spacing grows, and at zero starts wait for the window to end. Until a
// response has carried the headers it does nothing, so an instance or a stub
// server sees no change.
//
// The estimate is the remaining count from the response to the most recently
// *started* request that has answered, minus every request started after it.
// Counting from the start covers the client's own in-flight requests, which the
// gateway counted after that remaining was taken; responses from older requests,
// which arrive out of order under concurrency, are ignored for the same reason.
//
// A gateway 429 (remaining 0 on the refusal itself) also sets a hold that no
// later response lifts, so every goroutine stops until the window ends: a
// limiter that counts refused requests would keep refusing a client whose other
// goroutines kept sending.
type requestThrottle struct {
	mu   sync.Mutex
	last time.Time // start reserved for the most recent caller
	seq  uint64    // starts reserved so far

	limit       int64     // last x-ratelimit-limit seen; 0 until one is
	budgetKnown bool      // a response has carried the headers
	budgetSeq   uint64    // seq of the request that response answered
	budgetLeft  int64     // its x-ratelimit-remaining
	windowEnd   time.Time // local time its x-ratelimit-reset falls at
	hold        time.Time // set by a gateway 429; no start before it
}

// wait blocks until the caller's reserved start is reached and returns the
// start's sequence number, to hand back to observe with the response, and how
// long it waited. A nil throttle never waits. A cancelled ctx aborts the wait;
// the reserved slot is not rolled back, which at worst spaces the next caller
// slightly further out.
func (t *requestThrottle) wait(ctx context.Context) (uint64, time.Duration, error) {
	if t == nil {
		return 0, 0, nil
	}
	now := time.Now()
	start, seq := t.reserve(now)
	d := start.Sub(now)
	if d <= 0 {
		return seq, 0, nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return seq, d, nil
	case <-ctx.Done():
		return seq, d, ctx.Err()
	}
}

// reserve returns the earliest time a request arriving at now may start, and
// records it as reserved.
//
// The window end is padded by a second wherever a start must wait for the
// window, because x-ratelimit-reset and Date are both whole seconds and the
// window ends somewhere in the second after reset. Starts released at the end
// of a wait are jittered across a further second, so goroutines held together
// do not all start at the same instant. The spacing uses the unpadded end, so a
// client with budget left is not slowed by the rounding.
func (t *requestThrottle) reserve(now time.Time) (time.Time, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.seq++
	seq := t.seq
	start := now
	if start.Before(t.hold) {
		start = t.hold.Add(rand.N(time.Second))
	}
	// The ceiling and the budget are each a gap after the previous start, and
	// the stricter one wins: both are measured from the same base, so their
	// gaps never add up.
	base := start
	if t.limit > 0 {
		start = later(start, t.last.Add(limitSpacing(t.limit)))
	}
	if paddedEnd := t.windowEnd.Add(time.Second); t.budgetKnown && base.Before(paddedEnd) {
		left := t.budgetLeft - int64(seq-t.budgetSeq-1)
		if left <= 0 {
			start = later(start, paddedEnd.Add(rand.N(time.Second)))
		} else if from := later(base, t.last); from.Before(t.windowEnd) {
			start = later(start, from.Add(t.windowEnd.Sub(from)/time.Duration(left)))
		}
	}
	t.last = start
	return start, seq
}

// observe records the rate-limit headers of the response to the request that
// started as seq, received at recv. A response without them changes nothing,
// and neither does a nil throttle.
//
// A success reporting remaining 0 is ignored rather than read as an exhausted
// budget: a limiter that reports 0 on every response would otherwise hold every
// request for a whole window. The cost of ignoring a genuine 0 is that the next
// request draws the 429 that sets the hold.
func (t *requestThrottle) observe(resp *http.Response, seq uint64, recv time.Time) {
	if t == nil {
		return
	}
	if limit, err := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Limit"), 10, 64); err == nil && limit > 0 {
		t.mu.Lock()
		t.limit = limit
		t.mu.Unlock()
	}
	remaining, untilReset, ok := parseRateLimitHeaders(resp.Header, rateLimitWaitMax)
	if !ok {
		return
	}
	refused := resp.StatusCode == http.StatusTooManyRequests && remaining == 0
	if remaining == 0 && !refused {
		return
	}
	end := recv.Add(untilReset)

	t.mu.Lock()
	defer t.mu.Unlock()
	if refused {
		t.hold = later(t.hold, end.Add(time.Second))
	}
	if !t.budgetKnown || seq > t.budgetSeq {
		t.budgetKnown, t.budgetSeq, t.budgetLeft, t.windowEnd = true, seq, remaining, end
	}
}

// limitSpacing is the minimum gap between starts once the gateway has
// reported its limit: the published one-second window shared across 80% of
// the limit, so a client running flat out stays a fifth under it (12.5ms, or
// 80 a second, at today's 100). The limit comes off the wire; only the window
// is assumed, and it is the one the platform documentation publishes. A
// limit of 0 is Tyk's "disabled" and sets no ceiling.
func limitSpacing(limit int64) time.Duration {
	return (time.Second + time.Second/4) / time.Duration(limit)
}

// later returns whichever of a and b is later.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// rateLimitResetWait returns how long to wait before retrying a 429, read from
// x-ratelimit-reset, and false when that header does not apply.
//
// It applies only to the gateway's own refusal: no Retry-After (which, when
// present, wins), and remaining 0. A service behind the gateway can answer 429
// too — the blueprints report does when its data store is overloaded — and the
// gateway stamps its headers on that response with remaining above 0 and a
// reset taken when the request started; waiting for that reset would retry an
// overloaded service every second with no backoff, so it falls back to the
// caller's exponential curve.
//
// The wait is measured against the response's Date, so client clock skew
// cancels out, padded a second (both values are whole seconds), jittered across
// another, and clamped to maxWait.
func rateLimitResetWait(resp *http.Response, maxWait time.Duration) (time.Duration, bool) {
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "" {
		return 0, false
	}
	remaining, untilReset, ok := parseRateLimitHeaders(resp.Header, maxWait)
	if !ok || remaining != 0 {
		return 0, false
	}
	return min(untilReset+time.Second+rand.N(time.Second), maxWait), true
}

// parseRateLimitHeaders reads x-ratelimit-remaining and x-ratelimit-reset from
// h, and returns the remaining count and how long until the reset, measured
// against h's Date (the local clock when Date is absent) and clamped to
// [0, maxWait]. It reports false when either header is absent or unusable.
//
// The seconds are counted as integers before they become a Duration: a reset
// sent in epoch milliseconds would otherwise saturate time.Time.Sub and wrap
// negative when padded.
func parseRateLimitHeaders(h http.Header, maxWait time.Duration) (int64, time.Duration, bool) {
	remaining, err := strconv.ParseInt(h.Get("X-Ratelimit-Remaining"), 10, 64)
	if err != nil || remaining < 0 {
		return 0, 0, false
	}
	reset, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64)
	if err != nil || reset <= 0 {
		return 0, 0, false
	}
	now, err := http.ParseTime(h.Get("Date"))
	if err != nil {
		now = time.Now()
	}
	secs := min(max(reset-now.Unix(), 0), int64(maxWait/time.Second))
	return remaining, time.Duration(secs) * time.Second, true
}
