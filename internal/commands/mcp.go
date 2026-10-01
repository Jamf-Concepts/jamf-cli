// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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

The connecting AI gets three tools:
  - list_commands   : browse the command catalog one level at a time, or search it
  - run_command     : execute any jamf-cli command and get its output back
  - generate_report : write a shareable HTML fleet report and return its path

Commands run as child processes of this binary using the same profile this
'mcp serve' was started with (-p/--profile or JAMF_PROFILE), so credentials are
never passed over the protocol. Children run with --no-input, so commands that
would prompt (setup, unconfirmed destructive ops) fail fast instead of hanging;
the model must pass --yes to confirm a destructive command.

For the richest session, use a Platform profile (auth-method: platform). One set
of Platform Gateway credentials covers both the Jamf Pro API and Platform-specific
commands (blueprints, compliance benchmarks, DDM reports).

generate_report needs a report directory, which the connecting AI cannot
choose. Set one with: jamf-cli config set-report-dir <dir>`,
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	var inputDirFlag string
	cmd := &cobra.Command{
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
  }

The profile is resolved once at startup — -p/--profile, then JAMF_PROFILE, then
the configured default — and every child is pinned to it. The server refuses to
start with no profile and no credentials in the environment, rather than
answering every tool call with the same auth error.

generate_report collects in two tiers, and the connecting AI is told to run the
fast one first and ask before the full one. So an administrator should expect a
question about a "full report" rather than a long silence; what each tier costs
is in 'jamf-cli dashboard --help'.

run_command judges the command and flags cobra resolves the arguments to, so
an alias or a flag ahead of the command path is judged the same as the plain
spelling. It refuses:
  - 'multi', 'mcp', 'completion' and shell completion, the config write
    subcommands, every 'setup', both 'backup' commands and
    'jamf-cloud-distribution-service sync' (also mounted as 'packages sync'),
    which pick their own instance or write where the model says
  - 'config validate' and 'doctor', which resolve or report every profile's
    credentials and probe other profiles' URLs ('config show' runs, with each
    token, client ID and client secret shown as <redacted>, and 'config list
    --status' checks only this server's profile)
  - 'auth token' under 'platform', 'pro' and 'protect', and 'pro
    api-authentication token', 'oauth-token' and 'keep-alive', which print a
    live access token
  - 'pro sso-oauth-session-tokens', which prints the session's access and ID
    tokens
  - 'pro api-integrations client-credentials' and 'protect api-clients
    apply', which mint a new client secret or password and print it
  - 'pro cloud-distribution-point create' and 'patch', whose response carries
    the CloudFront private key that signs download URLs
  - 'pro jamf-pro-user-account-settings change-password' and 'pro accounts
    create', 'update' and 'apply', which set a Jamf Pro login password to a
    value the model chose
  - 'protect action-configs export', whose document carries each report
    client's header values, the SIEM or webhook credential, verbatim
  - 'protect downloads csr' and 'websocket-auth', which write the tenant's
    .p12 key material into the directory this server was started in
  - any flag naming a profile, URL, token, tenant, environment or output file
  - any flag whose value is a local path: --from-file, --file, --script-file,
    --mobileconfig-file, --appconfig-file, --custom-payload-file, --body-file,
    --input, --password-file, --dir, --save-to, --report-dir, and a command's
    own --output (the global -o/--output format flag stays available), except
    as --input-dir allows below
  - 'pro diff' with a --source or --target that is neither this server's
    profile nor a directory inside --input-dir
Some allowed commands print a third-party credential, shown as <redacted>
here: the report-client header values and the userinfo and query of each
report-client URL in 'protect action-configs get' and 'apply' (a secret in a
URL path, as a Slack webhook carries, is still shown), the Sentinel shared key
of 'protect data-forwarding get' and 'update', the password of 'protect
api-clients get', the CloudFront private key and CDN password of 'pro
cloud-distribution-point list', and every Classic 'get' and 'list' field the
Classic --set refuses as a credential (an SMTP, LDAP, webhook, directory
binding or distribution point password, the VPP sToken, the JWT signing key,
the institutional FileVault keystore). Each secret inside a configuration
profile's payloads in 'classic-macos-config-profiles' and
'classic-mobile-config-profiles' is redacted too: a Wi-Fi, EAP, VPN or account
password, a VPN shared secret, a SCEP challenge, and an identity certificate
with its password. A payload that does not decode is redacted whole. So a
Classic '-o raw' is not the wire bytes here, and 'pro diff' shows those
fields' old and new values as <redacted> while still reporting the change.
Secrets of the pinned tenant's own devices are shown: the LAPS password, the
recovery lock password, the FileVault personal recovery key, and the bootstrap
token, unlock token and AirPlay password in device inventory. So are the JCDS
upload credentials of 'pro jamf-cloud-distribution-service renew-credentials'
and 'pro jamf-cloud-distribution-service-files create'. Blueprint configuration
is shown as the Platform API answers it, a secret a component carries
included, in the 'pro blueprints' and 'school blueprints' reads and in 'pro
diff' on blueprints.
Some allowed commands write a file into the directory this server was started
in, named by Jamf or by the command's own argument: the other 'protect
downloads' and 'pro jcds download' without -O, and 'protect plans
config-profile'. Start the server from a directory where that is acceptable.

It also refuses 'dashboard', because it returns a command's stdout as text and
the report is a 320-800 KB document — generate_report writes that to a file
instead.

--input-dir <dir> lets the model pass files it needs to read: a path given to
--from-file, --file, --script-file, --mobileconfig-file, --appconfig-file,
--custom-payload-file, --body-file, --input, a 'pro diff' --source or --target
directory, or the --dir of 'protect analytics import' and 'protect
unified-logging-filters import' is accepted when it exists and resolves inside
<dir>, symlinks followed. A relative path is taken from the directory this
server was started in, so pass an absolute path. --password-file and every
write-side path flag stay refused. The directory must exist, and an empty
value is an error; there is no config key for it.`,
		Args: refuseStrayPositionals,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("input-dir") && inputDirFlag == "" {
				return errors.New("--input-dir is empty: name the directory the model may read from, or drop the flag to allow no reads")
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("determining executable path: %w", err)
			}
			// Resolve the pin once, at startup, and pass a concrete name to
			// every child. `profile` alone is the -p var, so a server started
			// without -p had no pin at all and each child re-resolved the
			// default — which a `config set-default` during the session could
			// move underneath it.
			cfg, _ := config.Load()
			serverProfile, err := resolveMCPServerProfile(cfg)
			if err != nil {
				return err
			}

			inputDir, err := resolveMCPInputDir(inputDirFlag)
			if err != nil {
				return err
			}

			if !noHints {
				printMCPStartupHints(cmd.ErrOrStderr(), cfg)
			}
			installMCPResolver(cmd.Root(), inputDir)

			server := mcp.NewServer(&mcp.Implementation{
				Name:    "jamf-cli",
				Version: cmd.Root().Version,
			}, nil)

			mcp.AddTool(server, &mcp.Tool{
				Name: "list_commands",
				Description: "Browse or search the jamf-cli command catalog. Call this first to find " +
					"what you can run, then use run_command. Each result line is one JSON object.\n\n" +
					"With no arguments, it lists the top level: the products (pro, protect, school, " +
					"security, platform) and the core commands. A row with \"subcommands\": N stands " +
					"for N commands under it. Pass its command as prefix to open it, for example " +
					"prefix \"pro\", then prefix \"pro computers\". A row with no subcommands is a " +
					"command you can run, listed with its flags. A few groups can also be run on " +
					"their own.\n\n" +
					"Pass query to find commands by words in their path or description, for " +
					"example \"delete policy\". Add prefix to search one product or resource. " +
					"Search rows carry no flags: pass a command as prefix to see its flags.\n\n" +
					"Commands marked \"destructive\": true mutate or erase state and require an " +
					"explicit --yes. For a command's arguments and flag details, run it with --help " +
					"through run_command, e.g. [\"pro\",\"computers\",\"list\",\"--help\"]. A last line " +
					"with \"truncated\": true means rows were left out: narrow the query or add a prefix.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCommandsInput) (*mcp.CallToolResult, any, error) {
				return listCommands(ctx, executable, serverProfile, in), nil, nil
			})

			mcp.AddTool(server, &mcp.Tool{
				Name: "run_command",
				Description: "Execute a jamf-cli command. Pass the command and its flags as an " +
					"args array, e.g. [\"pro\",\"computers\",\"list\"] or " +
					"[\"pro\",\"policies\",\"get\",\"--name\",\"My Policy\"]. Output defaults to " +
					"JSON. Do not include credentials. The server is pinned to the profile it " +
					"was started with and judges the command your args resolve to, aliases " +
					"included. Rejected: any flag naming a profile, URL, token, tenant, " +
					"environment or output file; any flag whose value is a local file or " +
					"directory (--from-file, --file, --script-file, --save-to, --dir and the " +
					"like; the -o/--output format flag is fine); 'multi', 'mcp', 'completion', " +
					"the config write subcommands, 'config validate', 'doctor', " +
					"every command that prints an access token ('auth token', " +
					"'pro api-authentication token', 'oauth-token', 'keep-alive'), " +
					"'pro sso-oauth-session-tokens', " +
					"the commands that mint and print a credential ('pro api-integrations " +
					"client-credentials', 'protect api-clients apply'), " +
					"'pro cloud-distribution-point create' and 'patch' (they print a " +
					"CloudFront private key), the commands that set a Jamf Pro login password " +
					"('pro jamf-pro-user-account-settings change-password', 'pro accounts " +
					"create', 'update', 'apply'), 'protect downloads csr' and 'websocket-auth', " +
					"'protect action-configs export', " +
					"every 'setup', the backup commands and jcds sync; and " +
					"'pro diff' against anything but this server's profile or a directory the " +
					"input directory allows. " + inputDirToolNote(inputDir) +
					" Use generate_report rather than 'dashboard': this tool returns " +
					"stdout as text and the dashboard writes a 320-800 KB HTML document there. " +
					"Report-client header values and URL userinfo and query, the Sentinel " +
					"shared key, Protect API client passwords, the CloudFront private key of " +
					"'pro cloud-distribution-point list' and Classic credential fields " +
					"(passwords, the VPP sToken, the JWT signing key), and the secrets inside " +
					"Classic configuration profile payloads (Wi-Fi, VPN and identity passwords, " +
					"shared secrets, SCEP challenges, PKCS#12 certificates), in 'get', 'list' and " +
					"'pro diff', print as <redacted>; " +
					"device secrets such as the LAPS password, and blueprint configuration, are shown. " +
					"Output is truncated past 256 KB. Destructive commands (delete, etc.) " +
					"require an explicit --yes in args or they will refuse to run.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in runCommandInput) (*mcp.CallToolResult, any, error) {
				if err := refuseReportThroughRunCommand(in.Args); err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return runChild(ctx, executable, serverProfile, in.Args), nil, nil
			})

			mcp.AddTool(server, &mcp.Tool{
				Name: "generate_report",
				Description: "Generate a shareable, self-contained HTML fleet report and write " +
					"it into the report directory the administrator configured. Returns the " +
					"file path, its size, and any warnings — never the HTML, which is far " +
					"too large for a conversation. Use this when the administrator wants " +
					"something to share or action rather than read now; use run_command for " +
					"questions answered by a table.\n\n" +
					"TWO-TIER COLLECTION — call generate_report WITHOUT full:true first. " +
					"The fast report covers fleet counts, security posture, " +
					"OS distribution, check-in compliance, audit findings, and environment stats. " +
					dashboardCostNote + "\n\n" +
					"After it completes, get the fleet size with run_command " +
					"[\"pro\",\"computers-inventory\",\"list\",\"--limit\",\"1\",\"--field\",\"totalCount\"] " +
					"— generate_report returns only the path, size and warnings, never the report's " +
					"own figures — then offer the extended report: 'The instance has N managed " +
					"devices. I can run a full report that also includes patch compliance, hardware " +
					"models, cleanup analysis, and org structure. Would you like the full report?' " +
					"Only set full:true after explicit confirmation.\n\n" +
					"The report covers the profile this server was started with. A Platform " +
					"profile (auth-method: platform) gives the most comprehensive report: it " +
					"authenticates one set of credentials against the Jamf Platform Gateway and " +
					"collects both Jamf Pro data and Platform-specific data (blueprints, compliance " +
					"benchmarks, DDM reports). The destination and file name are server-derived " +
					"and cannot be set per call. After calling it, tell the administrator the " +
					"path and summarize what the report says.",
			}, func(ctx context.Context, _ *mcp.CallToolRequest, in generateReportInput) (*mcp.CallToolResult, any, error) {
				return runReportChild(ctx, executable, serverProfile, in, time.Now()), nil, nil
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
	cmd.Flags().StringVar(&inputDirFlag, "input-dir", "", "directory the connecting model may name files inside for read-side path flags such as --from-file")
	return cmd
}

// inputDirToolNote tells the model where read-side path flags may point.
func inputDirToolNote(inputDir string) string {
	if inputDir == "" {
		return "Read-side path flags are refused too: this server allows no input directory."
	}
	return "Read-side path flags (--from-file, --file, --script-file and the like) are accepted for existing paths inside " +
		inputDir + ", the only directory this server reads from; --password-file stays refused."
}

// resolveMCPInputDir returns dir with every symlink resolved, or "" when no
// input directory is set. The server refuses to start on one it cannot use.
func resolveMCPInputDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", fmt.Errorf("--input-dir %s is not accessible: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("--input-dir %s is not accessible: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--input-dir %s is not a directory", dir)
	}
	return abs, nil
}

type runCommandInput struct {
	Args []string `json:"args" jsonschema:"the jamf-cli command and flags to run, as separate array elements (do not join into one string)"`
}

type generateReportInput struct {
	Title       string   `json:"title,omitempty" jsonschema:"report title shown in the HTML heading; must not begin with a dash"`
	SmartGroups []string `json:"smart_groups,omitempty" jsonschema:"smart group names to visualize"`
	Full        bool     `json:"full,omitempty" jsonschema:"set to true to collect additional sections: patch compliance, hardware models, cleanup analysis, org structure and the fleet-scaled audit checks. Omit or set false for the default fast report. Before setting this, tell the user how many devices the instance has and ask if they want the extended report — see the tool description for what each tier costs."`
}

// childEnv is the environment both MCP children run with.
//
// JAMF_CLI_ARGS is removed, and that is the point. cmd/jamf-cli/main.go reads
// it and injectEnvArgs prepends its shell-split contents ahead of everything
// buildChildArgs injects — so an inherited `--out-file /tmp/x` set the very
// flag blockedChildFlagPrefixes refuses, and every generate_report wrote its
// HTML there while the O_EXCL file the server opened stayed at zero bytes and
// the model was told the report had been written. The server builds this argv;
// nothing may be prepended to it.
//
// JAMF_CLI_MCP=1 is appended so the child knows it is an MCP child, and an
// inherited JAMF_CLI_MCP_PROFILE or JAMF_CLI_MCP_INPUT_DIR is dropped so only
// pinnedChildEnv sets them.
func childEnv() []string {
	env := os.Environ()
	kept := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && (name == "JAMF_CLI_ARGS" || name == mcpPinnedProfileEnvVar || name == mcpInputDirEnvVar) {
			continue
		}
		kept = append(kept, kv)
	}
	return append(kept, mcpChildEnvVar+"=1")
}

const (
	mcpChildEnvVar         = registry.MCPChildEnvVar
	mcpPinnedProfileEnvVar = "JAMF_CLI_MCP_PROFILE"
	mcpInputDirEnvVar      = "JAMF_CLI_MCP_INPUT_DIR"
)

// pinnedChildEnv is childEnv plus the profile the server is pinned to and the
// input directory it allows, which refuseInMCPChild judges the child's own
// parse against.
func pinnedChildEnv(serverProfile string) []string {
	return append(childEnv(),
		mcpPinnedProfileEnvVar+"="+serverProfile,
		mcpInputDirEnvVar+"="+installedMCPInputDir())
}

type listCommandsInput struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"a command path to open, such as \"pro\" or \"pro computers\"; omit it for the top level"`
	Query  string `json:"query,omitempty" jsonschema:"words to find in command paths and descriptions, such as \"delete policy\"; searches under prefix when both are set"`
}

const (
	listCommandsBrowseFields = "command,description,destructive,subcommands,flags"
	listCommandsSearchFields = "command,description,destructive"

	// maxListCommandsBytes keeps one list_commands result inline in Claude Code.
	// Claude Code 2.1.274 saves a text tool result longer than 50,000 characters
	// to a file and hands the model only its path, whatever its token count.
	maxListCommandsBytes = 40 << 10
)

// listCommandsArgs builds the `commands` invocation for one list_commands call.
// The model's text is joined to its flag with "=", so it reaches the child as a
// flag value and never as a flag of its own.
func listCommandsArgs(in listCommandsInput) []string {
	args := []string{"commands", "-o", "ndjson"}
	if prefix := strings.TrimSpace(in.Prefix); prefix != "" {
		args = append(args, "--prefix="+prefix)
	}
	if query := strings.TrimSpace(in.Query); query != "" {
		return append(args, "--search="+query, "--select="+listCommandsSearchFields)
	}
	return append(args, "--children", "--select="+listCommandsBrowseFields)
}

// listCommands returns the catalog child's stdout alone, since one stderr line
// in it makes the catalog invalid JSON.
func listCommands(ctx context.Context, executable, serverProfile string, in listCommandsInput) *mcp.CallToolResult {
	childArgs, err := buildChildArgs(serverProfile, listCommandsArgs(in))
	if err != nil {
		return errorResult(err.Error())
	}

	var stderr bytes.Buffer
	child := exec.CommandContext(ctx, executable, childArgs...)
	child.Env = pinnedChildEnv(serverProfile)
	child.Stderr = &stderr
	out, err := child.Output()
	if err != nil {
		text := fmt.Sprintf("listing commands failed: %v", err)
		if warnings := strings.TrimSpace(tailWarnings(stderr.Bytes())); warnings != "" {
			text += "\n\n" + warnings
		}
		return errorResult(text)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		out = []byte(`{"matches":0,"hint":"nothing is listed under this prefix"}` + "\n")
		if strings.TrimSpace(in.Query) != "" {
			out = []byte(`{"matches":0,"hint":"no command matches; use fewer or shorter words, or browse with prefix"}` + "\n")
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: capCatalogLines(out)}},
	}
}

// listCommandsNoteBytes is room kept under maxListCommandsBytes for the
// truncation line.
const listCommandsNoteBytes = 256

// capCatalogLines keeps whole NDJSON lines up to maxListCommandsBytes. When it
// drops any, it ends with one JSON line counting them, so every line parses.
func capCatalogLines(out []byte) string {
	lines := strings.SplitAfter(string(out), "\n")
	var b strings.Builder
	for i, line := range lines {
		if b.Len()+len(line) <= maxListCommandsBytes-listCommandsNoteBytes {
			b.WriteString(line)
			continue
		}
		omitted := 0
		for _, rest := range lines[i:] {
			if strings.TrimSpace(rest) != "" {
				omitted++
			}
		}
		fmt.Fprintf(&b, `{"truncated":true,"omitted":%d,"hint":"narrow the query or add a prefix"}`+"\n", omitted)
		break
	}
	return b.String()
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
	child.Env = pinnedChildEnv(serverProfile)
	out, err := child.CombinedOutput()

	text := capChildOutput(out)
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

// maxChildOutputBytes caps what one run_command result may carry. 256 KiB is
// roughly 64k tokens — already more than a tool result should spend, and a
// ceiling rather than a target.
//
// The cap exists because run_command returns whatever the child printed and the
// tree has commands whose output is unbounded: a full inventory sweep, a
// backup's progress log, a report. Truncating a table costs the tail of an
// answer; returning 500 KB costs the conversation.
const maxChildOutputBytes = 256 << 10

// capChildOutput truncates the child's output to maxChildOutputBytes, keeping
// the head — the opposite of tailWarnings, because a command's answer starts at
// the top where a warning stream explains itself at the bottom.
func capChildOutput(b []byte) string {
	if len(b) <= maxChildOutputBytes {
		return string(b)
	}
	return string(b[:maxChildOutputBytes]) +
		fmt.Sprintf("\n\n(output truncated at %d bytes — narrow the query with a filter, --limit or --field,"+
			" or use generate_report if you want a whole-fleet document)", maxChildOutputBytes)
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// blockedChildFlagPrefixes are flag names a connecting model must not be able
// to set. Every one of them would point the child at a different instance or
// scope, swap the credentials the server was launched with, or write to an
// arbitrary host path. The operator pins the target, identity, scope and output
// destination once via `mcp serve`; the model only chooses which command to run.
//
// Matched by **prefix**, which is the whole point. The list used to be matched
// exactly, so every flag the tree grows whose name merely starts with a blocked
// one slipped past a rule written for it: `--profiles` (on `multi`) and
// `--include-profile` (on `dashboard`, added by the same change as this
// comment) both select credentials and both were accepted. Adding one more
// literal per discovery is how that happens again.
//
// --token covers --token-file and any future --token-* ; --profile covers
// --profiles; --include-profile has to be named, being a suffix rather than a
// prefix of --profile. --tenant-id and --environment-id are the two
// mutually-exclusive gateway scope selectors and both redirect the request, so
// blocking one without the other leaves the hole open.
//
// A deny-list cannot be complete, so this is a floor rather than the boundary:
// mcpRefusedCommands carries the namespaces no flag list can pin, and
// mcpLocalPathFlags the flags whose value is a path on this machine.
var blockedChildFlagPrefixes = []string{
	"--profile",
	"--include-profile",
	"--url",
	"--token",
	"--tenant-id",
	"--environment-id",
	"--out-file",
}

// mcpRefusedCommands are resolved command paths, each refused with everything
// beneath it, and why. `dashboard` is not here: generate_report shares
// buildChildArgs and must still spawn it, so the run_command handler refuses it
// instead.
//
// A command that prints a credential working outside this server is refused.
// A secret of the pinned tenant's own devices is not: the operator decided the
// model may read them. So the LAPS password (`pro local-admin-password
// password`, `password-by-guid`, `audit`, `audit-by-guid`), the recovery lock
// password (`pro computer-inventory view-recovery-lock-password`), the
// FileVault personal recovery key (`filevault`, `filevault-by-id`) and the
// bootstrap token, unlock token and AirPlay password in device inventory all
// run, as do the commands that set or clear such a secret. So do `pro
// jamf-cloud-distribution-service renew-credentials` and
// `pro jamf-cloud-distribution-service-files create`, whose upload credentials
// reach only the pinned tenant's JCDS bucket.
//
// A command that prints such a credential inside a larger record runs with it
// redacted instead, in every output format: the Classic credential fields
// (redactClassicReadInMCPChild), the secrets in a Classic configuration
// profile's payloads plist (redactClassicProfilePayloadsInMCPChild, through
// profileconvert.RedactPayloadSecrets), the cloud distribution point's keys
// (cdnKeyRedactingClient), and the same fields in `pro diff`. Blueprint
// configuration is not redacted: the Platform SDK decodes each response
// inside its transport, so there is no per-response hook, and a component's
// secret keys vary by declaration type. It is named as shown in the help and
// the tool description instead.
// TestMCPSecretNamingLeaves_AreClassified holds every leaf whose help names a
// credential to one side of this line.
var mcpRefusedCommands = []refusedCommand{
	{"jamf-cli multi", refusedPicksTarget},
	{"jamf-cli mcp", refusedPicksTarget},
	{"jamf-cli completion", "'completion install' writes into this machine's shell configuration, and the rest print a script no MCP client can use"},
	{"jamf-cli config set-default", refusedPicksTarget},
	{"jamf-cli config add-profile", refusedPicksTarget},
	{"jamf-cli config remove-profile", refusedPicksTarget},
	{"jamf-cli config set-report-dir", refusedPicksTarget},
	{"jamf-cli config validate", refusedReadsCredentials},
	{"jamf-cli doctor", refusedReadsCredentials},
	{"jamf-cli platform auth token", refusedPrintsToken},
	{"jamf-cli pro auth token", refusedPrintsToken},
	{"jamf-cli protect auth token", refusedPrintsToken},
	{"jamf-cli pro api-authentication token", refusedPrintsToken},
	{"jamf-cli pro api-authentication oauth-token", refusedPrintsToken},
	{"jamf-cli pro api-authentication keep-alive", refusedPrintsToken},
	{"jamf-cli pro sso-oauth-session-tokens", refusedPrintsToken},
	{"jamf-cli pro api-integrations client-credentials", refusedMintsClientSecret},
	{"jamf-cli pro cloud-distribution-point create", refusedPrintsCDNKey},
	{"jamf-cli pro cloud-distribution-point patch", refusedPrintsCDNKey},
	{"jamf-cli pro jamf-pro-user-account-settings change-password", refusedSetsLoginPassword},
	{"jamf-cli pro accounts create", refusedSetsLoginPassword},
	{"jamf-cli pro accounts update", refusedSetsLoginPassword},
	{"jamf-cli pro accounts apply", refusedSetsLoginPassword},
	{"jamf-cli protect downloads csr", refusedWritesKeyMaterial},
	{"jamf-cli protect downloads websocket-auth", refusedWritesKeyMaterial},
	{"jamf-cli protect api-clients apply", refusedMintsProtectPassword},
	{"jamf-cli protect action-configs export", refusedExportsHeaders},
	{"jamf-cli pro setup", refusedPicksTarget},
	{"jamf-cli platform setup", refusedPicksTarget},
	{"jamf-cli protect setup", refusedPicksTarget},
	{"jamf-cli school setup", refusedPicksTarget},
	{"jamf-cli security setup", refusedPicksTarget},
	{"jamf-cli pro backup", refusedPicksTarget},
	{"jamf-cli protect backup", refusedPicksTarget},
	{"jamf-cli pro jamf-cloud-distribution-service sync", refusedPicksTarget},
	{"jamf-cli pro packages sync", refusedPicksTarget},
}

type refusedCommand struct {
	path, why string
}

const (
	refusedPicksTarget          = "it selects its own instance or writes to a path of its own, so the profile this server was started with cannot pin it"
	refusedReadsCredentials     = "it resolves or reports the credentials of profiles other than the one this server is pinned to, and probes their URLs"
	refusedPrintsToken          = "it prints a live access token, which works outside this server and every refusal it applies until it expires"
	refusedMintsClientSecret    = "it mints a new client secret for the API integration and prints it, a credential that works outside this server until it is rotated"
	refusedMintsProtectPassword = "creating an API client mints a new password and prints it, a credential that works outside this server until the client is deleted"
	refusedPrintsCDNKey         = "its response carries the CloudFront private key that signs download URLs, a credential that works outside this server"
	refusedSetsLoginPassword    = "it sets a Jamf Pro login password to a value the model chose, a credential that works outside this server"
	refusedWritesKeyMaterial    = "it writes the tenant's .p12 key material into the directory this server was started in, under a fixed name that replaces any file already there"
	refusedExportsHeaders       = "its document carries each report client's header values verbatim (the SIEM or webhook bearer token), and a redacted copy would overwrite the real credential when applied; 'protect action-configs get' shows the configuration with them redacted"
)

// isCompletionRequest reports whether name is cobra's hidden completion
// command, which parses no flags of its own and runs the target command's
// completion functions. Cobra adds it only when it is invoked, so a resolve
// against the tree cannot find it and it is matched by name instead.
func isCompletionRequest(name string) bool {
	return name == cobra.ShellCompRequestCmd || name == cobra.ShellCompNoDescRequestCmd
}

// localPathUse is what a command does with a flag whose value is a path on the
// machine running the server.
type localPathUse string

const (
	pathRead       localPathUse = "reads"
	pathCredential localPathUse = "reads a credential from"
	pathWrite      localPathUse = "writes"
)

// mcpLocalPathFlags classifies path flags by name. `output` reaches here only
// where a leaf declares its own; the root's persistent --output is the format
// selector.
var mcpLocalPathFlags = map[string]localPathUse{
	"from-file":           pathRead,
	"file":                pathRead,
	"script-file":         pathRead,
	"mobileconfig-file":   pathRead,
	"appconfig-file":      pathRead,
	"custom-payload-file": pathRead,
	"body-file":           pathRead,
	"input":               pathRead,
	"password-file":       pathCredential,
	"save-to":             pathWrite,
	"output":              pathWrite,
	"report-dir":          pathWrite,
}

// mcpDirFlags classifies --dir per command, since it is an input on some and a
// destination that `--delete` empties on others.
var mcpDirFlags = map[string]localPathUse{
	"jamf-cli protect analytics import":                 pathRead,
	"jamf-cli protect unified-logging-filters import":   pathRead,
	"jamf-cli pro jamf-cloud-distribution-service sync": pathWrite,
	"jamf-cli pro packages sync":                        pathWrite,
}

// localPathFlagUse classifies one flag occurrence on the command at path. A
// --dir on a command mcpDirFlags does not name is taken as a destination.
func localPathFlagUse(path string, s flagSetting) (localPathUse, bool) {
	if s.rootOutput {
		return "", false
	}
	if s.name == "dir" {
		if use, ok := mcpDirFlags[path]; ok {
			return use, true
		}
		return pathWrite, true
	}
	use, ok := mcpLocalPathFlags[s.name]
	return use, ok
}

const (
	proDiffPath   = "jamf-cli pro diff"
	dashboardPath = "jamf-cli dashboard"
)

// childInvocation is what cobra resolves a child argv to. An empty path means
// cobra refuses the argv before running anything. inputDir is the one
// directory read-side path flags may name, "" when none is allowed.
type childInvocation struct {
	path     string
	settings []flagSetting
	inputDir string
}

// flagSetting is one occurrence of a flag on the command line, in order. A
// repeatable flag set twice is two settings.
type flagSetting struct {
	name, value string
	rootOutput  bool
}

// mcpResolver is the tree run_command argv is resolved against and the input
// directory `mcp serve` allows reads from. Every resolve holds the lock for its
// whole length: Find merges persistent flags into the leaf, and ParseAll
// records its arguments on the leaf's flag set.
var mcpResolver struct {
	sync.Mutex
	root     *cobra.Command
	inputDir string
}

// installMCPResolver makes later resolves use root and inputDir, which must
// already be symlink-resolved. `mcp serve` installs the tree it is running in,
// because NewRootCmd rebinds every flag variable in this package to its
// default, and main reads those after serve returns.
func installMCPResolver(root *cobra.Command, inputDir string) {
	mcpResolver.Lock()
	defer mcpResolver.Unlock()
	mcpResolver.root = root
	mcpResolver.inputDir = inputDir
}

func installedMCPInputDir() string {
	mcpResolver.Lock()
	defer mcpResolver.Unlock()
	return mcpResolver.inputDir
}

// resolveChildInvocation returns what a child process given args would run.
//
// It mirrors ExecuteC: Find, not Traverse, since the root does not set
// TraverseChildren. ParseAll hands each occurrence to a callback and never
// calls Set, so no flag variable changes. A parse error ends the list where the
// child's own parse stops, and the child's FlagErrorFunc always returns an
// error, so nothing after it can run.
func resolveChildInvocation(args []string) childInvocation {
	mcpResolver.Lock()
	defer mcpResolver.Unlock()
	root := mcpResolver.root
	if root == nil {
		root = NewRootCmd(cliVersion, "", "", "")
		root.InitDefaultHelpCmd()
		root.InitDefaultCompletionCmd()
		root.InitDefaultVersionFlag()
	}

	cmd, rest, err := root.Find(args)
	if err != nil {
		return childInvocation{}
	}
	inv := childInvocation{path: cmd.CommandPath(), inputDir: mcpResolver.inputDir}
	if cmd.DisableFlagParsing {
		return inv
	}
	cmd.InitDefaultHelpFlag()
	rootOutput := root.PersistentFlags().Lookup("output")
	_ = cmd.Flags().ParseAll(rest, func(f *pflag.Flag, value string) error {
		inv.settings = append(inv.settings, flagSetting{name: f.Name, value: value, rootOutput: f == rootOutput})
		return nil
	})
	return inv
}

// refuseOverMCP returns why inv is not available to an MCP client pinned to
// pinnedProfile, or nil.
func refuseOverMCP(inv childInvocation, pinnedProfile string) error {
	for _, refused := range mcpRefusedCommands {
		if inv.path == refused.path || strings.HasPrefix(inv.path, refused.path+" ") {
			return fmt.Errorf("command %q is not available over MCP: %s", strings.TrimPrefix(inv.path, "jamf-cli "), refused.why)
		}
	}
	for _, s := range inv.settings {
		if isBlockedChildFlag("--" + s.name) {
			return fmt.Errorf("flag %q is not allowed: the MCP server is pinned to the configuration it was started with; the target instance, credentials, and output destination cannot be overridden per command", "--"+s.name)
		}
		if use, ok := localPathFlagUse(inv.path, s); ok {
			if err := refuseLocalPath(use, s, inv.inputDir); err != nil {
				return err
			}
		}
		if inv.path == proDiffPath && (s.name == "source" || s.name == "target") {
			if err := refuseDiffSide(s, pinnedProfile, inv.inputDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// refuseInMCPChild applies refuseOverMCP to the command this process parsed,
// when it was spawned by `mcp serve`. The server's own --profile is not a
// setting the model made, so it is skipped when it names the pinned profile.
func refuseInMCPChild(cmd *cobra.Command) error {
	if os.Getenv(mcpChildEnvVar) != "1" {
		return nil
	}
	if isCompletionRequest(cmd.Name()) {
		return errCompletionOverMCP
	}
	pinned := os.Getenv(mcpPinnedProfileEnvVar)
	inv := childInvocation{path: cmd.CommandPath(), inputDir: os.Getenv(mcpInputDirEnvVar)}
	rootOutput := cmd.Root().PersistentFlags().Lookup("output")
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == "profile" && pinned != "" && f.Value.String() == pinned {
			return
		}
		values := []string{f.Value.String()}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			values = sv.GetSlice()
		}
		for _, v := range values {
			inv.settings = append(inv.settings, flagSetting{name: f.Name, value: v, rootOutput: f == rootOutput})
		}
	})
	return refuseOverMCP(inv, pinned)
}

// refuseDiffSide judges one `pro diff` side: a directory is a local read under
// the input-directory rule, and a profile must be the pinned one.
func refuseDiffSide(s flagSetting, pinnedProfile, inputDir string) error {
	if isDirectoryPath(s.value) {
		dir, err := expandDiffDir(s.value)
		if err != nil {
			return fmt.Errorf("pro diff --%s %q is not available over MCP: %w", s.name, s.value, err)
		}
		return refuseLocalPath(pathRead, flagSetting{name: s.name, value: dir}, inputDir)
	}
	if pinnedProfile == "" {
		return fmt.Errorf("pro diff --%s %q names a config profile, and this server was started without one: over MCP each side must be a backup directory inside --input-dir", s.name, s.value)
	}
	if s.value != pinnedProfile {
		return fmt.Errorf("pro diff --%s %q names a config profile other than %q, the one this server is pinned to: over MCP each side must be that profile or a backup directory inside --input-dir", s.name, s.value, pinnedProfile)
	}
	return nil
}

// refuseLocalPath allows a read-side path only when it exists and resolves
// inside inputDir. The child opens the path again after this check, so a
// symlink swapped in between is not caught.
func refuseLocalPath(use localPathUse, s flagSetting, inputDir string) error {
	if use != pathRead || inputDir == "" {
		err := fmt.Errorf("flag --%s is not available over MCP: it %s a path on the machine running this server, which the connecting model must not choose", s.name, use)
		switch {
		case use == pathRead:
			err = fmt.Errorf("%w; the administrator can allow reads from one directory with 'mcp serve --input-dir <dir>'", err)
		case s.name == "output":
			err = fmt.Errorf("%w; the global -o <format> flag (json, yaml, table, csv) is still accepted", err)
		}
		return err
	}
	if s.value == "" {
		return fmt.Errorf("flag --%s names no path; over MCP it must name an existing path inside the input directory %s", s.name, inputDir)
	}
	resolved, err := filepath.Abs(s.value)
	if err == nil {
		resolved, err = filepath.EvalSymlinks(resolved)
	}
	if err != nil {
		return fmt.Errorf("flag --%s %q cannot be used over MCP: %v; it must name an existing path inside the input directory %s", s.name, s.value, err, inputDir)
	}
	if !insideDir(inputDir, resolved) {
		err := fmt.Errorf("flag --%s %q is not available over MCP: it resolves to %s, outside the input directory %s", s.name, s.value, resolved, inputDir)
		if !filepath.IsAbs(s.value) {
			err = fmt.Errorf("%w; a relative path resolves against the directory this server was started in, not the input directory, so pass an absolute path inside it", err)
		}
		return err
	}
	return nil
}

// insideDir reports whether path is dir or lies beneath it. Both must be
// absolute and symlink-resolved.
func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// refuseMCPChildReadOutsideInputDir refuses a file a command found by listing
// a directory when it resolves outside the input directory of the `mcp serve`
// that spawned this process. Outside an MCP child it allows everything.
func refuseMCPChildReadOutsideInputDir(path string) error {
	inputDir := os.Getenv(mcpInputDirEnvVar)
	if os.Getenv(mcpChildEnvVar) != "1" || inputDir == "" {
		return nil
	}
	resolved, err := filepath.Abs(path)
	if err == nil {
		resolved, err = filepath.EvalSymlinks(resolved)
	}
	if err != nil {
		return fmt.Errorf("%s is %w: %v", path, errMCPChildReadRefused, err)
	}
	if !insideDir(inputDir, resolved) {
		return fmt.Errorf("%s is %w: it resolves to %s, outside the input directory %s", path, errMCPChildReadRefused, resolved, inputDir)
	}
	return nil
}

var errMCPChildReadRefused = errors.New("not readable over MCP")

var errCompletionOverMCP = errors.New("shell completion is not available over MCP: it runs another command's completion without the flag checks every command gets; use list_commands or --help instead")

func isBlockedChildFlag(arg string) bool {
	if !strings.HasPrefix(arg, "--") {
		return false
	}
	// Compare the flag NAME, so --profile=x is judged as --profile.
	name, _, _ := strings.Cut(arg, "=")
	for _, f := range blockedChildFlagPrefixes {
		if strings.HasPrefix(name, f) {
			return true
		}
	}
	return false
}

// buildChildArgs validates a model-supplied command and returns the full
// argument list for the child invocation: the server's pinned profile and an
// enforced --no-input, then the model's args with any --no-input of its own
// dropped. The refusal is judged on what cobra resolves that argv to.
func buildChildArgs(serverProfile string, args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("args must not be empty; provide a command such as [\"pro\",\"computers\",\"list\"]")
	}
	if slices.ContainsFunc(args, isCompletionRequest) {
		return nil, errCompletionOverMCP
	}

	childArgs := make([]string, 0, len(args)+3)
	injected := 1
	if serverProfile != "" {
		childArgs = append(childArgs, "--profile", serverProfile)
		injected++
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

	inv := resolveChildInvocation(childArgs)
	if inv.path == "" {
		return childArgs, nil
	}
	// The injected flags lead the argv, so they are the first settings; a
	// model-supplied --profile naming the pinned profile is still refused.
	inv.settings = inv.settings[min(injected, len(inv.settings)):]
	if err := refuseOverMCP(inv, serverProfile); err != nil {
		return nil, err
	}
	return childArgs, nil
}

// printMCPStartupHints writes advisory hints to w when the server starts with a
// configuration that will cause a tool call to fail. Only called when !noHints.
func printMCPStartupHints(w io.Writer, cfg *config.Config) {
	if cfg == nil || cfg.ReportDirPath() == "" {
		_, _ = fmt.Fprintf(w, "hint: no report directory configured — generate_report will refuse until you run:\n      jamf-cli config set-report-dir <dir>\n")
	}
}

// reportFileName derives the HTML report's filename from the pinned profile and
// a UTC timestamp. It takes no title, so a model-supplied string cannot reach a
// path. The profile segment goes through protectFileNameSafe, so whatever an
// administrator named the profile stays one path segment inside the report dir.
func reportFileName(serverProfile string, now time.Time) string {
	name := serverProfile
	if name == "" {
		name = "default"
	}
	return fmt.Sprintf("jamf-report-%s-%s.html",
		protectFileNameSafe(name), now.UTC().Format("20060102T150405Z"))
}

const reportDirHint = "Set one with: jamf-cli config set-report-dir <dir>"

// resolveReportDir returns the directory reports are written to, or a refusal.
// Every failure here is returned before any child process starts. A missing
// directory is refused rather than created: `pro setup` does the MkdirAll when
// the administrator names one, and a typo'd report-dir silently materialising a
// directory tree is worse than an error.
func resolveReportDir(cfg *config.Config) (string, error) {
	dir := cfg.ReportDirPath()
	if dir == "" {
		return "", fmt.Errorf("no report directory is configured, and the MCP server has no destination parameter to fall back on. %s", reportDirHint)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("report directory %s is not accessible: %w. Create it, or choose another. %s", dir, err, reportDirHint)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("report directory %s is not a directory. %s", dir, reportDirHint)
	}
	if err := checkReportDirMode(dir, info); err != nil {
		return "", fmt.Errorf("%w. %s", err, reportDirHint)
	}
	// A directory that passes every check above and still cannot be written to
	// surfaces as a bare "permission denied" from OpenFile, inside a model
	// conversation, without the hint every other refusal on this path carries.
	if err := checkReportDirWritable(dir); err != nil {
		return "", fmt.Errorf("report directory %s is not writable: %w. %s", dir, err, reportDirHint)
	}
	return dir, nil
}

// checkReportDirMode refuses a report directory that others can write to.
//
// os.MkdirAll(dir, 0700) leaves an existing directory's mode untouched, so
// createReportFile's 0600 is correct and irrelevant: inside a 0777 directory a
// local user can pre-create the deterministic report filename to deny
// generation — O_EXCL then errors — or substitute their own file for the
// administrator to open and forward.
func checkReportDirMode(dir string, info os.FileInfo) error {
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("report directory %s is group- or world-writable (mode %04o), so another local user could replace a report before it is shared; run: chmod go-w %s",
			dir, perm, dir)
	}
	return nil
}

// checkReportDirWritable proves the directory is writable by this process, by
// creating and removing a file rather than by reasoning about the mode bits:
// ownership, ACLs and read-only mounts all make a permissive mode a lie.
func checkReportDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".jamf-report-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// createReportFile opens the report file with O_EXCL, so a name collision — two
// reports generated inside the same second — errors rather than overwriting a
// report the administrator may already have shared.
//
// 0600 is the file's own mode and says nothing about the directory holding it:
// MkdirAll leaves an existing directory's mode alone, which is why
// resolveReportDir checks it separately.
func createReportFile(dir, name string) (*os.File, error) {
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating report file %s: %w", path, err)
	}
	return f, nil
}

// buildReportArgs is the dashboard invocation for a model-requested report.
//
// No --include-profile: the server pins one profile at launch, so an MCP report
// covers that profile. Cross-product reports stay a CLI capability. No
// --out-file either — stdout is a file this server opened, which is what keeps
// the flag on blockedChildFlags.
//
// A blank value is dropped so an omitted field means "use the dashboard
// default" rather than passing an empty one. A value that looks like a flag
// (begins with "-") is dropped too: it can only have come from the model, and a
// report has no field whose value is a flag, so emitting it as a bare token is
// the one way it could reach the child as a flag rather than a value.
func buildReportArgs(in generateReportInput) ([]string, error) {
	args := []string{"dashboard"}
	if v := strings.TrimSpace(in.Title); v != "" {
		if strings.HasPrefix(v, "-") {
			return nil, fmt.Errorf("title %q cannot begin with \"-\": it would reach the report generator as a flag rather than as a value", in.Title)
		}
		args = append(args, "--title", in.Title)
	}
	for _, g := range in.SmartGroups {
		v := strings.TrimSpace(g)
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "-") {
			return nil, fmt.Errorf("smart group name %q cannot begin with \"-\": it would reach the report generator as a flag rather than as a value", g)
		}
		args = append(args, "--smart-groups", g)
	}
	if in.Full {
		args = append(args, "--full")
	}
	return args, nil
}

// refuseReportThroughRunCommand refuses `dashboard` on the run_command path.
//
// run_command returns the child's stdout as tool text and the dashboard writes
// a 320–800 KB HTML document to stdout, so one tool result would carry
// 80k–200k tokens of markup with the collector's stderr warnings spliced
// through it — the exact cost generate_report exists to avoid, and with no file
// left behind to share. list_commands advertises `dashboard` like any other
// command, so the model will reach for it unless told.
//
// Refused here rather than in buildChildArgs, which runReportChild shares and
// which must still be able to spawn it.
func refuseReportThroughRunCommand(args []string) error {
	if resolveChildInvocation(args).path != dashboardPath {
		return nil
	}
	return errors.New("use the generate_report tool for HTML reports: run_command returns stdout as tool text, " +
		"and the dashboard writes a 320-800 KB HTML document there (80k-200k tokens) with no file left to share")
}

// resolveMCPServerProfile resolves the profile every child is pinned to, using
// the same chain as the rest of the CLI: -p, then JAMF_PROFILE, then the
// configured default.
//
// It returns an empty name — meaning "let the child resolve the flag/env
// credential chain" — only when this invocation carries credentials of its own.
// Otherwise it refuses to start: a server with no credentials and no profile
// answers every tool call with the same auth error, which the model reports as
// a Jamf problem rather than as a misconfigured server.
func resolveMCPServerProfile(cfg *config.Config) (string, error) {
	name := profile
	if name == "" {
		name = os.Getenv("JAMF_PROFILE")
	}
	if name == "" && cfg != nil {
		name = cfg.DefaultProfile
	}
	if name != "" {
		return name, nil
	}
	if dashboardHasInvocationCredentials() {
		return "", nil
	}
	return "", errors.New("mcp serve has no profile to pin: pass -p/--profile, set JAMF_PROFILE, " +
		"or configure a default with 'jamf-cli config set-default'\n\n" +
		"Every tool call runs as a child of this server and uses the profile it was started with, " +
		"so the server will not start without one")
}

// reportWarningTailBytes caps how much of the child's stderr reaches the model.
// The dashboard warns per partial failure, so a child failing once per device
// could otherwise spend the whole conversation's context on repeated warnings.
const reportWarningTailBytes = 4 << 10

// tailWarnings truncates the child's stderr to its last reportWarningTailBytes.
// The tail rather than the head, because the last line is the one that explains
// the exit.
func tailWarnings(b []byte) string {
	if len(b) <= reportWarningTailBytes {
		return string(b)
	}
	return "(earlier warnings omitted)\n" + string(b[len(b)-reportWarningTailBytes:])
}

// runReportChild generates an HTML report by re-invoking this binary as
// `jamf-cli dashboard` and returns its path, size, and warnings — never the
// HTML, which at 320–800 KB is 80k–200k tokens, and which truncation would
// turn into a corrupt file rather than a shorter report.
//
// This cannot reuse runChild: that calls CombinedOutput(), which would
// interleave the dashboard's partial-failure warnings into the middle of the
// HTML document. The two streams are kept apart — stdout is the report file,
// stderr is a buffer.
//
// It loads config itself because newMCPCmd and newMCPServeCmd take no
// arguments, so there is no CLIContext to thread.
func runReportChild(ctx context.Context, executable, serverProfile string, in generateReportInput, now time.Time) *mcp.CallToolResult {
	cfg, err := config.Load()
	if err != nil {
		return errorResult(fmt.Sprintf("reading config: %v", err))
	}
	dir, err := resolveReportDir(cfg)
	if err != nil {
		return errorResult(err.Error())
	}
	reportArgs, err := buildReportArgs(in)
	if err != nil {
		return errorResult(err.Error())
	}
	childArgs, err := buildChildArgs(serverProfile, reportArgs)
	if err != nil {
		return errorResult(err.Error())
	}

	f, err := createReportFile(dir, reportFileName(serverProfile, now))
	if err != nil {
		return errorResult(err.Error())
	}
	path := f.Name()

	var stderr bytes.Buffer
	child := exec.CommandContext(ctx, executable, childArgs...)
	child.Env = pinnedChildEnv(serverProfile)
	child.Stdout = f
	child.Stderr = &stderr

	runErr := child.Run()
	closeErr := f.Close()
	warnings := strings.TrimSpace(tailWarnings(stderr.Bytes()))

	// Exit 7 means the document is complete and carries its own
	// incomplete-sections banner in its header — the dashboard writes the whole
	// HTML and then reports that some sections are missing. Any other non-zero
	// exit may have truncated the document, and a partial HTML document is not
	// a smaller report.
	//
	// Deleting it on 7 made the banner undeliverable by the only path that
	// produces one: the model was told "report generation failed: exit status 7"
	// and the report directory was left empty.
	partial := isPartialFailureExit(runErr)

	if runErr != nil && !partial {
		_ = os.Remove(path)
		text := fmt.Sprintf("report generation failed: %v", runErr)
		if warnings != "" {
			text += "\n\n" + warnings
		}
		return errorResult(text)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return errorResult(fmt.Sprintf("writing report %s: %v", path, closeErr))
	}

	var size int64
	if info, statErr := os.Stat(path); statErr == nil {
		size = info.Size()
	}
	// A zero-byte report is a failed generation reported as a success. It is
	// what an inherited JAMF_CLI_ARGS --out-file produced, and it is
	// indistinguishable from a working report in the text below.
	if size == 0 {
		_ = os.Remove(path)
		text := fmt.Sprintf("report generation produced no output: %s was written empty", path)
		if warnings != "" {
			text += "\n\n" + warnings
		}
		return errorResult(text)
	}

	text := fmt.Sprintf("Report written to %s (%d bytes). Open or share that file; its contents are not returned here.", path, size)
	if partial {
		text += "\n\nThe report is complete as a document but some sections could not be collected." +
			" It is marked incomplete in its own header, which names them — tell the administrator that," +
			" and that the figures shown do not cover the whole fleet."
	}
	if warnings != "" {
		text += "\n\nWarnings during generation (some sections may be incomplete):\n" + warnings
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// isPartialFailureExit reports whether err is the child exiting with
// exitcode.PartialFailure. Matched on the exit status rather than on the error
// text, which is "exit status 7" and carries nothing else.
func isPartialFailureExit(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == exitcode.PartialFailure
}
