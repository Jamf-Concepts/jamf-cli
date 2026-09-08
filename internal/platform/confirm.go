// Copyright 2026, Jamf Software LLC

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// ErrNoPlatformClient is returned, wrapped, when no platform client could be
// built. It exists so a caller that knows *why* one is missing can add that
// reason: the message below lists the credentials to supply, and there is one
// path where every one of them is already present and only the scope level was
// dropped. See AnnotateScopeLevelError.
var ErrNoPlatformClient = errors.New("this command requires platform gateway auth")

// RequirePlatformClient returns a descriptive error when client is nil.
// Generated platform commands call this at the top of RunE so users get
// clear setup guidance instead of a nil-pointer panic.
func RequirePlatformClient(client *jamfplatform.Client) error {
	if client == nil {
		return fmt.Errorf("%w\n\n"+
			"Set up a platform profile:\n"+
			"  jamf-cli config add-profile <name> --auth-method platform --url <gateway-url> --tenant-id <id>\n\n"+
			"Or use environment variables:\n"+
			"  JAMF_URL, JAMF_CLIENT_ID, JAMF_CLIENT_SECRET, JAMF_TENANT_ID", ErrNoPlatformClient)
	}
	return nil
}

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
