// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

type textNamedPolicy struct{ id, name string }

// textNamedPolicyServer serves a policy list whose names read as numbers or
// booleans, as XML or as JSON.
type textNamedPolicyServer struct {
	records []textNamedPolicy
	json    bool
	calls   []string
}

func (s *textNamedPolicyServer) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	s.calls = append(s.calls, method+" "+path)
	if body != nil {
		_, _ = io.ReadAll(body)
	}
	reply := func(code int, ct, b string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(b))}, nil
	}
	const base = "/JSSResource/policies"
	switch {
	case method == "GET" && path == base && s.json:
		parts := make([]string, len(s.records))
		for i, r := range s.records {
			parts[i] = fmt.Sprintf(`{"id":%s,"name":%q}`, r.id, r.name)
		}
		return reply(200, "application/json", `{"policies":[`+strings.Join(parts, ",")+`]}`)
	case method == "GET" && path == base:
		var b strings.Builder
		fmt.Fprintf(&b, "<policies><size>%d</size>", len(s.records))
		for _, r := range s.records {
			fmt.Fprintf(&b, "<policy><id>%s</id><name>%s</name></policy>", r.id, r.name)
		}
		b.WriteString("</policies>")
		return reply(200, "application/xml", b.String())
	case method == "GET" && strings.HasPrefix(path, base+"/id/"):
		id := strings.TrimPrefix(path, base+"/id/")
		for _, r := range s.records {
			if r.id == id {
				return reply(200, "application/xml", fmt.Sprintf("<policy><general><id>%s</id><name>%s</name></general></policy>", r.id, r.name))
			}
		}
	case method == "PUT" || method == "DELETE" || method == "POST":
		return reply(201, "application/xml", "<policy><id>1</id></policy>")
	}
	return reply(404, "text/plain", "not found")
}

var _ registry.HTTPClient = (*textNamedPolicyServer)(nil)

func (s *textNamedPolicyServer) writes() []string {
	var w []string
	for _, c := range s.calls {
		if strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "DELETE ") || strings.HasPrefix(c, "POST ") {
			w = append(w, c)
		}
	}
	return w
}

type textNameCommand struct {
	name   string
	newCmd func(*registry.CLIContext) *cobra.Command
	args   func(t *testing.T, name string) []string
	write  string
}

func textNameCommands() []textNameCommand {
	policyXML := func(t *testing.T, name string) string {
		return writeXML(t, "<policy><general><name>"+name+"</name><enabled>false</enabled></general></policy>")
	}
	return []textNameCommand{
		{"update", newClassicPoliciesUpdateCmd, func(t *testing.T, name string) []string {
			return []string{"update", "--no-input", "--name", name, "--from-file", policyXML(t, name)}
		}, "PUT"},
		{"delete", newClassicPoliciesDeleteCmd, func(_ *testing.T, name string) []string {
			return []string{"delete", "--no-input", "--yes", "--name", name}
		}, "DELETE"},
		{"apply", newClassicPoliciesApplyCmd, func(t *testing.T, name string) []string {
			return []string{"apply", "--no-input", "--yes", "--from-file", policyXML(t, name)}
		}, "PUT"},
	}
}

// TestClassicNameThatReadsAsANumberOrBooleanResolves: a record named 2024,
// true or 1.50 is found by update --name, delete --name and apply, from an
// XML list and from a JSON one, and the write goes to that record's id.
func TestClassicNameThatReadsAsANumberOrBooleanResolves(t *testing.T) {
	records := []textNamedPolicy{{"11", "2024"}, {"12", "true"}, {"13", "1.50"}, {"14", "Other"}}
	for _, format := range []string{"xml", "json"} {
		for _, c := range textNameCommands() {
			for _, r := range records[:3] {
				t.Run(format+"/"+c.name+"/"+r.name, func(t *testing.T) {
					server := &textNamedPolicyServer{records: records, json: format == "json"}
					stdinFromDevNull(t)
					if err := runClassicCmd(c.newCmd, server, c.args(t, r.name)...); err != nil {
						t.Fatalf("%s %q: %v (calls %v)", c.name, r.name, err, server.calls)
					}
					want := c.write + " /JSSResource/policies/id/" + r.id
					if w := server.writes(); len(w) != 1 || w[0] != want {
						t.Errorf("writes = %v, want [%s]", w, want)
					}
				})
			}
		}
	}
}

// TestClassicNumericNameSharedByTwoRecordsIsACollision: two records both named
// 2024 are refused by update --name, delete --name and apply, naming both ids,
// and nothing is written.
func TestClassicNumericNameSharedByTwoRecordsIsACollision(t *testing.T) {
	for _, c := range textNameCommands() {
		t.Run(c.name, func(t *testing.T) {
			server := &textNamedPolicyServer{records: []textNamedPolicy{{"11", "2024"}, {"12", "2024"}}}
			stdinFromDevNull(t)
			err := runClassicCmd(c.newCmd, server, c.args(t, "2024")...)
			if err == nil || !strings.Contains(err.Error(), "IDs: 11, 12") {
				t.Fatalf("%s on two records named 2024: err=%v, want a collision naming 11 and 12 (calls %v)", c.name, err, server.calls)
			}
			if code := exitcode.CodeFrom(err); code != exitcode.Usage {
				t.Errorf("exit code = %d, want %d (Usage)", code, exitcode.Usage)
			}
			if w := server.writes(); len(w) != 0 {
				t.Errorf("no write may be sent on a name collision, got %v", w)
			}
		})
	}
}
