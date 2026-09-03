// Copyright 2026, Jamf Software LLC

package client

import (
	"strings"
	"testing"
)

// A 403 from a Jamf Pro instance is an API-role problem, granted in Jamf Pro. A
// 403 from the gateway is a permission problem, granted in Jamf Account, and the
// two vocabularies name different things — so the hint has to follow the path
// the request actually took.
func TestForbiddenHintFollowsTheAPIThatServedTheRequest(t *testing.T) {
	instance := forbiddenHint("GET", "/api/v1/categories")
	if !strings.Contains(instance, "API role") {
		t.Errorf("instance hint = %q, want it to name the API role", instance)
	}
	if strings.Contains(instance, "Jamf Account") {
		t.Errorf("instance hint = %q, must not send an instance credential to Jamf Account", instance)
	}

	gw := forbiddenHint("GET", "/pro/v1/categories")
	for _, want := range []string{"Jamf Account", "Categories: Read", "categories:read"} {
		if !strings.Contains(gw, want) {
			t.Errorf("gateway hint = %q, missing %q", gw, want)
		}
	}
	if strings.Contains(gw, "API role") {
		t.Errorf("gateway hint = %q, must not name a Jamf Pro API role", gw)
	}
}

// Classic paths are assembled at runtime from the resource plus whichever lookup
// is in play, so the hint has to survive a concrete id in the path.
func TestForbiddenHintResolvesAClassicLookupPath(t *testing.T) {
	got := forbiddenHint("PUT", "/proclassic/categories/id/3")
	if !strings.Contains(got, "categories:update") {
		t.Errorf("hint = %q, want the Classic update capability", got)
	}
}

// An endpoint with no recorded capability must still name the right console.
// Answering with the Jamf Pro wording there would be the same wrong answer as
// answering it for a known one.
func TestForbiddenHintFallsBackWithoutInventingAPermission(t *testing.T) {
	got := forbiddenHint("GET", "/pro/v1/no-such-endpoint")
	if !strings.Contains(got, "Jamf Account") {
		t.Errorf("hint = %q, want the gateway fallback", got)
	}
	if strings.Contains(got, ":read") {
		t.Errorf("hint = %q, invented a capability", got)
	}
}
