// Copyright 2026, Jamf Software LLC

package platform

import "fmt"

// ApplyName reads the human-readable name out of a request body assembled by
// ReadBody, for the generated `apply` commands.
//
// apply's contract is that the input is the desired state and carries its own
// identity, so the name comes from the body rather than a flag: a --name flag
// beside it would be a second source of truth, and the two disagreeing has no
// correct resolution. That makes "the body has no name" a usage error worth a
// specific message — without one the failure surfaces further down as a create
// that the server rejects for a missing required field, or worse as an
// unintended create because an empty name resolved to nothing.
//
// The value must be a non-empty string. A name that arrives as a number or a
// null is rejected here rather than coerced: apply matches it against list
// results, and "1" matching an item whose name is the integer 1 is a
// coincidence, not a resolution.
func ApplyName(body any, field string) (string, error) {
	if body == nil {
		return "", fmt.Errorf("input required: use --from-file or pipe JSON or YAML to stdin")
	}
	obj, ok := body.(map[string]any)
	if !ok {
		return "", fmt.Errorf("apply requires a JSON object body, got %T", body)
	}
	raw, present := obj[field]
	if !present {
		return "", fmt.Errorf("input must include a %q field to identify which resource to apply", field)
	}
	name, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("input field %q must be a string, got %T", field, raw)
	}
	if name == "" {
		return "", fmt.Errorf("input field %q must not be empty", field)
	}
	return name, nil
}
