package protect

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

// PrintList outputs a list of items through the CLI formatter.
func PrintList[T any](out registry.OutputFormatter, items []T) error {
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	return out.PrintRaw(data)
}
