// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"sync"
	"testing"
)

// rootFlagState is every root-bound package var, which main's error path reads
// after `mcp serve` returns.
type rootFlagState struct {
	profile, outputFmt, outFile, fieldName, serverURL, tokenFile, tenantID, environmentID, cliVersion           string
	quiet, noHints, noInput, noColor, dryRun, wide, compact, allowPartialFailure, noVersionCheck, noUpdateCheck bool
	verboseLevel                                                                                                int
}

func snapshotRootFlagState() rootFlagState {
	return rootFlagState{
		profile: profile, outputFmt: outputFmt, outFile: outFile, fieldName: fieldName, serverURL: serverURL,
		tokenFile: tokenFile, tenantID: tenantID, environmentID: environmentID, cliVersion: cliVersion,
		quiet: quiet, noHints: noHints, noInput: noInput, noColor: noColor, dryRun: dryRun, wide: wide,
		compact: compact, allowPartialFailure: allowPartialFailure, noVersionCheck: noVersionCheck,
		noUpdateCheck: noUpdateCheck, verboseLevel: verboseLevel,
	}
}

func setRootFlagSentinels() {
	profile, outputFmt, outFile, fieldName, serverURL = "sentinel-profile", "yaml", "/sentinel/out", "sentinel", "https://sentinel.example"
	tokenFile, tenantID, environmentID, cliVersion = "/sentinel/token", "sentinel-tenant", "sentinel-env", "sentinel-version"
	quiet, noHints, noInput, noColor, dryRun, wide, compact = true, true, true, true, true, true, true
	allowPartialFailure, noVersionCheck, noUpdateCheck, verboseLevel = true, true, true, 3
}

var concurrentRunCommandArgs = [][]string{
	{"pro", "computers", "list"},
	{"cfg", "set-default", "x"},
	{"-q", "pro", "backup", "--output", "/x"},
	{"pro", "scripts", "create", "--script-file", "/etc/passwd"},
	{"pro", "diff", "--source", "/a", "--target", "other"},
	{"-o", "json", "pro", "computers", "list", "-vvv", "-n"},
	{"-q", "db"},
	{"pro", "packages", "sync", "--dir", "/x", "--delete"},
}

// Resolving a run_command argv inside `mcp serve` must not rebind a root flag
// var: tool calls run concurrently, and main reads these after serve returns.
func TestResolveChildInvocation_LeavesTheServingProcessFlagStateAlone(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	t.Cleanup(resetGlobals)
	installMCPResolver(root, "")
	t.Cleanup(func() { installMCPResolver(nil, "") })

	setRootFlagSentinels()
	want := snapshotRootFlagState()

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := concurrentRunCommandArgs[i%len(concurrentRunCommandArgs)]
			_, _ = buildChildArgs("prod", args)
			_ = refuseReportThroughRunCommand(args)
		}()
	}
	wg.Wait()

	if got := snapshotRootFlagState(); got != want {
		t.Errorf("resolving run_command argv changed the serving process's flag state:\n got %s\nwant %s",
			fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want))
	}
}
