// Copyright 2026, Jamf Software LLC

package platform

import (
	"errors"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// ErrNotFound is returned when a resource name cannot be resolved to an ID.
//
// Callers should prefer the SDK's built-in resolver methods directly:
//
//	blueprints.New(c).ResolveBlueprintIDByName(ctx, name)
//	compliancebenchmarks.New(c).ResolveBenchmarkIDByName(ctx, name)
//	compliancebenchmarks.New(c).ResolveBaselineIDByName(ctx, name)
//	devicegroups.New(c).ResolveDeviceGroupIDByName(ctx, name)
//	devices.New(c).ResolveDeviceIDByName(ctx, name)
//	devices.New(c).ResolveDeviceIDBySerialNumber(ctx, serial)
//
// The SDK constructs subpackage clients cheaply (just a transport pointer),
// so per-call instantiation is fine. ErrNotFound stays here for use in
// platform.IsNotFound assertions across the codebase.
var ErrNotFound = errors.New("not found")

// IsNotFound reports whether err is or wraps ErrNotFound, or is a 404
// *APIResponseError from the Platform SDK (returned by Resolve* methods when
// a name lookup yields zero results).
func IsNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	if apiErr := jamfplatform.AsAPIError(err); apiErr != nil && apiErr.HasStatus(404) {
		return true
	}
	return false
}
