// Copyright 2026, Jamf Software LLC

package gateway

import (
	"fmt"
	"strings"
)

// instanceRemedy is the only thing an operator can actually do about an endpoint
// the gateway does not carry: send it to Jamf Pro directly. Nothing in this CLI
// can route around it.
const instanceRemedy = "Run it against a Jamf Pro instance directly — a profile whose url is your instance and whose auth-method is oauth2 or token."

// transitionalNote explains an endpoint that is outside the published surface
// but may still answer today. Said without a date, because none is published:
// the claim is that the surface is the contract, not that a particular build
// removes the route.
const transitionalNote = "The gateway still routes some endpoints its published API omits, so this one may answer today — that is transitional, and a workflow built on it will break without notice."

// Note renders the hint appended to a gateway 403 or 404, or "" when there is
// nothing to add.
//
// Most such requests never reach the wire: checkAPIMatch refuses the command
// pre-flight. This is for the cases that do — a hand-written command fanning out
// over many endpoints carries one annotation for the whole batch, so only the
// request itself knows which endpoint was refused.
func Note(level Level, basis Basis, detail string) string {
	if level != Unserved {
		return ""
	}
	if basis == BasisProbe {
		return fmt.Sprintf("The Jamf Platform gateway does not serve this endpoint — %s. %s", detail, instanceRemedy)
	}
	return fmt.Sprintf("This endpoint is not part of the Jamf Platform gateway's published API — %s. %s %s",
		detail, transitionalNote, instanceRemedy)
}

// NoteForRequest is Note over the table, for a concrete gateway-form request.
func NoteForRequest(method, path string) string {
	f, ok := Lookup(method, path)
	if !ok {
		return ""
	}
	return Note(f.Level, f.Basis, f.Detail)
}

// Refusal renders the pre-flight refusal. cmdPath is the invoked command, e.g.
// "pro app-installer-titles list".
//
// A probed entry states the fact. An unpublished one says the endpoint is
// outside the supported surface and that answering today is transitional —
// refusing it now is the point, because letting it through means a workflow gets
// built on a route that is going away and the failure arrives later as an
// unexplained breakage.
func Refusal(cmdPath string, basis Basis, detail string) string {
	var b strings.Builder
	if basis == BasisProbe {
		fmt.Fprintf(&b, "%s is not served by the Jamf Platform gateway", cmdPath)
	} else {
		fmt.Fprintf(&b, "%s is not part of the Jamf Platform gateway's published API", cmdPath)
	}
	if detail != "" {
		fmt.Fprintf(&b, "\n\n%s.", capitalise(detail))
	}
	// Paragraphs are left unwrapped so the terminal wraps them at its own width.
	// Hand-wrapping some and not others is what made this message ragged.
	if basis == BasisProbe {
		b.WriteString("\n\nThe gateway answers an unrouted path with 403 BAD_PERMISSIONS, which is indistinguishable from a missing API-role privilege, so the request is refused here instead of sending you after a grant that cannot help.")
	} else {
		b.WriteString("\n\n" + transitionalNote + " It is refused here rather than later, when the gateway's 403 BAD_PERMISSIONS would be indistinguishable from a missing API-role privilege.")
	}
	b.WriteString("\n\n" + instanceRemedy)
	if ProAPIVersion != "" {
		fmt.Fprintf(&b, "\n\nPublished surface: Jamf Pro API %s, Classic API %s.", ProAPIVersion, ClassicAPIVersion)
	}
	return b.String()
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
