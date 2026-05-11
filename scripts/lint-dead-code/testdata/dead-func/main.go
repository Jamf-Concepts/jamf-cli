// Copyright 2026, Jamf Software LLC

//go:build ignore

package deadfunc

import (
	"github.com/spf13/cobra"
)

var deadFuncName string

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "demo",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = deadFuncName
			return nil
		},
	}
	cmd.Flags().StringVar(&deadFuncName, "name", "", "name")
	wire()
	return cmd
}

func wire() {
	newCmd()
}

// orphan is never referenced anywhere — should be flagged dead.
func orphan() {
	_ = "no callers"
}
