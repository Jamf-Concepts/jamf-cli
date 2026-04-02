// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"gopkg.in/yaml.v3"
)

// printProtectResult outputs a single item. For table/csv/plain, it uses the
// flattened map for clean column output. For json/yaml, it outputs the full struct.
func printProtectResult(out registry.OutputFormatter, item any, flattened map[string]any) error {
	switch outputFmt {
	case "table", "csv", "plain":
		data, err := json.Marshal(flattened)
		if err != nil {
			return fmt.Errorf("marshalling output: %w", err)
		}
		return out.PrintRaw(data)
	default:
		return protect.PrintOne(out, item)
	}
}

// printProtectExport outputs data as JSON (default) or YAML based on the global output format.
func printProtectExport(data any) error {
	switch outputFmt {
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		return enc.Encode(data)
	default:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
}

// unmarshalProtectInput tries JSON first, then YAML, into the target.
func unmarshalProtectInput(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err == nil {
		return nil
	}
	if err := yaml.Unmarshal(data, target); err == nil {
		return nil
	}
	return fmt.Errorf("input is not valid JSON or YAML")
}

// readProtectInput reads JSON input from --from-file flag or stdin.
func readProtectInput(fromFile string) ([]byte, error) {
	if fromFile != "" {
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("reading input file: %w", err)
		}
		return data, nil
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		if len(data) > 0 {
			return data, nil
		}
	}

	return nil, fmt.Errorf("input required: use --from-file or pipe JSON to stdin")
}

// confirmProtectDelete prompts for confirmation before a destructive operation.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmProtectDelete(resourceType, name string, yes bool) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would delete %s %q\n", resourceType, name)
		return false, nil
	}
	if !yes {
		if noInput {
			return false, fmt.Errorf("destructive operation requires --yes when --no-input is set")
		}
		fmt.Fprintf(os.Stderr, "This will delete %s %q. Type 'yes' to confirm: ", resourceType, name)
		var confirm string
		if _, err := fmt.Scanln(&confirm); err != nil {
			return false, fmt.Errorf("reading confirmation: %w", err)
		}
		if confirm != "yes" {
			return false, fmt.Errorf("aborted")
		}
	}
	return true, nil
}

// confirmProtectReplace prompts for confirmation before replacing an existing resource.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmProtectReplace(resourceType, name string, yes bool) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would replace %s %q\n", resourceType, name)
		return false, nil
	}
	if !yes {
		if noInput {
			return false, fmt.Errorf("destructive operation requires --yes when --no-input is set")
		}
		fmt.Fprintf(os.Stderr, "%s %q already exists and will be replaced. Type 'yes' to confirm: ", resourceType, name)
		var confirm string
		if _, err := fmt.Scanln(&confirm); err != nil {
			return false, fmt.Errorf("reading confirmation: %w", err)
		}
		if confirm != "yes" {
			return false, fmt.Errorf("aborted")
		}
	}
	return true, nil
}
