// Copyright 2026, Jamf Software LLC

package commands

import (
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

// applyAuditScopeNote appends the help text and nothing else.
//
// The runtime half is gone. annotateAuditScopeError spelled by hand the fact
// x-scope-types now states — audit is environment-only — and it had already
// gone stale once: it used to add that the spec listed organization as allowed
// while the gateway refused it from a header, which build v2056 made false.
// AnnotateScopeLevelError reads the annotation instead, so the sentence cannot
// disagree with the artifact, and every platform command gets it rather than
// the one command whose gap someone happened to hit.
//
// The help text stays hand-written because it carries two things no annotation
// does: the codes each wrong level earns, and that audit is served in every
// region unlike the Jamf Account trio.
func applyAuditScopeNote(cmd *cobra.Command) {
	cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n" + auditScopeHelp
	for _, sub := range cmd.Commands() {
		applyAuditScopeNote(sub)
	}
}
