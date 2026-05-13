// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"strings"
	"testing"
)

func TestValidateOpts_RequiredMissing(t *testing.T) {
	spec := ParamSpec{Name: "below-version", Type: "string", Required: true}
	tmpl := Template{
		Slug:   "test/required",
		Params: []ParamSpec{spec},
	}
	_, err := tmpl.ResolveOpts(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
	if !strings.Contains(err.Error(), "--below-version") {
		t.Errorf("expected error to mention --below-version, got: %v", err)
	}
}

func TestValidateOpts_TypeMismatch(t *testing.T) {
	spec := ParamSpec{Name: "days", Type: "int"}
	tmpl := Template{
		Slug:   "test/typed",
		Params: []ParamSpec{spec},
	}
	_, err := tmpl.ResolveOpts(map[string]any{"days": "not-an-int"})
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "expected int") {
		t.Errorf("expected 'expected int' in error, got: %v", err)
	}
}

func TestValidateOpts_DefaultApplied(t *testing.T) {
	spec := ParamSpec{Name: "days", Type: "int", Default: 7}
	tmpl := Template{
		Slug:   "test/default",
		Params: []ParamSpec{spec},
	}
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts["days"] != 7 {
		t.Fatalf("expected default 7, got %v", opts["days"])
	}
}

func TestValidateOpts_NoParamsAccepted(t *testing.T) {
	tmpl := Template{Slug: "test/noparam", Params: nil}
	opts, err := tmpl.ResolveOpts(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("expected empty opts, got %v", opts)
	}
}

func TestValidateOpts_StringIntPartialParseRejected(t *testing.T) {
	spec := ParamSpec{Name: "days", Type: "int"}
	tmpl := Template{
		Slug:   "test/partial",
		Params: []ParamSpec{spec},
	}
	_, err := tmpl.ResolveOpts(map[string]any{"days": "30d"})
	if err == nil {
		t.Fatal("expected error for partial-parse '30d', got nil")
	}
	if !strings.Contains(err.Error(), "expected int") {
		t.Errorf("expected 'expected int' in error, got: %v", err)
	}
}
