// Copyright 2026, Jamf Software LLC

package security

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
// Generated security commands call this for PUT/POST bodies. Returns nil
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
		if err := applySet(m, strings.Split(key, "."), parseSetValue(val)); err != nil {
			return nil, fmt.Errorf("--set %q: %w", s, err)
		}
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
	// A stat failure is not "definitely no input". Collapsing the two sent no
	// body at all where the caller had piped one, with nothing reported at any
	// verbosity — the same class of silent wrong answer the empty-pipe rule
	// above exists to get right, in the branch the rule never covered.
	if err != nil {
		return nil, fmt.Errorf("checking stdin: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return nil, nil
	}
	// One byte past the ceiling, so hitting it is distinguishable from real
	// end-of-input. A LimitReader stopped at exactly the cap returns io.EOF,
	// which is what a complete body returns too, and a truncated JSON or YAML
	// document can still parse — so the cut lands as a body missing its
	// trailing fields, reported as success, on the resources whose own help
	// says the fields you omit are cleared.
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, stdinBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	if len(raw) > stdinBodyLimit {
		return nil, fmt.Errorf("stdin body exceeds %dMB; use --from-file for a larger payload", stdinBodyLimit>>20)
	}
	return raw, nil
}

// stdinBodyLimit caps a piped request body, matching readApplyInput's ceiling
// on the Pro side.
const stdinBodyLimit = 10 << 20

// applySet walks path through m, creating intermediate maps as needed, and
// stores v at the leaf. Errors rather than silently clobbering when a path
// segment already holds a non-object value (e.g. from --from-file or an earlier
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
