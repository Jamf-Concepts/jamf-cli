// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/listfile"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

// packageFile is a package record and the file it names on a distribution point.
type packageFile struct {
	id, fileName string
}

// wrapPackagesDeleteSMB extends the generated `packages delete` so it also
// removes the file from an SMB file share distribution point. The generated
// command deletes the record only, which leaves the binary orphaned on a share
// that Jamf Pro never cleans.
//
// The generated command stays in charge of ids, confirmation, the cooldown and
// the record delete; this wrapper decides whether a share is involved, reads
// each record's file name before the record is gone, and removes the file only
// for a record that is verifiably gone afterwards. It selects the share by the
// same rule as `packages upload`: --distribution-point, else the master SMB
// distribution point when the tenant has no cloud distribution point.
func wrapPackagesDeleteSMB(cliCtx *registry.CLIContext, del *cobra.Command) {
	var flagDP string
	del.Flags().StringVar(&flagDP, "distribution-point", "", "also remove the package from this SMB distribution point, by its Jamf Pro name (default: the master SMB distribution point when the tenant has no cloud distribution point)")
	inner := del.RunE
	del.Long = strings.TrimSpace(del.Long) + `

On a tenant with an SMB file share distribution point (named with
--distribution-point, or the master one when the tenant has no cloud
distribution point), the package's file is also removed from the share's
Packages folder, once the record is gone. Jamf Pro never touches the share
itself. The SMB account is found as "pro packages upload" finds it; see that
command. The share is checked before any record is deleted. A file already
absent from the share is reported and is not an error. A failure to remove the
file after the record was deleted exits non-zero and names the file.`

	del.RunE = func(cmd *cobra.Command, args []string) error {
		return deleteWithShareCleanup(cliCtx, cmd, args, flagDP, inner, func() ([]packageFile, error) {
			return packageDeleteTargets(cmd.Context(), cmd, cliCtx.Client, args)
		})
	}
}

// wrapPackagesDeleteMultipleSMB is wrapPackagesDeleteSMB for `delete-multiple`,
// which takes its ids from --ids or a {"ids": [...]} body on stdin. Stdin is
// read only once a share is involved, and its ids are handed on as --ids so the
// generated command, which would otherwise find stdin consumed, still sends
// them.
func wrapPackagesDeleteMultipleSMB(cliCtx *registry.CLIContext, del *cobra.Command) {
	var flagDP string
	del.Flags().StringVar(&flagDP, "distribution-point", "", "also remove the packages from this SMB distribution point, by its Jamf Pro name (default: the master SMB distribution point when the tenant has no cloud distribution point)")
	inner := del.RunE
	del.Long = strings.TrimSpace(del.Long) + `

On a tenant with an SMB file share distribution point (named with
--distribution-point, or the master one when the tenant has no cloud
distribution point), each package's file is also removed from the share's
Packages folder, once its record is gone. See "pro packages delete" for the
rules, which are the same.`

	del.RunE = func(cmd *cobra.Command, args []string) error {
		if scaffold, _ := cmd.Flags().GetBool("scaffold"); scaffold {
			return inner(cmd, args)
		}
		return deleteWithShareCleanup(cliCtx, cmd, args, flagDP, inner, func() ([]packageFile, error) {
			ids, _ := cmd.Flags().GetStringSlice("ids")
			if len(ids) == 0 {
				var err error
				if ids, err = idsFromStdin(); err != nil {
					return nil, err
				}
				if err := cmd.Flags().Set("ids", strings.Join(ids, ",")); err != nil {
					return nil, err
				}
			}
			var out []packageFile
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] {
					continue
				}
				seen[id] = true
				p, err := packageFileFor(cmd.Context(), cliCtx.Client, id, false)
				if err != nil {
					return nil, err
				}
				out = append(out, p)
			}
			return out, nil
		})
	}
}

// idsFromStdin reads the {"ids": [...]} body delete-multiple accepts.
func idsFromStdin() ([]string, error) {
	if st, err := os.Stdin.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return nil, exitcode.New(exitcode.Usage, "no package ids: pass --ids or pipe {\"ids\": [...]}")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading ids from stdin: %w", err)
	}
	var body struct {
		IDs []json.RawMessage `json:"ids"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("stdin is not {\"ids\": [...]}: %w", err)
	}
	var ids []string
	for _, r := range body.IDs {
		ids = append(ids, strings.Trim(strings.TrimSpace(string(r)), `"`))
	}
	if len(ids) == 0 {
		return nil, exitcode.New(exitcode.Usage, "no package ids: the body names none")
	}
	return ids, nil
}

// deleteWithShareCleanup runs a record-deleting command (inner) and, when a
// share is involved, removes the file of every record that is gone afterwards.
// listTargets names those records and is called only once a share is chosen,
// before anything is deleted.
func deleteWithShareCleanup(cliCtx *registry.CLIContext, cmd *cobra.Command, args []string, flagDP string, inner func(*cobra.Command, []string) error, listTargets func() ([]packageFile, error)) error {
	ctx := cmd.Context()
	client := cliCtx.Client
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	dp, err := selectFileShareDP(ctx, cliCtx, flagDP)
	if err != nil {
		return err
	}
	if dp == nil {
		return inner(cmd, args)
	}

	targets, err := listTargets()
	if err != nil {
		return err
	}

	stored, err := storedSMBCredential(cliCtx.ProfileName, dp.name)
	if err != nil {
		return err
	}
	noInput, _ := cmd.Flags().GetBool("no-input")
	user, pw, krb, err := resolveSMBAuth(os.Stderr, bufio.NewReader(os.Stdin), *dp, stored, noInput)
	if err != nil {
		return err
	}
	defer krb.close()
	tgt := smbTarget(*dp, user, pw, krb)
	auth := "password"
	if krb != nil {
		auth = "Kerberos"
	}
	fmt.Fprintf(os.Stderr, "Distribution point: %s (SMB, %s, %s)\n", dp.label(), termSafe(tgt.UNC()), auth)
	// Before any record is deleted: a wrong account or share must leave
	// every record in place.
	if err := smbupload.Preflight(ctx, tgt); err != nil {
		return fmt.Errorf("checking %s: %w", termSafe(tgt.UNC()), err)
	}
	for _, p := range targets {
		verb := "will also be removed"
		if dryRun {
			verb = "would also be removed"
		}
		fmt.Fprintf(os.Stderr, "File %s %s from %s\n", termSafe(p.fileName), verb, termSafe(tgt.UNC()))
	}
	if dryRun {
		return inner(cmd, args)
	}

	recordErr := inner(cmd, args)

	// Which records are gone is read back rather than inferred from the
	// generated command's result: a bulk delete carries on past a failure.
	var failed []string
	var removed, absent int
	for _, p := range targets {
		gone, gerr := packageRecordGone(ctx, client, p.id)
		if gerr != nil {
			failed = append(failed, fmt.Sprintf("%s (could not tell whether package %s was deleted: %v)", termSafe(p.fileName), termSafe(p.id), termSafe(gerr)))
			continue
		}
		if !gone {
			continue // record kept, so its file stays
		}
		switch rerr := smbupload.Remove(ctx, tgt, p.fileName); {
		case rerr == nil:
			removed++
			fmt.Fprintf(os.Stderr, "Removed %s from %s\n", termSafe(p.fileName), termSafe(tgt.UNC()))
		case errors.Is(rerr, fs.ErrNotExist):
			absent++
			fmt.Fprintf(os.Stderr, "%s was not on %s; nothing to remove\n", termSafe(p.fileName), termSafe(tgt.UNC()))
		default:
			failed = append(failed, fmt.Sprintf("%s (%v)", termSafe(p.fileName), termSafe(rerr)))
		}
	}
	if len(failed) > 0 {
		return exitcode.New(exitcode.General,
			fmt.Sprintf("the package record was deleted but %d file(s) remain on %s: %s", len(failed), termSafe(tgt.UNC()), strings.Join(failed, "; "))).
			WithHint("remove the file from the share by hand; the record is gone, so deleting again will not retry it")
	}
	return recordErr
}

// selectFileShareDP chooses the share a delete clears or upload-multiple writes, by the upload's
// rule, or nil when the delete is record-only. A distribution point named with
// --distribution-point is an explicit request and any problem with it is an
// error; the implicit choice is made only when the tenant states it has no
// cloud distribution point and a master SMB distribution point exists.
func selectFileShareDP(ctx context.Context, cliCtx *registry.CLIContext, name string) (*fileShareDP, error) {
	client := cliCtx.Client
	if name != "" {
		dp, err := resolveFileShareDP(ctx, client, name)
		if err != nil {
			return nil, err
		}
		return &dp, nil
	}
	proSDK := proSDKFor(cliCtx)
	var cdp cdpReader
	if proSDK != nil {
		cdp = proSDK
	}
	_, err := chooseUploadRoute(ctx, cdp)
	if err == nil && proSDK == nil && instanceHasNoCloudDP(ctx, client) {
		err = errNoCloudDistributionPoint
	}
	if !errors.Is(err, errNoCloudDistributionPoint) {
		return nil, nil //nolint:nilnil // a cloud distribution point is configured: record-only
	}
	dp, derr := resolveFileShareDP(ctx, client, "")
	if derr != nil {
		return nil, nil //nolint:nilnil,nilerr // no file share either: the record is all there is
	}
	return &dp, nil
}

// packageDeleteTargets reads, before anything is deleted, the file name of every
// package the generated command is about to delete.
func packageDeleteTargets(ctx context.Context, cmd *cobra.Command, client registry.HTTPClient, args []string) ([]packageFile, error) {
	fromFile, _ := cmd.Flags().GetString("from-file")
	name, _ := cmd.Flags().GetString("name")

	var refs []string
	switch {
	case fromFile != "":
		entries, err := listfile.Read(fromFile)
		if err != nil {
			return nil, fmt.Errorf("reading --from-file: %w", err)
		}
		for _, e := range entries {
			refs = append(refs, e.Value)
		}
	case name != "":
		refs = []string{name}
	case len(args) > 0:
		refs = []string{args[0]}
	}

	var out []packageFile
	seen := map[string]bool{}
	for _, ref := range refs {
		p, err := packageFileFor(ctx, client, ref, name != "" || fromFile != "" && !resolve.IsNumericID(ref))
		if err != nil {
			return nil, err
		}
		if !seen[p.id] {
			seen[p.id] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// packageFileFor reads one record's file name. ref is an id, or a package name
// when byName is set (a numeric --from-file entry is an id, as the generated
// command reads it).
func packageFileFor(ctx context.Context, client registry.HTTPClient, ref string, byName bool) (packageFile, error) {
	if !byName {
		data, err := fetchJSON(ctx, client, "/v1/packages/"+url.PathEscape(ref))
		if err != nil {
			return packageFile{}, fmt.Errorf("reading package %s: %w", termSafe(ref), err)
		}
		return packageFile{id: ref, fileName: fileNameOf(data)}.checked()
	}
	path := "/v1/packages?page-size=2&filter=" + url.QueryEscape(fmt.Sprintf(`packageName=="%s"`, resolve.EscapeRSQL(ref)))
	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return packageFile{}, fmt.Errorf("searching for package %q: %w", termSafe(ref), err)
	}
	results, _ := data["results"].([]any)
	if len(results) != 1 {
		return packageFile{}, fmt.Errorf("package name %q matches %d packages; delete by id so the right file is removed from the share", termSafe(ref), len(results))
	}
	rec, _ := results[0].(map[string]any)
	return packageFile{id: extractField(rec, "id"), fileName: fileNameOf(rec)}.checked()
}

func fileNameOf(rec map[string]any) string {
	s, _ := rec["fileName"].(string)
	return s
}

// checked refuses a record that names no usable file, before anything is
// deleted: the share path would otherwise be guessed.
func (p packageFile) checked() (packageFile, error) {
	if p.id == "" {
		return p, errors.New("package record has no id")
	}
	if p.fileName == "" {
		return p, fmt.Errorf("package %s names no file, so there is nothing to remove from the share; delete it without --distribution-point", termSafe(p.id))
	}
	if err := smbupload.ValidateFileName(p.fileName); err != nil {
		return p, fmt.Errorf("package %s: %w", termSafe(p.id), err)
	}
	return p, nil
}

// packageRecordGone reports whether a package record no longer exists. Only a
// 404 counts: any other answer leaves the file where it is.
func packageRecordGone(ctx context.Context, client registry.HTTPClient, id string) (bool, error) {
	resp, err := client.Do(ctx, http.MethodGet, "/v1/packages/"+url.PathEscape(id), nil)
	if err != nil {
		// The client turns a 404 into an error, so absence arrives here.
		if exitcode.CodeFrom(err) == exitcode.NotFound {
			return true, nil
		}
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusNotFound, nil
}
