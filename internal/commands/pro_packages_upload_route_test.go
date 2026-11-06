// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/pro"
	"github.com/jamf/jamfplatform-go-sdk/jcdsupload"
)

type fakeCDP struct {
	point *pro.CloudDistributionPoint
	err   error
}

func (f fakeCDP) GetCloudDistributionPointV1(context.Context) (*pro.CloudDistributionPoint, error) {
	return f.point, f.err
}

func TestRouteForCdnType(t *testing.T) {
	cases := map[string]uploadRoute{
		"JAMF_CLOUD":    uploadViaJCDS,
		"jamf_cloud":    uploadViaJCDS,
		"AMAZON_S3":     uploadViaAPI,
		"AKAMAI":        uploadViaAPI,
		"RACKSPACE":     uploadViaAPI,
		"NONE":          uploadViaAPI,
		"SOMETHING_NEW": uploadViaAPI,
		"":              uploadViaAPI,
	}
	for cdn, want := range cases {
		if got := routeForCdnType(cdn); got != want {
			t.Errorf("routeForCdnType(%q) = %s, want %s", cdn, got, want)
		}
	}
}

func TestChooseUploadRoute(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		cdp     cdpReader
		want    uploadRoute
		wantErr bool
	}{
		{"instance profile", nil, uploadViaAPI, false},
		{"JAMF_CLOUD", fakeCDP{point: &pro.CloudDistributionPoint{CdnType: "JAMF_CLOUD"}}, uploadViaJCDS, false},
		{"AMAZON_S3", fakeCDP{point: &pro.CloudDistributionPoint{CdnType: "AMAZON_S3"}}, uploadViaAPI, false},
		{"failed read takes the API route", fakeCDP{err: errors.New("boom")}, uploadViaAPI, false},
		{"NONE is refused", fakeCDP{point: &pro.CloudDistributionPoint{CdnType: "NONE"}}, uploadViaAPI, true},
		{"none is refused case-insensitively", fakeCDP{point: &pro.CloudDistributionPoint{CdnType: "none"}}, uploadViaAPI, true},
		{"absent record is refused", fakeCDP{}, uploadViaAPI, true},
	}
	for _, c := range cases {
		got, err := chooseUploadRoute(ctx, c.cdp)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("%s: route = %s, want %s", c.name, got, c.want)
		}
		if c.wantErr && !errors.Is(err, errNoCloudDistributionPoint) {
			t.Errorf("%s: err = %v, want errNoCloudDistributionPoint", c.name, err)
		}
	}
}

func TestUploadPackageFileDirect_UsesFileBaseName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Tool-1.0.pkg")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := directUpload
	t.Cleanup(func() { directUpload = orig })

	var gotName, gotBody string
	directUpload = func(_ context.Context, _ jcdsupload.API, name string, body io.Reader) error {
		b, _ := io.ReadAll(body)
		gotName, gotBody = name, string(b)
		return nil
	}
	if err := uploadPackageFileDirect(context.Background(), nil, path); err != nil {
		t.Fatal(err)
	}
	if gotName != "Tool-1.0.pkg" || gotBody != "payload" {
		t.Errorf("name=%q body=%q", gotName, gotBody)
	}

	directUpload = func(context.Context, jcdsupload.API, string, io.Reader) error { return errors.New("s3 down") }
	if err := uploadPackageFileDirect(context.Background(), nil, path); err == nil {
		t.Error("want error from failed direct upload")
	}
}

// fakeRouteClient answers every request with one canned body, or an error.
type fakeRouteClient struct {
	body string
	err  error
}

func (f *fakeRouteClient) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestInstanceHasNoCloudDP(t *testing.T) {
	cases := map[string]struct {
		body string
		err  error
		want bool
	}{
		"none":             {`{"cdnType":"NONE"}`, nil, true},
		"none, any case":   {`{"cdnType":"none"}`, nil, true},
		"jamf cloud":       {`{"cdnType":"JAMF_CLOUD"}`, nil, false},
		"s3":               {`{"cdnType":"AWS_S3"}`, nil, false},
		"no field":         {`{}`, nil, false},
		"unreadable (403)": {``, errors.New("HTTP 403"), false},
	}
	for name, c := range cases {
		fake := &fakeRouteClient{body: c.body, err: c.err}
		if got := instanceHasNoCloudDP(context.Background(), fake); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}
