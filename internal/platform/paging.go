// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// PageFlags holds a generated paginated list's walk flags: the same four, with
// the same meaning, as a Jamf Pro list — --all, --page, --page-size, --limit.
type PageFlags struct {
	All      bool
	Page     int
	PageSize int
	Limit    int

	cmd     *cobra.Command
	ceiling int
}

// RegisterPageFlags registers the walk flags on cmd. ceiling is the largest
// page the endpoint honours (parser.PageSizeFromSpec).
func RegisterPageFlags(cmd *cobra.Command, f *PageFlags, ceiling int) {
	f.cmd = cmd
	f.ceiling = ceiling
	cmd.Flags().BoolVar(&f.All, "all", true, "Fetch all pages (set --all=false for single page)")
	cmd.Flags().IntVar(&f.Page, "page", 0, "Page to return, zero-based; setting it returns that page alone instead of every page")
	cmd.Flags().IntVar(&f.PageSize, "page-size", defaultPageSize(ceiling),
		fmt.Sprintf("Results per page, max %d; --all requests %d unless this asks for fewer", ceiling, ceiling))
	cmd.Flags().IntVar(&f.Limit, "limit", 0, "Maximum total results to return (0 = unlimited)")
}

// defaultPageSize is the single-page size when --page-size is not given: the
// API's own default, or the ceiling where an endpoint declares a lower one.
func defaultPageSize(ceiling int) int {
	return min(100, ceiling)
}

// ListPages fetches a paginated collection at path as the flags in f ask and
// returns its rows as the server's own JSON.
//
// The whole-collection walk goes through the SDK's ListAllRaw, which halves the
// page size on a server timeout and resumes from the rows already held, rather
// than re-sending the identical page (issue 392, the Jamf Pro half of which is
// registry.ShrinkAfterTimeout). Each shrink is reported on stderr, as Pro's walk
// reports it.
//
// --page-size is honoured below the ceiling and clamped above it out loud: a
// Jamf service may answer an oversized page-size by silently clamping it, which
// is how a walk is truncated (issue 385).
func ListPages(ctx context.Context, cliCtx *registry.CLIContext, path string, q url.Values, f *PageFlags, arrayKey string) ([]json.RawMessage, error) {
	if arrayKey == "" {
		arrayKey = "results"
	}
	if f.Limit < 0 {
		return nil, exitcode.New(exitcode.Usage, fmt.Sprintf("--limit must not be negative, got %d", f.Limit))
	}
	if f.Page < 0 {
		return nil, exitcode.New(exitcode.Usage, fmt.Sprintf("--page must not be negative, got %d", f.Page))
	}
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize(f.ceiling)
	}
	if pageSize > f.ceiling {
		cliCtx.Output.NotePageSizeClamped(pageSize, f.ceiling)
		pageSize = f.ceiling
	}
	transport := cliCtx.PlatformSDKClient.Transport()

	if f.All && !f.cmd.Flags().Changed("page") {
		walkSize := f.ceiling
		if f.cmd.Flags().Changed("page-size") {
			walkSize = pageSize
		}
		// A small --limit stays a small request.
		if f.Limit > 0 && f.Limit < walkSize {
			walkSize = f.Limit
		}
		rows, err := transport.ListAllRaw(ctx, path, q, jamfplatform.RawPageWalk{
			PageSize:     walkSize,
			ResultsField: arrayKey,
			Limit:        f.Limit,
			OnShrink:     cliCtx.Output.NotePageSizeReduced,
		})
		if err != nil {
			return nil, serverTimeout(ctx, err)
		}
		return rows, nil
	}

	pq := url.Values{}
	for k, v := range q {
		pq[k] = v
	}
	pq.Set("page", strconv.Itoa(f.Page))
	pq.Set("page-size", strconv.Itoa(pageSize))
	var envelope map[string]json.RawMessage
	if err := transport.DoExpect(ctx, http.MethodGet, path+"?"+pq.Encode(), nil, http.StatusOK, &envelope); err != nil {
		return nil, serverTimeout(ctx, err)
	}
	rows := []json.RawMessage{}
	if raw, ok := envelope[arrayKey]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("%q in the response is not an array: %w", arrayKey, err)
		}
	}
	if f.Limit > 0 && len(rows) > f.Limit {
		rows = rows[:f.Limit]
	}
	return rows, nil
}

// serverTimeout marks a server timeout with registry.ErrServerTimeout, the
// error a Jamf Pro request reports for the same condition, and names the
// remedy. The SDK error stays in the chain, so its status and traceId still
// reach the 403/404 annotators. A deadline on the caller's own context is
// passed through: a smaller page does not extend it.
func serverTimeout(ctx context.Context, err error) error {
	if !jamfplatform.IsServerTimeout(err) || ctx.Err() != nil {
		return err
	}
	return exitcode.Wrap(exitcode.General, fmt.Errorf("%w: %w", registry.ErrServerTimeout, err)).
		WithHint("the server could not assemble a page in time, even at the smallest page size; narrow the list with --filter, or retry later")
}
