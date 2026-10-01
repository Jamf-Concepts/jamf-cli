// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/classic"
	"github.com/Jamf-Concepts/jamf-cli/generator/classicschema"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// mcpChildEnv is the variable `mcp serve` sets on every child it spawns.
const mcpChildEnv = "JAMF_CLI_MCP"

const classicSecretPrefix = "S3CRET-"

// classicResourcesWithSchemas is the same walk TestEverySecretBearingFieldIsRefusedForSet
// makes: the manifest with the committed schema artifact attached.
func classicResourcesWithSchemas(t *testing.T) []classic.ClassicResource {
	t.Helper()
	res, err := classic.ParseManifest("../../../../specs/classic/resources.yaml")
	if err != nil {
		t.Fatalf("loading the Classic manifest: %v", err)
	}
	art, err := classicschema.Load("../../../../specs/classic/schemas.json")
	if err != nil || art == nil {
		t.Fatalf("loading the Classic schema artifact: %v", err)
	}
	if err := classic.AttachSchemas(res, art); err != nil {
		t.Fatalf("attaching schemas: %v", err)
	}
	return res
}

// credentialLeaf is the element name a dotted credential path ends in.
func credentialLeaf(path string) string {
	return strings.TrimSuffix(path[strings.LastIndex(path, ".")+1:], "[]")
}

// withSecrets returns xml with every element named leaf holding a sentinel.
func withSecrets(xml string, leaves []string) string {
	for _, leaf := range leaves {
		re := regexp.MustCompile(`<` + leaf + `>[^<]*</` + leaf + `>|<` + leaf + `/>`)
		xml = re.ReplaceAllString(xml, "<"+leaf+">"+classicSecretPrefix+leaf+"</"+leaf+">")
	}
	return xml
}

type classicReadClient struct{ body string }

func (c classicReadClient) Do(_ context.Context, method, _ string, _ io.Reader) (*http.Response, error) {
	if method != http.MethodGet {
		return nil, fmt.Errorf("unexpected %s", method)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(c.body))}, nil
}

// runClassicRead runs `<cli> <args>` against a client answering body, with -o
// format when format is not empty, and returns what it printed.
func runClassicRead(t *testing.T, body, format string, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	f := output.New(cmp.Or(format, "json"), true, false)
	f.SetWriter(&buf)
	ctx := &registry.CLIContext{Client: classicReadClient{body: body}, Output: &ndjsonOutput{f: f, buf: &buf}}
	root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("output", "o", "json", "")
	RegisterClassicCommands(root, ctx)
	if format != "" {
		args = append(args, "-o", format)
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	return buf.String()
}

// TestClassicRead_RedactsEveryCredentialFieldInAnMCPChild walks every
// string-typed field the generator refuses for --set and shows a Classic get in
// an MCP child prints the marker in its place, in every format a model can ask
// for. XML keeps the marker escaped, since it is element text.
func TestClassicRead_RedactsEveryCredentialFieldInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnv, "1")
	checked := 0
	for _, r := range classicResourcesWithSchemas(t) {
		paths := r.CredentialFields()
		if len(paths) == 0 || !slices.Contains(r.Operations, "get") {
			continue
		}
		scaffold, err := r.ScaffoldXML()
		if err != nil {
			t.Fatalf("%s: scaffold: %v", r.CLIName, err)
		}
		var leaves []string
		for _, p := range paths {
			leaves = append(leaves, credentialLeaf(p))
		}
		doc := withSecrets(scaffold, leaves)
		for _, p := range paths {
			if !strings.Contains(doc, classicSecretPrefix+credentialLeaf(p)) {
				t.Errorf("%s: the scaffold carries no <%s> for %s, so the sweep cannot show it redacted", r.CLIName, credentialLeaf(p), p)
			}
			checked++
		}
		for _, format := range []string{"", "json", "yaml", "table", "plain", "xml", "raw"} {
			got := runClassicRead(t, doc, format, r.CLIName, "get", "1")
			if strings.Contains(got, classicSecretPrefix) {
				t.Errorf("%s get -o %q prints a credential field over MCP:\n%s", r.CLIName, format, got)
			}
			marker := "<redacted>"
			if format == "" || format == "xml" || format == "raw" {
				marker = "&lt;redacted&gt;"
			}
			if !strings.Contains(got, marker) {
				t.Errorf("%s get -o %q should print %s for each credential field (%v):\n%s", r.CLIName, format, marker, paths, got)
			}
		}
	}
	if checked < 25 {
		t.Errorf("the sweep checked only %d credential fields, too few to be walking the shipped artifact", checked)
	}
}

func TestClassicList_RedactsCredentialFieldsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnv, "1")
	body := `<vpp_accounts><size>1</size><vpp_account><id>1</id><name>VPP</name><service_token>` + classicSecretPrefix + `stoken</service_token></vpp_account></vpp_accounts>`
	for _, format := range []string{"", "json", "table"} {
		got := runClassicRead(t, body, format, "classic-vpp-accounts", "list")
		if strings.Contains(got, classicSecretPrefix) {
			t.Errorf("classic-vpp-accounts list -o %q prints the sToken over MCP:\n%s", format, got)
		}
	}
}

func TestClassicRead_OutsideMCPPrintsCredentialFieldsUnchanged(t *testing.T) {
	t.Setenv(mcpChildEnv, "")
	for _, r := range classicResourcesWithSchemas(t) {
		paths := r.CredentialFields()
		if len(paths) == 0 || !slices.Contains(r.Operations, "get") {
			continue
		}
		scaffold, err := r.ScaffoldXML()
		if err != nil {
			t.Fatalf("%s: scaffold: %v", r.CLIName, err)
		}
		var leaves []string
		for _, p := range paths {
			leaves = append(leaves, credentialLeaf(p))
		}
		doc := withSecrets(scaffold, leaves)
		if got := runClassicRead(t, doc, "raw", r.CLIName, "get", "1"); strings.TrimSpace(got) != strings.TrimSpace(doc) {
			t.Errorf("%s get -o raw outside MCP must print the wire bytes unchanged:\n got %s\nwant %s", r.CLIName, got, doc)
		}
		got := runClassicRead(t, doc, "json", r.CLIName, "get", "1")
		for _, leaf := range leaves {
			if !strings.Contains(got, classicSecretPrefix+leaf) {
				t.Errorf("%s get -o json outside MCP must print %s unchanged:\n%s", r.CLIName, leaf, got)
			}
		}
		if strings.Contains(got, "redacted") {
			t.Errorf("%s get outside MCP redacted something:\n%s", r.CLIName, got)
		}
	}
}
