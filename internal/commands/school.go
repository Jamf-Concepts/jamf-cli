// Copyright 2026, Jamf Software LLC

package commands

import (
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newSchoolCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "school",
		Short: "Jamf School commands",
		Long:  "Commands for interacting with Jamf School — education device management, users, classes, and apps.",
	}

	// Core
	cmd.AddCommand(newSchoolSetupCmd())
	cmd.AddCommand(newSchoolOverviewCmd(cliCtx))

	// Devices
	cmd.AddCommand(newSchoolDevicesCmd(cliCtx))
	cmd.AddCommand(newSchoolDeviceGroupsCmd(cliCtx))

	// Users & Organization
	cmd.AddCommand(newSchoolUsersCmd(cliCtx))
	cmd.AddCommand(newSchoolGroupsCmd(cliCtx))
	cmd.AddCommand(newSchoolClassesCmd(cliCtx))

	// Content
	cmd.AddCommand(newSchoolProfilesCmd(cliCtx))
	cmd.AddCommand(newSchoolAppsCmd(cliCtx))

	// Infrastructure
	cmd.AddCommand(newSchoolLocationsCmd(cliCtx))
	cmd.AddCommand(newSchoolIBeaconsCmd(cliCtx))
	cmd.AddCommand(newSchoolDEPDevicesCmd(cliCtx))

	// Apply aliases and groups
	applySchoolAliases(cmd)
	applySchoolGroups(cmd)

	return cmd
}
