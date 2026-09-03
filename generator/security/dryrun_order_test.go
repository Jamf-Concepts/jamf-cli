// Copyright 2026, Jamf Software LLC

package security

import (
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// TestDryRunPreviewIsEmittedBeforeTheConfirmation pins the ordering of the two
// gates on a destructive Security Cloud command. ConfirmAction errors when
// --yes is absent and stdin is not a terminal, so with the confirmation first a
// preview of `device-lifecycle purge` — the most destructive command in the CLI
// — was unobtainable in CI without also pre-authorising the real purge. The day
// -n falls off that command line, or out of JAMF_CLI_ARGS, the purge runs with
// its confirmation already suppressed.
//
// Asserted on the emitted source rather than only at runtime because the same
// ordering decision is made independently in the Platform template, and the
// generated commands are only as ordered as the template that writes them.
func TestDryRunPreviewIsEmittedBeforeTheConfirmation(t *testing.T) {
	// The shape of device-lifecycle purge: destructive, a {customerId} filled
	// at request time, and a body that scopes what is purged.
	op := &parser.Operation{
		Name:          "purge",
		Method:        "POST",
		Path:          "/lifecycle/v1/{customerId}/devices/purge",
		IsDestructive: true,
		RequestBody:   &parser.RequestBody{Schema: objSchema(map[string]string{"deviceIds": "array"})},
		Responses:     map[string]*parser.Response{"200": {Schema: objSchema(map[string]string{"purged": "integer"})}},
	}
	r := &parser.Resource{Name: "device-lifecycle", GoName: "DeviceLifecycle", Operations: []*parser.Operation{op}}

	files, err := Generate([]*parser.Resource{r}, map[string]string{"device-lifecycle": "Lifecycle"}, t.TempDir())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}
	src := string(b)

	preview := strings.Index(src, "security.ReportDryRun(")
	confirm := strings.Index(src, "security.ConfirmAction(")
	if preview < 0 {
		t.Fatal("generated purge emits no dry-run preview")
	}
	if confirm < 0 {
		t.Fatal("generated purge emits no confirmation")
	}
	if preview > confirm {
		t.Error("the confirmation is emitted before the dry-run preview, so -n cannot be used without --yes on a destructive command")
	}
	// The confirmation still names the resolved customer, which is never
	// user-facing — it comes off the Lifecycle JWT — so a purge would otherwise
	// be confirmed without saying whose devices it purges.
	if !strings.Contains(src, `fmt.Sprintf("purge for customer %q", customerID)`) {
		t.Error("the confirmation no longer names the resolved customer")
	}
	// The unscoped-body refusal is a validation, not an authorisation, so it
	// stays ahead of the preview: there is nothing worth previewing about a
	// purge with no scope.
	refusal := strings.Index(src, "refusing an unscoped purge")
	if refusal < 0 || refusal > preview {
		t.Error("the unscoped-body refusal must stay ahead of the dry-run preview")
	}
}
