// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// NameLookup is the --list-file lookup for a Platform collection: one listing
// (paged, see listAllItems) serves every line in the file. Each item answers
// to its ID and to the same fields --name reads — nameField when the resource
// names one, then name, title and displayName — and resolve.ResolveNamedEntries
// matches each line against those.
//
// IDs are matched against the listing rather than recognised by their shape,
// because Platform IDs have no one shape: most are UUIDs, an SSO domain's is
// an integer, and an SSO connection's is `con_` plus 16 characters, which a
// shape test read as a name and reported unmatched. Matching the listing also
// means an ID that names nothing is skipped before any request is sent for it.
// An item the list returns with no ID cannot be addressed and is left out.
func NameLookup(client *jamfplatform.Client, listPath, nameField string) resolve.NameLookup {
	return func(ctx context.Context, _ []string) ([]resolve.NamedRecord, error) {
		items, err := listAllItems(ctx, client, listPath)
		if err != nil {
			return nil, err
		}
		fields := defaultNameFields
		if nameField != "" {
			fields = append([]string{nameField}, defaultNameFields...)
		}
		var out []resolve.NamedRecord
		for _, item := range items {
			id := extractID(item)
			if id == "" {
				continue
			}
			out = append(out, resolve.NamedRecord{ID: id, Name: id})
			seen := map[string]bool{id: true}
			for _, f := range fields {
				if v, ok := item[f].(string); ok && v != "" && !seen[v] {
					seen[v] = true
					out = append(out, resolve.NamedRecord{ID: id, Name: v})
				}
			}
		}
		return out, nil
	}
}

// NamedTargets resolves a Platform --list-file against the collection at
// listPath: every line, ID or name, is matched in one listing (see NameLookup).
func NamedTargets(ctx context.Context, client *jamfplatform.Client, label, listPath, nameField, path string) ([]resolve.Target, int, error) {
	return resolve.ResolveNamedFile(ctx, resolve.NamedEntrySpec{
		Label:  label,
		Lookup: NameLookup(client, listPath, nameField),
		IsID:   neverAnID,
	}, path)
}

// neverAnID sends every line to the lookup, IDs included.
func neverAnID(string) bool { return false }

// PrintListRecords is `get --list-file` for every command whose records are
// fetched one at a time: it reads each target with fetch — the request
// `get <id>` sends — and prints the records as one JSON array, in the order the
// list named them, through PrintRaw, so every -o format, --select and
// --out-file apply. An empty result prints [], never null. A record that
// cannot be read is reported on stderr and counted with the skipped entries;
// see FinishListBatch for the exit code.
func PrintListRecords(cmd *cobra.Command, out registry.OutputFormatter, targets []resolve.Target, skipped int, noun string, fetch func(ctx context.Context, id string) (any, error)) error {
	if skipped > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %d --list-file entr(ies) could not be resolved and will be skipped\n", skipped)
	}
	records := make([]any, 0, len(targets))
	failed := skipped
	var firstErr error
	for _, t := range targets {
		rec, err := fetch(cmd.Context(), t.ID)
		if err != nil {
			err = t.Hide(err)
			_, _ = fmt.Fprintf(os.Stderr, "  warning: could not read %s: %s\n", t.Label, output.SanitizeTerminal(err.Error()))
			if firstErr == nil {
				firstErr = err
			}
			failed++
			continue
		}
		records = append(records, rec)
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err := out.PrintRaw(b); err != nil {
		return err
	}
	return FinishListBatch(cmd, len(records), failed, firstErr, noun)
}

// FinishListBatch maps a --list-file tally to the process result: nil when
// nothing failed; a partial failure (exit 7) when some succeeded, or a warning
// and nil under --allow-partial-failure; firstErr's own exit code when nothing
// succeeded.
func FinishListBatch(cmd *cobra.Command, succeeded, failed int, firstErr error, noun string) error {
	if failed == 0 {
		return nil
	}
	if allow, _ := cmd.Flags().GetBool("allow-partial-failure"); allow && succeeded > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %d of %d %s failed; continuing (--allow-partial-failure)\n", failed, succeeded+failed, noun)
		return nil
	}
	return exitcode.PartialOrPropagate(succeeded, failed, firstErr, fmt.Sprintf("%d of %d %s failed", failed, succeeded+failed, noun))
}

// RunListDelete is `delete --list-file` for a generated Platform command: the
// dry-run preview first, then one confirmation for the whole batch, then one
// request per target, each reported on stderr.
func RunListDelete(cmd *cobra.Command, dryRun, yes bool, targets []resolve.Target, skipped int, noun string, send func(ctx context.Context, id string) (path string, err error), previewPath func(id string) string) error {
	if skipped > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %d --list-file entr(ies) could not be resolved and will be skipped\n", skipped)
	}
	if dryRun {
		for _, t := range targets {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[dry-run] DELETE %s (%s)\n", previewPath(t.ID), t.Label)
		}
		return nil
	}
	if err := ConfirmAction("delete", fmt.Sprintf("%d %s", len(targets), noun), yes); err != nil {
		return err
	}
	ok, failed := 0, skipped
	var firstErr error
	for _, t := range targets {
		if _, err := send(cmd.Context(), t.ID); err != nil {
			err = t.Hide(err)
			_, _ = fmt.Fprintf(os.Stderr, "delete %s failed: %s\n", t.Label, output.SanitizeTerminal(err.Error()))
			if firstErr == nil {
				firstErr = err
			}
			failed++
			continue
		}
		_, _ = fmt.Fprintf(os.Stderr, "Deleted %s\n", t.Label)
		ok++
	}
	return FinishListBatch(cmd, ok, failed, firstErr, noun+" deletes")
}
