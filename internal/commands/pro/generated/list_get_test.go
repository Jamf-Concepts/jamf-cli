// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// routeClient answers a request by the longest route its unescaped path
// contains; anything unrouted is a 404, the way client.Do reports one.
type routeClient struct {
	routes map[string]string
	calls  []string
}

func (c *routeClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	decoded, _ := url.QueryUnescape(path)
	c.calls = append(c.calls, method+" "+decoded)
	best := ""
	for r := range c.routes {
		if strings.Contains(decoded, r) && len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		return nil, exitcode.New(exitcode.NotFound, "resource not found (HTTP 404)")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(c.routes[best]))}, nil
}

func writeList(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// runListGetCmd runs `<args>` through the registered tree, with -o format when
// format is set, and returns stdout and the error.
func runListGetCmd(t *testing.T, client registry.HTTPClient, format string, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	f := output.New(cmp.Or(format, "json"), true, false)
	f.SetWriter(&buf)
	ctx := &registry.CLIContext{Client: client, Output: &ndjsonOutput{f: f, buf: &buf}}
	root := &cobra.Command{Use: "jamf-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("output", "o", "json", "")
	root.PersistentFlags().Bool("allow-partial-failure", false, "")
	RegisterCommands(root, ctx)
	RegisterClassicCommands(root, ctx)
	if format != "" {
		args = append(args, "-o", format)
	}
	root.SetArgs(args)
	orig := os.Stderr
	os.Stderr, _ = os.Open(os.DevNull)
	err := root.Execute()
	os.Stderr = orig
	return buf.String(), err
}

// TestProListGet_PrintsEveryRecordInListOrder: names are resolved in one
// filtered request, each record is read with the request `get <id>` sends, the
// records come back as one array in the order the file named them, and a
// record that cannot be read costs exit 7 without hiding the rest.
func TestProListGet_PrintsEveryRecordInListOrder(t *testing.T) {
	client := &routeClient{routes: map[string]string{
		`/v1/buildings?filter=name=in=`: `{"totalCount":1,"results":[{"id":"7","name":"Main Office"}]}`,
		`/v1/buildings/7`:               `{"id":"7","name":"Main Office"}`,
		`/v1/buildings/3`:               `{"id":"3","name":"Annex"}`,
	}}
	out, err := runListGetCmd(t, client, "json", "buildings", "get", "--list-file", writeList(t, "3\nMain Office\n404\n"))
	if code := exitcode.CodeFrom(err); code != exitcode.PartialFailure {
		t.Fatalf("exit = %d (%v), want 7 for the record that could not be read", code, err)
	}
	var got []map[string]any
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", jerr, out)
	}
	if len(got) != 2 || got[0]["id"] != "3" || got[1]["id"] != "7" {
		t.Errorf("records = %v, want 3 then 7, in list order", got)
	}
	lookups := 0
	for _, c := range client.calls {
		if strings.Contains(c, "filter=") {
			lookups++
		}
	}
	if lookups != 1 {
		t.Errorf("sent %d name lookups, want 1: %v", lookups, client.calls)
	}
}

func TestProListGet_RefusesAnIDBesideTheList(t *testing.T) {
	_, err := runListGetCmd(t, &routeClient{}, "json", "buildings", "get", "7", "--list-file", writeList(t, "7\n"))
	if err == nil || !strings.Contains(err.Error(), "--list-file") {
		t.Errorf("err = %v, want <id> and --list-file refused together", err)
	}
}

// TestClassicListGet_PrintsOneDocument: under the default format the records
// are one XML document shaped like a Classic list, and under -o json one array
// of the records unwrapped the way `get -o json` unwraps one.
func TestClassicListGet_PrintsOneDocument(t *testing.T) {
	printer := func(id string) string {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><printer><id>%s</id><name>P%s</name></printer>`, id, id)
	}
	client := &routeClient{routes: map[string]string{
		"/JSSResource/printers/id/1": printer("1"),
		"/JSSResource/printers/id/2": printer("2"),
	}}
	list := writeList(t, "1\n2\n")

	xmlOut, err := runListGetCmd(t, client, "", "classic-printers", "get", "--list-file", list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(xmlOut, "<?xml") != 1 || !strings.Contains(xmlOut, "<printers>") || !strings.Contains(xmlOut, "<size>2</size>") || strings.Count(xmlOut, "<printer>") != 2 {
		t.Errorf("default output is not one Classic-list-shaped document:\n%s", xmlOut)
	}

	jsonOut, err := runListGetCmd(t, client, "json", "classic-printers", "get", "--list-file", list)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil || len(got) != 2 || got[0]["name"] != "P1" {
		t.Errorf("-o json = %s (%v), want two unwrapped printers", jsonOut, err)
	}
}

// TestClassicListGet_RedactsInAnMCPChild: each record in the list passes the
// same MCP-child redaction `get <id>` applies, so a list is not a way round it.
func TestClassicListGet_RedactsInAnMCPChild(t *testing.T) {
	t.Setenv(mcpChildEnv, "1")
	server := &routeClient{routes: map[string]string{
		"/JSSResource/ldapservers/id/4": `<ldap_server><connection><id>4</id><name>Corp</name><account><distinguished_username>cn=svc</distinguished_username><password>hunter2-secret</password></account></connection></ldap_server>`,
	}}
	out, err := runListGetCmd(t, server, "json", "classic-ldap-servers", "get", "--list-file", writeList(t, "4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hunter2-secret") {
		t.Errorf("a credential reached the MCP child's output through --list-file:\n%s", out)
	}
}
