// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// timeoutNarrowers are the flags that narrow what a request asks for, in the
// order the hint names them. A command is offered one only when it declares it:
// `pro patch-software-title-configurations list` has none, and was told to pass
// a smaller --page-size anyway (issue 476). --page-size is separate because it
// shrinks the page rather than the result set.
var timeoutNarrowers = []string{"section", "filter"}

// RefineTimeoutHint replaces the generic server-timeout hint with one naming
// only the narrowing flags cmd declares.
//
// The layers that raise registry.ErrServerTimeout — the Pro HTTP client and the
// Platform SDK paging helper — see a request, not a cobra command, so they
// cannot know which flags exist. Doing it here, once, after Execute returns,
// covers every namespace (pro, platform, security gateway-served commands,
// handwritten and generated alike) without a per-command table that the next
// generated command would miss.
//
// Only the hint that registry.ServerTimeoutHint stamped is replaced. A write's
// "may have been applied" hint and any hint a command set itself pass through.
func RefineTimeoutHint(cmd *cobra.Command, err error) error {
	if err == nil || !errors.Is(err, registry.ErrServerTimeout) {
		return err
	}
	var e *exitcode.Error
	if !errors.As(err, &e) || e.Hint != registry.ServerTimeoutHint {
		return err
	}
	e.Hint = timeoutHintFor(cmd)
	return err
}

// timeoutHintFor builds the hint for a command from the narrowing flags it
// declares, local or inherited.
func timeoutHintFor(cmd *cobra.Command) string {
	const lead = "the server did not answer in time"
	if cmd == nil {
		return lead + "; retry later"
	}
	var advice []string
	if cmd.Flags().Lookup("page-size") != nil {
		advice = append(advice, "pass a smaller --page-size")
	}
	var narrowers []string
	for _, name := range timeoutNarrowers {
		if cmd.Flags().Lookup(name) != nil {
			narrowers = append(narrowers, "--"+name)
		}
	}
	if len(narrowers) > 0 {
		advice = append(advice, "narrow it with "+strings.Join(narrowers, " or "))
	}
	if len(advice) == 0 {
		return lead + "; this command has no flag that narrows the request, so retry later"
	}
	return lead + "; " + strings.Join(advice, ", or ") + ", or retry later"
}
