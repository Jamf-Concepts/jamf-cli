// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

// smbSession is a share and the account to write it with, resolved once by
// upload-multiple and carried in the context so each upload skips the lookup,
// the prompt and the preflight.
type smbSession struct {
	dp  fileShareDP
	tgt smbupload.Target
}

type smbSessionKey struct{}

func newPackagesUploadMultipleCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		categoryID string
		priority   int
		flagYes    bool
		flagDP     string
	)

	cmd := &cobra.Command{
		Use:   "upload-multiple <file>...",
		Short: "Upload several package files to Jamf Pro",
		Long: `Upload each named package file as "pro packages upload" uploads one: the same
route (Jamf Cloud, the Jamf Pro API or an SMB file share distribution point),
the same hash or size check, and a package record created or replaced per file.

The distribution point and its account are resolved once, and an SMB share is
checked once, before the first file is sent, Xonce and a wrong account stops the run before anything is created. Every file is
checked to exist before the first upload. Files go one after another; a file
that fails does not stop the rest, the run ends non-zero and names each failure
(--allow-partial-failure turns a partial failure into a warning).

Each package takes its file name as its display name; use "pro packages upload"
for a custom --name. --category-id and --priority apply to every file.`,
		Example: `  # Upload two packages
  jamf-cli pro packages upload-multiple ./Firefox-134.0.pkg ./Chrome-131.pkg

  # Upload every package in a folder to an SMB distribution point
  jamf-cli pro packages upload-multiple ./pkgs/*.pkg --distribution-point "Branch Office" --yes`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, files []string) error {
			ctx := cmd.Context()

			seen := map[string]string{}
			for _, f := range files {
				info, err := os.Stat(f)
				if err != nil {
					return fmt.Errorf("cannot access file: %w", err)
				}
				if info.IsDir() {
					return fmt.Errorf("%s is a directory, not a file", f)
				}
				key := strings.ToLower(filepath.Base(f))
				if prev, dup := seen[key]; dup {
					return exitcode.New(exitcode.Usage,
						fmt.Sprintf("%s and %s share the file name %q: Jamf Pro keys a package on its file name, so one would replace the other", termSafe(prev), termSafe(f), termSafe(filepath.Base(f))))
				}
				seen[key] = f
			}

			dp, err := selectFileShareDP(ctx, cliCtx, flagDP)
			if err != nil {
				return err
			}
			if dp != nil {
				session := &smbSession{dp: *dp}
				if !dryRun {
					stored, serr := storedSMBCredential(cliCtx.ProfileName, dp.name)
					if serr != nil {
						return serr
					}
					noInput, _ := cmd.Flags().GetBool("no-input")
					user, pw, krb, aerr := resolveSMBAuth(os.Stderr, bufio.NewReader(os.Stdin), *dp, stored, noInput)
					if aerr != nil {
						return aerr
					}
					defer krb.close()
					session.tgt = smbTarget(*dp, user, pw, krb)
					auth := "password"
					if krb != nil {
						auth = "Kerberos"
					}
					fmt.Fprintf(os.Stderr, "Distribution point: %s (SMB, %s, %s)\n", dp.label(), termSafe(session.tgt.UNC()), auth)
					if err := smbupload.Preflight(ctx, session.tgt); err != nil {
						return fmt.Errorf("checking %s: %w", termSafe(session.tgt.UNC()), err)
					}
				}
				ctx = context.WithValue(ctx, smbSessionKey{}, session)
				cmd.SetContext(ctx)
			}

			var ok, failed int
			var firstErr error
			var failures []string
			for i, f := range files {
				if ctx.Err() != nil {
					break
				}
				fmt.Fprintf(os.Stderr, "\n[%d/%d] %s\n", i+1, len(files), termSafe(filepath.Base(f)))
				// A fresh command per file: the single upload keeps the display
				// name it derived in a closure, and would reuse the first one.
				one := newPackagesUploadCmd(cliCtx)
				for k, v := range map[string]string{
					"file":        f,
					"category-id": categoryID,
					"priority":    strconv.Itoa(priority),
					"yes":         strconv.FormatBool(flagYes),
				} {
					if err := one.Flags().Set(k, v); err != nil {
						return err
					}
				}
				if dp == nil && flagDP != "" {
					_ = one.Flags().Set("distribution-point", flagDP)
				}
				if err := one.RunE(cmd, nil); err != nil {
					failed++
					if firstErr == nil {
						firstErr = err
					}
					failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
					fmt.Fprintf(os.Stderr, "upload of %s failed: %v\n", termSafe(f), termSafe(err))
					continue
				}
				ok++
			}

			fmt.Fprintf(os.Stderr, "\n%d of %d packages uploaded\n", ok, len(files))
			if failed == 0 {
				return nil
			}
			for _, line := range failures {
				fmt.Fprintf(os.Stderr, "  failed: %s\n", termSafe(line))
			}
			if allow, _ := cmd.Flags().GetBool("allow-partial-failure"); ok > 0 && allow {
				fmt.Fprintf(os.Stderr, "warning: %d of %d packages failed; continuing (--allow-partial-failure)\n", failed, ok+failed)
				return nil
			}
			return exitcode.PartialOrPropagate(ok, failed, firstErr, fmt.Sprintf("%d of %d package uploads failed", failed, ok+failed))
		},
	}

	cmd.Flags().StringVar(&categoryID, "category-id", "-1", "category ID for every package")
	cmd.Flags().IntVar(&priority, "priority", 10, "installation priority for every package (1-20)")
	cmd.Flags().StringVar(&flagDP, "distribution-point", "", "upload to this SMB distribution point, by its Jamf Pro name (default: the master SMB distribution point when the tenant has no cloud distribution point)")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing package")

	return cmd
}
