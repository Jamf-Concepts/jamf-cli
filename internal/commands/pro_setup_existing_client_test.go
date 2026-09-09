// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

// oauth2TokenServer serves the client-credentials exchange, accepting exactly
// one pair. Everything else answers 401, which is what a Jamf Pro instance
// returns for a wrong client ID or secret.
func oauth2TokenServer(t *testing.T, wantID, wantSecret string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/api/oauth/token") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.PostForm.Get("client_id") != wantID || r.PostForm.Get("client_secret") != wantSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "verified-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	}))
}

func TestSetupInstanceWithExistingClient_StoresVerifiedPair(t *testing.T) {
	server := oauth2TokenServer(t, "byo-cid", "byo-csec")
	defer server.Close()

	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cfg := &config.Config{Profiles: make(map[string]config.Profile)}
	var out bytes.Buffer

	if err := setupInstanceWithExistingClient(context.Background(), &out, cfg, server.URL, "byo-cid", "byo-csec", "byo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := mock.items[keychain.DefaultService+"/byo/client-id"]; got != "byo-cid" {
		t.Errorf("keychain client-id = %q, want %q", got, "byo-cid")
	}
	if got := mock.items[keychain.DefaultService+"/byo/client-secret"]; got != "byo-csec" {
		t.Errorf("keychain client-secret = %q, want %q", got, "byo-csec")
	}

	p := cfg.Profiles["byo"]
	if p.AuthMethod != "oauth2" {
		t.Errorf("AuthMethod = %q, want oauth2", p.AuthMethod)
	}
	if p.URL != server.URL {
		t.Errorf("URL = %q, want %q", p.URL, server.URL)
	}
	if want := keychain.KeychainRef("byo", "client-id"); p.ClientID != want {
		t.Errorf("ClientID = %q, want %q", p.ClientID, want)
	}
	if want := keychain.KeychainRef("byo", "client-secret"); p.ClientSecret != want {
		t.Errorf("ClientSecret = %q, want %q", p.ClientSecret, want)
	}
}

// A pair that does not authenticate must leave nothing behind. Saving first and
// verifying afterwards would give the operator a profile on disk that fails on
// every command, which is the failure this path exists to prevent.
func TestSetupInstanceWithExistingClient_BadPairWritesNothing(t *testing.T) {
	server := oauth2TokenServer(t, "byo-cid", "byo-csec")
	defer server.Close()

	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	defer func() { config.KeychainStore = old }()

	cfg := &config.Config{Profiles: make(map[string]config.Profile)}
	var out bytes.Buffer

	err := setupInstanceWithExistingClient(context.Background(), &out, cfg, server.URL, "byo-cid", "wrong-secret", "byo")
	if err == nil {
		t.Fatal("expected an error for a secret the server rejects")
	}
	if len(mock.items) != 0 {
		t.Errorf("keychain written despite verification failure: %v", mock.items)
	}
	if _, ok := cfg.Profiles["byo"]; ok {
		t.Error("profile written despite verification failure")
	}
}

func TestPromptCredentialSource(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"enter takes the default", "\n", credentialSourceExisting},
		{"first option", "1\n", credentialSourceExisting},
		{"second option", "2\n", credentialSourceCreate},
		{"padded", "  2  \n", credentialSourceCreate},
		{"out of range falls back to the default", "9\n", credentialSourceExisting},
		{"garbage falls back to the default", "banana\n", credentialSourceExisting},
		{"eof falls back to the default", "", credentialSourceExisting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := promptCredentialSource(&out, newTestReader(tc.input))
			if got != tc.want {
				t.Errorf("promptCredentialSource(%q) = %q, want %q", tc.input, got, tc.want)
			}
			// Both options must be listed, or the default is undiscoverable.
			for _, opt := range credentialSourceOptions {
				if !strings.Contains(out.String(), opt.displayName) {
					t.Errorf("prompt omits option %q:\n%s", opt.displayName, out.String())
				}
			}
		})
	}
}

// --scope and --rotate-credentials describe an API role and client that
// "--credentials existing" does not create. Refusing beats ignoring: the
// operator set --scope believing it constrained their credential.
func TestProSetup_ExistingClientRefusesRoleCreationFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "scope",
			args: []string{"--url", "https://example.jamfcloud.com", "--credentials", "existing", "--scope", "read-only"},
			want: "--scope cannot be used with --credentials existing",
		},
		{
			name: "rotate-credentials",
			args: []string{"--url", "https://example.jamfcloud.com", "--credentials", "existing", "--rotate-credentials"},
			want: "--rotate-credentials cannot be used with --credentials existing",
		},
		{
			name: "unknown source",
			args: []string{"--url", "https://example.jamfcloud.com", "--credentials", "byo"},
			want: "invalid --credentials",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newConfigSetupCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected an error, got none; output:\n%s", out.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// The deprecation note has to stay accurate on the three points the loose
// reading gets wrong, since it is the only place a user is told any of them.
func TestJamfProAuthDeprecationNoteIsSpecific(t *testing.T) {
	for _, want := range []string{
		"cloud-hosted",
		"second half of 2027",
		"self-hosted instances are not affected",
		"learn.jamf.com",
	} {
		if !strings.Contains(strings.ToLower(jamfProAuthDeprecationNote), strings.ToLower(want)) {
			t.Errorf("deprecation note omits %q:\n%s", want, jamfProAuthDeprecationNote)
		}
	}
	// It must not claim the removal is imminent, which is what "a future
	// release" reads as next to an estimated date three releases of Jamf Pro
	// a year away.
	if strings.Contains(jamfProAuthDeprecationNote, "next release") {
		t.Error("deprecation note overstates the timeline")
	}
}

func newTestReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }
