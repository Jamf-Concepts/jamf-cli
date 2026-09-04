// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newMCPCmd exposes the entire jamf-cli command tree to MCP-capable AI clients
// (Claude Desktop, Cursor, IDE assistants, custom agents) over a stdio
// transport. Rather than hand-mapping ~200 commands to ~200 typed tools, it
// ships two generic tools — list_commands (discovery) and run_command
// (execution) — and lets the connecting model compose CLI invocations from the
// catalog. Execution re-invokes this same binary as a child process, so auth,
// output formatting, gateway routing, and version checks all run in the
// child's normal PersistentPreRunE: zero duplicated logic, zero cobra-state
// reuse risk.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Expose jamf-cli to AI clients over the Model Context Protocol",
		Long: `Serve jamf-cli's command tree to MCP-capable AI clients over stdio.

The connecting AI gets two tools:
  - list_commands : the full command catalog (names, descriptions, flags)
  - run_command   : execute any jamf-cli command and get its output back

Commands run as child processes of this binary using the same profile this
'mcp serve' was started with (-p/--profile or JAMF_PROFILE), so credentials are
never passed over the protocol. Children run with --no-input, so commands that
would prompt (setup, unconfirmed destructive ops) fail fast instead of hanging;
the model must pass --yes to confirm a destructive command.`,
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start an MCP server on stdio",
		Long: `Start an MCP server that speaks JSON-RPC over stdin/stdout.

Configure it in an MCP client (example for Claude Desktop's config):

  {
    "mcpServers": {
      "jamf-cli": {
        "command": "jamf-cli",
        "args": ["-p", "my-profile", "mcp", "serve"]
      }
    }
  }`,
		Args: refuseStrayPositionals,
		RunE: func(cmd *cobra.Command, _ []string) error {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("determining executable path: %w", err)
			}
			// Capture the profile this server was launched with so every
			// child invocation targets the same instance.
			serverProfile := profile

			server := mcp.NewServer(&mcp.Implementation{
				Name:    "jamf-cli",
				Version: cmd.Root().Version,
			}, nil)

			mcp.AddTool(server, &mcp.Tool{
				Name: "list_commands",
				Description: "List every available jamf-cli command with its description and " +
					"flags. Commands that mutate or erase state are marked \"destructive\": true " +
					"and require an explicit --yes. Call this first to discover what you can run, " +
					"then use run_command.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return runChild(ctx, executable, serverProfile, []string{"commands", "-o", "json"}), nil, nil
			})

			mcp.AddTool(server, &mcp.Tool{
				Name: "run_command",
				Description: "Execute a jamf-cli command. Pass the command and its flags as an " +
					"args array, e.g. [\"pro\",\"computers\",\"list\"] or " +
					"[\"pro\",\"policies\",\"get\",\"--name\",\"My Policy\"]. Output defaults to " +
					"JSON. Do not include credentials. The server is pinned to the profile it " +
					"was started with: --profile/-p, --url, --token-file, --tenant-id, and " +
					"--out-file are rejected. Destructive commands (delete, etc.) " +
					"require an explicit --yes in args or they will refuse to run.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in runCommandInput) (*mcp.CallToolResult, any, error) {
				return runChild(ctx, executable, serverProfile, in.Args), nil, nil
			})

			// A client disconnecting closes stdin, which Run reports as EOF,
			// context cancellation, or the SDK's "server is closing" JSON-RPC
			// error (an internal type, so matched by message). All three are
			// normal session ends, not CLI failures — don't print an error.
			if err := server.Run(cmd.Context(), &mcp.StdioTransport{}); err != nil &&
				!errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) &&
				!strings.Contains(err.Error(), "server is closing") {
				return err
			}
			return nil
		},
	}
}

type runCommandInput struct {
	Args []string `json:"args" jsonschema:"the jamf-cli command and flags to run, as separate array elements (do not join into one string)"`
}

// runChild re-invokes this binary with the given args, injecting the server's
// profile and --no-input, and returns the combined output as an MCP tool
// result. A non-zero exit is reported as an error result (IsError) with the
// captured output, not a transport-level failure.
func runChild(ctx context.Context, executable, serverProfile string, args []string) *mcp.CallToolResult {
	childArgs, err := buildChildArgs(serverProfile, args)
	if err != nil {
		return errorResult(err.Error())
	}

	child := exec.CommandContext(ctx, executable, childArgs...)
	child.Env = append(os.Environ(), "JAMF_CLI_MCP=1")
	out, err := child.CombinedOutput()

	text := string(out)
	if err != nil {
		if text != "" {
			text = fmt.Sprintf("command failed: %v\n\n%s", err, text)
		} else {
			text = fmt.Sprintf("command failed: %v", err)
		}
		return errorResult(text)
	}
	if strings.TrimSpace(text) == "" {
		text = "(command produced no output)"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// blockedChildFlags are flags a connecting model must not be able to set. The
// first four would point the child at a different instance or swap the
// credentials the server was launched with; --out-file would let the model
// write command output to an arbitrary host path. The operator pins the target,
// identity, and output destination once via `mcp serve`; the model only chooses
// which command to run.
var blockedChildFlags = []string{"--profile", "--url", "--token-file", "--tenant-id", "--out-file"}

func isBlockedChildFlag(arg string) bool {
	// Short-flag form (single dash, not "--"): pflag accepts the --profile
	// shorthand -p attached (-pProd) or clustered after value-less bool
	// shorthands (-np Prod), so any short token carrying 'p' can set the
	// profile. Reject them all — 'p' is the only sensitive shorthand and no
	// other global shorthand uses it. A rare false positive (e.g. -oplain)
	// fails closed; the model can fall back to "-o plain".
	if len(arg) >= 2 && arg[0] == '-' && arg[1] != '-' && strings.ContainsRune(arg, 'p') {
		return true
	}
	for _, f := range blockedChildFlags {
		if arg == f || strings.HasPrefix(arg, f+"=") {
			return true
		}
	}
	return false
}

// buildChildArgs validates a model-supplied command and returns the full
// argument list for the child invocation. It rejects empty input and any
// instance-, credential-, or output-redirecting flag (see blockedChildFlags),
// drops any model-supplied --no-input, then injects the server's pinned profile
// and an enforced --no-input the model cannot disable.
func buildChildArgs(serverProfile string, args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("args must not be empty; provide a command such as [\"pro\",\"computers\",\"list\"]")
	}
	for _, a := range args {
		if isBlockedChildFlag(a) {
			return nil, fmt.Errorf("flag %q is not allowed: the MCP server is pinned to the configuration it was started with; the target instance, credentials, and output destination cannot be overridden per command", a)
		}
	}

	childArgs := make([]string, 0, len(args)+3)
	if serverProfile != "" {
		childArgs = append(childArgs, "--profile", serverProfile)
	}
	// Enforce --no-input: inject our own and drop any the model supplied, so it
	// cannot re-enable prompting (e.g. --no-input=false) in a child that has no
	// terminal to prompt on.
	childArgs = append(childArgs, "--no-input")
	for _, a := range args {
		if a == "--no-input" || strings.HasPrefix(a, "--no-input=") {
			continue
		}
		childArgs = append(childArgs, a)
	}
	return childArgs, nil
}
