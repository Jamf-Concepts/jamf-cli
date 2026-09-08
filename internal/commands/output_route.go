// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// multiSectionFormatNote is the --help caveat for a command that renders more
// than one section. Each section is a complete top-level document, so csv,
// ndjson and plain produce N of them in one destination with nothing between,
// the section banner being suppressed for a format a parser reads.
//
// One constant rather than a copy per command: five commands carried the
// sentence and two with the same shape were missed.
const multiSectionFormatNote = `

With no -o flag, this report writes a table. Then --out-file receives that
table, not JSON. Use -o json or -o yaml to write structured data to the file.
Those two emit one document holding every section. csv, ndjson and plain emit
one undelimited block per section, which no parser reads as a single file.`

// printRows renders rows through the shared formatter, which is where
// --out-file, --select, --compact, --quiet and --no-hints apply.
//
// --field applies no projector, matching every generated command: an extracted
// scalar is not a row to narrow.
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
	return printThrough(formatterFor(cliCtx, outputFmt), rows)
}

// printThrough renders rows through f, honouring --field. printRows passes the
// formatter for the global -o value; `multi` and `group-tools export` pass one
// for a format their own argument names.
//
// The --field branch is here because nothing in internal/output reads
// fieldName, so a caller reaching Print directly discards the flag.
func printThrough(f *output.Formatter, rows []map[string]any) error {
	if fieldName != "" {
		return printFieldValues(f.Writer(), rows, fieldName)
	}
	return f.Print(rows)
}

// projectionRendersNothing reports whether the live --select or --compact
// empties every row, which is when table, csv, plain and detail write nothing.
// A caller reads it to decide whether to write a section header.
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
	// without the second condition `pro report security -o table --select
	// nosuchfield` writes 105 bytes of box-drawing lines and no rows.
	if header != "" && !projectionRendersNothing(rows) && !output.IsMachineRendered(output.Format(outputFmt)) {
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
