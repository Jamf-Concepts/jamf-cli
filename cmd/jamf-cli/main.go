// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/google/shlex"
)

var (
	version        = "dev"
	commit         = "none"
	date           = "unknown"
	specProVersion = "unknown"
)

// injectEnvArgs prepends flags from JAMF_CLI_ARGS into args (after the program name).
// Supports shell-word splitting so quoted values (e.g. --profile "My Profile") work correctly.
// Caller must guarantee len(args) >= 1 (os.Args always satisfies this).
func injectEnvArgs(args []string, env string) []string {
	if env == "" {
		return args
	}
	extra, err := shlex.Split(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid JAMF_CLI_ARGS: %v\n", err)
		return args
	}
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

	cmd := commands.NewRootCmd(version, commit, date, specProVersion)
	executedCmd, err := cmd.ExecuteC()
	if err != nil {
		err = commands.ClassifyError(err)
		err = commands.EnrichPrivilegeError(executedCmd, err)
		if !commands.FormatError(err) {
			commands.FprintError(os.Stderr, err)
		}
		os.Exit(exitcode.CodeFrom(err))
	}
}
