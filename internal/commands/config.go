package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ktn-jamf/jamfpro-cli/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration and profiles",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigAddProfileCmd())
	cmd.AddCommand(newConfigRemoveProfileCmd())
	cmd.AddCommand(newConfigSetDefaultCmd())
	cmd.AddCommand(newConfigSetupCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show resolved configuration with sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "# Config file: %s\n", config.ConfigPath())

			data, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("marshalling config for display: %w", err)
			}

			fmt.Fprint(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), config.ConfigPath())
		},
	}
}

func newConfigAddProfileCmd() *cobra.Command {
	var (
		profileURL       string
		authMethod       string
		profileTok       string
		profileClientID  string
		profileClientSec string
	)

	cmd := &cobra.Command{
		Use:   "add-profile <name>",
		Short: "Add or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Validate auth-method
			validMethods := map[string]bool{"token": true, "basic": true, "oauth2": true}
			if !validMethods[authMethod] {
				return fmt.Errorf("invalid --auth-method %q: must be token, basic, or oauth2", authMethod)
			}

			// Validate auth-method-specific requirements
			if authMethod == "oauth2" && profileClientID == "" {
				return fmt.Errorf("--client-id is required when --auth-method is oauth2")
			}
			if authMethod == "oauth2" && profileClientSec == "" {
				return fmt.Errorf("--client-secret is required when --auth-method is oauth2")
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			p := config.Profile{
				URL:          profileURL,
				AuthMethod:   authMethod,
				Token:        profileTok,
				ClientID:     profileClientID,
				ClientSecret: profileClientSec,
			}

			cfg.Profiles[name] = p

			// Auto-set as default if this is the first profile
			if len(cfg.Profiles) == 1 {
				cfg.DefaultProfile = name
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Profile %q saved.\n", name)
			if cfg.DefaultProfile == name {
				fmt.Fprintf(cmd.OutOrStdout(), "Set as default profile.\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profileURL, "url", "", "Jamf Pro server URL")
	cmd.Flags().StringVar(&authMethod, "auth-method", "token", "authentication method: token, basic, oauth2")
	cmd.Flags().StringVar(&profileTok, "token", "", "API token (literal, env:VAR, or file:/path)")
	cmd.Flags().StringVar(&profileClientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&profileClientSec, "client-secret", "", "OAuth2 client secret (literal, env:VAR, or file:/path)")

	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func newConfigRemoveProfileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-profile <name>",
		Short: "Remove a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found", name)
			}

			delete(cfg.Profiles, name)

			// Clear default if we just removed it
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = ""
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Profile %q removed.\n", name)
			return nil
		},
	}
}

func newConfigSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found in config", name)
			}

			cfg.DefaultProfile = name

			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Default profile set to %q.\n", name)
			return nil
		},
	}
}
