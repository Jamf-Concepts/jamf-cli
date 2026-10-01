// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// duplicateNameClassicServer holds two records sharing one name. Its /name/
// endpoint answers with serverPick, as the real Classic API answers with one
// record of its own choosing.
type duplicateNameClassicServer struct {
	apiPath, root, listRoot string
	ids                     []string
	serverPick              string
	calls                   []string
	puts                    []string
}

func (s *duplicateNameClassicServer) record(id string) string {
	return fmt.Sprintf(`<%s><general><id>%s</id><name>Baseline</name><payloads>&lt;plist&gt;&lt;dict&gt;&lt;key&gt;PayloadUUID&lt;/key&gt;&lt;string&gt;UUID-%s&lt;/string&gt;&lt;/dict&gt;&lt;/plist&gt;</payloads></general></%s>`, s.root, id, id, s.root)
}

func (s *duplicateNameClassicServer) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	s.calls = append(s.calls, method+" "+path)
	if body != nil {
		b, _ := io.ReadAll(body)
		if method == "PUT" {
			s.puts = append(s.puts, string(b))
		}
	}
	reply := func(code int, b string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(b))}, nil
	}
	base := "/JSSResource/" + s.apiPath
	switch {
	case method == "GET" && path == base:
		var b strings.Builder
		fmt.Fprintf(&b, "<%s><size>%d</size>", s.listRoot, len(s.ids))
		for _, id := range s.ids {
			fmt.Fprintf(&b, "<%s><id>%s</id><name>Baseline</name></%s>", s.root, id, s.root)
		}
		fmt.Fprintf(&b, "</%s>", s.listRoot)
		return reply(200, b.String())
	case method == "GET" && strings.HasPrefix(path, base+"/name/"):
		return reply(200, s.record(s.serverPick))
	case method == "GET" && strings.HasPrefix(path, base+"/id/"):
		id := strings.SplitN(strings.TrimPrefix(path, base+"/id/"), "/", 2)[0]
		if !slices.Contains(s.ids, id) {
			return reply(404, "not found")
		}
		return reply(200, s.record(id))
	case method == "PUT":
		return reply(201, fmt.Sprintf("<%s><id>%s</id></%s>", s.root, s.serverPick, s.root))
	}
	return reply(404, "not found")
}

var _ registry.HTTPClient = (*duplicateNameClassicServer)(nil)

func (s *duplicateNameClassicServer) writes() []string {
	var w []string
	for _, c := range s.calls {
		if strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "DELETE ") {
			w = append(w, c)
		}
	}
	return w
}

// TestClassicUpdateByNameRefusesDuplicateName holds update --name to the same
// contract delete --name and apply already keep: a name shared by two records
// is refused under --no-input, naming both ids, and nothing is written.
func TestClassicUpdateByNameRefusesDuplicateName(t *testing.T) {
	cases := []struct {
		name   string
		newCmd func(*registry.CLIContext) *cobra.Command
		server *duplicateNameClassicServer
		body   string
	}{
		{
			name:   "config profile branch",
			newCmd: newClassicMacosConfigProfilesUpdateCmd,
			server: &duplicateNameClassicServer{apiPath: "osxconfigurationprofiles", root: "os_x_configuration_profile", listRoot: "os_x_configuration_profiles"},
			body:   "<os_x_configuration_profile><general><description>new</description></general></os_x_configuration_profile>",
		},
		{
			name:   "fetch-merge-put branch",
			newCmd: newClassicMacAppsUpdateCmd,
			server: &duplicateNameClassicServer{apiPath: "macapplications", root: "mac_application", listRoot: "mac_applications"},
			body:   "<mac_application><general><name>Baseline</name></general></mac_application>",
		},
		{
			name:   "generic branch",
			newCmd: newClassicPoliciesUpdateCmd,
			server: &duplicateNameClassicServer{apiPath: "policies", root: "policy", listRoot: "policies"},
			body:   "<policy><general><enabled>false</enabled></general></policy>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.server.ids = []string{"11", "22"}
			tc.server.serverPick = "22"
			stdinFromDevNull(t)

			root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().Bool("no-input", false, "")
			root.AddCommand(tc.newCmd(&registry.CLIContext{Client: tc.server, Output: newNDJSONOutput()}))
			root.SetArgs([]string{"update", "--no-input", "--name", "Baseline", "--from-file", writeXML(t, tc.body)})
			err := root.Execute()

			t.Logf("calls: %v", tc.server.calls)
			if err == nil {
				t.Fatalf("update --name on a name shared by ids 11 and 22 reported success; writes sent: %v", tc.server.writes())
			}
			for _, id := range tc.server.ids {
				if !strings.Contains(err.Error(), id) {
					t.Errorf("refusal should name id %s, got %v", id, err)
				}
			}
			if w := tc.server.writes(); len(w) != 0 {
				t.Errorf("no write may be sent on a name collision, got %v", w)
			}
		})
	}
}

// TestClassicDeleteByNameRefusesDuplicateName is the control: the same fake
// server, driven through delete --name, is already refused today.
func TestClassicDeleteByNameRefusesDuplicateName(t *testing.T) {
	server := &duplicateNameClassicServer{apiPath: "policies", root: "policy", listRoot: "policies", ids: []string{"11", "22"}, serverPick: "22"}
	stdinFromDevNull(t)
	root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("no-input", false, "")
	root.AddCommand(newClassicPoliciesDeleteCmd(&registry.CLIContext{Client: server, Output: newNDJSONOutput()}))
	root.SetArgs([]string{"delete", "--no-input", "--yes", "--name", "Baseline"})
	err := root.Execute()
	t.Logf("calls: %v err: %v", server.calls, err)
	if err == nil || !strings.Contains(err.Error(), "11") || !strings.Contains(err.Error(), "22") {
		t.Fatalf("delete --name should refuse naming both ids, got %v", err)
	}
	if w := server.writes(); len(w) != 0 {
		t.Errorf("no write may be sent on a name collision, got %v", w)
	}
}
