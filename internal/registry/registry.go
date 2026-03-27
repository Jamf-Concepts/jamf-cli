// Package registry defines the shared interfaces and types used by all
// product command packages (pro, protect, etc.) and the root command.
package registry

import (
	"context"
	"io"
	"net/http"
)

// HTTPClient interface for making API requests.
type HTTPClient interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// OutputFormatter interface for formatting output.
type OutputFormatter interface {
	PrintResponse(resp *http.Response) error
	PrintRaw(data []byte) error
}

// CLIContext holds the shared client and output formatter for all commands.
// It is populated in PersistentPreRunE after token/URL resolution.
type CLIContext struct {
	Client HTTPClient
	Output OutputFormatter
}
