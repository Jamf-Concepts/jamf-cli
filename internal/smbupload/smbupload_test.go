// Copyright 2026, Jamf Software LLC

package smbupload

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestValidateFileName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b.pkg", `a\b.pkg`, "../x.pkg", "x\x00.pkg", "x.pkg:stream", "a*.pkg", "a?.pkg", "a|b.pkg", "name.", "name "} {
		if ValidateFileName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateFileName("Firefox 134.0.pkg"); err != nil {
		t.Errorf("plain name refused: %v", err)
	}
}

func TestTargetValidate_NamesTheMissingPieceNotTheSecret(t *testing.T) {
	tg := Target{Host: "h", Share: "s", Username: "u", Password: "hunter2"}
	if err := (Target{Host: "h", Share: "s", Username: "u"}).Validate(); err == nil {
		t.Fatal("missing password accepted")
	}
	if err := tg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetValidate_RefusesAShareNameThatIsAPath(t *testing.T) {
	for _, share := range []string{"share/Packages", `share\Packages`, "/Users/Shared/jamfshare"} {
		tg := Target{Host: "h", Share: share, Username: "u", Password: "p"}
		if err := tg.Validate(); err == nil {
			t.Errorf("share %q accepted", share)
		}
	}
}

func TestAddr(t *testing.T) {
	if got := (Target{Host: "fs"}).Addr(); got != "fs:445" {
		t.Errorf("default port: %s", got)
	}
	if got := (Target{Host: "::1", Port: 4450}).Addr(); got != "[::1]:4450" {
		t.Errorf("ipv6: %s", got)
	}
}

// liveTarget reads a Samba share from the environment. Skipped unless
// JAMFCLI_TEST_SMB_ADDR (host:port) is set, so CI without a share passes.
func liveTarget(t *testing.T) Target {
	t.Helper()
	addr := os.Getenv("JAMFCLI_TEST_SMB_ADDR")
	if addr == "" {
		t.Skip("JAMFCLI_TEST_SMB_ADDR not set")
	}
	host, port, _ := strings.Cut(addr, ":")
	p, _ := strconv.Atoi(port)
	return Target{
		Host: host, Port: p,
		Share:    os.Getenv("JAMFCLI_TEST_SMB_SHARE"),
		Username: os.Getenv("JAMFCLI_TEST_SMB_USER"),
		Password: os.Getenv("JAMFCLI_TEST_SMB_PASSWORD"),
	}
}

func TestUpload_Live(t *testing.T) {
	tg := liveTarget(t)
	ctx := context.Background()
	body := bytes.Repeat([]byte("jamf-cli smb "), 100_000)

	n, err := Upload(ctx, tg, "live-test.pkg", bytes.NewReader(body))
	if err != nil || n != int64(len(body)) {
		t.Fatalf("upload: n=%d err=%v", n, err)
	}
	if sz, err := Size(ctx, tg, "live-test.pkg"); err != nil || sz != int64(len(body)) {
		t.Fatalf("size: %d %v", sz, err)
	}
	// Replacing truncates the prior copy.
	if _, err := Upload(ctx, tg, "live-test.pkg", bytes.NewReader(body[:1000])); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if sz, _ := Size(ctx, tg, "live-test.pkg"); sz != 1000 {
		t.Fatalf("replaced size %d", sz)
	}
	if _, err := Size(ctx, tg, "live-test.pkg"+partSuffix); err == nil {
		t.Fatal("part file left behind")
	}
}

func TestUpload_WrongPasswordDoesNotEchoIt(t *testing.T) {
	tg := liveTarget(t)
	tg.Password = "definitely-wrong-pw"
	_, err := Upload(context.Background(), tg, "x.pkg", strings.NewReader("x"))
	if err == nil {
		t.Fatal("bad password accepted")
	}
	if strings.Contains(err.Error(), tg.Password) {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestUpload_RefusesAShareWithoutAPackagesFolder_Live(t *testing.T) {
	tg := liveTarget(t)
	if v := os.Getenv("JAMFCLI_TEST_SMB_NO_PACKAGES_SHARE"); v != "" {
		tg.Share = v
	} else {
		t.Skip("JAMFCLI_TEST_SMB_NO_PACKAGES_SHARE not set")
	}
	_, err := Upload(context.Background(), tg, "x.pkg", strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), PackagesDir) {
		t.Fatalf("want a missing-Packages error, got %v", err)
	}
}

func TestKerberosTarget_NeedsADNSNameAndAConfig(t *testing.T) {
	tg := Target{Host: "10.0.0.5", Share: "s", CCacheFile: "/c", KrbConfigFile: "/k"}
	if err := tg.Validate(); err == nil || !strings.Contains(err.Error(), "DNS name") {
		t.Fatalf("IP accepted: %v", err)
	}
	tg.Host = "fs.example.test"
	if err := tg.Validate(); err != nil {
		t.Fatalf("host name refused: %v", err)
	}
	tg.KrbConfigFile = ""
	if err := tg.Validate(); err == nil {
		t.Fatal("missing krb5.conf accepted")
	}
}

func TestRemove_Live(t *testing.T) {
	tg := liveTarget(t)
	ctx := context.Background()
	if _, err := Upload(ctx, tg, "remove-me.pkg", strings.NewReader("payload")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := Remove(ctx, tg, "remove-me.pkg"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Size(ctx, tg, "remove-me.pkg"); err == nil {
		t.Fatal("file still on the share")
	}
	// Removing again is reported as absent, not as a failure to act on.
	if err := Remove(ctx, tg, "remove-me.pkg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("second remove: want ErrNotExist, got %v", err)
	}
}

func TestRemove_RefusesANameThatIsNotAPlainFile(t *testing.T) {
	tg := Target{Host: "h", Share: "s", Username: "u", Password: "p"}
	for _, bad := range []string{"", "..", "a/b.pkg", `a\b.pkg`, "x.pkg:stream"} {
		if err := Remove(context.Background(), tg, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNewKerberosCredential_MalformedConfigIsAnErrorNotAPanic(t *testing.T) {
	dir := t.TempDir()
	conf := dir + "/krb5.conf"
	if err := os.WriteFile(conf, []byte("[libdefaults]\n default_realm = A.TEST\n[realms]\n A.TEST = { kdc = 127.0.0.1:88 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := dir + "/cc"
	if err := os.WriteFile(cc, []byte("not a cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newKerberosCredential(Target{CCacheFile: cc, KrbConfigFile: conf})
	if err == nil {
		t.Fatal("malformed input accepted")
	}
}
