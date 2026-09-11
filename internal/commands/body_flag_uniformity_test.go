// Copyright 2026, Jamf Software LLC

package commands

import "testing"

// bodyFileUploadLeaves are the leaves whose --file flag names a *payload* to
// upload rather than a request body to send, so the rename to --from-file does
// not apply to them.
//
// The distinction is not cosmetic. --from-file carries a promise across the
// whole CLI — the same path can arrive on stdin instead — and a multipart
// upload cannot keep it: the transport needs a filename and a length, so there
// is nothing to pipe. Renaming these would produce one flag name with two
// capabilities, which is the inconsistency the rename exists to remove, spelled
// the other way round.
//
// Keyed by full command path, so a new upload command has to be considered
// rather than silently inheriting the exemption.
var bodyFileUploadLeaves = map[string]string{
	"pro packages upload":                        "multipart .pkg upload",
	"pro icon upload":                            "multipart icon image upload",
	"pro self-service upload":                    "multipart branding image upload",
	"pro mobile-device-prestages upload":         "multipart prestage attachment upload",
	"pro inventory-preload upload":               "multipart CSV upload",
	"pro inventory-preload csv-validate":         "multipart CSV upload, validated not stored",
	"pro computer-extension-attributes upload":   "multipart attribute payload upload",
	"pro enrollment-customization-images upload": "multipart enrollment image upload",
	"pro computer-inventory upload":              "multipart computer attachment upload",
	// --file/--dir are a pair here: one YAML document, or a directory of them.
	// Renaming the singular half alone would break the pairing, and neither
	// half accepts a pipe.
	"protect unified-logging-filters import": "paired with --dir for a YAML import",
	"protect analytics import":               "paired with --dir for a YAML import",
}

// TestRequestBodyFlagIsUniformlyFromFile holds every leaf that reads a request
// body from a path to the one flag name: --from-file.
//
// Four products reached this CLI through four generators, and two of them
// (Platform, Security Cloud) spelled the body flag --file while Pro, Classic,
// Protect and School spelled it --from-file. `pro platform-device-groups` had
// both, in one file: patch and patch-members took --file, apply took
// --from-file. That is the split this test prevents recurring — a walk, not a
// count, so a new generated or hand-written command is covered without editing
// anything here.
//
// The --file flags that remain are uploads, listed above with their reason.
// This test deliberately says nothing about whether a given --from-file reads
// stdin: the flag has a third legitimate sense — a newline-delimited list of
// IDs or names for the bulk deletes and `multi` — which never did and should
// not claim to. Whether the *body* readers do is covered where the behaviour
// lives, in internal/platform and internal/security ReadBody tests.
func TestRequestBodyFlagIsUniformlyFromFile(t *testing.T) {
	leaves := runnableLeaves(NewRootCmd("test", "none", "none", "none"))
	if len(leaves) == 0 {
		t.Fatal("no leaves walked")
	}

	seenUpload := map[string]bool{}
	sawFromFile := false
	for _, l := range leaves {
		if l.cmd.Flags().Lookup("from-file") != nil {
			sawFromFile = true
		}
		if l.cmd.Flags().Lookup("file") == nil {
			continue
		}
		if _, exempt := bodyFileUploadLeaves[l.path]; exempt {
			seenUpload[l.path] = true
			continue
		}
		t.Errorf("leaf %q takes --file: the request-body flag is --from-file everywhere else in this CLI. "+
			"Rename it, or add it to bodyFileUploadLeaves with the reason it is an upload rather than a body.", l.path)
	}

	if !sawFromFile {
		t.Error("no leaf exposes --from-file — the walk found nothing to check")
	}
	for path, reason := range bodyFileUploadLeaves {
		if !seenUpload[path] {
			t.Errorf("bodyFileUploadLeaves lists %q (%s) but no such leaf takes --file — stale entry", path, reason)
		}
	}
}
