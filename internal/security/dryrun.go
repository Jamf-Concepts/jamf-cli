package security

import (
	"encoding/json"
	"fmt"
	"io"
)

// ReportDryRun prints the request a mutating command would have sent and
// reports nothing else, so the caller can return nil without touching the API.
//
// Generated commands call this instead of the transport when --dry-run is set.
// The gate has to live in the command rather than in the HTTP layer because the
// Security Cloud client asserts an exact success status: a synthetic response
// invented down there would have to guess 200 vs 201 vs 204 per operation and
// would fail the assertion whenever it guessed wrong, turning a preview into an
// error. The command already knows the method, the resolved path and the body.
//
// Output goes to w (stderr for real commands) so a dry run cannot be mistaken
// for command output on a pipe.
func ReportDryRun(w io.Writer, method, path string, body any) error {
	_, _ = fmt.Fprintf(w, "[dry-run] %s %s\n", method, path)
	if body != nil {
		if b, err := json.MarshalIndent(body, "", "  "); err == nil && string(b) != "{}" && string(b) != "null" {
			_, _ = fmt.Fprintf(w, "[dry-run] Request body:\n%s\n", b)
		}
	}
	return nil
}
