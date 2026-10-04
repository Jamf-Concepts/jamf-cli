// Copyright 2026, Jamf Software LLC

package parser

import "testing"

// TestOpTakesBodyFromFile pins which writes get a body --from-file: every
// body-carrying write except the ones whose --from-file means something else
// or that cannot take a pipe.
func TestOpTakesBodyFromFile(t *testing.T) {
	plain := &Resource{Name: "things"}
	body := &RequestBody{}
	cases := []struct {
		name string
		op   *Operation
		r    *Resource
		want bool
	}{
		{"create", &Operation{Method: "POST", Path: "/v1/things", Name: "create", RequestBody: body}, plain, true},
		{"update", &Operation{Method: "PUT", Path: "/v1/things/{id}", Name: "update", RequestBody: body}, plain, true},
		{"non-patch PATCH action", &Operation{Method: "PATCH", Path: "/v1/things/{id}/x", Name: "x", RequestBody: body}, plain, true},
		{"bodyless action", &Operation{Method: "POST", Path: "/v1/things/{id}/redeploy", Name: "redeploy"}, plain, false},
		{"merge-patch patch has its own", &Operation{Method: "PATCH", Path: "/v1/things/{id}", Name: "patch", RequestBody: body}, plain, false},
		{"multipart upload takes --file", &Operation{Method: "POST", Path: "/v1/things/{id}/upload", Name: "upload", RequestBody: &RequestBody{IsMultipart: true}}, plain, false},
		{"delete-multiple takes --ids", &Operation{Method: "POST", Path: "/v1/things/delete-multiple", Name: "delete-multiple", RequestBody: body}, plain, false},
		{"GET", &Operation{Method: "GET", Path: "/v1/things", Name: "list"}, plain, false},
	}
	for _, tc := range cases {
		if got := opTakesBodyFromFile(tc.op, tc.r); got != tc.want {
			t.Errorf("%s: opTakesBodyFromFile = %v, want %v", tc.name, got, tc.want)
		}
	}
}
