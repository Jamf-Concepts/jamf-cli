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

The command calculates SHA3-512, SHA-256, and MD5 hashes of the file and writes
them to the package metadata after upload.`,
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

			var previousHash string // non-empty when replacing an existing package
			if pkgID != "" {
				if !flagYes {
					noInput, _ := cmd.Flags().GetBool("no-input")
					if noInput {
						return fmt.Errorf("package %q already exists (id %s); use --yes to replace when --no-input is set", fileName, pkgID)
					}
					fmt.Fprintf(os.Stderr, "Package %q already exists (id %s). Replace? Type 'yes' to confirm: ", fileName, pkgID)
					var confirm string
					_, _ = fmt.Scanln(&confirm)
					if confirm != "yes" {
						return fmt.Errorf("aborted")
					}
				}

				// Record the existing hash so verification can distinguish
				// a stale old hash from a genuine mismatch after upload.
				if data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))); err == nil {
					previousHash, _ = data["hashValue"].(string)
				}

				fmt.Fprintf(os.Stderr, "Updating existing package (id %s)\n", pkgID)
			} else {
				// 4. Create package metadata
				fmt.Fprintf(os.Stderr, "Creating package record...\n")
				pkgID, err = createPackage(ctx, client, packageName, fileName, categoryID, priority)
				if err != nil {
					return fmt.Errorf("creating package: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Created package (id %s)\n", pkgID)
			}

			// 5. Upload the binary
			fmt.Fprintf(os.Stderr, "Uploading %s...\n", fileName)
			if err := uploadPackageFile(ctx, uploader, pkgID, filePath); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Upload complete\n")

			// 6. Verify upload — poll until server-computed hash matches local hash.
			// The server computes hashType/hashValue/sha3512/sha256/md5/size itself
			// from the uploaded JCDS file, so no PUT of hash metadata is needed (and
			// a PUT would blank the server-managed size field). verifyPackageUpload
			// returns the complete record on success — reuse it for display.
			fmt.Fprintf(os.Stderr, "Verifying upload...\n")
			data, err := verifyPackageUpload(ctx, client, pkgID, fileName, hashes.sha3, previousHash)
			if err != nil {
				if strings.Contains(err.Error(), "hash mismatch") {
					return fmt.Errorf("upload verification failed: %w", err)
				}
				// Timeout is non-fatal — server may still be processing
				fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
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
					fmt.Fprintf(os.Stderr, "Package uploaded successfully (id %s)\n", pkgID)
					return nil
				}
			}
			result, _ := json.Marshal(data)
			return cliCtx.Output.PrintRaw(result)
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to the package file (required)")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVar(&packageName, "name", "", "display name for the package (defaults to filename)")
	cmd.Flags().StringVar(&categoryID, "category-id", "-1", "category ID for the package")
	cmd.Flags().IntVar(&priority, "priority", 10, "installation priority for the package (1-20)")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "skip confirmation when replacing an existing package")

	return cmd
}

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
	path := fmt.Sprintf("/v1/packages?filter=%s&page-size=1",
		url.QueryEscape(fmt.Sprintf(`fileName=="%s"`, fileName)))

	data, err := fetchJSON(ctx, client, path)
	if err != nil {
		return "", fmt.Errorf("searching for package: %w", err)
	}

	totalCount, _ := data["totalCount"].(float64)
	if totalCount == 0 {
		return "", nil
	}

	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		return "", nil
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return "", nil
	}
	return extractField(first, "id"), nil
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

// verifyPackageUpload polls the package until the server's computed hash matches
// the expected value. On each iteration it nudges the JCDS inventory refresh
// (errors ignored — transient 500s and concurrency failures are expected).
// previousHash is the hash that was on the package record before upload (empty for
// new packages). When the server still returns this value, polling continues because
// the server hasn't recomputed from the new file yet. A different non-matching
// SHA3_512 hash indicates genuine corruption and fails immediately.
//
// On success it returns the verified package record. The server computes
// hashType/hashValue/sha3512/sha256/md5/size itself from the uploaded JCDS
// file, so this record is complete — callers can display it directly without
// a further GET, and must not PUT hash metadata back (a PUT blanks the
// server-managed size field).
func verifyPackageUpload(ctx context.Context, client registry.HTTPClient, pkgID, fileName, expectedSHA3, previousHash string) (map[string]any, error) {
	const (
		verifyTimeout  = 10 * time.Minute
		verifyInterval = 10 * time.Second
	)

	refreshPath := fmt.Sprintf("/v1/cloud-distribution-point/refresh-inventory?file-name=%s", url.QueryEscape(fileName))
	pkgPath := fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))

	deadline := time.Now().Add(verifyTimeout)
	for time.Now().Before(deadline) {
		// Wait first — give the server time to process
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(verifyInterval):
		}

		// Nudge JCDS refresh (ignore errors — transient failures are expected)
		if resp, err := client.Do(ctx, "POST", refreshPath, nil); err == nil {
			_ = resp.Body.Close()
		}

		// Check server hash
		data, err := fetchJSON(ctx, client, pkgPath)
		if err != nil {
			continue // transient fetch failure, retry
		}

		hashType, _ := data["hashType"].(string)
		hashValue, _ := data["hashValue"].(string)

		if hashValue == "" {
			fmt.Fprintf(os.Stderr, "  waiting for server hash computation...\n")
			continue
		}

		if hashType == "SHA3_512" && hashValue == expectedSHA3 {
			return data, nil
		}

		if hashType == "SHA3_512" && hashValue == previousHash {
			// Server still has the old file's hash — hasn't recomputed yet
			fmt.Fprintf(os.Stderr, "  waiting for server to recompute hash...\n")
			continue
		}

		if hashType == "SHA3_512" {
			// Server computed a new SHA3_512 hash that doesn't match — corrupted upload
			return nil, fmt.Errorf("hash mismatch: server=%s local=%s", hashValue, expectedSHA3)
		}

		// Hash type hasn't been updated to SHA3_512 yet — keep polling
		fmt.Fprintf(os.Stderr, "  waiting for hash type update (current: %s)...\n", hashType)
	}

	return nil, fmt.Errorf("timed out after %v waiting for server hash to match (check package integrity)", verifyTimeout)
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
