// Copyright 2026, Jamf Software LLC

package scope

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// policyStore serves a Classic policy collection listing and one document per
// id, and stores each PUT body as that id's document so the verification read
// sees what was written. /name/ answers with namePick's document whatever the
// name, the way the server answers one record when two share a name.
type policyStore struct {
	list     string
	docs     map[string]string
	namePick string
	requests []string
}

func (p *policyStore) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	p.requests = append(p.requests, method+" "+path)
	respond := func(s string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(s))}, nil
	}
	if path == "/JSSResource/policies" {
		return respond(p.list)
	}
	if strings.HasPrefix(path, "/JSSResource/policies/name/") {
		return respond(p.docs[p.namePick])
	}
	id, ok := strings.CutPrefix(path, "/JSSResource/policies/id/")
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	if method == http.MethodPut {
		b, _ := io.ReadAll(body)
		p.docs[id] = string(b)
		return respond("")
	}
	return respond(p.docs[id])
}

func (p *policyStore) puts() []string {
	var out []string
	for _, r := range p.requests {
		if strings.HasPrefix(r, http.MethodPut) {
			out = append(out, r)
		}
	}
	return out
}

type discardOutput struct{ registry.OutputFormatter }

func (discardOutput) PrintRaw([]byte) error { return nil }

func runScopeAdd(t *testing.T, store *policyStore, args ...string) error {
	t.Helper()
	res := Resource{APIPath: "policies", SingularKey: "policy", CLIName: "classic-policies"}
	cmd := NewScopeCmd(&registry.CLIContext{Client: store, Output: discardOutput{}}, res)
	cmd.SetArgs(append([]string{"add"}, args...))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.ExecuteContext(context.Background())
}

const emptyPolicy = `<policy><general><id>%s</id></general><scope/></policy>`

func TestScopeAdd_DuplicatePolicyNameIsRefusedWithNoWrite(t *testing.T) {
	store := &policyStore{
		list: `<policies><size>2</size>` +
			`<policy><id>11</id><name>Patch Tuesday</name></policy>` +
			`<policy><id>12</id><name>Patch Tuesday</name></policy></policies>`,
		docs: map[string]string{
			"11": strings.Replace(emptyPolicy, "%s", "11", 1),
			"12": strings.Replace(emptyPolicy, "%s", "12", 1),
		},
		namePick: "11",
	}
	err := runScopeAdd(t, store, "--name", "Patch Tuesday", "--computer-group", "Lab")
	if err == nil {
		t.Fatal("scope add --name on a duplicated policy name succeeded; want a refusal")
	}
	for _, id := range []string{"11", "12"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("refusal %q does not name id %s", err, id)
		}
	}
	if puts := store.puts(); len(puts) != 0 {
		t.Errorf("an ambiguous name must write nothing; sent %v", puts)
	}
	for _, r := range store.requests {
		if strings.Contains(r, "/name/") {
			t.Errorf("resolved through the server's /name/ pick: %s", r)
		}
	}
}

func TestScopeAdd_UniquePolicyNameWritesToItsID(t *testing.T) {
	store := &policyStore{
		list: `<policies><size>2</size>` +
			`<policy><id>11</id><name>Patch Tuesday</name></policy>` +
			`<policy><id>12</id><name>Patch Wednesday</name></policy></policies>`,
		docs: map[string]string{
			"11": strings.Replace(emptyPolicy, "%s", "11", 1),
			"12": strings.Replace(emptyPolicy, "%s", "12", 1),
		},
		namePick: "12",
	}
	if err := runScopeAdd(t, store, "--name", "Patch Wednesday", "--computer-group", "Lab"); err != nil {
		t.Fatalf("scope add: %v", err)
	}
	puts := store.puts()
	if len(puts) != 1 || puts[0] != "PUT /JSSResource/policies/id/12" {
		t.Errorf("writes = %v, want one PUT to /JSSResource/policies/id/12", puts)
	}
	if !strings.Contains(store.docs["12"], "Lab") || strings.Contains(store.docs["11"], "Lab") {
		t.Errorf("the group landed on the wrong policy: 11=%q 12=%q", store.docs["11"], store.docs["12"])
	}
}
