package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/redact"
)

// writeClassifier reports whether a request changes state on the server. It
// decides both what --dry-run refuses and what the retry policy may re-send.
type writeClassifier func(req *http.Request, body []byte) bool

// isRESTWrite classifies by method, for the Jamf School SDK, which reads with
// GET and writes with POST, PUT and DELETE.
func isRESTWrite(req *http.Request, _ []byte) bool {
	return !retrySafeMethod(req.Method)
}

// isGraphQLMutation classifies a Jamf Protect request, every one of which is a
// POST: reads and writes differ only in the GraphQL document. A body with no
// "query" key is not a GraphQL operation (the token exchange) and is a read.
// A body that cannot be read as GraphQL is a write, so -n fails closed.
func isGraphQLMutation(_ *http.Request, body []byte) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return len(bytes.TrimSpace(body)) > 0
	}
	raw, ok := payload["query"]
	if !ok {
		return false
	}
	var doc string
	if err := json.Unmarshal(raw, &doc); err != nil {
		return true
	}
	return graphQLDocumentWrites(doc)
}

// graphQLDocumentWrites reports whether any top-level operation in doc is a
// mutation or a subscription. Strings, block strings and comments are skipped,
// and only words outside every brace and parenthesis are operation keywords.
func graphQLDocumentWrites(doc string) bool {
	depth := 0
	for i := 0; i < len(doc); {
		c := doc[i]
		switch {
		case c == '#':
			for i < len(doc) && doc[i] != '\n' {
				i++
			}
		case strings.HasPrefix(doc[i:], `"""`):
			end := strings.Index(doc[i+3:], `"""`)
			if end < 0 {
				return true
			}
			i += 3 + end + 3
		case c == '"':
			i++
			for i < len(doc) && doc[i] != '"' {
				if doc[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case c == '{' || c == '(' || c == '[':
			depth++
			i++
		case c == '}' || c == ')' || c == ']':
			depth--
			i++
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			start := i
			for i < len(doc) && (doc[i] == '_' || doc[i] >= 'a' && doc[i] <= 'z' || doc[i] >= 'A' && doc[i] <= 'Z' || doc[i] >= '0' && doc[i] <= '9') {
				i++
			}
			if word := doc[start:i]; depth == 0 && (word == "mutation" || word == "subscription") {
				return true
			}
		default:
			i++
		}
	}
	return false
}

// graphQLOperationName returns the operation name in a GraphQL payload, for
// the --dry-run report.
func graphQLOperationName(body []byte) string {
	var payload struct {
		OperationName string `json:"operationName"`
		Query         string `json:"query"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if payload.OperationName != "" {
		return payload.OperationName
	}
	q := strings.TrimSpace(payload.Query)
	for _, kw := range []string{"mutation", "subscription"} {
		if rest, ok := strings.CutPrefix(q, kw); ok {
			name := strings.TrimSpace(rest)
			if end := strings.IndexAny(name, "({ \t\n"); end >= 0 {
				name = name[:end]
			}
			return name
		}
	}
	return ""
}

func retrySafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// errSentWrite marks a transport failure the retry policy must not re-send:
// a write that reached the server, or any request that timed out after it was
// sent. Wrapped by sendTracker and recognised by sdkRetryPolicy.
var errSentWrite = errors.New("the request reached the server and may have been applied — check the object's current state before retrying")

type writeKey struct{}

// sendTracker sits under the retry client, so it sees every attempt. It
// classifies the request, refuses a write under --dry-run, and records on the
// error whether a failed attempt had reached the server.
type sendTracker struct {
	inner    http.RoundTripper
	isWrite  writeClassifier
	dryRun   bool
	describe func(req *http.Request, body []byte) string
}

func (t *sendTracker) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	write := t.isWrite(req, body)

	var sent atomic.Bool
	ctx := context.WithValue(req.Context(), writeKey{}, write)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteHeaders: func() { sent.Store(true) },
	})
	req = req.Clone(ctx)
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}

	if write && t.dryRun {
		return t.refuse(req, body), nil
	}

	resp, err := t.inner.RoundTrip(req)
	if err != nil && sent.Load() && (write || isTimeoutErr(err)) {
		return nil, fmt.Errorf("%w: %w", errSentWrite, err)
	}
	return resp, err
}

// refuse reports the write on stderr and answers it with a 412, a status the
// retry policy does not re-send. A transport error would be retried.
func (t *sendTracker) refuse(req *http.Request, body []byte) *http.Response {
	what := req.Method + " " + req.URL.Path
	if t.describe != nil {
		if d := t.describe(req, body); d != "" {
			what += " (" + d + ")"
		}
	}
	fmt.Fprintf(os.Stderr, "[dry-run] %s\n", what)
	if len(body) > 0 && verboseLevel >= 3 {
		fmt.Fprintf(os.Stderr, "[dry-run] Request body:\n%s\n", redact.Body(body))
	}
	msg := fmt.Sprintf("refused %s: --dry-run is set and this command has no preview mode. Re-run without -n to apply.", what)
	envelope, _ := json.Marshal(map[string]any{
		"httpStatus": http.StatusPreconditionFailed,
		"errors":     []map[string]string{{"code": "DRY_RUN", "description": msg}},
	})
	return &http.Response{
		StatusCode: http.StatusPreconditionFailed,
		Status:     "412 Precondition Failed",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(envelope)),
		Request:    req,
	}
}

func isTimeoutErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// sdkRetryPolicy re-sends a request only when re-sending cannot apply a change
// twice, as internal/client's doWithRetry does: a 429 is a refusal and is
// retried whatever the request; a 5xx is retried for a read only; a transport
// failure is retried unless a write had reached the server; a timeout is never
// retried.
func sdkRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, errSentWrite) || isTimeoutErr(err) {
			return false, nil
		}
		return retryablehttp.ErrorPropagatedRetryPolicy(ctx, resp, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	if resp.Request == nil {
		return false, nil
	}
	if write, _ := resp.Request.Context().Value(writeKey{}).(bool); write {
		return false, nil
	}
	return retryablehttp.ErrorPropagatedRetryPolicy(ctx, resp, err)
}

// sdkRetryBackoff is retryablehttp's backoff with the server-supplied
// Retry-After held to the ceiling internal/client puts on every server wait.
func sdkRetryBackoff(minWait, maxWait time.Duration, attempt int, resp *http.Response) time.Duration {
	return min(retryablehttp.DefaultBackoff(minWait, maxWait, attempt, resp), client.RateLimitWaitMax)
}

// newSDKRetryClient builds the HTTP client handed to the Jamf Protect and Jamf
// School SDKs, neither of which retries on its own. It keeps a cookie jar,
// because Protect's load balancer pins a session with a cookie.
//
// Never hand it to the Platform SDK: that SDK wraps the client in its own
// retry loop, and this one would nest inside it (see newPlatformSDKClient).
func newSDKRetryClient(isWrite writeClassifier, describe func(*http.Request, []byte) string, plainRetries bool) *http.Client {
	jar, _ := cookiejar.New(nil)
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.Logger = nil
	rc.CheckRetry = sdkRetryPolicy
	rc.Backoff = sdkRetryBackoff
	rc.HTTPClient.Timeout = 60 * time.Second
	rc.HTTPClient.Jar = jar
	rc.HTTPClient.Transport = &sendTracker{
		inner:    verboseRoundTripper(rc.HTTPClient.Transport, plainRetries),
		isWrite:  isWrite,
		dryRun:   dryRun,
		describe: describe,
	}
	return rc.StandardClient()
}

// newProtectHTTPClient is newSDKRetryClient for Jamf Protect's GraphQL API.
// Every call is a POST to one URL, so verbose output carries no retry labels.
func newProtectHTTPClient() *http.Client {
	return newSDKRetryClient(isGraphQLMutation, func(_ *http.Request, body []byte) string {
		if name := graphQLOperationName(body); name != "" {
			return "mutation " + name
		}
		return "mutation"
	}, true)
}

// newSchoolHTTPClient is newSDKRetryClient for the Jamf School REST API.
func newSchoolHTTPClient() *http.Client {
	return newSDKRetryClient(isRESTWrite, nil, false)
}
