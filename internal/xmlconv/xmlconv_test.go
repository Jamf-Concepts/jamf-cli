// Copyright 2026, Jamf Software LLC

package xmlconv

import (
	"encoding/json"
	"testing"
)

func TestIsXML(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"xml declaration", `<?xml version="1.0"?><root/>`, true},
		{"element", `<root><child/></root>`, true},
		{"whitespace then xml", `  <root/>`, true},
		{"json object", `{"key": "value"}`, false},
		{"json array", `[1, 2, 3]`, false},
		{"empty", ``, false},
		{"whitespace only", `   `, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsXML([]byte(tt.data)); got != tt.want {
				t.Errorf("IsXML(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestCoerceValue(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"", ""},
		{"true", true},
		{"false", false},
		{"42", 42.0},
		{"-5", -5.0},
		{"3.14", 3.14},
		{"0", 0.0},
		{"0.5", 0.5},
		{"007", "007"},     // Leading zero — keep as string
		{"00", "00"},       // Leading zeros
		{"-007", "-007"},   // Negative leading zero
		{"hello", "hello"}, // Plain string
		{"1e5", 1e5},       // Scientific notation
		{"-0.0", 0.0},      // Negative zero parses as float64 zero
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := coerceValue(tt.input)
			if got != tt.want {
				t.Errorf("coerceValue(%q) = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestCountListItems(t *testing.T) {
	tests := []struct {
		name    string
		xml     string
		want    int
		wantErr bool
	}{
		{
			name: "two items",
			xml:  `<policies><size>2</size><policy><id>1</id></policy><policy><id>2</id></policy></policies>`,
			want: 2,
		},
		{
			name: "one item",
			xml:  `<policies><size>1</size><policy><id>1</id></policy></policies>`,
			want: 1,
		},
		{
			name: "zero items",
			xml:  `<policies><size>0</size></policies>`,
			want: 0,
		},
		{
			name: "no size element",
			xml:  `<policies><policy><id>1</id></policy></policies>`,
			want: 1,
		},
		{
			name:    "invalid xml",
			xml:     `not xml`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CountListItems([]byte(tt.xml))
			if (err != nil) != tt.wantErr {
				t.Errorf("CountListItems() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CountListItems() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractListItems(t *testing.T) {
	tests := []struct {
		name    string
		xml     string
		want    string // expected JSON representation
		wantErr bool
	}{
		{
			name: "multiple items",
			xml:  `<policies><size>2</size><policy><id>1</id><name>A</name></policy><policy><id>2</id><name>B</name></policy></policies>`,
			want: `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`,
		},
		{
			name: "single item",
			xml:  `<packages><size>1</size><package><id>5</id><name>Chrome.pkg</name></package></packages>`,
			want: `[{"id":5,"name":"Chrome.pkg"}]`,
		},
		{
			name: "no items",
			xml:  `<policies><size>0</size></policies>`,
			want: `[]`,
		},
		{
			name: "no size element",
			xml:  `<policies><policy><id>1</id></policy></policies>`,
			want: `[{"id":1}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractListItems([]byte(tt.xml))
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractListItems() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != tt.want {
				t.Errorf("ExtractListItems() =\n  %s\nwant:\n  %s", gotJSON, tt.want)
			}
		})
	}
}

func TestToMap_DetailResponse(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<policy>
  <general>
    <id>1</id>
    <name>Test Policy</name>
    <enabled>true</enabled>
    <category>
      <id>5</id>
      <name>Deployment</name>
    </category>
  </general>
  <scope>
    <all_computers>false</all_computers>
    <computers>
      <size>0</size>
    </computers>
    <computer_groups>
      <size>1</size>
      <computer_group>
        <id>10</id>
        <name>All Managed</name>
      </computer_group>
    </computer_groups>
  </scope>
</policy>`

	m, err := ToMap([]byte(xml))
	if err != nil {
		t.Fatalf("ToMap() error: %v", err)
	}

	policy, ok := m["policy"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"policy\"] to be a map")
	}

	general, ok := policy["general"].(map[string]any)
	if !ok {
		t.Fatal("expected policy[\"general\"] to be a map")
	}
	if general["id"] != 1.0 {
		t.Errorf("general[\"id\"] = %v, want 1", general["id"])
	}
	if general["name"] != "Test Policy" {
		t.Errorf("general[\"name\"] = %v, want \"Test Policy\"", general["name"])
	}
	if general["enabled"] != true {
		t.Errorf("general[\"enabled\"] = %v, want true", general["enabled"])
	}

	scope, ok := policy["scope"].(map[string]any)
	if !ok {
		t.Fatal("expected policy[\"scope\"] to be a map")
	}
	if scope["all_computers"] != false {
		t.Errorf("scope[\"all_computers\"] = %v, want false", scope["all_computers"])
	}

	// computers has <size>0</size> → should be empty array
	computers, ok := scope["computers"].([]any)
	if !ok {
		t.Fatalf("expected scope[\"computers\"] to be []any, got %T", scope["computers"])
	}
	if len(computers) != 0 {
		t.Errorf("len(computers) = %d, want 0", len(computers))
	}

	// computer_groups has <size>1</size> + one group → array of 1
	groups, ok := scope["computer_groups"].([]any)
	if !ok {
		t.Fatalf("expected scope[\"computer_groups\"] to be []any, got %T", scope["computer_groups"])
	}
	if len(groups) != 1 {
		t.Errorf("len(computer_groups) = %d, want 1", len(groups))
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatal("expected computer_groups[0] to be a map")
	}
	if group["name"] != "All Managed" {
		t.Errorf("group[\"name\"] = %v, want \"All Managed\"", group["name"])
	}
}

func TestToMap_ListResponse(t *testing.T) {
	xml := `<policies><size>2</size><policy><id>1</id><name>A</name></policy><policy><id>2</id><name>B</name></policy></policies>`

	m, err := ToMap([]byte(xml))
	if err != nil {
		t.Fatalf("ToMap() error: %v", err)
	}

	// Root with <size> → becomes array
	policies, ok := m["policies"].([]any)
	if !ok {
		t.Fatalf("expected m[\"policies\"] to be []any, got %T", m["policies"])
	}
	if len(policies) != 2 {
		t.Errorf("len(policies) = %d, want 2", len(policies))
	}
}

func TestToMap_ComputerGroupDetail(t *testing.T) {
	xml := `<computer_group>
  <id>1</id>
  <name>Test Group</name>
  <is_smart>false</is_smart>
  <computers>
    <size>2</size>
    <computer>
      <id>5</id>
      <name>Mac1</name>
    </computer>
    <computer>
      <id>6</id>
      <name>Mac2</name>
    </computer>
  </computers>
</computer_group>`

	m, err := ToMap([]byte(xml))
	if err != nil {
		t.Fatalf("ToMap() error: %v", err)
	}

	group, ok := m["computer_group"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"computer_group\"] to be a map")
	}
	if group["is_smart"] != false {
		t.Errorf("is_smart = %v, want false", group["is_smart"])
	}

	// computers has <size> → array
	computers, ok := group["computers"].([]any)
	if !ok {
		t.Fatalf("expected group[\"computers\"] to be []any, got %T", group["computers"])
	}
	if len(computers) != 2 {
		t.Errorf("len(computers) = %d, want 2", len(computers))
	}
}

func TestToJSON(t *testing.T) {
	xml := `<policy><general><id>1</id><name>Test</name></general></policy>`

	jsonBytes, err := ToJSON([]byte(xml))
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	policy, ok := m["policy"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"policy\"] to be a map")
	}
	general, ok := policy["general"].(map[string]any)
	if !ok {
		t.Fatal("expected policy[\"general\"] to be a map")
	}
	if general["name"] != "Test" {
		t.Errorf("name = %v, want \"Test\"", general["name"])
	}
}

func TestToMap_EmptyCollections(t *testing.T) {
	// Elements without <size> and no children become empty strings
	xml := `<scope>
  <all_computers>false</all_computers>
  <computers>
    <size>0</size>
  </computers>
  <limitations>
    <users/>
    <user_groups/>
  </limitations>
</scope>`

	m, err := ToMap([]byte(xml))
	if err != nil {
		t.Fatalf("ToMap() error: %v", err)
	}

	scope, ok := m["scope"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"scope\"] to be a map")
	}

	// computers with <size>0 → empty array
	computers, ok := scope["computers"].([]any)
	if !ok {
		t.Fatalf("expected scope[\"computers\"] to be []any, got %T", scope["computers"])
	}
	if len(computers) != 0 {
		t.Errorf("len(computers) = %d, want 0", len(computers))
	}

	// limitations without <size> → map with empty string children
	limitations, ok := scope["limitations"].(map[string]any)
	if !ok {
		t.Fatalf("expected scope[\"limitations\"] to be a map, got %T", scope["limitations"])
	}
	if limitations["users"] != "" {
		t.Errorf("limitations[\"users\"] = %v, want \"\"", limitations["users"])
	}
}
