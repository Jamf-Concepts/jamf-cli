package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/auth"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/commands/generated"
	"github.com/Jamf-Concepts/jamfpro-cli/internal/config"
)

// ---------------------------------------------------------------------------
// Seed definitions
// ---------------------------------------------------------------------------

const smokeTestPrefix = "_smoke-test"

type seedDef struct {
	Name       string // display name for test output
	CreatePath string // POST path
	Body       string // JSON body
	IsClassic  bool
	ListPath   string // GET path to find existing resources (for cleanup)
	WrapperKey string // classic list wrapper key
}

var seedDefs = []seedDef{
	// ── Classic API (XML bodies — Classic API requires Content-Type: text/xml) ──
	{
		Name: "classic-advanced-computer-searches", IsClassic: true,
		CreatePath: "/JSSResource/advancedcomputersearches/id/0",
		ListPath:   "/JSSResource/advancedcomputersearches", WrapperKey: "advancedcomputersearches",
		Body: `<?xml version="1.0" encoding="UTF-8"?><advanced_computer_search><name>_smoke-test-search</name></advanced_computer_search>`,
	},
	{
		Name: "classic-allowed-file-extensions", IsClassic: true,
		CreatePath: "/JSSResource/allowedfileextensions/id/0",
		ListPath:   "/JSSResource/allowedfileextensions", WrapperKey: "allowedfileextensions",
		Body: `<?xml version="1.0" encoding="UTF-8"?><allowed_file_extension><extension>.smoketest</extension></allowed_file_extension>`,
	},
	{
		Name: "classic-computer-ext-attrs", IsClassic: true,
		CreatePath: "/JSSResource/computerextensionattributes/id/0",
		ListPath:   "/JSSResource/computerextensionattributes", WrapperKey: "computer_extension_attributes",
		Body: `<?xml version="1.0" encoding="UTF-8"?><computer_extension_attribute><name>_smoke-test-ea</name><data_type>String</data_type><input_type><type>Text Field</type></input_type></computer_extension_attribute>`,
	},
	{
		Name: "classic-directory-bindings", IsClassic: true,
		CreatePath: "/JSSResource/directorybindings/id/0",
		ListPath:   "/JSSResource/directorybindings", WrapperKey: "directory_bindings",
		Body: `<?xml version="1.0" encoding="UTF-8"?><directory_binding><name>_smoke-test-binding</name><type>Active Directory</type><domain>smoke.test</domain></directory_binding>`,
	},
	{
		Name: "classic-disk-encryption-configs", IsClassic: true,
		CreatePath: "/JSSResource/diskencryptionconfigurations/id/0",
		ListPath:   "/JSSResource/diskencryptionconfigurations", WrapperKey: "disk_encryption_configurations",
		Body: `<?xml version="1.0" encoding="UTF-8"?><disk_encryption_configuration><name>_smoke-test-disk-enc</name><key_type>Individual</key_type></disk_encryption_configuration>`,
	},
	{
		Name: "classic-distribution-points", IsClassic: true,
		CreatePath: "/JSSResource/distributionpoints/id/0",
		ListPath:   "/JSSResource/distributionpoints", WrapperKey: "distribution_points",
		Body: `<?xml version="1.0" encoding="UTF-8"?><distribution_point><name>_smoke-test-dp</name><ip_address>10.99.99.1</ip_address><connection_type>SMB</connection_type><share_name>smoke</share_name><share_port>139</share_port></distribution_point>`,
	},
	{
		Name: "classic-dock-items", IsClassic: true,
		CreatePath: "/JSSResource/dockitems/id/0",
		ListPath:   "/JSSResource/dockitems", WrapperKey: "dock_items",
		Body: `<?xml version="1.0" encoding="UTF-8"?><dock_item><name>_smoke-test-dock</name><type>App</type><path>/Applications/Calculator.app</path></dock_item>`,
	},
	{
		Name: "classic-ibeacons", IsClassic: true,
		CreatePath: "/JSSResource/ibeacons/id/0",
		ListPath:   "/JSSResource/ibeacons", WrapperKey: "ibeacons",
		Body: `<?xml version="1.0" encoding="UTF-8"?><ibeacon><name>_smoke-test-ibeacon</name><uuid>E2C56DB5-DFFB-48D2-B060-D0F5A71096E0</uuid></ibeacon>`,
	},
	{
		Name: "classic-licensed-software", IsClassic: true,
		CreatePath: "/JSSResource/licensedsoftware/id/0",
		ListPath:   "/JSSResource/licensedsoftware", WrapperKey: "licensed_software",
		Body: `<?xml version="1.0" encoding="UTF-8"?><licensed_software><name>_smoke-test-licensed</name></licensed_software>`,
	},
	{
		Name: "classic-network-segments", IsClassic: true,
		CreatePath: "/JSSResource/networksegments/id/0",
		ListPath:   "/JSSResource/networksegments", WrapperKey: "network_segments",
		Body: `<?xml version="1.0" encoding="UTF-8"?><network_segment><name>_smoke-test-segment</name><starting_address>10.99.99.0</starting_address><ending_address>10.99.99.255</ending_address></network_segment>`,
	},
	{
		Name: "classic-restricted-software", IsClassic: true,
		CreatePath: "/JSSResource/restrictedsoftware/id/0",
		ListPath:   "/JSSResource/restrictedsoftware", WrapperKey: "restricted_software",
		Body: `<?xml version="1.0" encoding="UTF-8"?><restricted_software><name>_smoke-test-restricted</name><process_name>smoketest</process_name></restricted_software>`,
	},
	{
		Name: "classic-software-update-servers", IsClassic: true,
		CreatePath: "/JSSResource/softwareupdateservers/id/0",
		ListPath:   "/JSSResource/softwareupdateservers", WrapperKey: "software_update_servers",
		Body: `<?xml version="1.0" encoding="UTF-8"?><software_update_server><name>_smoke-test-sus</name><ip_address>10.99.99.2</ip_address><port>8088</port></software_update_server>`,
	},
	{
		Name: "classic-user-ext-attrs", IsClassic: true,
		CreatePath: "/JSSResource/userextensionattributes/id/0",
		ListPath:   "/JSSResource/userextensionattributes", WrapperKey: "user_extension_attributes",
		Body: `<?xml version="1.0" encoding="UTF-8"?><user_extension_attribute><name>_smoke-test-user-ea</name><data_type>String</data_type><input_type><type>Text Field</type></input_type></user_extension_attribute>`,
	},

	// ── Modern API ───────────────────────────────────────────────
	{
		Name:       "advanced-mobile-device-searches",
		CreatePath: "/v1/advanced-mobile-device-searches",
		ListPath:   "/v1/advanced-mobile-device-searches",
		Body:       `{"name":"_smoke-test-mobile-search","criteria":[]}`,
	},
	{
		Name:       "advanced-user-content-searches",
		CreatePath: "/v1/advanced-user-content-searches",
		ListPath:   "/v1/advanced-user-content-searches",
		Body:       `{"name":"_smoke-test-user-search","criteria":[]}`,
	},
}

// ---------------------------------------------------------------------------
// Raw HTTP helper for Classic API (needs Content-Type: text/xml)
// ---------------------------------------------------------------------------

// smokeBaseURL returns the Jamf Pro base URL and an auth provider for raw HTTP calls.
func smokeBaseURL(t *testing.T) (string, auth.Provider) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("cannot load config: %v", err)
	}
	url, provider, err := ResolveAuthForProfile(cfg, AuthParams{
		Profile:      os.Getenv("JAMF_PROFILE"),
		ServerURL:    os.Getenv("JAMF_URL"),
		Token:        os.Getenv("JAMF_TOKEN"),
		ClientID:     os.Getenv("JAMF_CLIENT_ID"),
		ClientSecret: os.Getenv("JAMF_CLIENT_SECRET"),
	})
	if err != nil {
		t.Skipf("cannot resolve auth: %v", err)
	}
	return url, provider
}

// classicPOST sends an XML POST to a Classic API endpoint.
func classicPOST(ctx context.Context, baseURL string, provider auth.Provider, path, xmlBody string) (*http.Response, error) {
	token, err := provider.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	fullURL := baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(xmlBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("Accept", "application/json")

	return http.DefaultClient.Do(req)
}

// ---------------------------------------------------------------------------
// TestSmoke_Seed — create one resource per seedable type
// ---------------------------------------------------------------------------

func TestSmoke_Seed(t *testing.T) {
	if os.Getenv("JAMF_SMOKE_TEST") == "" {
		t.Skip("set JAMF_SMOKE_TEST=1 to run smoke seed")
	}

	httpClient := smokeClient(t)
	baseURL, provider := smokeBaseURL(t)
	ctx := context.Background()

	var created, skipped, failed int

	for _, sd := range seedDefs {
		sd := sd
		t.Run(sd.Name, func(t *testing.T) {
			// Check if resource already exists
			if resourceExists(ctx, httpClient, sd) {
				t.Logf("already exists, skipping")
				skipped++
				return
			}

			reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if sd.IsClassic {
				// Classic API: raw HTTP with Content-Type: text/xml
				resp, err := classicPOST(reqCtx, baseURL, provider, sd.CreatePath, sd.Body)
				if err != nil {
					t.Logf("create failed: %v", err)
					failed++
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode >= 400 {
					t.Logf("create failed (HTTP %d): %s", resp.StatusCode, truncate(body, 200))
					failed++
					return
				}
				t.Logf("created (HTTP %d)", resp.StatusCode)
				created++
			} else {
				// Modern API: use the standard client (Content-Type: application/json)
				resp, err := httpClient.Do(reqCtx, "POST", sd.CreatePath, strings.NewReader(sd.Body))
				if err != nil {
					t.Logf("create failed: %v", err)
					failed++
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				t.Logf("created (HTTP %d): %s", resp.StatusCode, truncate(body, 100))
				created++
			}
		})
	}

	t.Logf("\nSeed summary: %d created, %d already existed, %d failed", created, skipped, failed)
}

// resourceExists checks if a _smoke-test resource already exists for this type.
func resourceExists(ctx context.Context, client generated.HTTPClient, sd seedDef) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := client.Do(reqCtx, "GET", sd.ListPath, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	return strings.Contains(string(body), smokeTestPrefix)
}

// ---------------------------------------------------------------------------
// TestSmoke_Cleanup — find and delete all _smoke-test resources
// ---------------------------------------------------------------------------

func TestSmoke_Cleanup(t *testing.T) {
	if os.Getenv("JAMF_SMOKE_TEST") == "" {
		t.Skip("set JAMF_SMOKE_TEST=1 to run smoke cleanup")
	}

	httpClient := smokeClient(t)
	ctx := context.Background()

	var deleted, failed int

	for _, sd := range seedDefs {
		sd := sd
		t.Run(sd.Name, func(t *testing.T) {
			ids := findSmokeResources(ctx, httpClient, sd)
			if len(ids) == 0 {
				t.Log("no _smoke-test resources found")
				return
			}

			for _, id := range ids {
				deletePath := buildDeletePath(sd, id)
				reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)

				resp, err := httpClient.Do(reqCtx, "DELETE", deletePath, nil)
				cancel()
				if err != nil {
					t.Logf("delete %s failed: %v", id, err)
					failed++
				} else {
					_ = resp.Body.Close()
					t.Logf("deleted %s", id)
					deleted++
				}
			}
		})
	}

	t.Logf("\nCleanup summary: %d deleted, %d failed", deleted, failed)
}

// findSmokeResources lists resources and returns IDs of any containing _smoke-test.
func findSmokeResources(ctx context.Context, client generated.HTTPClient, sd seedDef) []string {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := client.Do(reqCtx, "GET", sd.ListPath, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var items []map[string]interface{}

	if sd.IsClassic && sd.WrapperKey != "" {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(body, &wrapper) != nil {
			return nil
		}
		inner, ok := wrapper[sd.WrapperKey]
		if !ok {
			return nil
		}
		json.Unmarshal(inner, &items)
	} else {
		// Modern: try paginated, then array
		var paginated struct {
			Results []map[string]interface{} `json:"results"`
		}
		if json.Unmarshal(body, &paginated) == nil && len(paginated.Results) > 0 {
			items = paginated.Results
		} else {
			json.Unmarshal(body, &items)
		}
	}

	var ids []string
	for _, item := range items {
		name, _ := item["name"].(string)
		if strings.Contains(name, smokeTestPrefix) {
			id := extractID(item)
			if id != "" {
				ids = append(ids, id)
			}
		}
		// Also check "extension" field for allowed-file-extensions
		if ext, ok := item["extension"].(string); ok && strings.Contains(ext, "smoketest") {
			id := extractID(item)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func buildDeletePath(sd seedDef, id string) string {
	if sd.IsClassic {
		// Classic delete: /JSSResource/<path>/id/<id>
		// ListPath is /JSSResource/<path>, so append /id/<id>
		return sd.ListPath + "/id/" + id
	}
	// Modern delete: same as list path + /<id>
	return sd.ListPath + "/" + id
}
