// Copyright 2026, Jamf Software LLC

package monolith

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SubtreeSpec routes one branch of a path subtree into its own per-resource
// spec file. Prefix is the collection path the file owns; the longest matching
// prefix wins, so a parent and its children can both be declared.
type SubtreeSpec struct {
	Prefix      string
	Filename    string
	Title       string
	Description string
}

// ExtractSubtree derives per-resource spec files for one subtree of a
// consolidated Jamf Pro OpenAPI document, leaving every other file in specsDir
// alone.
//
// This exists because one Jamf Pro surface is published only on the gateway.
// App Installers sits under hiddenapi/ in jamf/jss, so the jss bundle and the
// instance's own /api/schema/ monolith both exclude it, and the specs this repo
// generated its commands from were reverse-engineered for exactly that reason.
// public-apis-oas#430 published all 23 operations into the gateway's Jamf Pro
// API spec on 2026-09-03, which is the SDK's api/pro_api.json — the same file
// gateway coverage is derived from. So the authoritative spec now arrives with
// the coverage sync, and these files are derived from it rather than maintained
// by hand.
//
// It is deliberately not Split. Split owns the whole of specsDir: it wipes every
// root *.yaml, routes every path in the document, and partitions components into
// a shared _MonolithLibrary.yaml. Handing it pro_api.json would regenerate all
// 164 specs from the gateway's version-filtered view of the Pro API and delete
// the commands that view has withdrawn. This walks one subtree, writes only the
// files it is given, and inlines each bucket's whole component closure so the
// files are self-contained — a shared library would entangle them with Split's
// wipe-and-regenerate contract, and PreservedSpecs' $ref scan with it.
//
// Three transforms turn a gateway-published operation into a Pro-direct one.
// Each is a property of the publication rather than of the endpoint, so it is
// applied here rather than corrected by hand afterwards:
//
//   - Header parameters are dropped. The gateway declares X-Tenant-Id on every
//     operation; it is the scope this CLI's own client stamps on a request
//     (client.setScopeHeader), never a flag a user supplies, and the parser
//     turns a declared parameter into one.
//   - x-required-privileges-legacy is promoted over x-required-privileges. A
//     Pro command's jamf:privileges annotation speaks Jamf Pro API-role prose
//     ("Read Mac Applications"); the gateway spec's x-required-privileges is
//     the GA capability vocabulary ("applications:read"), which reaches the
//     same command through specs/gateway/coverage.json as jamf:gateway-privileges.
//     Publishing the slug as the Pro privilege would send an operator to a
//     console where that grant does not exist — the same class of wrong answer
//     the two-vocabulary split in internal/privileges exists to prevent.
//   - x-action is stamped on any operation whose path is deeper than its
//     collection path and whose terminal segment is a literal. jss carries
//     x-action and the gateway publication drops it, and the parser needs it to
//     name an operation after its verb: without it POST .../version-update,
//     POST .../installation-retry (both forms) and POST .../deployments all
//     infer "create" and collide, and GET .../titles/{id}/versions collides
//     with the canonical get. Precedent for a GET carrying it is in the repo's
//     own specs (ComputerPrestageScopeV2, PatchSoftwareTitleConfigurations).
//
// Returns the files written (sorted) and any warnings — an operation with no
// legacy privilege list is reported rather than silently published with the
// gateway's slug.
func ExtractSubtree(source, specsDir, subtree string, specs []SubtreeSpec) ([]string, []string, error) {
	doc, err := readDoc(source)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", source, err)
	}
	paths, ok := asMap(doc["paths"])
	if !ok {
		return nil, nil, fmt.Errorf("%s has no paths object", source)
	}
	monoComponents, _ := asMap(doc["components"])

	byFile := make(map[string]map[string]any, len(specs))
	specByFile := make(map[string]SubtreeSpec, len(specs))
	for _, s := range specs {
		specByFile[s.Filename] = s
	}

	var warnings []string
	for path, pi := range paths {
		if path != subtree && !strings.HasPrefix(path, subtree+"/") {
			continue
		}
		owner, ok := longestPrefixOwner(path, specs)
		if !ok {
			// Loudly, not silently: a new family under this subtree is a spec
			// file somebody has to decide the name of, and dropping it would
			// lose an endpoint with nothing to notice.
			return nil, nil, fmt.Errorf("no SubtreeSpec owns %s — add one for its collection path", path)
		}
		item, ws := proDirectPathItem(path, pi, owner.Prefix, monoComponents)
		warnings = append(warnings, ws...)
		if byFile[owner.Filename] == nil {
			byFile[owner.Filename] = map[string]any{}
		}
		byFile[owner.Filename][path] = item
	}

	var written []string
	for filename, bucketPaths := range byFile {
		s := specByFile[filename]
		seed := map[string]bool{}
		collectRefs(bucketPaths, seed)
		comps := buildComponents(closureRefs(seed, monoComponents), monoComponents)

		out := map[string]any{
			"openapi": doc["openapi"],
			"info": map[string]any{
				"title":       s.Title,
				"description": s.Description,
				"version":     "0.0.1",
			},
			"paths": coerceExamples(bucketPaths),
		}
		if len(comps) > 0 {
			out["components"] = coerceExamples(comps).(map[string]any)
		}
		outPath := filepath.Join(specsDir, filename)
		if err := writeYAML(outPath, out); err != nil {
			return nil, nil, fmt.Errorf("writing %s: %w", filename, err)
		}
		written = append(written, outPath)
	}
	for _, s := range specs {
		if byFile[s.Filename] == nil {
			warnings = append(warnings, fmt.Sprintf("no path under %s — %s not written", s.Prefix, s.Filename))
		}
	}

	sort.Strings(written)
	sort.Strings(warnings)
	return written, warnings, nil
}

// longestPrefixOwner picks the spec whose Prefix is the longest match for path,
// so a declared parent (/v1/app-installers) does not swallow its children.
func longestPrefixOwner(path string, specs []SubtreeSpec) (SubtreeSpec, bool) {
	best := SubtreeSpec{}
	found := false
	for _, s := range specs {
		if path != s.Prefix && !strings.HasPrefix(path, s.Prefix+"/") {
			continue
		}
		if !found || len(s.Prefix) > len(best.Prefix) {
			best, found = s, true
		}
	}
	return best, found
}

// httpMethods are the operation keys of a path item. Anything else at that
// level (parameters, summary, servers) is not an operation.
var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// proDirectPathItem applies the three transforms described on ExtractSubtree to
// one path item, in place, and returns any warnings.
func proDirectPathItem(path string, pi any, collectionPath string, monoComponents map[string]any) (any, []string) {
	item, ok := asMap(pi)
	if !ok {
		return pi, nil
	}
	var warnings []string

	if params, ok := item["parameters"].([]any); ok {
		item["parameters"] = dropHeaderParams(params, monoComponents)
	}

	action := isActionPath(path, collectionPath)
	for _, method := range httpMethods {
		op, ok := asMap(item[method])
		if !ok {
			continue
		}
		delete(op, "security")

		if legacy, ok := op["x-required-privileges-legacy"]; ok {
			op["x-required-privileges"] = legacy
			delete(op, "x-required-privileges-legacy")
		} else if _, ok := op["x-required-privileges"]; ok {
			delete(op, "x-required-privileges")
			warnings = append(warnings, fmt.Sprintf("%s %s declares no x-required-privileges-legacy; published with no Jamf Pro privileges", strings.ToUpper(method), path))
		}

		if params, ok := op["parameters"].([]any); ok {
			op["parameters"] = dropHeaderParams(params, monoComponents)
		}

		if action {
			op["x-action"] = true
		}
	}
	return item, warnings
}

// isActionPath reports whether an operation's path is deeper than its
// collection path and ends in a literal segment — the shape jss marks
// x-action, and the shape the parser names after its terminal segment.
func isActionPath(path, collectionPath string) bool {
	if path == collectionPath || !strings.HasPrefix(path, collectionPath+"/") {
		return false
	}
	seg := path[strings.LastIndex(path, "/")+1:]
	return seg != "" && !strings.HasPrefix(seg, "{")
}

// dropHeaderParams removes header parameters, inline or $ref'd. A $ref is
// resolved against the document's own components so a header declared once and
// referenced everywhere (X-Tenant-Id) is dropped everywhere.
func dropHeaderParams(params []any, monoComponents map[string]any) []any {
	kept := make([]any, 0, len(params))
	for _, p := range params {
		pm, ok := asMap(p)
		if !ok {
			kept = append(kept, p)
			continue
		}
		if in, ok := pm["in"].(string); ok {
			if in == "header" {
				continue
			}
			kept = append(kept, p)
			continue
		}
		if ref, ok := pm["$ref"].(string); ok && refIsHeaderParam(ref, monoComponents) {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func refIsHeaderParam(ref string, monoComponents map[string]any) bool {
	const prefix = "#/components/parameters/"
	if !strings.HasPrefix(ref, prefix) {
		return false
	}
	declared, ok := asMap(monoComponents["parameters"])
	if !ok {
		return false
	}
	param, ok := asMap(declared[strings.TrimPrefix(ref, prefix)])
	if !ok {
		return false
	}
	in, _ := param["in"].(string)
	return in == "header"
}
