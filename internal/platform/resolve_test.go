// Copyright 2026, Jamf Software LLC

package platform

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	// Direct sentinel
	if !IsNotFound(ErrNotFound) {
		t.Error("expected IsNotFound(ErrNotFound) = true")
	}

	// Wrapped sentinel (as produced by ResolveBlueprintID)
	wrapped := fmt.Errorf("blueprint %q not found: %w", "test", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Error("expected IsNotFound on wrapped error = true")
	}

	// Non-matching error
	other := errors.New("network timeout")
	if IsNotFound(other) {
		t.Error("expected IsNotFound on unrelated error = false")
	}

	// Nil
	if IsNotFound(nil) {
		t.Error("expected IsNotFound(nil) = false")
	}
}
