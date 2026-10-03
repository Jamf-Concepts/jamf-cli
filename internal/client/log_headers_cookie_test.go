// Copyright 2026, Jamf Software LLC

package client

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestLogHeaders_RedactsCookiesInEveryMode(t *testing.T) {
	h := http.Header{}
	h.Add("Set-Cookie", "APBALANCEID=aws.S3CRET-affinity; Path=/; Secure")
	h.Add("Cookie", "JSESSIONID=S3CRET-session")
	h.Set("Content-Type", "application/xml")
	for _, redactAuth := range []bool{true, false} {
		var buf bytes.Buffer
		logHeaders(&buf, h, redactAuth)
		got := buf.String()
		if strings.Contains(got, "S3CRET-") {
			t.Errorf("logHeaders(redactAuth=%v) prints a cookie value:\n%s", redactAuth, got)
		}
		for _, want := range []string{"Set-Cookie: [redacted]", "Cookie: [redacted]", "Content-Type: application/xml"} {
			if !strings.Contains(got, want) {
				t.Errorf("logHeaders(redactAuth=%v) should print %q:\n%s", redactAuth, want, got)
			}
		}
	}
}
