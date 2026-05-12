// Copyright 2026, Jamf Software LLC

//go:build ignore

package deadflag

import (
	"github.com/spf13/cobra"
)

var (
	usedName   string
	unusedFlag string
)

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "demo",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = usedName
			return nil
		},
	}
	cmd.Flags().StringVar(&usedName, "name", "", "name (used)")
	cmd.Flags().StringVar(&unusedFlag, "stale", "", "flag never read")
	register()
	return cmd
}

func register() {
	newCmd()
}
