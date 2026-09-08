// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// printRows renders rows through the shared formatter, which is where
// --out-file, --select, --compact, --quiet and --no-hints apply.
//
// --field takes a separate branch and applies no projector. That matches every
// generated command: an extracted scalar is not a row to narrow.
//
// A nil slice becomes an empty list. `null` breaks a jq pipeline on the tenants
// where a collection is empty, and the table and CSV writers reject an untyped
// nil.
//
// Rows reach Print unmarshalled. registry.OutputFormatter declares no
// Print(any), so the alternative is to marshal and call PrintRaw, which parses
// the bytes back. That costs about a second of CPU and a gigabyte of allocation
// on a fleet-sized report, turns every integer into a float64, and routes
// -o raw and -o xml to a renderer Print never selects.
func printRows(cliCtx *registry.CLIContext, rows []map[string]any) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	if fieldName != "" {
		return printFieldValues(writerFor(cliCtx), rows, fieldName)
	}
	// Reported here rather than in each renderer. table, csv, plain and detail
	// all decline an empty column set and write nothing.
	reportProjectionMiss(rows)
	return formatterFor(cliCtx, outputFmt).Print(rows)
}

// reportProjectionMiss says on stderr that --select or --compact matched no
// field in any row.
//
// table, csv, plain and detail decline an empty column set, so without the note
// `commands -o table --select nosuchfield` writes zero bytes on both streams
// and exits 0. json, yaml and ndjson still emit a document of empty objects.
//
// --quiet and --no-hints do not suppress it. Those flags suppress advisory
// hints, and under the four declining formats this note is the only signal that
// separates a mistyped field name from an empty collection.
func reportProjectionMiss(rows []map[string]any) {
	if !projectionRendersNothing(rows) {
		return
	}
	what := "--compact"
	if len(selectFields) > 0 {
		what = "--select " + strings.Join(selectFields, ",")
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s matched no field in %d row(s)\n", what, len(rows))
}

// reportFieldMiss says on stderr that --field named a field no row carried.
//
// A --field miss writes nothing at all, so without the note
// `commands --field nosuchfield --out-file f` leaves f empty with both streams
// silent and exits 0. --quiet and --no-hints do not suppress it, for the reason
// reportProjectionMiss gives.
func reportFieldMiss(rows []map[string]any, written int) {
	if written > 0 || len(rows) == 0 || fieldName == "" {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "--field %s matched no field in %d row(s)\n", fieldName, len(rows))
}

// projectionRendersNothing reports whether the live --select or --compact
// empties every row, which is when table, csv, plain and detail write nothing.
//
// The miss note and the section header both read it. Two predicates let a
// header survive rows that rendered nothing.
func projectionRendersNothing(rows []map[string]any) bool {
	return (output.Projector{Select: selectFields, Compact: compact}).RendersNothing(rows)
}

// printSection writes a section header above a row set.
//
// A machine-rendered format gets no header, because csv.Reader yields a
// one-field row for a `──` line. output.IsMachineRendered answers that, and it
// sits beside Print's own switch so the two cannot disagree: -o xml and -o raw
// have no case there, so both render tables and both keep their header.
//
// The rows go through printRows, so a section and a whole-command read emit the
// same bytes for the same format.
func printSection(cliCtx *registry.CLIContext, header string, rows []map[string]any) error {
	// A header only above a body. The renderers decline an empty column set, so
	// without this check `pro report security -o table --select nosuchfield`
	// writes 105 bytes of box-drawing lines and no rows.
	if projectionRendersNothing(rows) {
		return printRows(cliCtx, rows)
	}
	if header != "" && !output.IsMachineRendered(output.Format(outputFmt)) {
		if _, err := fmt.Fprint(writerFor(cliCtx), header); err != nil {
			return err
		}
	}
	return printRows(cliCtx, rows)
}

// formatterFor returns the shared formatter rendering in format. printRows
// passes the global -o value. `multi` and `group-tools export` pass a format
// their own argument names. Cloning keeps the writer and the projector, which a
// fresh formatter drops.
//
// The fallback covers a caller reached with a test double, or one reached before
// PersistentPreRunE wired the output up. It calls the shared builder rather than
// output.New, so the flags a fresh formatter discards still apply.
func formatterFor(cliCtx *registry.CLIContext, format string) *output.Formatter {
	if cliCtx != nil {
		if co, ok := cliCtx.Output.(*cliOutput); ok && co.Formatter != nil {
			return co.WithFormat(format)
		}
	}
	// No file handle. PersistentPreRunE opens --out-file, and it has not run on
	// this path.
	return buildOutputFormatter(nil, false).WithFormat(format)
}
