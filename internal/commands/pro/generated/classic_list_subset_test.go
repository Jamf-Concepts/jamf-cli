// Copyright 2026, Jamf Software LLC

package generated

import (
	"strings"
	"testing"
)

func TestExtractClassicListSubset_MultipleUsers(t *testing.T) {
	body := []byte(`<accounts>
  <users>
    <user><id>1</id><name>alice</name></user>
    <user><id>2</id><name>bob</name></user>
  </users>
  <groups>
    <group><id>10</id><name>admins</name></group>
  </groups>
</accounts>`)

	items, err := extractClassicListSubset(body, "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 users, got %d: %+v", len(items), items)
	}
	if items[0]["name"] != "alice" || items[1]["name"] != "bob" {
		t.Errorf("unexpected users: %+v", items)
	}
}

func TestExtractClassicListSubset_SingleItem(t *testing.T) {
	body := []byte(`<accounts><users><user><id>1</id><name>alice</name></user></users></accounts>`)

	items, err := extractClassicListSubset(body, "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0]["name"] != "alice" {
		t.Errorf("want [alice], got %+v", items)
	}
}

func TestExtractClassicListSubset_EmptySelfClosing(t *testing.T) {
	body := []byte(`<accounts><users/><groups><group><id>1</id><name>admins</name></group></groups></accounts>`)

	items, err := extractClassicListSubset(body, "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items == nil {
		t.Fatal("want empty slice, got nil")
	}
	if len(items) != 0 {
		t.Errorf("want 0 items, got %+v", items)
	}
}

func TestExtractClassicListSubset_MissingSubset(t *testing.T) {
	body := []byte(`<accounts><groups><group><id>1</id><name>admins</name></group></groups></accounts>`)

	items, err := extractClassicListSubset(body, "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("want 0 items when subset absent, got %+v", items)
	}
}

func TestExtractClassicListSubset_GroupsSubset(t *testing.T) {
	body := []byte(`<accounts>
  <users><user><id>1</id><name>alice</name></user></users>
  <groups>
    <group><id>10</id><name>admins</name><site><id>-1</id><name>NONE</name></site></group>
    <group><id>11</id><name>auditors</name></group>
  </groups>
</accounts>`)

	items, err := extractClassicListSubset(body, "groups")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 groups, got %d", len(items))
	}
	// Nested <site> should be preserved.
	site, ok := items[0]["site"].(map[string]any)
	if !ok {
		t.Fatalf("want site map on first group, got %T: %+v", items[0]["site"], items[0])
	}
	if site["name"] != "NONE" {
		t.Errorf("site.name = %v, want NONE", site["name"])
	}
}

func TestExtractClassicListSubset_MalformedXML(t *testing.T) {
	_, err := extractClassicListSubset([]byte(`<accounts><users><unclosed`), "users")
	if err == nil {
		t.Error("want error for malformed XML, got nil")
	}
}

func TestSliceClassicListSubsetXML_UsersSubtree(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><accounts><users><user><id>1</id></user></users><groups><group><id>10</id></group></groups></accounts>`)

	out := string(sliceClassicListSubsetXML(body, "users"))
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?><users>`) {
		t.Errorf("want XML-decl + <users> prefix, got %q", out)
	}
	if !strings.HasSuffix(out, `</users>`) {
		t.Errorf("want </users> suffix, got %q", out)
	}
	if strings.Contains(out, "<group>") || strings.Contains(out, "<groups>") {
		t.Errorf("groups subtree leaked into users slice: %q", out)
	}
}

func TestSliceClassicListSubsetXML_SelfClosingSubset(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><accounts><users/><groups><group><id>10</id></group></groups></accounts>`)

	out := string(sliceClassicListSubsetXML(body, "users"))
	want := `<?xml version="1.0" encoding="UTF-8"?><users/>`
	if out != want {
		t.Errorf("want %q, got %q", want, out)
	}
}

func TestSliceClassicListSubsetXML_AbsentSubset(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><accounts><groups><group><id>10</id></group></groups></accounts>`)

	out := sliceClassicListSubsetXML(body, "users")
	// Falls back to original body so something is still printed.
	if string(out) != string(body) {
		t.Errorf("want unchanged body when subset absent, got %q", out)
	}
}
