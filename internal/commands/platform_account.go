// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// usGatewayURL is the only gateway that serves the Jamf Account APIs.
const usGatewayURL = "https://us.api.jamfcloud.com"

// newAccountCmds builds the Jamf Account command tree — licensing, partners and
// SSO — with the US-only guard and help text applied to every leaf.
//
// These three specs are organization-scoped: the organization is resolved from
// the access token, so no scope header is sent at all and a profile carrying a
// tenant or environment ID is the wrong credential for them.
func newAccountCmds(cliCtx *registry.CLIContext) []*cobra.Command {
	cmds := []*cobra.Command{
		platformgen.NewAccountLicensesCmd(cliCtx),
		platformgen.NewDealRegistrationsCmd(cliCtx),
		platformgen.NewDistributorConfigurationCmd(cliCtx),
		platformgen.NewDistributorPurchaseOrdersCmd(cliCtx),
		platformgen.NewDistributorQuotesCmd(cliCtx),
		platformgen.NewSsoConnectionsCmd(cliCtx),
		platformgen.NewSsoDomainsCmd(cliCtx),
	}
	for _, cmd := range cmds {
		applyAccountUSOnly(cliCtx, cmd)
	}
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd.Name(), "distributor-") {
			applyDistributorNote(cmd)
		}
	}
	return cmds
}

// accountUSOnlyHelp is appended to every Jamf Account command's Long text.
//
// The help text carries the constraint as well as the runtime guard because a
// reader working out which profile to make needs it before they run anything —
// and because `--help` is reachable without a profile at all.
const accountUSOnlyHelp = `
The Jamf Account APIs are US-only. They are organization-level services that
Jamf serves from one region by design, so this command works only with a
profile whose gateway is ` + usGatewayURL + `. The EU and APAC gateways answer a
bare "404 page not found" for these paths.

The credential must also be organization-scoped — created at the organization
level in Jamf Account, with neither a tenant ID nor an environment ID on the
profile. The organization is resolved from the access token, so no scope header
is sent.`

// applyAccountUSOnly appends the US-only note to a command and every
// descendant, and wraps each leaf's RunE with the region guard.
//
// The guard exists because the wire symptom is actively misleading: outside the
// US the gateway answers Tyk's bare "404 page not found" — no traceId, no
// error envelope, nothing naming a region — which reads as a wrong path or a
// withdrawn endpoint rather than as "this API is not served here". Wire-checked
// 2026-08-31 with an organization-scoped US credential: every account path
// answered on us.api.jamfcloud.com and 404 on both eu and apac.
func applyAccountUSOnly(cliCtx *registry.CLIContext, cmd *cobra.Command) {
	cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n" + accountUSOnlyHelp

	if cmd.RunE != nil {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			if err := requireUSGateway(cliCtx); err != nil {
				return err
			}
			return inner(c, args)
		}
	}
	for _, sub := range cmd.Commands() {
		applyAccountUSOnly(cliCtx, sub)
	}
}

// requireUSGateway refuses a Jamf Account call from a profile pointing at any
// gateway but the US one.
//
// A nil platform client is not this function's failure to report: --scaffold
// runs without auth (it sends nothing), and every other unauthenticated path
// already has a clearer error waiting in platform.RequirePlatformClient. So the
// check passes through rather than inventing a region complaint for a profile
// that was never resolved.
func requireUSGateway(cliCtx *registry.CLIContext) error {
	if cliCtx == nil || cliCtx.PlatformSDKClient == nil {
		return nil
	}
	base := strings.TrimSuffix(cliCtx.PlatformSDKClient.BaseURL(), "/")
	if strings.EqualFold(base, usGatewayURL) {
		return nil
	}
	return fmt.Errorf("the Jamf Account APIs are served only from the US gateway (%s); this profile uses %s\n\n"+
		"These are organization-level services Jamf serves from one region by design, not a rollout gap: "+
		"the EU and APAC gateways answer a bare \"404 page not found\" for these paths, which is "+
		"indistinguishable from a wrong URL.\n\n"+
		"Run `jamf-cli platform setup` and choose US, with an organization-scoped credential "+
		"(supply neither a tenant ID nor an environment ID)", usGatewayURL, base)
}

// distributorNote is appended to the three distributor command groups.
//
// Two facts a caller cannot get from the spec. The operations require the
// calling organization to be a registered Jamf distributor — public-apis-oas
// documents a 404 DistributorNotFound for that — and as of 2026-08-31 they do
// not work for anyone, because the answer on the wire is an undocumented
// 400 naming a Skyway OAuth scope. See distributorScopeNote.
const distributorNote = `
Distributor operations additionally require the calling organization to be a
registered Jamf distributor. As of 2026-08-31 they answer an undocumented 400
for every caller — see the note the command prints on that error.`

func applyDistributorNote(cmd *cobra.Command) {
	cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n" + distributorNote
	if cmd.RunE != nil {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return annotateDistributorScopeError(inner(c, args))
		}
	}
	for _, sub := range cmd.Commands() {
		applyDistributorNote(sub)
	}
}

// distributorScopeNote explains the leaked upstream error the distributor
// endpoints answer, because the untreated form sends the operator hunting for a
// scope no credential can hold.
//
// The wire answer is 400 {"error":"invalid_scope","error_description":"Invalid
// scopes: skyway-use2-product"}. That is not the caller's scope: the gateway's
// own /auth/token echoes back verbatim whatever scope was requested and refuses
// it, so the string names what jamf-account asked *its* issuer for and was
// refused — and `use2` is the dev deployment, where prod's Skyway definitions
// are use1. Probed 2026-08-31: requesting skyway-use1-product,
// skyway-product or account-api-external-product-us from
// us.api.jamfcloud.com/auth/token is refused identically, and prod's Skyway
// api-definitions are tagged internal-use1 against a different JWT source, so
// no external customer or distributor credential can hold one. Nothing on the
// caller's side changes this answer.
const distributorScopeNote = "\n\nnote: this is an upstream fault, not a problem with your credential or " +
	"privileges. The scope named in the error is the one Jamf Account asked its own token issuer " +
	"for and was refused — prod requests a dev-deployment Skyway scope — so no credential, " +
	"privilege grant or distributor registration changes the answer. Report it to Jamf with the " +
	"traceId if one is present."

// annotateDistributorScopeError appends distributorScopeNote to the leaked
// invalid_scope error and leaves every other error alone.
//
// Gated on both markers rather than on the status: a 400 from these endpoints
// can legitimately mean a malformed purchase order, and a bare invalid_scope
// from somewhere else is not this bug. A false positive has to cost a paragraph
// rather than a confidently wrong exclusive explanation.
func annotateDistributorScopeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid_scope") || !strings.Contains(msg, "skyway") {
		return err
	}
	return fmt.Errorf("%s%s", msg, distributorScopeNote)
}
