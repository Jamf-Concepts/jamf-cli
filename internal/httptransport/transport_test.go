// Copyright 2026, Jamf Software LLC

package httptransport

import (
	"testing"
	"time"
)

func TestNew_NoClientTimeout(t *testing.T) {
	// The package exposes only a Transport factory; Client.Timeout must be
	// set by callers (they deliberately leave it zero). Guard against a
	// future edit that accidentally re-introduces a whole-request deadline
	// here by asserting the transport carries only per-phase deadlines.
	tr := New()

	if tr.TLSHandshakeTimeout != tlsHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", tr.TLSHandshakeTimeout, tlsHandshakeTimeout)
	}
	if tr.ResponseHeaderTimeout != ResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, ResponseHeaderTimeout)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if tr.MaxIdleConnsPerHost != maxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", tr.MaxIdleConnsPerHost, maxIdleConnsPerHost)
	}
	if tr.WriteBufferSize != 1<<20 {
		t.Errorf("WriteBufferSize = %d, want %d", tr.WriteBufferSize, 1<<20)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", tr.IdleConnTimeout)
	}
}

func TestNew_FreshPerCall(t *testing.T) {
	a := New()
	b := New()
	if a == b {
		t.Error("New() returned the same *http.Transport twice; callers must own their pool")
	}
}

// The CloudFront edge in front of the platform gateway answers 504 after 90s.
// A header timeout at or under that abandons a request whose answer — a
// result, or a 504 a paginated walk can act on — was still coming (issue 392).
func TestResponseHeaderTimeout_OutlastsTheGatewayEdge(t *testing.T) {
	const gatewayEdge = 90 * time.Second
	if ResponseHeaderTimeout <= gatewayEdge {
		t.Errorf("ResponseHeaderTimeout = %v, want more than the gateway edge's %v", ResponseHeaderTimeout, gatewayEdge)
	}
}
