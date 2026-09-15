// Copyright 2026, Jamf Software LLC

package scope

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ScopeDrop records one scope category whose members the server did not store.
type ScopeDrop struct {
	Section  string   // target, limitation or exclusion
	Category string   // the CLI flag name, e.g. "class"
	Missing  []string // the members that went in and did not come back
}

// VerifyScopeWrite re-reads a resource's scope after a write and reports every
// member that was sent and did not come back.
//
// It compares the WHOLE scope rather than only the item the command changed,
// which the single-item check it replaced could not do — and collateral loss is
// real, not hypothetical. An ebook's <classes> target can only be set while it
// is empty: once populated, any later scope write clears it, whether the body
// carries the identical value or omits it (wire-checked 2026-09-12, 5/5 each
// way). So `scope add --building` on an ebook that has a class destroys the
// class, and a check scoped to --building sees a clean write.
//
// Because <scope> is replaced wholesale, that shape of loss can appear on any
// category the server decides not to store, so the guard is general rather than
// a rule about ebooks. `touched` is named separately only so the message can
// lead with the member the caller asked about.
func VerifyScopeWrite(ctx context.Context, client registry.HTTPClient, res Resource, id string, sent *ScopeXML, touched ScopeTarget, touchedSection string, expectPresent bool) error {
	_, got, err := FetchScope(ctx, client, res, Ref{ID: id})
	if err != nil {
		return fmt.Errorf("verifying scope: %w", err)
	}

	// The member the command changed, reported in its own words: absent when
	// it should be there is usually an identifier that named no record, which
	// is a different diagnosis from a category the server refused to keep.
	if items := readScopeItems(got, touchedSection, touched.FlagName); itemPresent(items, touched.Name) != expectPresent {
		return silentDropError(res.SingularKey, touchedSection, touched.FlagName, touched.Name, expectPresent)
	}

	drops := DiffScope(sent, got, touchedSection, touched)
	if len(drops) == 0 {
		return nil
	}
	// The message states what happened and leaves the cause to the
	// per-category note. Asserting a server limitation as the cause would be
	// wrong for any category the CLI has a workaround for — class targets
	// being exactly that — and the operator's first need is to know which
	// members are gone either way.
	return fmt.Errorf("the write succeeded but the server did not keep %s; those members are gone and need re-adding%s",
		describeDrops(drops), categoryDropNote(res.SingularKey, drops))
}

// DiffScope returns every category where a member present in sent is absent
// from got. The touched member is excluded, since its own absence is reported
// with a more specific diagnosis.
func DiffScope(sent, got *ScopeXML, touchedSection string, touched ScopeTarget) []ScopeDrop {
	if sent == nil || got == nil {
		return nil
	}
	var drops []ScopeDrop
	for _, section := range Sections {
		for _, flag := range scopeFlagNames {
			before := readScopeItems(sent, section, flag)
			if before == nil || len(before.Items) == 0 {
				continue
			}
			after := readScopeItems(got, section, flag)
			var missing []string
			for _, item := range before.Items {
				label := itemLabel(item)
				if section == touchedSection && flag == touched.FlagName && strings.EqualFold(label, touched.Name) {
					continue
				}
				if !itemPresent(after, label) {
					missing = append(missing, label)
				}
			}
			if len(missing) > 0 {
				drops = append(drops, ScopeDrop{Section: section, Category: flag, Missing: missing})
			}
		}
	}
	return drops
}

// itemLabel picks the identifier to report and match a scope member by,
// preferring the name a caller would recognise.
func itemLabel(item NamedItem) string {
	switch {
	case item.Name != "":
		return item.Name
	case item.ID != "":
		return item.ID
	default:
		return item.UDID
	}
}

// itemPresent matches a member by name, ID or UDID — the server augments what
// it was sent (a member sent by name comes back with an ID, and a network
// segment gains a uid), so equality of the elements is not the test.
func itemPresent(items *ScopeItemSlice, value string) bool {
	if items == nil || value == "" {
		return false
	}
	for _, item := range items.Items {
		if strings.EqualFold(item.Name, value) ||
			item.ID == value ||
			strings.EqualFold(item.UDID, value) {
			return true
		}
	}
	return false
}

// describeDrops renders the dropped members for the error message.
func describeDrops(drops []ScopeDrop) string {
	parts := make([]string, 0, len(drops))
	for _, d := range drops {
		members := append([]string(nil), d.Missing...)
		sort.Strings(members)
		parts = append(parts, fmt.Sprintf("%s %s %s", d.Section, pluralCategory(d.Category, len(members)), quoteAll(members)))
	}
	sort.Strings(parts)
	return humanList(parts)
}

func pluralCategory(category string, n int) string {
	if n == 1 {
		return category
	}
	return category + "s"
}

func quoteAll(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("%q", v)
	}
	return humanList(out)
}

// categoryDropNote explains a drop where the cause is known, so the operator
// is not left to guess whether retrying will help.
func categoryDropNote(singularKey string, drops []ScopeDrop) string {
	if singularKey != "ebook" {
		return ""
	}
	for _, d := range drops {
		if d.Category == flagClass {
			// PutScope's two-request delivery exists precisely to stop this,
			// so reaching here means the workaround did not hold — the server
			// rule has moved, or the second request did not land. Say that,
			// rather than repeating the limitation as though it were expected.
			return ". Class targets are delivered in two requests specifically to survive this (see PutScope), so this is unexpected: re-run to restore the class, and if it recurs the server's rule for <classes> has changed"
		}
	}
	return ""
}
