// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// printRows renders rows through the shared formatter. A nil slice becomes an
// empty list: `null` breaks a jq pipeline on exactly the tenants where the
// collection is empty, and the table and CSV writers refuse an untyped nil.
func printRows(cliCtx *registry.CLIContext, rows []map[string]any) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	return printData(cliCtx, rows)
}

// printData renders any shape through the shared formatter, which is where
// --out-file, --select, --field, --compact, --quiet and --no-hints are applied.
// registry.OutputFormatter declares no Print(any), so the value is marshalled
// first and handed to PrintRaw — the route `config list` established, and the
// one that reaches cliOutput.PrintRaw where --field is implemented.
func printData(cliCtx *registry.CLIContext, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	if cliCtx == nil || cliCtx.Output == nil {
		// Only a test fixture reaches this. The root command owns the only
		// PersistentPreRunE in the tree and builds the formatter before every
		// command, the auth-skipped ones included, so no production path can
		// arrive without one — and this fallback formatter carries no global
		// output flag, which is the defect the shared route exists to remove.
		return formatterFor(cliCtx, outputFmt).PrintRaw(b)
	}
	return cliCtx.Output.PrintRaw(b)
}

// formatterFor returns the shared formatter rendering in format, for the two
// commands whose own argument names it rather than -o: `multi` reads the format
// from the inner command's arguments, and `group-tools export` from --format.
// Cloning keeps the writer and the projector, which a fresh formatter drops.
func formatterFor(cliCtx *registry.CLIContext, format string) *output.Formatter {
	if cliCtx != nil {
		if co, ok := cliCtx.Output.(*cliOutput); ok && co.Formatter != nil {
			return co.Formatter.WithFormat(format)
		}
	}
	// Mirrors writerFor's nil guard: a caller reached with a test double, or
	// before PersistentPreRunE wired the output up, still prints.
	return output.New(format, noColor, wide)
}
