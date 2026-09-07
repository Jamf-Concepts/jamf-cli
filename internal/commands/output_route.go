// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"

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
	return formatterFor(cliCtx, outputFmt).Print(rows)
}

// reportFieldMiss says on stderr that --field named nothing any row carried.
//
// Without it, `commands --field nosuchfield --out-file f` left f at 0 bytes at
// exit 0 with both streams empty, so a job could not tell a wrong field name
// from an empty result.
//
// NOT suppressed by --quiet or --no-hints, unlike an advisory hint: a --field
// miss produces no output at all, and silencing the only signal that anything
// happened is what makes it indistinguishable from success. --select needs no
// equivalent, because a projection that matches nothing still emits a document
// of empty objects rather than nothing.
func reportFieldMiss(rows []map[string]any, written int) {
	if written > 0 || len(rows) == 0 || fieldName == "" {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "--field %s matched no field in %d row(s)\n", fieldName, len(rows))
}

// printSection writes a section header above a row set.
//
// The header is withheld for a machine-rendered format, because a `──` line in
// a JSON or CSV destination is not something a parser can read: with the set
// hand-written here it went wrong in both directions, suppressing the banner
// above the tables that -o xml and -o raw actually render while writing
// box-drawing lines into a CSV file. output.IsMachineRendered sits beside
// Print's own switch so the two cannot drift.
//
// It renders through printRows rather than reproducing its tail, so a section
// and a whole-command read emit the same bytes for the same format.
func printSection(cliCtx *registry.CLIContext, header string, rows []map[string]any) error {
	// A header only above a body. The renderers decline an empty column set, so
	// without this the banner outlived the rows it announced:
	// `pro report security -o table --select nosuchfield` produced 105 bytes of
	// nothing but box-drawing lines.
	if (output.Projector{Select: selectFields, Compact: compact}).RendersNothing(rows) {
		return printRows(cliCtx, rows)
	}
	if header != "" && !output.IsMachineRendered(output.Format(outputFmt)) {
		if _, err := fmt.Fprint(writerFor(cliCtx), header); err != nil {
			return err
		}
	}
	return printRows(cliCtx, rows)
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
