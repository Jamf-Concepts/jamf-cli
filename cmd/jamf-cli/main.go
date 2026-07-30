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

func main() {
	os.Args = injectEnvArgs(os.Args, os.Getenv("JAMF_CLI_ARGS"))
	version = resolveVersion(version, debug.ReadBuildInfo)

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
