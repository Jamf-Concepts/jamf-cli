// Copyright 2026, Jamf Software LLC

// Package gateway derives, loads and interprets the Jamf Platform gateway's
// published Jamf Pro and Classic API surface.
//
// The gateway does not expose every Jamf Pro endpoint, and its refusals are not
// self-describing: an unrouted path answers 403 BAD_PERMISSIONS — byte for byte
// what a missing API-role privilege answers — so `pro app-installer-titles list`
// on a platform profile used to send an operator hunting for a grant that could
// not help.
//
// The source is jamfplatform-go-sdk's published api/, the same place
// specs/platform/ comes from:
//
//	pro_api.json                          the Jamf Pro API as published ON the
//	                                      gateway (servers: {region}.api.jamfcloud.com/pro)
//	classic_api_resource_documentation.json  the Classic API, likewise (/proclassic)
//
// Both are complete as of SDK adb8d7b, which whitelisted the remaining 38 jpapi
// paths: 528 and 273 paths, method-for-method identical to the gateway's own
// spec drops. Each operation also carries x-required-privileges in the gateway
// scope vocabulary (`categories:read`, `device-actions:execute`), which is what
// makes the SDK sufficient on its own — see the note on Scopes below.
//
// These two specs are deliberately NOT in PLATFORM_SDK_SPECS. They describe Jamf
// Pro APIs this repo already generates from specs/*.yaml, and feeding them to the
// platform generator would emit a second set of Pro commands from gateway paths.
// Only presence, method and scope are taken.
package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Source spec filenames, relative to the drop directory. They match the SDK's
// api/ filenames exactly, so a refresh is a copy with no mapping to keep in step
// — the same rule specs/platform/ follows.
const (
	// ProSpecFile is exported because it is not only a coverage source: it is
	// also the only published spec carrying the App Installers surface, which
	// generator/monolith derives specs/AppInstaller*.yaml from in the same run.
	ProSpecFile     = "pro_api.json"
	classicSpecFile = "classic_api_resource_documentation.json"
)

// SourceFiles is the set sync-gateway-coverage copies out of an SDK checkout.
var SourceFiles = []string{ProSpecFile, classicSpecFile}

// Gateway path prefixes. A caller-facing Jamf Pro path is rewritten onto one of
// these by client.rewritePathForGateway before it is sent, so the manifest is
// keyed the same way — that is the form the runtime has in hand, and it keeps the
// Pro and Classic namespaces from colliding in one table.
const (
	ProPrefix     = "/pro"
	ClassicPrefix = "/proclassic"
)

// CoverageFile is the committed manifest's path, relative to the specs dir.
const CoverageFile = "gateway/coverage.json"

// paramRE matches an OpenAPI path parameter. Parameter *names* differ freely
// between the gateway's specs and this repo's ({id} vs {computerId} vs
// {policyId}) without meaning anything, so every path is normalised to a
// positional {} before it is compared or stored.
var paramRE = regexp.MustCompile(`\{[^}]+\}`)

// NormalisePath replaces every path parameter with a positional {}.
func NormalisePath(p string) string { return paramRE.ReplaceAllString(p, "{}") }

// Coverage is the committed manifest. Written by Extract, read by Load.
type Coverage struct {
	Note    string  `json:"_note"`
	Sources Sources `json:"sources"`

	// Spec maps a gateway path (ProPrefix/ClassicPrefix + normalised path) to the
	// methods the published spec declares for it. This is the whole basis of a
	// verdict: declared is served, undeclared is refused.
	Spec map[string][]string `json:"spec"`

	// Scopes maps the same key to method → the gateway scopes the operation
	// requires, from x-required-privileges.
	//
	// Nothing consumes these yet. They are the missing input for the Platform 403
	// privilege hint (only Pro appends privilege names to a 403 today), and they
	// are why this package no longer needs the GitOps bundle: the bundle's
	// _permissions/routes.yaml carried exactly this map, and as of SDK adb8d7b the
	// two agree entry for entry — 1352 operations, zero disagreements, none on
	// either side alone.
	Scopes map[string]map[string][]string `json:"scopes"`
}

// Sources records what the manifest was derived from, so a stale copy is
// visible without re-fetching the specs.
type Sources struct {
	Pro     SpecSource `json:"pro"`
	Classic SpecSource `json:"classic"`
	// SDKCommit is the jamfplatform-go-sdk revision the specs were copied from,
	// when the sync was given an SDK checkout. Empty when derived straight from
	// the drop directory, which is the case a developer hits after editing a spec
	// by hand — better empty than confidently wrong.
	SDKCommit string `json:"sdkCommit,omitempty"`
}

// SpecSource identifies one of the source specs.
type SpecSource struct {
	File       string `json:"file"`
	Title      string `json:"title"`
	Version    string `json:"version"`
	Paths      int    `json:"paths"`
	Operations int    `json:"operations"`
}

const coverageNote = "The Jamf Pro and Classic API surface the Jamf Platform gateway publishes, plus the gateway scope each operation requires. " +
	"Derived by `make sync-gateway-coverage` from jamfplatform-go-sdk's api/pro_api.json and api/classic_api_resource_documentation.json — " +
	"see generator/gateway/coverage.go. Paths are gateway-form (/pro, /proclassic) with every path parameter normalised to {}. " +
	"NOT a command source: nothing here is fed to ParseSpec, and these two specs are deliberately absent from PLATFORM_SDK_SPECS."

// Extract reads the two specs under srcDir and derives the manifest. sdkCommit
// is optional provenance, recorded verbatim.
func Extract(srcDir, sdkCommit string) (*Coverage, error) {
	cov := &Coverage{
		Note:   coverageNote,
		Spec:   map[string][]string{},
		Scopes: map[string]map[string][]string{},
	}
	cov.Sources.SDKCommit = sdkCommit

	for _, s := range []struct {
		file   string
		prefix string
		dst    *SpecSource
	}{
		{ProSpecFile, ProPrefix, &cov.Sources.Pro},
		{classicSpecFile, ClassicPrefix, &cov.Sources.Classic},
	} {
		src, err := parseSpec(filepath.Join(srcDir, s.file), s.prefix, cov)
		if err != nil {
			return nil, err
		}
		src.File = s.file
		*s.dst = src
	}

	if len(cov.Spec) == 0 {
		return nil, fmt.Errorf("the specs under %s declared no paths", srcDir)
	}
	return cov, nil
}

// openAPIDoc is the sliver of an OpenAPI document this package reads.
// Deliberately minimal: taking more would invite someone to generate from it.
//
// A path item's keys are not all operations — OpenAPI allows a path-level
// `parameters` array and `$ref`/`summary`/`description` beside the methods — so
// the values stay raw and only the keys that name an HTTP method are decoded.
// Typing them all as operations fails outright on the first path that declares
// shared parameters (/v1/log-flushing/task/{id}, in this spec).
type openAPIDoc struct {
	Info struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

// operation is the one field a coverage verdict needs off an operation object.
type operation struct {
	Privileges []string `json:"x-required-privileges"`
}

var httpMethods = map[string]bool{
	"GET": true, "PUT": true, "POST": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

func parseSpec(path, prefix string, into *Coverage) (SpecSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SpecSource{}, fmt.Errorf("reading gateway spec: %w", err)
	}
	var doc openAPIDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return SpecSource{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	var ops int
	for p, item := range doc.Paths {
		key := prefix + NormalisePath(p)
		declared := into.Spec[key]
		for m, raw := range item {
			um := strings.ToUpper(m)
			if !httpMethods[um] {
				continue
			}
			var op operation
			if err := json.Unmarshal(raw, &op); err != nil {
				return SpecSource{}, fmt.Errorf("parsing %s %s in %s: %w", um, p, path, err)
			}
			ops++
			declared = appendUnique(declared, um)
			if len(op.Privileges) == 0 {
				// Not every operation needs a scope: 44 Jamf Pro endpoints are
				// unauthenticated (/v1/health-check, /v1/jamf-pro-version,
				// /v1/locales). An absent scope list is not an absent operation.
				continue
			}
			if into.Scopes[key] == nil {
				into.Scopes[key] = map[string][]string{}
			}
			scopes := append([]string(nil), op.Privileges...)
			sort.Strings(scopes)
			into.Scopes[key][um] = scopes
		}
		sort.Strings(declared)
		into.Spec[key] = declared
	}
	return SpecSource{
		Title:      doc.Info.Title,
		Version:    doc.Info.Version,
		Paths:      len(doc.Paths),
		Operations: ops,
	}, nil
}

func appendUnique(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

// CarryForwardProvenance copies provenance from a previously written manifest
// that this run could not determine for itself. Today that is the SDK revision:
// it describes where the *specs* came from, so re-deriving from the same
// unchanged specs must not lose it.
//
// Without this, any re-derivation without JAMFPLATFORM_SDK_PATH blanked the
// field, and verify-gateway-coverage then reported a stale manifest that was
// byte-identical apart from the provenance it had just erased. Same reasoning as
// the Protect backup _meta manifest carrying the previous run's inventory
// forward rather than blanking it.
//
// A nil prev is a no-op, so a first run records whatever it was given.
func CarryForwardProvenance(cov, prev *Coverage) {
	if cov == nil || prev == nil {
		return
	}
	if cov.Sources.SDKCommit == "" {
		cov.Sources.SDKCommit = prev.Sources.SDKCommit
	}
}

// Write marshals the manifest to path, creating the directory if needed.
func Write(cov *Coverage, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cov, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Load reads a manifest written by Write. A missing file is not an error: it
// returns (nil, nil), so a tree with no manifest generates commands with no
// gateway verdicts rather than failing — the manifest is committed, but the
// generator has to keep working for anyone who has not synced the specs.
func Load(path string) (*Coverage, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cov Coverage
	if err := json.Unmarshal(b, &cov); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cov, nil
}
