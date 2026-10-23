// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHashFile checks the single-pass hasher against the standard library.
func TestHashFile(t *testing.T) {
	content := []byte("jamf-cli package upload test payload")
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pkg")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := hashFile(path)
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}

	s3 := sha3.Sum512(content)
	s2 := sha256.Sum256(content)
	m5 := md5.Sum(content)
	if want := hex.EncodeToString(s3[:]); got.sha3 != want {
		t.Errorf("sha3 = %s, want %s", got.sha3, want)
	}
	if want := hex.EncodeToString(s2[:]); got.sha256 != want {
		t.Errorf("sha256 = %s, want %s", got.sha256, want)
	}
	if want := hex.EncodeToString(m5[:]); got.md5 != want {
		t.Errorf("md5 = %s, want %s", got.md5, want)
	}
}

// pkgVerifyServer mocks the package GET + refresh-inventory endpoints used by
// verifyPackageUpload. It serves a sequence of hashValue strings (one per GET
// poll, the last repeating) so a test can model the server taking several
// polls to recompute the hash after an upload.
type pkgVerifyServer struct {
	srv      *httptest.Server
	hashSeq  []string
	pollN    atomic.Int32 // GET count
	lastBody string
}

func newPkgVerifyServer(hashSeq []string) *pkgVerifyServer {
	p := &pkgVerifyServer{hashSeq: hashSeq}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/cloud-distribution-point/refresh-inventory", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/packages/702", func(w http.ResponseWriter, _ *http.Request) {
		i := int(p.pollN.Add(1)) - 1
		if i >= len(p.hashSeq) {
			i = len(p.hashSeq) - 1
		}
		hv := p.hashSeq[i]
		p.lastBody = fmt.Sprintf(`{"id":"702","fileName":"x.pkg","hashType":"SHA3_512","hashValue":%q,"sha3512":%q,"size":"123"}`, hv, hv)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(p.lastBody))
	})
	p.srv = httptest.NewServer(mux)
	return p
}

func (p *pkgVerifyServer) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := p.srv.URL + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, body)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func (p *pkgVerifyServer) close() { p.srv.Close() }

// withFastVerifyPoll shrinks the verify timing so the polling logic can be
// exercised in milliseconds, restoring the originals on cleanup.
func withFastVerifyPoll(t *testing.T) {
	t.Helper()
	origTimeout, origInterval := verifyTimeout, verifyInterval
	verifyTimeout = 2 * time.Second
	verifyInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		verifyTimeout = origTimeout
		verifyInterval = origInterval
	})
}

func TestVerifyPackageUpload(t *testing.T) {
	const (
		newHash = "aaaa1111"
		oldHash = "bbbb2222"
		badHash = "cccc3333"
	)

	tests := []struct {
		name         string
		hashSeq      []string
		previousHash string
		wantErr      string
		wantHashVal  string // expected hashValue in returned record on success
	}{
		{
			name:        "matches on first poll",
			hashSeq:     []string{newHash},
			wantHashVal: newHash,
		},
		{
			name:         "waits out server recompute then matches",
			hashSeq:      []string{oldHash, oldHash, oldHash, newHash},
			previousHash: oldHash,
			wantHashVal:  newHash,
		},
		{
			name:         "genuine mismatch fails fast",
			hashSeq:      []string{badHash},
			previousHash: oldHash,
			wantErr:      "hash mismatch",
		},
		{
			name:        "empty hash then populated",
			hashSeq:     []string{"", "", newHash},
			wantHashVal: newHash,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFastVerifyPoll(t)
			srv := newPkgVerifyServer(tc.hashSeq)
			defer srv.close()

			data, err := verifyPackageUpload(context.Background(), srv, "702", "x.pkg", fileHashes{sha3: newHash}, previousHashes{sha3: tc.previousHash})

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if data == nil {
				t.Fatal("expected non-nil verified record")
			}
			if got, _ := data["hashValue"].(string); got != tc.wantHashVal {
				t.Errorf("returned hashValue = %q, want %q", got, tc.wantHashVal)
			}
		})
	}
}

// TestVerifyPackageUpload_S3MD5Only models an S3 distribution point: the server
// reports hashType MD5, computes only md5, and never fills sha3512. Verification
// must succeed on the MD5 once the transfer is READY and must not wait for a
// SHA3-512 that will never arrive.
func TestVerifyPackageUpload_S3MD5Only(t *testing.T) {
	const (
		localMD5 = "11111111111111111111111111111111"
		oldMD5   = "22222222222222222222222222222222"
		badMD5   = "33333333333333333333333333333333"
	)
	tests := []struct {
		name    string
		bodies  []string
		prev    previousHashes
		wantErr string
	}{
		{
			name:   "matches once READY",
			bodies: []string{`{"hashType":"MD5","md5":"` + localMD5 + `","cloudTransferStatus":"READY","sha3512":""}`},
		},
		{
			name: "waits while PENDING then matches",
			bodies: []string{
				`{"hashType":"MD5","md5":"","cloudTransferStatus":"PENDING"}`,
				`{"hashType":"MD5","md5":"` + localMD5 + `","cloudTransferStatus":"READY"}`,
			},
		},
		{
			name: "waits out the previous file's md5",
			bodies: []string{
				`{"hashType":"MD5","md5":"` + oldMD5 + `","cloudTransferStatus":"READY"}`,
				`{"hashType":"MD5","md5":"` + localMD5 + `","cloudTransferStatus":"READY"}`,
			},
			prev: previousHashes{md5: oldMD5},
		},
		{
			name:    "different md5 fails fast",
			bodies:  []string{`{"hashType":"MD5","md5":"` + badMD5 + `","cloudTransferStatus":"READY"}`},
			prev:    previousHashes{md5: oldMD5},
			wantErr: "hash mismatch",
		},
		{
			name:    "ERROR transfer fails",
			bodies:  []string{`{"hashType":"MD5","md5":"","cloudTransferStatus":"ERROR"}`},
			wantErr: "ERROR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFastVerifyPoll(t)
			var n atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/cloud-distribution-point/refresh-inventory", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("/v1/packages/702", func(w http.ResponseWriter, _ *http.Request) {
				i := min(int(n.Add(1))-1, len(tc.bodies)-1)
				_, _ = w.Write([]byte(tc.bodies[i]))
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()
			srv := &pkgVerifyServer{srv: ts}

			_, err := verifyPackageUpload(context.Background(), srv, "702", "x.pkg", fileHashes{sha3: "unused", md5: localMD5}, tc.prev)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestVerifyPackageUpload_StrongestHashDecides checks the preference order: the
// strongest populated hash is the one compared, and hashType/hashValue are
// ignored.
func TestVerifyPackageUpload_StrongestHashDecides(t *testing.T) {
	local := fileHashes{sha3: "s3", sha256: "s2", md5: "m5"}
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"sha256 alone", `{"sha256":"s2","md5":"","hashType":"MD5"}`, ""},
		{"sha3512 wins over a wrong md5", `{"sha3512":"s3","md5":"wrong"}`, ""},
		{"wrong sha3512 fails despite matching md5", `{"sha3512":"wrong","md5":"m5"}`, "hash mismatch"},
		{"hashValue ignored", `{"hashType":"SHA3_512","hashValue":"junk","md5":"m5"}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFastVerifyPoll(t)
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/cloud-distribution-point/refresh-inventory", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("/v1/packages/702", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			_, err := verifyPackageUpload(context.Background(), &pkgVerifyServer{srv: ts}, "702", "x.pkg", local, previousHashes{})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
