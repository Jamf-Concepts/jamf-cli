package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeFrom_ExitCodeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", New(Usage, "bad flags"), Usage},
		{"auth error", New(Authentication, "bad creds"), Authentication},
		{"not found", New(NotFound, "missing"), NotFound},
		{"permission denied", New(PermissionDenied, "forbidden"), PermissionDenied},
		{"rate limited", New(RateLimited, "slow down"), RateLimited},
		{"general", New(General, "oops"), General},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeFrom(tt.err); got != tt.want {
				t.Errorf("CodeFrom() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCodeFrom_WrappedError(t *testing.T) {
	inner := New(NotFound, "resource missing")
	wrapped := fmt.Errorf("outer context: %w", inner)

	if got := CodeFrom(wrapped); got != NotFound {
		t.Errorf("CodeFrom(wrapped) = %d, want %d", got, NotFound)
	}
}

func TestCodeFrom_PlainError(t *testing.T) {
	err := errors.New("plain error")
	if got := CodeFrom(err); got != General {
		t.Errorf("CodeFrom(plain) = %d, want %d (General)", got, General)
	}
}

func TestError_Error(t *testing.T) {
	e := New(Authentication, "invalid token")
	if e.Error() != "invalid token" {
		t.Errorf("Error() = %q, want %q", e.Error(), "invalid token")
	}
}

func TestError_ErrorWithWrapped(t *testing.T) {
	inner := errors.New("connection refused")
	e := Wrap(General, inner)
	want := "connection refused"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestError_ErrorWithDistinctMessage(t *testing.T) {
	inner := errors.New("connection refused")
	e := &Error{Code: General, Message: "network error", Err: inner}
	want := "network error: connection refused"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestError_Unwrap(t *testing.T) {
	inner := errors.New("root cause")
	e := &Error{Code: General, Message: "wrapper", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("expected Unwrap to expose inner error")
	}
}

func TestCodeName(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{Success, "success"},
		{General, "general"},
		{Usage, "usage"},
		{Authentication, "authentication"},
		{NotFound, "not_found"},
		{PermissionDenied, "permission_denied"},
		{RateLimited, "rate_limited"},
		{99, "general"}, // unknown code falls back to general
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := CodeName(tt.code); got != tt.want {
				t.Errorf("CodeName(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	inner := errors.New("timeout")
	e := Wrap(RateLimited, inner)
	if e.Code != RateLimited {
		t.Errorf("Code = %d, want %d", e.Code, RateLimited)
	}
	if !errors.Is(e, inner) {
		t.Error("expected wrapped error to be unwrappable")
	}
}
