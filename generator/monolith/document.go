// Copyright 2026, Jamf Software LLC

// Package monolith reads a consolidated Jamf Pro OpenAPI document and writes it
// into specs/ as a single normalised file, plus the App Installer subtree that
// no consolidated document carries.
//
// It used to *split* that document into 165 per-resource files, and then the
// parser named each resource after the file it landed in. Those filenames were
// upstream's jss module names, appeared in no spec, and silently decided the
// command name, the endpoint-version family, whether a `-preview` tag reached a
// command, and whether the splitter could delete the file. Resource identity now
// comes from the URL paths (parser.ParseMonolith), so the split carried naming
// rather than information and the routing it needed is gone: the layout scan,
// the tag-derived fallback, the shared/exclusive component partitioning and the
// filename protection list with it.
//
// What survives is document handling — fetching, example coercion and
// deterministic YAML output. The last of those is the one real benefit the 165
// files gave: a 2 MB document written as sorted YAML still produces a readable
// `git diff` on a spec ingest.
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

	"gopkg.in/yaml.v3"
)

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

// NormalisedSpecFile is the filename Normalise writes the consolidated Jamf Pro
// API document to.
const NormalisedSpecFile = "JamfProAPI.yaml"

// Normalise reads a consolidated OpenAPI document from source — a local path or
// an http(s):// URL — and writes it into specsDir as one deterministic YAML
// file, returning the path written.
//
// Two transforms, and both are the reason this is not a plain copy:
//
//   - Example coercion. The document is JSON, so a `type: string` field with
//     `example: 3` round-trips as an integer and every scaffold built from it
//     emits a numeric literal. coerceExamples puts it back.
//   - Sorted, deterministic YAML. A 2 MB document written as the server sent it
//     is one unreadable line and every ingest is an unreviewable diff. Sorting
//     every mapping key means a spec ingest produces a diff someone can read,
//     which is the one thing the 165-file layout was genuinely good for.
func Normalise(source, specsDir string) (string, error) {
	doc, err := readDoc(source)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", source, err)
	}
	if _, ok := asMap(doc["paths"]); !ok {
		return "", fmt.Errorf("%s declares no paths object", source)
	}
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(specsDir, NormalisedSpecFile)
	if err := writeYAML(out, coerceExamples(doc)); err != nil {
		return "", fmt.Errorf("writing %s: %w", out, err)
	}
	return out, nil
}

// PruneStaleSpecs removes every *.yaml directly in specsDir that is not in
// keep, returning the names it removed. Subdirectories are untouched, and so is
// anything whose name begins with a dot — metadata such as .spec-version lives
// beside the specs.
//
// This is what turns the old 165-file layout into the two files that replace it,
// and it reports every removal rather than doing it quietly: a spec file
// disappearing used to rename commands, and even now that it cannot, an ingest
// that silently deleted 163 files would be indistinguishable from one that
// failed halfway.
func PruneStaleSpecs(specsDir string, keep []string) ([]string, error) {
	wanted := make(map[string]bool, len(keep))
	for _, k := range keep {
		wanted[filepath.Base(k)] = true
	}
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") || strings.HasPrefix(name, ".") {
			continue
		}
		if wanted[name] {
			continue
		}
		if err := os.Remove(filepath.Join(specsDir, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	sort.Strings(removed)
	return removed, nil
}
