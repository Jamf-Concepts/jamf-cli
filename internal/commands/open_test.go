// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// openProducts are the namespaces that carry an `open`. Every product with a
// web interface has one, and the list is spelled out rather than derived so
// adding a product namespace without its `open` fails here.
var openProducts = []string{"pro", "protect", "school", "security"}

// TestEveryProductHasAnOpenCommand pins the shape all four share: the same
// verb, the same --print flag, and a help group — an ungrouped command is
// callable but absent from the grouped `--help` output, which is where someone
// would look for it.
func TestEveryProductHasAnOpenCommand(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	for _, product := range openProducts {
		parent := findSubcommand(root, product)
		if parent == nil {
			t.Fatalf("%s command not found", product)
		}
		open := findSubcommand(parent, "open")
		if open == nil {
			t.Errorf("%s open not wired — add it in %s.go", product, product)
			continue
		}
		if open.Flags().Lookup("print") == nil {
			t.Errorf("%s open has no --print flag: the URL must be reachable without a browser", product)
		}
		if open.GroupID == "" {
			t.Errorf("%s open has no GroupID — add it to %sGroupMap in groups.go", product, product)
		}
		if open.Runnable() == false {
			t.Errorf("%s open is not runnable", product)
		}
	}
}

// TestOpenSkipsAuthOnlyWhereItCallsNothing asserts the split that makes the
// feature work on a half-configured profile without making `pro open` lie: the
// three products whose URL is their configured base URL opt out of auth
// resolution, and `pro open` does not, because on a gateway profile the Jamf
// Pro URL is not a credential input at all and has to be read from the API.
func TestOpenSkipsAuthOnlyWhereItCallsNothing(t *testing.T) {
	root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")

	want := map[string]bool{"pro": false, "protect": true, "school": true, "security": true}
	for product, skip := range want {
		open := findSubcommand(findSubcommand(root, product), "open")
		if open == nil {
			t.Fatalf("%s open not wired", product)
		}
		got := open.Annotations[noAuthAnnotation] == "true"
		if got != skip {
			t.Errorf("%s open: %s = %v, want %v", product, noAuthAnnotation, got, skip)
		}
	}
}

// captureOpenOutput runs openOrPrint with a formatter writing to a buffer and
// returns what reached the destination.
func captureOpenOutput(t *testing.T, url string, printOnly bool, cliCtx *registry.CLIContext) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	formatter := output.New("json", true, false)
	formatter.SetWriter(&buf)
	if cliCtx == nil {
		cliCtx = &registry.CLIContext{}
	}
	cliCtx.Output = &cliOutput{formatter}

	prevFmt, prevField := outputFmt, fieldName
	outputFmt, fieldName = "json", ""
	defer func() { outputFmt, fieldName = prevFmt, prevField }()

	err := openOrPrint(cliCtx, url, printOnly)
	return buf.String(), err
}

// TestOpenOrPrintEmitsTheURLAsData is what makes the command scriptable: the
// print path goes through printRows, so -o json, --field, --select and
// --out-file all apply. Writing it to stderr as prose would leave a caller
// parsing a sentence.
func TestOpenOrPrintEmitsTheURLAsData(t *testing.T) {
	out, err := captureOpenOutput(t, "https://tenant.jamfcloud.com/", true, nil)
	if err != nil {
		t.Fatalf("openOrPrint: %v", err)
	}
	if !strings.Contains(out, `"url"`) || !strings.Contains(out, "https://tenant.jamfcloud.com") {
		t.Errorf("printed %q, want a url field carrying the trimmed URL", out)
	}
	if strings.Contains(out, "jamfcloud.com/\"") {
		t.Errorf("printed %q with a trailing slash — Validate should have trimmed it", out)
	}
}

// TestOpenOrPrintPrintsUnderDryRun covers the rule that -n must not be a
// documented no-op: launching a browser is the whole effect of this command,
// so under --dry-run it prints instead.
func TestOpenOrPrintPrintsUnderDryRun(t *testing.T) {
	out, err := captureOpenOutput(t, "https://tenant.jamfcloud.com", false, &registry.CLIContext{DryRun: true})
	if err != nil {
		t.Fatalf("openOrPrint: %v", err)
	}
	if !strings.Contains(out, "https://tenant.jamfcloud.com") {
		t.Errorf("under --dry-run printed %q, want the URL", out)
	}
}

// TestOpenOrPrintRefusesANonWebURL asserts the validation is in the shared
// path rather than only inside browser.Open, so a profile holding a bad URL is
// refused whether the command would launch or print. Printing it instead would
// hand the caller a value to paste.
func TestOpenOrPrintRefusesANonWebURL(t *testing.T) {
	for _, raw := range []string{"", "tenant.jamfcloud.com", "file:///etc/passwd"} {
		if _, err := captureOpenOutput(t, raw, true, nil); err == nil {
			t.Errorf("openOrPrint(%q) returned no error", raw)
		}
	}
}

// openMockClient answers one path.
type openMockClient struct {
	path, body string
	status     int
	calls      int
}

func (m *openMockClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	m.calls++
	if path != m.path {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}
	status := m.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(m.body)), Header: make(http.Header)}, nil
}

// TestProWebURLReadsTheAPIOnlyOnAGatewayProfile is the behavioural heart of
// `pro open`. An instance profile already holds the URL it authenticated
// against, so spending a request there would make the command fail for reasons
// unrelated to opening a browser; a gateway profile names a tenant and never a
// Jamf Pro host, so the request is the only source.
func TestProWebURLReadsTheAPIOnlyOnAGatewayProfile(t *testing.T) {
	prev := serverURL
	defer func() { serverURL = prev }()

	t.Run("instance profile makes no request", func(t *testing.T) {
		serverURL = "https://tenant.jamfcloud.com"
		client := &openMockClient{path: proServerURLPath, body: `{"url":"https://elsewhere.jamfcloud.com"}`}
		got, err := proWebURL(context.Background(), &registry.CLIContext{
			Client:       client,
			AuthProvider: &auth.OAuth2Provider{},
		})
		if err != nil {
			t.Fatalf("proWebURL: %v", err)
		}
		if got != "https://tenant.jamfcloud.com" {
			t.Errorf("got %q, want the configured URL", got)
		}
		if client.calls != 0 {
			t.Errorf("made %d requests on an instance profile, want 0", client.calls)
		}
	})

	t.Run("gateway profile reads the API", func(t *testing.T) {
		// Set to something wrong: on a gateway profile serverURL is the
		// gateway host, which is exactly what must not be opened.
		serverURL = "https://eu.api.jamfcloud.com"
		client := &openMockClient{path: proServerURLPath, body: `{"url":"https://tenant.jamfcloud.com"}`}
		got, err := proWebURL(context.Background(), &registry.CLIContext{
			Client:       client,
			AuthProvider: &auth.PlatformOAuth2Provider{},
		})
		if err != nil {
			t.Fatalf("proWebURL: %v", err)
		}
		if got != "https://tenant.jamfcloud.com" {
			t.Errorf("got %q, want the URL the API reported", got)
		}
		if client.calls != 1 {
			t.Errorf("made %d requests, want 1", client.calls)
		}
	})

	t.Run("gateway profile with no url field errors", func(t *testing.T) {
		serverURL = "https://eu.api.jamfcloud.com"
		client := &openMockClient{path: proServerURLPath, body: `{}`}
		if got, err := proWebURL(context.Background(), &registry.CLIContext{
			Client:       client,
			AuthProvider: &auth.PlatformOAuth2Provider{},
		}); err == nil {
			t.Errorf("got %q, want an error naming the endpoint", got)
		}
	})
}

// TestConfiguredWebURLPrefersTheFlagThenTheEnvironment pins the ladder the
// three request-free products share. It stops at the profile here — reading one
// would need a config file — and the profile arm is the documented tail.
func TestConfiguredWebURLPrefersTheFlagThenTheEnvironment(t *testing.T) {
	target := openTarget{product: "protect", label: "Jamf Protect", envVars: []string{"JAMFPROTECT_URL", "JAMF_URL"}, setup: "protect setup"}

	prev := serverURL
	defer func() { serverURL = prev }()

	serverURL = "https://from-flag.example.com"
	t.Setenv("JAMFPROTECT_URL", "https://from-env.example.com")
	got, err := configuredWebURL(target)
	if err != nil {
		t.Fatalf("configuredWebURL: %v", err)
	}
	if got != "https://from-flag.example.com" {
		t.Errorf("got %q, want --url to win", got)
	}

	serverURL = ""
	if got, err = configuredWebURL(target); err != nil || got != "https://from-env.example.com" {
		t.Errorf("got (%q, %v), want the product's own environment variable", got, err)
	}

	// The second name is a fallback, not an alternative of equal rank.
	t.Setenv("JAMFPROTECT_URL", "")
	t.Setenv("JAMF_URL", "https://generic.example.com")
	if got, err = configuredWebURL(target); err != nil || got != "https://generic.example.com" {
		t.Errorf("got (%q, %v), want the generic environment variable", got, err)
	}
}

// TestEnvPhraseReadsAsEnglish — the message naming the variables is the whole
// answer when nothing is configured. A single name is a sentence about that
// variable, not a one-item list: "one of JAMFSCHOOL_URL" reads as truncated,
// which is what School's message said when this was one code path.
func TestEnvPhraseReadsAsEnglish(t *testing.T) {
	cases := map[string][]string{
		"the A environment variable": {"A"},
		"one of A or B":              {"A", "B"},
		"one of A, B or C":           {"A", "B", "C"},
	}
	for want, names := range cases {
		if got := envPhrase(names); got != want {
			t.Errorf("envPhrase(%v) = %q, want %q", names, got, want)
		}
	}
}
