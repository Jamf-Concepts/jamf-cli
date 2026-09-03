// Copyright 2026, Jamf Software LLC

package parser

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/gateway"
)

// TestApplyGatewayAnnRendersTheVerdictWhenRefused is the unit half: apply is
// synthesized from list plus create-or-update, so its verdict has to come from
// those, and a refused apply must not also advertise a grant.
func TestApplyGatewayAnnRendersTheVerdictWhenRefused(t *testing.T) {
	ann := applyGatewayAnn

	served := &Resource{Operations: []*Operation{
		{Name: "list", GatewayPrivileges: []string{"categories:read"}},
		{Name: "create", GatewayPrivileges: []string{"categories:create"}},
		{Name: "update", GatewayPrivileges: []string{"categories:update"}},
	}}
	got := ann(served)
	want := `, "jamf:gateway-privileges": "categories:create,categories:read,categories:update"`
	if got != want {
		t.Errorf("served apply annotation = %q, want %q", got, want)
	}

	// The trap this closes: the withdrawn operation is the one apply starts
	// with, and the operations it kept still declare scopes. Annotating those
	// would advertise a grant that cannot make the command work — and, worse,
	// omitting jamf:gateway leaves checkAPIMatch nothing to refuse on, so apply
	// went out to the wire while every sibling verb was refused pre-flight.
	refused := &Resource{Operations: []*Operation{
		{Name: "list", GatewayLevel: "unserved", GatewayBasis: "unpublished", GatewayDetail: "GET /pro/v2/computer-groups/static-groups"},
		{Name: "create", GatewayPrivileges: []string{"computer-groups:create"}},
	}}
	got = ann(refused)
	for _, want := range []string{
		`"jamf:gateway": "unserved"`,
		`"jamf:gateway-basis": "unpublished"`,
		`"jamf:gateway-detail": "GET /pro/v2/computer-groups/static-groups"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("refused apply annotation %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "jamf:gateway-privileges") {
		t.Errorf("refused apply annotation %q names a privilege; a refused command must not advertise a grant that cannot make it work", got)
	}
}

// TestApplyGatewayVerdictNamesTheFirstRefusedOperation pins the send order:
// apply lists to resolve the name, then creates or replaces, so the reason a
// refusal carries is the earliest failure rather than whichever the map
// iteration reached first.
func TestApplyGatewayVerdictNamesTheFirstRefusedOperation(t *testing.T) {
	r := &Resource{Operations: []*Operation{
		{Name: "update", GatewayLevel: "unserved", GatewayDetail: "PUT"},
		{Name: "create", GatewayLevel: "unserved", GatewayDetail: "POST"},
		{Name: "list", GatewayLevel: "unserved", GatewayDetail: "GET"},
	}}
	v := applyGatewayVerdict(r)
	if v == nil {
		t.Fatal("applyGatewayVerdict returned nil for a wholly refused resource")
	}
	if v.GatewayDetail != "GET" {
		t.Errorf("verdict detail = %q, want the list operation's — apply sends it first", v.GatewayDetail)
	}

	// A verb apply never sends must not decide its verdict.
	unrelated := &Resource{Operations: []*Operation{
		{Name: "list"},
		{Name: "create"},
		{Name: "delete", GatewayLevel: "unserved"},
	}}
	if v := applyGatewayVerdict(unrelated); v != nil {
		t.Errorf("a refused delete decided apply's verdict (%+v); apply never sends it", v)
	}
}

// TestApplyCarriesTheSameVerdictAsItsSiblings is the live half, over the
// committed specs and gateway coverage manifest. Every generated `apply` on a
// resource whose list-and-write operations the gateway does not publish must be
// refused pre-flight the way its siblings are. It was not: `pro
// static-computer-groups list` and `create` were both refused while `apply` —
// the command a workflow is actually built out of — went out to the wire and
// answered a bare 500.
func TestApplyCarriesTheSameVerdictAsItsSiblings(t *testing.T) {
	resources := liveModernResourcesWithGatewayVerdicts(t)

	var whollyRefused int
	for _, r := range resources {
		if !shouldGenerateApply(r) {
			continue
		}
		var sent []*Operation
		for _, name := range applyGatewayOpNames {
			for _, op := range r.Operations {
				if op.Name == name {
					sent = append(sent, op)
				}
			}
		}
		allRefused := len(sent) > 0
		anyRefused := false
		for _, op := range sent {
			if op.GatewayLevel == "" {
				allRefused = false
			} else {
				anyRefused = true
			}
		}

		v := applyGatewayVerdict(r)
		if anyRefused && v == nil {
			t.Errorf("%s: apply carries no gateway verdict while an operation it sends is refused", r.Name)
		}
		if !anyRefused && v != nil {
			t.Errorf("%s: apply is refused (%s) while every operation it sends is served", r.Name, v.GatewayLevel)
		}
		if allRefused {
			whollyRefused++
			if v == nil || v.GatewayLevel != "unserved" {
				t.Errorf("%s: every operation apply sends is refused, but apply is not", r.Name)
			}
		}
	}
	if whollyRefused == 0 {
		t.Fatal("no wholly refused modern resource ships an apply — this test no longer exercises the case it was written for; check the coverage manifest loaded")
	}
}

// liveModernResourcesWithGatewayVerdicts replays the part of the generator
// pipeline that stamps gateway verdicts onto modern operations: parse, the
// consolidation passes that rewrite paths, then gateway.Apply over the
// committed manifest. Kept here rather than reaching for the generator binary
// because a verdict is a property of a parsed operation, and package main is
// not importable.
func liveModernResourcesWithGatewayVerdicts(t *testing.T) []*Resource {
	t.Helper()
	specsDir, err := filepath.Abs("../../specs")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	specs, err := filepath.Glob(filepath.Join(specsDir, "*.yaml"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("no specs found under %s: %v", specsDir, err)
	}
	var resources []*Resource
	for _, s := range specs {
		parsed, err := ParseSpec(s)
		if err != nil {
			continue // a spec this generator cannot parse is not this test's subject
		}
		resources = append(resources, parsed...)
	}
	resources = DeduplicateVersioned(resources)
	ApplyNameOverrides(resources)
	ApplyListDetailPaths(resources)
	ApplyGetDetailPaths(resources)

	coverage, err := gateway.Load(filepath.Join(specsDir, gateway.CoverageFile))
	if err != nil {
		t.Fatalf("loading the gateway coverage manifest: %v", err)
	}
	var ops []gateway.Op
	for _, r := range resources {
		for _, op := range r.Operations {
			ops = append(ops, gateway.Op{
				Method:      op.Method,
				GatewayPath: gateway.ProPrefix + op.Path,
				Set: func(v gateway.Verdict) {
					op.GatewayLevel, op.GatewayBasis, op.GatewayDetail = string(v.Level), string(v.Basis), v.Detail
					op.GatewayPrivileges = v.Scopes
				},
			})
		}
	}
	gateway.Apply(coverage, ops)
	return resources
}
