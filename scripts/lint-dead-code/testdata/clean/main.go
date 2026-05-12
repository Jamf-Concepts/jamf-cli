// Copyright 2026, Jamf Software LLC

//go:build ignore

package clean

import (
	"github.com/spf13/cobra"
)

var (
	cleanName string
	cleanVerb bool
)

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "clean",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cleanName
			_ = cleanVerb
			return nil
		},
	}
	cmd.Flags().StringVar(&cleanName, "name", "", "the name")
	cmd.Flags().BoolVar(&cleanVerb, "verbose", false, "verbose")
	helper()
	return cmd
}

func helper() {
	newCmd()
}
