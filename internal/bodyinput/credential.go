// Copyright 2026, Jamf Software LLC

package bodyinput

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RefuseCredentialSets returns an error for the first --set pair that would put
// a credential on the command line, where it lands in shell history, ps output
// and CI logs. credentialPaths are the dotted body paths the generator found to
// carry a secret, with "[]" for an array element. A pair is refused when its key
// is one of them, or when its value is a JSON object or array that carries one,
// since `--set deviceSyncAuth='{"clientSecret":"…"}'` exposes the secret just
// the same. The value is decoded whatever its first byte, so leading whitespace
// cannot hide one. Paths compare case-insensitively.
func RefuseCredentialSets(sets []string, credentialPaths []string) error {
	if len(credentialPaths) == 0 {
		return nil
	}
	refused := make(map[string]bool, len(credentialPaths))
	for _, p := range credentialPaths {
		refused[strings.ToLower(p)] = true
	}
	for _, s := range sets {
		key, raw, _ := strings.Cut(s, "=")
		hit := ""
		if refused[strings.ToLower(key)] {
			hit = key
		} else if v := any(nil); json.Unmarshal([]byte(raw), &v) == nil {
			hit = credentialIn(key, v, refused)
		}
		if hit != "" {
			return fmt.Errorf("--set %s: %s is a credential and cannot be passed as a flag value, where it would land in shell history, ps output and CI logs; put it in the request body and pass that with --from-file <file> or on stdin (--set can still override the other fields)", key, hit)
		}
	}
	return nil
}

func credentialIn(path string, v any, refused map[string]bool) string {
	if refused[strings.ToLower(path)] {
		return path
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if hit := credentialIn(path+"."+k, t[k], refused); hit != "" {
				return hit
			}
		}
	case []any:
		for _, child := range t {
			if hit := credentialIn(path+"[]", child, refused); hit != "" {
				return hit
			}
		}
	}
	return ""
}
