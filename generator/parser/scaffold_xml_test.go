// Copyright 2026, Jamf Software LLC

package parser

import (
	"strings"
	"testing"
)

// obj builds an object schema from properties.
func obj(name string, props map[string]*Property) *Schema {
	return &Schema{Name: name, Type: "object", Properties: props}
}

func str(example any) *Property  { return &Property{Type: "string", Example: example} }
func integer() *Property         { return &Property{Type: "integer"} }
func boolean() *Property         { return &Property{Type: "boolean"} }
func nested(s *Schema) *Property { return &Property{Type: "object", Nested: s} }
func arr(items *Schema) *Property {
	return &Property{Type: "array", Items: items}
}

func TestScaffoldXML_WrapsInRootAndRendersScalars(t *testing.T) {
	s := obj("network_segment", map[string]*Property{
		"name":               str("Amsterdam Office"),
		"starting_address":   str("10.1.1.1"),
		"override_buildings": boolean(),
	})

	got, err := ScaffoldXML(s, "network_segment")
	if err != nil {
		t.Fatal(err)
	}

	want := `<network_segment>
  <name>Amsterdam Office</name>
  <override_buildings>false</override_buildings>
  <starting_address>10.1.1.1</starting_address>
</network_segment>
`
	if got != want {
		t.Errorf("scaffold mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestScaffoldXML_CollapsesTheRepeatedElementWrapper pins the shape the whole
// XML renderer exists for. The Classic API's JSON representation models
// <criteria><criterion>…</criterion></criteria> as an array of single-key
// objects, and the wrapper level is an artefact of that rendering — so the
// element name has to be read off the items schema and the extra level dropped.
//
// Verified against the wire 2026-09-02: a computer group created from this shape
// read back with criteria intact.
func TestScaffoldXML_CollapsesTheRepeatedElementWrapper(t *testing.T) {
	criterion := obj("criterion", map[string]*Property{
		"name":     str("Last Inventory Update"),
		"and_or":   {Type: "string", Enum: []string{"and", "or"}},
		"priority": integer(),
	})
	items := obj("", map[string]*Property{
		"criterion": nested(criterion),
		"size":      integer(),
	})
	s := obj("computer_group", map[string]*Property{
		"name":     str("Group Name"),
		"criteria": arr(items),
	})

	got, err := ScaffoldXML(s, "computer_group")
	if err != nil {
		t.Fatal(err)
	}

	want := `<computer_group>
  <name>Group Name</name>
  <criteria>
    <criterion>
      <name>Last Inventory Update</name>
      <and_or></and_or>
      <priority>0</priority>
    </criterion>
  </criteria>
</computer_group>
`
	if got != want {
		t.Errorf("scaffold mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "<size>") {
		t.Error("the size count element leaked into the request template")
	}
}

// TestScaffoldXML_RendersAnObjectWithAnArrayChild covers the OTHER modelling the
// same spec uses for the same XML.
//
// policy.scripts is declared as an object holding a `script` array plus `size`,
// where policy.criteria is declared as an array of wrappers. Both render to
// <scripts><script>…</script></scripts>, and a renderer that only handled the
// first shape would emit the second one a level short.
func TestScaffoldXML_RendersAnObjectWithAnArrayChild(t *testing.T) {
	script := obj("", map[string]*Property{
		"id":       integer(),
		"priority": str("After"),
	})
	scripts := obj("scripts", map[string]*Property{
		"script": arr(script),
		"size":   integer(),
	})
	s := obj("policy", map[string]*Property{"scripts": nested(scripts)})

	got, err := ScaffoldXML(s, "policy")
	if err != nil {
		t.Fatal(err)
	}

	want := `<policy>
  <scripts>
    <script>
      <id>0</id>
      <priority>After</priority>
    </script>
  </scripts>
</policy>
`
	if got != want {
		t.Errorf("scaffold mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestScaffoldXML_ScalarArrayRendersAnEmptyElement(t *testing.T) {
	// policy.general.date_time_limitations.no_execute_on is an array of day
	// strings. There is no object shape to show, and rendering a specimen would
	// imply an empty string is a meaningful entry — the same reason ScaffoldJSON
	// renders a scalar array as [].
	s := obj("no_execute_on", map[string]*Property{
		"day": arr(&Schema{Type: "string"}),
	})

	got, err := ScaffoldXML(s, "no_execute_on")
	if err != nil {
		t.Fatal(err)
	}
	want := "<no_execute_on>\n  <day></day>\n</no_execute_on>\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestScaffoldXML_OmitsReadOnlyKeepsWriteOnly(t *testing.T) {
	// The two rules inherited from ScaffoldJSON. Classic declares readOnly on
	// exactly two resource families — computer_invitation and
	// mobile_device_invitation — so this is the shape that exercises it.
	s := obj("computer_invitation", map[string]*Property{
		"invitation":        {Type: "string", ReadOnly: true},
		"invitation_status": {Type: "string", ReadOnly: true},
		"expiration_date":   {Type: "string"},
		"secret":            {Type: "string", WriteOnly: true},
	})

	got, err := ScaffoldXML(s, "computer_invitation")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "invitation_status") || strings.Contains(got, "<invitation>") {
		t.Errorf("read-only field rendered into a request template:\n%s", got)
	}
	if !strings.Contains(got, "<secret></secret>") {
		t.Errorf("write-only field dropped; it is the one a caller most needs prompting for:\n%s", got)
	}
	if !strings.Contains(got, "<expiration_date>") {
		t.Errorf("ordinary field dropped:\n%s", got)
	}
}

func TestScaffoldXML_HoistsIdAndNameThenSortsAlphabetically(t *testing.T) {
	s := obj("policy", map[string]*Property{
		"zeta":  str(nil),
		"name":  str(nil),
		"id":    integer(),
		"alpha": str(nil),
	})

	got, err := ScaffoldXML(s, "policy")
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"<id>", "<name>", "<alpha>", "<zeta>"}
	last := -1
	for _, tag := range wantOrder {
		i := strings.Index(got, tag)
		if i < 0 {
			t.Fatalf("%s missing from:\n%s", tag, got)
		}
		if i < last {
			t.Errorf("%s out of order in:\n%s", tag, got)
		}
		last = i
	}
}

// TestScaffoldXML_EscapesOnlyElementContentCharacters pins the escaping
// decision. `&` must be escaped — PI-827 records that the Classic API
// extra-decodes some element bodies — while a quote must not be, because the
// spec's own policy examples are shell commands and encoding/xml's EscapeText
// rendered one as `echo &#34;foobar&#34;`.
func TestScaffoldXML_EscapesOnlyElementContentCharacters(t *testing.T) {
	s := obj("files_processes", map[string]*Property{
		"run_command": str(`echo "a" && b < c > d`),
	})

	got, err := ScaffoldXML(s, "files_processes")
	if err != nil {
		t.Fatal(err)
	}
	want := `<run_command>echo "a" &amp;&amp; b &lt; c &gt; d</run_command>`
	if !strings.Contains(got, want) {
		t.Errorf("escaping mismatch:\ngot:\n%s\nwant to contain:\n%s", got, want)
	}
}

func TestScaffoldXML_NoRootIsAnError(t *testing.T) {
	// An empty root would render <>…</>, which is not XML. Returned rather than
	// swallowed because this runs at generation time, and the alternative is a
	// command shipping a broken --scaffold while `make generate` exits 0.
	if _, err := ScaffoldXML(obj("x", map[string]*Property{"a": str(nil)}), ""); err == nil {
		t.Fatal("expected an error for an empty root element name")
	}
}

func TestScaffoldXML_NilSchemaIsEmpty(t *testing.T) {
	got, err := ScaffoldXML(nil, "policy")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty output for a nil schema, got %q", got)
	}
}

// TestClassicIsCountElement_DiscriminatesTheOverloadedName covers the two
// meanings the spec gives one property name. `size` beside a repeated element is
// the Classic API's collection counter; `partition.size` is a capacity in MB.
// A blanket drop on the name deletes real data.
func TestClassicIsCountElement_DiscriminatesTheOverloadedName(t *testing.T) {
	counterSiblings := obj("scripts", map[string]*Property{
		"script": arr(obj("", map[string]*Property{"id": integer()})),
		"size":   integer(),
	})
	capacitySiblings := obj("partition", map[string]*Property{
		"name":            str(nil),
		"size":            {Type: "integer", Example: float64(94128)},
		"percentage_full": integer(),
	})

	tests := []struct {
		name     string
		siblings *Schema
		want     bool
	}{
		{"counter beside an array", counterSiblings, true},
		{"capacity among scalars", capacitySiblings, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassicIsCountElement("size", tt.siblings.Properties["size"], tt.siblings)
			if got != tt.want {
				t.Errorf("ClassicIsCountElement = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassicIsCountElement_OnlyMatchesTheNameAndNumericTypes(t *testing.T) {
	siblings := obj("wrapper", map[string]*Property{
		"item": arr(obj("", map[string]*Property{"id": integer()})),
		"size": {Type: "string"},
		"name": str(nil),
	})
	if ClassicIsCountElement("size", siblings.Properties["size"], siblings) {
		t.Error("a string-typed size is not a counter")
	}
	if ClassicIsCountElement("name", siblings.Properties["name"], siblings) {
		t.Error("only the literal name \"size\" is the counter")
	}
}

// TestClassicRepeatedElement_RefusesAmbiguousShapes pins the cases with no
// unambiguous element name. Four array items in the spec carry more than one
// object child (account.groups.group among them), and 42 carry a scalar — for
// both, guessing an element name would put a made-up tag on the wire.
func TestClassicRepeatedElement_RefusesAmbiguousShapes(t *testing.T) {
	twoObjects := obj("", map[string]*Property{
		"privileges": nested(obj("privileges", map[string]*Property{"a": str(nil)})),
		"site":       nested(obj("site", map[string]*Property{"b": str(nil)})),
	})
	scalarItems := &Schema{Type: "string"}

	if got := ClassicRepeatedElement(twoObjects); got != "" {
		t.Errorf("two object children should have no element name, got %q", got)
	}
	if got := ClassicRepeatedElement(scalarItems); got != "" {
		t.Errorf("scalar items should have no element name, got %q", got)
	}
	if got := ClassicRepeatedElement(nil); got != "" {
		t.Errorf("nil items should have no element name, got %q", got)
	}
}

// TestClassicArrayElementSchema_SkipsTheWrapperLevel is what makes a dotted path
// read criteria[].name rather than criteria[].criterion.name.
func TestClassicArrayElementSchema_SkipsTheWrapperLevel(t *testing.T) {
	criterion := obj("criterion", map[string]*Property{"name": str(nil)})
	items := obj("", map[string]*Property{
		"criterion": nested(criterion),
		"size":      integer(),
	})

	got := ClassicArrayElementSchema(items)
	if got == nil {
		t.Fatal("expected the wrapper's sole object child")
	}
	if _, ok := got.Properties["name"]; !ok {
		t.Errorf("expected the criterion's own properties, got %v", got.Properties)
	}
	if _, ok := got.Properties["criterion"]; ok {
		t.Error("the wrapper level was not skipped")
	}
}

func TestClassicArrayElementSchema_FallsBackToTheItemsSchema(t *testing.T) {
	// policy.scripts.script's items ARE the element's own fields, with no
	// wrapper child — so the items schema is the element.
	flat := obj("", map[string]*Property{"id": integer(), "priority": str(nil)})
	got := ClassicArrayElementSchema(flat)
	if got == nil || len(got.Properties) != 2 {
		t.Fatalf("expected the items schema itself, got %#v", got)
	}
	if ClassicArrayElementSchema(&Schema{Type: "string"}) != nil {
		t.Error("a scalar element has no shape to return")
	}
}
