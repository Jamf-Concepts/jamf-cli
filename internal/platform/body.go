// Copyright 2026, Jamf Software LLC

package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/bodyinput"
)

// ReadBody assembles a JSON-marshallable request body from --from-file (a JSON
// or YAML file path, or piped stdin when the flag is absent) and --set overrides
// ("key=value", "nested.key=value"). When no body is supplied an empty object is
// the starting point. Set values are JSON-decoded when they look like JSON
// (true/false/null/number/[]/{}/"...") and treated as strings otherwise.
// Dot-separated keys descend into nested maps.
//
// Generated platform commands call this for POST/PATCH bodies. Returns nil
// when there's nothing to send (no input, no overrides) so callers can decide
// whether the op accepts an empty body or should error.
func ReadBody(file string, sets []string) (any, error) {
	var body any = map[string]any{}
	raw, err := readBodyInput(file)
	if err != nil {
		return nil, err
	}
	// A named --from-file must parse even when empty: an empty file decoding to a
	// nil body means "send no body", so a write that silently sent nothing would
	// be indistinguishable from one that was never given a file. An empty *pipe*
	// gets the opposite treatment, and for the reason readBodyInput gives.
	hasInput := file != "" || len(bytes.TrimSpace(raw)) > 0
	if hasInput {
		body, err = bodyinput.Normalize(raw)
		if err != nil {
			if file != "" {
				return nil, fmt.Errorf("parsing body file %s: %w", file, err)
			}
			return nil, fmt.Errorf("parsing body from stdin: %w", err)
		}
	}
	if len(sets) == 0 {
		if !hasInput {
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
		applySet(m, strings.Split(key, "."), parseSetValue(val))
	}
	return m, nil
}

// readBodyInput returns the raw request body bytes: the contents of --from-file
// when it names a path, otherwise piped stdin. Reading stdin is what makes
// `--scaffold | edit | apply` a pipeline rather than a detour through a temp
// file, and it is the shape the Pro generated commands (readApplyInput) and the
// Protect hand-written ones (readInput) have always had.
//
// An empty pipe returns no bytes rather than an error. "stdin is not a
// character device" is true of the empty stdin a CI runner hands every process,
// so it cannot be read as "a body was supplied" — a caller that requires a body
// (device-lifecycle purge, the --set-only creates) must still reject the empty
// case on its own terms. The 10MB ceiling matches readApplyInput.
func readBodyInput(file string) ([]byte, error) {
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading body file: %w", err)
		}
		return raw, nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	return raw, nil
}

// applySet walks path through m, creating intermediate maps as needed, and
// stores v at the leaf.
func applySet(m map[string]any, path []string, v any) {
	for i, segment := range path {
		if i == len(path)-1 {
			m[segment] = v
			return
		}
		next, ok := m[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[segment] = next
		}
		m = next
	}
}

// parseSetValue decodes typed JSON literals (booleans, numbers, null, arrays,
// objects, quoted strings) and falls back to a plain string for everything
// else. This mirrors the behaviour of the Pro generated --set parser.
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
