// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/bodyinput"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"gopkg.in/yaml.v3"
)

// printResult outputs a single item. The column formats get the flattened map
// for clean column output; json, yaml, ndjson, xml and raw get the full struct.
//
// The keep-set is named and the flattened shape is the default, rather than the
// other way round, because the format string is not normalised: this used to
// match "table", "csv" and "plain" exactly, so any other value — a mis-cased
// -o Table, or the internal json-multi that means JSON on the wire and a table
// on the screen — took the full struct to a table renderer. See
// output.RendersStructureVerbatim.
func printResult(out registry.OutputFormatter, item any, flattened map[string]any) error {
	if output.RendersStructureVerbatim(outputFmt) {
		return protect.PrintOne(out, item)
	}
	data, err := json.Marshal(flattened)
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	return out.PrintRaw(data)
}

// printExport outputs data as JSON (default) or YAML based on the global output format.
func printExport(data any) error {
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

// unmarshalInput tries JSON first, then YAML, into the target.
//
// The YAML rung is normalised through JSON — decoded to a generic value, then
// re-marshalled and bound with encoding/json — rather than handed to yaml.v3's
// own struct binding, because yaml.v3 reads neither `json` tags nor
// encoding/json's treatment of json.RawMessage. A key written as the `json`
// tag spells it (`activationPredicate`) binds to nothing there, and a mapping
// arriving at a json.RawMessage field is refused outright, which is what made
// `pro blueprints export -o yaml` output unreadable by `pro blueprints apply`.
// encoding/json matches a key case-insensitively, so a document written either
// way — the lower-cased keys yaml.v3 emits, or the `json`-tag keys an export
// writes — binds through this rung.
//
// yaml.v3's own binding stays as the last rung. It takes the values
// encoding/json refuses (a non-string mapping key, a timestamp scalar) once
// bodyinput has no answer for them, and an input carrying no content at all,
// which bodyinput.Normalize reports as an error and every caller here has
// always taken as "leave the target alone".
func unmarshalInput(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err == nil {
		return nil
	}
	if v, err := bodyinput.Normalize(data); err == nil {
		if shaped, err := json.Marshal(v); err == nil {
			if err := json.Unmarshal(shaped, target); err == nil {
				return nil
			}
		}
	}
	if err := yaml.Unmarshal(data, target); err == nil {
		return nil
	}
	return fmt.Errorf("input is not valid JSON or YAML")
}

// readInput reads JSON input from --from-file flag or stdin.
func readInput(fromFile string) ([]byte, error) {
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

// confirmDelete prompts for confirmation before a destructive operation.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmDelete(resourceType, name string, yes bool) (bool, error) {
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

// confirmAction prompts for confirmation before a device action (restart, erase, etc.).
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmAction(action, name string, yes bool) (bool, error) {
	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would %s %q\n", action, name)
		return false, nil
	}
	if !yes {
		if noInput {
			return false, fmt.Errorf("destructive operation requires --yes when --no-input is set")
		}
		fmt.Fprintf(os.Stderr, "This will %s %q. Type 'yes' to confirm: ", action, name)
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

// confirmReplace prompts for confirmation before replacing an existing resource.
// Returns true if the operation should proceed, false if it was dry-run/aborted.
func confirmReplace(resourceType, name string, yes bool) (bool, error) {
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
