package commands

import "github.com/spf13/cobra"

// commandAliases maps command names to their short aliases.
var commandAliases = map[string][]string{
	"computers":      {"comp"},
	"mobile-devices": {"md"},
	"scripts":        {"scr"},
	"buildings":      {"bld"},
	"categories":     {"cat"},
	"departments":    {"dept"},
	"config":         {"cfg"},
}

// applyAliases appends Aliases to any root subcommand that has a mapping.
func applyAliases(root *cobra.Command) {
	for _, cmd := range root.Commands() {
		if aliases, ok := commandAliases[cmd.Name()]; ok {
			cmd.Aliases = append(cmd.Aliases, aliases...)
		}
	}
}
