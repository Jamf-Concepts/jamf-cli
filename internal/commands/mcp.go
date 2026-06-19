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
		Args: cobra.NoArgs,
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
					"flags. Call this first to discover what you can run, then use run_command.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
				return runChild(ctx, executable, serverProfile, []string{"commands", "-o", "json"}), nil, nil
			})

			mcp.AddTool(server, &mcp.Tool{
				Name: "run_command",
				Description: "Execute a jamf-cli command. Pass the command and its flags as an " +
					"args array, e.g. [\"pro\",\"computers\",\"list\"] or " +
					"[\"pro\",\"policies\",\"get\",\"--name\",\"My Policy\"]. Output defaults to " +
					"JSON. Do not include credentials. Destructive commands (delete, etc.) " +
					"require an explicit --yes in args or they will refuse to run.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in runCommandInput) (*mcp.CallToolResult, any, error) {
				if len(in.Args) == 0 {
					return errorResult("args must not be empty; provide a command such as [\"pro\",\"computers\",\"list\"]"), nil, nil
				}
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
	childArgs := make([]string, 0, len(args)+3)
	if serverProfile != "" && !hasProfileFlag(args) {
		childArgs = append(childArgs, "--profile", serverProfile)
	}
	if !hasFlag(args, "--no-input") {
		childArgs = append(childArgs, "--no-input")
	}
	childArgs = append(childArgs, args...)

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

func hasProfileFlag(args []string) bool {
	for _, a := range args {
		if a == "-p" || a == "--profile" || strings.HasPrefix(a, "--profile=") {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}
