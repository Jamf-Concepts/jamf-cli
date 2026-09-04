// Copyright 2026, Jamf Software LLC

// Command lint-shared-output refuses a command that builds its own output
// formatter instead of printing through the one PersistentPreRunE assembles.
//
// The second formatter is the defect. PersistentPreRunE applies every global
// output flag to the shared formatter with SetWriter, SetProjector, SetQuiet,
// SetNoHints and SetExplicitNoColor, so a formatter built anywhere else carries
// none of them and writes to os.Stdout. The flags are parsed, accepted and
// discarded: `-o json --out-file titles.json` exited 0, wrote 2934 bytes to
// standard output and left the file at 0 bytes, which a scheduled job cannot
// tell from a tenant with no data.
//
// It is a lint rather than a note in CLAUDE.md because both people and coding
// agents copy the pattern that already surrounds the code. 27 commands held the
// second formatter, every one of them a copy of its neighbour.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	root := flag.String("root", "internal", "root directory to scan")
	flag.Parse()

	res, err := scan(*root, defaultExemptions)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "lint-shared-output: %v\n", err)
		os.Exit(1)
	}

	if res.clean() {
		fmt.Printf("Every output.Formatter built under %s is accounted for.\n", *root)
		return
	}

	printReport(os.Stdout, res)
	os.Exit(2)
}

func printReport(out io.Writer, res result) {
	if len(res.findings) > 0 {
		_, _ = fmt.Fprintf(out, "FORMATTERS OUTSIDE THE SHARED ROUTE (%d):\n", len(res.findings))
		for _, f := range res.findings {
			_, _ = fmt.Fprintf(out, "  %s:%d  in %s  (%s)\n", f.file, f.line, f.fn, f.form)
		}
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintln(out, "Print through the shared formatter instead, which carries --out-file,")
		_, _ = fmt.Fprintln(out, "--select, --compact, --quiet and --no-hints:")
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintln(out, "  return printRows(cliCtx, rows)")
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintln(out, "A command whose own flag names the format calls formatterFor(cliCtx, format).")
		_, _ = fmt.Fprintln(out, "A renderer of its own text takes writerFor(cliCtx) as its writer.")
		_, _ = fmt.Fprintln(out, "A site that genuinely cannot use the shared formatter needs an entry in")
		_, _ = fmt.Fprintln(out, "defaultExemptions (scripts/lint-shared-output/scan.go) stating why.")
		_, _ = fmt.Fprintln(out, "")
	}

	if len(res.stale) > 0 {
		_, _ = fmt.Fprintf(out, "STALE EXEMPTIONS (%d):\n", len(res.stale))
		for _, e := range res.stale {
			_, _ = fmt.Fprintf(out, "  %s  %s  (%s)\n", e.file, e.fn, staleAdvice(e.reason))
		}
		_, _ = fmt.Fprintln(out, "")
		_, _ = fmt.Fprintln(out, "Correct the entry in defaultExemptions, or delete it when its site is gone.")
		_, _ = fmt.Fprintln(out, "")
	}
}

func staleAdvice(reason staleReason) string {
	switch reason {
	case fileNotFound:
		return "names no file under the scanned root — correct the path"
	case funcNotFound:
		return "names no function in that file — correct the name"
	default:
		return "no longer builds a formatter — delete the entry"
	}
}
