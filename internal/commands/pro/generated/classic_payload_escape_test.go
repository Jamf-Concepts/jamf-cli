// Copyright 2026, Jamf Software LLC

package generated

import (
	"strings"
	"testing"
)

// ─── normalizeClassicProfilePayloadsForSend ───────────────────────────────────
//
// The Classic API validates payload content after one entity decode and
// 409s when that yields a bare "&" or "<" — so raw CDATA is rejected for
// any plist containing "&amp;"/"&lt;" and the escaped form is the only
// submittable one (PI-827). Storage is then per-payload-type: MCX custom
// settings and notification settings fragments are entity-decoded once
// (escape stores them byte-exact); other payload types keep the extra
// layer, which verifyClassicProfileStored surfaces as a warning.

func cdataContent(t *testing.T, body string) string {
	t.Helper()
	const openMark = "<payloads><![CDATA["
	const closeMark = "]]></payloads>"
	si := strings.Index(body, openMark)
	ei := strings.LastIndex(body, closeMark)
	if si < 0 || ei < 0 {
		t.Fatalf("body has no CDATA payloads block: %s", body)
	}
	inner := body[si+len(openMark) : ei]
	if strings.Contains(inner, "]]>") {
		t.Fatalf("CDATA content contains a raw terminator — malformed XML: %s", body)
	}
	return inner
}

func TestNormalizePayloads_CDATA_EscapedOnce(t *testing.T) {
	// The wire form carries every "&" escaped once; the server's single
	// decode of MCX-family fragments restores the plist byte-exact.
	plists := []string{
		`<string>R&amp;D</string>`,
		`<string>a &lt; b &gt; c</string>`,
		`<string>raw > and ' and "</string>`,
		`<string>literal &amp;amp; entity text</string>`,
	}
	for _, p := range plists {
		body := `<os_x_configuration_profile><general><payloads><![CDATA[` + p + `]]></payloads></general></os_x_configuration_profile>`
		escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
		if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != p {
			t.Errorf("server-side decode would store %q, want %q", decoded, p)
		}
	}
}

func TestNormalizePayloads_TextForm_ConvertedToCDATA(t *testing.T) {
	// A GET/backup response carries payloads as entity-escaped text. The
	// normalizer decodes it once to the true plist and re-wraps as CDATA.
	truePlist := `<?xml version="1.0"?><plist><dict><key>k</key><string>R&amp;D > 'x'</string></dict></plist>`
	escapedOnce := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(truePlist)
	body := `<os_x_configuration_profile><general><payloads>` + escapedOnce + `</payloads></general></os_x_configuration_profile>`
	escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
	if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != truePlist {
		t.Errorf("text-form round-trip: server would store %q, want %q", decoded, truePlist)
	}
}

func TestNormalizePayloads_TextForm_NumericEntities(t *testing.T) {
	body := `<general><payloads>&lt;string&gt;A &#38; B &#x26; C&lt;/string&gt;</payloads></general>`
	escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
	want := `<string>A & B & C</string>`
	if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != want {
		t.Errorf("server would store %q, want %q", decoded, want)
	}
}

func TestNormalizePayloads_CDATATerminatorGuarded(t *testing.T) {
	body := `<general><payloads><![CDATA[<string>a]]&gt;b</string>]]></payloads></general>`
	escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
	if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != `<string>a]]&gt;b</string>` {
		t.Errorf("server would store %q", decoded)
	}
}

func TestNormalizePayloads_OutsideContentUntouched(t *testing.T) {
	body := `<general><name>R&amp;D Profile</name><payloads><![CDATA[x & y]]></payloads><description>a &amp; b</description></general>`
	got := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
	if !strings.Contains(got, `<name>R&amp;D Profile</name>`) || !strings.Contains(got, `<description>a &amp; b</description>`) {
		t.Errorf("content outside payloads was modified: %s", got)
	}
	if !strings.Contains(got, `<![CDATA[x &amp; y]]>`) {
		t.Errorf("payloads content not escaped: %s", got)
	}
}

func TestNormalizePayloads_NoPayloads(t *testing.T) {
	body := `<webhook><name>a &amp; b</name></webhook>`
	if got := string(normalizeClassicProfilePayloadsForSend([]byte(body))); got != body {
		t.Errorf("body without payloads changed: got %s", got)
	}
}

func TestNormalizePayloads_EmptyPayloads(t *testing.T) {
	body := `<general><payloads></payloads></general>`
	if got := string(normalizeClassicProfilePayloadsForSend([]byte(body))); got != body {
		t.Errorf("empty payloads changed: got %s", got)
	}
}

func TestNormalizePayloads_UnterminatedCDATA(t *testing.T) {
	body := `<general><payloads><![CDATA[broken`
	if got := string(normalizeClassicProfilePayloadsForSend([]byte(body))); got != body {
		t.Errorf("unterminated CDATA changed: got %s", got)
	}
}

// ─── verify/response helpers ──────────────────────────────────────────────────

func TestClassicProfilePayloadFromBody(t *testing.T) {
	// The body carries the normalizer's escaped form; extraction undoes the
	// single escape to recover the submitted plist.
	body := `<general><payloads><![CDATA[<plist><string>R&amp;amp;D</string></plist>]]></payloads></general>`
	if got := string(classicProfilePayloadFromBody([]byte(body))); got != "<plist><string>R&amp;D</string></plist>" {
		t.Errorf("got %q", got)
	}
	if got := classicProfilePayloadFromBody([]byte(`<general><payloads>text</payloads></general>`)); got != nil {
		t.Errorf("text-form should return nil, got %q", got)
	}
}

func TestClassicCreatedResourceID(t *testing.T) {
	if got := classicCreatedResourceID([]byte(`<os_x_configuration_profile><id>7105</id></os_x_configuration_profile>`)); got != "7105" {
		t.Errorf("got %q", got)
	}
	if got := classicCreatedResourceID([]byte(`<html>error</html>`)); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// ─── stripCDATASections ───────────────────────────────────────────────────────

func TestStripCDATASections_PreservesContentExactly(t *testing.T) {
	// Trailing/leading whitespace inside CDATA is significant — this is why
	// the rewrite is textual instead of a plist parse/re-serialise round trip.
	in := `<string><![CDATA[cdata & raw < > " ]]></string>`
	want := `<string>cdata &amp; raw &lt; &gt; " </string>`
	if got := string(stripCDATASections([]byte(in))); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestStripCDATASections_MultipleSections(t *testing.T) {
	in := `<a><![CDATA[x & y]]></a><b>plain &amp; kept</b><c><![CDATA[z < w]]></c>`
	want := `<a>x &amp; y</a><b>plain &amp; kept</b><c>z &lt; w</c>`
	if got := string(stripCDATASections([]byte(in))); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestStripCDATASections_NoCDATA(t *testing.T) {
	in := `<string>plain &amp; text</string>`
	if got := string(stripCDATASections([]byte(in))); got != in {
		t.Errorf("content without CDATA changed: got %s", got)
	}
}

func TestStripCDATASections_Unterminated(t *testing.T) {
	in := `<string><![CDATA[never ends`
	if got := string(stripCDATASections([]byte(in))); got != in {
		t.Errorf("unterminated CDATA changed: got %s", got)
	}
}
