// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/progress"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// DigiCert's privilege-check documents 204 (all permissions present) and 403
// (body lists the missing ones) as its two results. Both must reach the user:
// the 204 has no body to print, and the 403 must not be swallowed into a
// permission-denied error about the caller's own API role.

// jsonOutput captures formatter output for assertions.
type jsonOutput struct {
	f   *output.Formatter
	buf *bytes.Buffer
}

func newJSONOutput() *jsonOutput {
	buf := &bytes.Buffer{}
	f := output.New("json", true, false)
	f.SetWriter(buf)
	return &jsonOutput{f: f, buf: buf}
}

func (o *jsonOutput) PrintResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return o.f.PrintRaw(body)
}

func (o *jsonOutput) PrintRaw(data []byte) error   { return o.f.PrintRaw(data) }
func (o *jsonOutput) PrintBytes(data []byte) error { return o.f.PrintBytes(data) }
func (o *jsonOutput) Format() string               { return o.f.Format() }
func (o *jsonOutput) PaginationProgress() *progress.Reporter {
	return progress.New(io.Discard, progress.Silent)
}

// statusClient serves one canned status/body and records whether the request
// context marked that status as an expected result.
type statusClient struct {
	status        int
	body          string
	statusAllowed bool
}

func (c *statusClient) Do(ctx context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	c.statusAllowed = registry.StatusAllowed(ctx, c.status)
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}

func runPrivilegeCheck(t *testing.T, client *statusClient) (string, error) {
	t.Helper()
	out := newJSONOutput()
	cmd := NewDigiCertSettingsCmd(&registry.CLIContext{Client: client, Output: out})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"privilege-check", "12"})
	err := cmd.Execute()
	return out.buf.String(), err
}

// A passing check (204, no body) must still emit something machine-readable.
func TestPrivilegeCheck_NoContentEmitsResult(t *testing.T) {
	client := &statusClient{status: http.StatusNoContent}
	stdout, err := runPrivilegeCheck(t, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, `"result"`) || !strings.Contains(stdout, "ok") {
		t.Errorf("204 printed %q, want a machine-readable success object", stdout)
	}
}

// A failing check must print the server's missing-permission list and exit
// non-zero — not surface a permission-denied error about the caller's own token.
func TestPrivilegeCheck_ForbiddenPrintsMissingPermissions(t *testing.T) {
	const body = `{"errors":[{"code":"MISSING_PERMISSION","description":"certificate:issue"}]}`
	client := &statusClient{status: http.StatusForbidden, body: body}
	stdout, err := runPrivilegeCheck(t, client)

	if !client.statusAllowed {
		t.Error("the request did not mark 403 as an expected result, so the real client would map it to an error")
	}
	if err == nil {
		t.Fatal("expected a non-zero exit when permissions are missing")
	}
	if got := exitcode.CodeFrom(err); got == exitcode.PermissionDenied {
		t.Error("403 still reported as permission_denied, which blames the caller's own API role")
	}
	if !strings.Contains(stdout, "certificate:issue") {
		t.Errorf("missing-permission list not printed; stdout = %q", stdout)
	}
	if !strings.Contains(err.Error(), "missing one or more required permissions") {
		t.Errorf("error = %q, want it to name the DigiCert account's missing permissions", err.Error())
	}
}

// A status the endpoint does not document as a result (404 for a bad ID) stays a
// plain error — the opt-out is per-status, not "ignore all failures".
func TestPrivilegeCheck_OtherStatusesNotAllowed(t *testing.T) {
	client := &statusClient{status: http.StatusNotFound, body: `{"httpStatus":404}`}
	if _, _ = runPrivilegeCheck(t, client); client.statusAllowed {
		t.Error("404 was marked as an expected result; only the documented statuses may be")
	}
}

// Jamf reuses 403 for "this API token is not authorized" — verified live against
// a tenant whose token lacked the privilege. That must keep reporting as a
// permission problem with the caller's own role, not as a DigiCert account result.
func TestPrivilegeCheck_TokenFailureStaysPermissionDenied(t *testing.T) {
	const tokenDenied = `{"httpStatus":403,"errors":[{"code":"BAD_PERMISSIONS",` +
		`"description":"The given token was not authorized to access the requested resource."}]}`
	client := &statusClient{status: http.StatusForbidden, body: tokenDenied}
	stdout, err := runPrivilegeCheck(t, client)

	if err == nil {
		t.Fatal("expected an error for a token-authorization failure")
	}
	if got := exitcode.CodeFrom(err); got != exitcode.PermissionDenied {
		t.Errorf("exit code = %d, want %d (permission denied)", got, exitcode.PermissionDenied)
	}
	if strings.Contains(err.Error(), "DigiCert account is missing") {
		t.Errorf("token failure mislabeled as a DigiCert account result: %v", err)
	}
	if strings.Contains(stdout, "BAD_PERMISSIONS") {
		t.Errorf("token failure rendered as a result payload on stdout: %q", stdout)
	}
}

// renderDocumentedStatus's classifier: only the BAD_PERMISSIONS sentinel means
// "the caller's token", and a non-JSON or unrelated body is an endpoint result.
func TestIsTokenAuthorizationError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"token failure", `{"errors":[{"code":"BAD_PERMISSIONS"}]}`, true},
		{"endpoint result", `{"errors":[{"code":"MISSING_PERMISSION","description":"certificate:issue"}]}`, false},
		{"mixed, token sentinel present", `{"errors":[{"code":"OTHER"},{"code":"BAD_PERMISSIONS"}]}`, true},
		{"no errors array", `{"missingPermissions":["certificate:issue"]}`, false},
		{"empty body", ``, false},
		{"not JSON", `<html>403</html>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTokenAuthorizationError([]byte(tt.body)); got != tt.want {
				t.Errorf("isTokenAuthorizationError(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
