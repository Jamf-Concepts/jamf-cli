package main

import (
	"fmt"
	"os"

	"github.com/ktn-jamf/jamfpro-cli/internal/commands"
	"github.com/ktn-jamf/jamfpro-cli/internal/exitcode"
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
