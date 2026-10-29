// Copyright 2026, Jamf Software LLC

// Package smbupload writes a package file to an SMB file share distribution
// point. It speaks SMB2/3 only, through a pure-Go client, so the release
// pipeline's CGO_ENABLED=0 cross-compile is unaffected. AFP and SMB1 are not
// supported and never will be.
package smbupload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2/v2"
	"github.com/hirochachacha/go-smb2/v2/auth"
)

// DefaultPort is the SMB-over-TCP port.
const DefaultPort = 445

// dialTimeout bounds the TCP connect. The transfer itself is bounded by ctx.
const dialTimeout = 30 * time.Second

// Target is one share on one file server, with the account to write it as.
// Password is never formatted into an error or a log line.
type Target struct {
	Host     string
	Port     int // 0 means DefaultPort
	Share    string
	Domain   string
	Username string
	Password string

	// CCacheFile, when set, authenticates with Kerberos from this FILE
	// credential cache and Username/Password are not used. KrbConfigFile is the
	// krb5.conf naming the realm's KDC. Kerberos needs the server's DNS name:
	// the service principal is cifs/<Host>, which an IP address cannot form.
	CCacheFile    string
	KrbConfigFile string
}

// Kerberos reports whether the target authenticates with a ticket.
func (t Target) Kerberos() bool { return t.CCacheFile != "" }

// Addr is host:port for dialing.
func (t Target) Addr() string {
	port := t.Port
	if port == 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// UNC is the share as an operator would write it, for messages.
func (t Target) UNC() string {
	return `\\` + t.Host + `\` + t.Share
}

// who names the account for a message.
func (t Target) who() string {
	if t.Kerberos() {
		return "the Kerberos ticket holder"
	}
	return fmt.Sprintf("%q", t.Username)
}

// Validate refuses a target that cannot work, before any connection.
func (t Target) Validate() error {
	switch {
	case t.Host == "":
		return errors.New("distribution point has no host")
	case t.Share == "":
		return errors.New("distribution point has no share name")
	case strings.ContainsAny(t.Share, `/\`):
		return fmt.Errorf("share name %q is a path: it must be the share's own name, as in \\\\server\\SHARE, with no folders", t.Share)
	case t.Kerberos():
		if net.ParseIP(t.Host) != nil {
			return fmt.Errorf("Kerberos needs the server's DNS name, and %q is an IP address: set the distribution point's server to the host name", t.Host)
		}
		if t.KrbConfigFile == "" {
			return errors.New("no krb5.conf for Kerberos")
		}
		return nil
	case t.Username == "":
		return errors.New("no SMB username")
	case t.Password == "":
		return errors.New("no SMB password")
	}
	return nil
}

// PackagesDir is the folder inside the share that holds packages. Jamf's
// distribution point setup says to "always use a folder named Packages to
// avoid issues with Jamf Pro being able to locate packages", and Jamf Pro and
// Jamf Admin both read and write it there: the share is the parent. It is never
// created here: a share without it is the wrong share, and creating the folder
// would hide that.
const PackagesDir = "Packages"

// partSuffix marks a file still being written.
const partSuffix = ".jamf-cli-part"

// renameAttempts and renameBackoff bound the retry of a refused rename. Vars,
// not consts, so a test can shrink them.
var (
	renameAttempts = 3
	renameBackoff  = 2 * time.Second
)

// ValidateFileName refuses a name that is not a bare file name valid on every
// server. A share root is a flat package directory and nothing here may step
// out of it. The Windows-reserved characters are refused for Samba too, so one
// rule holds for both: a colon on Windows names an alternate data stream, so
// "x.pkg:evil" would write a hidden stream rather than a package, and the
// others are not legal in an NTFS name at all.
func ValidateFileName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\:*?\"<>|\x00") ||
		strings.TrimRight(name, ". ") != name {
		return fmt.Errorf("%q is not a plain file name", name)
	}
	return nil
}

// Upload copies src into the share's Packages folder as name, replacing a file of the
// same name. It reports the bytes written. A write that has started is never
// retried: the caller sees the error and decides.
func Upload(ctx context.Context, t Target, name string, src io.Reader) (int64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	if err := ValidateFileName(name); err != nil {
		return 0, err
	}

	share, closeShare, err := mount(ctx, t)
	if err != nil {
		return 0, err
	}
	defer closeShare()

	// Written under a temporary name and renamed into place, so an interrupted
	// upload never leaves a truncated package under the name Jamf Pro serves.
	// The rename replaces an existing file in one step (ReplaceIfExists), so
	// there is no window with the package absent, and a Windows server that
	// holds the old file open refuses the rename and leaves it intact.
	if err := requirePackagesDir(ctx, share, t); err != nil {
		return 0, err
	}
	final := PackagesDir + "/" + name
	part := final + partSuffix
	cleanup := func() { _ = share.Remove(context.WithoutCancel(ctx), part) }

	dst, err := share.Create(ctx, part)
	if err != nil {
		return 0, fmt.Errorf("creating %s on %s: %w", name+partSuffix, t.UNC(), err)
	}
	n, copyErr := dst.ReadFrom(ctx, src)
	closeErr := dst.Close(ctx)
	if copyErr != nil || closeErr != nil {
		cleanup()
		return n, fmt.Errorf("writing %s to %s: %w", name, t.UNC(), errors.Join(copyErr, closeErr))
	}

	// A Windows server's antivirus or indexer can hold the new file for a
	// moment after the close and refuse the rename with a sharing violation.
	// A refused rename changed nothing, so it is safe to repeat.
	var renameErr error
	for attempt := range renameAttempts {
		if renameErr = share.Rename(ctx, part, final); renameErr == nil {
			return n, nil
		}
		if attempt < renameAttempts-1 {
			select {
			case <-ctx.Done():
			case <-time.After(renameBackoff):
			}
		}
	}
	cleanup()
	return n, fmt.Errorf("renaming %s into place on %s: %w", name, t.UNC(), renameErr)
}

// Verify proves the account signs in and the share mounts, and nothing more. It
// is what setup runs before saving a credential: a share that has no Packages
// folder yet is not a wrong password.
func Verify(ctx context.Context, t Target) error {
	if err := t.Validate(); err != nil {
		return err
	}
	_, closeShare, err := mount(ctx, t)
	if err != nil {
		return err
	}
	closeShare()
	return nil
}

// Preflight proves the target before anything is written: the account signs in,
// the share mounts and holds a Packages folder. A caller that creates a package
// record first runs this, so a wrong share or account leaves nothing behind.
func Preflight(ctx context.Context, t Target) error {
	if err := t.Validate(); err != nil {
		return err
	}
	share, closeShare, err := mount(ctx, t)
	if err != nil {
		return err
	}
	defer closeShare()
	return requirePackagesDir(ctx, share, t)
}

func requirePackagesDir(ctx context.Context, share *smb2.Share, t Target) error {
	if st, err := share.Stat(ctx, PackagesDir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s has no %s folder: Jamf Pro looks for packages there. Create it on the share, or check the distribution point's share name", t.UNC(), PackagesDir)
	}
	return nil
}

// Size reports the size of name on the share, for a post-upload check.
func Size(ctx context.Context, t Target, name string) (int64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	if err := ValidateFileName(name); err != nil {
		return 0, err
	}
	share, closeShare, err := mount(ctx, t)
	if err != nil {
		return 0, err
	}
	defer closeShare()
	st, err := share.Stat(ctx, PackagesDir+"/"+name)
	if err != nil {
		return 0, fmt.Errorf("stat %s on %s: %w", name, t.UNC(), err)
	}
	return st.Size(), nil
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// mount opens a session and mounts the share. The returned func releases both,
// and is safe to call after ctx is cancelled.
func mount(ctx context.Context, t Target) (*smb2.Share, func(), error) {
	var creds smb2.Credentials
	var closeCreds func()
	if t.Kerberos() {
		kc, err := auth.NewKerberosCredential(auth.KerberosCCache{ConfigFile: t.KrbConfigFile, File: t.CCacheFile})
		if err != nil {
			return nil, nil, fmt.Errorf("reading the Kerberos ticket cache %s: %w", t.CCacheFile, err)
		}
		creds, closeCreds = kc, func() { _ = kc.Close() }
	} else {
		domain := t.Domain
		creds, closeCreds = auth.NTLMCredential{User: t.Username, Password: t.Password, Domain: &domain}, func() {}
	}
	d := &smb2.Dialer{
		Credentials: creds,
		TransportDialer: smb2.TCPDialer{
			Port:   t.Port,
			Dialer: &net.Dialer{Timeout: dialTimeout},
		},
	}
	session, err := d.Dial(ctx, t.Host)
	if err != nil {
		closeCreds()
		return nil, nil, fmt.Errorf("signing in to %s as %s: %w", t.Addr(), t.who(), err)
	}
	share, err := session.Mount(ctx, t.Share)
	if err != nil {
		_ = session.Close()
		closeCreds()
		return nil, nil, fmt.Errorf("mounting share %s: %w", t.UNC(), err)
	}
	return share, func() {
		bg := context.WithoutCancel(ctx)
		_ = share.Unmount(bg)
		_ = session.Close()
		closeCreds()
	}, nil
}
