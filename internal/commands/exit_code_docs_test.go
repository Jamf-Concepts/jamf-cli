package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// Exit code 8 was added in this change as the machine-readable contract for a
// policy refusal, and both exit-code tables stopped at 7. agent_context.md is
// the CLI's own operating guidance for automated consumers, and its preamble is
// "React to a non-zero exit without parsing the message" — so an agent
// following it treats 8 as an unclassified failure and retries or aborts, where
// the right action is neither. On a GA gateway profile 8 is the most likely
// non-zero exit, which makes it the row an agent needs most.
//
// The tables also disagreed with the code: they named 8 "Refused by policy"
// while CodeName(8) returns "unsupported", the string that actually appears in
// the -o json envelope's exitCodeName and in no document. Both now carry the
// literal string alongside the prose.
//
// This test is the guard for the next code, which is the part that stops the
// same gap recurring: a code in internal/exitcode with no row in either table
// fails here rather than being discovered by a consumer.

// exitCodeDocs are the documents that must carry a row per exit code, relative
// to this package.
var exitCodeDocs = []string{
	"agent_context.md",
	"../../README.md",
}

func TestEveryExitCodeIsDocumented(t *testing.T) {
	// Every code internal/exitcode defines, by value. Kept as an explicit list
	// because Go cannot enumerate untyped constants — so a new code needs a
	// line here, which is the prompt to document it.
	codes := []int{
		exitcode.Success,
		exitcode.General,
		exitcode.Usage,
		exitcode.Authentication,
		exitcode.NotFound,
		exitcode.PermissionDenied,
		exitcode.RateLimited,
		exitcode.PartialFailure,
		exitcode.Unsupported,
	}

	// A code CodeName does not know is a code whose envelope would read
	// "general", which is worse than an undocumented one.
	for _, code := range codes {
		if name := exitcode.CodeName(code); name == "general" && code != exitcode.General {
			t.Errorf("CodeName(%d) falls through to %q — add a case in internal/exitcode", code, name)
		}
	}

	// Guard against the list above going stale in the other direction: a code
	// added to internal/exitcode and not to this list would be silently
	// unchecked. CodeName knowing a value this list omits is that signal.
	for code := 0; code < 32; code++ {
		known := false
		for _, c := range codes {
			if c == code {
				known = true
				break
			}
		}
		if known {
			continue
		}
		if name := exitcode.CodeName(code); name != "general" {
			t.Errorf("internal/exitcode knows code %d as %q but this test's list omits it — "+
				"add it, then document it in %v", code, name, exitCodeDocs)
		}
	}

	for _, doc := range exitCodeDocs {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v\n"+
				"This guard checks the documented exit-code tables; if the file moved, "+
				"update exitCodeDocs rather than deleting the guard.", doc, err)
		}
		text := string(body)
		for _, code := range codes {
			name := exitcode.CodeName(code)
			// A row for the code: a table cell holding the number.
			if !strings.Contains(text, fmt.Sprintf("| %d ", code)) &&
				!strings.Contains(text, fmt.Sprintf("| %d    ", code)) {
				t.Errorf("%s has no exit-code table row for %d (%s) — a consumer told to "+
					"react to the exit code without parsing the message cannot act on a "+
					"code the table omits", doc, code, name)
			}
			// And the literal exitCodeName, which is what a script matches on.
			if !strings.Contains(text, name) {
				t.Errorf("%s never mentions the exitCodeName %q for code %d, so the string "+
					"that appears in the -o json envelope is documented nowhere",
					doc, name, code)
			}
		}
	}
}
