// Copyright 2026, Jamf Software LLC

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintDetail_SingleObjectTable(t *testing.T) {
	var buf bytes.Buffer
	f := New("table", true /*noColor*/, false)
	f.SetWriter(&buf)
	if err := f.Print(map[string]any{"id": "42", "name": "Lab Mac", "managed": true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "id") || !strings.Contains(out, "42") {
		t.Fatalf("missing id row:\n%s", out)
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "Lab Mac") {
		t.Fatalf("missing name row:\n%s", out)
	}
	if strings.Contains(out, "map[") {
		t.Fatalf("rendered Go map repr instead of detail view:\n%s", out)
	}
}

func TestPrintRaw_ArrayOfOneStaysTable(t *testing.T) {
	var buf bytes.Buffer
	f := New("table", true, false)
	f.SetWriter(&buf)
	if err := f.PrintRaw([]byte(`[{"id":"1","name":"a"}]`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "total)") {
		t.Fatalf("array-of-one should render as table:\n%s", buf.String())
	}
}

func TestPrintRaw_ObjectGetsDetail(t *testing.T) {
	var buf bytes.Buffer
	f := New("table", true, false)
	f.SetWriter(&buf)
	if err := f.PrintRaw([]byte(`{"id":"7","name":"solo"}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "total)") {
		t.Fatalf("single object should NOT render as a table:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "solo") {
		t.Fatalf("missing value:\n%s", buf.String())
	}
}
