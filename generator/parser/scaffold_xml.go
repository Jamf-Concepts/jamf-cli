// Copyright 2026, Jamf Software LLC

package parser

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ClassicCountElement is the name of the count element the Jamf Pro Classic API
// puts inside every repeated-element wrapper: `<criteria><size>1</size>…`.
//
// It is server-computed and the spec does not mark it readOnly — it appears 104
// times, 98 of them as a plain `$ref` to a shared integer schema — so nothing
// but ClassicIsCountElement keeps it out of request templates. Wire-checked
// 2026-09-02: a create carrying `<size>` and one omitting it both answer 201, so
// dropping it is safe as well as correct.
const ClassicCountElement = "size"

// ClassicIsCountElement reports whether a property named `size` is the Classic
// API's collection counter rather than a field in its own right.
//
// The name is overloaded in the spec and a blanket drop would delete real data.
// Of the 104 occurrences, two are physical capacities in MB, not counts:
// computer_post's `hardware.storage[].device.size` (example 512287) and the
// `partition.size` beneath it. Neither is reachable today — no Classic resource
// in specs/classic/resources.yaml binds computer_post, since Pro's modern
// computers-inventory covers computers — and
// TestNoBoundResourceCarriesASemanticSizeField fails if an ingest ever binds
// one.
//
// The discriminator is a repeated sibling: a counter counts something, so it
// only ever appears beside an array or, in an array-item wrapper, beside the
// object being repeated. `partition`, whose twelve siblings are all scalars, is
// correctly kept by that test. `device` is not — it has a `partition` array
// beside it — which is exactly why the guard test exists rather than a cleverer
// rule: the honest fix when that case becomes reachable is to read the spec
// again, not to guess harder now.
func ClassicIsCountElement(name string, prop *Property, siblings *Schema) bool {
	if name != ClassicCountElement || prop == nil || siblings == nil {
		return false
	}
	if prop.Type != "integer" && prop.Type != "number" {
		return false
	}
	for siblingName, sibling := range siblings.Properties {
		if siblingName == name || sibling == nil {
			continue
		}
		if sibling.Type == "array" || (sibling.Nested != nil && len(sibling.Nested.Properties) > 0) {
			return true
		}
	}
	return false
}

// ClassicRepeatedElement returns the element name a Classic array's members are
// wrapped in, or "" when the array does not have that shape.
//
// The Classic API is XML, and its JSON representation renders a repeated element
// as an array of single-key objects:
//
//	<criteria>            criteria: {type: array, items: {properties: {
//	  <size>1</size>          criterion: {…},
//	  <criterion>…</criterion>  size: {$ref: size}
//	</criteria>             }}}
//
// So the element name lives one level below the array, as the name of the items
// schema's sole object-valued property. It is not derivable from the array's own
// name: `criteria` holds `criterion`, `computers` holds `computer`, and
// `scope.limit_to_users.user_groups` holds `user_group`.
//
// Returns "" for an array of scalars and for an items schema with more than one
// object-valued property, neither of which has an unambiguous element name.
func ClassicRepeatedElement(items *Schema) string {
	name, _ := classicRepeated(items)
	return name
}

// ClassicArrayElementSchema returns the schema of one member of a Classic array:
// the sole object-valued property's own schema when the array has the
// repeated-element shape, and the items schema itself otherwise.
//
// This is what makes a dotted path skip the wrapper. A criterion's fields are
// addressed as `criteria[].name`, not `criteria[].criterion.name`, because the
// wrapper is an artefact of rendering XML as JSON rather than a level a caller
// should have to type.
func ClassicArrayElementSchema(items *Schema) *Schema {
	if _, elem := classicRepeated(items); elem != nil {
		return elem
	}
	if items == nil || len(items.Properties) == 0 {
		return nil
	}
	return items
}

// classicRepeated implements the shared test behind the two functions above.
func classicRepeated(items *Schema) (string, *Schema) {
	if items == nil {
		return "", nil
	}
	var name string
	var elem *Schema
	for propName, prop := range items.Properties {
		if prop == nil || ClassicIsCountElement(propName, prop, items) {
			continue
		}
		// An object-valued property is one carrying a resolved nested schema.
		// Type is not a reliable test on its own — plenty of Classic schemas
		// declare properties without a `type` — which is the same reason
		// propertyValue in scaffold.go treats "" as an object.
		if prop.Nested == nil || len(prop.Nested.Properties) == 0 {
			return "", nil
		}
		if elem != nil {
			// More than one object-valued child: no unambiguous element name.
			return "", nil
		}
		name, elem = propName, prop.Nested
	}
	return name, elem
}

// ScaffoldXML returns an indented XML template for a Classic API request body,
// derived from its parsed schema and wrapped in the given root element.
//
// It is the XML sibling of ScaffoldJSON and shares its rules — read-only
// properties omitted, write-only kept, a spec example preferred over a
// placeholder, one element shown for an array of objects — so a scaffold means
// the same thing whichever API a command belongs to
// (docs/solutions/conventions/one-scaffold-walker-2026-08-20.md).
//
// It is a separate renderer rather than a re-marshal of ScaffoldJSON's output
// because the JSON is not the wire format here: the Classic API takes XML, and
// three of its shapes have no faithful JSON round trip. Repeated elements
// collapse a wrapper level (see ClassicRepeatedElement), the `size` count
// element has to be dropped, and element order carries meaning in XML while a
// JSON object's key order does not — so the template is built straight from the
// schema.
//
// Two extra rules of its own, both wire-checked against a live tenant on
// 2026-09-02:
//
//   - The `size` count element is dropped. It is server-computed, and a create
//     carrying it and one omitting it both answer 201.
//   - Every `id` the spec declares is kept, including a resource's own. A body
//     `id` is inert on the Classic API: a create sending `<id>99999</id>`
//     answered 201 and assigned 226, and a PUT to /networksegments/id/228
//     carrying `<id>229</id>` updated 228 while leaving 229 untouched — the URL
//     wins. So there is nothing to protect a caller from, and the alternative
//     is worse: most `id` elements in a Classic body are foreign keys the caller
//     is supposed to supply (a policy's category, site, dock item and directory
//     binding all reference one), so a rule that stripped them would have to
//     distinguish identity from reference, and getting that wrong silently
//     removes the field that binds a policy to its category.
func ScaffoldXML(s *Schema, root string) (string, error) {
	if s == nil {
		return "", nil
	}
	if root == "" {
		return "", fmt.Errorf("rendering XML scaffold for schema %q: no root element name", s.Name)
	}

	var b strings.Builder
	if err := writeXMLElement(&b, root, s, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeXMLElement renders one element and its children.
func writeXMLElement(b *strings.Builder, name string, s *Schema, depth int) error {
	indent := strings.Repeat("  ", depth)

	// A schema that is itself an array renders its element directly under this
	// name — a bare-array Classic body has no wrapper of its own.
	if s != nil && s.Type == "array" {
		return writeXMLArray(b, name, s.Items, depth)
	}

	if s == nil || len(s.Properties) == 0 {
		fmt.Fprintf(b, "%s<%s></%s>\n", indent, name, name)
		return nil
	}

	fmt.Fprintf(b, "%s<%s>\n", indent, name)
	for _, propName := range sortedPropertyNames(s) {
		prop := s.Properties[propName]
		if err := writeXMLProperty(b, propName, prop, depth+1); err != nil {
			return err
		}
	}
	fmt.Fprintf(b, "%s</%s>\n", indent, name)
	return nil
}

// writeXMLProperty renders one property as an element.
func writeXMLProperty(b *strings.Builder, name string, p *Property, depth int) error {
	indent := strings.Repeat("  ", depth)

	switch {
	case p.Type == "array":
		return writeXMLArray(b, name, p.Items, depth)
	case p.Nested != nil && len(p.Nested.Properties) > 0:
		return writeXMLElement(b, name, p.Nested, depth)
	}

	fmt.Fprintf(b, "%s<%s>%s</%s>\n", indent, name, xmlText(scalarPlaceholder(p)), name)
	return nil
}

// writeXMLArray renders a repeated element and its single specimen member.
//
// An array of scalars, or one whose element shape the spec does not describe,
// renders as an empty wrapper: the same answer ScaffoldJSON gives with "[]",
// for the same reason — a specimen scalar would imply an empty string is a
// meaningful entry, and there is no element name to hang it on.
func writeXMLArray(b *strings.Builder, name string, items *Schema, depth int) error {
	indent := strings.Repeat("  ", depth)

	elemName, elem := classicRepeated(items)
	if elem == nil {
		// Not the repeated-element shape. An items schema with properties of
		// its own is still worth showing, under the array's own name.
		if items != nil && len(items.Properties) > 0 {
			fmt.Fprintf(b, "%s<%s>\n", indent, name)
			for _, propName := range sortedPropertyNames(items) {
				if err := writeXMLProperty(b, propName, items.Properties[propName], depth+1); err != nil {
					return err
				}
			}
			fmt.Fprintf(b, "%s</%s>\n", indent, name)
			return nil
		}
		fmt.Fprintf(b, "%s<%s></%s>\n", indent, name, name)
		return nil
	}

	fmt.Fprintf(b, "%s<%s>\n", indent, name)
	if err := writeXMLElement(b, elemName, elem, depth+1); err != nil {
		return err
	}
	fmt.Fprintf(b, "%s</%s>\n", indent, name)
	return nil
}

// sortedPropertyNames orders an element's children, dropping the ones a request
// template must not carry.
//
// Alphabetical, with `id` and `name` hoisted to the front. Element order is not
// significant to the Classic API — a partial PUT carrying only `<name>` is
// honoured, wire-checked 2026-09-02 — but it is significant to whoever reads the
// template, and a resource's identity belongs at the top rather than wherever
// the alphabet puts it. Deterministic either way, so regeneration is a no-op.
func sortedPropertyNames(s *Schema) []string {
	names := make([]string, 0, len(s.Properties))
	for name, prop := range s.Properties {
		if prop == nil || prop.ReadOnly || prop.VariantOnly {
			continue
		}
		if ClassicIsCountElement(name, prop, s) {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := xmlNameRank(names[i]), xmlNameRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

func xmlNameRank(name string) int {
	switch name {
	case "id":
		return 0
	case "name":
		return 1
	}
	return 2
}

// scalarPlaceholder renders the value shown for a scalar property: its spec
// example when it has one, else a type-appropriate empty value.
//
// Matches ScaffoldJSON's rule that a real value teaches the format and "" does
// not — the Classic spec carries 1382 examples, so this is the common case, not
// the exception.
func scalarPlaceholder(p *Property) string {
	if p.Example != nil {
		if s, ok := exampleString(p.Example); ok {
			return s
		}
	}
	switch p.Type {
	case "boolean":
		return "false"
	case "integer", "number":
		return "0"
	}
	return ""
}

// exampleString renders a spec example as element text. Reports false for a
// composite, which has no faithful single-element form.
func exampleString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case bool:
		return strconv.FormatBool(val), true
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case int:
		return strconv.Itoa(val), true
	case int64:
		return strconv.FormatInt(val, 10), true
	}
	return "", false
}

// xmlText escapes a spec example for use as element content.
//
// Escapes the three characters that must not appear literally in element
// content, and nothing else. `&` matters most: PI-827 records that the Classic
// API extra-decodes some element bodies, so an under-escaped `&` in an example
// produces a template that fails on upload with a 409 naming nothing useful.
//
// encoding/xml's EscapeText is deliberately not used, even though it is the
// obvious choice. It also escapes quotes and newlines — correct for an
// attribute value, needless for element content — and a policy scaffold is full
// of shell commands, so it rendered the spec's own example as
// `echo &#34;foobar&#34;`. A template a caller has to un-escape by hand before
// editing is worse than one that is merely conservative.
func xmlText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
