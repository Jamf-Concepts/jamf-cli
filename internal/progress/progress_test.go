// Copyright 2026, Jamf Software LLC

package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestReporter_Events(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Events)
	r.Update(100, 500)
	r.Update(200, 500)
	r.Stop()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 event lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"event":"page_fetch"`) || !strings.Contains(lines[0], `"fetched":100`) || !strings.Contains(lines[0], `"total":500`) {
		t.Errorf("bad event line: %q", lines[0])
	}
}

func TestReporter_EventsUnknownTotal(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Events)
	r.Update(100, 0) // unknown total
	r.Stop()
	out := buf.String()
	if !strings.Contains(out, `"total":null`) || strings.Contains(out, `"total":0`) {
		t.Errorf("unknown total should emit null, not 0: %q", out)
	}
	if !strings.Contains(out, `"fetched":100`) {
		t.Errorf("event line missing fetched: %q", out)
	}
}

func TestReporter_Silent(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Silent)
	r.Update(100, 500)
	r.Stop()
	if buf.Len() != 0 {
		t.Errorf("silent reporter must emit nothing, got %q", buf.String())
	}
}

func TestReporter_Interactive(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Interactive)
	r.Update(100, 500)
	r.Stop()
	out := buf.String()
	if !strings.Contains(out, "\r") || !strings.Contains(out, "Fetched 100 / 500") {
		t.Errorf("interactive output should carry an in-place count line, got %q", out)
	}
}

func TestReporter_InteractiveUnknownTotal(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Interactive)
	r.Update(100, 0)
	r.Stop()
	if !strings.Contains(buf.String(), "Fetched 100") || strings.Contains(buf.String(), "/ 0") {
		t.Errorf("unknown total should omit the denominator, got %q", buf.String())
	}
}

func TestReporter_StopIdempotent(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Interactive)
	r.Update(50, 100)
	r.Stop()
	after1 := buf.String()
	r.Stop() // second call must be a no-op
	if buf.String() != after1 {
		t.Errorf("second Stop() must not write anything; buffer grew from %q to %q", after1, buf.String())
	}
}
