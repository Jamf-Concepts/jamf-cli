// Copyright 2026, Jamf Software LLC

package classic

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// bodySpecLiteral renders one resource's classicBodySpec as a Go composite
// literal for the generated source.
//
// Baked in at generation time rather than loaded at runtime, so the CLI binary
// carries no schema and no parser for one. The same choice the modern Pro
// generator makes for its --set field-type map.
func bodySpecLiteral(r ClassicResource) string {
	if !r.HasBodySchema() {
		return ""
	}

	var b strings.Builder
	b.WriteString("classicBodySpec{\n")
	fmt.Fprintf(&b, "\tRoot:   %s,\n", strconv.Quote(r.BodyRoot))
	fmt.Fprintf(&b, "\tSchema: %s,\n", strconv.Quote(r.BodySchemaName))

	scaffold, err := r.ScaffoldXML()
	if err != nil {
		// Unreachable for a resource that passed HasBodySchema, and rendered as
		// a compile error rather than an empty scaffold if it ever happens:
		// `make generate` exiting 0 with a silently empty --scaffold is the
		// failure mode this whole feature exists to remove.
		return fmt.Sprintf("classicBodySpec{} /* ERROR rendering scaffold: %v */", err)
	}
	fmt.Fprintf(&b, "\tScaffold: %s,\n", backquote(scaffold))

	types := r.SetFieldTypes()
	b.WriteString("\tFieldTypes: map[string]string{\n")
	for _, k := range r.SetFieldTypeKeys() {
		fmt.Fprintf(&b, "\t\t%s: %s,\n", strconv.Quote(k), strconv.Quote(types[k]))
	}
	b.WriteString("\t},\n")

	// Only the enum paths --set can actually receive. EnumChoices also reports
	// the ones inside a repeated element (criteria[].and_or), which --help
	// documents and --set refuses by name — carrying them into the binary would
	// be dead weight in every one of the 43 resources.
	var settableEnums []EnumChoice
	for _, e := range r.EnumChoices() {
		if _, ok := types[e.Path]; ok {
			settableEnums = append(settableEnums, e)
		}
	}
	if len(settableEnums) > 0 {
		b.WriteString("\tEnums: map[string][]string{\n")
		for _, e := range settableEnums {
			quoted := make([]string, len(e.Values))
			for i, v := range e.Values {
				quoted[i] = strconv.Quote(v)
			}
			fmt.Fprintf(&b, "\t\t%s: {%s},\n", strconv.Quote(e.Path), strings.Join(quoted, ", "))
		}
		b.WriteString("\t},\n")
	}

	if creds := r.CredentialFields(); len(creds) > 0 {
		b.WriteString("\tCredentials: map[string]bool{\n")
		for _, c := range creds {
			fmt.Fprintf(&b, "\t\t%s: true,\n", strconv.Quote(c))
		}
		b.WriteString("\t},\n")
	}

	b.WriteString("}")
	return b.String()
}

// backquote renders a string as a Go raw string literal, falling back to an
// interpreted one when the content contains a backquote of its own.
//
// A scaffold is multi-line XML, so a raw literal keeps the generated source
// readable and diffable — which matters, because a spec change shows up in
// `git diff internal/commands/pro/generated/` as the template line that moved.
func backquote(s string) string {
	if !strings.ContainsAny(s, "`\r") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

// bodyHelp renders the tail appended to a write command's Long help: which
// fields are required, which top-level sections exist, and what an
// enum-constrained field accepts.
//
// The enum block is the part that cannot be got any other way. The Classic API
// does not validate its own enums — wire-checked 2026-09-02, a policy created
// with an out-of-range `frequency` answers 201 and reads back the default — so
// without this a caller has no way to learn the legal set short of reading the
// spec. Required fields are at least discoverable, via a 409 whose HTML body
// names one missing field at a time.
func bodyHelp(r ClassicResource) string {
	if !r.HasBodySchema() {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n\nBody fields are derived from the Classic API spec (schema %q).", r.BodySchemaName)
	b.WriteString("\nRun with --scaffold to print a complete XML template.")
	if len(r.RepeatedElements()) > 0 {
		b.WriteString("\nThe template populates every optional section with one specimen entry,\nincluding references whose <id> points at nothing on your instance — delete\nthe sections you do not need. A dangling reference is answered with a 500.")
	}

	if req := r.RequiredFields(); len(req) > 0 {
		fmt.Fprintf(&b, "\n\nRequired: %s", strings.Join(req, ", "))
	}
	if opt := r.TopLevelOptionalFields(); len(opt) > 0 {
		fmt.Fprintf(&b, "\nOptional sections: %s", wrapList(opt, 68, "  "))
	}

	if enums := r.EnumChoices(); len(enums) > 0 {
		b.WriteString("\n\nAllowed values:")
		for _, e := range enums {
			fmt.Fprintf(&b, "\n  %s: %s", e.Path, strings.Join(e.Values, " | "))
		}
		b.WriteString("\n\nThe Classic API does not reject an out-of-range value — it substitutes\nits default silently — so --set refuses one rather than letting it through.")
	}

	if creds := r.CredentialFields(); len(creds) > 0 {
		fmt.Fprintf(&b, "\n\nCredential fields (--from-file only, never --set): %s", strings.Join(creds, ", "))
	}

	return b.String()
}

// wrapList joins names with ", " and wraps at width, indenting continuations.
func wrapList(names []string, width int, indent string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	var b strings.Builder
	line := 0
	for i, n := range sorted {
		if i > 0 {
			b.WriteString(",")
			if line+len(n)+2 > width {
				b.WriteString("\n" + indent)
				line = len(indent)
			} else {
				b.WriteString(" ")
				line++
			}
		}
		b.WriteString(n)
		line += len(n)
	}
	return b.String()
}
