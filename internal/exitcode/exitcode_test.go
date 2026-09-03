// Copyright 2026, Jamf Software LLC

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
		{PartialFailure, "partial_failure"},
		{Unsupported, "unsupported"},
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

func TestCodeName_PartialFailure(t *testing.T) {
	if got := CodeName(PartialFailure); got != "partial_failure" {
		t.Fatalf("CodeName(PartialFailure) = %q, want %q", got, "partial_failure")
	}
	if PartialFailure != 7 {
		t.Fatalf("PartialFailure = %d, want 7", PartialFailure)
	}
}

func TestWithHintAndDetails(t *testing.T) {
	err := New(NotFound, "missing").WithHint("run list").WithDetails(map[string]any{"id": "5"})
	if err.Hint != "run list" {
		t.Fatalf("Hint = %q", err.Hint)
	}
	if err.Details["id"] != "5" {
		t.Fatalf("Details = %v", err.Details)
	}
	if got := err.Error(); got != "missing" {
		t.Fatalf("Error() = %q, want %q", got, "missing")
	}
}

func TestPartialOrPropagate(t *testing.T) {
	err := PartialOrPropagate(3, 2, New(Authentication, "401"), "2 of 5 failed")
	if CodeFrom(err) != PartialFailure {
		t.Fatalf("partial: code = %d, want %d", CodeFrom(err), PartialFailure)
	}
	err = PartialOrPropagate(0, 5, New(Authentication, "401"), "5 of 5 failed")
	if CodeFrom(err) != Authentication {
		t.Fatalf("total: code = %d, want %d", CodeFrom(err), Authentication)
	}
	err = PartialOrPropagate(0, 5, nil, "all failed")
	if CodeFrom(err) != General {
		t.Fatalf("total/nil: code = %d, want %d", CodeFrom(err), General)
	}
	if PartialOrPropagate(5, 0, nil, "") != nil {
		t.Fatal("no failures should return nil")
	}
}

// Unsupported is a policy refusal: the command is real and correctly invoked and
// only the credentials cannot reach the API serving it. It has to be its own
// code, because Usage is also every flag error, unknown subcommand, missing URL,
// missing credential, retired host and scope conflict — so a wrapper script
// reading 2 cannot tell a refusal it should degrade around from an invocation
// bug it should stop on.
func TestCodeName_Unsupported(t *testing.T) {
	if got := CodeName(Unsupported); got != "unsupported" {
		t.Fatalf("CodeName(Unsupported) = %q, want %q", got, "unsupported")
	}
	if Unsupported != 8 {
		t.Fatalf("Unsupported = %d, want 8 — the value is documented in README and in the Platform API GA guide", Unsupported)
	}
	if Unsupported == Usage {
		t.Fatal("a policy refusal must not share an exit code with a usage error")
	}
}
