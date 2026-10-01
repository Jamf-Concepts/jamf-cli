// Copyright 2026, Jamf Software LLC

package generated

import (
	"strings"
	"testing"
)

// TestRefuseCredentialSetsDecodesAsTheBuilderDoes refuses every container
// value parseJSONSetValue would decode: one led by JSON whitespace, and one
// carrying a trailing closer the decoder's More check lets through.
func TestRefuseCredentialSetsDecodesAsTheBuilderDoes(t *testing.T) {
	paths := []string{"gsxKeystore.keystorePassword", "users[].password"}
	var sets []string
	for _, lead := range []string{"", " ", "\t", "\n", "\r\n"} {
		sets = append(sets,
			"gsxKeystore="+lead+`{"keystorePassword":"x"}`,
			"users="+lead+`[{"password":"x"}]`,
		)
	}
	sets = append(sets, `gsxKeystore={"keystorePassword":"x"}}`, `users=[{"password":"x"}]]`)
	for _, set := range sets {
		_, raw, _ := strings.Cut(set, "=")
		if _, err := parseJSONSetValue(raw); err != nil {
			t.Fatalf("parseJSONSetValue(%q) = %v; the case no longer reaches the body builder", raw, err)
		}
		if err := refuseCredentialSets([]string{set}, paths); err == nil || !strings.Contains(err.Error(), "is a credential") {
			t.Errorf("refuseCredentialSets(%q) = %v, want a refusal", set, err)
		}
	}
}
