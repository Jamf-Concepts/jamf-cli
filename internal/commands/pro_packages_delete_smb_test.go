// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/smbupload"
)

// deleteStubClient answers each path with a canned status and body. A 404 comes
// back as the exitcode.NotFound error the real client raises, not a response.
type deleteStubClient struct {
	routes map[string]stubAnswer
}

type stubAnswer struct {
	status int
	body   string
}

func (c deleteStubClient) Do(_ context.Context, _, path string, _ io.Reader) (*http.Response, error) {
	a, ok := c.routes[path]
	if !ok {
		a = stubAnswer{status: http.StatusNotFound}
	}
	if a.status == http.StatusNotFound {
		return nil, exitcode.New(exitcode.NotFound, "resource not found (HTTP 404): GET "+path)
	}
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(a.body)), Header: http.Header{}}, nil
}

func TestPackageRecordGone_OnlyA404CountsAsGone(t *testing.T) {
	c := deleteStubClient{routes: map[string]stubAnswer{
		"/v1/packages/1": {status: 200, body: `{}`},
		"/v1/packages/3": {status: 500},
	}}
	if gone, err := packageRecordGone(context.Background(), c, "1"); err != nil || gone {
		t.Errorf("present record: gone=%v err=%v", gone, err)
	}
	if gone, err := packageRecordGone(context.Background(), c, "2"); err != nil || !gone {
		t.Errorf("deleted record: gone=%v err=%v", gone, err)
	}
	// A 500 says nothing about the record, so its file must stay.
	if gone, _ := packageRecordGone(context.Background(), c, "3"); gone {
		t.Error("a 500 was read as the record being gone")
	}
}

func TestPackageFile_Checked(t *testing.T) {
	for name, p := range map[string]packageFile{
		"no id":          {fileName: "a.pkg"},
		"no file":        {id: "1"},
		"path traversal": {id: "1", fileName: "../a.pkg"},
		"stream":         {id: "1", fileName: "a.pkg:evil"},
	} {
		if _, err := p.checked(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := (packageFile{id: "1", fileName: "Firefox 134.pkg"}).checked(); err != nil {
		t.Errorf("plain name refused: %v", err)
	}
}

func TestPackageFileFor_ByNameRefusesAmbiguity(t *testing.T) {
	c := deleteStubClient{routes: map[string]stubAnswer{}}
	one := `{"results":[{"id":"7","fileName":"a.pkg"}]}`
	two := `{"results":[{"id":"7","fileName":"a.pkg"},{"id":"8","fileName":"b.pkg"}]}`
	c.routes[`/v1/packages?page-size=2&filter=packageName%3D%3D%22one%22`] = stubAnswer{status: 200, body: one}
	c.routes[`/v1/packages?page-size=2&filter=packageName%3D%3D%22two%22`] = stubAnswer{status: 200, body: two}

	p, err := packageFileFor(context.Background(), c, "one", true)
	if err != nil || p.id != "7" || p.fileName != "a.pkg" {
		t.Fatalf("unique name: %+v %v", p, err)
	}
	if _, err := packageFileFor(context.Background(), c, "two", true); err == nil {
		t.Fatal("two matches accepted: the wrong file could be removed from the share")
	}
}

func TestPackageFileFor_ByID(t *testing.T) {
	c := deleteStubClient{routes: map[string]stubAnswer{"/v1/packages/5": {status: 200, body: `{"id":"5","fileName":"x.pkg"}`}}}
	p, err := packageFileFor(context.Background(), c, "5", false)
	if err != nil || p.fileName != "x.pkg" {
		t.Fatalf("%+v %v", p, err)
	}
}

// A plain delete never reads a distribution point or resolves an SMB account:
// the client below answers every request with an error, so any lookup fails.
func TestDeleteWithShareCleanup_NoFlagIsRecordOnly(t *testing.T) {
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.SetContext(context.Background())
	cliCtx := &registry.CLIContext{Client: deleteStubClient{}}

	called := false
	inner := func(*cobra.Command, []string) error { called = true; return nil }
	listed := func() ([]packageFile, error) { t.Fatal("targets listed without a share"); return nil, nil }

	if err := deleteWithShareCleanup(cliCtx, cmd, nil, "", inner, listed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("the record delete did not run")
	}
}

func TestRemoveGoneFiles_OnlyAGoneRecordLosesItsFile(t *testing.T) {
	var removed []string
	orig := shareRemove
	shareRemove = func(_ context.Context, _ smbupload.Target, name string) error {
		removed = append(removed, name)
		return nil
	}
	t.Cleanup(func() { shareRemove = orig })

	client := deleteStubClient{routes: map[string]stubAnswer{
		"/v1/packages/1": {status: http.StatusOK, body: `{}`}, // kept
		// /v1/packages/2 is absent: a 404, so gone
	}}
	targets := []packageFile{{id: "1", fileName: "kept.pkg"}, {id: "2", fileName: "gone.pkg"}}

	if err := removeGoneFiles(context.Background(), client, smbupload.Target{}, targets, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "gone.pkg" {
		t.Fatalf("removed %v, want only gone.pkg", removed)
	}
}

func TestRemoveGoneFiles_KeepsTheRecordError(t *testing.T) {
	orig := shareRemove
	shareRemove = func(context.Context, smbupload.Target, string) error { return errors.New("access denied") }
	t.Cleanup(func() { shareRemove = orig })

	recordErr := errors.New("record delete failed")
	targets := []packageFile{{id: "2", fileName: "gone.pkg"}}
	err := removeGoneFiles(context.Background(), deleteStubClient{}, smbupload.Target{}, targets, recordErr)
	if !errors.Is(err, recordErr) {
		t.Fatalf("record error lost: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "gone.pkg") {
		t.Fatalf("share failure not reported: %v", err)
	}
}

func TestSelectFileShareDP_NoShareWithoutOne(t *testing.T) {
	for name, body := range map[string]string{"cloud DP configured": `{"cdnType":"JCDS2"}`, "no cloud DP and no share": `{"cdnType":"NONE"}`} {
		client := deleteStubClient{routes: map[string]stubAnswer{"/v1/cloud-distribution-point": {status: http.StatusOK, body: body}}}
		dp, err := selectFileShareDP(context.Background(), &registry.CLIContext{Client: client}, "")
		if err != nil || dp != nil {
			t.Errorf("%s: got %v, %v; want nil, nil", name, dp, err)
		}
	}
}
