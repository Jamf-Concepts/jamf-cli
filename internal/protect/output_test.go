package protect

import (
	"encoding/json"
	"net/http"
	"testing"
)

// mockFormatter captures the raw bytes passed to PrintRaw.
type mockFormatter struct {
	lastData []byte
}

func (m *mockFormatter) PrintResponse(_ *http.Response) error { return nil }
func (m *mockFormatter) PrintRaw(data []byte) error {
	m.lastData = make([]byte, len(data))
	copy(m.lastData, data)
	return nil
}

func TestPrintOne_MarshalsSingleItem(t *testing.T) {
	type item struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	f := &mockFormatter{}
	if err := PrintOne(f, item{Name: "test", ID: 42}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got item
	if err := json.Unmarshal(f.lastData, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "test" || got.ID != 42 {
		t.Fatalf("got %+v, want {Name:test ID:42}", got)
	}
}

func TestPrintList_MarshalsSlice(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}

	items := []item{
		{Name: "alpha"},
		{Name: "beta"},
	}

	f := &mockFormatter{}
	if err := PrintList(f, items); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []item
	if err := json.Unmarshal(f.lastData, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("got %+v, want [{alpha} {beta}]", got)
	}
}

func TestPrintList_EmptySlice(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}

	f := &mockFormatter{}
	if err := PrintList(f, []item{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(f.lastData) != "[]" {
		t.Fatalf("got %q, want %q", string(f.lastData), "[]")
	}
}
