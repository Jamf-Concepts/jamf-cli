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

// mergeSecurityProfileBase starts the Radar half of a profile from whatever the
// profile already holds, so the gateway credentials `platform setup` writes are
// not zeroed — see the matching note on mergePlatformProfile.
//
// Product and auth-method are only filled when unset, so running `security
// setup` second against a platform profile does not demote it: auth-method
// "platform" is what ResolveAuthForProfile reads to enter gateway auth for the
// `pro` and `platform` namespaces, and nothing requires the value "security" —
// the `security` tree is selected by the command namespace, not by this field.
// writeSecurityCredentialSummary reports each Radar pair's state in the
// profile that was SAVED, not the prompts that were answered.
//
// The setup command merges into an existing profile, so a pair whose
// application ID was left blank keeps its stored keychain references. Listing
// only the pairs entered on this run therefore told an operator who pressed
// Enter at a prompt — as the command's own help instructs — that the pair was
// gone, while `security stream get` went on using the stored credential and
// failed inside the token exchange. Hand-editing config.yaml was the only way
// out, which is what setup exists to avoid.
//
// The merge itself is load-bearing and stays: a profile carries every
// product's credentials, so replacing it wholesale zeroed whatever
// `platform setup` had written. What changed is that the summary now
// describes the result.
func writeSecurityCredentialSummary(out io.Writer, prof config.Profile, riskID, lifecycleID, sseID string) {
	for _, pair := range []struct {
		label   string
		entered string
		saved   string
	}{
		{"Risk API", riskID, prof.RiskClientID},
		{"Device Lifecycle API", lifecycleID, prof.LifecycleClientID},
		{"Shared Signals & Events", sseID, prof.SSEClientID},
	} {
		switch {
		case pair.entered != "":
			_, _ = fmt.Fprintf(out, "  %-24s application ID %s, secret stored in system keychain\n", pair.label+":", pair.entered)
		case pair.saved != "":
			_, _ = fmt.Fprintf(out, "  %-24s retained from a previous run (unchanged)\n", pair.label+":")
		default:
			_, _ = fmt.Fprintf(out, "  %-24s not configured\n", pair.label+":")
		}
	}
}

func mergeSecurityProfileBase(existing config.Profile) config.Profile {
	p := existing
	if p.Product == "" {
		p.Product = "security"
	}
	if p.AuthMethod == "" {
		p.AuthMethod = "security"
	}
	return p
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

On a re-run, a skipped pair is LEFT AS IT WAS rather than removed — this
command merges into the profile, so it can be run once per API without the
later run discarding the earlier one's credentials (a profile also carries
the platform credentials "jamf-cli platform setup" writes). The closing
summary says which pairs were entered and which were retained. To remove a
pair, delete the profile with "jamf-cli config remove-profile <name>" and
run setup again with only the pairs you want.

There's no per-tenant URL to configure — all three APIs share Jamf's global
production host, and tenancy is carried inside the credentials themselves.

This command covers the Radar APIs only. The rest of Jamf Security Cloud —
dns-*, ztna-*, content-categories, device-groups and uem-* — is served on the
Jamf Platform gateway with platform client credentials and a Security Cloud
tenant ID; configure that with "jamf-cli platform setup".`,
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			store := config.GetKeychainStore()
			prof := mergeSecurityProfileBase(cfg.Profiles[setupProfile])
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

			cfg.Profiles[setupProfile] = prof
			cfg.DefaultProfile = setupProfile

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(out, "\n✓ Profile %q saved and set as default.\n", setupProfile)
			_, _ = fmt.Fprintln(out, "  Product:       Jamf Security Cloud")
			writeSecurityCredentialSummary(out, prof, riskID, lifecycleID, sseID)

			// The rest of Jamf Security Cloud is served on the platform gateway
			// and takes different credentials. `platform setup` owns that; say
			// so here, because `security --help` is where someone configuring
			// this product looks first and nothing else would point them on.
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "The dns-*, ztna-*, content-categories, device-groups and uem-* commands are")
			_, _ = fmt.Fprintln(out, "served on the Jamf Platform gateway and need platform credentials plus a")
			_, _ = fmt.Fprintln(out, "Jamf Security Cloud tenant ID. Configure those with:")
			_, _ = fmt.Fprintln(out, "  jamf-cli platform setup")

			return nil
		},
	}

	cmd.Flags().StringVar(&setupProfile, "profile-name", "", "profile name (default: \"security\")")

	return cmd
}
