// Copyright 2026, Jamf Software LLC

package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

// Environment variables that carry the SMB account for a non-interactive run.
// The account is a service account on a file server, so it follows the machine
// credential rule: an environment variable for CI, a prompt for a person, and
// never a flag, stdin or a plain value in the config file.
const (
	smbUsernameEnv = "JAMFSMB_USERNAME"
	smbPasswordEnv = "JAMFSMB_PASSWORD"

	// Kerberos reads the same variables every Kerberos client does, so a
	// ticket from kinit, a bound Mac's login or Jamf Connect needs no setting
	// of ours.
	krbCCacheEnv = "KRB5CCNAME"
	krbConfigEnv = "KRB5_CONFIG"
)

// fileShareDP is a Classic file share distribution point, reduced to what an
// upload needs. Jamf Pro never returns the account's password (the record
// carries a masked hash), so it is not a field here.
type fileShareDP struct {
	id, name       string
	host, share    string
	domain         string
	username       string
	connectionType string
	port           int
	master         bool
}

func (d fileShareDP) isSMB() bool { return strings.EqualFold(d.connectionType, "SMB") }

// label names the DP in a message. It is server-supplied text, so it is
// sanitised for a terminal.
func (d fileShareDP) label() string {
	return fmt.Sprintf("%q (id %s)", termSafe(d.name), termSafe(d.id))
}

// errNoFileShareDP is returned when a file share distribution point was needed
// and none could be chosen.
var errNoFileShareDP = errors.New("no file share distribution point is configured")

// listFileShareDPs reads every Classic file share distribution point. The
// list endpoint returns only id and name, so each is read for its own record;
// a distribution point count is small.
func listFileShareDPs(ctx context.Context, client registry.HTTPClient) ([]fileShareDP, error) {
	list, err := fetchJSON(ctx, client, "/JSSResource/distributionpoints")
	if err != nil {
		return nil, fmt.Errorf("listing distribution points: %w", err)
	}
	var out []fileShareDP
	for _, item := range classicItems(list, "distribution_point") {
		id := scalarString(item["id"])
		if id == "" {
			continue
		}
		rec, err := fetchJSON(ctx, client, "/JSSResource/distributionpoints/id/"+url.PathEscape(id))
		if err != nil {
			return nil, fmt.Errorf("reading distribution point %s: %w", termSafe(id), err)
		}
		if inner, ok := rec["distribution_point"].(map[string]any); ok {
			rec = inner
		}
		out = append(out, fileShareDPFromRecord(rec))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].master && !out[j].master })
	return out, nil
}

// classicItems finds the repeated element in a Classic list converted from
// XML. The converter collapses the wrapper, so the records sit directly under
// the plural name ({"distribution_points": [...]}); a single record may be an
// object, and a hand-built map may carry the singular name.
func classicItems(m map[string]any, element string) []map[string]any {
	for _, key := range []string{element + "s", element} {
		if v, ok := m[key]; ok {
			return toMaps(v)
		}
	}
	return nil
}

func toMaps(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return []map[string]any{x}
	case []any:
		var out []map[string]any
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func fileShareDPFromRecord(rec map[string]any) fileShareDP {
	port, _ := strconv.Atoi(scalarString(rec["share_port"]))
	return fileShareDP{
		id:             scalarString(rec["id"]),
		name:           scalarString(rec["name"]),
		host:           scalarString(rec["ip_address"]),
		share:          scalarString(rec["share_name"]),
		domain:         scalarString(rec["workgroup_or_domain"]),
		username:       scalarString(rec["read_write_username"]),
		connectionType: scalarString(rec["connection_type"]),
		port:           port,
		master:         strings.EqualFold(scalarString(rec["is_master"]), "true"),
	}
}

// pickFileShareDP chooses the distribution point to upload to: the one named,
// else the master. AFP is refused outright. It is deprecated by Apple and has
// no client here, so it is never selected and never offered as a fallback;
// the same holds for any connection type this CLI does not recognise.
func pickFileShareDP(dps []fileShareDP, name string) (fileShareDP, error) {
	var chosen *fileShareDP
	if name != "" {
		for i := range dps {
			if strings.EqualFold(dps[i].name, name) {
				if chosen != nil {
					return fileShareDP{}, exitcode.New(exitcode.Usage,
						fmt.Sprintf("two distribution points are named %q: rename one in Jamf Pro", termSafe(name)))
				}
				chosen = &dps[i]
			}
		}
		if chosen == nil {
			return fileShareDP{}, exitcode.New(exitcode.Usage, fmt.Sprintf("no distribution point named %q", termSafe(name))).
				WithHint("list them with `jamf-cli pro classic-distribution-points list`")
		}
	} else {
		for i := range dps {
			if dps[i].master {
				chosen = &dps[i]
				break
			}
		}
		if chosen == nil {
			return fileShareDP{}, errNoFileShareDP
		}
	}
	if !chosen.isSMB() {
		return fileShareDP{}, exitcode.New(exitcode.Usage,
			fmt.Sprintf("distribution point %s uses the %q protocol, and only SMB is supported", chosen.label(), termSafe(chosen.connectionType))).
			WithHint("name an SMB distribution point with --distribution-point, or upload through the API to a cloud distribution point")
	}
	if chosen.host == "" || chosen.share == "" {
		return fileShareDP{}, exitcode.New(exitcode.Usage,
			fmt.Sprintf("distribution point %s has no server address or share name", chosen.label()))
	}
	return *chosen, nil
}

// smbCredentials resolves the account for dp: the environment first, then the
// account saved in the profile by `smb-setup`, then a prompt. The username defaults to the one on the distribution point record,
// which is not a secret; the password is never defaulted and is never printed.
// With --no-input there is no prompt, and the error names the variables.
func smbCredentials(w io.Writer, reader *bufio.Reader, dp fileShareDP, stored smbStored, noPrompt bool) (user, password string, err error) {
	user = os.Getenv(smbUsernameEnv)
	if user == "" {
		user = stored.username
	}
	if user == "" {
		user = dp.username
	}
	password = os.Getenv(smbPasswordEnv)
	if password == "" {
		password = stored.password
	}
	if password != "" && user != "" {
		return user, password, nil
	}

	if noPrompt || !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", exitcode.New(exitcode.Usage,
			"no SMB credentials for distribution point "+dp.label()+": none supplied, none saved, and no Kerberos ticket").
			WithHint("set " + smbUsernameEnv + " and " + smbPasswordEnv + ", save an account with `jamf-cli pro packages smb-setup`, or get a ticket (kinit, with " + krbCCacheEnv + " naming a FILE cache)")
	}

	_, _ = fmt.Fprintf(w, "Enter the account that can write to %s\n", termSafe(`\\`+dp.host+`\`+dp.share))
	if user == "" {
		_, _ = fmt.Fprint(w, "Username: ")
		line, rerr := reader.ReadString('\n')
		if rerr != nil && line == "" {
			return "", "", fmt.Errorf("reading username: %w", rerr)
		}
		user = strings.TrimSpace(line)
		if user == "" {
			return "", "", errors.New("username is required")
		}
	} else {
		_, _ = fmt.Fprintf(w, "Username: %s\n", termSafe(user))
	}
	if password == "" {
		_, _ = fmt.Fprint(w, "Password: ")
		b, perr := term.ReadPassword(int(os.Stdin.Fd()))
		if perr != nil {
			return "", "", fmt.Errorf("reading password: %w", perr)
		}
		_, _ = fmt.Fprintln(w)
		password = string(b)
		if password == "" {
			return "", "", errors.New("password is required")
		}
	}
	return user, password, nil
}

// kerberosCache is a ticket cache and the krb5.conf to use with it.
type kerberosCache struct{ ccacheFile, configFile string }

// defaultCCacheFile is where a Linux kinit puts a ticket when KRB5CCNAME is
// unset. A var so a test can point it elsewhere. macOS keeps its tickets in the
// system API cache instead, which this client cannot open, so there is no file
// to find there.
var defaultCCacheFile = func() string { return fmt.Sprintf("/tmp/krb5cc_%d", os.Getuid()) }

// smbKerberos reports the Kerberos ticket cache to authenticate with, or nil
// when there is none. The caller asks only after a password has been ruled out:
// an explicit or saved account always beats a ticket, and a ticket beats a
// prompt.
//
// KRB5CCNAME names the cache when set, and a value that cannot be used is an
// error rather than a fall-through, since it is an explicit choice that would
// otherwise be ignored. When it is unset, the default FILE cache is used if it
// exists.
//
// Only a FILE cache can be read. macOS keeps the ticket of a bound Mac, NoMAD
// or Jamf Connect login in the system's API cache, which this client cannot
// open, so an explicit API: or KCM: name is refused with the one command that
// copies it out.
func smbKerberos() (*kerberosCache, error) {
	name := os.Getenv(krbCCacheEnv)
	explicit := name != ""
	if !explicit {
		name = defaultCCacheFile()
		if _, err := os.Stat(name); err != nil {
			return nil, nil //nolint:nilerr // no default ticket file is the ordinary case
		}
	}
	if kind, _, ok := strings.Cut(name, ":"); ok && kind != "FILE" && !strings.HasPrefix(name, "/") {
		return nil, exitcode.New(exitcode.Usage,
			fmt.Sprintf("%s names a %q credential cache, and only a FILE cache can be read", krbCCacheEnv, termSafe(kind))).
			WithHint("copy the ticket to a file: `kcc copy_cred_cache --krbtgt-only FILE:/tmp/jamf-cli.ccache`, then set " +
				krbCCacheEnv + "=FILE:/tmp/jamf-cli.ccache")
	}
	file := strings.TrimPrefix(name, "FILE:")
	if _, err := os.Stat(file); err != nil {
		return nil, exitcode.New(exitcode.Usage, fmt.Sprintf("%s names %s, which cannot be read: %v", krbCCacheEnv, termSafe(file), err))
	}
	conf := os.Getenv(krbConfigEnv)
	if conf == "" {
		conf = "/etc/krb5.conf"
	}
	if _, err := os.Stat(conf); err != nil {
		if !explicit {
			// A default ticket file with no krb5.conf is not a request for
			// Kerberos; it must not turn a prompt or a clear "no credentials"
			// error into a krb5.conf complaint.
			return nil, nil //nolint:nilerr // see above
		}
		return nil, exitcode.New(exitcode.Usage, fmt.Sprintf("Kerberos needs a krb5.conf, and %s cannot be read: %v", termSafe(conf), err)).
			WithHint("set " + krbConfigEnv + " to the file that names your realm's KDC")
	}
	return &kerberosCache{ccacheFile: file, configFile: conf}, nil
}

// resolveSMBAuth decides how to sign in to dp. In order:
//
//  1. a password in JAMFSMB_PASSWORD,
//  2. the account saved in the profile by `smb-setup`,
//  3. a Kerberos ticket,
//  4. a prompt, on a terminal and without --no-input; otherwise an error.
//
// Anything the caller supplied or saved beats a ticket, because it is an
// explicit choice and a ticket is merely present. A ticket beats a prompt.
func resolveSMBAuth(w io.Writer, reader *bufio.Reader, dp fileShareDP, stored smbStored, noInput bool) (user, password string, krb *kerberosCache, err error) {
	if os.Getenv(smbPasswordEnv) == "" && stored.password == "" {
		if krb, err = smbKerberos(); err != nil || krb != nil {
			return "", "", krb, err
		}
	}
	user, password, err = smbCredentials(w, reader, dp, stored, noInput)
	return user, password, nil, err
}

// smbTarget joins the distribution point record and the resolved account.
func smbTarget(dp fileShareDP, user, password string, krb *kerberosCache) smbupload.Target {
	t := smbupload.Target{
		Host:     dp.host,
		Port:     dp.port,
		Share:    dp.share,
		Domain:   dp.domain,
		Username: user,
		Password: password,
	}
	if krb != nil {
		t.Username, t.Password = "", ""
		t.CCacheFile, t.KrbConfigFile = krb.ccacheFile, krb.configFile
	}
	return t
}

// uploadPackageFileSMB writes a local file to the share and confirms the size
// that landed. The size check is the whole of the verification: the bytes never
// pass through Jamf Pro, so it computes no hash to compare against.
func uploadPackageFileSMB(ctx context.Context, t smbupload.Target, filePath string, want int64) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening upload file: %w", err)
	}
	defer func() { _ = f.Close() }()

	name := filepath.Base(filePath)
	if _, err := smbupload.Upload(ctx, t, name, f); err != nil {
		return fmt.Errorf("uploading package to %s: %w", termSafe(t.UNC()), err)
	}
	got, err := smbupload.Size(ctx, t, name)
	if err != nil {
		return fmt.Errorf("checking the uploaded file: %w", err)
	}
	if got != want {
		return fmt.Errorf("%s holds %d bytes of %s, expected %d", termSafe(t.UNC()), got, termSafe(name), want)
	}
	return nil
}

// resolveFileShareDP reads the distribution points and picks one.
func resolveFileShareDP(ctx context.Context, client registry.HTTPClient, name string) (fileShareDP, error) {
	dps, err := listFileShareDPs(ctx, client)
	if err != nil {
		return fileShareDP{}, err
	}
	return pickFileShareDP(dps, name)
}
