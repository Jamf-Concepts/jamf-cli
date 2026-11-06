// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hirochachacha/go-smb2/v2/auth"
)

// fakeTools stands in for kcc and klist. kcc writes ccache into the FILE: path
// it is given; klist prints the principal.
func fakeTools(t *testing.T, kccErr error, kccWrites bool, klistOut string) {
	t.Helper()
	oldRun, oldOS := runTool, goos
	goos = "darwin"
	runTool = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case kccPath:
			if kccErr != nil {
				return nil, kccErr
			}
			if kccWrites {
				dest := strings.TrimPrefix(args[len(args)-1], "FILE:")
				if err := os.WriteFile(dest, []byte("ticket"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			return nil, nil
		case klistPath:
			return []byte(klistOut), nil
		}
		t.Fatalf("unexpected tool %s", name)
		return nil, nil
	}
	t.Cleanup(func() { runTool, goos = oldRun, oldOS })
}

const klistSample = "Credentials cache: FILE:/x\n        Principal: jdoe@CORP.EXAMPLE.COM\n\n  Issued   Expires   Principal\n"

func noSystemKrb5Conf(t *testing.T) {
	t.Helper()
	old := systemKrb5Confs
	systemKrb5Confs = []string{filepath.Join(t.TempDir(), "absent")}
	t.Setenv(krbConfigEnv, "")
	t.Cleanup(func() { systemKrb5Confs = old })
}

func TestDiscoverSystemTicket_CopiesPrivatelyAndGeneratesAConfigFromTheRealm(t *testing.T) {
	noSystemKrb5Conf(t)
	fakeTools(t, nil, true, klistSample)

	k := discoverSystemTicket()
	if k == nil {
		t.Fatal("no ticket found")
	}
	dir := filepath.Dir(k.ccacheFile)
	if st, _ := os.Stat(k.ccacheFile); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("ticket copy mode = %v, want 0600", st)
	}
	if st, _ := os.Stat(dir); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("ticket directory mode = %v, want 0700", st)
	}
	conf, err := os.ReadFile(k.configFile)
	if err != nil || !strings.Contains(string(conf), "default_realm = CORP.EXAMPLE.COM") || !strings.Contains(string(conf), "dns_lookup_kdc = true") {
		t.Errorf("generated krb5.conf: %q %v", conf, err)
	}
	k.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("ticket directory survives close: %v", err)
	}
}

func TestDiscoverSystemTicket_UsesTheSystemKrb5ConfWhenThereIsOne(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "krb5.conf")
	if err := os.WriteFile(conf, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(krbConfigEnv, conf)
	fakeTools(t, nil, true, klistSample)

	k := discoverSystemTicket()
	if k == nil || k.configFile != conf {
		t.Fatalf("%+v", k)
	}
	k.close()
	if _, err := os.Stat(conf); err != nil {
		t.Fatal("close removed the user's krb5.conf")
	}
}

func TestDiscoverSystemTicket_NothingFoundLeavesNothingBehind(t *testing.T) {
	noSystemKrb5Conf(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	for name, run := range map[string]func(){
		"no ticket":         func() { fakeTools(t, os.ErrNotExist, false, klistSample) },
		"empty copy":        func() { fakeTools(t, nil, false, klistSample) },
		"no principal line": func() { fakeTools(t, nil, true, "Credentials cache: x\n") },
	} {
		run()
		if k := discoverSystemTicket(); k != nil {
			t.Errorf("%s: found %+v", name, k)
		}
		if left, _ := os.ReadDir(tmp); len(left) != 0 {
			t.Errorf("%s: left %d entries in the temp dir", name, len(left))
		}
	}
}

func TestDiscoverSystemTicket_OnlyOnMacOS(t *testing.T) {
	fakeTools(t, nil, true, klistSample)
	goos = "linux"
	if k := discoverSystemTicket(); k != nil {
		t.Fatalf("linux ran the macOS tools: %+v", k)
	}
}

func TestCloseIsNilSafe(t *testing.T) {
	var k *kerberosCache
	k.close()
	(&kerberosCache{ccacheFile: "/mine"}).close()
}

// TestDiscoverSystemTicket_Live runs the real kcc and klist against the ticket
// the signed-in macOS user holds, and loads the copy with the SMB client's own
// Kerberos reader. Skipped unless JAMFCLI_TEST_KRB_LIVE=1, since it needs a
// ticket in the system cache (kinit, a bound Mac or Jamf Connect).
func TestDiscoverSystemTicket_Live(t *testing.T) {
	if os.Getenv("JAMFCLI_TEST_KRB_LIVE") != "1" {
		t.Skip("JAMFCLI_TEST_KRB_LIVE not set")
	}
	k := discoverSystemTicket()
	if k == nil {
		t.Fatal("no ticket found; run kinit first")
	}
	defer k.close()
	if st, err := os.Stat(k.ccacheFile); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("copy: %v %v", st, err)
	}
	cred, err := auth.NewKerberosCredential(auth.KerberosCCache{ConfigFile: k.configFile, File: k.ccacheFile})
	if err != nil {
		t.Fatalf("the SMB client cannot read the copy: %v", err)
	}
	_ = cred.Close()
}
