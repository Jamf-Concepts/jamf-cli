// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

// macOS keeps the Kerberos ticket of a bound Mac, NoMAD or Jamf Connect login in
// the system's API credential cache, which the SMB client cannot open: it reads
// only FILE caches. discoverSystemTicket copies the ticket-granting ticket into
// a private file with the system's own tool, so a signed-in user needs to set
// nothing. The file holds a live TGT, so it lives in a 0700 directory made for
// the one upload and is removed when the upload ends (kerberosCache.close).

const (
	kccPath   = "/usr/bin/kcc"
	klistPath = "/usr/bin/klist"

	// toolTimeout bounds each system tool. A hung directory service must not
	// hang an upload that would otherwise report "no credentials".
	toolTimeout = 10 * time.Second
)

// goos and runTool are vars so a test can stand in for the platform and for the
// system tools. Production never reassigns them.
var (
	goos    = runtime.GOOS
	runTool = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

// principalRealm reads the realm from klist's "Principal: user@REALM" line.
var principalRealm = regexp.MustCompile(`(?m)^\s*Principal:\s+\S+@(\S+?)\s*$`)

// systemKrb5Confs are where a krb5.conf is looked for when KRB5_CONFIG is unset,
// in order. macOS's own is the second: bound Macs rarely have an /etc/krb5.conf.
var systemKrb5Confs = []string{"/etc/krb5.conf", "/Library/Preferences/edu.mit.Kerberos"}

// close removes the private ticket copy, if there is one. It is safe on nil and
// on a cache that names a user's own file.
func (k *kerberosCache) close() {
	if k != nil && k.cleanup != nil {
		k.cleanup()
	}
}

// discoverSystemTicket returns a FILE copy of the signed-in user's
// ticket-granting ticket, or nil when there is none to copy: not macOS, no
// ticket, or a tool that fails. It never errors: "nothing found" is the
// ordinary case, and the caller then reports the missing credentials.
func discoverSystemTicket() *kerberosCache {
	if goos != "darwin" {
		return nil
	}
	dir, err := os.MkdirTemp("", "jamf-cli-krb-")
	if err != nil {
		return nil
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	file := filepath.Join(dir, "ccache")
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()

	// --fcache-version=4: the client reads format 4, and the tool's default
	// may not be it.
	if _, err := runTool(ctx, kccPath, "copy_cred_cache", "--krbtgt-only", "--fcache-version=4", "FILE:"+file); err != nil {
		cleanup()
		return nil
	}
	if st, err := os.Stat(file); err != nil || st.Size() == 0 {
		cleanup()
		return nil
	}
	_ = os.Chmod(file, 0o600)

	out, err := runTool(ctx, klistPath, "-c", "FILE:"+file)
	m := principalRealm.FindSubmatch(out)
	if err != nil || m == nil {
		cleanup()
		return nil
	}
	conf := systemKrb5Conf()
	if conf == "" {
		// No krb5.conf: name the ticket's realm and find its KDC in DNS.
		conf = filepath.Join(dir, "krb5.conf")
		body := fmt.Sprintf("[libdefaults]\n  default_realm = %s\n  dns_lookup_kdc = true\n  dns_lookup_realm = false\n", m[1])
		if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
			cleanup()
			return nil
		}
	}
	return &kerberosCache{ccacheFile: file, configFile: conf, cleanup: cleanup}
}

// systemKrb5Conf is the first krb5.conf that exists: KRB5_CONFIG, else the
// system's own.
func systemKrb5Conf() string {
	candidates := systemKrb5Confs
	if c := os.Getenv(krbConfigEnv); c != "" {
		candidates = []string{c}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
