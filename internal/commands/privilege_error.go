// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// EnrichPrivilegeError augments a 403 PermissionDenied error's hint with the
// specific Jamf privileges the executed command declares via its
// jamf:privileges annotation (emitted by the generator from
// x-required-privileges). For any other error code, a nil command/error, or a
// command without the annotation, err is returned unchanged.
func EnrichPrivilegeError(cmd *cobra.Command, err error) error {
	if cmd == nil || err == nil {
		return err
	}
	privs := cmd.Annotations["jamf:privileges"]
	if privs == "" {
		return err
	}
	var e *exitcode.Error
	if !errors.As(err, &e) || e.Code != exitcode.PermissionDenied {
		return err
	}
	names := strings.ReplaceAll(privs, ",", ", ")
	if e.Hint != "" {
		e.Hint = strings.TrimRight(e.Hint, ". ") + ". Required privilege(s): " + names
	} else {
		e.Hint = "Required privilege(s): " + names
	}
	return err
}
