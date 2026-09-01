// Copyright 2026, Jamf Software LLC

package gateway_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
	"github.com/Jamf-Concepts/jamf-cli/generator/gateway"
	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
)

// specsDir locates the repo's specs from this package's directory.
const specsDir = "../../specs"

// gatewayOp is one request this CLI can send, in gateway form.
type gatewayOp struct{ method, path string }

// gatewayOps is every request this CLI can send, modern and Classic.
//
// The consolidation passes have to run, not just ParseSpec. Three spec files
// declare computers-inventory at v2, v3 and v4 and only one of them becomes a
// command; parsing alone counts all three, so a version this CLI cannot send is
// weighed as heavily as one it does — and the withdrawn versions are exactly
// what a gateway spec drop removes. Left out, the refusal count read 105 of 811
// where the shipped surface accounts for 46, and the ratio guard below fired on
// resources that are not commands.
//
// Only the passes that decide which resource survives or rewrite a path are
// replayed, in generator/main.go's order. The rest set names, columns and
// lookup fields, none of which a verdict reads.
func gatewayOps(t *testing.T) []gatewayOp {
	t.Helper()
	var out []gatewayOp

	specs, err := filepath.Glob(filepath.Join(specsDir, "*.yaml"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("no specs found under %s: %v", specsDir, err)
	}
	var resources []*parser.Resource
	for _, s := range specs {
		parsed, err := parser.ParseSpec(s)
		if err != nil {
			continue // a spec this generator cannot parse is not this test's subject
		}
		resources = append(resources, parsed...)
	}
	resources = parser.DeduplicateVersioned(resources)
	parser.ApplyNameOverrides(resources)
	parser.ApplyListDetailPaths(resources)
	parser.ApplyGetDetailPaths(resources)

	for _, r := range resources {
		for _, op := range r.Operations {
			out = append(out, gatewayOp{op.Method, gateway.NormalisePath(gateway.ProPrefix + op.Path)})
			// A get with --section keeps its own path and reaches the detail
			// endpoint otherwise, so both are requests this CLI sends.
			if op.Name == "get" && r.GetDetailPath != "" {
				out = append(out, gatewayOp{op.Method, gateway.NormalisePath(gateway.ProPrefix + r.GetDetailPath)})
			}
		}
	}

	cls, err := classic.ParseManifest(filepath.Join(specsDir, "classic", "resources.yaml"))
	if err != nil {
		t.Fatalf("parsing the classic manifest: %v", err)
	}
	for _, r := range cls {
		out = append(out, gatewayOp{"GET", gateway.ClassicPrefix + "/" + r.Path})
	}
	return out
}

// gatewayPaths is gatewayOps reduced to distinct paths, for the override checks
// that are about paths rather than requests.
func gatewayPaths(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, op := range gatewayOps(t) {
		if !seen[op.path] {
			seen[op.path] = true
			out = append(out, op.path)
		}
	}
	return out
}

// An override entry that stops matching anything is how a table like this goes
// stale, and staleness here is expensive in both directions: a probedUnserved
// entry keeps refusing a command that may since have been routed, and a
// forceServed entry silently stops suppressing the note it was added for.
// Nothing in a spec or a bundle announces either change, so the guard is a test.
func TestEveryOverrideStillMatchesACommandThisCLISends(t *testing.T) {
	paths := gatewayPaths(t)
	probed, served := gateway.OverrideKeys()

	for _, table := range []struct {
		name string
		keys []string
	}{
		{"probedUnserved", probed},
		{"forceServed", served},
	} {
		for _, key := range table.keys {
			// A key is either "{METHOD} {path}" or a bare path prefix.
			path := key
			if i := strings.IndexByte(key, ' '); i >= 0 {
				path = key[i+1:]
			}
			matched := false
			for _, p := range paths {
				if p == path || strings.HasPrefix(p, path+"/") {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("%s entry %q matches no path this CLI sends — remove it, or fix the path", table.name, key)
			}
		}
	}
}

// Every refusal must be able to say why. A refusal with no evidence to quote is
// the failure mode this whole mechanism exists to remove, pointing the other way:
// an operator told "not part of the published API" with nothing behind it has no
// more to go on than the gateway's bare 403.
func TestEveryRefusalCarriesItsEvidence(t *testing.T) {
	cov := loadCoverage(t)
	for _, op := range gatewayOps(t) {
		v := cov.Verdict(op.method, op.path)
		if v.Level != gateway.Unserved {
			continue
		}
		if v.Detail == "" {
			t.Errorf("%s %s is refused with no evidence to quote", op.method, op.path)
		}
		switch v.Basis {
		case gateway.BasisProbe, gateway.BasisUnpublished:
		default:
			t.Errorf("%s %s is refused with basis %q, which selects no wording", op.method, op.path, v.Basis)
		}
	}
}

// A manifest read wrongly does not error — it just resolves everything as
// absent, and every gateway profile then refuses the whole Pro namespace. The
// serviceSegment marker-substring bug is the precedent: an empty service
// silently dropped the namespace from every generated path with no warning.
//
// Judged per request rather than per path, because a POST-only endpoint is
// legitimately not served for GET.
func TestAlmostEveryRequestResolvesAsServed(t *testing.T) {
	cov := loadCoverage(t)
	ops := gatewayOps(t)
	var refused int
	for _, op := range ops {
		if cov.Verdict(op.method, op.path).Level == gateway.Unserved {
			refused++
		}
	}
	// 44 operations are refused today out of ~810. Allow headroom for a bundle
	// that withdraws more, but fail long before "the manifest is not matching".
	if refused > len(ops)/10 {
		t.Errorf("%d of %d requests are refused — more than 10%%, which usually means the manifest is not being matched", refused, len(ops))
	}
	if refused == 0 {
		t.Error("no request is refused at all — the manifest is present but contributing nothing")
	}
}

func loadCoverage(t *testing.T) *gateway.Coverage {
	t.Helper()
	cov, err := gateway.Load(filepath.Join(specsDir, gateway.CoverageFile))
	if err != nil {
		t.Fatalf("loading the coverage manifest: %v", err)
	}
	if cov == nil {
		t.Skip("no coverage manifest committed")
	}
	return cov
}
