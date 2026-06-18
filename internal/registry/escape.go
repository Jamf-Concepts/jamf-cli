// Copyright 2026, Jamf Software LLC

package registry

import (
	"net/url"
	"strings"
)

// EscapeClassicPathSegment percent-encodes s for use as a single path segment
// in a Jamf classic API URL — typically the {name} in
// /JSSResource/<resource>/name/{name}.
//
// It deliberately does not use url.PathEscape. PathEscape leaves "+" literal
// because "+" is a valid path character per RFC 3986, but the classic API
// form-decodes "+" to a space inside the path, so a name such as
// "Aged 5+ Years" never matches (the server ends up looking up "Aged 5  Years"
// and returns 404).
//
// url.QueryEscape percent-encodes "+" (as %2B) along with every other reserved
// character; it only differs from what we want by emitting "+" for spaces, so
// we rewrite those back to %20. The result is a fully percent-encoded segment
// that round-trips correctly whether the server decodes it as a path or as a
// form value — verified live against the classic API for "+ & = : @ $ ( ) , ;
// ' % ? #", spaces and non-ASCII names.
//
// Caveat: a "/" in a name encodes to %2F, which the classic API rejects (HTTP
// 400). Such names cannot be looked up through the /name/ endpoint at all and
// must be resolved by id; no encoding can work around that.
func EscapeClassicPathSegment(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
