// Copyright 2026, Jamf Software LLC

package clean

import (
	"encoding/json"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func printThings(cliCtx *registry.CLIContext, rows []map[string]any) error {
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return cliCtx.Output.PrintRaw(data)
}

func formatName() string {
	return string(output.FormatJSON)
}
