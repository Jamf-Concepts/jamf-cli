// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/pro"
	"github.com/jamf/jamfplatform-go-sdk/jcdsupload"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// uploadRoute is how a package binary reaches the tenant's cloud distribution
// point. The route is a property of the distribution point, not of the file.
type uploadRoute int

const (
	// uploadViaAPI sends the binary through POST /v1/packages/{id}/upload, which
	// hands it to whichever distribution point the tenant has configured. It is
	// the only route that reaches a third-party store (Amazon S3, Akamai,
	// Rackspace), and the only one an instance (non-gateway) profile has.
	uploadViaAPI uploadRoute = iota

	// uploadViaJCDS sends the binary straight to the Jamf Cloud Distribution
	// Service's storage with the temporary credentials Jamf Pro issues, through
	// jcdsupload.Upload. The gateway and CloudFront are not in the data path, so
	// the gateway's per-stream HTTP/2 window does not throttle it. It is a
	// JCDS-only route: against any other distribution point the file lands in
	// JCDS storage and the package record never links to it.
	uploadViaJCDS
)

func (r uploadRoute) String() string {
	if r == uploadViaJCDS {
		return "jcds"
	}
	return "api"
}

// routeForCdnType picks the route for a distribution point type. Only
// JAMF_CLOUD takes the direct route, and only on a positive match: every other
// value, including one this CLI has never seen, takes the API route because
// that reaches the configured distribution point whatever it is.
func routeForCdnType(cdnType string) uploadRoute {
	if strings.EqualFold(cdnType, pro.CloudDistributionPointCdnTypeJamfCloud) {
		return uploadViaJCDS
	}
	return uploadViaAPI
}

// cdpReader is the one SDK call the route choice needs.
type cdpReader interface {
	GetCloudDistributionPointV1(ctx context.Context) (*pro.CloudDistributionPoint, error)
}

// chooseUploadRoute reports the route for the active profile, and refuses an
// upload the tenant has nowhere to put.
//
// The direct route needs the Platform SDK client, which exists only under
// gateway auth, so an instance profile (cdp nil) always takes the API route
// without a request. Under the gateway, a failed read of the distribution point
// also takes the API route: the direct route is used only when the tenant has
// confirmed JCDS, because on any other distribution point it strands the file
// in JCDS storage and the verification poll waits out its whole timeout.
//
// Only cdnType NONE is refused, and only because it is unambiguous: the tenant
// states it has no cloud distribution point, so an upload could never be
// verified and the poll would run to its timeout. The endpoint answers 200 with
// NONE for that state, never 404, so an absent record is treated the same way.
// A failed read does not refuse: the check converts a slow failure into a fast
// one, and a transient read error must not become a new failure.
func chooseUploadRoute(ctx context.Context, cdp cdpReader) (uploadRoute, error) {
	if cdp == nil {
		return uploadViaAPI, nil
	}
	point, err := cdp.GetCloudDistributionPointV1(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not read the cloud distribution point (%v); uploading through the API\n", termSafe(err))
		return uploadViaAPI, nil
	}
	if point == nil || strings.EqualFold(point.CdnType, pro.CloudDistributionPointCdnTypeNone) {
		return uploadViaAPI, errNoCloudDistributionPoint
	}
	return routeForCdnType(point.CdnType), nil
}

// errNoCloudDistributionPoint is chooseUploadRoute's refusal.
var errNoCloudDistributionPoint = errors.New("no cloud distribution point is configured: an uploaded file could never be verified. " +
	"Configure one in Settings → Server infrastructure → Cloud distribution point (or with `pro cloud-distribution-point create`), " +
	"or, for an SMB file share distribution point, name it with --distribution-point (or make one the principal), then retry")

// instanceHasNoCloudDP answers, for a profile with no Platform SDK client (an
// instance profile), whether the tenant states it has no cloud distribution
// point. chooseUploadRoute cannot ask: it reads the point through the SDK, and
// without that every upload took the API route, so a tenant with only a file
// share distribution point never found it.
//
// Only an explicit NONE answers true. A failed read, a missing privilege or an
// unexpected shape answers false and says nothing: the upload then takes the
// API route as it always did, and no new warning appears for a client that
// cannot read this endpoint.
func instanceHasNoCloudDP(ctx context.Context, client registry.HTTPClient) bool {
	data, err := fetchJSON(ctx, client, "/v1/cloud-distribution-point")
	if err != nil {
		return false
	}
	cdn, _ := data["cdnType"].(string)
	return strings.EqualFold(cdn, pro.CloudDistributionPointCdnTypeNone)
}

// proSDKFor returns the Platform SDK's Pro client, or nil when the profile is
// not on the platform gateway.
func proSDKFor(cliCtx *registry.CLIContext) *pro.Client {
	if cliCtx.PlatformSDKClient == nil {
		return nil
	}
	return pro.New(cliCtx.PlatformSDKClient)
}

// directUpload is the direct route's transfer, held in a variable so a test can
// stand in for the S3 upload jcdsupload.Upload makes, which needs live
// temporary credentials. Production never reassigns it.
var directUpload = func(ctx context.Context, api jcdsupload.API, fileName string, body io.Reader) error {
	return jcdsupload.Upload(ctx, api, fileName, body)
}

// uploadPackageFileDirect sends a local file to JCDS under the name the package
// record already carries. No record id is needed: JCDS matches the file to its
// record by name when the inventory refreshes, which is why the caller creates
// (or finds) the record by this same file name first.
func uploadPackageFileDirect(ctx context.Context, api jcdsupload.API, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening upload file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var body io.Reader = f
	var progress *uploadProgress
	if info, err := f.Stat(); err == nil {
		progress = newUploadProgress(f, info.Size())
		body = progressFile{File: f, p: progress}
	}
	err = directUpload(ctx, api, filepath.Base(filePath), body)
	if progress != nil {
		progress.finish()
	}
	if err != nil {
		return fmt.Errorf("uploading package to JCDS: %w", err)
	}
	return nil
}
