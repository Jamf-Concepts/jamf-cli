// Copyright 2026, Jamf Software LLC

package generated

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// cobraExactOne is cobra.ExactArgs(1), named so the wrapper test reads as the
// generated code does.
var cobraExactOne cobra.PositionalArgs = func(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
	}
	return nil
}

// groupSpec mirrors the shape the generator emits for classic-computer-groups,
// trimmed to the fields these tests exercise. Hand-written rather than taken
// from bodySpecClassicComputerGroups so a spec refresh does not silently change
// what the parser is asserted against.
var groupSpec = classicBodySpec{
	Root:   "computer_group",
	Schema: "computer_group_post",
	Scaffold: `<computer_group>
  <name>Group Name</name>
</computer_group>
`,
	FieldTypes: map[string]string{
		"id":           "integer",
		"name":         "string",
		"is_smart":     "boolean",
		"criteria":     "array",
		"site":         "object",
		"site.id":      "integer",
		"site.name":    "string",
		"access_level": "string",
	},
	Enums: map[string][]string{
		"access_level": {"Full Access", "Site Access"},
	},
	Credentials: map[string]bool{"read_write_password": true},
}

func TestBuildClassicXMLFromSet_NestsAndOrdersElements(t *testing.T) {
	got, err := buildClassicXMLFromSet([]string{"site.name=HQ", "is_smart=false", "name=Engineering"}, groupSpec)
	if err != nil {
		t.Fatal(err)
	}
	// id then name then alphabetical, matching parser.ScaffoldXML, so a body
	// built with --set and one edited from a scaffold read the same.
	want := `<computer_group>
  <name>Engineering</name>
  <is_smart>false</is_smart>
  <site>
    <name>HQ</name>
  </site>
</computer_group>
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildClassicXMLFromSet_CoercesAgainstTheSpecType(t *testing.T) {
	tests := []struct {
		name    string
		pair    string
		want    string
		wantErr string
	}{
		// Classic XML is untyped on the wire, so a wrong type is accepted
		// silently and no response field reveals it — hence coercing against the
		// spec rather than guessing from the value's shape.
		{name: "a numeric-looking string stays a string", pair: "name=42", want: "<name>42</name>"},
		{name: "a boolean normalises", pair: "is_smart=TRUE", want: "<is_smart>true</is_smart>"},
		{name: "an integer passes through", pair: "id=17", want: "<id>17</id>"},
		{name: "a non-boolean is refused", pair: "is_smart=yes-please", wantErr: "is a boolean"},
		{name: "a non-integer is refused", pair: "id=seventeen", wantErr: "is not a valid integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildClassicXMLFromSet([]string{tt.pair}, groupSpec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Errorf("got:\n%s\nwant to contain %q", got, tt.want)
			}
		})
	}
}

// TestBuildClassicXMLFromSet_RefusesAnOutOfRangeEnum is the check the server
// does not do. Wire-checked 2026-09-02: a policy created with
// frequency="Twice per fortnight" answers 201 and reads back "Once per
// computer", and a criterion with and_or="maybe" reads back "and". The value is
// silently replaced by the default, so a caller who guesses wrong gets a working
// object that does the wrong thing.
func TestBuildClassicXMLFromSet_RefusesAnOutOfRangeEnum(t *testing.T) {
	_, err := buildClassicXMLFromSet([]string{"access_level=Partial Access"}, groupSpec)
	if err == nil {
		t.Fatal("expected an out-of-range enum value to be refused")
	}
	for _, want := range []string{"Full Access", "Site Access", "silently substitutes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}

	if _, err := buildClassicXMLFromSet([]string{"access_level=Full Access"}, groupSpec); err != nil {
		t.Errorf("an in-range value must be accepted: %v", err)
	}
}

// TestBuildClassicXMLFromSet_RefusesACredential enforces the credential policy
// that none of the three existing --set implementations enforces. A flag value
// lands in shell history, in ps output and in CI logs, and the Classic surface
// is where SMTP, LDAP, distribution-point and directory-binding passwords live.
func TestBuildClassicXMLFromSet_RefusesACredential(t *testing.T) {
	spec := groupSpec
	spec.FieldTypes = map[string]string{"read_write_password": "string", "name": "string"}

	_, err := buildClassicXMLFromSet([]string{"read_write_password=hunter2"}, spec)
	if err == nil {
		t.Fatal("expected a credential field to be refused")
	}
	if !strings.Contains(err.Error(), "--from-file") {
		t.Errorf("the refusal must name the route that works: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the refusal must not echo the secret: %v", err)
	}
}

func TestBuildClassicXMLFromSet_RefusesAnUnknownField(t *testing.T) {
	// The Classic API discards an element it does not recognise — wire-checked
	// 2026-09-02, a create carrying <bogus_unknown_element> answered 201 and
	// dropped it — so an unrecognised --set has to be refused here or it is
	// accepted by everything and changes nothing.
	_, err := buildClassicXMLFromSet([]string{"nmae=x"}, groupSpec)
	if err == nil {
		t.Fatal("expected an unknown field to be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "computer_group_post") {
		t.Errorf("the refusal should name the schema: %v", err)
	}
	// Siblings are suggested so a typo is obvious rather than a spelling hunt.
	for _, want := range []string{"name", "is_smart"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected sibling %q suggested: %v", want, err)
		}
	}
}

// TestBuildClassicXMLFromSet_NeverSuggestsACredential guards the third place a
// credential field could leak into a prompt: the unknown-field message's sibling
// list. A webhook's siblings include "password", and naming it invites the caller
// to type a secret onto a command line.
func TestBuildClassicXMLFromSet_NeverSuggestsACredential(t *testing.T) {
	spec := groupSpec
	spec.FieldTypes = map[string]string{"name": "string", "password": "string", "enabled": "boolean"}
	spec.Credentials = map[string]bool{"password": true}

	_, err := buildClassicXMLFromSet([]string{"nmae=x"}, spec)
	if err == nil {
		t.Fatal("expected an unknown field to be refused")
	}
	if strings.Contains(err.Error(), "password") {
		t.Errorf("the suggestion list must not name a credential field: %v", err)
	}
	if !strings.Contains(err.Error(), "enabled") {
		t.Errorf("non-credential siblings should still be suggested: %v", err)
	}
}

func TestBuildClassicXMLFromSet_SuggestsSiblingsWithinTheSamePrefix(t *testing.T) {
	_, err := buildClassicXMLFromSet([]string{"site.nmae=x"}, groupSpec)
	if err == nil {
		t.Fatal("expected an unknown nested field to be refused")
	}
	if !strings.Contains(err.Error(), "site.name") {
		t.Errorf("expected the nested sibling suggested: %v", err)
	}
	if strings.Contains(err.Error(), " is_smart") {
		t.Errorf("suggestions should stay within the prefix: %v", err)
	}
}

// TestClassicSetParentKind_ExplainsWhyAPathCannotBeDescended is the difference
// between a wrong message and a right one. criteria is a repeated element, so
// --set criteria.name can never work — but the leaf is simply absent from
// FieldTypes, and without this check the caller is told the field does not
// exist and goes to check their spelling.
func TestClassicSetParentKind_ExplainsWhyAPathCannotBeDescended(t *testing.T) {
	tests := []struct {
		name string
		pair string
		want string
	}{
		{"through an array", "criteria.name=x", "repeated element"},
		{"the bracket spelling help uses", "criteria[].and_or=and", "repeated element"},
		{"the array itself", "criteria=x", "repeated element"},
		{"through a scalar", "name.foo=x", "is a value, not a section"},
		{"an object as a whole", "site=HQ", "is a section, not a value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildClassicXMLFromSet([]string{tt.pair}, groupSpec)
			if err == nil {
				t.Fatalf("expected %q to be refused", tt.pair)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestBuildClassicXMLFromSet_RejectsMalformedPairs(t *testing.T) {
	for _, pair := range []string{"noequals", "=novalue"} {
		if _, err := buildClassicXMLFromSet([]string{pair}, groupSpec); err == nil {
			t.Errorf("expected %q to be refused", pair)
		}
	}
}

func TestBuildClassicXMLFromSet_RefusesConflictingPaths(t *testing.T) {
	// Setting a leaf and then a field beneath it, or the reverse, describes two
	// different bodies. Refused rather than resolved by ordering, which would
	// make the result depend on flag order.
	if _, err := buildClassicXMLFromSet([]string{"site.name=HQ", "site.name.x=y"}, groupSpec); err == nil {
		t.Error("expected a nested key under a set value to be refused")
	}
}

func TestBuildClassicXMLFromSet_EscapesElementContent(t *testing.T) {
	// PI-827: the Classic API extra-decodes some element bodies, so an
	// under-escaped & is refused on upload with a 409 that names nothing.
	got, err := buildClassicXMLFromSet([]string{`name=R&D <team>`}, groupSpec)
	if err != nil {
		t.Fatal(err)
	}
	want := "<name>R&amp;D &lt;team&gt;</name>"
	if !strings.Contains(string(got), want) {
		t.Errorf("got:\n%s\nwant to contain %q", got, want)
	}
}

func TestBuildClassicXMLFromSet_EmptyValueIsAllowed(t *testing.T) {
	// Clearing a field is a legitimate edit, and an empty value skips the enum
	// check for the same reason.
	got, err := buildClassicXMLFromSet([]string{"name="}, groupSpec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<name></name>") {
		t.Errorf("got:\n%s", got)
	}
}

func TestReadClassicBodyOrSet_NoSetFlagsFallsThroughToTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.xml")
	body := "<computer_group><name>from file</name></computer_group>"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readClassicBodyOrSet(path, nil, groupSpec)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("got %q, want the file verbatim", got)
	}
}

// TestReadClassicBodyOrSet_RefusesSetWithFromFile pins the divergence from the
// Platform and Security Cloud --set, which overlay onto a --file body.
// Overlaying here would mean parsing the caller's XML, merging and
// re-marshalling it — and a Classic config-profile body carries a mobileconfig
// inside CDATA that PI-827 says the server extra-decodes, so a round trip
// through a generic XML map is how a payload gets mangled. It is also
// unnecessary: a Classic PUT is a partial update, so --set alone is a valid one.
func TestReadClassicBodyOrSet_RefusesSetWithFromFile(t *testing.T) {
	_, err := readClassicBodyOrSet("/some/file.xml", []string{"name=x"}, groupSpec)
	if err == nil {
		t.Fatal("expected --set with --from-file to be refused")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v", err)
	}
}

func TestReadClassicBodyOrSet_EmptySpecStillReadsAFile(t *testing.T) {
	// The four resources with no schema get classicBodySpec{} and no --set flag,
	// so the call site passes nil and must behave exactly as readClassicBody did
	// before the artifact existed.
	dir := t.TempDir()
	path := filepath.Join(dir, "body.xml")
	if err := os.WriteFile(path, []byte("<computer_configuration/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readClassicBodyOrSet(path, nil, classicBodySpec{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<computer_configuration/>" {
		t.Errorf("got %q", got)
	}
}

func TestClassicXMLText_EscapesTheThreeContentCharacters(t *testing.T) {
	// Quotes deliberately survive: the spec's own policy examples are shell
	// commands, and escaping them makes a template a caller has to un-escape.
	got := classicXMLText(`a & b < c > d "e"`)
	want := `a &amp; b &lt; c &gt; d "e"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGeneratedClassicCommandsCarryScaffoldAndSet checks the wiring: a resource
// with a schema gets both flags on every write, and a resource without gets
// neither. Reading the generated source rather than the cobra tree, because the
// gate is a template condition.
func TestGeneratedClassicCommandsCarryScaffoldAndSet(t *testing.T) {
	tests := []struct {
		file      string
		wantFlags bool
	}{
		{"classic_computer_groups.go", true},
		{"classic_policies.go", true},
		{"classic_network_segments.go", true},
		// computerconfigurations is dead in the Classic API and binds no schema.
		{"classic_computer_configs.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Skipf("generated file unavailable: %v", err)
			}
			code := string(data)
			hasScaffold := strings.Contains(code, `"scaffold", false, "Print an XML body template`)
			hasSet := strings.Contains(code, `cmd.Flags().StringArrayVar(&flagSet, "set"`)
			if hasScaffold != tt.wantFlags || hasSet != tt.wantFlags {
				t.Errorf("scaffold=%v set=%v, want both %v", hasScaffold, hasSet, tt.wantFlags)
			}
			if tt.wantFlags && !strings.Contains(code, "printClassicScaffold(bodySpec") {
				t.Error("no --scaffold early return emitted")
			}
			// The spec var is emitted either way, so the four call sites stay
			// unconditional.
			if !strings.Contains(code, "= classicBodySpec{") {
				t.Error("no classicBodySpec var emitted")
			}
		})
	}
}

// TestClassicScaffoldArgs_RelaxesOnlyForScaffold guards the reason the wrapper
// exists. Cobra validates Args before RunE, so an update requiring an <id>
// refused "update --scaffold" with "accepts 1 arg(s), received 0" and never
// reached the early return that prints the template. What must not happen is the
// relaxation leaking into a real update.
func TestClassicScaffoldArgs_RelaxesOnlyForScaffold(t *testing.T) {
	scaffold := false
	validate := classicScaffoldArgs(&scaffold, cobraExactOne)

	if err := validate(nil, nil); err == nil {
		t.Error("without --scaffold, a missing positional must still be refused")
	}
	if err := validate(nil, []string{"1"}); err != nil {
		t.Errorf("one positional must be accepted: %v", err)
	}

	scaffold = true
	if err := validate(nil, nil); err != nil {
		t.Errorf("with --scaffold, zero positionals must be accepted: %v", err)
	}
}

func TestClassicScaffoldArgs_TolerantOfNils(t *testing.T) {
	// The template always passes both, but a nil inner validator or scaffold
	// pointer must not panic — this runs on every invocation of every classic
	// update.
	if err := classicScaffoldArgs(nil, nil)(nil, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestGeneratedUpdateWrapsItsArgsValidator checks the wiring, since the wrapper
// is only useful if the template actually applies it.
func TestGeneratedUpdateWrapsItsArgsValidator(t *testing.T) {
	for _, file := range []string{"classic_computer_groups.go", "classic_smtp_server.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Skipf("generated file unavailable: %v", err)
		}
		if !strings.Contains(string(data), "classicScaffoldArgs(&flagScaffold,") {
			t.Errorf("%s: update does not wrap its Args validator, so --scaffold is unreachable", file)
		}
	}
}

// TestGeneratedScaffoldFlagIsNotOnReadCommands guards a bug that shipped during
// development: the early return was inserted into `get` instead of `create`,
// because both commands' RunE opens with the same two lines.
func TestGeneratedScaffoldFlagIsNotOnReadCommands(t *testing.T) {
	data, err := os.ReadFile("classic_computer_groups.go")
	if err != nil {
		t.Skipf("generated file unavailable: %v", err)
	}
	code := string(data)

	// Slice out each command constructor and check which ones reference the
	// scaffold guard.
	for _, fn := range []struct {
		name string
		want bool
	}{
		{"newClassicComputerGroupsListCmd", false},
		{"newClassicComputerGroupsGetCmd", false},
		{"newClassicComputerGroupsDeleteCmd", false},
		{"newClassicComputerGroupsCreateCmd", true},
		{"newClassicComputerGroupsUpdateCmd", true},
		{"newClassicComputerGroupsApplyCmd", true},
	} {
		start := strings.Index(code, "func "+fn.name+"(")
		if start < 0 {
			t.Errorf("%s not generated", fn.name)
			continue
		}
		end := strings.Index(code[start+1:], "\nfunc ")
		body := code[start:]
		if end >= 0 {
			body = code[start : start+1+end]
		}
		if got := strings.Contains(body, "if flagScaffold {"); got != fn.want {
			t.Errorf("%s scaffold guard = %v, want %v", fn.name, got, fn.want)
		}
	}
}
