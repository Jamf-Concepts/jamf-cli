// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// requirePlatformClient returns an error if the Platform SDK client is not
// available. Platform commands call this at the top of RunE so users get a
// clear message instead of a nil-pointer panic.
func requirePlatformClient(cliCtx *registry.CLIContext) error {
	if cliCtx.PlatformClient == nil {
		return fmt.Errorf("this command requires platform gateway auth\n\n" +
			"Set up a platform profile:\n" +
			"  jamf-cli config add-profile <name> --auth-method platform --url <gateway-url> --tenant-id <id>\n\n" +
			"Or use environment variables:\n" +
			"  JAMF_URL, JAMF_CLIENT_ID, JAMF_CLIENT_SECRET, JAMF_TENANT_ID")
	}
	return nil
}

// printScaffold marshals the given value as indented JSON to stdout.
// Used by apply commands with --scaffold to show the expected input structure.
func printScaffold(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
