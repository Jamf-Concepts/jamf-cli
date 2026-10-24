// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
)

// requestLog records what reached a fake server.
type requestLog struct {
	mu   sync.Mutex
	reqs []string
}

func (l *requestLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reqs = append(l.reqs, s)
}

func (l *requestLog) matching(sub string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, r := range l.reqs {
		if strings.Contains(r, sub) {
			out = append(out, r)
		}
	}
	return out
}

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Cleanup(func() { dryRun, noInput = false, false })
}

func writeInput(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDryRun_ProtectMutationIsNeverSent(t *testing.T) {
	isolateHome(t)
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		log.add(r.URL.Path + " " + string(body))
		w.Header().Set("Content-Type", "application/json")
		switch q := string(body); {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"fake-protect-token","expires_in":3600,"token_type":"Bearer"}`)
		case strings.Contains(q, "listApiClients"):
			_, _ = io.WriteString(w, `{"data":{"listApiClients":{"items":[],"pageInfo":{"next":null,"total":0}}}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"createApiClient":{"clientId":"minted","name":"new-agent","password":"minted-secret"}}}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("JAMFPROTECT_URL", srv.URL)
	t.Setenv("JAMFPROTECT_CLIENT_ID", "fake-client-id")
	t.Setenv("JAMFPROTECT_CLIENT_SECRET", "fake-client-secret")

	in := writeInput(t, "client.json", `{"name":"new-agent","roles":[]}`)
	stdout, stderr, err := runRoot(t, "-n", "--no-input", "protect", "api-clients", "apply", "--from-file", in)
	if err == nil {
		t.Fatalf("apply under -n succeeded; want the create refused. stdout=%q", stdout)
	}
	if got := log.matching("mutation"); len(got) != 0 {
		t.Fatalf("-n sent %d mutation(s) to Protect: %q", len(got), got)
	}
	if len(log.matching("listApiClients")) == 0 {
		t.Errorf("-n sent no read; the name lookup should still go out")
	}
	if !strings.Contains(stderr, "[dry-run] POST /app (mutation createApiClient)") || !strings.Contains(err.Error()+stderr, "--dry-run is set") {
		t.Errorf("want a [dry-run] report on stderr and the refusal in the error; stderr=%q err=%v", stderr, err)
	}
	if strings.Contains(stdout, "minted-secret") {
		t.Errorf("a credential was minted under -n: %q", stdout)
	}
}

func TestDryRun_SchoolCreateIsNeverSent(t *testing.T) {
	isolateHome(t)
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"code":200,"count":0,"users":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":200,"id":7}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("JAMFSCHOOL_URL", srv.URL)
	t.Setenv("JAMFSCHOOL_NETWORK_ID", "1")
	t.Setenv("JAMFSCHOOL_API_KEY", "fake-key")

	in := writeInput(t, "user.json", `{"Username":"new-student","Email":"s@example.invalid"}`)
	_, stderr, err := runRoot(t, "-n", "--no-input", "school", "users", "apply", "--from-file", in)
	if err == nil {
		t.Fatalf("apply under -n succeeded; want the create refused")
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if got := log.matching(m + " "); len(got) != 0 {
			t.Fatalf("-n sent %q to School", got)
		}
	}
	if !strings.Contains(stderr, "[dry-run] POST /api/users") {
		t.Errorf("want a [dry-run] report on stderr; got %q", stderr)
	}
}

func TestDryRun_SchoolGuardRefusesEveryWriteMethod(t *testing.T) {
	t.Cleanup(func() { dryRun = false })
	dryRun = true
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.Method)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	c := newSchoolHTTPClient()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req, _ := http.NewRequest(m, srv.URL+"/api/users/1", strings.NewReader(`{}`))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusPreconditionFailed {
			t.Errorf("%s under -n answered %d; want 412", m, resp.StatusCode)
		}
	}
	resp, err := c.Get(srv.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := log.matching(""); len(got) != 1 || got[0] != http.MethodGet {
		t.Errorf("server saw %q; want exactly one GET", got)
	}
}

func TestDryRun_ProUploadIsNeverSent(t *testing.T) {
	isolateHome(t)
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"totalCount":0,"results":[]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("JAMF_URL", srv.URL)
	t.Setenv("JAMF_TOKEN", "fake-token")

	csv := writeInput(t, "preload.csv", "Serial Number,Device Type\nC02TEST,Computer\n")
	pkg := writeInput(t, "Tool.pkg", "not really a package")
	for _, args := range [][]string{
		{"-n", "--no-input", "pro", "inventory-preload", "upload", "--file", csv},
		{"-n", "--no-input", "pro", "packages", "upload", "--file", pkg},
	} {
		_, stderr, err := runRoot(t, args...)
		if err != nil {
			t.Fatalf("%v: %v (stderr %q)", args, err, stderr)
		}
		if !strings.Contains(stderr, "[dry-run]") {
			t.Errorf("%v: no [dry-run] report on stderr: %q", args, stderr)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if got := log.matching(m + " "); len(got) != 0 {
			t.Fatalf("-n sent %q", got)
		}
	}
}

func TestDryRunUploader_RefusesAnUploadWithNoPreview(t *testing.T) {
	_, err := dryRunUploader{}.Upload(t.Context(), "/v1/icon", bytes.NewReader(nil), "multipart/form-data", 0)
	if err == nil || !strings.Contains(err.Error(), "--dry-run is set") {
		t.Fatalf("Upload under -n returned %v; want a refusal", err)
	}
}

func TestSDKRetryClient_WriteIsSentOnceOn502(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client func() *http.Client
		body   string
	}{
		{"protect mutation", newProtectHTTPClient, `{"query":"mutation createGroup($input: GroupInput!) { createGroup(input: $input) { id } }"}`},
		{"school POST", newSchoolHTTPClient, `{"name":"g"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log requestLog
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.add(r.Method)
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, "upstream down")
			}))
			t.Cleanup(srv.Close)
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/graphql", strings.NewReader(tc.body))
			resp, err := tc.client().Do(req)
			if err != nil {
				t.Fatalf("want the 502 itself back, got %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				t.Errorf("status %d; want 502", resp.StatusCode)
			}
			if n := len(log.matching("")); n != 1 {
				t.Errorf("server received %d POSTs for one 502; want 1", n)
			}
		})
	}
}

func TestSDKRetryClient_WriteIsSentOnceOnAResetAfterSending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client func() *http.Client
		body   string
	}{
		{"protect mutation", newProtectHTTPClient, `{"query":"mutation { deleteGroup(id: \"1\") { id } }"}`},
		{"school POST", newSchoolHTTPClient, `{"name":"g"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log requestLog
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)
				log.add(r.Method)
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Error("server does not support hijacking")
					return
				}
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}))
			t.Cleanup(srv.Close)
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/graphql", strings.NewReader(tc.body))
			resp, err := tc.client().Do(req)
			if err == nil {
				_ = resp.Body.Close()
				t.Fatal("want a transport error")
			}
			if !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("error %q does not say the write may have been applied", err)
			}
			if n := len(log.matching("")); n != 1 {
				t.Errorf("server received %d POSTs after one reset; want 1", n)
			}
		})
	}
}

func TestSDKRetryClient_ReadsAndRefusalsAreStillRetried(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client func() *http.Client
		method string
		body   string
		first  int
	}{
		{"protect query on 502", newProtectHTTPClient, http.MethodPost, `{"query":"query listGroups { listGroups { items { id } } }"}`, http.StatusBadGateway},
		{"school GET on 502", newSchoolHTTPClient, http.MethodGet, "", http.StatusBadGateway},
		{"protect mutation on 429", newProtectHTTPClient, http.MethodPost, `{"query":"mutation { createGroup { id } }"}`, http.StatusTooManyRequests},
		{"school POST on 429", newSchoolHTTPClient, http.MethodPost, `{}`, http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log requestLog
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.add(r.Method)
				if len(log.matching("")) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(tc.first)
					return
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			t.Cleanup(srv.Close)
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req, _ := http.NewRequest(tc.method, srv.URL+"/graphql", body)
			resp, err := tc.client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || len(log.matching("")) != 2 {
				t.Errorf("status %d after %d request(s); want 200 after 2", resp.StatusCode, len(log.matching("")))
			}
		})
	}
}

func TestSDKRetryBackoff_HugeRetryAfterIsBounded(t *testing.T) {
	for _, tc := range []struct {
		status int
		header string
	}{
		{http.StatusTooManyRequests, "2147483647"},
		{http.StatusServiceUnavailable, "2147483647"},
		{http.StatusTooManyRequests, time.Now().Add(365 * 24 * time.Hour).UTC().Format(http.TimeFormat)},
	} {
		resp := &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.header}}}
		if got := sdkRetryBackoff(time.Second, 30*time.Second, 0, resp); got > client.RateLimitWaitMax {
			t.Errorf("%d with Retry-After %q waits %v; want at most %v", tc.status, tc.header, got, client.RateLimitWaitMax)
		}
	}
}

func TestIsGraphQLMutation(t *testing.T) {
	app, _ := http.NewRequest(http.MethodPost, "https://protect.example.invalid/app", nil)
	tokenReq, _ := http.NewRequest(http.MethodPost, "https://protect.example.invalid/token", nil)
	for _, tc := range []struct {
		req  *http.Request
		body string
		want bool
	}{
		{app, `{"query":"query listGroups { listGroups { items { id } } }"}`, false},
		{app, `{"query":"{ getOrganization { uuid } }"}`, false},
		{app, `{"query":"mutation createGroup($i: GroupInput!) { createGroup(input: $i) { id } }"}`, true},
		{app, `{"query":"  # a comment naming mutation\n query q { a }"}`, false},
		{app, `{"query":"query q { a(note: \"mutation\") }"}`, false},
		{app, `{"query":"query q { a(note: \"say \\\"hi\\\" mutation\") }"}`, false},
		{app, `{"query":"query q { a } mutation m { b }"}`, true},
		{app, `{"query":"subscription s { a }"}`, true},
		{app, `{"query":"query q { mutation { id } }"}`, false},
		{app, `{"query":"query q { a(subscription: 1) }"}`, false},
		{app, `{"query":"query q($x: String = \"mutation\") { a(s: [\"mutation\"]) }"}`, false},
		{app, `{"query":"query q { a(s: \"\"\" mutation { \"\"\") }"}`, false},
		{app, `{"query":"query q { a(s: \"\"\" \\\"\"\" mutation \"\"\") }"}`, false},
		{app, `{"query":"query q { a(s: \"\"\"x\"\"\") } mutation m { b }"}`, true},
		{app, `{"query":"query q { a(s: \"\"\" \\\"\"\" \"\"\") } mutation m { b }"}`, true},
		{app, `{"query":"query q { a(s: \"\"\" never closed ) } mutation m { b }"}`, true},
		{app, `{"query":"query q { a(s: \"never closed) } mutation m { b }"}`, true},
		{app, `{"query":"{ a } #x\rmutation M { b }"}`, true},
		{app, `{"query":"query q { a(s: \"x\n) } mutation m { b } # \")}"}`, true},
		{app, `{"query":"query q { a(s: \"x\r) } mutation m { b } # \")}"}`, true},
		{app, `{"query":"query q { a(s: \"\"\"\nline \"q\"\n\"\"\") }"}`, false},
		{app, `{"query":"query q { a { b } "}`, true},
		{app, `{"query":"} mutation m { b }"}`, true},
		{app, `{"client_id":"x","password":"y"}`, true},
		{app, `{"extensions":{"persistedQuery":{"sha256Hash":"abc"}}}`, true},
		{app, `[{"query":"query q { a }"}]`, true},
		{app, `not json`, true},
		{app, `{"query":42}`, true},
		{app, ``, false},
		{tokenReq, `{"client_id":"x","password":"y"}`, false},
		{nil, `{"client_id":"x","password":"y"}`, true},
	} {
		if got := isGraphQLMutation(tc.req, []byte(tc.body)); got != tc.want {
			path := "<nil>"
			if tc.req != nil {
				path = tc.req.URL.Path
			}
			t.Errorf("isGraphQLMutation(%s, %s) = %v, want %v", path, tc.body, got, tc.want)
		}
	}
}

// TestGraphQLDocumentWrites_EverySDKDocument classifies every GraphQL document
// the Protect SDK sends, so an SDK bump that adds a document this lexer reads
// the wrong way fails here rather than letting a write through -n.
func TestGraphQLDocumentWrites_EverySDKDocument(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/Jamf-Concepts/jamfprotect-go-sdk").Output()
	if err != nil {
		t.Fatalf("locating the Protect SDK module: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "jamfprotect")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("listing %s: %d files, %v", dir, len(files), err)
	}
	fset := gotoken.NewFileSet()
	exprs := map[string]ast.Expr{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != gotoken.CONST && gd.Tok != gotoken.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						exprs[name.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	var eval func(ast.Expr) (string, bool)
	eval = func(e ast.Expr) (string, bool) {
		switch e := e.(type) {
		case *ast.BasicLit:
			if e.Kind != gotoken.STRING {
				return "", false
			}
			v, err := strconv.Unquote(e.Value)
			return v, err == nil
		case *ast.BinaryExpr:
			l, lok := eval(e.X)
			r, rok := eval(e.Y)
			return l + r, lok && rok && e.Op == gotoken.ADD
		case *ast.Ident:
			if x, ok := exprs[e.Name]; ok {
				return eval(x)
			}
		case *ast.ParenExpr:
			return eval(e.X)
		}
		return "", false
	}
	writes, reads := 0, 0
	for _, name := range slices.Sorted(maps.Keys(exprs)) {
		doc, ok := eval(exprs[name])
		if !ok {
			continue
		}
		var want bool
		switch trimmed := strings.TrimSpace(doc); {
		case strings.HasPrefix(trimmed, "mutation"), strings.HasPrefix(trimmed, "subscription"):
			want, writes = true, writes+1
		case strings.HasPrefix(trimmed, "query"), strings.HasPrefix(trimmed, "{"):
			reads++
		default:
			continue
		}
		if got := graphQLDocumentWrites(doc); got != want {
			t.Errorf("SDK document %s classified write=%v, want %v", name, got, want)
		}
	}
	if writes == 0 || reads == 0 {
		t.Fatalf("found %d mutation and %d query documents in %s; the sweep checked nothing", writes, reads, dir)
	}
	t.Logf("classified %d mutation and %d query documents", writes, reads)
}

type fakeTimeout struct{}

func (fakeTimeout) Error() string   { return "i/o timeout" }
func (fakeTimeout) Timeout() bool   { return true }
func (fakeTimeout) Temporary() bool { return true }

// sentThenTimeout reports the headers written and then times out, the shape of
// a server that accepted a request and never answered.
type sentThenTimeout struct{}

func (sentThenTimeout) RoundTrip(req *http.Request) (*http.Response, error) {
	if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteHeaders != nil {
		trace.WroteHeaders()
	}
	return nil, fakeTimeout{}
}

func TestSendTracker_OnlyASentWriteIsReportedAsPossiblyApplied(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   bool
	}{
		{http.MethodGet, false},
		{http.MethodPost, true},
	} {
		tr := &sendTracker{inner: sentThenTimeout{}, isWrite: isRESTWrite}
		req, _ := http.NewRequest(tc.method, "https://school.example.invalid/api/users", nil)
		_, err := tr.RoundTrip(req)
		if err == nil {
			t.Fatalf("%s: want the timeout back", tc.method)
		}
		if got := errors.Is(err, errSentWrite); got != tc.want {
			t.Errorf("%s timing out after it was sent: errSentWrite=%v, want %v (%v)", tc.method, got, tc.want, err)
		}
	}
}

func TestSDKRetryPolicy_ATimeoutIsNeverRetried(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, "https://school.example.invalid/api/users", nil)
		err := &url.Error{Op: method, URL: req.URL.String(), Err: fakeTimeout{}}
		retry, perr := sdkRetryPolicy(t.Context(), nil, err)
		if retry || perr != nil {
			t.Errorf("%s timeout: retry=%v err=%v; want no retry", method, retry, perr)
		}
	}
}

func TestDryRun_ProCSVValidateIsStillSent(t *testing.T) {
	isolateHome(t)
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"recordCount":1}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("JAMF_URL", srv.URL)
	t.Setenv("JAMF_TOKEN", "fake-token")

	csv := writeInput(t, "preload.csv", "Serial Number,Device Type\nC02TEST,Computer\n")
	stdout, stderr, err := runRoot(t, "-n", "--no-input", "-o", "json", "pro", "inventory-preload", "csv-validate", "--file", csv)
	if err != nil {
		t.Fatalf("csv-validate under -n: %v (stderr %q)", err, stderr)
	}
	if got := log.matching("POST /api/v2/inventory-preload/csv-validate"); len(got) != 1 {
		t.Errorf("server saw %q; want one csv-validate POST", log.matching(""))
	}
	if !strings.Contains(stdout, "recordCount") {
		t.Errorf("stdout %q does not carry the validation result", stdout)
	}

	missing := filepath.Join(t.TempDir(), "absent.csv")
	if _, _, err := runRoot(t, "-n", "--no-input", "pro", "inventory-preload", "upload", "--file", missing); err == nil {
		t.Errorf("upload -n of a missing file succeeded; want the same error as without -n")
	}
}
