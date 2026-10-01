// Copyright 2026, Jamf Software LLC

package bodyinput

import (
	"strings"
	"testing"
)

func TestRefuseCredentialSets(t *testing.T) {
	paths := []string{"deviceSyncAuth.clientSecret", "users[].password", "token"}
	for _, tc := range []struct {
		name string
		sets []string
		hit  string
	}{
		{"dotted key", []string{"vendor=JAMF_PRO", "deviceSyncAuth.clientSecret=x"}, "deviceSyncAuth.clientSecret"},
		{"object value", []string{`deviceSyncAuth={"clientId":"id","clientSecret":"x"}`}, "deviceSyncAuth.clientSecret"},
		{"array of objects", []string{`users=[{"name":"a"},{"password":"x"}]`}, "users[].password"},
		{"case-insensitive key", []string{"TOKEN=x"}, "TOKEN"},
		{"numeric-looking secret", []string{"token=1234"}, "token"},
		{"empty value still names the field", []string{"token="}, "token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseCredentialSets(tc.sets, paths)
			if err == nil {
				t.Fatalf("RefuseCredentialSets(%q) = nil, want a refusal", tc.sets)
			}
			for _, want := range []string{tc.hit + " is a credential", "--from-file", "stdin"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}

	for _, sets := range [][]string{
		{"deviceSyncAuth.clientId=id", "vendor=JAMF_PRO"},
		{`deviceSyncAuth={"clientId":"id"}`},
		{`users=[{"name":"a"}]`},
		{"note={not json"},
	} {
		if err := RefuseCredentialSets(sets, paths); err != nil {
			t.Errorf("RefuseCredentialSets(%q) = %v, want nil", sets, err)
		}
	}
	if err := RefuseCredentialSets([]string{"token=x"}, nil); err != nil {
		t.Errorf("an operation with no credential paths refuses nothing, got %v", err)
	}
}
