// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// printRows renders rows through the shared formatter, which is where
// --out-file, --select, --compact, --quiet and --no-hints are applied.
//
// --field is partly among them. It now follows --out-file, through the shared
// printFieldValues, which closes the half of issue #352 that mattered: a report
// used to split between the file and the terminal. It still applies no
// projector, so --select and --compact do not narrow its values and the
// advisory hint --quiet and --no-hints suppress is never reached — that is its
// behaviour on every generated command too, and narrowing an already-extracted
// scalar means something different from narrowing a row.
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
// concrete formatter is reachable here, so none of that has to be paid. The
// --field branch above avoids it for the same reason.
func printRows(cliCtx *registry.CLIContext, rows []map[string]any) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	// --field extracts straight from the rows. It used to marshal them and hand
	// the bytes to PrintRaw, which parsed them back to the type they already
	// were: ~200ms and up to a gigabyte of peak allocation on a fleet-sized
	// report, to reach the same extraction.
	if fieldName != "" {
		return printFieldValues(writerFor(cliCtx), rows, fieldName)
	}
	// A --select that matches nothing in these rows leaves every row empty, and
	// a count over no columns reads as a rendering fault. This is the normal
	// case on a multi-section report: the sections do not share a schema, so
	// `--select reason` is carried by one section and absent from the rest.
	// --select was inert on these reports until this change, so the symptom
	// arrives with it.
	//
	// Printing nothing for such a section is the honest answer. It does mean a
	// whole command whose rows carry none of the named paths prints nothing at
	// all, which is still better than a banner claiming N results above a blank
	// header.
	if (output.Projector{Select: selectFields}).SelectsNothing(rows) {
		return nil
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
