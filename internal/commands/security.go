// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	securitygen "github.com/Jamf-Concepts/jamf-cli/internal/commands/security/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newSecurityCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Jamf Security Cloud commands",
		Long:  "Commands for interacting with Jamf Security Cloud (Radar) — device risk, device lifecycle, Shared Signals & Events stream configuration, DNS and ZTNA policy, content categories, device groups, and UEM Connect.",
	}

	// Hand-written: credential setup owns business logic (prompting for up
	// to three independent API credential pairs) that isn't a spec-driven
	// CRUD operation.
	cmd.AddCommand(newSecuritySetupCmd())

	// Generated: every Risk/Device Lifecycle/SSE operation maps cleanly to a
	// single HTTP call, so — per the same contract Platform commands follow —
	// none of it is hand-written.
	cmd.AddCommand(securitygen.NewRiskCmd(cliCtx))
	cmd.AddCommand(securitygen.NewDeviceLifecycleCmd(cliCtx))
	cmd.AddCommand(securitygen.NewStreamCmd(cliCtx))
	cmd.AddCommand(securitygen.NewStatusCmd(cliCtx))
	cmd.AddCommand(securitygen.NewVerificationCmd(cliCtx))
	cmd.AddCommand(securitygen.NewJwksCmd(cliCtx))
	cmd.AddCommand(securitygen.NewWellKnownCmd(cliCtx))

	// Generated from the Security Cloud specs served on the platform gateway
	// (/api/securitycloud) rather than on api.wandera.com, so these reach the
	// product through platform auth and a Security Cloud tenant ID instead of
	// the per-API scoped credentials the commands above use. Same namespace
	// regardless: it is one product to whoever is typing, and `pro` already
	// mixes two auth paths under one namespace the same way.
	//
	// Each is runtime-gated by platform.RequirePlatformClient, so a profile
	// configured only for Risk/Lifecycle/SSE still gets a usable `security`
	// tree — these subcommands just report what to configure.
	cmd.AddCommand(platformgen.NewDnsZonesCmd(cliCtx))
	cmd.AddCommand(platformgen.NewDnsSearchDomainsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewDnsCustomHostnameMappingsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewZtnaAppsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewZtnaGatewaysCmd(cliCtx))
	cmd.AddCommand(platformgen.NewZtnaGroupedGatewaysCmd(cliCtx))
	cmd.AddCommand(platformgen.NewZtnaSharedGatewaysCmd(cliCtx))
	cmd.AddCommand(platformgen.NewZtnaPredefinedAppsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewContentCategoriesCmd(cliCtx))
	cmd.AddCommand(platformgen.NewDeviceGroupsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewUemConnectorsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewUemConnectorEnablementCmd(cliCtx))
	cmd.AddCommand(platformgen.NewUemSyncSettingsCmd(cliCtx))
	cmd.AddCommand(platformgen.NewUemSyncCmd(cliCtx))
	cmd.AddCommand(platformgen.NewUemActivationProfilesCmd(cliCtx))

	applySecurityAliases(cmd)
	applySecurityGroups(cmd)

	return cmd
}
