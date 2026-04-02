// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd := commands.NewRootCmd(version, commit, date)
	if err := cmd.Execute(); err != nil {
		if !commands.FormatError(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitcode.CodeFrom(err))
	}
}
