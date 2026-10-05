// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// textsAt returns the character data of every element at path below the root,
// as an XML parser (and so the Classic API) reads the document.
func textsAt(t *testing.T, body []byte, path ...string) []string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(body))
	var stack []string
	var texts []string
	var cur *strings.Builder
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return texts
		}
		if err != nil {
			t.Fatalf("output is not well-formed XML: %v\n%s", err, body)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			stack = append(stack, tok.Name.Local)
			if len(stack) > 1 && slices.Equal(stack[1:], path) {
				cur = &strings.Builder{}
			}
		case xml.CharData:
			if cur != nil && len(stack) > 1 && slices.Equal(stack[1:], path) {
				cur.Write(tok)
			}
		case xml.EndElement:
			if len(stack) > 1 && slices.Equal(stack[1:], path) {
				texts = append(texts, cur.String())
				cur = nil
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func wantTexts(t *testing.T, body []byte, want []string, path ...string) {
	t.Helper()
	if got := textsAt(t, body, path...); !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q\n%s", strings.Join(path, "/"), got, want, body)
	}
}

func TestInjectClassicFileFields_IgnoresDecoysTheServerDoesNotRead(t *testing.T) {
	spec := func(path string) []classicFileFieldSpec {
		return []classicFileFieldSpec{{FilePath: path, ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "none"}}
	}
	for name, body := range map[string]string{
		"comment": `<os_x_configuration_profile><!-- <payloads></payloads> --><general><name>Baseline</name><payloads><![CDATA[ATTACKER]]></payloads></general></os_x_configuration_profile>`,
		"cdata":   `<os_x_configuration_profile><general><description><![CDATA[<payloads></payloads>]]></description><name>Baseline</name><payloads>ATTACKER</payloads></general></os_x_configuration_profile>`,
		"parent":  `<os_x_configuration_profile><scope><payloads>x</payloads></scope><general><name>Baseline</name><payloads>ATTACKER</payloads></general></os_x_configuration_profile>`,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := injectClassicFileFields([]byte(body), "os_x_configuration_profile", spec(writeTempFile(t, "own.mobileconfig", "OPERATOR")))
			if err != nil {
				t.Fatal(err)
			}
			wantTexts(t, out, []string{"OPERATOR"}, "general", "payloads")
		})
	}
}

func TestInjectClassicFileFields_InsertsIntoTheRealParent(t *testing.T) {
	body := `<mac_application><!-- <app_configuration></app_configuration> --><general><name>App</name></general><app_configuration></app_configuration></mac_application>`
	out, err := injectClassicFileFields([]byte(body), "mac_application", []classicFileFieldSpec{
		{FilePath: writeTempFile(t, "app.plist", "PREFS"), ParentPath: []string{"app_configuration"}, LeafName: "preferences", Encoding: "raw", NameFallback: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTexts(t, out, []string{"PREFS"}, "app_configuration", "preferences")
}

func TestInjectClassicFileFields_RefusesARepeatedTarget(t *testing.T) {
	body := `<os_x_configuration_profile><general><payloads>A</payloads><payloads>B</payloads></general></os_x_configuration_profile>`
	_, err := injectClassicFileFields([]byte(body), "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: writeTempFile(t, "own.mobileconfig", "OPERATOR"), ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "none"},
	})
	if err == nil || !strings.Contains(err.Error(), "general/payloads") {
		t.Fatalf("err = %v, want a refusal naming general/payloads", err)
	}
}

func TestInjectClassicFileFields_NameFallbackSeesOnlyGeneralName(t *testing.T) {
	body := `<os_x_configuration_profile><general><category><name>Security</name></category><!-- <name>x</name> --></general></os_x_configuration_profile>`
	out, err := injectClassicFileFields([]byte(body), "os_x_configuration_profile", []classicFileFieldSpec{
		{FilePath: writeTempFile(t, "Wi-Fi.mobileconfig", "P"), ParentPath: []string{"general"}, LeafName: "payloads", Encoding: "xml-cdata", NameFallback: "strip-ext"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTexts(t, out, []string{"Wi-Fi"}, "general", "name")
	wantTexts(t, out, []string{"Security"}, "general", "category", "name")
}

func TestSetClassicGeneralName_SetsTheNameTheServerReads(t *testing.T) {
	for name, body := range map[string]string{
		"category first": `<os_x_configuration_profile><general><category><name>x</name></category><name>Corp Baseline</name></general></os_x_configuration_profile>`,
		"cdata":          `<os_x_configuration_profile><general><description><![CDATA[<name>x</name>]]></description><name>Corp Baseline</name></general></os_x_configuration_profile>`,
		"comment":        `<os_x_configuration_profile><!-- <general><name>x</name></general> --><general><name>Corp Baseline</name></general></os_x_configuration_profile>`,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := setClassicGeneralName([]byte(body), "os_x_configuration_profile", "Vendor Test")
			if err != nil {
				t.Fatal(err)
			}
			wantTexts(t, out, []string{"Vendor Test"}, "general", "name")
		})
	}
}

func TestHasClassicGeneralName_IgnoresOtherNames(t *testing.T) {
	for _, body := range []string{
		`<p><general><category><name>x</name></category></general></p>`,
		`<p><general><!-- <name>x</name> --></general></p>`,
		`<p><general><description><![CDATA[<name>x</name>]]></description></general></p>`,
	} {
		if has, err := hasClassicGeneralName([]byte(body)); err != nil || has {
			t.Errorf("hasClassicGeneralName(%s) = %v, %v; the document has no general/name", body, has, err)
		}
	}
}

func TestInjectClassicRedeployOnUpdate_LandsInTheRealGeneral(t *testing.T) {
	for _, body := range []string{
		`<p><!-- <general> --><general><name>n</name></general></p>`,
		`<p><general><!-- <redeploy_on_update>Newly Assigned</redeploy_on_update> --><name>n</name></general></p>`,
	} {
		out, err := injectClassicRedeployOnUpdate([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		wantTexts(t, out, []string{"All"}, "general", "redeploy_on_update")
	}
	kept, err := injectClassicRedeployOnUpdate([]byte(`<p><general><redeploy_on_update>Newly Assigned</redeploy_on_update></general></p>`))
	if err != nil {
		t.Fatal(err)
	}
	wantTexts(t, kept, []string{"Newly Assigned"}, "general", "redeploy_on_update")
}

func TestClassicProfilePayloadEditors_IgnoreACommentedPayloads(t *testing.T) {
	body := []byte(`<p><!-- <payloads><![CDATA[DECOY]]></payloads> --><general><payloads><![CDATA[REAL &amp; MORE]]></payloads></general></p>`)
	if got := string(classicProfilePayloadFromBody(body)); got != "REAL & MORE" {
		t.Errorf("classicProfilePayloadFromBody = %q, want the real payload", got)
	}
	const decoy = `<!-- <payloads>DECOY</payloads> -->`
	plain := `<p><general><payloads>REAL &amp;amp; MORE</payloads></general></p>`
	normalized := normalizeForSend(t, []byte(strings.Replace(plain, "<general>", decoy+"<general>", 1)))
	if want := strings.Replace(string(normalizeForSend(t, []byte(plain))), "<general>", decoy+"<general>", 1); string(normalized) != want {
		t.Errorf("normalized with a decoy =\n%s\nwant the real payload normalized and the comment untouched:\n%s", normalized, want)
	}
	replaced := replaceClassicProfilePayload(body, []byte("NEW"))
	wantTexts(t, replaced, []string{"NEW"}, "general", "payloads")
}

// classicRoutingClient answers GETs from a fixed path table and records every
// request, so a test can assert which record a write was sent to.
type classicRoutingClient struct {
	gets  map[string]string
	calls []string
}

func (c *classicRoutingClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.calls = append(c.calls, method+" "+path)
	status, body := http.StatusCreated, `<os_x_configuration_profile><id>9</id></os_x_configuration_profile>`
	if method == http.MethodGet {
		status, body = http.StatusOK, c.gets[path]
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

var _ registry.HTTPClient = (*classicRoutingClient)(nil)

func (c *classicRoutingClient) writes() []string {
	var out []string
	for _, call := range c.calls {
		if !strings.HasPrefix(call, http.MethodGet+" ") {
			out = append(out, call)
		}
	}
	return out
}

func profileListClient() *classicRoutingClient {
	return &classicRoutingClient{gets: map[string]string{
		"/JSSResource/osxconfigurationprofiles": string(makeClassicListXML([][2]string{{"1", "Victim"}, {"2", "Corp WiFi"}})),
	}}
}

func TestClassicProfileApply_NameFlagIsTheLookupKey(t *testing.T) {
	client := profileListClient()
	p := writeXML(t, `<os_x_configuration_profile><name>Victim</name><general><payloads>DOC</payloads></general></os_x_configuration_profile>`)
	cmd := newClassicMacosConfigProfilesApplyCmd(&registry.CLIContext{Client: client, Output: newNDJSONOutput()})
	cmd.SetArgs([]string{"--name", "Corp WiFi", "--from-file", p, "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := client.writes(); !slices.Equal(got, []string{"PUT /JSSResource/osxconfigurationprofiles/id/2"}) {
		t.Errorf("writes = %q, want one PUT to the record --name names (id 2)", got)
	}
}

func TestClassicProfileApply_RefusesTwoDifferentNames(t *testing.T) {
	client := profileListClient()
	p := writeXML(t, `<os_x_configuration_profile><name>Victim</name><general><name>Vendor</name></general></os_x_configuration_profile>`)
	cmd := newClassicMacosConfigProfilesApplyCmd(&registry.CLIContext{Client: client, Output: newNDJSONOutput()})
	cmd.SetArgs([]string{"--from-file", p, "--yes"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Victim") || !strings.Contains(err.Error(), "Vendor") {
		t.Fatalf("err = %v, want a refusal naming both names", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("requests sent before the refusal: %q", client.calls)
	}
}

func TestExtractClassicName_OneNameOrTheSameTwice(t *testing.T) {
	for _, tc := range []struct{ key, body, want string }{
		{"policy", `<policy><general><name>P</name></general></policy>`, "P"},
		{"building", `<building><name>B</name></building>`, "B"},
		{"policy", `<policy><name>P</name><general><name> P </name></general></policy>`, "P"},
		{"policy", `<policy><general><name>P</name><category><name>C</name></category></general></policy>`, "P"},
	} {
		if got, err := extractClassicName([]byte(tc.body), tc.key); err != nil || got != tc.want {
			t.Errorf("extractClassicName(%s) = %q, %v; want %q", tc.body, got, err, tc.want)
		}
	}
}

func TestClassicProfileWrites_RefuseABodyTheEditorsCannotRead(t *testing.T) {
	// Create edits only <general><payloads>, so a second <general> with no
	// payload in it leaves create nothing to misread; update inserts the
	// redeploy marker into <general> and has to refuse it.
	for _, tc := range []struct {
		name, body string
		verbs      []string
	}{
		{"two generals", `<os_x_configuration_profile><general><name>A</name></general><general><name>B</name></general></os_x_configuration_profile>`, []string{"update"}},
		{"two payloads", `<os_x_configuration_profile><general><name>A</name><payloads>X</payloads><payloads>Y</payloads></general></os_x_configuration_profile>`, []string{"create", "update"}},
		{"latin-1 prolog", `<?xml version="1.0" encoding="ISO-8859-1"?><os_x_configuration_profile><general><name>A</name></general></os_x_configuration_profile>`, []string{"create", "update"}},
	} {
		for _, verb := range tc.verbs {
			t.Run(tc.name+"/"+verb, func(t *testing.T) {
				client := profileListClient()
				ctx := &registry.CLIContext{Client: client, Output: newNDJSONOutput()}
				cmd, args := newClassicMacosConfigProfilesCreateCmd(ctx), []string{}
				if verb == "update" {
					cmd, args = newClassicMacosConfigProfilesUpdateCmd(ctx), []string{"42"}
				}
				cmd.SetArgs(append(args, "--from-file", writeXML(t, tc.body)))
				if err := cmd.Execute(); err == nil {
					t.Fatal("expected a refusal")
				}
				if w := client.writes(); len(w) != 0 {
					t.Errorf("write sent despite the refusal: %q", w)
				}
			})
		}
	}
}

func TestInjectClassicRedeployOnUpdate_RefusesTwoGenerals(t *testing.T) {
	_, err := injectClassicRedeployOnUpdate([]byte(`<p><general/><general/></p>`))
	if err == nil || !strings.Contains(err.Error(), "general") {
		t.Fatalf("err = %v, want a refusal naming general", err)
	}
}
