// Copyright 2026, Jamf Software LLC

package parser

import "testing"

func totalCountOp(params ...*Parameter) *Operation {
	return &Operation{
		Method:     "GET",
		Parameters: params,
		Responses: map[string]*Response{
			"200": {Schema: &Schema{Type: "object", Properties: map[string]*Property{
				"totalCount": {Name: "totalCount", Type: "integer"},
				"results":    {Name: "results", Type: "array"},
			}}},
		},
	}
}

func TestMaxPageSize(t *testing.T) {
	pageSize := func(name string, max int) *Parameter {
		return &Parameter{Name: name, In: "query", Type: "integer", Maximum: max}
	}

	tests := []struct {
		name string
		op   *Operation
		want int
	}{
		{
			// The 122 Jamf Pro paginated operations that declare no maximum.
			name: "totalCount envelope with no declared maximum takes the verified Pro cap",
			op:   totalCountOp(pageSize("page-size", 0)),
			want: ProPageSizeCap,
		},
		{
			// /v1/users. A declared maximum wins even though it is LOWER than
			// the family cap, which is the case that matters: an endpoint that
			// names its ceiling tends to enforce it by rejecting the request.
			name: "declared maximum wins over the family cap",
			op:   totalCountOp(pageSize("page-size", 1000)),
			want: 1000,
		},
		{
			// /v1/sites/{id}/objects and /inventory-preload. The response is not
			// a page, so nothing here is backed by the pagination verification.
			name: "raw-array response stays at the API default",
			op: &Operation{
				Method: "GET", Parameters: []*Parameter{pageSize("page-size", 0)},
				Responses: map[string]*Response{"200": {Schema: &Schema{Type: "array"}}},
			},
			want: ConservativePageSize,
		},
		{
			name: "deprecated pagesize spelling is read for its maximum too",
			op:   totalCountOp(pageSize("pagesize", 250)),
			want: 250,
		},
		{
			// A path parameter called page-size would be nonsense, but the
			// filter is what stops a body or path field from setting the cap.
			name: "only a query parameter can declare the ceiling",
			op:   totalCountOp(&Parameter{Name: "page-size", In: "path", Maximum: 5}),
			want: ProPageSizeCap,
		},
		{
			name: "no operation at all is the API default, never the cap",
			op:   nil,
			want: ConservativePageSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxPageSize(tt.op); got != tt.want {
				t.Errorf("MaxPageSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPageSizeFromSpec_OnlyRaisesOnADeclaredMaximum(t *testing.T) {
	declared := []*Parameter{{Name: "page-size", In: "query", Type: "integer", Maximum: 1000}}
	if got := PageSizeFromSpec(declared); got != 1000 {
		t.Errorf("declared maximum: got %d, want 1000", got)
	}

	// The Pro cap must not leak across to the Platform and Security Cloud
	// services: each is a separate backend, verified separately or not at all.
	undeclared := []*Parameter{{Name: "page-size", In: "query", Type: "integer"}}
	if got := PageSizeFromSpec(undeclared); got != ConservativePageSize {
		t.Errorf("undeclared maximum: got %d, want %d", got, ConservativePageSize)
	}
}

func TestReturnsTotalCountPage(t *testing.T) {
	if !ReturnsTotalCountPage(totalCountOp()) {
		t.Error("a {totalCount, results} 200 response should be recognised as a page")
	}

	// A response carrying only one half of the envelope is not one. The Pro
	// auto-pagination loop reads both fields, and a shape missing either falls
	// through to its print-the-body-as-is branch.
	half := &Operation{Method: "GET", Responses: map[string]*Response{
		"200": {Schema: &Schema{Type: "object", Properties: map[string]*Property{
			"results": {Name: "results", Type: "array"},
		}}},
	}}
	if ReturnsTotalCountPage(half) {
		t.Error("a response with results but no totalCount is not a totalCount page")
	}

	if ReturnsTotalCountPage(&Operation{Method: "GET"}) {
		t.Error("an operation documenting no success body is not a totalCount page")
	}
}

// TestPaginationHelpIsFilledIn covers the other half of issue 385: --page and
// --page-size rendered with NO help text at all, because the published spec
// describes neither, so nothing said that --page is zero-based or that --all
// does not take --page-size.
func TestPaginationHelpIsFilledIn(t *testing.T) {
	op := totalCountOp(
		&Parameter{Name: "page", In: "query", Type: "integer"},
		&Parameter{Name: "page-size", In: "query", Type: "integer"},
	)
	annotatePaginationParams(op)
	for _, p := range op.Parameters {
		if p.Description == "" {
			t.Errorf("parameter %q still has no help text", p.Name)
		}
	}

	// A description the spec DOES supply is left alone — upstream's wording is
	// better than ours wherever it exists.
	kept := totalCountOp(&Parameter{Name: "page", In: "query", Type: "integer", Description: "spec wording"})
	annotatePaginationParams(kept)
	if kept.Parameters[0].Description != "spec wording" {
		t.Errorf("overwrote the spec's own description with %q", kept.Parameters[0].Description)
	}
}
