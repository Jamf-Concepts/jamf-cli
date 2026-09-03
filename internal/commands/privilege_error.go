// Copyright 2026, Jamf Software LLC

package commands

import (
	"errors"
	"strings"

	jamfplatform "github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/privileges"
)

// statusCarrier is the platform SDK's *jamfplatform.APIResponseError, as the
// only thing needed from it. Matched by behaviour rather than by type because
// the concrete type is an alias into the SDK's internal package: nothing outside
// the SDK can construct one, so a type assertion would make this branch
// reachable only by standing up an HTTP server and driving a real SDK call.
type statusCarrier interface {
	HasStatus(int) bool
}

// detailCarrier reads the structured error details off the same SDK error, for
// the one 403 that is not a permissions problem. Also matched by behaviour, and
// for a second reason beyond statusCarrier's: ErrorDetail's fields are
// exported, so a test double can carry a real code where it cannot carry a real
// *APIResponseError.
type detailCarrier interface {
	Details() []jamfplatform.ErrorDetail
}

// gatewayOwnershipForbidden is the gateway's code for a scope-level mismatch —
// an environment ID sent for a tenant-scoped integration, or the reverse. It
// shares its status with a genuine permissions failure and shares nothing else:
// the grants are already correct and the fix is one line of the profile.
const gatewayOwnershipForbidden = "OWNERSHIP_FORBIDDEN"

// hasGatewayErrorCode reports whether err carries the named structured code.
func hasGatewayErrorCode(err error, code string) bool {
	var d detailCarrier
	if !errors.As(err, &d) {
		return false
	}
	for _, detail := range d.Details() {
		if strings.EqualFold(detail.Code, code) {
			return true
		}
	}
	return false
}

// EnrichPrivilegeError names the permission a 403 wanted, in the vocabulary of
// whichever API served the command.
//
// The two vocabularies are independent sets and neither is derivable from the
// other. A Jamf Pro instance enforces API-role privileges ("Read Categories"),
// granted in Jamf Pro. The Jamf Platform gateway enforces GA capability
// permissions (categories:read), granted in Jamf Account when the API
// integration is created; the GA consolidation folded several Jamf Pro
// privileges into one capability, and Jamf Account no longer offers the old
// names at all. So printing the wrong one sends the operator to a console where
// the grant it names does not exist.
//
// Three cases, in the order they are checked:
//
//   - A Platform command (jamf:api platform-gateway, which covers the
//     gateway-served Security Cloud commands too). Its jamf:privileges
//     annotation is already the capability vocabulary, and its error comes
//     from the platform SDK rather than internal/client, so nothing has mapped
//     it to an exit code yet.
//   - A Pro or Classic command whose 403 hint already carries a platform
//     answer: internal/client wrote it, for the request it actually sent, on a
//     gateway credential. Left alone — appending the Jamf Pro names here is the
//     bug this whole function exists to avoid.
//   - Everything else: a Pro or Classic 403 against a Jamf Pro instance, where
//     the annotation's Jamf Pro privilege names are the right answer.
func EnrichPrivilegeError(cmd *cobra.Command, err error) error {
	if cmd == nil || err == nil {
		return err
	}
	privs := cmd.Annotations["jamf:privileges"]

	if cmd.Annotations[annotationAPI] == apiPlatformGateway {
		return enrichPlatformPrivilegeError(privs, err)
	}

	if privs == "" {
		return err
	}
	var e *exitcode.Error
	if !errors.As(err, &e) || e.Code != exitcode.PermissionDenied {
		return err
	}
	if privileges.HasHint(e.Hint) {
		return err
	}
	names := strings.ReplaceAll(privs, ",", ", ")
	if e.Hint != "" {
		e.Hint = strings.TrimRight(e.Hint, ". ") + ". Required privilege(s): " + names
	} else {
		e.Hint = "Required privilege(s): " + names
	}
	return err
}

// enrichPlatformPrivilegeError maps a platform SDK 403 onto the documented
// permission-denied exit code and names the Jamf Account permissions the
// operation requires.
//
// The status has to be read off the SDK error because a platform command
// returns it untouched: unlike internal/client, the SDK has no exit-code
// mapping, so a gateway 403 arrived as a bare exit 1 — the code the README
// documents for a generic failure, on the one failure with a specific code and a
// specific remedy. Only 403 is remapped; the rest are left as they are, since
// the privilege hint is the reason to touch it at all.
//
// The annotation is used rather than a path lookup because these commands do not
// go through internal/client and their annotation is already the capability
// vocabulary, straight from the spec's x-required-privileges.
func enrichPlatformPrivilegeError(privs string, err error) error {
	var e *exitcode.Error
	if errors.As(err, &e) {
		// Already classified — a dry-run guard, a documented-status render, or
		// a pre-flight refusal. Those are not the gateway's answer.
		if e.Code == exitcode.PermissionDenied && !privileges.HasHint(e.Hint) {
			e.Hint = joinHint(e.Hint, platformPrivilegeHint(privs))
		}
		return err
	}
	var apiErr statusCarrier
	if !errors.As(err, &apiErr) || !apiErr.HasStatus(403) {
		return err
	}
	// Two unrelated causes share this status, and the permissions hint is the
	// wrong answer for one of them. OWNERSHIP_FORBIDDEN means the scope header
	// does not match the level the credential was minted at — the grants are
	// already correct, and sending the operator (or their Jamf Account admin)
	// to grant capability permissions works a remediation that cannot help.
	// platform_audit.go already inspects the code before adding its note; this
	// is the same inspection.
	if hasGatewayErrorCode(err, gatewayOwnershipForbidden) {
		return exitcode.Wrap(exitcode.PermissionDenied, err).WithHint(scopeMismatchHint())
	}
	return exitcode.Wrap(exitcode.PermissionDenied, err).WithHint(platformPrivilegeHint(privs))
}

// scopeMismatchHint names the profile field to check for an OWNERSHIP_FORBIDDEN
// 403. It deliberately does not guess which level is right: a gateway token is
// opaque and carries an empty scope, so nothing on this side can read the level
// the credential belongs to — the header is the only signal, and a mismatch is
// only discoverable by sending it.
func scopeMismatchHint() string {
	return "The credential's scope level does not match the scope header sent. " +
		"An API integration is created at one level in Jamf Account — organization, " +
		"platform environment, or tenant — and only works with that level's header. " +
		"Check environment-id / tenant-id in this profile (jamf-cli config list shows " +
		"the scope; jamf-cli config path prints the file): an organization-scoped " +
		"credential must name neither. The capability permissions are not the problem here."
}

// platformPrivilegeHint renders the capability permissions, falling back to the
// generic gateway answer when the operation declares none. An operation with no
// x-required-privileges is not an operation needing no permission: the three
// Jamf Account specs are published with theirs stripped by the SDK's build, so
// naming nothing is the honest answer and inventing names here would shadow the
// real ones the day upstream restores them.
func platformPrivilegeHint(privs string) string {
	if privs == "" {
		return privileges.GatewayFallbackHint()
	}
	scopes := strings.Split(privs, ",")
	if hint := privileges.Hint(scopes); hint != "" {
		return hint
	}
	return privileges.GatewayFallbackHint()
}

func joinHint(existing, added string) string {
	if existing == "" {
		return added
	}
	return strings.TrimRight(existing, ". ") + ". " + added
}
