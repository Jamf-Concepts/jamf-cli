// Copyright 2026, Jamf Software LLC

package generated

import (
	"strings"
	"testing"
)

// ─── normalizeClassicProfilePayloadsForSend ───────────────────────────────────
//
// The Classic API does not treat <payloads> content per the XML spec: it
// entity-decodes CDATA content once and text content twice (PI-827). The
// normalizer rewrites any payloads form into CDATA with every "&" escaped
// once, which the server's single decode pass exactly inverts.

// serverIngest simulates what the Classic API stores for a CDATA-wrapped
// payload: one entity-decode pass over the content.
func serverIngest(t *testing.T, wire string) string {
	t.Helper()
	const openMark = "<payloads><![CDATA["
	const closeMark = "]]></payloads>"
	si := strings.Index(wire, openMark)
	ei := strings.Index(wire, closeMark)
	if si < 0 || ei < 0 {
		t.Fatalf("wire has no CDATA payloads block: %s", wire)
	}
	inner := wire[si+len(openMark) : ei]
	if strings.Contains(inner, "]]>") {
		t.Fatalf("wire CDATA content contains ]]> — malformed XML: %s", inner)
	}
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return r.Replace(inner)
}

func TestNormalizePayloads_CDATA_RoundTripsThroughServerDecode(t *testing.T) {
	plists := []string{
		`<string>R&amp;D</string>`,
		`<string>a &lt; b &gt; c &quot;d&quot;</string>`,
		`<string>x &#38; &#x26; y</string>`,
		`<string>raw > and ' and "</string>`,
		`<string>literal &amp;amp; entity text</string>`,
		"<string>target.signing_time >= timestamp('2025-05-31T00:00:00Z')</string>",
	}
	for _, p := range plists {
		body := `<os_x_configuration_profile><general><payloads><![CDATA[` + p + `]]></payloads></general></os_x_configuration_profile>`
		wire := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
		if got := serverIngest(t, wire); got != p {
			t.Errorf("server would store %q, want %q", got, p)
		}
	}
}

func TestNormalizePayloads_TextForm_ConvertedToCDATA(t *testing.T) {
	// A GET/backup response carries payloads as entity-escaped text. The
	// normalizer must decode it once to the true plist, then re-wrap so the
	// server stores the plist exactly.
	truePlist := `<?xml version="1.0"?><plist><dict><key>k</key><string>R&amp;D > 'x'</string></dict></plist>`
	escapedOnce := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(truePlist)
	body := `<os_x_configuration_profile><general><payloads>` + escapedOnce + `</payloads></general></os_x_configuration_profile>`
	wire := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
	if !strings.Contains(wire, "<payloads><![CDATA[") {
		t.Fatalf("text-form payloads not converted to CDATA: %s", wire)
	}
	if got := serverIngest(t, wire); got != truePlist {
		t.Errorf("server would store %q, want %q", got, truePlist)
	}
}

func TestNormalizePayloads_TextForm_NumericEntities(t *testing.T) {
	body := `<general><payloads>&lt;string&gt;A &#38; B &#x26; C&lt;/string&gt;</payloads></general>`
	wire := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
	want := `<string>A & B & C</string>`
	if got := serverIngest(t, wire); got != want {
		t.Errorf("server would store %q, want %q", got, want)
	}
}

func TestNormalizePayloads_CDATATerminatorGuarded(t *testing.T) {
	// "]]>" reaching the wrapper raw would truncate the CDATA section. The
	// guarded form must survive the server decode as the valid entity form.
	body := `<general><payloads><![CDATA[<string>a]]&gt;b</string>]]></payloads></general>`
	wire := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
	if strings.Contains(strings.TrimSuffix(wire[strings.Index(wire, "<![CDATA[")+9:], "]]></payloads></general>"), "]]>") {
		t.Fatalf("wire CDATA content contains raw ]]>: %s", wire)
	}
	if got := serverIngest(t, wire); got != `<string>a]]&gt;b</string>` {
		t.Errorf("server would store %q", got)
	}
}

func TestNormalizePayloads_OutsideContentUntouched(t *testing.T) {
	// Ampersands outside the payloads element (e.g. in the profile name,
	// already escaped once by classicXMLEscape) must not be touched.
	body := `<general><name>R&amp;D Profile</name><payloads><![CDATA[x & y]]></payloads><description>a &amp; b</description></general>`
	wire := string(normalizeClassicProfilePayloadsForSend([]byte(body)))
	if !strings.Contains(wire, `<name>R&amp;D Profile</name>`) || !strings.Contains(wire, `<description>a &amp; b</description>`) {
		t.Errorf("content outside payloads was modified: %s", wire)
	}
	if !strings.Contains(wire, `<![CDATA[x &amp; y]]>`) {
		t.Errorf("payloads content not escaped: %s", wire)
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
