// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"strings"
	"testing"
)

func TestResolveGroupPlatformID_ResolvesExactlyOneGroup(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantID  string
		wantErr []string
	}{
		{"one exact match", `{"totalCount":1,"results":[{"groupPlatformId":"p-1","groupName":"Decom"}]}`, "p-1", nil},
		{"two groups share the name", `{"totalCount":2,"results":[{"groupPlatformId":"p-1","groupName":"Decom"},{"groupPlatformId":"p-2","groupName":"Decom"}]}`, "", []string{"p-1", "p-2"}},
		{"more matches than the page holds", `{"totalCount":3,"results":[{"groupPlatformId":"p-1","groupName":"Decom"}]}`, "", []string{"3 groups"}},
		{"the one result is another group", `{"totalCount":1,"results":[{"groupPlatformId":"p-9","groupName":"Decommissioned"}]}`, "", []string{"no COMPUTER group"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &flushMockClient{responses: map[string]flushMockResponse{"GET /v2/groups?page-size=": {200, tc.body}}}
			id, err := resolveGroupPlatformID(context.Background(), mock, "Decom", "COMPUTER")
			if tc.wantErr == nil {
				if err != nil || id != tc.wantID {
					t.Fatalf("resolveGroupPlatformID = %q, %v; want %q", id, err, tc.wantID)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveGroupPlatformID = %q with no error; want a refusal", id)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
