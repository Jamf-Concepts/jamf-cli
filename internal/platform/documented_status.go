// Copyright 2026, Jamf Software LLC

package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

// DocumentedStatus names a non-2xx response that an operation documents as a
// *result* rather than a failure: the status the server answers, and the error
// code its body must carry for that reading to apply.
//
// The error code is part of the match on purpose. Allowing a bare status would
// mean any 404 on the endpoint renders as success, including one caused by a
// path or tenant mistake, and a wrong answer with a zero exit code is the
// failure mode that is hardest to notice.
type DocumentedStatus struct {
	Code      int
	ErrorCode string
	// Empty marks a status that means "this is not configured" rather than
	// carrying an answer in its body. The response is an error envelope
	// ({httpStatus, traceId, errors}), not the shape the endpoint returns when
	// the setting exists, so rendering it would make one command emit two
	// unrelated schemas depending on state — and its traceId changes per call,
	// so the output would not even be stable between two identical reads.
	// An empty object is rendered instead: same schema family, deterministic,
	// and every field the caller would read comes back null.
	Empty bool
}

// DoExpectDocumented performs a request whose failure statuses include some the
// endpoint documents as answers, and renders their body as the result instead of
// letting them become an exit-code error.
//
// Jamf Security Cloud's DNS search domain is the case this exists for: the
// tenant either has a search domain or it does not, and "does not" is answered
// as 404 SEARCH_DOMAIN_NOT_SET. Reported as an error, the ordinary empty state
// of a singleton settings endpoint exited 1 and printed a traceId, so a script
// asking "is a search domain configured?" could not tell "no" from "the request
// broke".
//
// Mirrors documentedStatusResults in the Jamf Pro generator, which does the same
// job through registry.WithAllowedStatuses; the platform transport has no
// allowed-status hook, so the mapping happens here on the way back out.
func DoExpectDocumented(
	ctx context.Context,
	client *jamfplatform.Client,
	method, path string,
	body any,
	expectedStatus int,
	documented []DocumentedStatus,
	result any,
) error {
	err := client.Transport().DoExpect(ctx, method, path, body, expectedStatus, result)
	if err == nil {
		return nil
	}
	var apiErr *jamfplatform.APIResponseError
	if !errors.As(err, &apiErr) {
		return err
	}
	match, ok := matchesDocumented(apiErr, documented)
	if !ok {
		return err
	}
	return renderDocumented(apiErr, match, result)
}

// renderDocumented turns a documented non-2xx response into the caller's result.
//
// Split from DoExpectDocumented so the rendering decision — which is the part
// with a judgement call in it — is testable without a live gateway.
func renderDocumented(apiErr *jamfplatform.APIResponseError, match DocumentedStatus, result any) error {
	if result == nil {
		return nil
	}
	if match.Empty {
		// Nothing is configured. Render an empty object rather than the error
		// envelope the server used to say so.
		if out, ok := result.(*any); ok {
			*out = map[string]any{}
		}
		return nil
	}
	// The documented body is the answer. Decode it into the caller's result so
	// it renders through the normal formatter; an undecodable body is not worth
	// failing over, since the status match already established what happened.
	if apiErr.Body != "" {
		_ = json.Unmarshal([]byte(apiErr.Body), result)
	}
	return nil
}

// matchesDocumented returns the documented status the error corresponds to —
// same status, and carrying the expected error code — and whether one matched.
func matchesDocumented(apiErr *jamfplatform.APIResponseError, documented []DocumentedStatus) (DocumentedStatus, bool) {
	for _, d := range documented {
		if !apiErr.HasStatus(d.Code) {
			continue
		}
		if d.ErrorCode == "" {
			return d, true
		}
		for _, e := range apiErr.Errors {
			if e.Code == d.ErrorCode {
				return d, true
			}
		}
	}
	return DocumentedStatus{}, false
}
