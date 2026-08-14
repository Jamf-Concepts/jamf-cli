// Copyright 2026, Jamf Software LLC

// Package monolith splits a single consolidated Jamf Pro OpenAPI document
// (e.g. the one served at /api/schema/) into per-resource spec files that
// match the layout expected by the rest of the generator pipeline.
//
// Routing strategy: for each path in the monolith, look up the spec filename
// that currently owns it in specsDir. If found, route the path into that
// bucket. Otherwise fall back to a filename derived from the operation's
// first tag (with manual overrides in overrides.go).
//
// Output: a shared library file (_MonolithLibrary.yaml) plus one file per
// bucket. For each component referenced by the monolith, the splitter counts
// how many buckets reach it transitively:
//
//   - Exclusive (1 bucket): inlined into that bucket's components block so
//     the parser's resource.Schemas map contains it. Heuristics that scan
//     declared schemas (e.g. detectNameField) see the same field set an
//     upstream per-file spec would expose.
//   - Shared (2+ buckets): emitted to the library file; bucket refs to it are
//     rewritten to external `_MonolithLibrary.yaml#/...` form.
//
// This replicates the original per-file + shared-library layout used
// upstream, where each resource spec declared its own primary schemas
// locally and imported cross-resource definitions via external $refs.
package monolith

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iancoleman/strcase"
	"gopkg.in/yaml.v3"
)

// LibraryFilename is the shared components file emitted by Split. Contains
// "Library" so the parser's non-resource file skip logic ignores it during
// resource extraction.
const LibraryFilename = "_MonolithLibrary.yaml"

// Split reads an OpenAPI monolith (JSON or YAML) from monolithSource and
// writes per-resource spec files into specsDir. monolithSource may be a
// local file path OR an http(s):// URL; URLs are fetched anonymously.
// Existing *.yaml files at the root of specsDir are removed; subdirectories
// (classic/) are left untouched. Files listed in
// PreservedSpecs (and any library files they depend on) are also left in
// place.
//
// Returns the list of written file paths (sorted) and any routing warnings
// encountered (e.g. new paths that did not match the existing layout).
func Split(monolithSource, specsDir string) ([]string, []string, error) {
	doc, err := readDoc(monolithSource)
	if err != nil {
		return nil, nil, fmt.Errorf("reading monolith: %w", err)
	}

	paths, ok := asMap(doc["paths"])
	if !ok {
		return nil, nil, fmt.Errorf("monolith has no paths object")
	}

	layout, tagOwners, protectedPaths, err := buildLayout(specsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("building layout from existing specs: %w", err)
	}

	// Spec files that already exist in the layout. Their info.title stays
	// filename-derived even when they pick up new paths — otherwise a file
	// flips between filename- and tag-derived titles depending on whether that
	// particular sync happened to bring it a new endpoint, producing diff churn
	// with no semantic content.
	existingFiles := make(map[string]bool, len(layout))
	for _, filename := range layout {
		existingFiles[filename] = true
	}

	buckets := make(map[string]map[string]any) // filename → paths subset
	newFileTag := make(map[string]string)      // new filename → tag (for titles)
	warnings := []string{}

	for path, pi := range paths {
		if protectedPaths[path] {
			continue
		}
		tag := firstTagOfPathItem(pi)
		if tag != "" && DroppedTags[tag] {
			continue
		}

		filename := layout[path]
		if filename == "" {
			switch {
			case tag != "" && TagFilenameOverrides[tag] != "":
				filename = TagFilenameOverrides[tag]
			case tag != "" && tagOwners[tag] != "":
				// An existing file already owns other paths with this tag;
				// keep the new path with its siblings rather than fragmenting.
				filename = tagOwners[tag]
			case tag != "":
				filename = pascalSingular(tag) + ".yaml"
			default:
				filename = "Untagged.yaml"
			}
			warnings = append(warnings, fmt.Sprintf("new path %s (tag=%q) not in existing layout; routed to %s", path, tag, filename))
			if !existingFiles[filename] {
				newFileTag[filename] = tag
			}
		}

		if _, exists := buckets[filename]; !exists {
			buckets[filename] = map[string]any{}
		}
		buckets[filename][path] = pi
	}

	if err := wipeRootYAMLs(specsDir); err != nil {
		return nil, nil, fmt.Errorf("wiping existing specs: %w", err)
	}

	monoComponents, _ := asMap(doc["components"])

	// Compute transitive ref closures per bucket so we can classify components
	// as exclusive (used by a single bucket) or shared (used by 2+ buckets).
	// Exclusive components are inlined into their owning bucket to preserve
	// upstream-style locally-declared schemas (important for parser heuristics
	// such as detectNameField that only scan doc.Components.Schemas).
	bucketClosures := make(map[string]map[string]bool, len(buckets))
	refOwners := map[string]map[string]bool{} // ref → set of bucket names
	for filename, bucketPaths := range buckets {
		seed := map[string]bool{}
		collectRefs(bucketPaths, seed)
		closure := closureRefs(seed, monoComponents)
		bucketClosures[filename] = closure
		for r := range closure {
			if refOwners[r] == nil {
				refOwners[r] = map[string]bool{}
			}
			refOwners[r][filename] = true
		}
	}

	shared := map[string]bool{}
	for r, owners := range refOwners {
		if len(owners) > 1 {
			shared[r] = true
		}
	}

	libraryComps := buildComponents(shared, monoComponents)
	written := []string{}

	if len(libraryComps) > 0 {
		libraryPath := filepath.Join(specsDir, LibraryFilename)
		libraryComps = coerceExamples(libraryComps).(map[string]any)
		libraryDoc := map[string]any{
			"openapi": doc["openapi"],
			"info": map[string]any{
				"title":   "Jamf Pro API - Shared Components (monolith-derived)",
				"version": "1.0.0",
			},
			"components": libraryComps,
		}
		if err := writeYAML(libraryPath, libraryDoc); err != nil {
			return nil, nil, fmt.Errorf("writing library: %w", err)
		}
		written = append(written, libraryPath)
	}

	for filename, bucketPaths := range buckets {
		outPath := filepath.Join(specsDir, filename)
		title := "Jamf Pro API - " + strings.TrimSuffix(filename, ".yaml")
		if tag, ok := newFileTag[filename]; ok && tag != "" {
			title = "Jamf Pro API - " + tag
		}

		localRefs := map[string]bool{}
		for r := range bucketClosures[filename] {
			if !shared[r] {
				localRefs[r] = true
			}
		}
		localComps := buildComponents(localRefs, monoComponents)

		rewrittenPaths := rewriteSharedRefs(bucketPaths, shared, LibraryFilename)
		rewrittenPaths = coerceExamples(rewrittenPaths)
		if len(localComps) > 0 {
			localComps = rewriteSharedRefs(localComps, shared, LibraryFilename).(map[string]any)
			localComps = coerceExamples(localComps).(map[string]any)
		}

		out := map[string]any{
			"openapi": doc["openapi"],
			"info": map[string]any{
				"title":   title,
				"version": "0.0.1",
			},
			"paths": rewrittenPaths,
		}
		if servers, ok := doc["servers"]; ok {
			out["servers"] = servers
		}
		if len(localComps) > 0 {
			out["components"] = localComps
		}
		if err := writeYAML(outPath, out); err != nil {
			return nil, nil, fmt.Errorf("writing %s: %w", filename, err)
		}
		written = append(written, outPath)
	}

	sort.Strings(written)
	sort.Strings(warnings)
	return written, warnings, nil
}

// readDoc reads an OpenAPI document from a local path or http(s) URL.
// Format is chosen by extension for files, and by Content-Type (with a JSON
// fallback) for URLs.
func readDoc(source string) (map[string]any, error) {
	var (
		data   []byte
		format string // "json" or "yaml"
		err    error
	)

	if isHTTPURL(source) {
		data, format, err = fetchDoc(source)
	} else {
		data, err = os.ReadFile(source)
		if err == nil {
			format = formatFromExt(source)
		}
	}
	if err != nil {
		return nil, err
	}

	var raw any
	switch format {
	case "json":
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing JSON: %w", err)
		}
	default:
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
	}

	norm := normalizeKeys(raw)
	doc, ok := norm.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document root is not a mapping")
	}
	return doc, nil
}

// isHTTPURL reports whether s is an http or https URL.
func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// formatFromExt returns "json" if path ends in .json, else "yaml".
func formatFromExt(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return "json"
	}
	return "yaml"
}

// fetchDoc retrieves an OpenAPI document over HTTP(S) anonymously. Returns
// body bytes and a detected format ("json" when Content-Type contains "json",
// else "yaml") with a first-byte fallback for servers that mislabel JSON.
func fetchDoc(rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json, application/yaml;q=0.9, */*;q=0.1")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("GET %s returned HTTP %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response body: %w", err)
	}

	format := "yaml"
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "json"):
		format = "json"
	case strings.Contains(ct, "yaml"):
		format = "yaml"
	case len(body) > 0 && (body[0] == '{' || body[0] == '['):
		format = "json"
	}
	return body, format, nil
}

// normalizeKeys walks a decoded tree and converts any map[any]any
// (from yaml.v3's default decoding) into map[string]any, recursively.
func normalizeKeys(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprintf("%v", k)] = normalizeKeys(val)
		}
		return out
	case map[string]any:
		for k, val := range x {
			x[k] = normalizeKeys(val)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = normalizeKeys(item)
		}
		return x
	default:
		return v
	}
}

// buildLayout walks specsDir (root only, no subdirs) and returns:
//   - path → filename for every path declared by existing spec files
//   - tag → filename for the dominant owner of each tag (file with the most
//     ops carrying that tag). Used to keep new paths with their existing
//     siblings when a tag is already owned by a known file.
//
// The shared library file is skipped.
func buildLayout(specsDir string) (map[string]string, map[string]string, map[string]bool, error) {
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, map[string]string{}, map[string]bool{}, nil
		}
		return nil, nil, nil, err
	}

	layout := make(map[string]string)
	tagCounts := make(map[string]map[string]int) // tag → {filename: count}
	protected := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		doc, err := readDoc(filepath.Join(specsDir, name))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reading existing spec %s: %w", name, err)
		}
		paths, ok := asMap(doc["paths"])
		if !ok {
			continue
		}
		isPreserved := PreservedSpecs[name]
		for p, pi := range paths {
			if isPreserved {
				protected[p] = true
				continue
			}
			if _, already := layout[p]; !already {
				layout[p] = name
			}
			for _, tag := range allTagsOfPathItem(pi) {
				if tagCounts[tag] == nil {
					tagCounts[tag] = map[string]int{}
				}
				tagCounts[tag][name]++
			}
		}
	}

	tagOwners := make(map[string]string, len(tagCounts))
	for tag, counts := range tagCounts {
		best, bestCount := "", -1
		for name, n := range counts {
			// Tie-break alphabetically for deterministic behaviour.
			if n > bestCount || (n == bestCount && name < best) {
				best, bestCount = name, n
			}
		}
		tagOwners[tag] = best
	}
	return layout, tagOwners, protected, nil
}

// allTagsOfPathItem returns every tag declared across all operations of a
// path item. Includes duplicates so callers can weight ownership.
func allTagsOfPathItem(pi any) []string {
	m, ok := asMap(pi)
	if !ok {
		return nil
	}
	var tags []string
	for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"} {
		op, ok := asMap(m[method])
		if !ok {
			continue
		}
		arr, ok := op["tags"].([]any)
		if !ok {
			continue
		}
		for _, t := range arr {
			if s, ok := t.(string); ok && s != "" {
				tags = append(tags, s)
			}
		}
	}
	return tags
}

// firstTagOfPathItem returns the first tag declared on any operation under
// the given path item. Returns "" if none is set.
func firstTagOfPathItem(pi any) string {
	m, ok := asMap(pi)
	if !ok {
		return ""
	}
	for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"} {
		op, ok := asMap(m[method])
		if !ok {
			continue
		}
		tags, ok := op["tags"].([]any)
		if !ok || len(tags) == 0 {
			continue
		}
		if t, ok := tags[0].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// pascalSingular converts a kebab-case tag like "mobile-devices" into
// PascalCase with a best-effort singularisation ("MobileDevice"). The result
// is purely a filename seed for new tags — the parser re-derives the canonical
// CLI name from the filename independently.
func pascalSingular(tag string) string {
	return strcase.ToCamel(singularizeKebab(tag))
}

func singularizeKebab(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "sses"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && !strings.HasSuffix(s, "us"):
		return strings.TrimSuffix(s, "s")
	default:
		return s
	}
}

// collectRefs walks v and records every local "#/components/<cat>/<name>"
// $ref string into out. External refs are ignored.
func collectRefs(v any, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "$ref" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, "#/components/") {
					out[s] = true
				}
				continue
			}
			collectRefs(val, out)
		}
	case []any:
		for _, item := range x {
			collectRefs(item, out)
		}
	}
}

// closureRefs returns the set of every component ref reachable transitively
// from seed via $refs inside the monolith's components.
func closureRefs(seed map[string]bool, monoComponents map[string]any) map[string]bool {
	visited := make(map[string]bool)
	stack := make([]string, 0, len(seed))
	for r := range seed {
		stack = append(stack, r)
	}
	for len(stack) > 0 {
		ref := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[ref] {
			continue
		}
		visited[ref] = true
		val, ok := lookupRef(ref, monoComponents)
		if !ok {
			continue
		}
		nested := map[string]bool{}
		collectRefs(val, nested)
		for s := range nested {
			if !visited[s] {
				stack = append(stack, s)
			}
		}
	}
	return visited
}

// buildComponents assembles a components subset containing the supplied refs,
// keyed by category and then by name. Refs that cannot be resolved in the
// monolith are silently skipped.
func buildComponents(refs map[string]bool, monoComponents map[string]any) map[string]any {
	subset := make(map[string]map[string]any)
	for ref := range refs {
		rest := strings.TrimPrefix(ref, "#/components/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			continue
		}
		cat, name := parts[0], parts[1]
		val, ok := lookupRef(ref, monoComponents)
		if !ok {
			continue
		}
		if subset[cat] == nil {
			subset[cat] = map[string]any{}
		}
		subset[cat][name] = val
	}
	out := make(map[string]any, len(subset))
	for cat, m := range subset {
		cm := make(map[string]any, len(m))
		maps.Copy(cm, m)
		out[cat] = cm
	}
	return out
}

// lookupRef resolves a "#/components/<cat>/<name>" string against the given
// monolith components block.
func lookupRef(ref string, monoComponents map[string]any) (any, bool) {
	rest := strings.TrimPrefix(ref, "#/components/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return nil, false
	}
	catMap, ok := monoComponents[parts[0]].(map[string]any)
	if !ok {
		return nil, false
	}
	val, ok := catMap[parts[1]]
	return val, ok
}

// coerceExamples walks v and fixes `example` values whose underlying Go type
// does not match the sibling `type` declaration. The monolith is JSON, so an
// OpenAPI `type: string` field with `example: 3` round-trips as an integer
// and makes scaffold generators emit numeric literals. This pass coerces such
// examples back to strings (and the mirror cases for integer/number/boolean
// types that carry a stringified example).
func coerceExamples(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if t, ok := x["type"].(string); ok {
			if ex, has := x["example"]; has {
				x["example"] = coerceValueToType(ex, t)
			}
		}
		for k, val := range x {
			x[k] = coerceExamples(val)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = coerceExamples(item)
		}
		return x
	default:
		return v
	}
}

// coerceValueToType converts a primitive scalar into the shape implied by the
// target OpenAPI `type` string. Non-primitive values (maps/slices) and values
// that already match the target are returned unchanged.
func coerceValueToType(v any, t string) any {
	switch t {
	case "string":
		switch x := v.(type) {
		case string:
			return x
		case float64:
			if x == float64(int64(x)) {
				return strconv.FormatInt(int64(x), 10)
			}
			return strconv.FormatFloat(x, 'g', -1, 64)
		case int:
			return strconv.Itoa(x)
		case int64:
			return strconv.FormatInt(x, 10)
		case bool:
			return strconv.FormatBool(x)
		}
	case "integer":
		if s, ok := v.(string); ok {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return float64(n)
			}
		}
	case "number":
		if s, ok := v.(string); ok {
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return n
			}
		}
	case "boolean":
		if s, ok := v.(string); ok {
			if b, err := strconv.ParseBool(s); err == nil {
				return b
			}
		}
	}
	return v
}

// rewriteSharedRefs deep-walks v and rewrites any $ref that targets a shared
// ref into its external "<library>#/components/..." form, leaving refs to
// exclusive (inlined) components as local "#/components/..." strings.
func rewriteSharedRefs(v any, shared map[string]bool, libraryFile string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "$ref" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, "#/components/") && shared[s] {
					out[k] = libraryFile + s
					continue
				}
			}
			out[k] = rewriteSharedRefs(val, shared, libraryFile)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = rewriteSharedRefs(item, shared, libraryFile)
		}
		return out
	default:
		return v
	}
}

// wipeRootYAMLs deletes every *.yaml file directly in specsDir (not in any
// subdirectory). Subdirs like classic/ are preserved,
// as are any files listed in PreservedSpecs and any library/definitions/common
// files that preserved specs may reference.
func wipeRootYAMLs(specsDir string) error {
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(specsDir, 0o755)
		}
		return err
	}
	// Collect external .yaml refs from every preserved spec so the library
	// files they depend on survive the wipe.
	requiredLibs := map[string]bool{}
	for name := range PreservedSpecs {
		data, err := os.ReadFile(filepath.Join(specsDir, name))
		if err != nil {
			continue
		}
		for _, ref := range extractExternalYAMLRefs(string(data)) {
			requiredLibs[ref] = true
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".yaml") {
			continue
		}
		if PreservedSpecs[n] || requiredLibs[n] {
			continue
		}
		if err := os.Remove(filepath.Join(specsDir, n)); err != nil {
			return err
		}
	}
	return nil
}

// extractExternalYAMLRefs returns the basenames of any .yaml files referenced
// via $ref-style "File.yaml#/..." fragments in the given content.
func extractExternalYAMLRefs(s string) []string {
	var out []string
	seen := map[string]bool{}
	// Scan for tokens of the form <Name>.yaml# — cheap, no regex engine needed.
	for i := 0; i < len(s); i++ {
		end := strings.Index(s[i:], ".yaml#")
		if end < 0 {
			break
		}
		j := i + end
		start := j
		for start > 0 {
			c := s[start-1]
			if c != '_' && c != '-' && (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				break
			}
			start--
		}
		name := s[start:j] + ".yaml"
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = j + len(".yaml#") - 1
	}
	return out
}

// writeYAML marshals v into a sorted, deterministic YAML document and writes
// it to path (0o644, overwriting).
func writeYAML(path string, v any) error {
	node := toNode(v)
	buf := &strings.Builder{}
	enc := yaml.NewEncoder(stringWriter{buf})
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// stringWriter adapts a *strings.Builder to an io.Writer.
type stringWriter struct{ b *strings.Builder }

func (s stringWriter) Write(p []byte) (int, error) { return s.b.Write(p) }

// toNode builds a yaml.Node tree with all mapping keys sorted alphabetically,
// giving stable output across runs regardless of Go map iteration order.
func toNode(v any) *yaml.Node {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		n := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range keys {
			n.Content = append(
				n.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: k},
				toNode(x[k]),
			)
		}
		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range x {
			n.Content = append(n.Content, toNode(item))
		}
		return n
	case string:
		// Tag explicitly to avoid yaml.v3 re-inferring numeric/bool types for
		// all-digit or true/false-looking strings (e.g. example: "3").
		return &yaml.Node{Kind: yaml.ScalarNode, Value: x, Tag: "!!str"}
	case bool:
		val := "false"
		if x {
			val = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: "!!bool"}
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}
	case float64:
		// JSON decode produces float64 for all numbers. Render as integer when
		// the value is whole to avoid ".0" noise in spec output.
		if x == float64(int64(x)) {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", int64(x)), Tag: "!!int"}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%v", x), Tag: "!!float"}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", x), Tag: "!!int"}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", x), Tag: "!!int"}
	default:
		n := &yaml.Node{}
		_ = n.Encode(v)
		return n
	}
}

// asMap is a typed-assertion shortcut returning the map and whether the value
// was a map[string]any.
func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
