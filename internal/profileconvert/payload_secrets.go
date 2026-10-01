// Copyright 2026, Jamf Software LLC

package profileconvert

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"howett.net/plist"
)

// RedactedPayloadValue is what a secret inside a profile payload is replaced by.
const RedactedPayloadValue = "<redacted>"

// isSecretPayloadKey reports whether a payload key holds a secret that works
// outside the profile: a Wi-Fi, EAP, VPN, account or identity password, a
// shared or client secret, or a SCEP challenge. Compared case-insensitively,
// because custom payloads do not follow Apple's casing.
func isSecretPayloadKey(key string) bool {
	k := strings.ToLower(key)
	return k == "challenge" || strings.HasSuffix(k, "password") || strings.HasSuffix(k, "secret")
}

// RedactPayloadSecrets returns profile, a configuration profile plist, with the
// value of every secret key at any depth replaced by RedactedPayloadValue, and
// the PayloadContent of a com.apple.security.pkcs12 payload too. A profile with
// no secret is returned unchanged; one with a secret is re-serialised as XML.
// It fails when profile is not a plist whose top level is a dictionary.
func RedactPayloadSecrets(profile []byte) ([]byte, error) {
	var v any
	if _, err := plist.Unmarshal(profile, &v); err != nil {
		return nil, fmt.Errorf("parsing profile plist: %w", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("profile plist is not a dictionary")
	}
	if !redactPayloadSecrets(v) {
		return profile, nil
	}
	out, err := plist.MarshalIndent(v, plist.XMLFormat, "\t")
	if err != nil {
		return nil, fmt.Errorf("re-serialising profile plist: %w", err)
	}
	return out, nil
}

func redactPayloadSecrets(v any) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		pkcs12 := isPKCS12Payload(t)
		for k, child := range t {
			if (isSecretPayloadKey(k) || pkcs12 && strings.EqualFold(k, "PayloadContent")) && isScalarSecret(child) {
				t[k] = RedactedPayloadValue
				changed = true
				continue
			}
			changed = redactPayloadSecrets(child) || changed
		}
	case []any:
		for _, child := range t {
			changed = redactPayloadSecrets(child) || changed
		}
	}
	return changed
}

func isPKCS12Payload(d map[string]any) bool {
	for k, v := range d {
		if s, ok := v.(string); ok && strings.EqualFold(k, "PayloadType") && strings.EqualFold(s, "com.apple.security.pkcs12") {
			return true
		}
	}
	return false
}

func isScalarSecret(v any) bool {
	switch t := v.(type) {
	case string:
		return t != ""
	case []byte:
		return len(t) > 0
	}
	return false
}

// RedactClassicProfilePayloads returns a Classic configuration profile body
// with RedactPayloadSecrets applied to the plist inside each <payloads>
// element, re-escaped as element text. A <payloads> that does not decode is
// replaced whole by RedactedPayloadValue. It fails when body is not well-formed
// XML, so a caller can refuse it rather than print it.
func RedactClassicProfilePayloads(body []byte) ([]byte, error) {
	type span struct {
		start, end int64
		text       string
	}
	var spans []span
	dec := xml.NewDecoder(bytes.NewReader(body))
	var (
		in, nested bool
		depth      int
		start      int64
		content    strings.Builder
	)
	for {
		before := dec.InputOffset()
		tok, err := dec.RawToken()
		if err == io.EOF {
			if depth != 0 {
				return nil, io.ErrUnexpectedEOF
			}
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if in {
				nested = true
			} else if t.Name.Local == "payloads" {
				in, nested, start = true, false, dec.InputOffset()
				content.Reset()
			}
		case xml.CharData:
			if in {
				content.Write(t)
			}
		case xml.EndElement:
			depth--
			if in && t.Name.Local == "payloads" {
				in = false
				if text, changed := redactedPayloadsText(content.String(), nested); changed {
					spans = append(spans, span{start, before, text})
				}
			}
		}
	}
	var out bytes.Buffer
	var cursor int64
	for _, sp := range spans {
		out.Write(body[cursor:sp.start])
		out.WriteString(sp.text)
		cursor = sp.end
	}
	out.Write(body[cursor:])
	return out.Bytes(), nil
}

// redactedPayloadsText is the escaped element text that replaces a <payloads>
// whose decoded text is profile, and whether it differs from what was there. A
// payloads element holding child elements, or a profile that does not decode,
// becomes the marker alone.
func redactedPayloadsText(profile string, nested bool) (string, bool) {
	if !nested && strings.TrimSpace(profile) == "" {
		return "", false
	}
	redacted := []byte(RedactedPayloadValue)
	if !nested {
		out, err := RedactPayloadSecrets([]byte(profile))
		if err == nil && string(out) == profile {
			return "", false
		}
		if err == nil {
			redacted = out
		}
	}
	var esc strings.Builder
	_ = xml.EscapeText(&esc, redacted)
	return esc.String(), true
}
