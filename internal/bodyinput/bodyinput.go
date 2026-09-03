// Copyright 2026, Jamf Software LLC

// Package bodyinput decodes a request body supplied as a file, accepting
// either JSON or YAML.
//
// It exists so the Platform and Security Cloud commands accept the same input
// formats the Pro generated commands do. The Pro template carries its own copy
// (normalizeInputToJSON in generator/parser/generator.go's registryTemplate)
// because generated code stands alone; these two products share this one, so
// the fallback ladder cannot drift between them.
package bodyinput

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Normalize decodes raw as JSON, falling back to YAML, and returns a value
// built from JSON-native types only (map[string]any, []any, string, float64,
// bool). YAML input is round-tripped through JSON rather than returned as
// yaml.v3 decoded it, because yaml.v3 produces types encoding/json refuses to
// marshal — a mapping with non-string keys decodes to map[any]any, and a
// timestamp scalar to time.Time. Returning those would move the failure from
// here to whichever transport marshals the body, where it reads as an internal
// error rather than as a malformed input file.
//
// The round trip alone cannot do that job, which is the trap this once fell
// into: json.Marshal is the call that refuses map[any]any, so handing it the
// value straight turned the two shapes it exists to fix into
// "re-marshaling YAML as JSON: json: unsupported type" — the malformed-input
// error, for a perfectly good YAML file. jsonSafe converts them first and the
// round trip then only settles number types. Go 1.27's encoding/json marshals
// map[any]any happily, so a developer on it sees none of this while CI on the
// toolchain go.mod declares fails; that is why the test pins the shapes rather
// than the error string.
//
// Input carrying no content is an error rather than a nil body. A nil body
// means "send no body" to every caller here, so an empty file, a file of YAML
// comments, or a literal null would otherwise make a write that sent nothing
// indistinguishable from one that was never given a --file at all.
func Normalize(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("input carries no content")
	}
	return v, nil
}

func decode(raw []byte) (any, error) {
	// Fast path: already JSON.
	var v any
	if json.Valid(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	// Shells interpret \n even inside single-quoted strings, so a JSON body
	// written by one can carry literal control characters inside a string
	// value, which is invalid JSON. Escape those and retry before falling
	// back to YAML, which would silently fold them into spaces.
	if fixed := escapeJSONStringLiterals(raw); json.Valid(fixed) {
		if err := json.Unmarshal(fixed, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var y any
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return nil, fmt.Errorf("input is not valid JSON or YAML: %w", err)
	}
	safe, err := jsonSafe(y)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(safe)
	if err != nil {
		return nil, fmt.Errorf("re-marshaling YAML as JSON: %w", err)
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("re-marshaling YAML as JSON: %w", err)
	}
	return v, nil
}

// jsonSafe rewrites the two yaml.v3 shapes encoding/json cannot marshal into
// ones it can: a mapping with any non-string key (map[any]any) and a timestamp
// scalar (time.Time). Everything else is walked through unchanged — the JSON
// round trip after this settles the numeric types, so there is nothing here to
// duplicate.
//
// A key is spelled with fmt.Sprint, which is what makes `80: http` reach the
// server as `"80"`. Nothing guards against a composite key — a mapping or
// sequence, which YAML permits and JSON has no spelling for — because yaml.v3
// refuses it before this is reached, with "yaml: invalid map key" naming the
// key; a guard here would be a branch no input can take.
func jsonSafe(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			sv, err := jsonSafe(val)
			if err != nil {
				return nil, err
			}
			out[k] = sv
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			sv, err := jsonSafe(val)
			if err != nil {
				return nil, err
			}
			out[fmt.Sprint(k)] = sv
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			sv, err := jsonSafe(val)
			if err != nil {
				return nil, err
			}
			out[i] = sv
		}
		return out, nil
	case time.Time:
		return x.Format(time.RFC3339Nano), nil
	default:
		return v, nil
	}
}

// escapeJSONStringLiterals escapes any U+0000-U+001F control character that
// appears inside a JSON string value.
func escapeJSONStringLiterals(data []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(data))
	inString := false
	escaped := false
	for _, b := range data {
		switch {
		case escaped:
			buf.WriteByte(b)
			escaped = false
		case b == '\\' && inString:
			buf.WriteByte(b)
			escaped = true
		case b == '"':
			inString = !inString
			buf.WriteByte(b)
		case inString && b <= 0x1f:
			fmt.Fprintf(&buf, `\u%04x`, b)
		default:
			buf.WriteByte(b)
		}
	}
	return buf.Bytes()
}
