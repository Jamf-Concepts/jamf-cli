// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	platgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	progen "github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
)

// newVersionCmd builds the `jamf-cli version` subcommand. Default output
// mirrors what `--version` prints. With -v, it also dumps the spec
// provenance baked in at generation time so users can answer "which
// Jamf Pro / Platform spec version is this CLI generated against?"
// without git archaeology.
func newVersionCmd(version, commit, date string) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print version information.

With -v, also prints the OpenAPI / manifest spec sources consumed at
code-generation time, with SHA256 hashes — useful for diagnosing
"why does this command 404 against my older Jamf Pro instance?"`,
		Run: func(cmd *cobra.Command, args []string) {
			printVersion(os.Stdout, version, commit, date, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print spec provenance (file + SHA256) for generated commands")
	return cmd
}

// printVersion writes the version banner — and provenance when verbose
// is true — to w. Pure function for easy testing.
func printVersion(w io.Writer, version, commit, date string, verbose bool) {
	pf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}
	pln := func(args ...any) {
		_, _ = fmt.Fprintln(w, args...)
	}

	pf("jamf-cli %s\n", version)
	pf("  commit: %s\n", commit)
	pf("  built:  %s\n", date)

	if !verbose {
		return
	}

	pln()
	pln("Pro spec sources:")
	if len(progen.Sources) == 0 {
		pln("  (none)")
	}
	for _, s := range progen.Sources {
		pf("  %s  sha256:%s\n", s.File, shortHash(s.SHA256))
	}

	pln()
	pln("Platform spec sources:")
	if len(platgen.Sources) == 0 {
		pln("  (none)")
	}
	for _, s := range platgen.Sources {
		pf("  %s  sha256:%s\n", s.File, shortHash(s.SHA256))
	}
}

// shortHash returns the first 12 chars of a hex hash. Long enough to
// distinguish revisions, short enough to scan in a terminal.
func shortHash(h string) string {
	if len(h) < 12 {
		return h
	}
	return h[:12]
}
