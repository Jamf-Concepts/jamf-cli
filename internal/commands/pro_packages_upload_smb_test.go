// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/keychain"
)

func TestFileShareDPFromRecord_ReadsTheConvertedXML(t *testing.T) {
	// The XML-to-map conversion yields ints for numerals, not float64.
	dp := fileShareDPFromRecord(map[string]any{
		"id": 12, "name": "smb", "ip_address": "fs.example.test", "share_name": "jamfshare",
		"share_port": 445, "is_master": true, "connection_type": "SMB",
		"workgroup_or_domain": "EXAMPLE", "read_write_username": "pkgwriter",
	})
	if dp.id != "12" || dp.port != 445 || !dp.master || dp.host != "fs.example.test" || dp.username != "pkgwriter" {
		t.Fatalf("%+v", dp)
	}
}

func TestClassicItems_PluralKeyHoldsTheArray(t *testing.T) {
	got := classicItems(map[string]any{"distribution_points": []any{
		map[string]any{"id": 1}, map[string]any{"id": 2},
	}}, "distribution_point")
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got := classicItems(map[string]any{"distribution_points": map[string]any{"id": 1}}, "distribution_point"); len(got) != 1 {
		t.Fatalf("single record: %v", got)
	}
	if classicItems(nil, "distribution_point") != nil {
		t.Fatal("nil map")
	}
}

func smb(name string, master bool, kind string) fileShareDP {
	return fileShareDP{id: "1", name: name, host: "h", share: "s", master: master, connectionType: kind}
}

func TestPickFileShareDP(t *testing.T) {
	dps := []fileShareDP{smb("afp-master", true, "AFP"), smb("smb-b", false, "SMB"), smb("smb-m", false, "SMB")}

	// The master is the default, and an AFP master is refused, not skipped.
	if _, err := pickFileShareDP(dps, ""); err == nil || !strings.Contains(err.Error(), "only SMB is supported") {
		t.Fatalf("AFP master: %v", err)
	}
	// A named SMB DP works whatever the master is.
	if dp, err := pickFileShareDP(dps, "SMB-M"); err != nil || dp.name != "smb-m" {
		t.Fatalf("named: %+v %v", dp, err)
	}
	// An AFP DP named explicitly is refused.
	if _, err := pickFileShareDP(dps, "afp-master"); err == nil {
		t.Fatal("named AFP accepted")
	}
	// An unrecognised connection type is refused the same way.
	if _, err := pickFileShareDP([]fileShareDP{smb("x", true, "NFS")}, ""); err == nil {
		t.Fatal("unknown type accepted")
	}
	// No master and no name: nothing to choose.
	if _, err := pickFileShareDP([]fileShareDP{smb("a", false, "SMB")}, ""); err != errNoFileShareDP {
		t.Fatalf("no master: %v", err)
	}
	if _, err := pickFileShareDP(dps, "missing"); err == nil {
		t.Fatal("unknown name accepted")
	}
	if _, err := pickFileShareDP([]fileShareDP{smb("d", false, "SMB"), smb("D", false, "SMB")}, "d"); err == nil {
		t.Fatal("ambiguous name accepted")
	}
}

func TestSMBCredentials_EnvironmentNeverPrompts(t *testing.T) {
	t.Setenv(smbUsernameEnv, "ci-user")
	t.Setenv(smbPasswordEnv, "s3cret")
	u, p, err := smbCredentials(io.Discard, bufio.NewReader(strings.NewReader("")), fileShareDP{username: "fromdp"}, smbStored{}, true)
	if err != nil || u != "ci-user" || p != "s3cret" {
		t.Fatalf("%q %q %v", u, p, err)
	}
}

func TestSMBCredentials_UsernameDefaultsToTheRecordButPasswordNeverDoes(t *testing.T) {
	t.Setenv(smbUsernameEnv, "")
	t.Setenv(smbPasswordEnv, "")
	_, _, err := smbCredentials(io.Discard, bufio.NewReader(strings.NewReader("")), fileShareDP{username: "fromdp", name: "d", id: "1"}, smbStored{}, true)
	if err == nil || !strings.Contains(err.Error(), "no SMB credentials") {
		t.Fatalf("--no-input with no password must fail, got %v", err)
	}
	if strings.Contains(err.Error(), "fromdp") && strings.Contains(err.Error(), "password=") {
		t.Fatal("error carries a credential")
	}
}

func TestSMBKerberos(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	conf := filepath.Join(dir, "krb5.conf")
	for _, f := range []string{cc, conf} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(krbConfigEnv, conf)
	t.Setenv(smbPasswordEnv, "")

	oldDefault := defaultCCacheFile
	defaultCCacheFile = func() string { return filepath.Join(dir, "krb5cc_default") }
	t.Cleanup(func() { defaultCCacheFile = oldDefault })

	t.Setenv(krbCCacheEnv, "")
	if k, err := smbKerberos(); k != nil || err != nil {
		t.Fatalf("no cache: %v %v", k, err)
	}
	// With KRB5CCNAME unset, a default ticket file is found, and a missing
	// krb5.conf then means "not Kerberos", not an error.
	if err := os.WriteFile(defaultCCacheFile(), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if k, err := smbKerberos(); err != nil || k == nil || k.ccacheFile != defaultCCacheFile() {
		t.Fatalf("default cache: %+v %v", k, err)
	}
	t.Setenv(krbConfigEnv, filepath.Join(dir, "absent.conf"))
	if k, err := smbKerberos(); k != nil || err != nil {
		t.Fatalf("default cache without krb5.conf: %v %v", k, err)
	}
	t.Setenv(krbConfigEnv, conf)
	if err := os.Remove(defaultCCacheFile()); err != nil {
		t.Fatal(err)
	}

	t.Setenv(krbCCacheEnv, "FILE:"+cc)
	if k, err := smbKerberos(); err != nil || k == nil || k.ccacheFile != cc || k.configFile != conf {
		t.Fatalf("file cache: %+v %v", k, err)
	}
	t.Setenv(krbCCacheEnv, cc)
	if k, _ := smbKerberos(); k == nil || k.ccacheFile != cc {
		t.Fatalf("bare path: %+v", k)
	}

	// The macOS system cache cannot be read; the error carries the remedy.
	t.Setenv(krbCCacheEnv, "API:AC3D45A0-80C2")
	if _, err := smbKerberos(); err == nil {
		t.Fatal("API cache accepted")
	}
	for _, kind := range []string{"KCM:501", "KEYRING:persistent:501"} {
		t.Setenv(krbCCacheEnv, kind)
		if _, err := smbKerberos(); err == nil {
			t.Fatalf("%s accepted", kind)
		}
	}

	t.Setenv(krbCCacheEnv, "FILE:"+filepath.Join(dir, "missing"))
	if _, err := smbKerberos(); err == nil {
		t.Fatal("missing cache accepted")
	}
	t.Setenv(krbCCacheEnv, "FILE:"+cc)
	t.Setenv(krbConfigEnv, filepath.Join(dir, "no.conf"))
	if _, err := smbKerberos(); err == nil {
		t.Fatal("missing krb5.conf accepted")
	}
}

func TestSMBTarget_KerberosDropsPassword(t *testing.T) {
	tg := smbTarget(fileShareDP{host: "fs.example.test", share: "s"}, "u", "p", &kerberosCache{ccacheFile: "/c", configFile: "/k"})
	if tg.Password != "" || tg.Username != "" || !tg.Kerberos() {
		t.Fatalf("%+v", tg)
	}
}

// smbConfigWith writes a config holding one profile and returns the keychain
// the saved password reference resolves against.
func smbConfigWith(t *testing.T, profile config.Profile) *mockKeychainStore {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{Profiles: map[string]config.Profile{"prod": profile}}); err != nil {
		t.Fatal(err)
	}
	mock := newMockKeychainStore()
	old := config.KeychainStore
	config.KeychainStore = mock
	t.Cleanup(func() { config.KeychainStore = old })
	return mock
}

func TestStoredSMBCredential_ReadsTheKeychainAndMatchesNamesLoosely(t *testing.T) {
	mock := smbConfigWith(t, config.Profile{SMBCredentials: map[string]config.SMBCredential{
		"JamfShare": {Username: "pkgwriter", Password: "keychain:jamf-cli/prod/smb-12-password"},
	}})
	mock.items["jamf-cli/prod/smb-12-password"] = "from-keychain"

	got, err := storedSMBCredential("prod", "jamfshare")
	if err != nil || !got.found || got.username != "pkgwriter" || got.password != "from-keychain" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStoredSMBCredential_NothingSavedIsNotAnError(t *testing.T) {
	smbConfigWith(t, config.Profile{})
	for _, profile := range []string{"", "prod", "no-such-profile"} {
		if got, err := storedSMBCredential(profile, "x"); err != nil || got.found {
			t.Errorf("profile %q: %+v %v", profile, got, err)
		}
	}
	// A CI runner has no config file at all.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, err := storedSMBCredential("prod", "x"); err != nil || got.found {
		t.Errorf("no config file: %+v %v", got, err)
	}
}

func TestStoredSMBCredential_AnUnreadableReferenceIsRefusedNotPrompted(t *testing.T) {
	smbConfigWith(t, config.Profile{SMBCredentials: map[string]config.SMBCredential{
		"d": {Username: "u", Password: "keychain:jamf-cli/prod/smb-1-password"},
	}})
	_, err := storedSMBCredential("prod", "d")
	if err == nil {
		t.Fatal("a reference the keychain cannot answer fell through")
	}
	if strings.Contains(err.Error(), "smb-1-password") && strings.Contains(err.Error(), "secret=") {
		t.Fatal("error carries a secret")
	}
}

func TestSMBCredentials_Precedence(t *testing.T) {
	stored := smbStored{found: true, username: "saved", password: "saved-pw"}
	dp := fileShareDP{username: "fromdp"}
	in := bufio.NewReader(strings.NewReader(""))

	t.Setenv(smbUsernameEnv, "")
	t.Setenv(smbPasswordEnv, "")
	if u, p, err := smbCredentials(io.Discard, in, dp, stored, true); err != nil || u != "saved" || p != "saved-pw" {
		t.Fatalf("saved account: %q %q %v", u, p, err)
	}
	t.Setenv(smbUsernameEnv, "ci")
	t.Setenv(smbPasswordEnv, "ci-pw")
	if u, p, err := smbCredentials(io.Discard, in, dp, stored, true); err != nil || u != "ci" || p != "ci-pw" {
		t.Fatalf("env must beat a saved account: %q %q %v", u, p, err)
	}
	// The saved username fills in for a password-only environment.
	t.Setenv(smbUsernameEnv, "")
	if u, p, err := smbCredentials(io.Discard, in, dp, stored, true); err != nil || u != "saved" || p != "ci-pw" {
		t.Fatalf("mixed: %q %q %v", u, p, err)
	}
}

func TestSMBDPNames_NamesOnlyAndSorted(t *testing.T) {
	p := config.Profile{SMBCredentials: map[string]config.SMBCredential{
		"b": {Password: "keychain:jamf-cli/p/smb-2-password"}, "a": {Password: "keychain:jamf-cli/p/smb-1-password"},
	}}
	got := smbDPNames(p)
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("%v", got)
	}
	if smbDPNames(config.Profile{}) != nil {
		t.Fatal("empty profile lists names")
	}
}

func TestSaveAndForgetSMBCredential_MergeKeepsOtherDistributionPoints(t *testing.T) {
	mock := smbConfigWith(t, config.Profile{SMBCredentials: map[string]config.SMBCredential{
		"other": {Username: "o", Password: "keychain:jamf-cli/prod/smb-7-password"},
		"mine":  {Username: "m", Password: keychain.KeychainRef("prod", smbPasswordField("12"))},
	}})
	mock.items["jamf-cli/prod/"+smbPasswordField("12")] = "pw"

	if err := forgetSMBCredential("prod", fileShareDP{id: "12", name: "mine", host: "h", share: "s"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	got := cfg.Profiles["prod"].SMBCredentials
	if _, ok := got["mine"]; ok || got["other"].Username != "o" {
		t.Fatalf("forget changed the wrong entries: %+v", got)
	}
	if _, ok := mock.items["jamf-cli/prod/"+smbPasswordField("12")]; ok {
		t.Fatal("keychain item left behind")
	}
}

func TestForgetSMBCredential_LeavesAReferenceTheUserOwns(t *testing.T) {
	smbConfigWith(t, config.Profile{SMBCredentials: map[string]config.SMBCredential{
		"mine": {Username: "m", Password: "env:THEIR_OWN_SECRET"},
	}})
	t.Setenv("THEIR_OWN_SECRET", "x")
	if err := forgetSMBCredential("prod", fileShareDP{id: "12", name: "mine"}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("THEIR_OWN_SECRET") != "x" {
		t.Fatal("touched a user's variable")
	}
}

func TestResolveSMBAuth_Precedence(t *testing.T) {
	dir := t.TempDir()
	cc, conf := filepath.Join(dir, "cc"), filepath.Join(dir, "krb5.conf")
	for _, f := range []string{cc, conf} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := defaultCCacheFile
	defaultCCacheFile = func() string { return filepath.Join(dir, "none") }
	t.Cleanup(func() { defaultCCacheFile = old })
	t.Setenv(krbCCacheEnv, "FILE:"+cc)
	t.Setenv(krbConfigEnv, conf)

	dp := fileShareDP{name: "d", id: "1", username: "fromdp"}
	saved := smbStored{found: true, username: "saved", password: "saved-pw"}
	resolve := func(st smbStored, noInput bool) (string, string, *kerberosCache, error) {
		return resolveSMBAuth(io.Discard, bufio.NewReader(strings.NewReader("")), dp, st, noInput)
	}

	// A supplied password beats a ticket.
	t.Setenv(smbUsernameEnv, "ci")
	t.Setenv(smbPasswordEnv, "ci-pw")
	if u, p, k, err := resolve(smbStored{}, true); err != nil || k != nil || u != "ci" || p != "ci-pw" {
		t.Fatalf("env password: %q %q %v %v", u, p, k, err)
	}
	// A saved account beats a ticket.
	t.Setenv(smbUsernameEnv, "")
	t.Setenv(smbPasswordEnv, "")
	if u, p, k, err := resolve(saved, true); err != nil || k != nil || u != "saved" || p != "saved-pw" {
		t.Fatalf("saved account: %q %q %v %v", u, p, k, err)
	}
	// Nothing supplied or saved: the ticket.
	if _, _, k, err := resolve(smbStored{}, true); err != nil || k == nil || k.ccacheFile != cc {
		t.Fatalf("ticket: %+v %v", k, err)
	}
	// A saved username with no password is not a credential; the ticket still wins over a prompt.
	if _, _, k, err := resolve(smbStored{found: true, username: "saved"}, true); err != nil || k == nil {
		t.Fatalf("username-only entry: %+v %v", k, err)
	}
	// No ticket, nothing supplied, no prompt allowed: an error naming the options.
	t.Setenv(krbCCacheEnv, "")
	_, _, k, err := resolve(smbStored{}, true)
	if err == nil || k != nil || !strings.Contains(err.Error(), "no SMB credentials") {
		t.Fatalf("neither: %+v %v", k, err)
	}
}
