// Copyright 2026, Jamf Software LLC

package classic

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iancoleman/strcase"
	"gopkg.in/yaml.v3"
)

// manifest is the top-level YAML structure.
type manifest struct {
	Resources []manifestResource `yaml:"resources"`
}

// manifestResource is a single entry in the YAML manifest.
type manifestResource struct {
	Name        string   `yaml:"name"`
	Path        string   `yaml:"path"`
	CLIName     string   `yaml:"cli_name"`
	Singular    string   `yaml:"singular"`
	Description string   `yaml:"description"`
	Operations  []string `yaml:"operations"`
	Lookups     []string `yaml:"lookups"`
	Scope       bool     `yaml:"scope"`
	NoSubsetPut bool     `yaml:"no_subset_put"`
	IDPath      string   `yaml:"id_path"`
	ListSubset  string   `yaml:"list_subset"`
	GroupsPath  string   `yaml:"groups_path"`
}

// ParseManifest reads the Classic API YAML manifest and returns a sorted
// slice of ClassicResource structs with all defaults applied.
func ParseManifest(path string) ([]ClassicResource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	var resources []ClassicResource
	for _, entry := range m.Resources {
		r, err := buildResource(entry)
		if err != nil {
			return nil, fmt.Errorf("resource %q: %w", entry.Name, err)
		}
		resources = append(resources, r)
	}

	sort.Slice(resources, func(i, j int) bool {
		return resources[i].CLIName < resources[j].CLIName
	})

	return resources, nil
}

// Default operations and lookups applied when the manifest entry omits them.
var (
	defaultOperations = []string{"list", "get", "create", "update", "delete"}
	defaultLookups    = []string{"id", "name"}
)

func buildResource(entry manifestResource) (ClassicResource, error) {
	if entry.Name == "" {
		return ClassicResource{}, fmt.Errorf("name is required")
	}
	if entry.Path == "" {
		return ClassicResource{}, fmt.Errorf("path is required")
	}

	cliName := entry.CLIName
	if cliName == "" {
		cliName = "classic-" + strcase.ToKebab(entry.Name)
	}

	singular := entry.Singular
	if singular == "" {
		singular = strings.TrimSuffix(entry.Name, "s")
	}

	// nil means omitted from YAML → apply defaults.
	// An explicit empty slice (operations: []) means intentionally no standard CRUD.
	operations := entry.Operations
	if operations == nil {
		operations = defaultOperations
	}

	lookups := entry.Lookups
	if lookups == nil {
		lookups = defaultLookups
	}

	idPath := entry.IDPath
	if idPath == "" {
		idPath = "id"
	}

	goName := strcase.ToCamel(cliName)

	return ClassicResource{
		Name:             entry.Name,
		Path:             entry.Path,
		CLIName:          cliName,
		GoName:           goName,
		Singular:         singular,
		Description:      entry.Description,
		Operations:       operations,
		Lookups:          lookups,
		HasScope:         entry.Scope,
		NoSubsetPut:      entry.NoSubsetPut,
		IDPath:           idPath,
		IsConfigProfile:  entry.Path == "osxconfigurationprofiles" || entry.Path == "mobiledeviceconfigurationprofiles",
		HasCustomPayload: entry.Path == "osxconfigurationprofiles",
		FileFields:       classicFileFields[entry.Path],
		ListSubset:       entry.ListSubset,
		GroupPath:        entry.GroupsPath,
	}, nil
}

// classicFileFields maps the manifest's "path" (e.g. "osxconfigurationprofiles")
// to the file-sourced fields the resource exposes on create/update/apply.
// See ClassicFileField for semantics.
var classicFileFields = map[string][]ClassicFileField{
	"osxconfigurationprofiles": {{
		Flag:                       "mobileconfig-file",
		XMLPath:                    "general/payloads",
		Encoding:                   "xml-cdata",
		Desc:                       "Path to a .mobileconfig file; contents are CDATA-wrapped into <general><payloads>",
		NameFallback:               "strip-ext",
		PreservePayloadIdentifiers: true,
	}},
	"mobiledeviceconfigurationprofiles": {{
		Flag:                       "mobileconfig-file",
		XMLPath:                    "general/payloads",
		Encoding:                   "xml-cdata",
		Desc:                       "Path to a .mobileconfig file; contents are CDATA-wrapped into <general><payloads>",
		NameFallback:               "strip-ext",
		PreservePayloadIdentifiers: true,
	}},
	"macapplications": {{
		Flag:          "appconfig-file",
		XMLPath:       "app_configuration/preferences",
		Encoding:      "xml-cdata",
		Desc:          "Path to an AppConfig plist; contents populate <app_configuration><preferences>",
		NameFallback:  "none",
		FetchMergePut: true,
	}},
	"mobiledeviceapplications": {{
		Flag:          "appconfig-file",
		XMLPath:       "app_configuration/preferences",
		Encoding:      "xml-cdata",
		Desc:          "Path to an AppConfig plist; contents populate <app_configuration><preferences>",
		NameFallback:  "none",
		FetchMergePut: true,
	}},
}
