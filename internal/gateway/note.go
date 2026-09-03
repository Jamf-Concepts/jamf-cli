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

// successor names the command that replaces one the gateway refuses, where the
// replacement already ships in this same binary. Curated by hand and
// deliberately tiny: the specs' own `deprecated: true` is dropped on the floor by
// the generator, so nothing derives this, and a wrong answer here is worse than
// no answer — it sends an operator to a command that is not the one they wanted.
//
// The table earns its place because the alternative remedy is disproportionate.
// instanceRemedy tells an operator to provision a second credential; for these
// three, swapping one command name is enough, and the binary they already have
// serves it on the profile they already have.
//
// It cannot be allowed to go stale, and nothing in a spec drop announces that a
// successor has moved or that a key has stopped being refused, so the guard is a
// test: internal/commands' TestGatewaySuccessorsNameCommandsTheBinaryShips fails
// when an entry's key or its replacement names a command the tree does not carry,
// and when a key is no longer refused at all.
type successor struct {
	// Command is the replacement, as a command path without the binary name.
	Command string
	// Why is a sentence fragment saying what makes it the replacement.
	Why string
}

// successors is keyed on a refused command path without the binary name. A key
// covers itself and everything beneath it, longest key first, so one entry
// answers for a whole resource's subcommands.
var successors = map[string]successor{
	// The gateway's published 11.31.0 surface withdrew 122 superseded Jamf Pro
	// endpoints. In almost every case the CLI moved onto the successor silently;
	// static computer groups are the exception, because the two versions ship
	// under two different command names.
	"pro static-computer-groups": {
		Command: "pro computer-groups-static-groups",
		Why:     "the same resource on the v3 endpoint the gateway publishes, where this command is the withdrawn v2",
	},
	// Deliberately not here: the standalone erase-device-computers and
	// remove-computer-mdm-profiles resources. Both are refused, but pro.go
	// removes them from the tree outright in favour of the hand-written
	// `pro computers-inventory erase` / `remove-mdm`, so there is no command left
	// for an operator to be refused on and nothing to point anywhere. The
	// staleness test is what established that, by failing on the entries.
}

// Successor returns the replacement for a refused command path — the full path
// as cobra reports it, binary name included, e.g.
// "jamf-cli pro static-computer-groups list". ok is false when nothing is
// curated for it, which is the normal answer.
//
// Matched by longest command-path prefix, so one entry covers every subcommand of
// a refused resource.
func Successor(cmdPath string) (command, why string, ok bool) {
	fields := strings.Fields(cmdPath)
	if len(fields) == 0 {
		return "", "", false
	}
	binary, rest := fields[0], fields[1:]
	for i := len(rest); i > 0; i-- {
		if s, found := successors[strings.Join(rest[:i], " ")]; found {
			return binary + " " + s.Command, s.Why, true
		}
	}
	return "", "", false
}

// SuccessorNote renders the successor sentence for a command path, or "" when
// nothing is curated. Shared by the refusal and by the --help caveat so the two
// cannot drift.
func SuccessorNote(cmdPath string) string {
	command, why, ok := Successor(cmdPath)
	if !ok {
		return ""
	}
	return fmt.Sprintf("Use `%s` instead — %s. It ships in this binary and is served by the gateway.", command, why)
}

// SuccessorTable returns the curated entries as refused path -> replacement
// path, both without the binary name, for the staleness test.
func SuccessorTable() map[string]string {
	out := make(map[string]string, len(successors))
	for k, v := range successors {
		out[k] = v.Command
	}
	return out
}

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
	// The successor comes before instanceRemedy because it is the cheaper fix by
	// a long way: provisioning a second credential against a Jamf Pro instance,
	// versus typing a different command name on the profile already in hand.
	if note := SuccessorNote(cmdPath); note != "" {
		b.WriteString("\n\n" + note)
		b.WriteString("\n\nFailing that, " + lowerFirst(instanceRemedy))
	} else {
		b.WriteString("\n\n" + instanceRemedy)
	}
	if ProAPIVersion != "" {
		fmt.Fprintf(&b, "\n\nPublished surface: Jamf Pro API %s, Classic API %s.", ProAPIVersion, ClassicAPIVersion)
	}
	return b.String()
}

// UnpublishedOverrideWarning renders the warning printed in place of the
// refusal when JAMF_CLI_ALLOW_UNPUBLISHED is set. It is deliberately loud and
// deliberately not silenceable: the whole point of the escape hatch is that the
// operator is told, on every affected invocation, that they are running against
// a route nobody has committed to keeping.
func UnpublishedOverrideWarning(cmdPath, detail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "warning: %s is not part of the Jamf Platform gateway's published API, and JAMF_CLI_ALLOW_UNPUBLISHED is set — sending it anyway.", cmdPath)
	if detail != "" {
		fmt.Fprintf(&b, "\n%s.", capitalise(detail))
	}
	b.WriteString("\nThe gateway routes it today; that is transitional and it will stop answering without notice, at which point the failure arrives as a bare 403 BAD_PERMISSIONS. This is a stopgap, not a supported mode — move the workflow onto a Jamf Pro instance profile.")
	return b.String()
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
