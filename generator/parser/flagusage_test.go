// Copyright 2026, Jamf Software LLC

package parser

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestFlagUsage(t *testing.T) {
	const in = "Fields to sort by.</br> Fields allowed for sorting: `name`, `description`.<br/>\n  Example: `name==\"x\"`<BR>"
	got := FlagUsage(in)
	want := "Fields to sort by. Fields allowed for sorting: 'name', 'description'. Example: 'name==\"x\"'"
	if got != want {
		t.Fatalf("FlagUsage = %q\nwant        %q", got, want)
	}
	// The property that matters: pflag no longer finds a placeholder in it.
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	fs.String("sort", "", got)
	if name, _ := pflag.UnquoteUsage(fs.Lookup("sort")); name != "string" {
		t.Errorf("placeholder = %q, want the type name", name)
	}
}
