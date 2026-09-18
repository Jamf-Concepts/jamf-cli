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
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
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
  - list_commands   : the full command catalog (names, descriptions, flags)
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
  }

The profile is resolved once at startup — -p/--profile, then JAMF_PROFILE, then
the configured default — and every child is pinned to it. The server refuses to
start with no profile and no credentials in the environment, rather than
answering every tool call with the same auth error.

generate_report collects in two tiers, and the connecting AI is told to run the
fast one first and ask before the full one. So an administrator should expect a
question about a "full report" rather than a long silence; what each tier costs
is in 'jamf-cli dashboard --help'.

run_command refuses anything that picks its own instance or destination: any
flag naming a profile, URL, token, tenant, environment or output file, plus
'multi', the config write subcommands and the two 'backup' commands. It also
refuses 'dashboard', because it returns a command's stdout as text and the
report is a 320-800 KB document — generate_report writes that to a file
instead.`,
		Args: refuseStrayPositionals,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			if !noHints {
				printMCPStartupHints(cmd.ErrOrStderr(), cfg)
			}

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
					"was started with, so any flag naming a profile, URL, token, tenant, " +
					"environment or output file is rejected, as are 'multi', the config write " +
					"subcommands and the backup commands, which choose their own target or " +
					"destination. Use generate_report rather than 'dashboard': this tool returns " +
					"stdout as text and the dashboard writes a 320-800 KB HTML document there. " +
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
// JAMF_CLI_MCP=1 is appended so the child knows it is an MCP child.
func childEnv() []string {
	env := os.Environ()
	kept := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name == "JAMF_CLI_ARGS" {
			continue
		}
		kept = append(kept, kv)
	}
	return append(kept, "JAMF_CLI_MCP=1")
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
	child.Env = childEnv()
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
// A deny-list cannot be complete — 362 commands declare --from-file alone — so
// this is a floor rather than the boundary. blockedChildCommandPaths carries
// the namespaces no flag list can pin.
var blockedChildFlagPrefixes = []string{
	"--profile",
	"--include-profile",
	"--url",
	"--token",
	"--tenant-id",
	"--environment-id",
	"--out-file",
}

// blockedChildCommandPaths are command paths a model must not run, as
// space-joined prefixes of the argument list.
//
// These resolve their own target or destination, so no flag list can pin them:
// `multi` takes its own --profiles and fans out across instances, and the
// config write subcommands persist a new default profile, a new credential or a
// new report directory to disk — after which every later child is pointed
// somewhere the operator never chose. `dashboard` is refused on the
// run_command path only, by the tool handler, because generate_report shares
// buildChildArgs and must still be able to spawn it.
var blockedChildCommandPaths = [][]string{
	{"multi"},
	{"config", "set-default"},
	{"config", "add-profile"},
	{"config", "remove-profile"},
	{"config", "set-report-dir"},
	// Both backup commands take --output as a destination *directory* rather
	// than an output format, so they write a tree wherever the model says.
	{"pro", "backup"},
	{"protect", "backup"},
}

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

// blockedChildCommand returns the refused command path when args begins with
// one, or nil. Positional matching only: a flag cannot name a command, and the
// first tokens of a jamf-cli invocation are its command path.
func blockedChildCommand(args []string) []string {
	for _, path := range blockedChildCommandPaths {
		if len(args) < len(path) {
			continue
		}
		match := true
		for i, seg := range path {
			if args[i] != seg {
				match = false
				break
			}
		}
		if match {
			return path
		}
	}
	return nil
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
	if path := blockedChildCommand(args); path != nil {
		return nil, fmt.Errorf("command %q is not available over MCP: it selects its own instance or writes to a path of its own, so the profile this server was started with cannot pin it", strings.Join(path, " "))
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
	if len(args) == 0 {
		return nil
	}
	if args[0] != "dashboard" && args[0] != "db" {
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
	child.Env = childEnv()
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
