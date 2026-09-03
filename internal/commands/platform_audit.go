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
// reachable only from an *environment*-scoped profile. Wire-checked 2026-08-31
// in US and EU, and the spec now agrees: GitOps build v2056 withdrew
// organization from audit_api.json's x-scope-types and made X-Environment-Id
// required, so the pair this CLI used to contradict is gone. The note below
// survives the correction because the gateway's own 400 still does not name
// the level that would work.
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
// that a scope was not found without saying which kind would be accepted, so
// naming the one level that is accepted is the whole content of the fix.
//
// It used to add that the spec listed organization as allowed while the gateway
// refused it from a header. Build v2056 withdrew organization from audit's
// x-scope-types, so that sentence became false and is gone — the note must not
// tell an operator to distrust a spec that now says the same thing this does.
const auditScopeNote = "\n\nnote: audit is reachable only from an environment-scoped profile. This one sent no " +
	"scope header, which means it is organization-scoped. Set an environment ID on the profile " +
	"(or JAMF_ENVIRONMENT_ID)."

// annotateAuditScopeError appends auditScopeNote to the gateway's
// missing-scope error and leaves every other error alone.
//
// Only REQUEST_CONTEXT_NOT_PROVIDED is annotated. INVALID_REQUEST_CONTEXT_TYPE
// — what a tenant-scoped profile earns — already names both the level it sent
// and the level expected, so there is nothing to add.
// The note is appended with %w, not %s. These wrappers are installed as RunE
// decorators, so they run *before* ClassifyError, EnrichPrivilegeError and
// exitcode.CodeFrom — all of which work by errors.As. Formatting with %s
// flattens the chain, so any error whose text matched lost its classification
// and fell back to exit 1.
func annotateAuditScopeError(err error) error {
	if err == nil || !strings.Contains(err.Error(), "REQUEST_CONTEXT_NOT_PROVIDED") {
		return err
	}
	return fmt.Errorf("%w%s", err, auditScopeNote)
}
