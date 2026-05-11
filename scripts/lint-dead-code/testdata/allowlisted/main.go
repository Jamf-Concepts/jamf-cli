// Copyright 2026, Jamf Software LLC

//go:build ignore

package allowlisted

import (
	"github.com/spf13/cobra"
)

var (
	usedName    string
	allowedFlag string
)

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "demo",
		Annotations: map[string]string{
			"lint:keep-flag": "stale",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = usedName
			return nil
		},
	}
	cmd.Flags().StringVar(&usedName, "name", "", "used name")
	cmd.Flags().StringVar(&allowedFlag, "stale", "", "flag allowlisted via annotation")
	register()
	return cmd
}

func register() {
	newCmd()
}

// keptOrphan would be dead but is allowlisted via the //lint:keep marker.
//
//lint:keep
func keptOrphan() {
	_ = "kept"
}
