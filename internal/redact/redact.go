// Copyright 2026, Jamf Software LLC

// Package redact removes credential values from request and response bodies
// before they reach a log or a preview. It imports nothing from this module, so
// every transport and every --dry-run reporter can share one rule.
package redact

import (
	"bytes"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const placeholder = "[REDACTED]"

// CredentialFieldPaths are dotted body paths whose leaf name alone is too
// generic to judge. The generator's shared --set matcher (generator/parser)
// refuses them and Body redacts them, so the two read this one list.
//
// A disk encryption configuration's institutional keystore is the case that
// needs it: `.key` and `.data` together are the base64 `.p12` and its key
// material — the private key that decrypts every institutionally-encrypted
// FileVault volume in the fleet — while the leaf names `key` and `data` are also
// worn by `key_type` and by the base64 icon, `.ipa` and `.mobileconfig` blobs on
// six other resources, which are not credentials.
var CredentialFieldPaths = []string{
	"institutional_recovery_key.key",
	"institutional_recovery_key.data",
}

// credentialContainers are the parents of CredentialFieldPaths. Body redacts
// every leaf inside one, since a log cannot tell which sibling is the secret.
var credentialContainers = func() []string {
	var out []string
	for _, p := range CredentialFieldPaths {
		if i := strings.LastIndex(p, "."); i > 0 && !slices.Contains(out, p[:i]) {
			out = append(out, p[:i])
		}
	}
	return out
}()

// credentialWordRe names a credential anywhere in a field name. Deliberately
// generous: a body reaches the log as bytes with no schema attached, and
// over-redacting a log line costs nothing while under-redacting one writes a
// secret to stderr and into whatever collects it.
var credentialWordRe = regexp.MustCompile(`(?i)password|passwd|passphrase|secret|(?:private|encryption|recovery|signing|api)[_-]?key|service[_-]?token`)

// credentialSuffixes name a credential only as the words a field name ends
// with. Matched at the end rather than anywhere, because each of these also
// starts names that hold no secret: tokenUrl, tokenEndpointAuthMethod,
// bootstrapTokenEscrowedStatus, token_type, pinned, keystoreFileName,
// authorizationEndpoint, challengeType, signatureAlgorithm.
var credentialSuffixes = [][]string{
	{"token"},
	{"pin"},
	{"passcode"},
	{"keystore"},
	{"keystore", "bytes"},
	{"keystore", "file"},
	{"authorization"},
	{"authorization", "header"},
	{"challenge"},
	{"signature"},
	{"credential"},
	{"credentials"},
}

// numericCredentialSuffixes are the only names whose numeric value is a
// secret. A count or a length named after a credential (passwordMinLength)
// stays readable.
var numericCredentialSuffixes = []string{"pin", "passcode"}

// IsCredentialName reports whether a JSON key, XML element, plist key, form
// parameter or query parameter named name carries a credential value.
func IsCredentialName(name string) bool {
	if credentialWordRe.MatchString(name) {
		return true
	}
	lower := strings.ToLower(name)
	for _, p := range CredentialFieldPaths {
		if lower == p || strings.HasSuffix(lower, "."+p) {
			return true
		}
	}
	words := Words(name)
	for _, suffix := range credentialSuffixes {
		if len(words) >= len(suffix) && slices.Equal(words[len(words)-len(suffix):], suffix) {
			return true
		}
	}
	return false
}

func isContainer(name string) bool {
	return slices.ContainsFunc(credentialContainers, func(c string) bool { return strings.EqualFold(c, name) })
}

func isNumericCredential(name string) bool {
	words := Words(name)
	return len(words) > 0 && slices.Contains(numericCredentialSuffixes, words[len(words)-1])
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

const (
	namePattern     = `[a-zA-Z0-9_.\[\]-]+`
	jsonString      = `"(?:[^"\\]|\\.)*"`
	jsonNumber      = `-?[0-9][0-9.eE+-]*`
	jsonStringArray = `\[\s*(?:` + jsonString + `\s*(?:,\s*` + jsonString + `\s*)*)?\]`

	// A plist's angle brackets, raw, entity-escaped once or more (a Classic
	// <payloads> element), or \u-escaped (a plist inside a Go-encoded JSON
	// string). The slash may be JSON-escaped too.
	plistLT    = `(?:<|&(?:amp;)*lt;|\\u003c)`
	plistGT    = `(?:>|&(?:amp;)*gt;|\\u003e)`
	plistSlash = `\\?/`
	plistGap   = `(?:\s|\\[nrt]|&#(?:x[0-9a-fA-F]+|[0-9]+);)*`

	// A leaf's text, which may hold CDATA sections: a password containing & or
	// < is written that way, and a section opens with the < a plain run stops at.
	xmlLeafText = `(?:[^<]|(?s:<!\[CDATA\[.*?\]\]>))*`
)

var (
	// A boolean value is not matched: a switch named like a credential
	// (username_password_required) holds no secret.
	jsonFieldRe = regexp.MustCompile(`("(?P<name>` + namePattern + `)"\s*:\s*)(?P<value>` + jsonString + `|` + jsonNumber + `|` + jsonStringArray + `)`)

	jsonScalarRe = regexp.MustCompile(`("` + namePattern + `"\s*:\s*)(?:` + jsonString + `|` + jsonNumber + `|` + jsonStringArray + `)`)

	// The closing tag is matched generically rather than by backreference,
	// which RE2 does not have. That is exact for a leaf element, which is what
	// every credential field in the Classic schemas is.
	xmlElementRe = regexp.MustCompile(`<(?P<name>` + namePattern + `)(\s[^>]*)?>` + xmlLeafText + `</[^>]*>`)

	xmlLeafTextRe = regexp.MustCompile(`(<[a-zA-Z_][^<>/]*>)` + xmlLeafText + `(</)`)

	// A plist names a value in a <key> and holds it in the <string> after it,
	// so neither element's own name says anything.
	plistPairRe = regexp.MustCompile(`(?is)(` + plistLT + `key` + plistGT + `\s*(?P<name>[^<>&\\]{1,128}?)\s*` + plistLT + plistSlash + `key` + plistGT + plistGap + plistLT + `string` + plistGT + `).*?(` + plistLT + plistSlash + `string` + plistGT + `)`)

	// The token exchange is the case this exists for: the SDK's
	// clientcredentials.Config retries with AuthStyleInParams after a failed
	// first attempt, so the second request body carries client_secret. The
	// value stops at a quote, '<' or space as well as '&': none survives form
	// encoding, so meeting one means the match is a URL query inside a JSON or
	// XML value, and running on would swallow the rest of that document.
	formFieldRe = regexp.MustCompile(`(^|&)(?P<name>` + namePattern + `)=[^&"<\s]*`)

	xmlContainerRes = func() []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, c := range credentialContainers {
			q := regexp.QuoteMeta(c)
			out = append(out, regexp.MustCompile(`(?is)(<`+q+`(?:\s[^>]*)?>)(.*?)(</`+q+`\s*>)`))
		}
		return out
	}()

	jsonContainerRes = func() []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, c := range credentialContainers {
			out = append(out, regexp.MustCompile(`(?i)"`+regexp.QuoteMeta(c)+`"\s*:\s*\{`))
		}
		return out
	}()
)

// Body replaces credential values in a request or response body with
// "[REDACTED]", across the encodings this CLI sends: JSON, Classic XML, a plist
// in either, and form-encoded.
func Body(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	out := redactContainers(data)
	out = replaceNamed(plistPairRe, out, func(m [][]byte) []byte {
		return concat(m[1], []byte(placeholder), m[len(m)-1])
	})
	out = replaceNamed(jsonFieldRe, out, func(m [][]byte) []byte {
		name, value := string(m[2]), m[3]
		switch {
		case isContainer(name):
			return m[0]
		case value[0] == '[':
			if bytes.Equal(bytes.Join(bytes.Fields(value), nil), []byte("[]")) {
				return m[0]
			}
			return concat(m[1], []byte(`["`+placeholder+`"]`))
		case value[0] != '"' && !isNumericCredential(name):
			return m[0]
		}
		return concat(m[1], []byte(`"`+placeholder+`"`))
	})
	out = replaceNamed(xmlElementRe, out, func(m [][]byte) []byte {
		if isContainer(string(m[1])) {
			return m[0]
		}
		return concat([]byte("<"), m[1], m[2], []byte(">"+placeholder+"</"), m[1], []byte(">"))
	})
	out = replaceNamed(formFieldRe, out, func(m [][]byte) []byte {
		return concat(m[1], m[2], []byte("="+placeholder))
	})
	return out
}

// redactContainers redacts every leaf inside a credential container. The
// container's text-only form (inventory's
// <institutional_recovery_key>Not Present</institutional_recovery_key>) is a
// status and holds no child, so it is left alone.
func redactContainers(data []byte) []byte {
	for _, re := range xmlContainerRes {
		data = re.ReplaceAllFunc(data, func(m []byte) []byte {
			g := re.FindSubmatch(m)
			return concat(g[1], xmlLeafTextRe.ReplaceAll(g[2], []byte("${1}"+placeholder+"${2}")), g[3])
		})
	}
	for _, re := range jsonContainerRes {
		var b bytes.Buffer
		rest := data
		for {
			loc := re.FindIndex(rest)
			if loc == nil {
				break
			}
			end := loc[1] - 1 + jsonObjectLen(rest[loc[1]-1:])
			b.Write(rest[:loc[1]])
			b.Write(jsonScalarRe.ReplaceAll(rest[loc[1]:end], []byte(`${1}"`+placeholder+`"`)))
			rest = rest[end:]
		}
		b.Write(rest)
		data = b.Bytes()
	}
	return data
}

// jsonObjectLen returns the length of the object obj starts with, or len(obj)
// when it is unterminated, so a truncated body is redacted to its end.
func jsonObjectLen(obj []byte) int {
	depth, inString, escaped := 0, false, false
	for i, c := range obj {
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = c == '\\'
			inString = c != '"'
		case c == '"':
			inString = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(obj)
}

// URL returns u for a log line with every credential-named query parameter's
// value replaced, keeping the order and spelling of the rest, and any userinfo
// password masked.
func URL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.Redacted()
	}
	c := *u
	params := strings.Split(c.RawQuery, "&")
	for i, p := range params {
		k, _, _ := strings.Cut(p, "=")
		if name, err := url.QueryUnescape(k); err == nil && IsCredentialName(name) {
			params[i] = k + "=" + placeholder
		}
	}
	c.RawQuery = strings.Join(params, "&")
	return c.Redacted()
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
