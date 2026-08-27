// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// classicBodyRecordingClient captures the body of every write it is handed.
type classicBodyRecordingClient struct {
	calls []string
	body  []byte
}

func (c *classicBodyRecordingClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	c.calls = append(c.calls, method+" "+path)
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		c.body = b
	}
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`<computer_invitation><id>1</id></computer_invitation>`))}, nil
}

var _ registry.HTTPClient = (*classicBodyRecordingClient)(nil)

func writeXML(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.xml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// pipeStdin parks data on stdin without closing the writer, so a code path that
// wrongly reads stdin blocks (and the test times out) rather than passing quietly.
func pipeStdin(t *testing.T, data string) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
		_ = w.Close()
	})
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
}

// stdinFromDevNull points stdin at a character device, the shape a terminal has,
// so "no body supplied" is what the code under test sees regardless of how the
// test binary itself was invoked.
func stdinFromDevNull(t *testing.T) {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		_ = f.Close()
	})
}

// ── readClassicBody ───────────────────────────────────────────────────────────

func TestReadClassicBody_ReadsFile(t *testing.T) {
	p := writeXML(t, "<computer_invitation><invitation>1234</invitation></computer_invitation>")
	got, err := readClassicBody(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<computer_invitation><invitation>1234</invitation></computer_invitation>" {
		t.Errorf("body = %q", got)
	}
}

func TestReadClassicBody_FileWinsOverStdin(t *testing.T) {
	pipeStdin(t, "<computer_invitation><invitation>from-stdin</invitation></computer_invitation>")
	p := writeXML(t, "<computer_invitation><invitation>from-file</invitation></computer_invitation>")

	got, err := readClassicBody(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "from-stdin") {
		t.Errorf("stdin consumed despite --from-file: %q", got)
	}
}

func TestReadClassicBody_MissingFileErrors(t *testing.T) {
	_, err := readClassicBody(filepath.Join(t.TempDir(), "nope.xml"))
	if err == nil {
		t.Fatal("expected an error for an unreadable --from-file")
	}
	if !strings.Contains(err.Error(), "--from-file") {
		t.Errorf("error should name the flag, got %v", err)
	}
}

func TestReadClassicBody_AbsentBodyIsNotAnError(t *testing.T) {
	// A terminal stdin and no flag: the caller decides whether that is fatal,
	// because classic create/update can build a body from file-field flags alone.
	stdinFromDevNull(t)
	got, err := readClassicBody("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("body = %q, want empty", got)
	}
}

// ── generated commands ────────────────────────────────────────────────────────

// TestClassicCreateFromFile drives the real RunE of a classic create that has no
// name-resolution collection (so no apply command exists and --from-file on
// create is the only file-based route — the AutoPkg JamfCLIRunner case).
func TestClassicCreateFromFile(t *testing.T) {
	mock := &classicBodyRecordingClient{}
	xml := "<computer_invitation><invitation>987654321</invitation></computer_invitation>"
	p := writeXML(t, xml)
	pipeStdin(t, "<computer_invitation><invitation>from-stdin</invitation></computer_invitation>")

	cmd := newClassicComputerInvitationsCreateCmd(&registry.CLIContext{Client: mock, Output: newNDJSONOutput()})
	cmd.SetArgs([]string{"--from-file", p})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create --from-file failed: %v", err)
	}

	if len(mock.calls) != 1 || mock.calls[0] != "POST /JSSResource/computerinvitations/id/0" {
		t.Fatalf("calls = %v", mock.calls)
	}
	if string(mock.body) != xml {
		t.Errorf("POST body = %q, want the file verbatim %q", mock.body, xml)
	}
}

func TestClassicCreateWithoutBodyNamesFromFile(t *testing.T) {
	stdinFromDevNull(t)
	mock := &classicBodyRecordingClient{}
	cmd := newClassicComputerInvitationsCreateCmd(&registry.CLIContext{Client: mock, Output: newNDJSONOutput()})
	cmd.SetArgs(nil)
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error with no body and no --from-file")
	}
	if !strings.Contains(err.Error(), "--from-file") {
		t.Errorf("error should offer --from-file, got %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("no request should have been sent, got %v", mock.calls)
	}
}

func TestClassicUpdateFromFile(t *testing.T) {
	mock := &classicBodyRecordingClient{}
	xml := "<smtp_server><enabled>true</enabled></smtp_server>"
	p := writeXML(t, xml)
	pipeStdin(t, "<smtp_server><enabled>from-stdin</enabled></smtp_server>")

	cmd := newClassicSmtpServerUpdateCmd(&registry.CLIContext{Client: mock, Output: newNDJSONOutput()})
	cmd.SetArgs([]string{"1", "--from-file", p})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --from-file failed: %v", err)
	}

	if len(mock.calls) != 1 || mock.calls[0] != "PUT /JSSResource/smtpserver/id/1" {
		t.Fatalf("calls = %v", mock.calls)
	}
	if string(mock.body) != xml {
		t.Errorf("PUT body = %q, want the file verbatim %q", mock.body, xml)
	}
}

// TestClassicCreateFromFileWithFileFields covers the other create shape: a
// resource whose create also accepts file-field flags, where --from-file supplies
// the surrounding XML the file field is injected into.
func TestClassicCreateFromFileWithFileFields(t *testing.T) {
	mock := &classicBodyRecordingClient{}
	p := writeXML(t, "<os_x_configuration_profile><general><name>From File</name></general></os_x_configuration_profile>")
	pipeStdin(t, "<os_x_configuration_profile><general><name>From Stdin</name></general></os_x_configuration_profile>")

	cmd := newClassicMacosConfigProfilesCreateCmd(&registry.CLIContext{Client: mock, Output: newNDJSONOutput()})
	cmd.SetArgs([]string{"--from-file", p})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create --from-file failed: %v", err)
	}
	if !strings.Contains(string(mock.body), "From File") {
		t.Errorf("POST body did not come from --from-file: %q", mock.body)
	}
	if strings.Contains(string(mock.body), "From Stdin") {
		t.Errorf("stdin consumed despite --from-file: %q", mock.body)
	}
}
