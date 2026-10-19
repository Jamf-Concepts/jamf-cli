package commands

import "testing"

func TestIsRemoteAdHocPolicy(t *testing.T) {
	cases := map[string]bool{
		"2021-07-19 at 6:59 PM | jane.doe$@example.com | 1 Computer": true,
		"2020-12-09 at 8:56 AM | John.Smith$ | 1 Computer":           true,
		"2021-03-24 at 12:10 PM | localadmin | 12 Computers":         true,
		"2014-06-04 at 3:04PM | jssadmin | 1 Computer":               true,
		"Deploy Chrome":                                  false,
		"2021-07-19 at 6:59 PM | someone":                false,
		"Nightly 2021-07-19 at 6:59 PM | a | 1 Computer": false,
		"": false,
	}
	for name, want := range cases {
		if got := isRemoteAdHocPolicy(name); got != want {
			t.Errorf("isRemoteAdHocPolicy(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDropRemoteAdHocPolicies(t *testing.T) {
	items := []any{
		map[string]any{"id": 1.0, "name": "Deploy Chrome"},
		map[string]any{"id": 2.0, "name": "2021-07-19 at 6:59 PM | localadmin | 1 Computer"},
		"not-a-map",
	}
	kept, excluded := dropRemoteAdHocPolicies(items)
	if excluded != 1 || len(kept) != 2 {
		t.Fatalf("kept %d, excluded %d; want 2 and 1", len(kept), excluded)
	}
}
