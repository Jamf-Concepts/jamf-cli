// Copyright 2026, Jamf Software LLC

package smartgroup

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// library is the in-memory registry of all curated templates.
// Concrete templates are registered via init() in their category files
// (encryption.go, updates.go, etc.).
var (
	libraryMu sync.RWMutex
	library   = make(map[string]Template)
)

// Register adds a template to the library. Panics on duplicate slug —
// duplicate slugs are a programming error, not a runtime condition.
func Register(t Template) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	if _, exists := library[t.Slug]; exists {
		panic(fmt.Sprintf("smartgroup: duplicate slug %q", t.Slug))
	}
	library[t.Slug] = t
}

// Unregister removes a template; used only in tests.
func Unregister(slug string) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	delete(library, slug)
}

// Lookup returns the template by slug. The second return value reports
// whether the slug exists in the library.
func Lookup(slug string) (Template, bool) {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	t, ok := library[slug]
	return t, ok
}

// All returns all templates ordered first by category, then by slug.
func All() []Template {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	out := make([]Template, 0, len(library))
	for _, t := range library {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// ByCategory returns templates in one category, sorted by slug.
func ByCategory(category string) []Template {
	cat := strings.ToLower(category)
	out := make([]Template, 0)
	for _, t := range All() {
		if t.Category == cat {
			out = append(out, t)
		}
	}
	return out
}

// Categories returns the sorted, unique list of categories present in the library.
func Categories() []string {
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	seen := make(map[string]struct{}, len(library))
	for _, t := range library {
		seen[t.Category] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// FuzzyMatch returns slugs that are similar to the input — used by the CLI
// to suggest corrections on unknown-slug errors. Returns at most 3 matches.
func FuzzyMatch(input string) []string {
	input = strings.ToLower(input)
	all := All()
	type scored struct {
		slug  string
		score int
	}
	cands := make([]scored, 0, len(all))
	for _, t := range all {
		score := simpleScore(strings.ToLower(t.Slug), input)
		if score > 0 {
			cands = append(cands, scored{t.Slug, score})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	out := make([]string, 0, 3)
	for i := 0; i < len(cands) && i < 3; i++ {
		out = append(out, cands[i].slug)
	}
	return out
}

func simpleScore(a, b string) int {
	if strings.Contains(a, b) {
		return 100 - len(a)
	}
	common := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			common++
		}
	}
	return common
}
