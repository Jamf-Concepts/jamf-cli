# Implement `--dry-run` Flag

## Context

The `--dry-run` (`-n`) global flag is declared in `internal/commands/root.go:264` but no code reads it. Customers using `--dry-run` before destructive operations (create, update, delete) get false safety — the operation executes anyway. This was called out as item #8 in the pod proposal's "Things You Might Be Missing" section.

## Approach: HTTP Client Wrapper (same pattern as `spinnerClient`)

The codebase already has a wrapper pattern — `spinnerClient` wraps `generated.HTTPClient` to show a loading spinner. A `dryRunClient` wrapper follows the identical pattern:

- **Read-only methods pass through:** GET and HEAD requests execute normally.
- **Mutating methods are intercepted:** POST, PUT, PATCH, DELETE print what *would* happen to stderr, then return a synthetic response so the command exits cleanly.

This works for all 80+ generated commands automatically with zero changes to generated code.

## Files to Modify

| File | Change |
|------|--------|
| `internal/commands/root.go` | Add `dryRunClient` struct + `Do()` method. Wrap the HTTP client with it when `dryRun == true`, positioned outside the spinner wrapper so the spinner doesn't appear for intercepted requests. |
| `internal/commands/root_test.go` | New test file. Test that dryRunClient passes through GET, intercepts POST/PUT/PATCH/DELETE, and returns a well-formed response. |

No changes to generated code, `client.go`, `main.go`, or any other files.

## Implementation Detail

### `dryRunClient` (in `root.go`)

```go
type dryRunClient struct {
    inner generated.HTTPClient
}

func (c *dryRunClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
    // Read-only methods pass through
    if method == "GET" || method == "HEAD" {
        return c.inner.Do(ctx, method, path, body)
    }

    // Print what would happen to stderr
    fmt.Fprintf(os.Stderr, "[dry-run] %s %s\n", method, path)

    // Show request body if present
    if body != nil {
        data, err := io.ReadAll(body)
        if err == nil && len(data) > 0 {
            fmt.Fprintf(os.Stderr, "[dry-run] Request body:\n%s\n", string(data))
        }
    }

    // Return synthetic empty response — generated commands
    // check resp.StatusCode and call resp.Body.Close(), so both must be valid.
    return &http.Response{
        StatusCode: http.StatusOK,
        Body:       io.NopCloser(strings.NewReader("{}")),
        Header:     make(http.Header),
    }, nil
}
```

### Wrapping order in `PersistentPreRunE`

```go
var httpClient generated.HTTPClient = &cliClient{client.New(...)}

// Dry-run wraps first (innermost) — intercepts before spinner sees it
if dryRun {
    httpClient = &dryRunClient{inner: httpClient}
}

// Spinner wraps outermost — only runs for requests that reach the network
if !quiet && !verbose {
    httpClient = &spinnerClient{inner: httpClient}
}
```

This ordering means:
- With `--dry-run`: spinner starts, dryRunClient intercepts the mutating request instantly (no network), spinner stops. Fast, no visible delay.
- Without `--dry-run`: spinner wraps the real network request as before.

### What the user sees

```
$ jamfpro-cli categories create --dry-run < payload.json
[dry-run] POST /v1/categories
[dry-run] Request body:
{"name":"Test Category","priority":1}
{}
```

- `[dry-run]` lines go to stderr (won't pollute piped output)
- `{}` is the synthetic response printed to stdout by the output formatter
- GET/list commands work normally even with `--dry-run` (safe to read)

## Verification

1. `go build ./...` — compiles
2. `go test ./...` — all existing + new tests pass
3. Manual test: `echo '{"name":"test"}' | ./bin/jamfpro-cli categories create --dry-run --url https://example.com --token fake` — prints dry-run message, no network request
4. Manual test: `./bin/jamfpro-cli categories list --dry-run --url https://example.com --token fake` — attempts the GET normally (will fail with network error, proving it passed through)
