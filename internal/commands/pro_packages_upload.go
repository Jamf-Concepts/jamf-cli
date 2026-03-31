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

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func newPackagesUploadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		filePath    string
		packageName string
		categoryID  string
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
  jamf-cli pro packages upload --file ./Firefox-134.0.pkg --category-id 5`,
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

			// 3. Check for existing package
			fmt.Fprintf(os.Stderr, "Checking for existing package...\n")
			pkgID, err := findPackageByFileName(ctx, client, fileName)
			if err != nil {
				return err
			}

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
				fmt.Fprintf(os.Stderr, "Updating existing package (id %s)\n", pkgID)
			} else {
				// 4. Create package metadata
				fmt.Fprintf(os.Stderr, "Creating package record...\n")
				pkgID, err = createPackage(ctx, client, packageName, fileName, categoryID)
				if err != nil {
					return fmt.Errorf("creating package: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Created package (id %s)\n", pkgID)
			}

			// 5. Upload the binary
			fmt.Fprintf(os.Stderr, "Uploading %s...\n", fileName)
			if err := uploadPackageFile(ctx, uploader, pkgID, filePath, fileName, fileSize); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Upload complete\n")

			// 6. Update metadata with hashes
			fmt.Fprintf(os.Stderr, "Updating package hashes...\n")
			if err := updatePackageHashes(ctx, client, pkgID, hashes); err != nil {
				return fmt.Errorf("updating hashes: %w", err)
			}

			// 7. Verify upload — poll until server hash matches
			fmt.Fprintf(os.Stderr, "Verifying upload...\n")
			if err := verifyPackageUpload(ctx, client, pkgID, fileName, hashes.sha3); err != nil {
				if strings.Contains(err.Error(), "hash mismatch") {
					return fmt.Errorf("upload verification failed: %w", err)
				}
				// Timeout is non-fatal — server may still be processing
				fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
				fmt.Fprintf(os.Stderr, "The file was uploaded and hashes were set. The server may still be processing.\n")
			} else {
				fmt.Fprintf(os.Stderr, "Verified: server hash matches local hash\n")
			}

			// 8. Print result
			data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID)))
			if err != nil {
				// Non-fatal: upload succeeded, just can't show final state
				fmt.Fprintf(os.Stderr, "Package uploaded successfully (id %s)\n", pkgID)
				return nil
			}
			result, _ := json.Marshal(data)
			return cliCtx.Output.PrintRaw(result)
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to the package file (required)")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().StringVar(&packageName, "name", "", "display name for the package (defaults to filename)")
	cmd.Flags().StringVar(&categoryID, "category-id", "-1", "category ID for the package")
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
func createPackage(ctx context.Context, client registry.HTTPClient, name, fileName, categoryID string) (string, error) {
	payload := map[string]any{
		"packageName":          name,
		"fileName":             fileName,
		"categoryId":           categoryID,
		"priority":             3,
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
// as multipart/form-data.
func uploadPackageFile(ctx context.Context, uploader registry.FileUploader, pkgID, filePath, fileName string, fileSize int64) error {
	boundary := "jamf-cli-upload-boundary"

	header := fmt.Sprintf("--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\nContent-Type: application/octet-stream\r\n\r\n",
		boundary, fileName)
	footer := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	contentLength := int64(len(header)) + fileSize + int64(len(footer))

	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()

		if _, err := pw.Write([]byte(header)); err != nil {
			pw.CloseWithError(err)
			return
		}

		f, err := os.Open(filePath)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(pw, f); err != nil {
			pw.CloseWithError(err)
			return
		}

		if _, err := pw.Write([]byte(footer)); err != nil {
			pw.CloseWithError(err)
			return
		}
	}()

	path := fmt.Sprintf("/v1/packages/%s/upload", url.PathEscape(pkgID))
	contentType := "multipart/form-data; boundary=" + boundary

	resp, err := uploader.Upload(ctx, path, pr, contentType, contentLength)
	if err != nil {
		return fmt.Errorf("uploading package: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// updatePackageHashes sets the hash fields on an existing package record.
func updatePackageHashes(ctx context.Context, client registry.HTTPClient, pkgID string, h fileHashes) error {
	// Fetch current package to preserve existing fields
	data, err := fetchJSON(ctx, client, fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID)))
	if err != nil {
		return fmt.Errorf("fetching package for hash update: %w", err)
	}

	// Update hash fields
	data["hashType"] = "SHA3_512"
	data["hashValue"] = h.sha3
	data["sha3512"] = h.sha3
	data["sha256"] = h.sha256
	data["md5"] = h.md5

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))
	resp, err := client.Do(ctx, "PUT", path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// verifyPackageUpload polls the package until the server's computed hash matches
// the expected value. On each iteration it nudges the JCDS inventory refresh
// (errors ignored — transient 500s and concurrency failures are expected).
// Returns nil on match, or an error if the hash mismatches or times out.
func verifyPackageUpload(ctx context.Context, client registry.HTTPClient, pkgID, fileName, expectedSHA3 string) error {
	const (
		verifyTimeout  = 10 * time.Minute
		verifyInterval = 10 * time.Second
	)

	refreshPath := fmt.Sprintf("/v1/cloudDistributionPoint/refresh-inventory?file-name=%s", url.QueryEscape(fileName))
	pkgPath := fmt.Sprintf("/v1/packages/%s", url.PathEscape(pkgID))

	deadline := time.Now().Add(verifyTimeout)
	for time.Now().Before(deadline) {
		// Wait first — give the server time to process
		select {
		case <-ctx.Done():
			return ctx.Err()
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
			return nil
		}

		// Server computed a hash but it doesn't match — corrupted upload
		return fmt.Errorf("hash mismatch: server=%s local=%s", hashValue, expectedSHA3)
	}

	return fmt.Errorf("timed out after %v waiting for server to confirm hash", verifyTimeout)
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
