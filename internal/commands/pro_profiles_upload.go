// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// ─── macOS Configuration Profiles ─────────────────────────────────────────────

func newMacOSProfileUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newProfileUploadCmd(cliCtx, profileUploadConfig{
		resourceName: "macOS configuration profile",
		apiPath:      "osxconfigurationprofiles",
		xmlRoot:      "os_x_configuration_profile",
	})
}

// ─── Mobile Device Configuration Profiles ─────────────────────────────────────

func newMobileProfileUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return newProfileUploadCmd(cliCtx, profileUploadConfig{
		resourceName: "mobile device configuration profile",
		apiPath:      "mobiledeviceconfigurationprofiles",
		xmlRoot:      "configuration_profile",
	})
}

// ─── Shared profile upload logic ──────────────────────────────────────────────

type profileUploadConfig struct {
	resourceName string // for display: "macOS configuration profile"
	apiPath      string // JSSResource path segment
	xmlRoot      string // XML root element name
}

func newProfileUploadCmd(cliCtx *registry.CLIContext, cfg profileUploadConfig) *cobra.Command {
	var (
		filePath    string
		profileName string
		flagYes     bool
	)

	cmd := &cobra.Command{
		Use:   "upload",
		Short: fmt.Sprintf("Upload a %s from a .mobileconfig file", cfg.resourceName),
		Long: fmt.Sprintf(`Upload a .mobileconfig file to Jamf Pro as a %s.

If a profile with the same name already exists, it is updated with the new
payload. Otherwise a new profile record is created.`, cfg.resourceName),
		Example: fmt.Sprintf(`  # Upload a profile
  jamf-cli pro %s upload --file wifi-settings.mobileconfig --name "Wi-Fi Settings"

  # Upload using the filename as the profile name
  jamf-cli pro %s upload --file wifi-settings.mobileconfig`,
			cliNameForAPI(cfg.apiPath), cliNameForAPI(cfg.apiPath)),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client := cliCtx.Client

			// Read the .mobileconfig file
			payload, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("reading profile file: %w", err)
			}

			if profileName == "" {
				profileName = fileBaseName(filePath)
			}

			fmt.Fprintf(os.Stderr, "Profile: %s (%d bytes)\n", profileName, len(payload))

			// Check for existing profile by name; also returns the existing
			// payload plist so we can preserve PayloadUUID/PayloadIdentifier
			// without a second API call.
			existingID, existingPayload, err := lookupClassicProfileByName(ctx, client, cfg.apiPath, cfg.xmlRoot, profileName)
			if err != nil {
				return err
			}

			if existingID != "" {
				if !flagYes {
					noInput, _ := cmd.Flags().GetBool("no-input")
					if noInput {
						return fmt.Errorf("profile %q already exists (id %s); use --yes to replace when --no-input is set", profileName, existingID)
					}
					fmt.Fprintf(os.Stderr, "Profile %q already exists (id %s). Replace? Type 'yes' to confirm: ", profileName, existingID)
					var confirm string
					_, _ = fmt.Scanln(&confirm)
					if confirm != "yes" {
						return fmt.Errorf("aborted")
					}
				}

				// Preserve PayloadUUID and PayloadIdentifier from the existing
				// profile so devices treat this as an update, not a new install.
				if len(existingPayload) > 0 {
					if modified, injectErr := profileconvert.InjectIdentifiers(payload, existingPayload); injectErr == nil {
						payload = modified
					}
				}

				// Update existing
				fmt.Fprintf(os.Stderr, "Updating existing profile (id %s)...\n", existingID)
				xmlBody := buildProfileXML(cfg.xmlRoot, profileName, string(payload))
				path := fmt.Sprintf("/JSSResource/%s/id/%s", cfg.apiPath, url.PathEscape(existingID))
				resp, err := client.Do(ctx, "PUT", path, bytes.NewReader(xmlBody))
				if err != nil {
					return fmt.Errorf("updating profile: %w", err)
				}
				_ = resp.Body.Close()
				fmt.Fprintf(os.Stderr, "Updated successfully\n")
			} else {
				// Create new
				fmt.Fprintf(os.Stderr, "Creating profile %q...\n", profileName)
				xmlBody := buildProfileXML(cfg.xmlRoot, profileName, string(payload))
				path := fmt.Sprintf("/JSSResource/%s/id/0", cfg.apiPath)
				resp, err := client.Do(ctx, "POST", path, bytes.NewReader(xmlBody))
				if err != nil {
					return fmt.Errorf("creating profile: %w", err)
				}
				_ = resp.Body.Close()
				fmt.Fprintf(os.Stderr, "Created successfully\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to the .mobileconfig file (required)")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVar(&profileName, "name", "", "profile name in Jamf Pro (defaults to filename without extension)")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing profile")

	return cmd
}

// lookupClassicProfileByName tries to GET a profile by name from the Classic API.
// Returns the ID and payload plist if found, empty string + nil if not found (404).
// Returning the payload in the same call avoids a second round-trip when the
// caller needs to preserve PayloadUUID/PayloadIdentifier on update.
func lookupClassicProfileByName(ctx context.Context, client registry.HTTPClient, apiPath, xmlRoot, name string) (id string, payloadPlist []byte, err error) {
	path := fmt.Sprintf("/JSSResource/%s/name/%s", apiPath, url.PathEscape(name))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		// 404 means not found — that's fine
		if isNotFound(err) {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("looking up profile: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", nil, err
	}

	// Parse XML to extract ID and existing payload plist.
	if xmlconv.IsXML(body) {
		m, parseErr := xmlconv.ToMap(body)
		if parseErr != nil {
			return "", nil, nil
		}
		if root, ok := m[xmlRoot].(map[string]any); ok {
			if general, ok := root["general"].(map[string]any); ok {
				id = extractField(general, "id")
				if payloads, ok := general["payloads"].(string); ok && payloads != "" {
					payloadPlist = []byte(payloads)
				}
				return id, payloadPlist, nil
			}
		}
	}

	return "", nil, nil
}

// buildProfileXML constructs the Classic API XML envelope for a configuration profile.
func buildProfileXML(xmlRoot, name, payload string) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	fmt.Fprintf(&buf, "<%s>\n  <general>\n    <name>%s</name>\n    <payloads>%s</payloads>\n  </general>\n</%s>",
		xmlRoot,
		xmlEscape(name),
		"<![CDATA["+payload+"]]>",
		xmlRoot)
	return buf.Bytes()
}

// xmlEscape escapes special XML characters in a string.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// fileBaseName returns the filename without extension.
func fileBaseName(path string) string {
	base := filepath.Base(path)
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '.' {
			return base[:i]
		}
	}
	return base
}

// cliNameForAPI maps API path segments to CLI command names.
func cliNameForAPI(apiPath string) string {
	switch apiPath {
	case "osxconfigurationprofiles":
		return "classic-macos-config-profiles"
	case "mobiledeviceconfigurationprofiles":
		return "classic-mobile-config-profiles"
	default:
		return apiPath
	}
}

// isNotFound checks if an error indicates a 404 response.
func isNotFound(err error) bool {
	var e *exitcode.Error
	return errors.As(err, &e) && e.Code == exitcode.NotFound
}
