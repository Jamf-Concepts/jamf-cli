// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// printRows renders rows through the shared formatter, which is where
// --out-file, --select, --compact, --quiet and --no-hints are applied.
//
// --field is not among them, and does not compose with them. It is implemented
// in cliOutput.PrintRaw, which writes the extracted values to os.Stdout itself
// and applies no projector: --out-file does not receive them, --select and
// --compact do not narrow them, and the advisory hint --quiet and --no-hints
// suppress is never reached. That is its behaviour on every generated command
// too, so changing it is a separate piece of work.
//
// A nil slice becomes an empty list: `null` breaks a jq pipeline on exactly the
// tenants where the collection is empty, and the table and CSV writers refuse
// an untyped nil.
//
// The rows reach Print as they are. registry.OutputFormatter declares no
// Print(any), so the obvious route is to marshal and hand the bytes to
// PrintRaw, and PrintRaw does arrive at the same renderers — but only after
// parsing the bytes back. That round trip costs a second of CPU and about a
// gigabyte on a fleet-sized report, turns every integer into a float64, and
// lands -o raw and -o xml on a renderer Print's own switch never selects. The
// concrete formatter is reachable here, so none of that has to be paid.
func printRows(cliCtx *registry.CLIContext, rows []map[string]any) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	// --field takes the byte route, cliOutput.PrintRaw being where it is
	// implemented. See the note above on what it therefore does not compose with.
	if fieldName != "" && cliCtx != nil && cliCtx.Output != nil {
		b, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("marshalling output: %w", err)
		}
		return cliCtx.Output.PrintRaw(b)
	}
	return formatterFor(cliCtx, outputFmt).Print(rows)
}

// formatterFor returns the shared formatter rendering in format. printRows asks
// for the global -o value; `multi` and `group-tools export` ask for a format
// their own argument names instead. Cloning keeps the writer and the projector,
// which a fresh formatter drops.
//
// The fallback is for a caller reached with a test double, or before
// PersistentPreRunE wired the output up, and mirrors writerFor's nil guard. It
// goes through the shared builder rather than output.New so that the flags a
// fresh formatter discards still apply on that path.
func formatterFor(cliCtx *registry.CLIContext, format string) *output.Formatter {
	if cliCtx != nil {
		if co, ok := cliCtx.Output.(*cliOutput); ok && co.Formatter != nil {
			return co.WithFormat(format)
		}
	}
	// No file handle: PersistentPreRunE is what opens --out-file, and it has
	// not run on this path.
	return buildOutputFormatter(nil, false).WithFormat(format)
}
