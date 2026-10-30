// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
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
