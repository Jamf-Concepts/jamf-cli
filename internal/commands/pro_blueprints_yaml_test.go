// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/devicegroups"
	"gopkg.in/yaml.v3"
)

// mathSettings is the shape the live `com.jamf.ddm.math-settings` component
// carries: nested objects, a boolean and a number, so a document that renders
// the wrong *kind* of value fails on something other than a string compare.
const mathSettings = `{"Calculator":{"Basic":{"Enabled":true},"Scientific":false},"SystemBehavior":{"KeyboardSuggestions":2}}`

func mathSettingsExport() blueprintExport {
	return blueprintExport{
		Name:        "JNUC Lab - Math Settings",
		Description: "maths",
		Scope: blueprintExportScope{DeviceGroups: []blueprintExportScopeGroup{
			{ID: "source-uuid", Name: "Lab Macs", DeviceType: "COMPUTER"},
		}},
		Steps: []blueprints.BlueprintStep{{
			Name:                strPtr("Step 1"),
			ActivationPredicate: strPtr("@status(device.model.family) == 'Mac'"),
			Components: []blueprints.Component{{
				Identifier:    "com.jamf.ddm.math-settings",
				Configuration: json.RawMessage(mathSettings),
			}},
		}},
	}
}

// asJSONShape re-reads a decoded YAML document through JSON so the numeric
// types settle (yaml.v3 decodes 2 as an int, encoding/json as a float64) and
// the two documents can be compared as shapes rather than as Go types.
func asJSONShape(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshalling document: %v", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("re-reading document: %v", err)
	}
	return out
}

// TestBlueprintExportYAMLCarriesTheSameDocumentAsJSON pins the whole document
// rather than the one field, because the two defects it covers are different
// kinds of divergence: a component's json.RawMessage configuration came out as
// a sequence of the integer bytes of its own JSON text (`- 123`, `- 34`, …),
// and every key came out lower-cased (`activationpredicate`), yaml.v3 reading
// neither encoding/json's json.RawMessage case nor the `json` tag.
func TestBlueprintExportYAMLCarriesTheSameDocumentAsJSON(t *testing.T) {
	exp := mathSettingsExport()

	prev := outputFmt
	t.Cleanup(func() { outputFmt = prev })

	outputFmt = "json"
	jsonOut := captureStdout(t, func() {
		if err := printExport(exp); err != nil {
			t.Errorf("printExport json: %v", err)
		}
	})
	outputFmt = "yaml"
	yamlOut := captureStdout(t, func() {
		if err := printExport(exp); err != nil {
			t.Errorf("printExport yaml: %v", err)
		}
	})

	var fromJSON, fromYAML any
	if err := json.Unmarshal([]byte(jsonOut), &fromJSON); err != nil {
		t.Fatalf("decoding the json export: %v\n%s", err, jsonOut)
	}
	if err := yaml.Unmarshal([]byte(yamlOut), &fromYAML); err != nil {
		t.Fatalf("decoding the yaml export: %v\n%s", err, yamlOut)
	}
	if !reflect.DeepEqual(asJSONShape(t, fromYAML), fromJSON) {
		t.Errorf("the yaml export is not the same document as the json one\njson:\n%s\nyaml:\n%s", jsonOut, yamlOut)
	}

	// And the shape itself, so this still fails if both formats regress
	// together.
	doc, ok := fromYAML.(map[string]any)
	if !ok {
		t.Fatalf("yaml export is not a mapping: %T", fromYAML)
	}
	steps, ok := doc["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("yaml steps: %#v", doc["steps"])
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("yaml step is not a mapping: %T", steps[0])
	}
	if _, ok := step["activationPredicate"]; !ok {
		t.Errorf("yaml step has no activationPredicate key, keys: %v", sortedDocKeys(step))
	}
	if _, ok := step["activationpredicate"]; ok {
		t.Errorf("yaml step carries the lower-cased activationpredicate key")
	}
	comps, ok := step["components"].([]any)
	if !ok || len(comps) != 1 {
		t.Fatalf("yaml components: %#v", step["components"])
	}
	comp, ok := comps[0].(map[string]any)
	if !ok {
		t.Fatalf("yaml component is not a mapping: %T", comps[0])
	}
	cfg, ok := comp["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("yaml configuration is %T, want a mapping (a sequence means the json.RawMessage bytes were rendered)", comp["configuration"])
	}
	if _, ok := cfg["Calculator"]; !ok {
		t.Errorf("yaml configuration has no Calculator key, keys: %v", sortedDocKeys(cfg))
	}
	if _, ok := cfg["SystemBehavior"]; !ok {
		t.Errorf("yaml configuration has no SystemBehavior key, keys: %v", sortedDocKeys(cfg))
	}
}

func sortedDocKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestBlueprintExportYAMLAppliesBack is the half that makes the export useful:
// `apply` has to read the file `export -o yaml` writes. It did — through the
// byte sequence, which only yaml.v3 could read back — so fixing the rendering
// without the read side would have traded a broken-looking document for one
// that no longer applies.
func TestBlueprintExportYAMLAppliesBack(t *testing.T) {
	prev := outputFmt
	t.Cleanup(func() { outputFmt = prev })
	outputFmt = "yaml"

	yamlOut := captureStdout(t, func() {
		if err := printExport(mathSettingsExport()); err != nil {
			t.Errorf("printExport yaml: %v", err)
		}
	})

	target := &classicHTTPMock{
		statusCode: 200,
		body:       `{"results":[{"groupPlatformId":"target-uuid","groupName":"Lab Macs"}]}`,
	}
	req, err := parseBlueprintApplyInput(context.Background(), []byte(yamlOut), target, nil)
	if err != nil {
		t.Fatalf("applying the yaml export: %v\n%s", err, yamlOut)
	}
	if req.Name != "JNUC Lab - Math Settings" {
		t.Errorf("name: got %q", req.Name)
	}
	if len(req.Scope.DeviceGroups) != 1 || req.Scope.DeviceGroups[0] != "target-uuid" {
		t.Errorf("scope: got %v, want [target-uuid]", req.Scope.DeviceGroups)
	}
	if len(req.Steps) != 1 || req.Steps[0].ActivationPredicate == nil {
		t.Fatalf("steps: %#v", req.Steps)
	}
	if got := *req.Steps[0].ActivationPredicate; got != "@status(device.model.family) == 'Mac'" {
		t.Errorf("activationPredicate: got %q", got)
	}
	var want, got any
	if err := json.Unmarshal([]byte(mathSettings), &want); err != nil {
		t.Fatalf("decoding the expected configuration: %v", err)
	}
	if err := json.Unmarshal(req.Steps[0].Components[0].Configuration, &got); err != nil {
		t.Fatalf("configuration did not survive as JSON: %v\n%s", err, req.Steps[0].Components[0].Configuration)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("configuration: got %#v, want %#v", got, want)
	}
}

// TestBlueprintApplyRefusesAByteSequenceConfiguration covers the one document
// the read side now binds and must not send: a YAML export written by 1.31.0 or
// earlier, whose configuration is a sequence of integers. A JSON array binds to
// a json.RawMessage without complaint, so nothing else would catch it before
// the gateway.
func TestBlueprintApplyRefusesAByteSequenceConfiguration(t *testing.T) {
	legacy := []byte(`name: Legacy
scope:
  deviceGroups:
    - id: source-uuid
      name: Lab Macs
      deviceType: COMPUTER
steps:
  - activationpredicate: null
    components:
      - configuration:
          - 123
          - 125
        identifier: com.jamf.ddm.math-settings
    name: Step 1
`)
	target := &classicHTTPMock{
		statusCode: 200,
		body:       `{"results":[{"groupPlatformId":"target-uuid","groupName":"Lab Macs"}]}`,
	}
	_, err := parseBlueprintApplyInput(context.Background(), legacy, target, nil)
	if err == nil {
		t.Fatal("expected a refusal for a byte-sequence configuration")
	}
	if !strings.Contains(err.Error(), "com.jamf.ddm.math-settings") {
		t.Errorf("refusal does not name the component: %v", err)
	}
	if !strings.Contains(err.Error(), "Re-export") {
		t.Errorf("refusal does not name the remedy: %v", err)
	}
}

// TestBlueprintScaffoldYAMLRendersConfigurationAsAMapping covers the other
// route the same json.RawMessage takes to stdout. `apply --scaffold -o yaml` is
// how the input format is discovered, so a scaffold showing a byte sequence
// teaches a document the command cannot read.
func TestBlueprintScaffoldYAMLRendersConfigurationAsAMapping(t *testing.T) {
	prev := outputFmt
	t.Cleanup(func() { outputFmt = prev })
	outputFmt = "yaml"

	out := captureStdout(t, func() {
		if err := printScaffold(blueprintScaffold()); err != nil {
			t.Errorf("printScaffold yaml: %v", err)
		}
	})

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding the scaffold: %v\n%s", err, out)
	}
	steps, ok := doc["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("scaffold steps: %#v", doc["steps"])
	}
	comps, ok := steps[0].(map[string]any)["components"].([]any)
	if !ok || len(comps) != 1 {
		t.Fatalf("scaffold components: %#v", steps[0])
	}
	cfg := comps[0].(map[string]any)["configuration"]
	if _, ok := cfg.(map[string]any); !ok && cfg != nil {
		t.Errorf("scaffold configuration is %T, want a mapping", cfg)
	}
}

// TestEveryScaffoldYAMLBindsBackThroughUnmarshalInput holds the two halves of
// the printScaffold change together: a scaffold rendered as YAML now carries
// the `json`-tag keys (it used to carry the lower-cased Go field names), so
// the read path has to bind them. A scaffold whose keys no `apply` reads back
// is worse than no scaffold — the command teaches an input format it then
// ignores field by field, silently.
func TestEveryScaffoldYAMLBindsBackThroughUnmarshalInput(t *testing.T) {
	prev := outputFmt
	t.Cleanup(func() { outputFmt = prev })

	cases := []struct {
		name     string
		scaffold any
		target   func() any
	}{
		{"blueprint", blueprintScaffold(), func() any { return &blueprints.CreateBlueprintRequest{} }},
		{"benchmark", benchmarkScaffold(), func() any { return &benchmarkPortableInput{} }},
		{"device group", deviceGroupScaffold(), func() any { return &devicegroups.DeviceGroupCreateRepresentationV1{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputFmt = "yaml"
			out := captureStdout(t, func() {
				if err := printScaffold(tc.scaffold); err != nil {
					t.Errorf("printScaffold yaml: %v", err)
				}
			})
			target := tc.target()
			if err := unmarshalInput([]byte(out), target); err != nil {
				t.Fatalf("scaffold does not bind back: %v\n%s", err, out)
			}
			want, err := json.Marshal(tc.scaffold)
			if err != nil {
				t.Fatalf("marshalling the scaffold: %v", err)
			}
			got, err := json.Marshal(target)
			if err != nil {
				t.Fatalf("marshalling what bound: %v", err)
			}
			var wantDoc, gotDoc any
			if err := json.Unmarshal(want, &wantDoc); err != nil {
				t.Fatalf("decoding the scaffold: %v", err)
			}
			if err := json.Unmarshal(got, &gotDoc); err != nil {
				t.Fatalf("decoding what bound: %v", err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("a field was dropped on the way back\nscaffold: %s\nbound:    %s\nyaml:\n%s", want, got, out)
			}
		})
	}
}
