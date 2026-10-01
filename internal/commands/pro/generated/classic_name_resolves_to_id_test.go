// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

func (s *duplicateNameClassicServer) nameLookups() []string {
	var n []string
	for _, c := range s.calls {
		if strings.Contains(c, "/name/") {
			n = append(n, c)
		}
	}
	return n
}

func stdinIsTerminal(t *testing.T, is bool) {
	t.Helper()
	orig := classicStdinIsTerminal
	classicStdinIsTerminal = func() bool { return is }
	t.Cleanup(func() { classicStdinIsTerminal = orig })
}

func runClassicCmd(newCmd func(*registry.CLIContext) *cobra.Command, client registry.HTTPClient, args ...string) error {
	root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("no-input", false, "")
	root.AddCommand(newCmd(&registry.CLIContext{Client: client, Output: newNDJSONOutput()}))
	root.SetArgs(args)
	return root.Execute()
}

// TestClassicApplyFetchMergePutFetchesTheChosenID: with two apps sharing a
// name, the record the operator picks is the one fetched, merged and PUT,
// whichever record the server's /name/ endpoint would have answered with.
func TestClassicApplyFetchMergePutFetchesTheChosenID(t *testing.T) {
	server := &duplicateNameClassicServer{apiPath: "macapplications", root: "mac_application", listRoot: "mac_applications", ids: []string{"11", "22"}, serverPick: "22"}
	pipeStdin(t, "1\n")
	stdinIsTerminal(t, true)

	body := writeXML(t, "<mac_application><general><name>Baseline</name></general></mac_application>")
	if err := runClassicCmd(newClassicMacAppsApplyCmd, server, "apply", "--yes", "--from-file", body); err != nil {
		t.Fatalf("apply after picking id 11: %v (calls %v)", err, server.calls)
	}

	if n := server.nameLookups(); len(n) != 0 {
		t.Errorf("apply must fetch by the chosen id, not by name: %v", n)
	}
	if w := server.writes(); len(w) != 1 || w[0] != "PUT /JSSResource/macapplications/id/11" {
		t.Errorf("writes = %v, want one PUT to id 11", w)
	}
	if len(server.puts) != 1 || !strings.Contains(server.puts[0], "<id>11</id>") || strings.Contains(server.puts[0], "<id>22</id>") {
		t.Errorf("PUT body must be record 11's, got %q", server.puts)
	}
}

// TestClassicNameCollisionRefusedWhenStdinIsNotATerminal: a pick is only
// prompted for when a human can answer it, so a choice piped to stdin is
// refused the same way --no-input is.
func TestClassicNameCollisionRefusedWhenStdinIsNotATerminal(t *testing.T) {
	server := &duplicateNameClassicServer{apiPath: "policies", root: "policy", listRoot: "policies", ids: []string{"11", "22"}, serverPick: "22"}
	pipeStdin(t, "1\n")

	err := runClassicCmd(newClassicPoliciesDeleteCmd, server, "delete", "--yes", "--name", "Baseline")
	if err == nil || !strings.Contains(err.Error(), "IDs: 11, 22") {
		t.Fatalf("delete --name with a piped pick: err=%v, want a refusal naming ids 11 and 22", err)
	}
	if w := server.writes(); len(w) != 0 {
		t.Errorf("no write may be sent on a name collision, got %v", w)
	}
}

// TestClassicUpdateByNameSoleMatchPutsToThatID: a name held by one record resolves to
// that record's id and the PUT goes to /id/; a name held by none is refused
// and nothing is written. Covers all three generated update forms.
func TestClassicUpdateByNameSoleMatchPutsToThatID(t *testing.T) {
	forms := []struct {
		name     string
		newCmd   func(*registry.CLIContext) *cobra.Command
		apiPath  string
		root     string
		listRoot string
		body     string
		putHas   string
	}{
		{
			name:     "config profile branch",
			newCmd:   newClassicMacosConfigProfilesUpdateCmd,
			apiPath:  "osxconfigurationprofiles",
			root:     "os_x_configuration_profile",
			listRoot: "os_x_configuration_profiles",
			body:     "<os_x_configuration_profile><general><payloads>&lt;plist version=&quot;1.0&quot;&gt;&lt;dict&gt;&lt;key&gt;PayloadUUID&lt;/key&gt;&lt;string&gt;NEW-UUID&lt;/string&gt;&lt;/dict&gt;&lt;/plist&gt;</payloads></general></os_x_configuration_profile>",
			putHas:   "UUID-11",
		},
		{
			name:     "fetch-merge-put branch",
			newCmd:   newClassicMacAppsUpdateCmd,
			apiPath:  "macapplications",
			root:     "mac_application",
			listRoot: "mac_applications",
			body:     "<mac_application><general><name>Baseline</name></general></mac_application>",
			putHas:   "<id>11</id>",
		},
		{
			name:     "generic branch",
			newCmd:   newClassicPoliciesUpdateCmd,
			apiPath:  "policies",
			root:     "policy",
			listRoot: "policies",
			body:     "<policy><general><enabled>false</enabled></general></policy>",
		},
	}
	for _, f := range forms {
		t.Run(f.name+"/sole match", func(t *testing.T) {
			server := &duplicateNameClassicServer{apiPath: f.apiPath, root: f.root, listRoot: f.listRoot, ids: []string{"11"}, serverPick: "99"}
			stdinFromDevNull(t)
			if err := runClassicCmd(f.newCmd, server, "update", "--no-input", "--name", "Baseline", "--from-file", writeXML(t, f.body)); err != nil {
				t.Fatalf("update --name with one match: %v (calls %v)", err, server.calls)
			}
			want := "PUT /JSSResource/" + f.apiPath + "/id/11"
			if w := server.writes(); len(w) != 1 || w[0] != want {
				t.Errorf("writes = %v, want [%s]", w, want)
			}
			if n := server.nameLookups(); len(n) != 0 {
				t.Errorf("update --name must not use the /name/ endpoint: %v", n)
			}
			if f.putHas != "" && (len(server.puts) != 1 || !strings.Contains(server.puts[0], f.putHas)) {
				t.Errorf("PUT body should carry %q from record 11, got %q", f.putHas, server.puts)
			}
		})
		t.Run(f.name+"/no match", func(t *testing.T) {
			server := &duplicateNameClassicServer{apiPath: f.apiPath, root: f.root, listRoot: f.listRoot, serverPick: "99"}
			stdinFromDevNull(t)
			err := runClassicCmd(f.newCmd, server, "update", "--no-input", "--name", "Baseline", "--from-file", writeXML(t, f.body))
			if err == nil || !strings.Contains(err.Error(), "found with name") {
				t.Fatalf("update --name with no match: err=%v, want a not-found error (calls %v)", err, server.calls)
			}
			if w := server.writes(); len(w) != 0 {
				t.Errorf("no write may be sent when nothing has the name, got %v", w)
			}
		})
	}
}

// TestFetchClassicGroupMemberIDsRefusesAnOversizedList: a group list past the
// read limit is refused rather than matched against its truncated head, where
// the second same-named group would be invisible.
func TestFetchClassicGroupMemberIDsRefusesAnOversizedList(t *testing.T) {
	const groupsPath = "/JSSResource/computergroups"
	var b strings.Builder
	b.WriteString(`<computer_groups><computer_group><id>5</id><name>Dupe</name></computer_group>`)
	for i := 0; b.Len() <= classicGroupBodyLimit; i++ {
		fmt.Fprintf(&b, `<computer_group><id>%d</id><name>pad</name></computer_group>`, 1000+i)
	}
	b.WriteString(`<computer_group><id>6</id><name>Dupe</name></computer_group></computer_groups>`)
	client := &mockHTTPClient{responses: map[string]mockResponse{
		groupsPath:           {body: []byte(b.String()), status: 200},
		groupsPath + "/id/5": {body: makeGroupXML("computers", "computer", []string{"50"}), status: 200},
	}}
	ids, err := fetchClassicGroupMemberIDs(context.Background(), client, groupsPath, "computers", "computer", "Dupe")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got ids=%v err=%v, want a refusal of the oversized list", ids, err)
	}
}
