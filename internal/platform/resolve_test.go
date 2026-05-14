// Copyright 2026, Jamf Software LLC

package platform

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Jamf-Concepts/jamfplatform-go-sdk/jamfplatform"
)

func TestIsNotFound(t *testing.T) {
	// Direct sentinel
	if !IsNotFound(ErrNotFound) {
		t.Error("expected IsNotFound(ErrNotFound) = true")
	}

	// Wrapped sentinel
	wrapped := fmt.Errorf("blueprint %q not found: %w", "test", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Error("expected IsNotFound on wrapped sentinel = true")
	}

	// SDK *APIResponseError 404 (returned by ResolveBlueprintIDByName on empty results)
	api404 := &jamfplatform.APIResponseError{StatusCode: 404}
	if !IsNotFound(api404) {
		t.Error("expected IsNotFound(*APIResponseError{404}) = true")
	}

	// SDK *APIResponseError 404 wrapped in fmt.Errorf (as the SDK wraps it)
	wrapped404 := fmt.Errorf("ResolveBlueprintIDByName(Brand New Blueprint): %w", api404)
	if !IsNotFound(wrapped404) {
		t.Error("expected IsNotFound on wrapped *APIResponseError{404} = true")
	}

	// SDK *APIResponseError non-404 must not match
	api500 := &jamfplatform.APIResponseError{StatusCode: 500}
	if IsNotFound(api500) {
		t.Error("expected IsNotFound(*APIResponseError{500}) = false")
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
