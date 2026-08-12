// Copyright 2026, Jamf Software LLC

package generated

import (
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
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
		want := strings.ReplaceAll(minimizeClassicPlistSourceEscaping(p), "]]>", "]]&gt;")
		if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != want {
			t.Errorf("server-side decode would store %q, want %q", decoded, want)
		}
	}
}

func TestMinimizeClassicPlistSourceEscaping(t *testing.T) {
	cases := map[string]string{
		"&quot;":    `"`,
		"&#34;":     `"`,
		"&#x22;":    `"`,
		"&apos;":    "'",
		"&#39;":     "'",
		"&gt;":      ">",
		"&#xA;":     "\n",
		"&#9;":      "\t",
		"&amp;":     "&amp;",
		"&lt;":      "&lt;",
		"&#38;":     "&#38;",
		"&#x26;":    "&#x26;",
		"&#60;":     "&#60;",
		"&bogus;":   "&bogus;",
		"A & B; C":  "A & B; C",
		"&amp;#34;": "&amp;#34;",
		"a]]&gt;b":  "a]]>b",
		// CR references stay undecoded — CR is the only whitespace character
		// Jamf Pro stores inside string values, and XML 1.0 §2.11 line-end
		// normalisation would turn a decoded literal CR into an LF the server
		// then deletes.
		"&#13;":   "&#13;",
		"&#xD;":   "&#xD;",
		"&#xd;":   "&#xd;",
		"&#013;":  "&#013;",
		"a&#13;b": "a&#13;b",
	}
	for in, want := range cases {
		if got := minimizeClassicPlistSourceEscaping(in); got != want {
			t.Errorf("minimize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePayloads_CRReferencesReachServerBare(t *testing.T) {
	// Jamf Pro's own UI emits "&#13;" for banner line breaks. The reference
	// must reach the wire with a bare "&" so the server's single decode yields
	// an actual CR — escaping it would store the reference as literal text.
	for _, ref := range []string{"&#13;", "&#xD;", "&#xd;", "&#013;"} {
		plist := `<string>Welcome to` + ref + ref + `[CUSTOMER NAME]</string>`
		body := `<general><payloads><![CDATA[` + plist + `]]></payloads></general>`
		escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
		if !strings.Contains(escaped, ref) {
			t.Errorf("%s: CR reference not left bare on the wire: %q", ref, escaped)
		}
	}
}

func TestNormalizePayloads_OnlyCRReferencesLeftBare(t *testing.T) {
	// The CR exemption must not generalise: "&#38;" left bare would decode to
	// a bare "&" and the server rejects the write with 409.
	body := `<general><payloads><![CDATA[<string>R&amp;D&#13;&#38;&#x26;</string>]]></payloads></general>`
	escaped := cdataContent(t, string(normalizeClassicProfilePayloadsForSend([]byte(body))))
	if !strings.Contains(escaped, "&#13;") {
		t.Errorf("CR reference was escaped: %q", escaped)
	}
	for _, ref := range []string{"&amp;#38;", "&amp;#x26;"} {
		if !strings.Contains(escaped, ref) {
			t.Errorf("expected %s to stay escaped once, got %q", ref, escaped)
		}
	}
	// The server's single decode must reproduce the minimised source: "&amp;",
	// "&#38;" and "&#x26;" all survive as references (decoding them would leave
	// a bare "&" the server rejects), while the CR reference decodes to a CR.
	if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != `<string>R&amp;D&#13;&#38;&#x26;</string>` {
		t.Errorf("server would store %q", decoded)
	}
}

func TestClassicCRRefLen(t *testing.T) {
	cases := map[string]int{
		"&#13;":     5,
		"&#xD;":     5,
		"&#xd;":     5,
		"&#013;":    6,
		"&#13;rest": 5,
		"&#10;":     0,
		"&#38;":     0,
		"&#x26;":    0,
		"&quot;":    0,
		"&#;":       0,
		"&#zz;":     0,
		"plain":     0,
		"&#13":      0,
	}
	for in, want := range cases {
		if got := classicCRRefLen(in); got != want {
			t.Errorf("classicCRRefLen(%q) = %d, want %d", in, got, want)
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
	want := minimizeClassicPlistSourceEscaping(truePlist)
	if decoded := strings.ReplaceAll(escaped, "&amp;", "&"); decoded != want {
		t.Errorf("text-form round-trip: server would store %q, want %q", decoded, want)
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

// ─── formatStoredPayloadWarning ────────────────────────────────────────────
//
// A server-injected PayloadContent entry shifts array indices and can turn
// one real defect into a column of noise, so the report caps at
// maxReportedPayloadDiffs (3) and names the remainder count instead of
// listing every diff.

func diffsN(n int) []profileconvert.PayloadDiff {
	diffs := make([]profileconvert.PayloadDiff, n)
	for i := range diffs {
		diffs[i] = profileconvert.PayloadDiff{Path: strings.Repeat("x", i+1), Reason: "value differs"}
	}
	return diffs
}

func TestFormatStoredPayloadWarning_UnderCapListsAllNoTail(t *testing.T) {
	got := formatStoredPayloadWarning(diffsN(2))
	if strings.Contains(got, "more") {
		t.Errorf("2 diffs should not truncate, got %q", got)
	}
	if strings.Count(got, "value differs") != 2 {
		t.Errorf("expected both diffs listed, got %q", got)
	}
}

func TestFormatStoredPayloadWarning_AtCapListsAllNoTail(t *testing.T) {
	got := formatStoredPayloadWarning(diffsN(maxReportedPayloadDiffs))
	if strings.Contains(got, "more") {
		t.Errorf("exactly maxReportedPayloadDiffs diffs should not truncate, got %q", got)
	}
	if strings.Count(got, "value differs") != maxReportedPayloadDiffs {
		t.Errorf("expected all %d diffs listed, got %q", maxReportedPayloadDiffs, got)
	}
}

func TestFormatStoredPayloadWarning_OverCapTruncatesWithRemainder(t *testing.T) {
	got := formatStoredPayloadWarning(diffsN(5))
	if strings.Count(got, "value differs") != maxReportedPayloadDiffs {
		t.Errorf("expected only %d diffs listed, got %q", maxReportedPayloadDiffs, got)
	}
	if !strings.Contains(got, "… and 2 more") {
		t.Errorf("expected a remainder tail naming 2 more, got %q", got)
	}
	if !strings.Contains(got, "warning: the server stored 5 payload value(s)") {
		t.Errorf("expected the header to report the true total (5), got %q", got)
	}
}
