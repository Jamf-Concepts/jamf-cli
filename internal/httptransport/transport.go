// Copyright 2026, Jamf Software LLC

// Package httptransport provides a shared *http.Transport factory used by the
// Jamf Pro HTTP client and the auth providers. Lives in its own package so
// both can import it without creating a cycle (client already depends on auth).
package httptransport

import (
	"net"
	"net/http"
	"time"
)

// Per-phase HTTP timeouts. http.Client.Timeout is deliberately unset — it's a
// whole-request deadline that caps body transfer, which breaks multi-GB
// package uploads and silently overrides caller-supplied context deadlines.
// Body transfer time is bounded solely by ctx; these phase timeouts exist to
// fail fast on dead networks, not healthy long transfers.
const (
	dialTimeout         = 10 * time.Second
	tlsHandshakeTimeout = 10 * time.Second
	idleConnTimeout     = 90 * time.Second
	// maxIdleConnsPerHost: Go default of 2 serialises parallel commands at
	// the connection pool. HTTP/2 multiplexes on a single conn when the
	// server speaks it, so this only binds on the HTTP/1.1 fallback path.
	maxIdleConnsPerHost = 10
)

// ResponseHeaderTimeout is how long a request waits for the server to start
// answering once it has been sent.
//
// Above 90s on purpose: the CloudFront edge in front of the platform gateway
// answers 504 after 90s (issue 392), and that 504 is a better answer than a
// client-side timeout — it arrives with a status the caller can act on, and
// at 60s the CLI gave up on requests the server was about to answer (a
// 1000-row computer inventory page took 45s on the reporter's tenant, and
// the same request timed out on another run). Not much above it either: it is
// also how long a dead connection hangs before the CLI says so.
const ResponseHeaderTimeout = 120 * time.Second

// New returns a fresh *http.Transport tuned for the CLI's workload: large
// package uploads, bursts of small API calls, HTTP/2 where the server
// supports it. Each caller owns its pool.
func New() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: ResponseHeaderTimeout,
		// 1 MiB write buffer pairs with the upload copy path — fewer
		// syscalls when streaming big package bodies.
		WriteBufferSize: 1 << 20,
		ReadBufferSize:  1 << 16,
	}
}

// HTTP1Only returns a copy of t that negotiates HTTP/1.1 and never HTTP/2.
// The copy has its own connection pool, so closing its idle connections cannot
// disturb t's.
func HTTP1Only(t *http.Transport) *http.Transport {
	c := t.Clone()
	c.ForceAttemptHTTP2 = false
	c.Protocols = new(http.Protocols)
	c.Protocols.SetHTTP1(true)
	// A TLS config the caller supplied can still offer h2 in ALPN, and the
	// server would then answer h2 to a transport that only speaks HTTP/1.1.
	if c.TLSClientConfig != nil {
		c.TLSClientConfig = c.TLSClientConfig.Clone()
		c.TLSClientConfig.NextProtos = []string{"http/1.1"}
	}
	return c
}
