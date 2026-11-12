// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type stubGetter struct{ body string }

func (g stubGetter) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(g.body))}, nil
}

// TestFetchJSON_ClassicXMLPastTenMegabytes guards #470: pro backup read each
// Classic item through a silent 10 MB cut-off, so a large advanced computer
// search failed with "XML syntax error: unexpected EOF".
func TestFetchJSON_ClassicXMLPastTenMegabytes(t *testing.T) {
	const computers = 350000
	xmlBody := "<advanced_computer_search><name>big</name><computers>" +
		strings.Repeat("<computer><name>x</name></computer>", computers) +
		"</computers></advanced_computer_search>"
	if len(xmlBody) <= 10<<20 {
		t.Fatalf("fixture is %d bytes, must exceed 10 MB", len(xmlBody))
	}

	got, err := fetchJSON(context.Background(), stubGetter{body: xmlBody}, "/JSSResource/advancedcomputersearches/id/1")
	if err != nil {
		t.Fatalf("fetchJSON: %v", err)
	}
	if got == nil {
		t.Fatal("fetchJSON returned no document")
	}
}
