// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

func newPackagesUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		filePath    string
		packageName string
		categoryID  string
		priority    int
		flagYes     bool
		flagDP      string
	)

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a package file to Jamf Pro",
		Long: `Upload a local package file to Jamf Pro's cloud distribution point.

If a package with the same filename already exists, its binary and hash metadata
are updated. Otherwise a new package record is created first.

Progress is printed to stderr while the file transfers: a line redrawn in place
on a terminal, a line per quarter anywhere else. --quiet silences it.

The command hashes the file locally and checks the result against the hash Jamf
Pro computes for the uploaded file. It never writes hashes to the package
record: Jamf computes them and the size itself. The strongest hash the server
reports is compared, in the order SHA3-512, SHA-256, MD5. Jamf Cloud (JCDS)
reports SHA3-512; an Amazon S3 distribution point reports MD5 only.

On a platform gateway profile whose cloud distribution point is Jamf Cloud
(JCDS), the file goes straight to JCDS storage through a short-lived upload
grant Jamf Pro issues, rather than through the gateway. Every other combination uploads
through the Jamf Pro API.

An SMB file share distribution point is reached directly over SMB2/3, because
Jamf Pro's upload endpoint only reaches a cloud distribution point. Name one
with --distribution-point, or leave the flag off on a tenant with no cloud
distribution point and the master SMB distribution point is used. AFP is not supported.
The account that writes to the share is read from JAMFSMB_USERNAME and
JAMFSMB_PASSWORD, else the account saved in the profile by
"pro packages smb-setup" (password in the keychain), else prompted for. Jamf
Pro never returns the password, and none is accepted as a flag. With none of those and a Kerberos ticket in a FILE
cache (KRB5CCNAME, else /tmp/krb5cc_<uid>), or the signed-in macOS user's
ticket, which is copied to a private temporary file for the upload and removed
after, the ticket is used. A supplied or saved account always wins over it. The
server must be named by DNS name, not an IP address. The package goes in the share's Packages folder, which
must already exist, written under a temporary name and renamed into place. Jamf Pro never sees the bytes, so it computes no hash:
the check is that the file on the share has the local file's size.`,
		Example: `  # Upload a package
  jamf-cli pro packages upload --file ./Firefox-134.0.pkg

  # Upload with a custom display name
  jamf-cli pro packages upload --file ./Firefox-134.0.pkg --name "Firefox"

  # Upload and assign to a category by ID
  jamf-cli pro packages upload --file ./Firefox-134.0.pkg --category-id 5

  # Upload and set installation priority
  jamf-cli pro packages upload --file ./Firefox-134.0.pkg --priority 10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			client := cliCtx.Client
			uploader := cliCtx.Uploader
			if uploader == nil {
				return fmt.Errorf("upload not supported in this context")
			}

			// 1. Validate file
			info, err := os.Stat(filePath)
			if err != nil {
				return fmt.Errorf("cannot access file: %w", err)
			}
			if info.IsDir() {
				return fmt.Errorf("%s is a directory, not a file", filePath)
			}
			fileName := filepath.Base(filePath)
			fileSize := info.Size()
			if packageName == "" {
				packageName = fileName
			}

			fmt.Fprintf(os.Stderr, "File: %s (%s)\n", fileName, humanSize(fileSize))

			// 2. Hash the file (single pass)
			fmt.Fprintf(os.Stderr, "Calculating hashes...\n")
			hashes, err := hashFile(filePath)
			if err != nil {
				return fmt.Errorf("hashing file: %w", err)
			}
			hashes.size = fileSize
			fmt.Fprintf(os.Stderr, "  SHA3-512: %s...\n", hashes.sha3[:16])
			fmt.Fprintf(os.Stderr, "  SHA-256:  %s...\n", hashes.sha256[:16])
			fmt.Fprintf(os.Stderr, "  MD5:      %s...\n", hashes.md5[:16])

			// 3. Check for existing package
			fmt.Fprintf(os.Stderr, "Checking for existing package...\n")
			pkgID, err := findPackageByFileName(ctx, client, fileName)
			if err != nil {
				return err
			}

			// An SMB distribution point is resolved before the preview so
			// -n names the share, and before any record is written so a
			// missing account fails with nothing to clean up.
			var smbDP *fileShareDP
			// upload-multiple resolves the share and its account once and hands
			// them on, so a prompted password is asked for once, not per file.
			session, _ := ctx.Value(smbSessionKey{}).(*smbSession)
			if session != nil {
				dp := session.dp
				smbDP = &dp
			} else if flagDP != "" {
				dp, err := resolveFileShareDP(ctx, client, flagDP)
				if err != nil {
					return err
				}
				smbDP = &dp
			}

			if dryRun {
				if smbDP != nil {
					fmt.Fprintf(os.Stderr, "[dry-run] Would upload %s to %s over SMB and %s the package record\n",
						filePath, termSafe(`\\`+smbDP.host+`\`+smbDP.share), map[bool]string{true: "update", false: "create"}[pkgID != ""])
					return nil
				}
				if pkgID != "" {
					fmt.Fprintf(os.Stderr, "[dry-run] Would replace the file of package %q (id %s) with %s\n", packageName, termSafe(pkgID), filePath)
				} else {
					fmt.Fprintf(os.Stderr, "[dry-run] Would create package %q and upload %s\n", packageName, filePath)
				}
				return nil
			}

			// Chosen before anything is written, so a tenant with no
			// distribution point is refused before a record is created.
			proSDK := proSDKFor(cliCtx)
			var cdp cdpReader
			if proSDK != nil {
				cdp = proSDK
			}
			route, err := chooseUploadRoute(ctx, cdp)
			if err == nil && smbDP == nil && proSDK == nil && instanceHasNoCloudDP(ctx, client) {
				err = errNoCloudDistributionPoint
			}
			if smbDP != nil && errors.Is(err, errNoCloudDistributionPoint) {
				err = nil // a named share is the destination; the cloud one is irrelevant
			}
			if smbDP == nil && errors.Is(err, errNoCloudDistributionPoint) {
				// No cloud distribution point: the master file share, if
				// there is one, is where the package goes.
				if dp, derr := resolveFileShareDP(ctx, client, ""); derr == nil {
					smbDP, err = &dp, nil
				}
			}
			if err != nil {
				return err
			}

			var smbTgt smbupload.Target
			if session != nil {
				smbTgt = session.tgt
			} else if smbDP != nil {
				stored, serr := storedSMBCredential(cliCtx.ProfileName, smbDP.name)
				if serr != nil {
					return serr
				}
				noInput, _ := cmd.Flags().GetBool("no-input")
				user, pw, krb, aerr := resolveSMBAuth(os.Stderr, bufio.NewReader(os.Stdin), *smbDP, stored, noInput)
				if aerr != nil {
					return aerr
				}
				defer krb.close() // removes a private copy of the ticket, if one was made
				smbTgt = smbTarget(*smbDP, user, pw, krb)
				auth := "password"
				if krb != nil {
					auth = "Kerberos"
				}
				fmt.Fprintf(os.Stderr, "Distribution point: %s (SMB, %s, %s)\n", smbDP.label(), termSafe(smbTgt.UNC()), auth)
				// Before any record exists: a wrong account, share or a missing
				// Packages folder must leave nothing behind.
				if err := smbupload.Preflight(ctx, smbTgt); err != nil {
					return fmt.Errorf("checking %s: %w", termSafe(smbTgt.UNC()), err)
				}
			}

			var prev previousHashes // non-empty when replacing an existing package
			if pkgID != "" {
				if !flagYes {
					noInput, _ := cmd.Flags().GetBool("no-input")
					if noInput {
						return fmt.Errorf("package %q already exists (id %s); use --yes to replace when --no-input is set", fileName, pkgID)
					}
					fmt.Fprintf(os.Stderr, "Package %q already exists (id %s). Replace? Type 'yes' to confirm: ", termSafe(fileName), termSafe(pkgID))
					var confirm string
					_, _ = fmt.Scanln(&confirm)
					if confirm != "yes" {
						return fmt.Errorf("aborted")
					}
				}

				// Record the existing hash so verification can distinguish
				// a stale old hash from a genuine mismatch after upload.
				if data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))); err == nil {
					prev.sha3, _ = data["hashValue"].(string)
					prev.md5, _ = data["md5"].(string)
					prev.size = sizeString(data["size"])
				}

				fmt.Fprintf(os.Stderr, "Updating existing package (id %s)\n", termSafe(pkgID))
			} else {
				// 4. Create package metadata
				fmt.Fprintf(os.Stderr, "Creating package record...\n")
				pkgID, err = createPackage(ctx, client, packageName, fileName, categoryID, priority)
				if err != nil {
					return fmt.Errorf("creating package: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Created package (id %s)\n", termSafe(pkgID))
			}

			// 5. Upload the binary
			fmt.Fprintf(os.Stderr, "Uploading %s...\n", fileName)
			if smbDP != nil {
				err = uploadPackageFileSMB(ctx, smbTgt, filePath, fileSize)
			} else if route == uploadViaJCDS {
				err = uploadPackageFileDirect(ctx, proSDK, filePath)
			} else {
				err = uploadPackageFile(ctx, uploader, pkgID, filePath)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Upload complete\n")

			// 6. Verify upload — poll until a server-computed hash matches the local
			// file. The server computes the hashes and size itself (SHA3-512 on JCDS,
			// MD5 only on S3; the strongest one present is compared), so none are sent: a PUT blanks the size, and the
			// distribution point's own inventory refresh overwrites it anyway.
			// verifyPackageUpload returns the complete record on success.
			if smbDP != nil {
				fmt.Fprintf(os.Stderr, "Verified: %s holds %s (Jamf Pro computes no hash for a file share)\n", termSafe(smbTgt.UNC()), humanSize(fileSize))
				data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID)))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Package uploaded successfully (id %s)\n", termSafe(pkgID))
					return nil
				}
				result, _ := json.Marshal(data)
				return cliCtx.Output.PrintRaw(result)
			}
			fmt.Fprintf(os.Stderr, "Verifying upload...\n")
			data, err := verifyPackageUpload(ctx, client, pkgID, fileName, hashes, prev)
			if err != nil {
				if strings.Contains(err.Error(), "hash mismatch") {
					return fmt.Errorf("upload verification failed: %w", err)
				}
				// Timeout is non-fatal — server may still be processing
				fmt.Fprintf(os.Stderr, "WARNING: %v\n", termSafe(err))
				var unverified *errUnverified
				if errors.As(err, &unverified) {
					fmt.Fprintf(os.Stderr, "The file was uploaded; compare it against your source before relying on it.\n")
				} else {
					fmt.Fprintf(os.Stderr, "The file was uploaded. The server may still be processing.\n")
				}
			} else {
				fmt.Fprintf(os.Stderr, "Verified: server hash matches local hash\n")
			}

			// 7. Print result. On a verify timeout we have no record in hand —
			// fetch once so the user still sees the current server state.
			if data == nil {
				data, err = fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID)))
				if err != nil {
					// Non-fatal: upload succeeded, just can't show final state
					fmt.Fprintf(os.Stderr, "Package uploaded successfully (id %s)\n", termSafe(pkgID))
					return nil
				}
			}
			result, _ := json.Marshal(data)
			return cliCtx.Output.PrintRaw(result)
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to the package file (required; --file, not --from-file: a multipart upload needs a name and a length, so it cannot be piped)")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVar(&packageName, "name", "", "display name for the package (defaults to filename)")
	cmd.Flags().StringVar(&categoryID, "category-id", "-1", "category ID for the package")
	cmd.Flags().IntVar(&priority, "priority", 10, "installation priority for the package (1-20)")
	cmd.Flags().StringVar(&flagDP, "distribution-point", "", "upload to this SMB distribution point, by its Jamf Pro name (default: the master SMB distribution point when the tenant has no cloud distribution point)")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing package")

	return cmd
}

// verifyTimeout/verifyInterval bound the post-upload hash-verification poll.
// They are package-level vars (not consts) so tests can shrink them.
var (
	verifyTimeout  = 10 * time.Minute
	verifyInterval = 10 * time.Second
	// verifyGrace is how long a package that is READY at the right size gets to
	// show a hash before the upload is reported as unverified. Jamf Pro never
	// computes one for some large packages, so waiting out verifyTimeout would
	// only make the caller wait for nothing.
	verifyGrace = 20 * time.Second
)

// errUnverified is a package that uploaded but could not be integrity-checked:
// the server computed no new hash. It is a warning, not a failure.
type errUnverified struct{ reason string }

func (e *errUnverified) Error() string { return e.reason }

// sizeString renders a record's size field, which arrives as a string or a number.
func sizeString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatInt(int64(n), 10)
	}
	return ""
}

// fileHashes holds the computed hash values for a package file.
type fileHashes struct {
	sha3   string
	sha256 string
	md5    string
	size   int64 // byte length of the file; 0 when unknown
}

// hashFile computes SHA3-512, SHA-256, and MD5 in a single pass.
func hashFile(path string) (fileHashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileHashes{}, err
	}
	defer func() { _ = f.Close() }()

	sha3Hash := sha3.New512()
	sha256Hash := sha256.New()
	md5Hash := md5.New()

	if _, err := io.Copy(io.MultiWriter(sha3Hash, sha256Hash, md5Hash), f); err != nil {
		return fileHashes{}, err
	}

	return fileHashes{
		sha3:   hex.EncodeToString(sha3Hash.Sum(nil)),
		sha256: hex.EncodeToString(sha256Hash.Sum(nil)),
		md5:    hex.EncodeToString(md5Hash.Sum(nil)),
	}, nil
}

// findPackageByFileName searches for a package with the given filename.
// Returns the package ID if found, empty string if not.
func findPackageByFileName(ctx context.Context, client registry.HTTPClient, fileName string) (string, error) {
	path := fmt.Sprintf("/v1/packages?filter=%s&page-size=2",
		url.QueryEscape(fmt.Sprintf(`fileName=="%s"`, resolve.EscapeRSQL(fileName))))

	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return "", fmt.Errorf("searching for package: %w", err)
	}

	results, _ := data["results"].([]any)
	var ids []string
	for _, r := range results {
		rec, ok := r.(map[string]any)
		if !ok {
			continue
		}
		// Jamf Pro matches and enforces fileName case-insensitively, so a case
		// variant is the same package and can be replaced, not created beside it.
		if name, _ := rec["fileName"].(string); strings.EqualFold(name, fileName) {
			ids = append(ids, extractField(rec, "id"))
		}
	}
	totalCount, _ := data["totalCount"].(float64)
	if len(ids) > 1 || (len(ids) == 1 && totalCount > 1) {
		return "", fmt.Errorf("%d packages have file name %q; refusing to pick one, remove the duplicate or upload under another name", max(len(ids), int(totalCount)), fileName)
	}
	if len(ids) == 0 && int(totalCount) > len(results) {
		return "", fmt.Errorf("the search for file name %q matched %d packages and returned %d, so it cannot show the name is unused; rename the file or remove the packages it matches", fileName, int(totalCount), len(results))
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// createPackage creates a new package record and returns its ID.
func createPackage(ctx context.Context, client registry.HTTPClient, name, fileName, categoryID string, priority int) (string, error) {
	payload := map[string]any{
		"packageName":          name,
		"fileName":             fileName,
		"categoryId":           categoryID,
		"priority":             priority,
		"fillUserTemplate":     false,
		"suppressEula":         false,
		"suppressUpdates":      false,
		"rebootRequired":       false,
		"osInstall":            false,
		"suppressRegistration": false,
		"suppressFromDock":     false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(ctx, "POST", "/v1/packages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	// Response contains {"id": "123", "href": "..."}
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing create response: %w", err)
	}
	id := extractField(result, "id")
	if id == "" {
		return "", fmt.Errorf("no id in create response: %s", string(respBody))
	}
	return id, nil
}

// uploadPackageFile streams a local file to the Jamf Pro upload endpoint
// as multipart/form-data. The body is a seekable composite of header +
// *os.File + footer — enabling Upload to retry on HTTP 429 without
// re-reading the file into memory.
func uploadPackageFile(ctx context.Context, uploader registry.FileUploader, pkgID, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening upload file: %w", err)
	}
	defer func() { _ = f.Close() }()

	body, contentType, contentLength, err := client.NewMultipartFileUpload("file", f)
	if err != nil {
		return fmt.Errorf("building multipart body: %w", err)
	}

	path := fmt.Sprintf("/v1/packages/%s/upload", url.PathEscape(pkgID))
	progress := newUploadProgress(body, contentLength)
	resp, err := uploader.Upload(ctx, path, progress, contentType, contentLength)
	progress.finish()
	if err != nil {
		return fmt.Errorf("uploading package: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// previousHashes is what the package record held before an upload replaced its
// file, so verification can tell a stale value from a genuinely wrong one.
type previousHashes struct {
	sha3   string
	sha256 string
	md5    string
	size   string // the record's size field, which Jamf Pro reports as a string
}

// hashCandidate pairs a package record field with the local and previous value
// of the same algorithm.
type hashCandidate struct {
	label, field, local, prev string
}

// verifyPackageUpload polls the package until the strongest hash the server has
// computed matches the local file. Nothing is written back: Jamf computes the
// hashes and the size itself, and a distribution point's inventory refresh
// (which runs on its own schedule as well as from the nudge below) overwrites
// any value a client PUTs.
//
// Which hashes exist depends on the distribution point: JCDS fills sha3512, an
// S3 distribution point only md5. So the check does not key on hashType or
// hashValue (only JCDS populates them) but takes the first populated field of
// sha3512, sha256, md5, which also covers a provider that starts reporting a
// stronger one.
//
// On each iteration it nudges the inventory refresh (errors ignored — transient
// 500s and concurrency failures are expected), then waits for the package's
// cloudTransferStatus to be READY before comparing. A value equal to the previous
// record's means the server has not recomputed yet, so polling continues. A
// different non-matching value is genuine corruption and fails immediately.
//
// On success it returns the verified package record, which is complete, so
// callers can display it without a further GET.
func verifyPackageUpload(ctx context.Context, client registry.HTTPClient, pkgID, fileName string, local fileHashes, prev previousHashes) (map[string]any, error) {
	refreshPath := fmt.Sprintf("/v1/cloud-distribution-point/refresh-inventory?file-name=%s", url.QueryEscape(fileName))
	pkgPath := fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))
	candidates := []hashCandidate{
		{"SHA3-512", "sha3512", local.sha3, prev.sha3},
		{"SHA-256", "sha256", local.sha256, prev.sha256},
		{"MD5", "md5", local.md5, prev.md5},
	}

	localSize := ""
	if local.size > 0 {
		localSize = strconv.FormatInt(local.size, 10)
	}
	// A replace whose old and new files have the same byte length cannot be
	// confirmed by size: the record looks identical before and after.
	sameSizeReplace := localSize != "" && prev.size == localSize
	var graceStart time.Time

	deadline := time.Now().Add(verifyTimeout)
	for time.Now().Before(deadline) {
		// Wait first — give the server time to process
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(verifyInterval):
		}

		// Nudge inventory refresh (ignore errors — transient failures are expected)
		if resp, err := client.Do(ctx, "POST", refreshPath, nil); err == nil {
			_ = resp.Body.Close()
		}

		data, err := fetchJSON(ctx, client, pkgPath)
		if err != nil {
			continue // transient fetch failure, retry
		}

		status, _ := data["cloudTransferStatus"].(string)
		if status == "ERROR" {
			return nil, fmt.Errorf("distribution point reports the transfer of %s as ERROR", fileName)
		}
		if status != "" && status != "READY" {
			fmt.Fprintf(os.Stderr, "  waiting for transfer (%s)...\n", termSafe(status))
			continue
		}

		verdict := "waiting for server hash computation..."
		for _, c := range candidates {
			server, _ := data[c.field].(string)
			if server == "" {
				continue
			}
			switch server {
			case c.local:
				return data, nil
			case c.prev:
				verdict = "waiting for server to recompute hash..."
			default:
				return nil, fmt.Errorf("hash mismatch: server %s=%s local=%s", c.label, server, c.local)
			}
			break // the strongest populated hash decides
		}
		fmt.Fprintf(os.Stderr, "  %s\n", verdict)

		// READY at the new file's size but still no matching hash: give the server
		// a short grace to compute one, then report it unverified instead of
		// waiting out the whole timeout.
		if localSize != "" && sizeString(data["size"]) == localSize {
			if graceStart.IsZero() {
				graceStart = time.Now()
			}
			if time.Since(graceStart) >= verifyGrace {
				return nil, unverifiedErr(fileName, localSize, prev, sameSizeReplace)
			}
		}
	}

	return nil, fmt.Errorf("timed out after %v waiting for the server to compute a hash (check package integrity)", verifyTimeout)
}

// humanSize formats a byte count as a human-readable string.
func humanSize(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

// unverifiedErr explains why a package that is READY at the right size could
// not be integrity-checked.
func unverifiedErr(fileName, size string, prev previousHashes, sameSizeReplace bool) error {
	why := fmt.Sprintf("%s is READY and its size matches (%s bytes), but Jamf Pro computed no hash for it", fileName, size)
	switch {
	case sameSizeReplace:
		why = fmt.Sprintf("%s was replaced by a file of identical size (%s bytes), so the replace cannot be confirmed by size, and Jamf Pro computed no new hash", fileName, size)
	case prev.sha3 != "" || prev.sha256 != "" || prev.md5 != "":
		why += " (the previous hash was cleared or left stale by the replace)"
	}
	return &errUnverified{reason: why + "; integrity NOT verified. Jamf Pro does not hash some large packages"}
}
