// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// appleSchemaBaseURL is the raw content URL for Apple's device-management profile schemas.
	appleSchemaBaseURL = "https://raw.githubusercontent.com/apple/device-management/release/mdm/profiles/"

	// schemaFetchTimeout is the HTTP timeout for fetching a single schema file.
	schemaFetchTimeout = 10 * time.Second

	// schemaMaxSize is the maximum size of a schema file to read (1MB).
	schemaMaxSize = 1 << 20
)

// schemaFilenameOverrides maps payload types to their YAML filename on GitHub
// when the filename doesn't match the payload type directly.
var schemaFilenameOverrides = map[string]string{
	"com.apple.MCX.Accounts":       "com.apple.MCX(Accounts)",
	"com.apple.MCX.MobileAccounts": "com.apple.MCX(Mobililty)", // Apple's typo in their repo
	"com.apple.MCX.TimeServer":     "com.apple.MCX(TimeServer)",
	"com.apple.preference.users":   "com.apple.preferences.users",
}

// appleProfileSchema represents the top-level structure of an Apple profile YAML schema.
type appleProfileSchema struct {
	PayloadKeys []applePayloadKey `yaml:"payloadkeys"`
}

// applePayloadKey represents a single key definition in the schema.
type applePayloadKey struct {
	Key      string   `yaml:"key"`
	Type     string   `yaml:"type"`
	Default  *yamlVal `yaml:"default"`
	Presence string   `yaml:"presence"`
}

// yamlVal wraps a YAML value that may be any type (bool, int, float, string).
// We need a custom type because yaml.v3 doesn't distinguish between absent
// and null for basic types.
type yamlVal struct {
	value any
	set   bool
}

func (v *yamlVal) UnmarshalYAML(node *yaml.Node) error {
	v.set = true
	switch node.Tag {
	case "!!bool":
		var b bool
		if err := node.Decode(&b); err != nil {
			return err
		}
		v.value = b
	case "!!int":
		var i int64
		if err := node.Decode(&i); err != nil {
			return err
		}
		v.value = i
	case "!!float":
		var f float64
		if err := node.Decode(&f); err != nil {
			return err
		}
		v.value = f
	case "!!str":
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		v.value = s
	default:
		// For complex types (maps, sequences), decode as generic any
		var a any
		if err := node.Decode(&a); err != nil {
			return err
		}
		v.value = a
	}
	return nil
}

// SchemaDefaults holds the default values and required field info for an Apple
// profile schema. Defaults maps key name → default value (only keys that have a
// default in the schema). Required lists keys with presence: required.
type SchemaDefaults struct {
	Defaults map[string]any
	Required []string
}

// SchemaFetcher fetches and caches Apple profile schemas for default stripping.
type SchemaFetcher struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]*schemaResult
}

type schemaResult struct {
	defaults *SchemaDefaults
	err      error
}

// NewSchemaFetcher creates a SchemaFetcher with the given HTTP client.
// If client is nil, a default client with a 10-second timeout is used.
func NewSchemaFetcher(client *http.Client) *SchemaFetcher {
	if client == nil {
		client = &http.Client{Timeout: schemaFetchTimeout}
	}
	return &SchemaFetcher{
		client: client,
		cache:  make(map[string]*schemaResult),
	}
}

// FetchDefaults retrieves the Apple schema for the given payload type and
// returns the default values for its keys. Results are cached per payload type.
// Returns nil defaults (not an error) if the schema is unavailable.
func (f *SchemaFetcher) FetchDefaults(payloadType string) (*SchemaDefaults, error) {
	f.mu.Lock()
	if cached, ok := f.cache[payloadType]; ok {
		f.mu.Unlock()
		return cached.defaults, cached.err
	}
	f.mu.Unlock()

	defaults, err := f.fetchAndParse(payloadType)

	f.mu.Lock()
	f.cache[payloadType] = &schemaResult{defaults: defaults, err: err}
	f.mu.Unlock()

	return defaults, err
}

func (f *SchemaFetcher) fetchAndParse(payloadType string) (*SchemaDefaults, error) {
	filename := payloadType
	if override, ok := schemaFilenameOverrides[payloadType]; ok {
		filename = override
	}

	url := appleSchemaBaseURL + filename + ".yaml"

	resp, err := f.client.Get(url) //nolint:noctx // simple GET with timeout on client
	if err != nil {
		return nil, nil // network error — degrade gracefully
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no schema available for this type
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil // unexpected status — degrade gracefully
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, schemaMaxSize))
	if err != nil {
		return nil, nil // read error — degrade gracefully
	}

	return ParseSchemaDefaults(body)
}

// ParseSchemaDefaults extracts default values from a raw Apple profile YAML schema.
// Exported for testing.
func ParseSchemaDefaults(data []byte) (*SchemaDefaults, error) {
	var schema appleProfileSchema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parsing Apple schema: %w", err)
	}

	defaults := make(map[string]any)
	var required []string
	for _, key := range schema.PayloadKeys {
		if key.Default != nil && key.Default.set {
			defaults[key.Key] = key.Default.value
		}
		if key.Presence == "required" {
			required = append(required, key.Key)
		}
	}

	return &SchemaDefaults{Defaults: defaults, Required: required}, nil
}

// StripDefaultKeys removes keys from a payload entry whose values match the
// Apple schema defaults. Returns the count of keys stripped. The payloadType
// and payloadIdentifier keys are never stripped.
func StripDefaultKeys(entry map[string]any, defaults *SchemaDefaults) (int, []string) {
	if defaults == nil || len(defaults.Defaults) == 0 {
		return 0, nil
	}

	var stripped []string
	for key, val := range entry {
		// Never strip structural keys
		if key == "payloadType" || key == "payloadIdentifier" {
			continue
		}

		defVal, hasDefault := defaults.Defaults[key]
		if !hasDefault {
			continue
		}

		if valuesEqual(val, defVal) {
			delete(entry, key)
			stripped = append(stripped, key)
		}
	}
	return len(stripped), stripped
}

// MissingRequiredKeys returns the names of required keys that are absent from
// the payload entry. A payload missing required keys is invalid and should be
// removed — the DDM API will reject it regardless.
func MissingRequiredKeys(entry map[string]any, defaults *SchemaDefaults) []string {
	if defaults == nil || len(defaults.Required) == 0 {
		return nil
	}
	var missing []string
	for _, key := range defaults.Required {
		if _, ok := entry[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// valuesEqual compares a plist-decoded value (from JSON round-trip) with a
// YAML-decoded default value. Handles type coercion between int64/float64
// which is the main cross-format difference.
func valuesEqual(plistVal, yamlDefault any) bool {
	// Normalise numeric types: plist values come through as float64 after
	// JSON round-trip, YAML defaults may be int64 or float64.
	pNorm := normaliseNumeric(plistVal)
	yNorm := normaliseNumeric(yamlDefault)

	switch pv := pNorm.(type) {
	case bool:
		yv, ok := yNorm.(bool)
		return ok && pv == yv
	case float64:
		yv, ok := yNorm.(float64)
		return ok && pv == yv
	case string:
		yv, ok := yNorm.(string)
		return ok && pv == yv
	default:
		// Don't strip complex types (arrays, dicts) — too risky
		return false
	}
}

// payloadIsEmpty returns true if a payload entry contains only structural
// keys (payloadType, payloadIdentifier) and no actual settings.
func payloadIsEmpty(entry map[string]any) bool {
	for k := range entry {
		if k != "payloadType" && k != "payloadIdentifier" {
			return false
		}
	}
	return true
}

func normaliseNumeric(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	default:
		return v
	}
}
