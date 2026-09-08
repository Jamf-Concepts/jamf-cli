// Copyright 2026, Jamf Software LLC

package security

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// TestNoSecurityResourceQualifiesForApply records why the Radar API commands
// have no `apply`, and turns that into something that fails when it stops being
// true.
//
// `apply` is create-or-update-by-name, so it needs a named collection: a POST
// that creates into a collection, an item PUT/PATCH addressed by ID, and a list
// to resolve the name against. The Radar surface has none — it is singletons
// (/sse/v1/stream, /sse/v1/status), actions (/risk/v1/override,
// /sse/v1/verification, the device-lifecycle purge) and read-only documents
// (/sse/.well-known, /sse/v1/jwks.json). `stream update` is already an
// idempotent create-or-replace on a singleton, which is what apply would be
// there, so adding the verb would only alias it.
//
// So "security has no apply" is a property of the API, not an omission — but
// only until a spec refresh adds a real collection. This test is the tripwire:
// when one arrives, it fails and says to synthesize the verb rather than
// leaving the namespace inconsistent with pro, classic and platform.
func TestNoSecurityResourceQualifiesForApply(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/security")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	if len(resources) == 0 {
		t.Skip("no Security Cloud specs committed")
	}

	for _, r := range resources {
		listPath, hasList := collectionListPath(r)
		if !hasList {
			continue
		}
		create := collectionCreate(r, listPath)
		update := itemUpdate(r)
		if create == nil || update == nil {
			continue
		}
		t.Errorf("security resource %q now has a named collection (list %s, create %s %s, update %s %s) and so qualifies for `apply`, "+
			"which the security generator does not emit. Synthesize it the way generator/platform does (applySpec), or add a "+
			"documented reason it must not have one.",
			r.Name, listPath, create.Method, create.Path, update.Method, update.Path)
	}
}

// collectionListPath returns the resource's own bodyless collection GET.
func collectionListPath(r *parser.Resource) (string, bool) {
	for _, op := range r.Operations {
		if !strings.EqualFold(op.Method, http.MethodGet) {
			continue
		}
		if len(pathParams(op.Path)) == 0 && (op.IsList || op.Name == "list") {
			return op.Path, true
		}
	}
	return "", false
}

// collectionCreate returns a non-destructive POST with a body onto the
// collection path itself — a create, as opposed to an action.
func collectionCreate(r *parser.Resource, listPath string) *parser.Operation {
	for _, op := range r.Operations {
		if !strings.EqualFold(op.Method, http.MethodPost) || op.RequestBody == nil || op.IsDestructive {
			continue
		}
		if op.Path == listPath {
			return op
		}
	}
	return nil
}

// itemUpdate returns a PUT or PATCH with a body addressed by exactly one path
// parameter — an item update, as opposed to a singleton replace.
func itemUpdate(r *parser.Resource) *parser.Operation {
	for _, op := range r.Operations {
		if op.RequestBody == nil {
			continue
		}
		if !strings.EqualFold(op.Method, http.MethodPut) && !strings.EqualFold(op.Method, http.MethodPatch) {
			continue
		}
		if len(pathParams(op.Path)) == 1 {
			return op
		}
	}
	return nil
}

func pathParams(p string) []string {
	var out []string
	for i := 0; i < len(p); i++ {
		if p[i] != '{' {
			continue
		}
		end := strings.IndexByte(p[i:], '}')
		if end < 0 {
			break
		}
		out = append(out, p[i+1:i+end])
		i += end
	}
	return out
}
