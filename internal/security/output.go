// Copyright 2026, Jamf Software LLC

package security

import (
	"encoding/json"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// PrintOne outputs a single item through the CLI formatter.
func PrintOne[T any](out registry.OutputFormatter, item T) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	return out.PrintRaw(data)
}
