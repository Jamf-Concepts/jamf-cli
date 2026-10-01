// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// putBodyRecorder keeps the body of every PUT it forwards.
type putBodyRecorder struct {
	*duplicateNameClassicServer
	puts []string
}

func (r *putBodyRecorder) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if method == "PUT" && body != nil {
		b, _ := io.ReadAll(body)
		r.puts = append(r.puts, string(b))
		body = nil
	}
	return r.duplicateNameClassicServer.Do(ctx, method, path, body)
}

func (s *duplicateNameClassicServer) nameLookups() []string {
	var n []string
	for _, c := range s.calls {
		if strings.Contains(c, "/name/") {
			n = append(n, c)
		}
	}
	return n
}

// TestClassicApplyFetchMergePutFetchesTheChosenID: with two apps sharing a
// name, the record the operator picks is the one fetched, merged and PUT,
// whichever record the server's /name/ endpoint would have answered with.
func TestClassicApplyFetchMergePutFetchesTheChosenID(t *testing.T) {
	server := &duplicateNameClassicServer{apiPath: "macapplications", root: "mac_application", listRoot: "mac_applications", ids: []string{"11", "22"}, serverPick: "22"}
	client := &putBodyRecorder{duplicateNameClassicServer: server}
	pipeStdin(t, "1\n")

	root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("no-input", false, "")
	root.AddCommand(newClassicMacAppsApplyCmd(&registry.CLIContext{Client: client, Output: newNDJSONOutput()}))
	root.SetArgs([]string{"apply", "--yes", "--from-file", writeXML(t, "<mac_application><general><name>Baseline</name></general></mac_application>")})
	if err := root.Execute(); err != nil {
		t.Fatalf("apply after picking id 11: %v (calls %v)", err, server.calls)
	}

	if n := server.nameLookups(); len(n) != 0 {
		t.Errorf("apply must fetch by the chosen id, not by name: %v", n)
	}
	if w := server.writes(); len(w) != 1 || w[0] != "PUT /JSSResource/macapplications/id/11" {
		t.Errorf("writes = %v, want one PUT to id 11", w)
	}
	if len(client.puts) != 1 || !strings.Contains(client.puts[0], "<id>11</id>") || strings.Contains(client.puts[0], "<id>22</id>") {
		t.Errorf("PUT body must be record 11's, got %q", client.puts)
	}
}

// TestClassicUpdateByNameSoleMatchPutsToThatID: a name held by one record
// resolves to that record's id and the PUT goes to /id/, in all three
// generated update forms.
func TestClassicUpdateByNameSoleMatchPutsToThatID(t *testing.T) {
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
			tc.server.ids = []string{"11"}
			tc.server.serverPick = "99"
			stdinFromDevNull(t)

			root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().Bool("no-input", false, "")
			root.AddCommand(tc.newCmd(&registry.CLIContext{Client: tc.server, Output: newNDJSONOutput()}))
			root.SetArgs([]string{"update", "--no-input", "--name", "Baseline", "--from-file", writeXML(t, tc.body)})
			if err := root.Execute(); err != nil {
				t.Fatalf("update --name with one match: %v (calls %v)", err, tc.server.calls)
			}

			want := "PUT /JSSResource/" + tc.server.apiPath + "/id/11"
			if w := tc.server.writes(); len(w) != 1 || w[0] != want {
				t.Errorf("writes = %v, want [%s]", w, want)
			}
			if n := tc.server.nameLookups(); len(n) != 0 {
				t.Errorf("update --name must not use the /name/ endpoint: %v", n)
			}
		})
	}
}
