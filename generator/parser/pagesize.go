// Copyright 2026, Jamf Software LLC

package parser

import "fmt"

// Page sizes the generated auto-pagination loops request per call.
//
// `--all` sends the largest page an endpoint is known to honour and ignores
// `--page-size`, rather than passing the caller's value through. That is not a
// convenience: a Jamf Pro page-size above the server's ceiling is **silently
// clamped**, not rejected, and the loop's termination test is
// `len(results) < pageSize`. So `--all --page-size 2500` against 2601
// departments gets 2000 rows back on page 0, reads 2000 < 2500 as "that was
// the last page", and reports 2000 records with no error and no warning
// (wire-checked against the gateway 2026-09-18, which also confirmed the
// server offsets by the clamped size, so even a loop that kept going would
// have to know the real ceiling to walk the collection). Choosing the page
// size from the endpoint rather than from the command line is what keeps that
// unreachable, and it costs the caller nothing — `--page-size` still means
// what it says on a single page (`--all=false`), clamped to the same ceiling.
const (
	// ProPageSizeCap is the Jamf Pro API's pagination ceiling for the
	// {"totalCount": N, "results": [...]} pagination style.
	//
	// Wire-verified by the SDK 2026-08-10 (see defaultMaxPageSize in
	// jamfplatform-go-sdk/tools/generate/util.go): 33 sampled endpoints of that
	// style — ListGroupsV1/V2, AccountsV1, EnrollmentAccessGroupV3 and 29
	// History endpoints across unrelated resource types — all clamped at
	// exactly 2000, silently, with no exceptions. Re-confirmed here on
	// /v1/departments through the platform gateway 2026-09-18: page-size 2001,
	// 2500, 5000 and 20000 each returned exactly 2000 of 2601 rows.
	ProPageSizeCap = 2000

	// ConservativePageSize is the page size for an endpoint whose ceiling is
	// neither declared by its spec nor covered by the verification above. It is
	// the API's own default, so it is the one value guaranteed to be honoured.
	ConservativePageSize = 100
)

// MaxPageSize returns the page size an `--all` walk of op should request per
// page.
//
// Three sources, in order of how much they are trusted:
//
//  1. The spec's own `maximum` on the page-size parameter. Per-endpoint and
//     authoritative where it exists — and it is the only source that can be
//     LOWER than the family default, which matters because a declared ceiling
//     is the case where the server tends to reject rather than clamp
//     (devices/v1 answers 400 above 1000).
//  2. ProPageSizeCap, for the {totalCount, results} pagination style the
//     verification above covers. That is what the Jamf Pro API's paginated
//     endpoints are, 122 of the 135 paginated operations in the published
//     spec.
//  3. ConservativePageSize for everything else — the two raw-array endpoints
//     (/inventory-preload, /v1/sites/{id}/objects), whose response is not a
//     page at all, and any future shape. A page size is only worth raising
//     where the raise is backed by evidence about that endpoint; guessing high
//     is the failure mode this whole file exists to prevent.
func MaxPageSize(op *Operation) int {
	if op == nil {
		return ConservativePageSize
	}
	if declared := DeclaredPageSizeMax(op.Parameters); declared > 0 {
		return declared
	}
	if ReturnsTotalCountPage(op) {
		return ProPageSizeCap
	}
	return ConservativePageSize
}

// DeclaredPageSizeMax returns the spec-declared maximum on the page-size query
// parameter, 0 when the spec declares none. All four spellings in use across the
// Jamf specs are accepted: the Jamf Pro API's "page-size" and deprecated
// "pagesize", and the Security Cloud Risk API's camelCase "pageSize".
func DeclaredPageSizeMax(params []*Parameter) int {
	for _, p := range params {
		if p == nil || p.In != "query" || !isPageSizeParamName(p.Name) {
			continue
		}
		if p.Maximum > 0 {
			return p.Maximum
		}
	}
	return 0
}

// PageSizeFromSpec returns the page size a Platform or Security Cloud
// pagination loop should request: the spec's declared maximum where there is
// one, ConservativePageSize otherwise.
//
// ProPageSizeCap deliberately does not apply here. It was verified against one
// backend — the Jamf Pro API's totalCount pagination — and each Platform
// namespace is a separate service with its own answer: devices/v1 rejects a
// page-size above 1000 with a 400 rather than clamping, AI Governance declares
// 500, audit 200 and 100, UEM Connect and the Risk API 100. Those are the
// endpoints that declare a maximum, and the declaration is what is trusted. For
// the rest, the Platform loop terminates on a short page, so a page size the
// server silently lowers would end the walk early — exactly the truncation the
// Pro side has wire evidence against and this side does not.
func PageSizeFromSpec(params []*Parameter) int {
	if declared := DeclaredPageSizeMax(params); declared > 0 {
		return declared
	}
	return ConservativePageSize
}

// ReturnsTotalCountPage reports whether op's success response is the
// {"totalCount": N, "results": [...]} envelope the Pro auto-pagination loop
// parses. False for a raw-array response, for an export POST that documents no
// JSON body, and for anything else.
func ReturnsTotalCountPage(op *Operation) bool {
	if op == nil {
		return false
	}
	for _, code := range []string{"200", "201"} {
		resp := op.Responses[code]
		if resp == nil || resp.Schema == nil || resp.Schema.Properties == nil {
			continue
		}
		if resp.Schema.Properties["totalCount"] != nil && resp.Schema.Properties["results"] != nil {
			return true
		}
	}
	return false
}

// HasPageSizeParam reports whether op exposes a page-size query parameter under
// the canonical spelling, which is the one the generated flag set binds to
// flagPageSize. False for /inventory-preload, the one paginated Pro operation
// that offers only the deprecated `pagesize` alias.
func HasPageSizeParam(op *Operation) bool {
	if op == nil {
		return false
	}
	for _, p := range op.Parameters {
		if p != nil && p.In == "query" && p.Name == "page-size" {
			return true
		}
	}
	return false
}

func isPageSizeParamName(name string) bool {
	switch name {
	case "page-size", "pagesize", "pageSize", "page_size":
		return true
	}
	return false
}

// annotatePaginationParams fills in help text for the pagination parameters,
// which the published Jamf Pro spec leaves undescribed — so `--page` and
// `--page-size` rendered with no help at all, and nothing said that `--page` is
// zero-based. Issue 385 was filed partly on the strength of that: `--page-size
// 20000 --page 1` reads as "everything in one page" and returns rows 2000-3999.
func annotatePaginationParams(op *Operation) {
	if op.Method != "GET" || !ReturnsTotalCountPage(op) {
		return
	}
	ceiling := MaxPageSize(op)
	for _, p := range op.Parameters {
		if p == nil || p.In != "query" || p.Description != "" {
			continue
		}
		switch {
		case p.Name == "page":
			p.Description = "Page to return, zero-based; setting it returns that page alone instead of every page"
		case isPageSizeParamName(p.Name):
			p.Description = fmt.Sprintf("Results per page, max %d, for a single page only — --all ignores it and requests %d", ceiling, ceiling)
		}
	}
}
