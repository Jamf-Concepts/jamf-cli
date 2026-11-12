// Copyright 2026, Jamf Software LLC

// Package useragent holds the one User-Agent every jamf-cli request sends, to
// every product: Jamf Pro (direct and through the platform gateway, token
// exchange included), the Platform, Protect and School SDKs, Security Cloud,
// doctor's probe and the update check.
//
// It names the real version rather than a constant. "jamf-cli/1.0" was sent by
// the Jamf Pro client for every release, which left a server-side log unable to
// say which build made a request — the thing a log is read for when a release
// is suspected of a regression. TestEveryUserAgentComesFromThisPackage refuses a
// User-Agent built anywhere else.
package useragent

import (
	"strings"
	"sync/atomic"
)

const comment = " (+https://github.com/Jamf-Concepts/jamf-cli)"

var current atomic.Value

func init() { current.Store(format("dev")) }

// Set records the running version. Called once, by NewRootCmd, before any
// client exists. A leading "v" is dropped, so a `go install` build (which takes
// the module version, v1.36.0) reads the same as a release build (1.36.0).
func Set(version string) { current.Store(format(version)) }

// String is the User-Agent header value: jamf-cli/<version> (+<repo>).
func String() string { return current.Load().(string) }

func format(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		v = "dev"
	}
	return "jamf-cli/" + v + comment
}
