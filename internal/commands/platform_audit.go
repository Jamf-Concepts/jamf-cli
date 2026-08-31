// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newPlatformAuditCmd builds the platform audit command with the scope note applied.
//
// Audit is served in every region — unlike the Jamf Account trio — but it is
// reachable only from an *environment*-scoped profile. The spec says otherwise:
// audit_api.json declares x-scope-types [environment, organization], and the
// gateway's own api-product declares the same pair, yet only environment is
// accepted from a header. Wire-checked 2026-08-31 in US and EU.
func newPlatformAuditCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := platformgen.NewAuditCmd(cliCtx)
	applyAuditScopeNote(cmd)
	return cmd
}

// auditScopeHelp is appended to every audit command's Long text.
const auditScopeHelp = `
Audit needs an environment-scoped profile — one carrying an environment ID.
An organization-scoped profile sends no scope header and the gateway answers
400 REQUEST_CONTEXT_NOT_PROVIDED; a tenant-scoped one answers 400
INVALID_REQUEST_CONTEXT_TYPE. Unlike the Jamf Account commands, audit is served
in every region.`

func applyAuditScopeNote(cmd *cobra.Command) {
	cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n" + auditScopeHelp
	if cmd.RunE != nil {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return annotateAuditScopeError(inner(c, args))
		}
	}
	for _, sub := range cmd.Commands() {
		applyAuditScopeNote(sub)
	}
}

// auditScopeNote names the profile change REQUEST_CONTEXT_NOT_PROVIDED wants.
//
// The gateway's message — "The request context could not be detected." — says
// that a scope was not found without saying which kind would be accepted, and
// the spec's own x-scope-types lists organization alongside environment, so a
// reader who checks the spec concludes their organization-scoped profile should
// have worked. Naming the one level that is accepted is the whole content of
// the fix.
const auditScopeNote = "\n\nnote: audit is reachable only from an environment-scoped profile. This one sent no " +
	"scope header, which means it is organization-scoped. Set an environment ID on the profile " +
	"(or JAMF_ENVIRONMENT_ID). The spec lists organization as an allowed scope for audit, but the " +
	"gateway does not accept it from a header."

// annotateAuditScopeError appends auditScopeNote to the gateway's
// missing-scope error and leaves every other error alone.
//
// Only REQUEST_CONTEXT_NOT_PROVIDED is annotated. INVALID_REQUEST_CONTEXT_TYPE
// — what a tenant-scoped profile earns — already names both the level it sent
// and the level expected, so there is nothing to add.
func annotateAuditScopeError(err error) error {
	if err == nil || !strings.Contains(err.Error(), "REQUEST_CONTEXT_NOT_PROVIDED") {
		return err
	}
	return fmt.Errorf("%s%s", err.Error(), auditScopeNote)
}
