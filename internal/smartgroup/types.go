// Copyright 2026, Jamf Software LLC

// Package smartgroup curates a library of Jamf Pro smart-group templates
// admins can instantiate via the CLI. Criterion-name strings are sourced from
// the JSS server (see criteria.go for citations); the library is exercised
// against a live tenant via pro smart-group verify-templates.
package smartgroup

import "fmt"

// ParamSpec describes a single named parameter on a parameterized template.
// Templates have at most one ParamSpec; multi-param templates should be split
// into discrete variants.
type ParamSpec struct {
	Name        string // CLI flag name, e.g. "stalled-after"
	Type        string // "int" | "string" | "version"
	Default     any    // applied when caller omits the param; nil iff Required
	Description string // for --help
	Required    bool
}

// Template is one curated smart-group recipe in the library.
type Template struct {
	Slug        string      // e.g. "encryption/not-encrypted"
	Category    string      // e.g. "encryption"
	Description string      // one-line for table listings
	Params      []ParamSpec // zero or one entry
	Build       func(opts map[string]any) (SmartGroupRequest, error)
}

// SmartGroupRequest is the JSON body posted to /v2/computer-groups/smart-groups.
// We define our own type rather than importing the generated SmartComputerGroupV2
// so the smartgroup package can be tested in isolation.
type SmartGroupRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Criteria    []Criterion `json:"criteria,omitempty"`
	SiteID      string      `json:"siteId,omitempty"`
}

// Criterion is one row in SmartGroupRequest.Criteria; matches the
// SmartSearchCriterion schema from specs/_MonolithLibrary.yaml.
type Criterion struct {
	AndOr        string `json:"andOr"`
	Name         string `json:"name"`
	SearchType   string `json:"searchType"`
	Value        string `json:"value"`
	Priority     int    `json:"priority"`
	OpeningParen bool   `json:"openingParen"`
	ClosingParen bool   `json:"closingParen"`
}

// ResolveOpts validates and normalises a caller-supplied opts map against the
// template's ParamSpec list. Required params must be present. Type mismatches
// return an error. Defaults are filled in when omitted.
func (t Template) ResolveOpts(in map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(t.Params))
	for _, p := range t.Params {
		raw, present := in[p.Name]
		if !present {
			if p.Required {
				return nil, fmt.Errorf("template %s requires param --%s", t.Slug, p.Name)
			}
			if p.Default != nil {
				out[p.Name] = p.Default
			}
			continue
		}
		val, err := coerceTo(p.Type, raw)
		if err != nil {
			return nil, fmt.Errorf("template %s: param --%s: %w", t.Slug, p.Name, err)
		}
		out[p.Name] = val
	}
	return out, nil
}

func coerceTo(typ string, raw any) (any, error) {
	switch typ {
	case "int":
		switch v := raw.(type) {
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case float64:
			return int(v), nil
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
				return nil, fmt.Errorf("expected int, got %q", v)
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected int, got %T", raw)
		}
	case "string", "version":
		if s, ok := raw.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("expected string, got %T", raw)
	default:
		return nil, fmt.Errorf("unknown param type %q", typ)
	}
}
