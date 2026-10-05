// Copyright 2026, Jamf Software LLC

package generated

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
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
			out := setClassicGeneralName([]byte(body), "os_x_configuration_profile", "Vendor Test")
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
		if hasClassicGeneralName(body) {
			t.Errorf("hasClassicGeneralName(%s) = true; the document has no general/name", body)
		}
	}
}

func TestInjectClassicRedeployOnUpdate_LandsInTheRealGeneral(t *testing.T) {
	for _, body := range []string{
		`<p><!-- <general> --><general><name>n</name></general></p>`,
		`<p><general><!-- <redeploy_on_update>Newly Assigned</redeploy_on_update> --><name>n</name></general></p>`,
	} {
		out := injectClassicRedeployOnUpdate([]byte(body))
		wantTexts(t, out, []string{"All"}, "general", "redeploy_on_update")
	}
	kept := injectClassicRedeployOnUpdate([]byte(`<p><general><redeploy_on_update>Newly Assigned</redeploy_on_update></general></p>`))
	wantTexts(t, kept, []string{"Newly Assigned"}, "general", "redeploy_on_update")
}

func TestClassicProfilePayloadEditors_IgnoreACommentedPayloads(t *testing.T) {
	body := []byte(`<p><!-- <payloads><![CDATA[DECOY]]></payloads> --><general><payloads><![CDATA[REAL &amp; MORE]]></payloads></general></p>`)
	if got := string(classicProfilePayloadFromBody(body)); got != "REAL & MORE" {
		t.Errorf("classicProfilePayloadFromBody = %q, want the real payload", got)
	}
	const decoy = `<!-- <payloads>DECOY</payloads> -->`
	plain := `<p><general><payloads>REAL &amp;amp; MORE</payloads></general></p>`
	normalized := normalizeClassicProfilePayloadsForSend([]byte(strings.Replace(plain, "<general>", decoy+"<general>", 1)))
	if want := strings.Replace(string(normalizeClassicProfilePayloadsForSend([]byte(plain))), "<general>", decoy+"<general>", 1); string(normalized) != want {
		t.Errorf("normalized with a decoy =\n%s\nwant the real payload normalized and the comment untouched:\n%s", normalized, want)
	}
	replaced := replaceClassicProfilePayload(body, []byte("NEW"))
	wantTexts(t, replaced, []string{"NEW"}, "general", "payloads")
}
