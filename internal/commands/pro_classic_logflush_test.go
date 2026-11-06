// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

func TestLogflushInterval(t *testing.T) {
	tests := []struct {
		n       int
		unit    string
		want    string
		wantErr bool
	}{
		{0, "DAYS", "Zero Days", false},
		{3, "months", "Three Months", false},
		{1, "week", "One Weeks", false}, // singular accepted
		{6, " Years ", "Six Years", false},
		{5, "DAYS", "", true}, // the API answers 500 for 5
		{4, "DAYS", "", true},
		{-1, "DAYS", "", true},
		{3, "HOURS", "", true},
		{3, "", "", true},
	}
	for _, tt := range tests {
		got, err := logflushInterval(tt.n, tt.unit)
		if (err != nil) != tt.wantErr {
			t.Errorf("logflushInterval(%d, %q) err = %v, wantErr %v", tt.n, tt.unit, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("logflushInterval(%d, %q) = %q, want %q", tt.n, tt.unit, got, tt.want)
		}
	}
}

// A non-numeric policy id in the path answers 201 and flushes every policy, so
// the validator is the only thing standing between a typo and a global flush.
func TestLogflushIDRefusesAnythingButAPositiveInteger(t *testing.T) {
	for _, bad := range []string{"", "abc", "0", "-3", "1.5", "7/interval/Zero+Days", "7 ", "1e3", "../x"} {
		if bad == "7 " {
			continue // trimmed, so legal; covered below
		}
		if got, err := logflushID("policy", bad); err == nil {
			t.Errorf("logflushID(%q) = %q, want an error", bad, got)
		}
	}
	if got, err := logflushID("policy", " 7 "); err != nil || got != "7" {
		t.Errorf("logflushID(\" 7 \") = %q, %v; want 7", got, err)
	}
}

func TestLogflushPolicyPath(t *testing.T) {
	got := logflushPolicyPath("42", "Three Months")
	want := "/JSSResource/logflush/policy/id/42/interval/Three+Months"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestLogflushComputersBody(t *testing.T) {
	got := logflushComputersBody([]string{"308", "309"}, "Zero Days")
	for _, want := range []string{
		"<log>policy</log>",
		"<interval>Zero Days</interval>",
		"<computer><id>308</id></computer><computer><id>309</id></computer>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q:\n%s", want, got)
		}
	}
}

// logflushMock records each request and answers a GET with a named record and
// a DELETE with 201, as the wire does.
type logflushMock struct {
	calls     []string
	bodies    []string
	missing   map[string]bool // GET path -> 404
	deleteSts int
}

func (m *logflushMock) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	m.calls = append(m.calls, method+" "+path)
	if body != nil {
		b, _ := io.ReadAll(body)
		m.bodies = append(m.bodies, string(b))
	}
	status, resp := http.StatusOK, `<policy><general><id>42</id><name>Install &amp; Go</name></general></policy>`
	if method == http.MethodDelete {
		status, resp = http.StatusCreated, `<logflush/>`
		if m.deleteSts != 0 {
			status = m.deleteSts
		}
	} else if m.missing[path] {
		status, resp = http.StatusNotFound, ""
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(resp)), Header: http.Header{}}, nil
}

func newLogflushCtx(m *logflushMock, dryRun bool) (*registry.CLIContext, *bytes.Buffer) {
	var out bytes.Buffer
	f := output.New("json", true, false)
	f.SetWriter(&out)
	return &registry.CLIContext{Client: m, Output: &cliOutput{f}, DryRun: dryRun}, &out
}

func runLogflushCmd(t *testing.T, cliCtx *registry.CLIContext, args ...string) error {
	t.Helper()
	cmd := newClassicLogflushCmd(cliCtx)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.ExecuteContext(context.Background())
}

func TestLogflushPolicy_SendsOneDeleteAfterLookup(t *testing.T) {
	m := &logflushMock{}
	cliCtx, out := newLogflushCtx(m, false)
	if err := runLogflushCmd(t, cliCtx, "policy", "42", "--older-than", "3", "--unit", "MONTHS", "--yes"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /JSSResource/policies/id/42",
		"DELETE /JSSResource/logflush/policy/id/42/interval/Three+Months",
	}
	if strings.Join(m.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", m.calls, want)
	}
	if !strings.Contains(out.String(), "Install & Go") {
		t.Errorf("row should carry the unescaped policy name:\n%s", out.String())
	}
}

func TestLogflushPolicy_RefusesBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"policy", "abc", "--yes"},
		{"policy", "42", "--older-than", "5", "--yes"},
		{"policy", "42", "--unit", "HOURS", "--yes"},
		{"computer", "308", "x", "--yes"},
	} {
		m := &logflushMock{}
		cliCtx, _ := newLogflushCtx(m, false)
		if err := runLogflushCmd(t, cliCtx, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
		if len(m.calls) != 0 {
			t.Errorf("%v: sent requests before validating: %v", args, m.calls)
		}
	}
}

func TestLogflushPolicy_UnknownPolicyNeverDeletes(t *testing.T) {
	m := &logflushMock{missing: map[string]bool{"/JSSResource/policies/id/42": true}}
	cliCtx, _ := newLogflushCtx(m, false)
	if err := runLogflushCmd(t, cliCtx, "policy", "42", "--yes"); err == nil {
		t.Fatal("want an error for a policy that does not exist")
	}
	for _, c := range m.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Errorf("sent %q for a policy that does not exist", c)
		}
	}
}

// Without --yes and without a terminal there is no way to confirm, so nothing
// may be deleted.
func TestLogflushPolicy_NeedsYesWithoutATerminal(t *testing.T) {
	m := &logflushMock{}
	cliCtx, _ := newLogflushCtx(m, false)
	err := runLogflushCmd(t, cliCtx, "policy", "42")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want one naming --yes", err)
	}
	for _, c := range m.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Errorf("sent %q without confirmation", c)
		}
	}
}

// -n must preview without needing --yes, and must still send the DELETE through
// the client so the dry-run wrapper reports it.
func TestLogflushPolicy_DryRunNeedsNoYes(t *testing.T) {
	m := &logflushMock{}
	cliCtx, out := newLogflushCtx(m, true)
	if err := runLogflushCmd(t, cliCtx, "policy", "42"); err != nil {
		t.Fatal(err)
	}
	if got := m.calls[len(m.calls)-1]; !strings.HasPrefix(got, "DELETE ") {
		t.Errorf("last call = %q, want the DELETE the dry-run wrapper reports", got)
	}
	if out.Len() != 0 {
		t.Errorf("a dry run reported a result row: %s", out.String())
	}
}

func TestLogflushComputer_BodyCarriesEveryIDAndTheInterval(t *testing.T) {
	m := &logflushMock{}
	cliCtx, _ := newLogflushCtx(m, false)
	if err := runLogflushCmd(t, cliCtx, "computer", "308", "309", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(m.bodies) != 1 {
		t.Fatalf("bodies = %d, want 1", len(m.bodies))
	}
	// The body form defaults an omitted interval to a full flush, so it must be explicit.
	for _, want := range []string{"<interval>Zero Days</interval>", "<id>308</id>", "<id>309</id>"} {
		if !strings.Contains(m.bodies[0], want) {
			t.Errorf("body missing %q:\n%s", want, m.bodies[0])
		}
	}
	if m.calls[len(m.calls)-1] != "DELETE /JSSResource/logflush" {
		t.Errorf("calls = %v", m.calls)
	}
}

func TestLogflush_ServerRefusalIsReported(t *testing.T) {
	m := &logflushMock{deleteSts: http.StatusInternalServerError}
	cliCtx, _ := newLogflushCtx(m, false)
	err := runLogflushCmd(t, cliCtx, "policy", "42", "--yes")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want HTTP 500", err)
	}
}
