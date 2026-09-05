// Copyright 2026, Jamf Software LLC

package main

import (
	"fmt"
	"os"
	"runtime/debug"

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

// resolveVersion falls back to the module version the Go toolchain stamps into
// the binary. Release builds get their version from -ldflags (see the
// Makefile), but `go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest`
// gets none and would report the "dev" default — which makes bug reports
// untraceable and suppresses the newer-release advisory, since that only runs
// on an exact release version.
func resolveVersion(ldflagsVersion string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if ldflagsVersion != "" && ldflagsVersion != "dev" {
		return ldflagsVersion
	}
	info, ok := readBuildInfo()
	if !ok || info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ldflagsVersion
	}
	return info.Main.Version
}

// run builds the command tree, executes args against it and returns the process
// exit code. It exists so the error-classification chain is reachable from a
// test: os.Exit is not callable from one, and this whole sequence used to live
// inside main, which meant nothing exercised it. Deleting the ClassifyError call
// left the entire test suite passing while every documented usage exit code
// silently reverted to 1.
//
// args is os.Args, program name included, and is passed to cobra rather than
// assigned back over os.Args, which is what made the sequence uncallable —
// cobra reads os.Args itself only when SetArgs was never called.
func run(args []string, envArgs string) int {
	args = injectEnvArgs(args, envArgs)
	version = resolveVersion(version, debug.ReadBuildInfo)

	cmd := commands.NewRootCmd(version, commit, date, specProVersion)
	cmd.SetArgs(args[1:])

	executedCmd, err := cmd.ExecuteC()
	if err == nil {
		return exitcode.Success
	}
	// Ahead of ClassifyError, and wrapping with %w, because both of the
	// steps below work by errors.As and a flattened chain loses the
	// classification — the trap platform_audit.go's decorator documented.
	err = commands.AnnotateScopeLevelError(executedCmd, err)
	err = commands.ClassifyError(err)
	err = commands.EnrichPrivilegeError(executedCmd, err)
	if !commands.FormatError(err) {
		commands.FprintError(os.Stderr, err)
	}
	return exitcode.CodeFrom(err)
}

func main() {
	os.Exit(run(os.Args, os.Getenv("JAMF_CLI_ARGS")))
}
