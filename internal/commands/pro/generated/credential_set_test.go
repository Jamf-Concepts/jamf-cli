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

// TestRefuseCredentialSetsMatchesEverySpellingOfAPath refuses a key the
// builder would send under another spelling of a credential path, and a key
// the path match cannot safely read at all.
func TestRefuseCredentialSetsMatchesEverySpellingOfAPath(t *testing.T) {
	paths := []string{"password", "basicAuthCredentials.password", "lapsUserPasswordList[].password"}
	for _, set := range []string{
		"pass-word=h", "pass_word=h", "basic_auth_credentials.password=h",
		" password=h", ".password=h", "basicAuthCredentials..password=h", "password[0]=h",
		`lapsUserPasswordList=[[{"password":"h"}]]`, `basic_auth_credentials={"pass_word":"h"}`,
	} {
		if err := refuseCredentialSets([]string{set}, paths); err == nil {
			t.Errorf("refuseCredentialSets(%q) = nil, want a refusal", set)
		}
	}
	if err := refuseCredentialSets([]string{"basicAuthCredentials.username=u"}, paths); err != nil {
		t.Errorf("a non-secret sibling was refused: %v", err)
	}
}
