// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// injectEnvArgs prepends flags from JAMF_CLI_ARGS into args (after the program name).
// Quoted values are not supported — use simple flags only (e.g. --quiet --no-input).
// Caller must guarantee len(args) >= 1 (os.Args always satisfies this).
func injectEnvArgs(args []string, env string) []string {
	extra := strings.Fields(env)
	if len(extra) == 0 {
		return args
	}
	result := make([]string, 0, len(args)+len(extra))
	result = append(result, args[0])
	result = append(result, extra...)
	result = append(result, args[1:]...)
	return result
}

func main() {
	os.Args = injectEnvArgs(os.Args, os.Getenv("JAMF_CLI_ARGS"))

	cmd := commands.NewRootCmd(version, commit, date)
	if err := cmd.Execute(); err != nil {
		if !commands.FormatError(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitcode.CodeFrom(err))
	}
}
