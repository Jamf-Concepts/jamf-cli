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
	// Drop the rows --select emptied, rather than asking whether they ALL
	// emptied. A table and a CSV take their column set from row 0 alone, so
	// keeping a row the select missed emptied the column set and discarded
	// every matched value in every later row. Dropping them makes the column
	// set correct by construction, and leaves zero rows in the case the old
	// all-or-nothing guard covered — which every renderer already handles as
	// an empty collection, so no format needs a special early return.
	rows, dropped := selectSurvivors(rows)
	reportSelectMiss(dropped)
	return formatterFor(cliCtx, outputFmt).Print(rows)
}

// selectSurvivors drops the rows --select emptied and reports how many.
//
// One decision for every renderer. The guard it replaced lived only in
// printRows, so `multi`'s aggregated branches, `group-tools export` and three
// report sections each answered the same condition differently — an empty
// collection with a warning here, `[{}]` in silence there, zero bytes
// elsewhere. Three answers to one question.
func selectSurvivors(rows []map[string]any) ([]map[string]any, int) {
	return (output.Projector{Select: selectFields}).DropEmptySelections(rows)
}

// reportSelectMiss says on stderr how many rows --select emptied. Without it
// the drop is silent, and a caller who mistyped a field gets an empty document
// and exit 0 with nothing explaining either.
//
// stderr rather than the formatter's writer, so it never lands in --out-file
// beside the data. Suppressed by --quiet and --no-hints, being advisory.
func reportSelectMiss(dropped int) {
	if dropped == 0 || quiet || noHints || len(selectFields) == 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "--select %s matched no field in %d row(s)\n", strings.Join(selectFields, ","), dropped)
}

// reportFieldMiss says on stderr that --field named nothing any row carried.
//
// --field's sibling --select warns on the same input, and without this the two
// answered identically-shaped mistakes differently: `commands --field
// nosuchfield --out-file f` left f at 0 bytes, exit 0, both streams empty, so a
// job could not tell a wrong field name from an empty result.
func reportFieldMiss(rows []map[string]any, written int) {
	if written > 0 || len(rows) == 0 || quiet || noHints || fieldName == "" {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "--field %s matched no field in %d row(s)\n", fieldName, len(rows))
}

// printSection writes a section header only when a body will follow it.
//
// The six hand-written multi-section reports wrote their own banner and then
// called printRows, so the drop suppressed the body with the banner already on
// the writer: `pro report security -o table --select nosuchfield` produced 105
// bytes of nothing but three box-drawing lines, one reading
// `── Flagged Devices (5) ──`, at exit 0. A CSV consumer received a stream of
// box-drawing characters. The decision has to be made before the header, and in
// one place, or the call sites disagree about it — three of them did.
//
// It renders through printRows rather than reproducing its tail, so a section
// and a whole-command read emit the same bytes for the same format. The two
// diverged when this reproduced only the early return: a select miss here wrote
// zero bytes where printRows wrote `[]`.
func printSection(cliCtx *registry.CLIContext, header string, rows []map[string]any) error {
	kept, dropped := selectSurvivors(rows)
	if len(kept) == 0 && dropped > 0 {
		reportSelectMiss(dropped)
		return nil
	}
	if header != "" {
		if _, err := fmt.Fprint(writerFor(cliCtx), header); err != nil {
			return err
		}
	}
	return printRows(cliCtx, kept)
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
