// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"sort"
	"testing"
)

func TestLibraryEmptyByDefault(t *testing.T) {
	_, ok := Lookup("nonexistent/slug")
	if ok {
		t.Fatal("expected Lookup of missing slug to return false")
	}
}

func TestRegisterDuplicateSlugPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate slug, got none")
		}
	}()
	tmpl := Template{Slug: "test/dup", Category: "test", Build: trivialBuild}
	Register(tmpl)
	defer Unregister("test/dup")
	Register(tmpl)
}

func TestCategoriesReturnsSortedUnique(t *testing.T) {
	Register(Template{Slug: "alpha/one", Category: "alpha", Build: trivialBuild})
	defer Unregister("alpha/one")
	Register(Template{Slug: "beta/one", Category: "beta", Build: trivialBuild})
	defer Unregister("beta/one")
	Register(Template{Slug: "alpha/two", Category: "alpha", Build: trivialBuild})
	defer Unregister("alpha/two")

	got := Categories()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("categories not sorted: %v", got)
	}
	foundAlpha, foundBeta := false, false
	for _, c := range got {
		if c == "alpha" {
			foundAlpha = true
		}
		if c == "beta" {
			foundBeta = true
		}
	}
	if !foundAlpha || !foundBeta {
		t.Fatalf("expected alpha and beta in %v", got)
	}
}

func trivialBuild(_ map[string]any) (SmartGroupRequest, error) {
	return SmartGroupRequest{}, nil
}
