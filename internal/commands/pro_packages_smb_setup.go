// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

// smbPasswordField is the keychain account suffix for a distribution point's
// write password. It carries the distribution point's id rather than its name:
// a name may hold a slash or a space, and keychain.ParseRef splits on slashes.
func smbPasswordField(dpID string) string { return "smb-" + dpID + "-password" }

// smbStored is a distribution point's account as saved in the active profile,
// already resolved. found is false when the profile holds nothing for it.
type smbStored struct {
	username, password string
	found              bool
}

// storedSMBCredential reads the account saved for dpName in the named profile.
//
// An absent config file, an unnamed profile or a profile with no entry is not
// an error, and answers found=false: a CI runner has none of them and gets its
// account from JAMFSMB_USERNAME and JAMFSMB_PASSWORD. A saved reference that
// cannot be resolved is an error, because falling through to a prompt would
// hide a keychain that refused the read.
func storedSMBCredential(profileName, dpName string) (smbStored, error) {
	if profileName == "" {
		return smbStored{}, nil
	}
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return smbStored{}, nil //nolint:nilerr // no readable config is the CI case, not a failure
	}
	p, ok := cfg.Profiles[profileName]
	if !ok {
		return smbStored{}, nil
	}
	cred, ok := lookupSMBCredential(p, dpName)
	if !ok {
		return smbStored{}, nil
	}
	out := smbStored{found: true}
	if out.username, err = resolveMaybeRef(cred.Username); err != nil {
		return smbStored{}, fmt.Errorf("reading the saved SMB username for %q: %w", termSafe(dpName), err)
	}
	if out.password, err = config.ResolveSecret(cred.Password); err != nil {
		return smbStored{}, exitcode.Wrap(exitcode.Authentication,
			fmt.Errorf("reading the saved SMB password for %q: %w", termSafe(dpName), err)).
			WithHint("run `jamf-cli pro packages smb-setup --distribution-point " + termSafe(dpName) + "` again, or set " + smbPasswordEnv)
	}
	return out, nil
}

// lookupSMBCredential finds a profile's entry for a distribution point. Names
// are matched case-insensitively, as pickFileShareDP matches them.
func lookupSMBCredential(p config.Profile, dpName string) (config.SMBCredential, bool) {
	for name, c := range p.SMBCredentials {
		if strings.EqualFold(name, dpName) {
			return c, true
		}
	}
	return config.SMBCredential{}, false
}

// resolveMaybeRef resolves a value that is a reference, and passes a literal
// through: a username is not a secret, so it may be written plainly.
func resolveMaybeRef(v string) (string, error) {
	for _, prefix := range []string{"env:", "file:", "keychain:"} {
		if strings.HasPrefix(v, prefix) {
			return config.ResolveSecret(v)
		}
	}
	return v, nil
}

// smbDPNames lists the distribution points a profile holds an account for,
// sorted. It is what `config list` shows: names only, never a value.
func smbDPNames(p config.Profile) []string {
	if len(p.SMBCredentials) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.SMBCredentials))
	for n := range p.SMBCredentials {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func newPackagesSMBSetupCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		dpName   string
		noVerify bool
		remove   bool
	)
	cmd := &cobra.Command{
		Use:   "smb-setup",
		Short: "Save the account that writes to an SMB distribution point",
		Long: `Save the account 'pro packages upload' uses to write a package to an SMB
distribution point, in the active profile.

Jamf Pro never returns that account's password, so it has to come from you. This
command prompts for the username and password, signs in to the share to prove
them, then stores the password in the system keychain and writes only a
reference to it, with the username, to the profile. No password is accepted as
a flag, an environment variable or on stdin here, and --no-input is refused.

The distribution point is the one named by --distribution-point, else the
master SMB one. One profile can hold an account for each.

A CI run does not use this command. It sets JAMFSMB_USERNAME and
JAMFSMB_PASSWORD, which take priority over anything saved.

--remove deletes the saved account and its keychain item.`,
		Example: `  # Save the account for the master SMB distribution point
  jamf-cli pro packages smb-setup

  # Save one for a named distribution point
  jamf-cli pro packages smb-setup --distribution-point "Branch Office"

  # Forget it
  jamf-cli pro packages smb-setup --distribution-point "Branch Office" --remove`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			noInput, _ := cmd.Flags().GetBool("no-input")
			if cliCtx.ProfileName == "" {
				return exitcode.New(exitcode.Usage, "no profile is in effect: smb-setup saves the account into one").
					WithHint("run `jamf-cli config add-profile` first, or pass -p <profile>")
			}
			if !remove && (noInput || !term.IsTerminal(int(os.Stdin.Fd()))) {
				return exitcode.New(exitcode.Usage, "smb-setup prompts for the password and does not run without a terminal").
					WithHint("in CI, set " + smbUsernameEnv + " and " + smbPasswordEnv + " instead")
			}
			dp, err := resolveFileShareDP(ctx, cliCtx.Client, dpName)
			if err != nil {
				return err
			}
			if remove {
				return forgetSMBCredential(cliCtx.ProfileName, dp)
			}
			return saveSMBCredential(ctx, cliCtx.ProfileName, dp, noVerify)
		},
	}
	cmd.Flags().StringVar(&dpName, "distribution-point", "", "the SMB distribution point to save the account for, by its Jamf Pro name (default: the master SMB distribution point)")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "save without signing in to the share first")
	cmd.Flags().BoolVar(&remove, "remove", false, "delete the saved account and its keychain item instead")
	return cmd
}

func saveSMBCredential(ctx context.Context, profileName string, dp fileShareDP, noVerify bool) error {
	user, password, err := promptSMBAccount(os.Stderr, bufio.NewReader(os.Stdin), dp)
	if err != nil {
		return err
	}

	target := smbTarget(dp, user, password, nil)
	if noVerify {
		fmt.Fprintln(os.Stderr, "Skipping verification (--no-verify).")
	} else {
		fmt.Fprint(os.Stderr, "Verifying credentials... ")
		if err := smbupload.Verify(ctx, target); err != nil {
			fmt.Fprintln(os.Stderr, "✗")
			return fmt.Errorf("%w\n\njamf-cli did not save the account. Add --no-verify to save it without checking", err)
		}
		fmt.Fprintln(os.Stderr, "✓")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	p, ok := cfg.Profiles[profileName]
	if !ok {
		return exitcode.New(exitcode.Usage, fmt.Sprintf("profile %q is not in the config file", termSafe(profileName))).
			WithHint("smb-setup saves into a profile; create one with `jamf-cli config add-profile`")
	}

	store := config.GetKeychainStore()
	field := smbPasswordField(dp.id)
	if err := store.Set(keychain.DefaultService, profileName+"/"+field, password); err != nil {
		return keychain.WriteError("SMB password", err)
	}

	// Merge into the profile: assigning a fresh map would drop the account of
	// every other distribution point.
	creds := make(map[string]config.SMBCredential, len(p.SMBCredentials)+1)
	for name, c := range p.SMBCredentials {
		if !strings.EqualFold(name, dp.name) {
			creds[name] = c
		}
	}
	creds[dp.name] = config.SMBCredential{Username: user, Password: keychain.KeychainRef(profileName, field)}
	p.SMBCredentials = creds
	cfg.Profiles[profileName] = p
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Saved the account for %s in profile %q. The password is in the keychain.\n", dp.label(), termSafe(profileName))
	return nil
}

func forgetSMBCredential(profileName string, dp fileShareDP) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	p, ok := cfg.Profiles[profileName]
	if !ok {
		return exitcode.New(exitcode.Usage, fmt.Sprintf("profile %q is not in the config file", termSafe(profileName)))
	}
	saved, had := lookupSMBCredential(p, dp.name)
	if !had {
		fmt.Fprintf(os.Stderr, "Profile %q holds no account for %s.\n", termSafe(profileName), dp.label())
		return nil
	}
	// Only an item this command wrote is deleted. A password the user pointed
	// at with env: or file: is theirs.
	if saved.Password == keychain.KeychainRef(profileName, smbPasswordField(dp.id)) {
		err := config.GetKeychainStore().Delete(keychain.DefaultService, profileName+"/"+smbPasswordField(dp.id))
		if err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return keychain.WriteError("SMB password", err)
		}
	}
	creds := make(map[string]config.SMBCredential, len(p.SMBCredentials))
	for name, c := range p.SMBCredentials {
		if !strings.EqualFold(name, dp.name) {
			creds[name] = c
		}
	}
	if len(creds) == 0 {
		creds = nil
	}
	p.SMBCredentials = creds
	cfg.Profiles[profileName] = p
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Removed the saved account for %s from profile %q.\n", dp.label(), termSafe(profileName))
	return nil
}

// promptSMBAccount reads the account for dp. The username defaults to the one
// on the distribution point record; the password is read without echo and is
// never defaulted.
func promptSMBAccount(w *os.File, reader *bufio.Reader, dp fileShareDP) (user, password string, err error) {
	_, _ = fmt.Fprintf(w, "Enter the account that can write to %s\n", termSafe(`\\`+dp.host+`\`+dp.share))
	if dp.username != "" {
		_, _ = fmt.Fprintf(w, "Username [%s]: ", termSafe(dp.username))
	} else {
		_, _ = fmt.Fprint(w, "Username: ")
	}
	line, rerr := reader.ReadString('\n')
	if rerr != nil && line == "" {
		return "", "", fmt.Errorf("reading username: %w", rerr)
	}
	user = strings.TrimSpace(line)
	if user == "" {
		user = dp.username
	}
	if user == "" {
		return "", "", errors.New("username is required")
	}
	_, _ = fmt.Fprint(w, "Password: ")
	b, perr := term.ReadPassword(int(os.Stdin.Fd()))
	if perr != nil {
		return "", "", fmt.Errorf("reading password: %w", perr)
	}
	_, _ = fmt.Fprintln(w)
	if len(b) == 0 {
		return "", "", errors.New("password is required")
	}
	return user, string(b), nil
}
