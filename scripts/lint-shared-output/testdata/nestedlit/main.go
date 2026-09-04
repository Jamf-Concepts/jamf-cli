// Copyright 2026, Jamf Software LLC

package nestedlit

import (
	"os"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

// An element of a slice or map literal may elide its own type, so the node
// that builds the formatter carries no type at all and the type is written
// once, on the enclosing literal.
func printThings(rows []map[string]any) error {
	fs := []output.Formatter{{}}
	byName := map[string]*output.Formatter{"json": {}}
	nested := [][]output.Formatter{{{}}}

	fs[0].SetWriter(os.Stdout)
	byName["json"].SetWriter(os.Stdout)
	nested[0][0].SetWriter(os.Stdout)
	return fs[0].Print(rows)
}

// A literal of the type that builds no element constructs nothing, and neither
// does a slice of something else whose elements elide their type.
func empty() ([]output.Formatter, []map[string]any) {
	return []output.Formatter{}, []map[string]any{{}}
}
