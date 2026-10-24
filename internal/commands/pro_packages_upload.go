// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

func newPackagesUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		filePath    string
		packageName string
		categoryID  string
		priority    int
		flagYes     bool
	)

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a package file to Jamf Pro",
		Long: `Upload a local package file to Jamf Pro's cloud distribution point.

If a package with the same filename already exists, its binary and hash metadata
are updated. Otherwise a new package record is created first.

The command hashes the file locally and checks the result against the hash Jamf
Pro computes for the uploaded file. It never writes hashes to the package
record: Jamf computes them and the size itself. The strongest hash the server
reports is compared, in the order SHA3-512, SHA-256, MD5. Jamf Cloud (JCDS)
reports SHA3-512; an Amazon S3 distribution point reports MD5 only.`,
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
			fmt.Fprintf(os.Stderr, "  SHA3-512: %s...\n", hashes.sha3[:16])
			fmt.Fprintf(os.Stderr, "  SHA-256:  %s...\n", hashes.sha256[:16])
			fmt.Fprintf(os.Stderr, "  MD5:      %s...\n", hashes.md5[:16])

			// 3. Check for existing package
			fmt.Fprintf(os.Stderr, "Checking for existing package...\n")
			pkgID, err := findPackageByFileName(ctx, client, fileName)
			if err != nil {
				return err
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
			if err := uploadPackageFile(ctx, uploader, pkgID, filePath); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Upload complete\n")

			// 6. Verify upload — poll until a server-computed hash matches the local
			// file. The server computes the hashes and size itself (SHA3-512 on JCDS,
			// MD5 only on S3; the strongest one present is compared), so none are sent: a PUT blanks the size, and the
			// distribution point's own inventory refresh overwrites it anyway.
			// verifyPackageUpload returns the complete record on success.
			fmt.Fprintf(os.Stderr, "Verifying upload...\n")
			data, err := verifyPackageUpload(ctx, client, pkgID, fileName, hashes, prev)
			if err != nil {
				if strings.Contains(err.Error(), "hash mismatch") {
					return fmt.Errorf("upload verification failed: %w", err)
				}
				// Timeout is non-fatal — server may still be processing
				fmt.Fprintf(os.Stderr, "WARNING: %v\n", termSafe(err))
				fmt.Fprintf(os.Stderr, "The file was uploaded. The server may still be processing.\n")
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
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing package")

	return cmd
}

// verifyTimeout/verifyInterval bound the post-upload hash-verification poll.
// They are package-level vars (not consts) so tests can shrink them.
var (
	verifyTimeout  = 10 * time.Minute
	verifyInterval = 10 * time.Second
)

// fileHashes holds the computed hash values for a package file.
type fileHashes struct {
	sha3   string
	sha256 string
	md5    string
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
	resp, err := uploader.Upload(ctx, path, body, contentType, contentLength)
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
