// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestJCDSDownloadErrorOmitsThePresignedQuery dials a loopback port nothing
// listens on, so the transport fails with a *url.Error carrying the URL.
func TestJCDSDownloadErrorOmitsThePresignedQuery(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	uri := "http://" + addr + "/bucket/f.pkg?X-Amz-Security-Token=SENT-sts&X-Amz-Signature=SENT-sig"
	_, err = jcdsStreamToFile(context.Background(), uri, filepath.Join(t.TempDir(), "f.pkg"))
	if err == nil {
		t.Fatal("download from a closed port succeeded")
	}
	if strings.Contains(err.Error(), "SENT-") {
		t.Errorf("the pre-signed query reached the error: %v", err)
	}
	if !strings.Contains(err.Error(), "/bucket/f.pkg") {
		t.Errorf("the error lost the object path: %v", err)
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		t.Errorf("the *url.Error is no longer in the chain: %v", err)
	}
}
