// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

// securityCredentialPair prompts for one Security Cloud API's application
// ID/secret, returning ("", "", nil) if the user skips it by leaving the ID
// blank. label is the human-readable API name shown in prompts (e.g. "Risk").
// Chains through the same *bufio.Reader across three calls (Risk, Device
// Lifecycle, SSE) — a read failure must surface rather than be silently
// treated as a skip, and the reader must not be holding buffered type-ahead
// when control switches to term.ReadPassword's raw-fd read, or the two would
// desync.
func securityCredentialPair(out *os.File, reader *bufio.Reader, label string) (id, secret string, err error) {
	_, _ = fmt.Fprintf(out, "%s API application ID (leave blank to skip): ", label)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", fmt.Errorf("reading %s API application ID: %w", label, err)
	}
	id = strings.TrimSpace(line)
	if id == "" {
		return "", "", nil
	}

	_, _ = fmt.Fprintf(out, "%s API application secret: ", label)
	if reader.Buffered() != 0 {
		return "", "", fmt.Errorf("unexpected buffered input before reading %s API application secret; input may be desynchronized", label)
	}
	secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", "", fmt.Errorf("reading %s API application secret: %w", label, err)
	}
	_, _ = fmt.Fprintln(out) // newline after hidden input
	secret = string(secretBytes)
	if secret == "" {
		return "", "", fmt.Errorf("%s API application secret is required once an application ID is given", label)
	}
	return id, secret, nil
}

func newSecuritySetupCmd() *cobra.Command {
	var setupProfile string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure a Jamf Security Cloud profile with API credentials",
		Long: `Prompts for Jamf Security Cloud (Radar) application credentials, then
saves them as a config profile. Secrets are stored in the system keychain.

Unlike Jamf Pro/Protect/School, Jamf Security Cloud provisions a separate
application ID/secret pair per API — Risk, Device Lifecycle, and Shared
Signals & Events each have their own "Security Integration" under
Settings > Security Integrations in the Radar portal. Configure whichever
you have access to; leave an application ID blank to skip that API. At
least one pair is required.

There's no per-tenant URL to configure — all three APIs share Jamf's global
production host, and tenancy is carried inside the credentials themselves.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := os.Stdout
			reader := bufio.NewReader(os.Stdin)

			// Profile name
			if setupProfile == "" {
				if noInput {
					setupProfile = "security"
				} else {
					_, _ = fmt.Fprint(out, "Profile name [security]: ")
					line, _ := reader.ReadString('\n')
					setupProfile = strings.TrimSpace(line)
					if setupProfile == "" {
						setupProfile = "security"
					}
				}
			}

			// Credentials are always collected interactively to prevent
			// exposure in shell history and process listings.
			if noInput {
				return fmt.Errorf("setup requires interactive input for credentials; cannot use --no-input")
			}

			riskID, riskSecret, err := securityCredentialPair(out, reader, "Risk")
			if err != nil {
				return err
			}
			lifecycleID, lifecycleSecret, err := securityCredentialPair(out, reader, "Device Lifecycle")
			if err != nil {
				return err
			}
			sseID, sseSecret, err := securityCredentialPair(out, reader, "Shared Signals & Events")
			if err != nil {
				return err
			}

			if riskID == "" && lifecycleID == "" && sseID == "" {
				return fmt.Errorf("at least one API's credentials are required (Risk, Device Lifecycle, or Shared Signals & Events)")
			}

			// Store secrets in keychain
			store := config.GetKeychainStore()
			prof := config.Profile{
				Product:    "security",
				AuthMethod: "security",
			}
			if riskID != "" {
				if err := store.Set(keychain.DefaultService, setupProfile+"/risk-client-id", riskID); err != nil {
					return keychain.WriteError("Risk API application ID", err)
				}
				if err := store.Set(keychain.DefaultService, setupProfile+"/risk-client-secret", riskSecret); err != nil {
					return keychain.WriteError("Risk API application secret", err)
				}
				prof.RiskClientID = keychain.KeychainRef(setupProfile, "risk-client-id")
				prof.RiskClientSecret = keychain.KeychainRef(setupProfile, "risk-client-secret")
			}
			if lifecycleID != "" {
				if err := store.Set(keychain.DefaultService, setupProfile+"/lifecycle-client-id", lifecycleID); err != nil {
					return keychain.WriteError("Device Lifecycle API application ID", err)
				}
				if err := store.Set(keychain.DefaultService, setupProfile+"/lifecycle-client-secret", lifecycleSecret); err != nil {
					return keychain.WriteError("Device Lifecycle API application secret", err)
				}
				prof.LifecycleClientID = keychain.KeychainRef(setupProfile, "lifecycle-client-id")
				prof.LifecycleClientSecret = keychain.KeychainRef(setupProfile, "lifecycle-client-secret")
			}
			if sseID != "" {
				if err := store.Set(keychain.DefaultService, setupProfile+"/sse-client-id", sseID); err != nil {
					return keychain.WriteError("Shared Signals & Events application ID", err)
				}
				if err := store.Set(keychain.DefaultService, setupProfile+"/sse-client-secret", sseSecret); err != nil {
					return keychain.WriteError("Shared Signals & Events application secret", err)
				}
				prof.SSEClientID = keychain.KeychainRef(setupProfile, "sse-client-id")
				prof.SSEClientSecret = keychain.KeychainRef(setupProfile, "sse-client-secret")
			}

			// Save profile to config
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cfg.Profiles[setupProfile] = prof
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(out, "\n✓ Profile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintln(out, "  Product:       Jamf Security Cloud")
			if riskID != "" {
				_, _ = fmt.Fprintf(out, "  Risk API:              application ID %s, secret stored in system keychain\n", riskID)
			}
			if lifecycleID != "" {
				_, _ = fmt.Fprintf(out, "  Device Lifecycle API:  application ID %s, secret stored in system keychain\n", lifecycleID)
			}
			if sseID != "" {
				_, _ = fmt.Fprintf(out, "  Shared Signals & Events: application ID %s, secret stored in system keychain\n", sseID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"security\")")

	return cmd
}
