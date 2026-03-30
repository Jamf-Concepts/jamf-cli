package commands

import "github.com/spf13/cobra"

// commandAliases maps Jamf Pro command names to their short aliases.
// Applied to children of the "pro" command.
var commandAliases = map[string][]string{
	"computers":      {"comp"},
	"mobile-devices": {"md"},
	"scripts":        {"scr"},
	"buildings":      {"bld"},
	"categories":     {"cat"},
	"departments":    {"dept"},
	"group-tools":    {"gt"},
}

// rootAliases maps root-level command names to short aliases.
var rootAliases = map[string][]string{
	"config": {"cfg"},
}

// applyAliases appends Aliases to any subcommand that has a mapping.
func applyAliases(parent *cobra.Command) {
	for _, cmd := range parent.Commands() {
		if aliases, ok := commandAliases[cmd.Name()]; ok {
			cmd.Aliases = append(cmd.Aliases, aliases...)
		}
	}
}

// applyRootAliases applies aliases to root-level commands.
func applyRootAliases(root *cobra.Command) {
	for _, cmd := range root.Commands() {
		if aliases, ok := rootAliases[cmd.Name()]; ok {
			cmd.Aliases = append(cmd.Aliases, aliases...)
		}
	}
}

// protectAliases maps Jamf Protect command names to their short aliases.
var protectAliases = map[string][]string{
	"removable-storage-control-sets": {"rscs"},
	"unified-logging-filters":        {"ulf"},
	"exception-sets":                 {"es"},
	"analytic-sets":                  {"as"},
	"action-configs":                 {"ac"},
	"custom-prevent-lists":           {"cpl"},
	"api-clients":                    {"apic"},
	"config-freeze":                  {"cf"},
	"computers":                      {"comp"},
	"data-forwarding":                {"df"},
	"data-retention":                 {"dr"},
}

// applyProtectAliases appends aliases to protect subcommands.
func applyProtectAliases(parent *cobra.Command) {
	for _, cmd := range parent.Commands() {
		if aliases, ok := protectAliases[cmd.Name()]; ok {
			cmd.Aliases = append(cmd.Aliases, aliases...)
		}
	}
}
