// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	progen "github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
)

func TestPrintVersion_DefaultBanner(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, "v1.0.0", "abc1234", "2026-05-08T00:00:00Z", false)
	out := buf.String()

	for _, want := range []string{"v1.0.0", "abc1234", "2026-05-08T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("default banner missing %q, got %q", want, out)
		}
	}
	if strings.Contains(out, "spec sources") {
		t.Errorf("default banner should not include provenance, got %q", out)
	}
}

func TestPrintVersion_VerboseShowsProvenance(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, "v1.0.0", "abc1234", "2026-05-08T00:00:00Z", true)
	out := buf.String()

	for _, want := range []string{"Pro spec sources:", "Pro Classic spec sources:", "Platform spec sources:"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q, got %q", want, out)
		}
	}
}

// TestPrintVersion_ClassicSectionExcludesModern guards the partitioning:
// the classic section must contain only specs/classic/* entries, and the
// modern Pro section must not include any of them. This is the visible
// half of the reviewer's concern in #193 — Classic specs landing under
// "Pro spec sources:" rather than getting their own header.
func TestPrintVersion_ClassicSectionExcludesModern(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, "v1.0.0", "abc1234", "2026-05-08T00:00:00Z", true)
	out := buf.String()

	proIdx := strings.Index(out, "Pro spec sources:")
	classicIdx := strings.Index(out, "Pro Classic spec sources:")
	platformIdx := strings.Index(out, "Platform spec sources:")
	if proIdx < 0 || classicIdx < 0 || platformIdx < 0 {
		t.Fatalf("missing one or more section headers in:\n%s", out)
	}
	if proIdx >= classicIdx || classicIdx >= platformIdx {
		t.Errorf("expected order Pro -> Pro Classic -> Platform, got idx=%d,%d,%d", proIdx, classicIdx, platformIdx)
	}

	proSection := out[proIdx:classicIdx]
	classicSection := out[classicIdx:platformIdx]

	if strings.Contains(proSection, "specs/classic/") {
		t.Errorf("modern Pro section leaked a classic entry:\n%s", proSection)
	}
	hasClassic := false
	for _, s := range progen.Sources {
		if strings.HasPrefix(s.File, classicSpecPrefix) {
			hasClassic = true
			break
		}
	}
	if hasClassic && !strings.Contains(classicSection, "specs/classic/") {
		t.Errorf("Pro Classic section missing classic entries:\n%s", classicSection)
	}
}

func TestShortHash(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"dca326f3407f53b3c7f000c598e28badf70cd608e51250396d0e35eabde0ab5b", "dca326f3407f"},
		{"abc", "abc"}, // shorter than 12 → returned as-is
		{"", ""},
	}
	for _, tc := range tests {
		if got := shortHash(tc.in); got != tc.want {
			t.Errorf("shortHash(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewVersionCmd_HelpMentionsVerbose(t *testing.T) {
	cmd := newVersionCmd(nil, "v1.0.0", "deadbeef", "2026-05-08")
	if !cmd.Flags().HasFlags() {
		t.Fatal("version command should declare a -v flag")
	}
	flag := cmd.Flags().Lookup("verbose")
	if flag == nil {
		t.Fatal("--verbose flag missing")
	}
	if flag.Shorthand != "v" {
		t.Errorf("expected shorthand 'v', got %q", flag.Shorthand)
	}
}

func TestPartitionProSources_RoutesByPrefix(t *testing.T) {
	in := []progen.SpecSource{
		{File: "specs/Building.yaml", SHA256: "aaa"},
		{File: "specs/classic/resources.yaml", SHA256: "bbb"},
		{File: "specs/Category.yaml", SHA256: "ccc"},
		{File: "specs/classic/other.yaml", SHA256: "ddd"},
	}
	modern, classic := partitionProSources(in)
	if len(modern) != 2 || modern[0].File != "specs/Building.yaml" || modern[1].File != "specs/Category.yaml" {
		t.Errorf("modern bucket wrong: %+v", modern)
	}
	if len(classic) != 2 || classic[0].File != "specs/classic/resources.yaml" || classic[1].File != "specs/classic/other.yaml" {
		t.Errorf("classic bucket wrong: %+v", classic)
	}
}

// TestBuildVersionReport_VerboseShape locks the JSON contract — the
// reviewer's primary nit was that `version -v --output json` printed
// text. This test exists so a future refactor can't silently regress
// the structured shape.
func TestBuildVersionReport_VerboseShape(t *testing.T) {
	r := buildVersionReport("v1.0.0", "abc1234", "2026-05-08T00:00:00Z", true)
	if r.Version != "v1.0.0" || r.Commit != "abc1234" || r.Built != "2026-05-08T00:00:00Z" {
		t.Errorf("banner fields wrong: %+v", r)
	}
	if r.Sources == nil {
		t.Fatal("verbose report must include specSources")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)
	for _, want := range []string{
		`"version":"v1.0.0"`,
		`"commit":"abc1234"`,
		`"built":"2026-05-08T00:00:00Z"`,
		`"specSources":{`,
		`"pro":[`,
		`"proClassic":[`,
		`"platform":[`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildVersionReport_NonVerboseOmitsSources(t *testing.T) {
	r := buildVersionReport("v1.0.0", "abc1234", "2026-05-08T00:00:00Z", false)
	if r.Sources != nil {
		t.Errorf("non-verbose report must not include specSources, got %+v", r.Sources)
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "specSources") {
		t.Errorf("specSources must be omitted from JSON, got %s", string(data))
	}
}
