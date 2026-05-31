// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestFormatErrorTo_IncludesHint(t *testing.T) {
	old := outputFmt
	outputFmt = "json"
	defer func() { outputFmt = old }()

	var buf bytes.Buffer
	err := exitcode.New(exitcode.NotFound, "missing").WithHint("run list")
	if !formatErrorTo(&buf, err) {
		t.Fatal("formatErrorTo returned false")
	}
	out := buf.String()
	if !strings.Contains(out, `"hint"`) || !strings.Contains(out, "run list") {
		t.Fatalf("hint not in envelope:\n%s", out)
	}
	if !strings.Contains(out, `"exitCodeName"`) {
		t.Fatalf("exitCodeName not in envelope:\n%s", out)
	}
}

func TestFormatErrorTo_NonJSONReturnsFalse(t *testing.T) {
	old := outputFmt
	outputFmt = "table"
	defer func() { outputFmt = old }()
	if formatErrorTo(&bytes.Buffer{}, exitcode.New(exitcode.General, "x")) {
		t.Fatal("formatErrorTo should return false when output is not json")
	}
}

func TestFprintError_PlainHintLine(t *testing.T) {
	var buf bytes.Buffer
	FprintError(&buf, exitcode.New(exitcode.Authentication, "auth failed").WithHint("re-auth"))
	out := buf.String()
	if !strings.Contains(out, "auth failed") || !strings.Contains(out, "hint: re-auth") {
		t.Fatalf("plain error missing message or hint line:\n%s", out)
	}
}

func TestFprintError_NoHintNoLine(t *testing.T) {
	var buf bytes.Buffer
	FprintError(&buf, exitcode.New(exitcode.General, "plain error"))
	if strings.Contains(buf.String(), "hint:") {
		t.Fatalf("should not print hint line when none set:\n%s", buf.String())
	}
}
