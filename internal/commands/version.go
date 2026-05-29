// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	platgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	progen "github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// classicSpecPrefix marks Pro specs sourced from the Classic API
// resources manifest (versus modern OpenAPI specs). Used to partition
// provenance output for skimmability.
const classicSpecPrefix = "specs/classic/"

// newVersionCmd builds the `jamf-cli version` subcommand. Default output
// mirrors what `--version` prints. With -v, it also dumps the spec
// provenance baked in at generation time so users can answer "which
// Jamf Pro / Platform spec version is this CLI generated against?"
// without git archaeology. Honours -o json/yaml; falls through to text
// for anything else.
func newVersionCmd(cliCtx *registry.CLIContext, version, commit, date, specProVersion string) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print version information.

With -v, also prints the OpenAPI / manifest spec sources consumed at
code-generation time, with SHA256 hashes — useful for diagnosing
"why does this command 404 against my older Jamf Pro instance?"

Output is text by default. Pass -o json or -o yaml to get a structured
report keyed by source group (pro, proClassic, platform).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderVersion(cliCtx, version, commit, date, specProVersion, verbose, outputFmt)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print spec provenance (file + SHA256) for generated commands")
	return cmd
}

// versionReport is the structured shape emitted for -o json/yaml. The
// Sources field is omitted when verbose is false to keep the default
// banner minimal.
type versionReport struct {
	Version        string        `json:"version"`
	Commit         string        `json:"commit"`
	Built          string        `json:"built"`
	SpecProVersion string        `json:"specProVersion,omitempty"`
	Sources        *versionSpecs `json:"specSources,omitempty"`
}

// versionSpecs partitions provenance by origin so consumers can filter
// without parsing file paths. proClassic is a separate group from pro
// because Classic-API resources are produced from a YAML manifest, not
// an OpenAPI spec — different upstream, different update cadence.
type versionSpecs struct {
	Pro        []specEntry `json:"pro"`
	ProClassic []specEntry `json:"proClassic"`
	Platform   []specEntry `json:"platform"`
}

type specEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// renderVersion routes to JSON/YAML or text based on outputFmt. JSON and
// YAML go through the formatter (which handles json↔yaml conversion);
// everything else (table, csv, xml, plain, default) renders the human
// banner. Mirrors doctor's format-routing convention.
func renderVersion(cliCtx *registry.CLIContext, version, commit, date, specProVersion string, verbose bool, format string) error {
	if format == "json" || format == "yaml" {
		report := buildVersionReport(version, commit, date, specProVersion, verbose)
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		if cliCtx != nil && cliCtx.Output != nil {
			return cliCtx.Output.PrintRaw(data)
		}
		_, err = fmt.Fprintln(os.Stdout, string(data))
		return err
	}
	w := writerFor(cliCtx)
	printVersion(w, version, commit, date, specProVersion, verbose)
	return nil
}

// buildVersionReport assembles the structured report. Pure function for
// easy testing of the JSON shape and classic-vs-modern partitioning.
func buildVersionReport(version, commit, date, specProVersion string, verbose bool) versionReport {
	r := versionReport{Version: version, Commit: commit, Built: date, SpecProVersion: specProVersion}
	if !verbose {
		return r
	}
	modern, classic := partitionProSources(progen.Sources)
	pro := make([]specEntry, len(modern))
	for i, s := range modern {
		pro[i] = specEntry{File: s.File, SHA256: s.SHA256}
	}
	proClassic := make([]specEntry, len(classic))
	for i, s := range classic {
		proClassic[i] = specEntry{File: s.File, SHA256: s.SHA256}
	}
	platform := make([]specEntry, len(platgen.Sources))
	for i, s := range platgen.Sources {
		platform[i] = specEntry{File: s.File, SHA256: s.SHA256}
	}
	r.Sources = &versionSpecs{Pro: pro, ProClassic: proClassic, Platform: platform}
	return r
}

// partitionProSources splits Pro provenance into modern (OpenAPI) and
// classic (resources.yaml manifest) buckets. Classic entries are those
// whose File starts with specs/classic/.
func partitionProSources(all []progen.SpecSource) (modern, classic []progen.SpecSource) {
	for _, s := range all {
		if strings.HasPrefix(s.File, classicSpecPrefix) {
			classic = append(classic, s)
			continue
		}
		modern = append(modern, s)
	}
	return modern, classic
}

// printVersion writes the version banner — and provenance when verbose
// is true — to w. Pure function for easy testing. Classic Pro specs get
// their own section so the manifest-derived entry doesn't get lost in
// the long modern-spec list.
func printVersion(w io.Writer, version, commit, date, specProVersion string, verbose bool) {
	pf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}
	pln := func(args ...any) {
		_, _ = fmt.Fprintln(w, args...)
	}

	pf("jamf-cli %s\n", version)
	pf("  commit: %s\n", commit)
	pf("  built:  %s\n", date)
	if specProVersion != "" && specProVersion != "unknown" {
		pf("  spec:   Jamf Pro %s\n", specProVersion)
	}

	if !verbose {
		return
	}

	modern, classic := partitionProSources(progen.Sources)

	pln()
	pln("Pro spec sources:")
	if len(modern) == 0 {
		pln("  (none)")
	}
	for _, s := range modern {
		pf("  %s  sha256:%s\n", s.File, shortHash(s.SHA256))
	}

	pln()
	pln("Pro Classic spec sources:")
	if len(classic) == 0 {
		pln("  (none)")
	}
	for _, s := range classic {
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
