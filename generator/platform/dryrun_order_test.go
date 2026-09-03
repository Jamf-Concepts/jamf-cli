// Copyright 2026, Jamf Software LLC

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDryRunPreviewIsEmittedBeforeTheConfirmation pins the ordering of the two
// gates on every destructive generated platform command. ConfirmAction errors
// when --yes is absent and stdin is not a terminal, so with the confirmation
// first, `-n` on a delete previewed nothing in CI and the operator's only fix
// was `-n --yes` — a command line that deletes for real the day the -n falls
// off it, or out of JAMF_CLI_ARGS. Interactively the same order prompted
// "Continue? [y/N]" and printed [dry-run] afterwards, teaching the operator
// that confirming is harmless.
//
// Over the live specs and every generated function rather than one specimen,
// because the two statements are emitted from separate template branches: the
// single-request path previews then confirms, and the paginated path has no
// preview to sit behind.
func TestDryRunPreviewIsEmittedBeforeTheConfirmation(t *testing.T) {
	specsDir, err := filepath.Abs("../../specs/platform")
	if err != nil {
		t.Fatalf("resolving specs dir: %v", err)
	}
	resources, _, err := LoadResources(specsDir)
	if err != nil {
		t.Fatalf("LoadResources: %v", err)
	}
	outDir := t.TempDir()
	files, err := Generate(resources, outDir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var checked int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		// One chunk per generated constructor, so an ordering fault in one
		// command cannot be masked by a correctly ordered sibling above it.
		for _, fn := range strings.Split(string(b), "\nfunc new") {
			confirm := strings.Index(fn, "platform.ConfirmAction(")
			if confirm < 0 {
				continue
			}
			preview := strings.Index(fn, "platform.ReportDryRun(")
			if preview < 0 {
				// A destructive GET (a paginated read) has nothing to preview.
				continue
			}
			checked++
			if preview > confirm {
				t.Errorf("%s: the confirmation is emitted before the dry-run preview, so -n cannot be used without --yes", filepath.Base(path))
			}
			// Name resolution has to stay ahead of both, or the preview reports
			// an unsubstituted {id} placeholder rather than the path that would
			// be sent.
			if resolve := strings.Index(fn, "resolvedID = "); resolve >= 0 && resolve > preview {
				t.Errorf("%s: --name is resolved after the dry-run preview, so the preview cannot report the resolved path", filepath.Base(path))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no generated platform command carries both a confirmation and a dry-run preview — the ordering this test pins is no longer exercised")
	}
}
