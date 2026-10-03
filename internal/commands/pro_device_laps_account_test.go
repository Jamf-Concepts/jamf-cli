// Copyright 2026, Jamf Software LLC

package commands

import (
	"strings"
	"testing"
)

func TestPickLAPSAccount_ResolvesExactlyOneAccount(t *testing.T) {
	cases := []struct {
		name     string
		accounts []lapsAccount
		userName string
		wantGUID string
		wantErr  []string
	}{
		{"one MDM account", []lapsAccount{{"g-1", "admin", "MDM"}, {"g-2", "jdoe", "USER"}}, "", "g-1", nil},
		{"no MDM account", []lapsAccount{{"g-1", "admin", "USER"}, {"g-2", "jdoe", "USER"}}, "", "", []string{"g-1", "g-2", "--user-name"}},
		{"two MDM accounts", []lapsAccount{{"g-1", "admin", "MDM"}, {"g-2", "admin2", "MDM"}}, "", "", []string{"g-1", "g-2"}},
		{"exact name beats a case variant", []lapsAccount{{"g-1", "Admin", "USER"}, {"g-2", "admin", "MDM"}}, "admin", "g-2", nil},
		{"two case variants of the name", []lapsAccount{{"g-1", "Admin", "USER"}, {"g-2", "ADMIN", "MDM"}}, "admin", "", []string{"g-1", "g-2"}},
		{"unknown name", []lapsAccount{{"g-1", "admin", "MDM"}}, "root", "", []string{"root", "g-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guid, err := pickLAPSAccount(tc.accounts, tc.userName)
			if tc.wantErr == nil {
				if err != nil || guid != tc.wantGUID {
					t.Fatalf("pickLAPSAccount = %q, %v; want %q", guid, err, tc.wantGUID)
				}
				return
			}
			if err == nil {
				t.Fatalf("pickLAPSAccount = %q with no error; want a refusal", guid)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %s", err, want)
				}
			}
		})
	}
}
