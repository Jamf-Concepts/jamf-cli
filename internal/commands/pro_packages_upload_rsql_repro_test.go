// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// rsqlNode is a parsed RSQL expression: an or/and of children, or a leaf
// comparison of field op pattern.
type rsqlNode struct {
	kind     string // "or", "and", "cmp"
	children []*rsqlNode
	field    string
	op       string
	literal  string // the value with escapes removed
	wildcard bool   // value held an unescaped *
	re       *regexp.Regexp
}

type rsqlParser struct {
	s string
	i int
}

func parseRSQL(s string) (*rsqlNode, error) {
	p := &rsqlParser{s: s}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.i != len(p.s) {
		return nil, fmt.Errorf("trailing input at %d: %q", p.i, p.s[p.i:])
	}
	return n, nil
}

func (p *rsqlParser) eat(tok string) bool {
	if strings.HasPrefix(p.s[p.i:], tok) {
		p.i += len(tok)
		return true
	}
	return false
}

func (p *rsqlParser) or() (*rsqlNode, error) {
	first, err := p.and()
	if err != nil {
		return nil, err
	}
	n := &rsqlNode{kind: "or", children: []*rsqlNode{first}}
	for p.eat(",") || p.eat(" or ") {
		c, err := p.and()
		if err != nil {
			return nil, err
		}
		n.children = append(n.children, c)
	}
	if len(n.children) == 1 {
		return first, nil
	}
	return n, nil
}

func (p *rsqlParser) and() (*rsqlNode, error) {
	first, err := p.cmp()
	if err != nil {
		return nil, err
	}
	n := &rsqlNode{kind: "and", children: []*rsqlNode{first}}
	for p.eat(";") || p.eat(" and ") {
		c, err := p.cmp()
		if err != nil {
			return nil, err
		}
		n.children = append(n.children, c)
	}
	if len(n.children) == 1 {
		return first, nil
	}
	return n, nil
}

func (p *rsqlParser) cmp() (*rsqlNode, error) {
	if p.eat("(") {
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if !p.eat(")") {
			return nil, fmt.Errorf("missing ) at %d", p.i)
		}
		return n, nil
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] == '.' || p.s[p.i] == '_' ||
		(p.s[p.i] >= 'a' && p.s[p.i] <= 'z') || (p.s[p.i] >= 'A' && p.s[p.i] <= 'Z')) {
		p.i++
	}
	field := p.s[start:p.i]
	if field == "" {
		return nil, fmt.Errorf("expected field at %d", p.i)
	}
	var op string
	switch {
	case p.eat("=="):
		op = "=="
	case p.eat("!="):
		op = "!="
	default:
		return nil, fmt.Errorf("expected operator at %d", p.i)
	}
	var lit, re strings.Builder
	wild := false
	if p.eat(`"`) {
		closed := false
		for p.i < len(p.s) {
			c := p.s[p.i]
			p.i++
			if c == '\\' && p.i < len(p.s) {
				lit.WriteByte(p.s[p.i])
				re.WriteString(regexp.QuoteMeta(p.s[p.i : p.i+1]))
				p.i++
				continue
			}
			if c == '"' {
				closed = true
				break
			}
			if c == '*' {
				wild = true
				re.WriteString(".*")
			} else {
				re.WriteString(regexp.QuoteMeta(string(c)))
			}
			lit.WriteByte(c)
		}
		if !closed {
			return nil, fmt.Errorf("unterminated string")
		}
	} else {
		for p.i < len(p.s) && !strings.ContainsRune(",;) ", rune(p.s[p.i])) {
			c := p.s[p.i]
			p.i++
			if c == '*' {
				wild = true
				re.WriteString(".*")
			} else {
				re.WriteString(regexp.QuoteMeta(string(c)))
			}
			lit.WriteByte(c)
		}
	}
	return &rsqlNode{
		kind: "cmp", field: field, op: op, literal: lit.String(), wildcard: wild,
		re: regexp.MustCompile("^" + re.String() + "$"),
	}, nil
}

func (n *rsqlNode) match(rec map[string]string) bool {
	switch n.kind {
	case "or":
		for _, c := range n.children {
			if c.match(rec) {
				return true
			}
		}
		return false
	case "and":
		for _, c := range n.children {
			if !c.match(rec) {
				return false
			}
		}
		return true
	}
	hit := n.re.MatchString(rec[n.field])
	if n.op == "!=" {
		return !hit
	}
	return hit
}

// fakeRSQLPackages is an in-process Jamf Pro /v1/packages that evaluates the
// filter with RSQL grammar (`,`/or, `;`/and, ==, !=, `*` wildcard, `\` escape)
// and honours page-size, so the lookup sees what a real server would return.
type fakeRSQLPackages struct {
	records       []map[string]string
	ignoreFilter  bool
	filters       []*rsqlNode
	rawFilters    []string
	uploadedPaths []string
}

func (f *fakeRSQLPackages) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	rec := httptest.NewRecorder()
	u, _ := url.Parse(path)
	switch {
	case method == http.MethodGet && u.Path == "/v1/packages":
		raw := u.Query().Get("filter")
		f.rawFilters = append(f.rawFilters, raw)
		node, err := parseRSQL(raw)
		if err != nil {
			rec.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(rec, `{"errors":[{"description":%q}]}`, err.Error())
			return rec.Result(), nil
		}
		f.filters = append(f.filters, node)
		var hits []map[string]string
		for _, r := range f.records {
			if f.ignoreFilter || node.match(r) {
				hits = append(hits, r)
			}
		}
		size := len(hits)
		if ps, err := strconv.Atoi(u.Query().Get("page-size")); err == nil && ps < size {
			size = ps
		}
		page := make([]any, 0, size)
		for _, h := range hits[:size] {
			page = append(page, h)
		}
		body, _ := json.Marshal(map[string]any{"totalCount": len(hits), "results": page})
		rec.Header().Set("Content-Type", "application/json")
		_, _ = rec.Write(body)
	case method == http.MethodGet && strings.HasPrefix(u.Path, "/v1/packages/"):
		id := strings.TrimPrefix(u.Path, "/v1/packages/")
		for _, r := range f.records {
			if r["id"] == id {
				body, _ := json.Marshal(r)
				rec.Header().Set("Content-Type", "application/json")
				_, _ = rec.Write(body)
				return rec.Result(), nil
			}
		}
		rec.WriteHeader(http.StatusNotFound)
	case method == http.MethodPost && u.Path == "/v1/packages":
		rec.WriteHeader(http.StatusCreated)
		_, _ = rec.Write([]byte(`{"id":"999"}`))
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

var errUploadRecorded = errors.New("upload recorded; stopping before verify")

func (f *fakeRSQLPackages) Upload(_ context.Context, path string, _ io.Reader, _ string, _ int64) (*http.Response, error) {
	f.uploadedPaths = append(f.uploadedPaths, path)
	return nil, errUploadRecorded
}

func tenantPackages() []map[string]string {
	return []map[string]string{
		{"id": "1", "fileName": "GlobalSecurityAgent.pkg", "packageName": "Security Agent (all computers)"},
		{"id": "2", "fileName": "Firefox.pkg", "packageName": "Firefox"},
	}
}

// Every name here is a legal macOS/Linux basename for which no package exists.
var hostilePackageNames = []string{
	`Nope" or fileName!="x.pkg`,                        // quote + or + != : matches every package
	`Nope",fileName=="GlobalSecurityAgent.pkg`,         // quote + , : targets one chosen package
	`Nope",id!="0";fileName=="Global*`,                 // quote , ; != * : targets by wildcard
	`*.pkg`,                                            // bare * inside the literal: EscapeRSQL leaves it
	`Nope",(fileName=="GlobalSecurityAgent.pkg");x=="`, // parentheses
}

// The lookup must send the filename as one quoted literal and must never
// resolve a hostile name to an unrelated package.
func TestRepro_FindPackageByFileName_FilenameIsRSQLEscaped(t *testing.T) {
	for _, name := range hostilePackageNames {
		t.Run(name, func(t *testing.T) {
			fake := &fakeRSQLPackages{records: tenantPackages()}
			id, err := findPackageByFileName(context.Background(), fake, name)

			if len(fake.filters) == 1 {
				n := fake.filters[0]
				if n.kind != "cmp" || n.field != "fileName" || n.op != "==" || n.wildcard || n.literal != name {
					t.Errorf("filter %q is not a single fileName== literal equal to the local name", fake.rawFilters[0])
				}
			}
			if err == nil && id != "" {
				t.Errorf("hostile filename %q resolved to unrelated package id %s; want not-found or an error", name, id)
			}
		})
	}
}

// Two packages answering one filename must be refused, not resolved to
// whichever the server lists first.
func TestRepro_FindPackageByFileName_RefusesMoreThanOneMatch(t *testing.T) {
	fake := &fakeRSQLPackages{records: []map[string]string{
		{"id": "7", "fileName": "Dup.pkg"},
		{"id": "8", "fileName": "Dup.pkg"},
	}}
	id, err := findPackageByFileName(context.Background(), fake, "Dup.pkg")
	if err == nil {
		t.Errorf("two packages share fileName Dup.pkg; lookup returned id %q with no error", id)
	}
}

// A returned record whose fileName is not the local name is not a match.
func TestRepro_FindPackageByFileName_ConfirmsReturnedFileName(t *testing.T) {
	fake := &fakeRSQLPackages{records: tenantPackages(), ignoreFilter: true}
	id, err := findPackageByFileName(context.Background(), fake, "Mine.pkg")
	if err == nil && id != "" {
		t.Errorf("server ignored the filter and returned id %s (fileName %q); lookup accepted it for Mine.pkg",
			id, fake.records[0]["fileName"])
	}
}

// End to end: `pro packages upload --file <hostile> --yes` must not POST the
// local file onto an unrelated existing package.
func TestRepro_PackagesUpload_HostileNameDoesNotReplaceUnrelatedPackage(t *testing.T) {
	dir := t.TempDir()
	name := `Nope",fileName=="GlobalSecurityAgent.pkg`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("attacker payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRSQLPackages{records: tenantPackages()}
	cmd := newPackagesUploadCmd(&registry.CLIContext{Client: fake, Uploader: fake})
	cmd.SetArgs([]string{"--file", path, "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	_ = cmd.Execute()

	for _, p := range fake.uploadedPaths {
		if p == "/v1/packages/1/upload" {
			t.Errorf("local file %q was uploaded onto unrelated package 1 (GlobalSecurityAgent.pkg); uploads: %v",
				name, fake.uploadedPaths)
		}
	}
}

// Controls: the fake resolves a benign name, and a correctly escaped literal
// for each hostile name matches nothing, so the failures above are the
// lookup's and not the fake's.
func TestRepro_Control_FakeIsNotOverPermissive(t *testing.T) {
	fake := &fakeRSQLPackages{records: tenantPackages()}
	if id, err := findPackageByFileName(context.Background(), fake, "Firefox.pkg"); err != nil || id != "2" {
		t.Fatalf("benign lookup = %q, %v; want 2", id, err)
	}
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `*`, `\*`, `(`, `\(`, `)`, `\)`, `;`, `\;`, `,`, `\,`)
	for _, name := range hostilePackageNames {
		node, err := parseRSQL(`fileName=="` + esc.Replace(name) + `"`)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if node.kind != "cmp" || node.literal != name || node.wildcard {
			t.Errorf("%q: escaped filter did not parse as one literal", name)
		}
		for _, r := range fake.records {
			if node.match(r) {
				t.Errorf("%q: escaped filter matched %v", name, r)
			}
		}
	}
}
