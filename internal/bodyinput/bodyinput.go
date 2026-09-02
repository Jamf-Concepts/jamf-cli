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
	out, err := json.Marshal(y)
	if err != nil {
		return nil, fmt.Errorf("re-marshaling YAML as JSON: %w", err)
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("re-marshaling YAML as JSON: %w", err)
	}
	return v, nil
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
