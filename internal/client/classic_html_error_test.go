// Copyright 2026, Jamf Software LLC

package client

import (
	"strings"
	"testing"
)

// classicStatusPage is the shape the Classic API really answers with — captured
// verbatim from a Jamf Pro 11.31.1 409 on 2026-09-12.
func classicStatusPage(status, reason string) []byte {
	return []byte(`<html>
<head>
   <title>Status page</title>
</head>
<body style="font-family: sans-serif;">
<p style="font-size: 1.2em;font-weight: bold;margin: 1em 0px;">` + status + `</p>
<p>` + reason + `</p>
<p>You can get technical details <a href="http://www.w3.org/Protocols/rfc2616/rfc2616-sec10.html#sec10.4.10">here</a>.<br>
Please continue your visit at our <a href="/">home page</a>.
</p>
</body>
</html>
`)
}

// TestClassicHTMLErrorReason_ExtractsTheActionableSentence covers the reasons
// this API actually gives. Each is specific and each used to arrive buried in
// ~400 bytes of markup and inline CSS — which, escaped into the JSON error
// envelope, is one unreadable line.
func TestClassicHTMLErrorReason_ExtractsTheActionableSentence(t *testing.T) {
	cases := []struct{ reason string }{
		{"Error: Unable to match computer group"},
		{"Error: Mobile device groups cannot be assigned to an macOS profile"},
		{"Error: Computer groups cannot be assigned to an iOS profile"},
		{"Error: Unable to match jss user"},
		{"Unable to update the database"},
	}
	for _, tc := range cases {
		got := classicHTMLErrorReason(classicStatusPage("Conflict", tc.reason))
		if got != tc.reason {
			t.Errorf("reason = %q, want %q", got, tc.reason)
		}
	}
}

// TestClassicHTMLErrorReason_DropsTheBoilerplate keeps the two paragraphs that
// say nothing out of the message: the status name (which the HTTP code already
// carries) and the links.
func TestClassicHTMLErrorReason_DropsTheBoilerplate(t *testing.T) {
	got := classicHTMLErrorReason(classicStatusPage("Conflict", "Error: Unable to match building"))
	for _, unwanted := range []string{"Conflict", "technical details", "home page", "<a ", "href"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("message still carries %q: %q", unwanted, got)
		}
	}
}

// TestClassicHTMLErrorReason_IgnoresOtherBodies is the containment: anything
// that is not a Classic status page returns "" so the caller falls back to the
// raw body. Swallowing an unrecognised body would hide a JSON error envelope,
// a gateway refusal, or a WAF block behind an empty message.
func TestClassicHTMLErrorReason_IgnoresOtherBodies(t *testing.T) {
	notPages := [][]byte{
		[]byte(`{"httpStatus":409,"traceId":"abc","errors":[{"code":"CONFLICT"}]}`),
		[]byte(`<html><body><p>Some other page entirely</p></body></html>`),
		[]byte(``),
		[]byte(`plain text`),
	}
	for _, body := range notPages {
		if got := classicHTMLErrorReason(body); got != "" {
			t.Errorf("body %q should not be treated as a Classic status page; got %q", body, got)
		}
	}
}

// TestClassicHTMLErrorReason_PageWithNoReasonFallsBack covers a status page
// carrying only boilerplate: returning "" sends the caller the raw body rather
// than an error with no message at all.
func TestClassicHTMLErrorReason_PageWithNoReasonFallsBack(t *testing.T) {
	page := []byte(`<html><head><title>Status page</title></head><body>
<p style="font-weight: bold;">Conflict</p>
<p>You can get technical details <a href="x">here</a>.</p>
</body></html>`)
	if got := classicHTMLErrorReason(page); got != "" {
		t.Errorf("a page with no reason paragraph should fall back; got %q", got)
	}
}

// TestHTTPStatusError_RendersTheClassicReason pins the wiring: the reason has
// to reach the message the user reads, and a non-Classic body must still carry
// its own bytes through.
func TestHTTPStatusError_RendersTheClassicReason(t *testing.T) {
	err := httpStatusError(409, "PUT", "/JSSResource/policies/id/1",
		classicStatusPage("Conflict", "Error: Unable to match computer group"))
	if err == nil {
		t.Fatal("expected an error")
	}
	want := "request failed (HTTP 409): Error: Unable to match computer group"
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}

	raw := httpStatusError(409, "PUT", "/api/v1/x", []byte(`{"errors":[{"code":"X"}]}`))
	if !strings.Contains(raw.Error(), `{"errors":[{"code":"X"}]}`) {
		t.Errorf("a non-Classic body should pass through verbatim: %v", raw)
	}
}
