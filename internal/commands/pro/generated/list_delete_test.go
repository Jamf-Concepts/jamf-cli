// Copyright 2026, Jamf Software LLC

package generated

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// denyAfterClient answers the first DELETEs with 204 and every later one with
// the permission error client.Do gives a 403, recording each path it was sent.
type denyAfterClient struct {
	okDeletes int
	calls     []string
}

func (c *denyAfterClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.calls = append(c.calls, method+" "+path)
	if method == http.MethodDelete {
		if c.okDeletes == 0 {
			return nil, exitcode.New(exitcode.PermissionDenied, "forbidden (HTTP 403)")
		}
		c.okDeletes--
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

// TestProListDelete_StopsOnPermissionError: a 403 means every later DELETE
// fails too, so the batch stops, sends nothing more and exits with the 403's
// own code rather than the partial-failure 7.
func TestProListDelete_StopsOnPermissionError(t *testing.T) {
	client := &denyAfterClient{okDeletes: 1}
	_, err := runListGetCmd(t, client, "json", "buildings", "delete", "--yes", "--list-file", writeList(t, "1\n2\n3\n4\n"))
	if code := exitcode.CodeFrom(err); code != exitcode.PermissionDenied {
		t.Fatalf("exit = %d (%v), want %d (permission denied)", code, err, exitcode.PermissionDenied)
	}
	deletes := 0
	for _, c := range client.calls {
		if strings.HasPrefix(c, "DELETE ") {
			deletes++
		}
	}
	if deletes != 2 {
		t.Errorf("sent %d DELETEs, want 2 (one success, one refusal, none after): %v", deletes, client.calls)
	}
}

// TestProListDelete_RefusesAPositionalID: `delete <id> --list-file f` would
// delete only f's records and leave <id> alone with exit 0, so it is refused
// before anything is sent.
func TestProListDelete_RefusesAPositionalID(t *testing.T) {
	client := &denyAfterClient{okDeletes: 5}
	_, err := runListGetCmd(t, client, "json", "buildings", "delete", "42", "--yes", "--list-file", writeList(t, "1\n"))
	if err == nil || !strings.Contains(err.Error(), "do not pass an <id>") {
		t.Fatalf("err = %v, want the positional-ID refusal", err)
	}
	for _, c := range client.calls {
		if strings.HasPrefix(c, "DELETE ") {
			t.Errorf("sent %q despite the refusal", c)
		}
	}
}
