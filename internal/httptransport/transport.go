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
	dialTimeout           = 10 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 60 * time.Second
	idleConnTimeout       = 90 * time.Second
	// maxIdleConnsPerHost: Go default of 2 serialises parallel commands at
	// the connection pool. HTTP/2 multiplexes on a single conn when the
	// server speaks it, so this only binds on the HTTP/1.1 fallback path.
	maxIdleConnsPerHost = 10
)

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
		ResponseHeaderTimeout: responseHeaderTimeout,
		// 1 MiB write buffer pairs with the upload copy path — fewer
		// syscalls when streaming big package bodies.
		WriteBufferSize: 1 << 20,
		ReadBufferSize:  1 << 16,
	}
}
