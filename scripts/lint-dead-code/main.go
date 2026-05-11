// Copyright 2026, Jamf Software LLC

// Command lint-dead-code detects dead Cobra flag bindings and dead unexported
// helpers under internal/commands/ via AST analysis.
//
// Behaviour is warn-only by default (exits 0 even when findings are present)
// for a 2-week calibration window. Pass --gate to flip to exit-2 on findings.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	var (
		root string
		gate bool
	)
	flag.StringVar(&root, "root", "internal/commands", "root directory to scan")
	flag.BoolVar(&gate, "gate", false, "exit non-zero when findings are present (default: warn-only)")
	flag.Parse()

	f, err := scan(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lint-dead-code: %v\n", err)
		os.Exit(1)
	}

	if len(f.deadFlags) == 0 && len(f.deadFuncs) == 0 {
		fmt.Println("No dead flags or functions found.")
		os.Exit(0)
	}

	printReport(os.Stdout, f, gate)
	if gate {
		os.Exit(2)
	}
	os.Exit(0)
}

func printReport(out io.Writer, f findings, gate bool) {
	w := &errWriter{w: out}
	w.printf("DEAD FLAGS (%d):\n", len(f.deadFlags))
	for _, df := range f.deadFlags {
		w.printf("  %s:%d  --%s  (var: %s)\n", df.file, df.line, df.flagName, df.backingVar)
	}
	w.println("")
	w.printf("DEAD FUNCTIONS (%d):\n", len(f.deadFuncs))
	for _, df := range f.deadFuncs {
		w.printf("  %s:%d  %s\n", df.file, df.line, df.name)
	}
	w.println("")
	w.println("To suppress an intentional finding:")
	w.println("  - Flags: add `Annotations: map[string]string{\"lint:keep-flag\": \"<name>,...\"}` to the owning *cobra.Command")
	w.println("  - Functions: add a `//lint:keep` line to the doc comment")
	w.println("")
	if gate {
		w.println("Mode: gating (will exit non-zero)")
	} else {
		w.println("Mode: warn-only (use --gate to fail on findings)")
	}
}

// errWriter swallows io.Writer errors. We're writing to os.Stdout in main; if
// that breaks, the lint exit code is the least of our concerns.
type errWriter struct {
	w io.Writer
}

func (e *errWriter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(e.w, format, args...)
}

func (e *errWriter) println(s string) {
	_, _ = fmt.Fprintln(e.w, s)
}
