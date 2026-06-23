// Copyright 2026, Jamf Software LLC

package commands

import (
	"reflect"
	"strings"
	"testing"
)

// The MCP server pins the instance/credentials at launch (the profile passed
// to `mcp serve`). A connecting model must not be able to redirect to a
// different instance or swap credentials by smuggling those flags into the
// run_command args array. buildChildArgs enforces that boundary.

func TestBuildChildArgs_RejectsEmptyArgs(t *testing.T) {
	if _, err := buildChildArgs("prod", nil); err == nil {
		t.Fatal("expected an error for empty args, got nil")
	}
}

func TestBuildChildArgs_RejectsInstanceAndCredentialFlags(t *testing.T) {
	blocked := [][]string{
		{"-p", "other"},
		{"--profile", "other"},
		{"--profile=other"},
		{"--url", "https://evil.example.com"},
		{"--url=https://evil.example.com"},
		{"--token-file", "/tmp/tok"},
		{"--token-file=/tmp/tok"},
		{"--tenant-id", "999"},
		{"--tenant-id=999"},
	}
	for _, override := range blocked {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected args %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_InjectsServerProfileAndNoInput(t *testing.T) {
	got, err := buildChildArgs("prod", []string{"pro", "computers", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--profile", "prod", "--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_OmitsProfileWhenServerProfileEmpty(t *testing.T) {
	got, err := buildChildArgs("", []string{"pro", "computers", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_NormalizesModelNoInput(t *testing.T) {
	// A model-supplied --no-input is dropped and the server's own injected once,
	// so --no-input appears exactly once regardless of what the model passed.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "--no-input"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--no-input", "pro", "computers", "list"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildChildArgs_EnforcesNoInputOverModelOverride(t *testing.T) {
	// A model must not be able to re-enable prompting by passing --no-input=false.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "--no-input=false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var noInputCount int
	for _, a := range got {
		if a == "--no-input=false" {
			t.Errorf("--no-input=false should be dropped, got %v", got)
		}
		if a == "--no-input" {
			noInputCount++
		}
	}
	if noInputCount != 1 {
		t.Errorf("expected exactly one enforced --no-input, got %d in %v", noInputCount, got)
	}
}

func TestBuildChildArgs_RejectsProfileShorthandForms(t *testing.T) {
	// pflag accepts the -p shorthand attached (-pProd) or clustered after
	// value-less bool shorthands (-np Prod, -qpProd); all set --profile.
	blocked := [][]string{
		{"-pProd"},
		{"-np", "Prod"},
		{"-qpProd"},
	}
	for _, override := range blocked {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected args %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_AllowsBenignShortFlags(t *testing.T) {
	// Short flags that don't carry the profile shorthand must pass through.
	got, err := buildChildArgs("", []string{"pro", "computers", "list", "-q", "-o", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"-q", "-o", "json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("benign flag %q should pass through, got %v", want, got)
		}
	}
}

func TestBuildChildArgs_RejectsOutFile(t *testing.T) {
	for _, override := range [][]string{{"--out-file", "/tmp/x"}, {"--out-file=/tmp/x"}} {
		args := append([]string{"pro", "computers", "list"}, override...)
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("expected --out-file %v to be rejected, got nil error", args)
		}
	}
}

func TestBuildChildArgs_AllowsDestructiveWithYes(t *testing.T) {
	// Full surface is intentional: destructive commands are reachable, gated by
	// --yes (and --no-input makes an unconfirmed one fail fast rather than hang).
	// buildChildArgs must not block them.
	got, err := buildChildArgs("prod", []string{"pro", "computers", "delete", "--id", "5", "--yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "delete") || !strings.Contains(joined, "--yes") {
		t.Errorf("destructive command should pass through unchanged, got %v", got)
	}
}
