// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestSecurityCloudSpecParity asserts every operation in the Security Cloud
// specs reaches the CLI as a subcommand.
//
// The generator emits only the HTTP methods its template handles
// (filterGenerableOps) and drops the rest without a warning. That is how PUT
// went missing: no platform spec had used it, so five operations — including
// the only way to set a DNS search domain — silently never generated, and the
// resources looked complete because every other verb was present.
//
// Security Cloud specs are refreshed often and from a repo this one does not
// control, so a refresh introducing an unhandled method, or a tag rename that
// leaves a resource unwired, has to fail here rather than quietly shrink the
// command tree. Counts, not names: this is a coverage check, and the per-name
// checks live in TestSecurityGatewayServedCommandsPresent.
//
// The count is of distinct *endpoints*, not raw spec operations: where a spec
// declares the same endpoint at two API versions, deduplicateVersionedOps keeps
// only the highest and it ships as one command. Counting operations instead made
// the arrival of a second version look like a regression — device groups
// publishing a v2 list alongside the deprecated v1 turned this test red while
// the command tree was in fact correct.
// versionSegments matches an API version path segment wherever it sits, the
// same shape generator/parser's stripVersionSegments removes.
var versionSegments = regexp.MustCompile(`/(?:v\d+|preview)(/|$)`)

func TestSecurityCloudSpecParity(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join("..", "..", "specs", "platform", "securitycloud_*.json"))
	if err != nil {
		t.Fatalf("globbing Security Cloud specs: %v", err)
	}
	if len(specs) == 0 {
		t.Skip("no Security Cloud specs committed")
	}

	// Keyed "method version-stripped-path" so two versions of one endpoint
	// collapse the same way the generator collapses them.
	endpoints := map[string]struct{}{}
	for _, path := range specs {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var doc struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		for specPath, item := range doc.Paths {
			for method := range item {
				switch method {
				case "get", "post", "put", "patch", "delete":
					endpoints[method+" "+versionSegments.ReplaceAllString(specPath, "$1")] = struct{}{}
				}
			}
		}
	}
	specOps := len(endpoints)

	security := findSecurityCmd(t)
	cliOps := 0
	for _, name := range gatewayServedSecurityResources {
		resource := findSubcommand(security, name)
		if resource == nil {
			t.Errorf("security command %q not wired — add it in security.go", name)
			continue
		}
		cliOps += len(resource.Commands())
	}

	if cliOps != specOps {
		t.Errorf("Security Cloud specs declare %d operations but the CLI exposes %d subcommands.\n"+
			"A shortfall usually means the specs use an HTTP method filterGenerableOps "+
			"(generator/platform/emitter.go) does not emit, or a new tag is unwired in security.go.",
			specOps, cliOps)
	}
}
