// Copyright 2026, Jamf Software LLC

// Package redact removes credential values from request and response bodies
// before they reach a log or a preview. It imports nothing from this module, so
// every transport and every --dry-run reporter can share one rule.
package redact

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const placeholder = "[REDACTED]"

// credentialWordRe names a credential anywhere in a field name. Deliberately
// generous: a body reaches the log as bytes with no schema attached, and
// over-redacting a log line costs nothing while under-redacting one writes a
// secret to stderr and into whatever collects it.
var credentialWordRe = regexp.MustCompile(`(?i)password|passwd|passphrase|secret|(?:private|encryption|recovery|signing|api)[_-]?key|service[_-]?token`)

// credentialSuffixes name a credential only as the words a field name ends
// with. Matched at the end rather than anywhere, because each of these also
// starts names that hold no secret: tokenUrl, tokenEndpointAuthMethod,
// bootstrapTokenEscrowedStatus, token_type, pinned, keystoreFileName,
// authorizationEndpoint.
var credentialSuffixes = [][]string{
	{"token"},
	{"pin"},
	{"passcode"},
	{"keystore"},
	{"keystore", "bytes"},
	{"authorization"},
	{"authorization", "header"},
}

// IsCredentialName reports whether a JSON key, XML element or form parameter
// named name carries a credential value.
func IsCredentialName(name string) bool {
	if credentialWordRe.MatchString(name) {
		return true
	}
	words := Words(name)
	for _, suffix := range credentialSuffixes {
		if len(words) >= len(suffix) && slices.Equal(words[len(words)-len(suffix):], suffix) {
			return true
		}
	}
	return false
}

// Words lowercases key and splits it into words, on "." "-" "_" "[" "]" and on
// camelCase boundaries.
//
// A boundary sits before an uppercase rune when the previous rune is lowercase
// or a digit (clientSecret -> client, secret), and also when the previous rune
// is uppercase and the NEXT is lowercase (SECRETValue -> secret, value). Without
// the second case a run of capitals swallows the word that follows it.
func Words(key string) []string {
	runes := []rune(key)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.FieldsFunc(b.String(), func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == '[' || r == ']'
	})
}

const namePattern = `[a-zA-Z0-9_.\[\]-]+`

var (
	// A string or a number is matched, since a PIN can be sent as either. A
	// boolean is not: a switch named like a credential
	// (username_password_required) holds no secret.
	jsonFieldRe = regexp.MustCompile(`("(?P<name>` + namePattern + `)"\s*:\s*)(?:"(?:[^"\\]|\\.)*"|-?[0-9][0-9.eE+-]*)`)

	// The text run is [^<]* and the closing tag is matched generically rather
	// than by backreference, which RE2 does not have. That is exact for a leaf
	// element, which is what every credential field in the Classic schemas is.
	xmlElementRe = regexp.MustCompile(`<(?P<name>` + namePattern + `)(\s[^>]*)?>[^<]*</[^>]*>`)

	// The token exchange is the case this exists for: the SDK's
	// clientcredentials.Config retries with AuthStyleInParams after a failed
	// first attempt, so the second request body carries client_secret. The
	// value stops at a quote, '<' or space as well as '&': none survives form
	// encoding, so meeting one means the match is a URL query inside a JSON or
	// XML value, and running on would swallow the rest of that document.
	formFieldRe = regexp.MustCompile(`(^|&)(?P<name>` + namePattern + `)=[^&"<\s]*`)
)

// Body replaces credential values in a request or response body with
// "[REDACTED]", across the three encodings this CLI sends: JSON, Classic XML and
// form-encoded.
func Body(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	out := replaceNamed(jsonFieldRe, data, func(m [][]byte) []byte {
		return concat(m[1], []byte(`"`+placeholder+`"`))
	})
	out = replaceNamed(xmlElementRe, out, func(m [][]byte) []byte {
		return concat([]byte("<"), m[1], m[2], []byte(">"+placeholder+"</"), m[1], []byte(">"))
	})
	out = replaceNamed(formFieldRe, out, func(m [][]byte) []byte {
		return concat(m[1], m[2], []byte("="+placeholder))
	})
	return out
}

// replaceNamed rewrites each match of re whose "name" group names a credential,
// leaving every other match byte-identical.
func replaceNamed(re *regexp.Regexp, data []byte, rewrite func(groups [][]byte) []byte) []byte {
	name := re.SubexpIndex("name")
	return re.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := re.FindSubmatch(match)
		if groups == nil || !IsCredentialName(string(groups[name])) {
			return match
		}
		return rewrite(groups)
	})
}

func concat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}
