// Copyright 2026, Jamf Software LLC

package generated

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── escapeJSONStringLiterals ─────────────────────────────────────────────────

func TestEscapeJSONStringLiterals_NoControlChars(t *testing.T) {
	input := []byte(`{"name":"hello world"}`)
	got := escapeJSONStringLiterals(input)
	if string(got) != string(input) {
		t.Errorf("got %s, want unchanged %s", got, input)
	}
}

func TestEscapeJSONStringLiterals_LiteralNewline(t *testing.T) {
	// zsh echo interprets \n as a real newline even in single-quoted strings
	input := []byte("{\"scriptContents\":\"#!/bin/bash\necho recon\"}")
	got := escapeJSONStringLiterals(input)
	want := `{"scriptContents":"#!/bin/bash\necho recon"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if !json.Valid(got) {
		t.Errorf("output is not valid JSON: %s", got)
	}
}

func TestEscapeJSONStringLiterals_LiteralCRLF(t *testing.T) {
	input := []byte("{\"x\":\"line1\r\nline2\"}")
	got := escapeJSONStringLiterals(input)
	want := `{"x":"line1\r\nline2"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEscapeJSONStringLiterals_LiteralTab(t *testing.T) {
	input := []byte("{\"x\":\"col1\tcol2\"}")
	got := escapeJSONStringLiterals(input)
	want := `{"x":"col1\tcol2"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEscapeJSONStringLiterals_EscapedQuoteNotMistreatedAsStringEnd(t *testing.T) {
	// \"  inside a string should NOT toggle inString
	input := []byte("{\"x\":\"say \\\"hello\\\" now\nend\"}")
	got := escapeJSONStringLiterals(input)
	if !json.Valid(got) {
		t.Errorf("output not valid JSON: %s", got)
	}
	var m map[string]string
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["x"] != "say \"hello\" now\nend" {
		t.Errorf("x = %q, want correct value", m["x"])
	}
}

func TestEscapeJSONStringLiterals_NewlineOutsideString(t *testing.T) {
	// Newlines between JSON tokens are valid — should pass through unchanged
	input := []byte("{\n\"x\":\"y\"\n}")
	got := escapeJSONStringLiterals(input)
	if string(got) != string(input) {
		t.Errorf("whitespace outside strings should not change: got %s", got)
	}
	if !json.Valid(got) {
		t.Errorf("output not valid JSON: %s", got)
	}
}

// ─── normalizeInputToJSON (zsh echo regression) ───────────────────────────────

func TestNormalizeInputToJSON_JSONPassthrough(t *testing.T) {
	input := []byte(`{"name":"Test","scriptContents":"#!/bin/bash\necho recon"}`)
	out, err := normalizeInputToJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(input) {
		t.Errorf("valid JSON should pass through unchanged: got %s", out)
	}
}

func TestNormalizeInputToJSON_ZshEchoLiteralNewline(t *testing.T) {
	// Simulates: echo '{"scriptContents":"#!/bin/bash\necho recon"}' in zsh
	// zsh echo interprets \n as real newline — input is invalid JSON
	input := []byte("{\"name\":\"Test\",\"scriptContents\":\"#!/bin/bash\necho recon\necho done\",\"priority\":\"AFTER\"}")
	if json.Valid(input) {
		t.Skip("input is already valid JSON on this platform")
	}
	out, err := normalizeInputToJSON(input)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if !json.Valid(out) {
		t.Errorf("output not valid JSON: %s", out)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["scriptContents"] != "#!/bin/bash\necho recon\necho done" {
		t.Errorf("scriptContents = %q, want newlines preserved", m["scriptContents"])
	}
}

func TestNormalizeInputToJSON_YAMLBlockLiteral(t *testing.T) {
	input := []byte("name: Test\nscriptContents: |\n  #!/bin/bash\n  echo recon\n")
	out, err := normalizeInputToJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Errorf("output not valid JSON: %s", out)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["name"] != "Test" {
		t.Errorf("name = %q, want Test", m["name"])
	}
	// Block literal trailing newline is preserved by yaml.v3
	if !strings.Contains(m["scriptContents"], "#!/bin/bash") || !strings.Contains(m["scriptContents"], "echo recon") {
		t.Errorf("scriptContents = %q, missing expected content", m["scriptContents"])
	}
}

// writeTempFile creates a file with given contents and returns its path.
// Uses t.TempDir() so cleanup is automatic.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file %s: %v", p, err)
	}
	return p
}

// unmarshalMap unmarshals JSON into a map for field-level assertions.
func unmarshalMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, data)
	}
	return m
}

// ─── injectFileFields (JSON request bodies) ───────────────────────────────────

func TestInjectFileFields_NoActiveFlag_ReturnsBodyUnchanged(t *testing.T) {
	body := []byte(`{"name":"foo","enabled":true}`)
	out, err := injectFileFields(body, []fileFieldSpec{
		{FilePath: "", Field: "scriptContents", Encoding: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(body) {
		t.Errorf("body changed: got %s, want %s", out, body)
	}
}

func TestInjectFileFields_NoActiveFlag_NilBodyStaysNil(t *testing.T) {
	out, err := injectFileFields(nil, []fileFieldSpec{{FilePath: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("got %q, want nil", out)
	}
}

func TestInjectFileFields_RawEncoding(t *testing.T) {
	content := "#!/bin/bash\necho hello\n"
	path := writeTempFile(t, "deploy.sh", content)
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "scriptContents", Encoding: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["scriptContents"] != content {
		t.Errorf("scriptContents = %q, want %q", m["scriptContents"], content)
	}
}

func TestInjectFileFields_Base64Encoding(t *testing.T) {
	content := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	path := filepath.Join(t.TempDir(), "token.p7m")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "encodedToken", Encoding: "base64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	want := base64.StdEncoding.EncodeToString(content)
	if m["encodedToken"] != want {
		t.Errorf("encodedToken = %q, want %q", m["encodedToken"], want)
	}
}

func TestInjectFileFields_DefaultEncodingIsRaw(t *testing.T) {
	content := "plain text"
	path := writeTempFile(t, "file.txt", content)
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "body", Encoding: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["body"] != content {
		t.Errorf("body = %q, want %q", m["body"], content)
	}
}

func TestInjectFileFields_CompanionFieldPopulatedWhenAbsent(t *testing.T) {
	path := writeTempFile(t, "nmartin.p7m", "data")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "encodedToken", Encoding: "base64", CompanionField: "tokenFileName"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["tokenFileName"] != "nmartin.p7m" {
		t.Errorf("tokenFileName = %q, want nmartin.p7m", m["tokenFileName"])
	}
}

func TestInjectFileFields_CompanionFieldRespectsExisting(t *testing.T) {
	path := writeTempFile(t, "nmartin.p7m", "data")
	body := []byte(`{"tokenFileName":"user-choice.p7m"}`)
	out, err := injectFileFields(body, []fileFieldSpec{
		{FilePath: path, Field: "encodedToken", Encoding: "base64", CompanionField: "tokenFileName"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["tokenFileName"] != "user-choice.p7m" {
		t.Errorf("tokenFileName clobbered: got %q, want user-choice.p7m", m["tokenFileName"])
	}
}

func TestInjectFileFields_NameFallback_KeepExt(t *testing.T) {
	path := writeTempFile(t, "deploy-chrome.sh", "script")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "scriptContents", Encoding: "raw", NameFallback: "keep-ext", NameField: "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["name"] != "deploy-chrome.sh" {
		t.Errorf("name = %q, want deploy-chrome.sh", m["name"])
	}
}

func TestInjectFileFields_NameFallback_StripExt(t *testing.T) {
	path := writeTempFile(t, "wifi-settings.mobileconfig", "<plist/>")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "payload", Encoding: "raw", NameFallback: "strip-ext", NameField: "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["name"] != "wifi-settings" {
		t.Errorf("name = %q, want wifi-settings", m["name"])
	}
}

func TestInjectFileFields_NameFallback_None_DoesNotSetName(t *testing.T) {
	path := writeTempFile(t, "token.vpptoken", "data")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "serviceToken", Encoding: "raw", NameFallback: "none", NameField: "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if _, has := m["name"]; has {
		t.Errorf("name should not have been set, got %v", m["name"])
	}
}

func TestInjectFileFields_NameFallback_RespectsExistingName(t *testing.T) {
	path := writeTempFile(t, "deploy-chrome.sh", "script")
	body := []byte(`{"name":"Custom Name"}`)
	out, err := injectFileFields(body, []fileFieldSpec{
		{FilePath: path, Field: "scriptContents", Encoding: "raw", NameFallback: "strip-ext", NameField: "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["name"] != "Custom Name" {
		t.Errorf("name overwritten: got %q, want Custom Name", m["name"])
	}
}

func TestInjectFileFields_OverwritesExistingField(t *testing.T) {
	path := writeTempFile(t, "new.sh", "new contents")
	body := []byte(`{"scriptContents":"old contents","priority":"AFTER"}`)
	out, err := injectFileFields(body, []fileFieldSpec{
		{FilePath: path, Field: "scriptContents", Encoding: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["scriptContents"] != "new contents" {
		t.Errorf("scriptContents = %q, want new contents", m["scriptContents"])
	}
	if m["priority"] != "AFTER" {
		t.Errorf("priority clobbered: got %v, want AFTER", m["priority"])
	}
}

func TestInjectFileFields_EmptyBodyBootstrapsObject(t *testing.T) {
	path := writeTempFile(t, "deploy.sh", "x")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: path, Field: "scriptContents", Encoding: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty body")
	}
	_ = unmarshalMap(t, out) // must be valid JSON
}

func TestInjectFileFields_FileNotFound(t *testing.T) {
	_, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: filepath.Join(t.TempDir(), "does-not-exist.sh"), Field: "scriptContents", Encoding: "raw"},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error should mention reading: %v", err)
	}
}

func TestInjectFileFields_InvalidJSONBody(t *testing.T) {
	path := writeTempFile(t, "x.sh", "x")
	_, err := injectFileFields([]byte(`not json`), []fileFieldSpec{
		{FilePath: path, Field: "scriptContents"},
	})
	if err == nil {
		t.Fatal("expected parse error on invalid JSON body")
	}
}

func TestInjectFileFields_MultipleSpecsAppliedInOrder(t *testing.T) {
	scriptPath := writeTempFile(t, "s.sh", "S")
	tokenPath := writeTempFile(t, "t.p7m", "T")
	out, err := injectFileFields(nil, []fileFieldSpec{
		{FilePath: scriptPath, Field: "scriptContents", Encoding: "raw"},
		{FilePath: tokenPath, Field: "encodedToken", Encoding: "base64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["scriptContents"] != "S" {
		t.Errorf("scriptContents = %v, want S", m["scriptContents"])
	}
	if m["encodedToken"] != base64.StdEncoding.EncodeToString([]byte("T")) {
		t.Errorf("encodedToken base64 wrong: got %v", m["encodedToken"])
	}
}

// ─── setBodyStringField ───────────────────────────────────────────────────────

func TestSetBodyStringField_EmptyValueIsNoop(t *testing.T) {
	body := []byte(`{"a":1}`)
	out, err := setBodyStringField(body, "name", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(body) {
		t.Errorf("body changed on empty value: got %s", out)
	}
}

func TestSetBodyStringField_EmptyBodyBootstraps(t *testing.T) {
	out, err := setBodyStringField(nil, "name", "foo")
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["name"] != "foo" {
		t.Errorf("name = %v, want foo", m["name"])
	}
}

func TestSetBodyStringField_OverwritesExisting(t *testing.T) {
	body := []byte(`{"name":"old","other":true}`)
	out, err := setBodyStringField(body, "name", "new")
	if err != nil {
		t.Fatal(err)
	}
	m := unmarshalMap(t, out)
	if m["name"] != "new" {
		t.Errorf("name = %v, want new", m["name"])
	}
	if m["other"] != true {
		t.Errorf("other clobbered: %v", m["other"])
	}
}

func TestSetBodyStringField_InvalidJSONErrors(t *testing.T) {
	_, err := setBodyStringField([]byte("not json"), "name", "foo")
	if err == nil {
		t.Fatal("expected error on invalid JSON body")
	}
}

// ─── injectClassicFileFields (XML request bodies) ─────────────────────────────

func TestInjectClassicFileFields_NoActiveFlag_ReturnsBodyUnchanged(t *testing.T) {
	body := []byte(`<os_x_configuration_profile><general><name>x</name></general></os_x_configuration_profile>`)
	out, err := injectClassicFileFields(body, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: "", LeafName: "payloads"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(body) {
		t.Errorf("body changed: got %s", out)
	}
}

func TestInjectClassicFileFields_EmptyBodyBootstrapsRoot(t *testing.T) {
	path := writeTempFile(t, "wifi.mobileconfig", "<plist/>")
	out, err := injectClassicFileFields(nil, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "strip-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "<os_x_configuration_profile>") || !strings.Contains(s, "</os_x_configuration_profile>") {
		t.Errorf("root element missing: %s", s)
	}
	if !strings.Contains(s, "<general>") || !strings.Contains(s, "</general>") {
		t.Errorf("general element missing: %s", s)
	}
	if !strings.Contains(s, "<payloads><![CDATA[<plist/>]]></payloads>") {
		t.Errorf("payloads leaf missing or not CDATA-wrapped: %s", s)
	}
	if !strings.Contains(s, "<name>wifi</name>") {
		t.Errorf("strip-ext name fallback missing: %s", s)
	}
}

func TestInjectClassicFileFields_WhitespaceOnlyBodyBootstraps(t *testing.T) {
	path := writeTempFile(t, "x.mobileconfig", "<plist/>")
	out, err := injectClassicFileFields([]byte("   \n\t  "), "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "<os_x_configuration_profile>") {
		t.Errorf("expected root bootstrap, got %s", out)
	}
}

func TestInjectClassicFileFields_ReplacesExistingLeaf(t *testing.T) {
	path := writeTempFile(t, "new.mobileconfig", "NEW_PAYLOAD")
	body := []byte(`<os_x_configuration_profile><general><name>Wi-Fi</name><payloads>OLD_PAYLOAD</payloads></general></os_x_configuration_profile>`)
	out, err := injectClassicFileFields(body, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "strip-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "OLD_PAYLOAD") {
		t.Errorf("old payload still present: %s", s)
	}
	if !strings.Contains(s, "<![CDATA[NEW_PAYLOAD]]>") {
		t.Errorf("new payload not CDATA-wrapped: %s", s)
	}
	// Name fallback respected — <name>Wi-Fi</name> stays.
	if !strings.Contains(s, "<name>Wi-Fi</name>") {
		t.Errorf("existing name lost: %s", s)
	}
	if strings.Contains(s, "<name>new</name>") {
		t.Errorf("name fallback clobbered existing name: %s", s)
	}
}

func TestInjectClassicFileFields_InsertsBeforeParentClose_WhenLeafMissing(t *testing.T) {
	path := writeTempFile(t, "app.plist", "PREFS")
	body := []byte(`<mobile_device_application><general><name>App</name></general><app_configuration></app_configuration></mobile_device_application>`)
	out, err := injectClassicFileFields(body, "mobile_device_application", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"app_configuration"}, LeafName: "preferences", Encoding: "xml-cdata", NameFallback: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	want := `<app_configuration><preferences><![CDATA[PREFS]]></preferences></app_configuration>`
	if !strings.Contains(s, want) {
		t.Errorf("missing %s in output:\n%s", want, s)
	}
}

func TestInjectClassicFileFields_BuildsMissingParentPath(t *testing.T) {
	path := writeTempFile(t, "app.plist", "PREFS")
	body := []byte(`<mobile_device_application><general><name>App</name></general></mobile_device_application>`)
	out, err := injectClassicFileFields(body, "mobile_device_application", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"app_configuration"}, LeafName: "preferences", Encoding: "xml-cdata", NameFallback: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "<app_configuration><preferences><![CDATA[PREFS]]></preferences></app_configuration>") {
		t.Errorf("parent was not constructed:\n%s", s)
	}
	// Existing general is untouched.
	if !strings.Contains(s, "<general><name>App</name></general>") {
		t.Errorf("general element mutated:\n%s", s)
	}
}

func TestInjectClassicFileFields_RawEncoding(t *testing.T) {
	path := writeTempFile(t, "app.plist", "<key>Foo</key><string>Bar</string>")
	out, err := injectClassicFileFields(nil, "mobile_device_application", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"app_configuration"}, LeafName: "preferences", Encoding: "raw", NameFallback: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "<preferences><key>Foo</key><string>Bar</string></preferences>") {
		t.Errorf("raw encoding not applied as expected:\n%s", s)
	}
	if strings.Contains(s, "CDATA") {
		t.Errorf("raw encoding should not wrap in CDATA:\n%s", s)
	}
}

func TestInjectClassicFileFields_NameFallback_KeepExt(t *testing.T) {
	path := writeTempFile(t, "wifi.mobileconfig", "<plist/>")
	out, err := injectClassicFileFields(nil, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "keep-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "<name>wifi.mobileconfig</name>") {
		t.Errorf("keep-ext fallback missing: %s", out)
	}
}

func TestInjectClassicFileFields_NameFallback_EscapesSpecialChars(t *testing.T) {
	path := writeTempFile(t, "name<with>chars&.sh", "content")
	out, err := injectClassicFileFields(nil, "script", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "raw", NameFallback: "keep-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Should have escaped form, not raw chars inside <name>
	if !strings.Contains(s, "&lt;") || !strings.Contains(s, "&gt;") || !strings.Contains(s, "&amp;") {
		t.Errorf("special chars not escaped in name: %s", s)
	}
	if strings.Contains(s, "<name>name<with>") {
		t.Errorf("raw special chars leaked into name: %s", s)
	}
}

// Scope/category may carry their own <name> elements. Name fallback should
// target <general><name>, not arbitrary <name> tags elsewhere, and should not
// fire when <general><name> is already present even alongside those.
func TestInjectClassicFileFields_NameFallback_IgnoresScopeName(t *testing.T) {
	path := writeTempFile(t, "test.mobileconfig", "P")
	body := []byte(`<os_x_configuration_profile><general><name>KeepMe</name></general><scope><computer_groups><computer_group><name>All Macs</name></computer_group></computer_groups></scope></os_x_configuration_profile>`)
	out, err := injectClassicFileFields(body, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "strip-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "<name>KeepMe</name>") {
		t.Errorf("existing general/name lost: %s", s)
	}
	if strings.Contains(s, "<name>test</name>") {
		t.Errorf("name fallback fired despite existing general/name: %s", s)
	}
	if !strings.Contains(s, "<name>All Macs</name>") {
		t.Errorf("scope/computer_group/name lost: %s", s)
	}
}

// When <general> exists without a <name>, fallback should inject one.
func TestInjectClassicFileFields_NameFallback_InjectsIntoExistingGeneral(t *testing.T) {
	path := writeTempFile(t, "my.mobileconfig", "P")
	body := []byte(`<os_x_configuration_profile><general><category><id>-1</id></category></general></os_x_configuration_profile>`)
	out, err := injectClassicFileFields(body, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "strip-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "<name>my</name>") {
		t.Errorf("name fallback not injected: %s", out)
	}
}

func TestInjectClassicFileFields_MalformedXML_LeafOpensWithoutClose(t *testing.T) {
	path := writeTempFile(t, "x.mobileconfig", "X")
	body := []byte(`<os_x_configuration_profile><general><payloads>unterminated</general></os_x_configuration_profile>`)
	_, err := injectClassicFileFields(body, "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "none"},
	})
	if err == nil {
		t.Fatal("expected error for unterminated leaf tag")
	}
	if !strings.Contains(err.Error(), "opens without close") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInjectClassicFileFields_FileNotFound(t *testing.T) {
	_, err := injectClassicFileFields(nil, "x", []classicFileFieldSpec{
		{FilePath: filepath.Join(t.TempDir(), "nope.mobileconfig"), ParentPath: []string{"general"}, LeafName: "payloads"},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error should mention reading: %v", err)
	}
}

func TestInjectClassicFileFields_NameFallback_NoneDoesNotInject(t *testing.T) {
	path := writeTempFile(t, "app.plist", "P")
	body := []byte(`<mobile_device_application><app_configuration></app_configuration></mobile_device_application>`)
	out, err := injectClassicFileFields(body, "mobile_device_application", []classicFileFieldSpec{
		{FilePath: path, ParentPath: []string{"app_configuration"}, LeafName: "preferences", Encoding: "xml-cdata", NameFallback: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "<name>") {
		t.Errorf("name should not have been injected with NameFallback=none: %s", out)
	}
}

// ─── hasClassicGeneralName ────────────────────────────────────────────────────

func TestHasClassicGeneralName(t *testing.T) {
	cases := []struct {
		desc string
		body string
		want bool
	}{
		{"name inside general", `<x><general><name>foo</name></general></x>`, true},
		{"name outside general", `<x><name>foo</name><general></general></x>`, false},
		{"name in scope only", `<x><general></general><scope><name>g</name></scope></x>`, false},
		{"no general at all", `<x><name>foo</name></x>`, false},
		{"general without name", `<x><general><category><id>-1</id></category></general></x>`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := hasClassicGeneralName(tc.body); got != tc.want {
				t.Errorf("got %v, want %v for %q", got, tc.want, tc.body)
			}
		})
	}
}

// ─── classicXMLEscape ─────────────────────────────────────────────────────────

func TestClassicXMLEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a<b", "a&lt;b"},
		{"a>b", "a&gt;b"},
		{"a&b", "a&amp;b"},
		{"a\"b", "a&#34;b"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := classicXMLEscape(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
