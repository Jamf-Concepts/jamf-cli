// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

type proSetCredentialServer struct {
	mu    sync.Mutex
	calls []string
	body  string
}

func (s *proSetCredentialServer) handler(getBody string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet {
			s.body += string(b)
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, getBody)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}
}

func (s *proSetCredentialServer) writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		if !strings.HasPrefix(c, http.MethodGet+" ") {
			out = append(out, c)
		}
	}
	return out
}

func isolateProSetCredentialEnv(t *testing.T, serverURL string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("JAMF_URL", serverURL)
	t.Setenv("JAMF_TOKEN", "fake-bearer-token")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_CLI_NO_UPDATE_CHECK", "1")
	t.Setenv("JAMF_CLI_ARGS", "")
	restoreOutputFlags(t)
}

// TestProSetRefusesACredentialField holds the generated Pro update and patch
// commands to the credential policy Classic --set already enforces: a
// credential value on argv reaches shell history, ps and CI logs, so it must be
// refused before any write.
func TestProSetRefusesACredentialField(t *testing.T) {
	const secret = "FAKE-S3cret-value"
	cases := []struct {
		name    string
		args    []string
		getBody string
	}{
		{"computer-prestages update recoveryLockPassword", []string{"pro", "computer-prestages", "update", "12", "--set", "recoveryLockPassword=" + secret}, `{"id":"12","displayName":"Lab","versionLock":1}`},
		{"computer-prestages update accountSettings.adminPassword", []string{"pro", "computer-prestages", "update", "12", "--set", "accountSettings.adminPassword=" + secret}, `{"id":"12","displayName":"Lab","versionLock":1}`},
		{"gsx-connection update token", []string{"pro", "gsx-connection", "update", "--set", "token=" + secret}, `{"enabled":true,"username":"svc"}`},
		{"gsx-connection patch gsxKeystore.keystorePassword", []string{"pro", "gsx-connection", "patch", "--set", "gsxKeystore.keystorePassword=" + secret}, `{}`},
		{"smtp-server update graphApiCredentials.clientSecret", []string{"pro", "smtp-server", "update", "--set", "graphApiCredentials.clientSecret=" + secret}, `{"enabled":true}`},
		{"gsx-connection patch gsxKeystore as a JSON object", []string{"pro", "gsx-connection", "patch", "--set", `gsxKeystore={"keystorePassword":"` + secret + `"}`}, `{}`},
		{"venafi patch refreshToken", []string{"pro", "venafi", "patch", "3", "--set", "refreshToken=" + secret}, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &proSetCredentialServer{}
			ts := httptest.NewServer(srv.handler(tc.getBody))
			t.Cleanup(ts.Close)
			isolateProSetCredentialEnv(t, ts.URL)

			root := NewRootCmd("test", "none", "none", "none")
			root.SetArgs(append(append([]string{}, tc.args...), "--no-input"))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			var err error
			_ = captureStdout(t, func() {
				_ = captureStderr(t, func() { err = root.Execute() })
			})

			t.Logf("err=%v calls=%v", err, srv.calls)
			if err == nil || !strings.Contains(err.Error(), "--from-file") {
				t.Errorf("jamf-cli %s: err = %v; want a credential refusal pointing at --from-file", strings.Join(tc.args, " "), err)
			}
			if w := srv.writes(); len(w) > 0 {
				t.Errorf("credential --set reached the server: %v", w)
			}
			if strings.Contains(srv.body, secret) {
				t.Errorf("the argv secret was sent in the request body: %s", srv.body)
			}
		})
	}
}

// TestProWriteOnlyWarningNeverRecommendsArgv pins the second half: the
// write-only-field warning must not tell the operator to put the secret on the
// command line.
func TestProWriteOnlyWarningNeverRecommendsArgv(t *testing.T) {
	srv := &proSetCredentialServer{}
	ts := httptest.NewServer(srv.handler(`{"id":"12","displayName":"Lab","versionLock":1}`))
	t.Cleanup(ts.Close)
	isolateProSetCredentialEnv(t, ts.URL)

	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs([]string{"pro", "computer-prestages", "update", "12", "--set", "department=Lab", "--no-input"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	var stderr string
	_ = captureStdout(t, func() {
		stderr = captureStderr(t, func() { _ = root.Execute() })
	})

	for _, field := range []string{"recoveryLockPassword", "accountSettings.adminPassword"} {
		if strings.Contains(stderr, "--set "+field+"=") {
			t.Errorf("stderr recommends passing %s on argv:\n%s", field, stderr)
		}
	}
	if !strings.Contains(stderr, "pipe the whole record with the field included on stdin") {
		t.Errorf("stderr does not give the stdin remedy:\n%s", stderr)
	}
}

// TestProSetCompletionOffersNoCredentialField keeps credential fields out of
// --set shell completion, as Classic already does.
func TestProSetCompletionOffersNoCredentialField(t *testing.T) {
	isolateProSetCredentialEnv(t, "https://example.invalid")
	root := NewRootCmd("test", "none", "none", "none")
	for _, tc := range []struct {
		path  []string
		field string
	}{
		{[]string{"pro", "computer-prestages", "update"}, "recoveryLockPassword="},
		{[]string{"pro", "gsx-connection", "update"}, "token="},
		{[]string{"pro", "venafi", "patch"}, "refreshToken="},
	} {
		cmd, _, err := root.Find(tc.path)
		if err != nil {
			t.Fatalf("%v: %v", tc.path, err)
		}
		fn, ok := cmd.GetFlagCompletionFunc("set")
		if !ok {
			t.Fatalf("%v: no --set completion registered", tc.path)
		}
		got, _ := fn(cmd, nil, "")
		for _, c := range got {
			if c == tc.field {
				t.Errorf("%v: --set completion offers %q", tc.path, tc.field)
			}
		}
	}

	cmd, _, err := root.Find([]string{"pro", "computer-prestages", "update"})
	if err != nil {
		t.Fatal(err)
	}
	fn, _ := cmd.GetFlagCompletionFunc("set")
	if got, _ := fn(cmd, nil, ""); !slices.Contains(got, "department=") {
		t.Errorf("--set completion no longer offers the settable department=; got %v", got)
	}
}

// TestProSetRefusesACredentialObjectLedByWhitespace holds the refusal to the
// decoder the body builder uses: parseJSONSetValue skips leading JSON
// whitespace and tolerates a trailing closer, so a value the builder sends as
// an object must be inspected as one, whatever its first byte.
func TestProSetRefusesACredentialObjectLedByWhitespace(t *testing.T) {
	const secret = "FAKE-S3cret-value"
	oldNoInput := noInput
	t.Cleanup(func() { noInput = oldNoInput })
	for _, lead := range []string{" ", "\t", "\n", "\r\n"} {
		for _, tc := range []struct {
			name    string
			args    []string
			getBody string
		}{
			{"smtp-server update graphApiCredentials", []string{"pro", "smtp-server", "update", "--set", "graphApiCredentials=" + lead + `{"clientId":"id","clientSecret":"` + secret + `"}`}, `{"enabled":true}`},
			{"gsx-connection patch gsxKeystore", []string{"pro", "gsx-connection", "patch", "--set", "gsxKeystore=" + lead + `{"keystorePassword":"` + secret + `"}`}, `{}`},
		} {
			t.Run(tc.name+" "+strconvQuote(lead), func(t *testing.T) {
				srv := &proSetCredentialServer{}
				ts := httptest.NewServer(srv.handler(tc.getBody))
				t.Cleanup(ts.Close)
				isolateProSetCredentialEnv(t, ts.URL)

				root := NewRootCmd("test", "none", "none", "none")
				root.SetArgs(append(append([]string{}, tc.args...), "--no-input"))
				root.SetOut(io.Discard)
				root.SetErr(io.Discard)
				var err error
				_ = captureStdout(t, func() {
					_ = captureStderr(t, func() { err = root.Execute() })
				})

				if err == nil || !strings.Contains(err.Error(), "is a credential") {
					t.Errorf("err = %v; want a credential refusal", err)
				}
				if strings.Contains(srv.body, secret) {
					t.Errorf("the argv secret was sent in the request body: %s", srv.body)
				}
			})
		}
	}
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }

// TestProSetRefusesAPrivateKeyBlob covers the PKCS#12 fields whose leaf name
// says nothing: an ADCS or DigiCert clientCert.data and a cloud LDAP
// keystore's fileBytes carry the client's private key. serverCert.data shares
// the ADCS schema and holds a public certificate, so it must still be sent.
func TestProSetRefusesAPrivateKeyBlob(t *testing.T) {
	const secret = "FAKE-P12-blob"
	oldNoInput := noInput
	t.Cleanup(func() { noInput = oldNoInput })
	run := func(t *testing.T, args []string, getBody string) (*proSetCredentialServer, error) {
		srv := &proSetCredentialServer{}
		ts := httptest.NewServer(srv.handler(getBody))
		t.Cleanup(ts.Close)
		isolateProSetCredentialEnv(t, ts.URL)
		root := NewRootCmd("test", "none", "none", "none")
		root.SetArgs(append(append([]string{}, args...), "--no-input"))
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		var err error
		_ = captureStdout(t, func() {
			_ = captureStderr(t, func() { err = root.Execute() })
		})
		return srv, err
	}
	for _, tc := range []struct {
		name    string
		args    []string
		getBody string
	}{
		{"adcs-settings patch clientCert.data", []string{"pro", "adcs-settings", "patch", "1", "--set", `clientCert.data=["` + secret + `"]`}, `{}`},
		{"digicert patch clientCert as a JSON object", []string{"pro", "digicert", "patch", "1", "--set", `clientCert={"filename":"c.p12","data":["` + secret + `"]}`}, `{}`},
		{"cloud-ldap update server.keystore.fileBytes", []string{"pro", "cloud-ldap", "update", "1", "--set", "server.keystore.fileBytes=" + secret}, `{"cloudIdPCommon":{"displayName":"x"},"server":{"enabled":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := run(t, tc.args, tc.getBody)
			if err == nil || !strings.Contains(err.Error(), "is a credential") {
				t.Errorf("err = %v; want a credential refusal", err)
			}
			if w := srv.writes(); len(w) > 0 {
				t.Errorf("credential --set reached the server: %v", w)
			}
			if strings.Contains(srv.body, secret) {
				t.Errorf("the argv blob was sent in the request body: %s", srv.body)
			}
		})
	}
	t.Run("adcs-settings patch serverCert.data stays settable", func(t *testing.T) {
		srv, err := run(t, []string{"pro", "adcs-settings", "patch", "1", "--set", `serverCert.data=["PUBLIC-cert"]`}, `{}`)
		if err != nil {
			t.Fatalf("serverCert.data refused: %v", err)
		}
		if !strings.Contains(srv.body, "PUBLIC-cert") {
			t.Errorf("serverCert.data was not sent; body %q, calls %v", srv.body, srv.calls)
		}
	})
}
