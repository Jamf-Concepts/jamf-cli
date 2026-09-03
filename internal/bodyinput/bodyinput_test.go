// Copyright 2026, Jamf Software LLC

package bodyinput

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeAcceptsJSONAndYAMLIdentically(t *testing.T) {
	jsonBody := []byte(`{"name":"zone","nameServers":[{"gatewayId":"gw-1","ip":"198.51.100.53"}],"secureDns":true}`)
	yamlBody := []byte(`name: zone
nameServers:
  - gatewayId: gw-1
    ip: 198.51.100.53
secureDns: true
`)

	fromJSON, err := Normalize(jsonBody)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	fromYAML, err := Normalize(yamlBody)
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	if !reflect.DeepEqual(fromJSON, fromYAML) {
		t.Errorf("JSON and YAML decoded differently:\n json = %#v\n yaml = %#v", fromJSON, fromYAML)
	}
}

// A YAML body must come back as JSON-native types, not as yaml.v3's own. The
// failure this pins is not a wrong value but a body that decodes here and then
// fails to marshal in the transport, where it reads as an internal error.
//
// The keys are chosen for what json.Marshal refuses, which moved with the
// toolchain and is why this pins shapes rather than an error. Any non-string key
// decodes to map[any]any, which Go 1.26's encoding/json rejects outright; 1.27's
// accepts an *integer* key (spelling it "80") and still rejects a boolean, float
// or null one — "object member name must be a string". So an integer key alone
// would no longer catch a regression here.
func TestNormalizeYAMLProducesMarshalableTypes(t *testing.T) {
	raw := []byte(`ports:
  80: http
  443: https
enabled:
  true: always
  1.5: sometimes
notAfter: 2026-09-02T10:00:00Z
`)
	v, err := Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if _, err := json.Marshal(v); err != nil {
		t.Fatalf("decoded body is not marshalable: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any, got %T", v)
	}
	ports, ok := m["ports"].(map[string]any)
	if !ok {
		t.Fatalf("nested mapping not normalised: %T", m["ports"])
	}
	if ports["80"] != "http" || ports["443"] != "https" {
		t.Errorf("integer keys not spelled as strings: %#v", ports)
	}
	enabled, ok := m["enabled"].(map[string]any)
	if !ok {
		t.Fatalf("mapping with non-integer scalar keys not normalised: %T", m["enabled"])
	}
	if enabled["true"] != "always" || enabled["1.5"] != "sometimes" {
		t.Errorf("boolean and float keys not spelled as strings: %#v", enabled)
	}
	if s, ok := m["notAfter"].(string); !ok || s == "" {
		t.Errorf("notAfter: got %#v, want an RFC 3339 string", m["notAfter"])
	}
}

// A top-level array is the shape the two DNS whole-list replaces take, and
// --set cannot express it, so --file is the only route.
func TestNormalizeAcceptsATopLevelArray(t *testing.T) {
	v, err := Normalize([]byte("- hostname: a.example.com\n- hostname: b.example.com\n"))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("want []any, got %T", v)
	}
	if len(arr) != 2 {
		t.Errorf("want 2 elements, got %d", len(arr))
	}
}

// JSON is tried first, and a JSON body carrying literal newlines inside a
// string value is repaired rather than handed to YAML, which folds them into
// spaces without saying so.
func TestNormalizeRepairsControlCharactersInsteadOfFoldingThem(t *testing.T) {
	v, err := Normalize([]byte("{\"script\":\"line one\nline two\"}"))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := v.(map[string]any)["script"]
	if got != "line one\nline two" {
		t.Errorf("newline not preserved: %q", got)
	}
}

// A nil body means "send no body" to every caller, so input that carries
// nothing must be an error — otherwise a write that sent nothing is
// indistinguishable from one that was never given a --file.
func TestNormalizeRefusesInputThatCarriesNoContent(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":          "",
		"whitespace":     "  \n\t\n",
		"comments only":  "# nothing but a comment\n",
		"explicit null":  "null",
		"yaml empty doc": "---\n",
	} {
		if _, err := Normalize([]byte(raw)); err == nil {
			t.Errorf("%s: want an error, got none", name)
		}
	}
}

func TestNormalizeRejectsInputThatIsNeitherFormat(t *testing.T) {
	_, err := Normalize([]byte("{this is: [not valid, either\n"))
	if err == nil {
		t.Fatal("want an error, got none")
	}
	if !strings.Contains(err.Error(), "not valid JSON or YAML") {
		t.Errorf("error should name both formats, got %q", err)
	}
}
