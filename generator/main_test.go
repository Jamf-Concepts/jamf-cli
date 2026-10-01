// Copyright 2026, Jamf Software LLC

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateEachNamesEveryRefusedResourceAndRecordsOnlyWrittenFiles(t *testing.T) {
	generated := map[string]bool{}
	err := generateEach([]string{"good", "bad-one", "bad-two"},
		func(s string) string { return s },
		func(s string) (string, error) {
			if strings.HasPrefix(s, "bad") {
				return "", errors.New("refused")
			}
			return "out/" + s + ".go", nil
		}, generated)
	if err == nil {
		t.Fatal("generateEach returned nil with two refused resources; main would go on to write the registry and sweep")
	}
	for _, want := range []string{"generating bad-one: refused", "generating bad-two: refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if len(generated) != 1 || !generated["good.go"] {
		t.Errorf("generated = %v, want only good.go", generated)
	}
}

func TestGenerateEachSucceedsWhenNothingIsRefused(t *testing.T) {
	generated := map[string]bool{}
	if err := generateEach([]string{"a"}, func(s string) string { return s },
		func(s string) (string, error) { return s + ".go", nil }, generated); err != nil {
		t.Fatalf("generateEach: %v", err)
	}
}
