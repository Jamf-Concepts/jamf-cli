// Copyright 2026, Jamf Software LLC

package registry

import (
	"context"
	"errors"
)

// ErrServerTimeout marks a request the server did not answer in time: the
// client's response-header timeout fired, or an edge in front of Jamf Pro
// answered 504 on its behalf. The CloudFront edge in front of the platform
// gateway gives up after 90 seconds (issue 392).
//
// For a page of a walk it is a property of how much work the request asked
// for, so re-sending the identical page is the wrong response: the walk asks
// for less instead — see ShrinkAfterTimeout and WithPageWalk. Any other GET
// has nothing smaller to ask for, so the client retries a 504 on it with
// backoff before surfacing this error. A client-side timeout is never retried.
var ErrServerTimeout = errors.New("the server did not answer in time")

// MinShrunkPageSize is the smallest page a fetch-everything walk shrinks to
// after a timed-out page. A page this small that still times out is not a
// page-size problem, and another halving would only lengthen the wait before
// the error.
const MinShrunkPageSize = 100

// ShrinkAfterTimeout returns the page size to retry a page at after it failed
// with err, or false when err is not a server timeout or pageSize is already
// at the floor — the caller then returns err as it would have.
//
// Halving, not a fixed step: on issue 392 a 2000-row page of computer
// inventory hit the edge's 90s limit where 1000 rows answered in 45s, so one
// halving is usually enough and the walk keeps as much of each round trip as
// the server can afford.
func ShrinkAfterTimeout(err error, pageSize int) (int, bool) {
	if !errors.Is(err, ErrServerTimeout) || pageSize <= MinShrunkPageSize {
		return pageSize, false
	}
	return max(pageSize/2, MinShrunkPageSize), true
}

// PageAt returns the page index holding the record at offset for a walk of
// pageSize rows per page, and how many leading rows of that page were already
// fetched and must be skipped.
//
// A walk that shrinks its page size mid-collection resumes from the rows it
// already holds rather than from the page number it was on: page 3 at 2000 is
// rows 6000–7999, and page 3 at 1000 is rows 3000–3999. skip is non-zero only
// when pageSize does not divide offset, which halving an odd page size (a
// --page-size 750 walk shrinks to 375, then 187) produces.
func PageAt(offset, pageSize int) (page, skip int) {
	return offset / pageSize, offset % pageSize
}

type pageWalkKey struct{}

// WithPageWalk marks ctx as carrying one page of a walk that answers a server
// timeout with ShrinkAfterTimeout. The client then does not re-send that page
// on a 504: the identical page asks for the work that just failed to finish in
// time, and the walk has a request that can succeed — a smaller page. A page
// already at MinShrunkPageSize is not marked, because the walk will not shrink
// it, so it keeps the 504 retry every other GET gets.
func WithPageWalk(ctx context.Context, pageSize int) context.Context {
	if pageSize <= MinShrunkPageSize {
		return ctx
	}
	return context.WithValue(ctx, pageWalkKey{}, true)
}

// IsPageWalk reports whether ctx was marked by WithPageWalk.
func IsPageWalk(ctx context.Context) bool {
	v, _ := ctx.Value(pageWalkKey{}).(bool)
	return v
}
