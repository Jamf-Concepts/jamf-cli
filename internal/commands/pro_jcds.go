// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// File-status values used in jcdsSyncFileResult.
const (
	jcdsStatusDownloaded    = "downloaded"
	jcdsStatusUpdated       = "updated"
	jcdsStatusSkipped       = "skipped"
	jcdsStatusDeleted       = "deleted"
	jcdsStatusFailed        = "failed"
	jcdsStatusWouldDownload = "would-download"
	jcdsStatusWouldUpdate   = "would-update"
	jcdsStatusWouldDelete   = "would-delete"
)

// jcdsSyncFileResult is the per-file entry in the sync report.
type jcdsSyncFileResult struct {
	FileName string `json:"fileName"`
	Status   string `json:"status"` // see jcdsStatus* constants
	Error    string `json:"error,omitempty"`
}

// jcdsSyncReport is the full structured output of jcds sync.
type jcdsSyncReport struct {
	DryRun  bool                 `json:"dryRun,omitempty"`
	Summary jcdsSyncSummary      `json:"summary"`
	Files   []jcdsSyncFileResult `json:"files"`
}

// jcdsSyncSummary holds the aggregate counts.
type jcdsSyncSummary struct {
	Downloaded int64 `json:"downloaded"`
	Updated    int64 `json:"updated"`
	Deleted    int64 `json:"deleted"`
	UpToDate   int64 `json:"upToDate"`
	Failed     int64 `json:"failed"`
}

// jcdsFileData matches the FileData schema from the JCDS API.
type jcdsFileData struct {
	FileName string `json:"fileName"`
	Length   int64  `json:"length"`
	MD5      string `json:"md5"`
	Region   string `json:"region"`
	SHA3     string `json:"sha3"`
}

// jcdsHTTPStatusError is returned by jcdsStreamToFile for non-2xx HTTP responses.
type jcdsHTTPStatusError struct {
	StatusCode int
}

func (e *jcdsHTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// jcdsFetchPresignedURL fetches a fresh pre-signed download URL from the JCDS API.
func jcdsFetchPresignedURL(ctx context.Context, client registry.HTTPClient, fileName string) (string, error) {
	apiPath := "/v1/jcds/files/" + url.PathEscape(fileName)
	resp, err := client.Do(ctx, "GET", apiPath, nil)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing download URL response: %w", err)
	}
	if result.URI == "" {
		return "", fmt.Errorf("API returned empty download URI")
	}
	return result.URI, nil
}

// jcdsDownloadFile fetches the pre-signed URL then streams the file to outPath.
// Retries once with a fresh URL on HTTP 403 — CloudFront signed URLs have a short
// TTL and can expire if the API response is slow or requests queue under concurrency.
func jcdsDownloadFile(ctx context.Context, client registry.HTTPClient, fileName, outPath string) error {
	uri, err := jcdsFetchPresignedURL(ctx, client, fileName)
	if err != nil {
		return err
	}

	n, err := jcdsStreamToFile(ctx, uri, outPath)
	var httpErr *jcdsHTTPStatusError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusForbidden {
		// Pre-signed URL expired — fetch a fresh one and retry once.
		fmt.Fprintf(os.Stderr, "warning: %s pre-signed URL expired (403), retrying with fresh URL\n", fileName)
		uri, err = jcdsFetchPresignedURL(ctx, client, fileName)
		if err != nil {
			return err
		}
		n, err = jcdsStreamToFile(ctx, uri, outPath)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Saved %s (%d bytes)\n", outPath, n)
	return nil
}

// jcdsStreamToFile downloads uri to outPath via a temp-file+rename.
// Returns bytes written; non-2xx responses return a *jcdsHTTPStatusError.
func jcdsStreamToFile(ctx context.Context, uri, outPath string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return 0, fmt.Errorf("creating download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Drain body so the connection can be reused, then surface status to caller.
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, &jcdsHTTPStatusError{StatusCode: resp.StatusCode}
	}

	dir := filepath.Dir(outPath)
	tmp, err := os.CreateTemp(dir, ".jcds-tmp-*")
	if err != nil {
		return 0, fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	buf := make([]byte, 4<<20) // 4MB — better throughput for large pkg files than default 32KB
	n, err := io.CopyBuffer(tmp, resp.Body, buf)
	if err != nil {
		_ = tmp.Close()
		return 0, fmt.Errorf("writing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return 0, fmt.Errorf("moving file into place: %w", err)
	}

	return n, nil
}

// jcdsFileSHA3 computes the hex SHA3-512 of a local file.
func jcdsFileSHA3(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha3.New512()
	buf := make([]byte, 4<<20) // 4MB — match download buffer size for large pkg files
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newJcdsDownloadCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		flagName   string
		flagOutput string
	)

	cmd := &cobra.Command{
		Use:   "download [<fileName>]",
		Short: "Download a file from the Jamf Cloud Distribution Service",
		Long:  "Retrieves a pre-signed download URL for the file and streams it to disk.",
		Example: `  # Download by filename
  jamf-cli pro jcds download MyPackage.pkg

  # Download with explicit output path
  jamf-cli pro jcds download MyPackage.pkg --output /tmp/MyPackage.pkg`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reqCtx := cmd.Context()

			var fileName string
			if flagName != "" {
				fileName = flagName
			} else if len(args) > 0 {
				fileName = args[0]
			} else {
				return fmt.Errorf("provide a <fileName> argument or --name")
			}

			outPath := flagOutput
			if outPath == "" {
				outPath = filepath.Base(fileName)
			}

			return jcdsDownloadFile(reqCtx, cliCtx.Client, fileName, outPath)
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "Name of the file to download")
	cmd.Flags().StringVarP(&flagOutput, "output", "O", "", "Output file path (default: <fileName>)")

	return cmd
}

func newJcdsSyncCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		flagDir         string
		flagDelete      bool
		flagDryRun      bool
		flagConcurrency int
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync JCDS files to a local directory",
		Long: `Syncs the Jamf Cloud Distribution Service to a local directory.

  - Downloads files missing locally
  - Re-downloads files whose SHA3-512 differs from JCDS (updated remotely)
  - Optionally deletes local files not present on JCDS (--delete)

Designed for scheduled runs on file-share distribution points.`,
		Example: `  # Sync JCDS to /Volumes/Packages (safe — no deletion)
  jamf-cli pro jcds sync --dir /Volumes/Packages

  # Sync and remove local files not on JCDS
  jamf-cli pro jcds sync --dir /Volumes/Packages --delete

  # Preview what would change without doing anything
  jamf-cli pro jcds sync --dir /Volumes/Packages --delete --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqCtx := cmd.Context()

			if flagConcurrency < 1 {
				return fmt.Errorf("--concurrency must be at least 1")
			}
			if err := os.MkdirAll(flagDir, 0o755); err != nil {
				return fmt.Errorf("creating destination directory: %w", err)
			}

			// Fetch JCDS file list.
			resp, err := cliCtx.Client.Do(reqCtx, "GET", "/v1/jcds/files", nil)
			if err != nil {
				return err
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return fmt.Errorf("reading file list: %w", err)
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}

			var jcdsFiles []jcdsFileData
			if err := json.Unmarshal(body, &jcdsFiles); err != nil {
				return fmt.Errorf("parsing file list: %w", err)
			}

			// Build JCDS filename set for existence checks.
			jcdsSet := make(map[string]struct{}, len(jcdsFiles))
			for _, f := range jcdsFiles {
				jcdsSet[f.FileName] = struct{}{}
			}

			// Scan local directory.
			entries, err := os.ReadDir(flagDir)
			if err != nil {
				return fmt.Errorf("reading directory: %w", err)
			}
			localFiles := make(map[string]bool, len(entries))
			for _, e := range entries {
				if !e.IsDir() {
					localFiles[e.Name()] = true
				}
			}

			var downloaded, updated, deleted, skipped, failed atomic.Int64

			// filesMu guards fileResults which is written by concurrent goroutines.
			var filesMu sync.Mutex
			fileResults := make([]jcdsSyncFileResult, 0, len(jcdsFiles)+len(localFiles))
			addFileResult := func(r jcdsSyncFileResult) {
				filesMu.Lock()
				fileResults = append(fileResults, r)
				filesMu.Unlock()
			}

			if flagDryRun {
				// Dry run: report what would happen without modifying anything.
				if flagDelete {
					for name := range localFiles {
						if _, onJCDS := jcdsSet[name]; !onJCDS {
							localPath := filepath.Join(flagDir, name)
							fmt.Fprintf(os.Stderr, "[dry-run] Would delete %s\n", localPath)
							addFileResult(jcdsSyncFileResult{FileName: name, Status: jcdsStatusWouldDelete})
							deleted.Add(1)
						}
					}
				}
				for _, f := range jcdsFiles {
					localPath := filepath.Join(flagDir, f.FileName)
					if !localFiles[f.FileName] {
						fmt.Fprintf(os.Stderr, "[dry-run] Would download %s (missing)\n", f.FileName)
						addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusWouldDownload})
						downloaded.Add(1)
						continue
					}
					if f.SHA3 != "" {
						localHash, err := jcdsFileSHA3(localPath)
						if err != nil {
							return fmt.Errorf("hashing %s: %w", localPath, err)
						}
						if !strings.EqualFold(localHash, f.SHA3) {
							fmt.Fprintf(os.Stderr, "[dry-run] Would update %s (SHA3-512 mismatch)\n", f.FileName)
							addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusWouldUpdate})
							updated.Add(1)
							continue
						}
					}
					addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusSkipped})
					skipped.Add(1)
				}
			} else {
				// Live run: delete orphans then download/update in parallel.
				if flagDelete {
					for name := range localFiles {
						if _, onJCDS := jcdsSet[name]; !onJCDS {
							localPath := filepath.Join(flagDir, name)
							if err := os.Remove(localPath); err != nil {
								fmt.Fprintf(os.Stderr, "warning: could not delete %s: %v\n", localPath, err)
								addFileResult(jcdsSyncFileResult{FileName: name, Status: jcdsStatusFailed, Error: err.Error()})
								failed.Add(1)
								continue
							}
							fmt.Fprintf(os.Stderr, "Deleted %s (not on JCDS)\n", localPath)
							addFileResult(jcdsSyncFileResult{FileName: name, Status: jcdsStatusDeleted})
							deleted.Add(1)
						}
					}
				}

				// Dispatch ALL work (hashing + downloading) into the worker pool
				// so the main goroutine never blocks on large-file SHA3 computation.
				sem := make(chan struct{}, flagConcurrency)
				var g errgroup.Group

				for _, f := range jcdsFiles {
					localPath := filepath.Join(flagDir, f.FileName)
					exists := localFiles[f.FileName]

					sem <- struct{}{}

					g.Go(func() error {
						defer func() { <-sem }()

						if !exists {
							if err := jcdsDownloadFile(reqCtx, cliCtx.Client, f.FileName, localPath); err != nil {
								fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", f.FileName, err)
								addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusFailed, Error: err.Error()})
								failed.Add(1)
								return nil
							}
							addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusDownloaded})
							downloaded.Add(1)
							return nil
						}

						if f.SHA3 != "" {
							localHash, err := jcdsFileSHA3(localPath)
							if err != nil {
								fmt.Fprintf(os.Stderr, "warning: skipping %s (hash error): %v\n", f.FileName, err)
								addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusFailed, Error: err.Error()})
								failed.Add(1)
								return nil
							}
							if !strings.EqualFold(localHash, f.SHA3) {
								fmt.Fprintf(os.Stderr, "Updating %s (SHA3-512 changed)\n", f.FileName)
								if err := jcdsDownloadFile(reqCtx, cliCtx.Client, f.FileName, localPath); err != nil {
									fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", f.FileName, err)
									addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusFailed, Error: err.Error()})
									failed.Add(1)
									return nil
								}
								addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusUpdated})
								updated.Add(1)
								return nil
							}
						}

						addFileResult(jcdsSyncFileResult{FileName: f.FileName, Status: jcdsStatusSkipped})
						skipped.Add(1)
						return nil
					})
				}

				if err := g.Wait(); err != nil {
					return err
				}
			}

			report := jcdsSyncReport{
				DryRun: flagDryRun,
				Summary: jcdsSyncSummary{
					Downloaded: downloaded.Load(),
					Updated:    updated.Load(),
					Deleted:    deleted.Load(),
					UpToDate:   skipped.Load(),
					Failed:     failed.Load(),
				},
				Files: fileResults,
			}
			data, err := json.Marshal(report)
			if err != nil {
				return fmt.Errorf("marshalling sync report: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}

	cmd.Flags().StringVar(&flagDir, "dir", "", "Local directory to sync into (required)")
	cmd.Flags().BoolVar(&flagDelete, "delete", false, "Delete local files not present on JCDS")
	cmd.Flags().BoolVarP(&flagDryRun, "dry-run", "n", false, "Preview changes without executing")
	cmd.Flags().IntVar(&flagConcurrency, "concurrency", 4, "Number of parallel downloads")
	_ = cmd.MarkFlagRequired("dir")

	return cmd
}
