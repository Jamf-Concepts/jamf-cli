// Copyright 2026, Jamf Software LLC

package security

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/bodyinput"
)

// ReadBody assembles a JSON-marshallable request body from --file (a JSON or
// YAML file path) and --set overrides ("key=value", "nested.key=value"). When file is
// empty an empty object is the starting point. Set values are JSON-decoded
// when they look like JSON (true/false/null/number/[]/{}/"...") and treated
// as strings otherwise. Dot-separated keys descend into nested maps.
//
// Generated security commands call this for PUT/POST bodies. Returns nil
// when there's nothing to send (no file, no overrides) so callers can decide
// whether the op accepts an empty body or should error.
func ReadBody(file string, sets []string) (any, error) {
	var body any = map[string]any{}
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading body file: %w", err)
		}
		body, err = bodyinput.Normalize(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing body file %s: %w", file, err)
		}
	}
	if len(sets) == 0 {
		if file == "" {
			return nil, nil
		}
		return body, nil
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("--set overrides require a JSON object body, got %T", body)
	}
	for _, s := range sets {
		key, val, found := strings.Cut(s, "=")
		if !found {
			return nil, fmt.Errorf("invalid --set %q: expected key=value", s)
		}
		if err := applySet(m, strings.Split(key, "."), parseSetValue(val)); err != nil {
			return nil, fmt.Errorf("--set %q: %w", s, err)
		}
	}
	return m, nil
}

// applySet walks path through m, creating intermediate maps as needed, and
// stores v at the leaf. Errors rather than silently clobbering when a path
// segment already holds a non-object value (e.g. from --file or an earlier
// --set) — matching the Pro generated --set parser's setNestedValue.
func applySet(m map[string]any, path []string, v any) error {
	for i, segment := range path {
		if i == len(path)-1 {
			m[segment] = v
			return nil
		}
		existing, present := m[segment]
		next, ok := existing.(map[string]any)
		if !ok {
			if present {
				return fmt.Errorf("cannot set nested key under non-object field %q", segment)
			}
			next = map[string]any{}
			m[segment] = next
		}
		m = next
	}
	return nil
}

// parseSetValue decodes typed JSON literals (booleans, numbers, null, arrays,
// objects, quoted strings) and falls back to a plain string for everything
// else. Intentionally more capable than the Pro/Platform generated --set
// parsers' parsePatchValue/parseValue, which parse only int64 before falling
// back to string (no float branch) — Security's --set has no merge-patch
// content-type constraint requiring that narrower behavior.
func parseSetValue(raw string) any {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	if len(raw) > 0 && (raw[0] == '[' || raw[0] == '{' || raw[0] == '"') {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v
		}
	}
	return raw
}
