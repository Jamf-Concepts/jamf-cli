// Copyright 2026, Jamf Software LLC

package platform

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ConfirmAction prompts the user to confirm a destructive action. Returns nil
// on confirmation, an error otherwise. When stdin is not a terminal (e.g. CI),
// requires the --yes flag (yes==true) and returns an error if it isn't set.
//
// Generated platform commands call this for destructive actions (delete,
// erase, etc.) before performing the request.
func ConfirmAction(action, target string, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("%s on %q requires --yes when stdin is not a terminal", action, target)
	}
	fmt.Fprintf(os.Stderr, "About to %s %q. Continue? [y/N] ", action, target)
	reader := bufio.NewReader(os.Stdin)
	resp, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	resp = strings.ToLower(strings.TrimSpace(resp))
	if resp != "y" && resp != "yes" {
		return fmt.Errorf("%s aborted", action)
	}
	return nil
}
