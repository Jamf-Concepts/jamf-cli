// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"strings"
	"testing"
)

func TestCriterionConstsNotEmpty(t *testing.T) {
	consts := allCriterionConsts()
	for name, value := range consts {
		if strings.TrimSpace(value) == "" {
			t.Errorf("criterion const %s is empty", name)
		}
	}
}

func TestCriterionConstsUnique(t *testing.T) {
	consts := allCriterionConsts()
	seen := make(map[string]string)
	for name, value := range consts {
		if other, ok := seen[value]; ok {
			t.Errorf("criterion value %q used by both %s and %s", value, name, other)
		}
		seen[value] = name
	}
}
