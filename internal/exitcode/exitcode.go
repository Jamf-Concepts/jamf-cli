// Copyright 2026, Jamf Software LLC

package exitcode

import (
	"errors"
	"fmt"
)

// Exit codes matching README documentation.
const (
	Success          = 0
	General          = 1
	Usage            = 2
	Authentication   = 3
	NotFound         = 4
	PermissionDenied = 5
	RateLimited      = 6
	PartialFailure   = 7
)

// Error is an error that carries a specific exit code.
type Error struct {
	Code    int
	Message string
	Err     error          // optional wrapped error
	Hint    string         // optional one-line remediation, surfaced separately
	Details map[string]any // optional structured extras for the JSON envelope
}

func (e *Error) Error() string {
	if e.Err != nil && e.Message == e.Err.Error() {
		// Wrap sets Message = Err.Error(); avoid "msg: msg" stutter.
		return e.Err.Error()
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Wrap returns a new Error with the given code wrapping err.
func Wrap(code int, err error) *Error {
	return &Error{Code: code, Err: err, Message: err.Error()}
}

// New returns a new Error with the given code and message.
func New(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// CodeName returns a short string label for an exit code.
func CodeName(code int) string {
	switch code {
	case Success:
		return "success"
	case General:
		return "general"
	case Usage:
		return "usage"
	case Authentication:
		return "authentication"
	case NotFound:
		return "not_found"
	case PermissionDenied:
		return "permission_denied"
	case RateLimited:
		return "rate_limited"
	case PartialFailure:
		return "partial_failure"
	default:
		return "general"
	}
}

// CodeFrom extracts the exit code from an error. If the error contains an
// *Error anywhere in its chain, that code is returned. Otherwise returns General (1).
func CodeFrom(err error) int {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return General
}

// WithHint attaches a one-line remediation hint and returns e for chaining.
func (e *Error) WithHint(hint string) *Error { e.Hint = hint; return e }

// WithDetails attaches structured extras and returns e for chaining.
func (e *Error) WithDetails(d map[string]any) *Error { e.Details = d; return e }

// PartialOrPropagate maps a batch tally to an exit error:
//   - failed == 0                 -> nil
//   - succeeded > 0 && failed > 0 -> PartialFailure (7) carrying msg + counts
//   - succeeded == 0 && failed > 0-> firstErr's exit code (propagated), or General
func PartialOrPropagate(succeeded, failed int, firstErr error, msg string) error {
	if failed == 0 {
		return nil
	}
	if succeeded > 0 {
		return New(PartialFailure, msg).WithDetails(map[string]any{
			"succeeded": succeeded,
			"failed":    failed,
		})
	}
	if firstErr != nil {
		return Wrap(CodeFrom(firstErr), firstErr)
	}
	return New(General, msg)
}
