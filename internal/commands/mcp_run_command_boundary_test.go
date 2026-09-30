// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Each argv is a spelling cobra resolves to a refused command: an alias, a
// persistent flag ahead of the command path, or a flag inside it.
var refusedCommandSpellings = []struct {
	args []string
	want string
}{
	{[]string{"cfg", "set-default", "x"}, "jamf-cli config set-default"},
	{[]string{"cfg", "add-profile"}, "jamf-cli config add-profile"},
	{[]string{"cfg", "remove-profile", "x"}, "jamf-cli config remove-profile"},
	{[]string{"cfg", "set-report-dir", "/x"}, "jamf-cli config set-report-dir"},
	{[]string{"--no-hints", "config", "set-default", "x"}, "jamf-cli config set-default"},
	{[]string{"--no-color", "multi", "--filter", "*", "--", "pro", "computers", "list"}, "jamf-cli multi"},
	{[]string{"-o", "json", "multi"}, "jamf-cli multi"},
	{[]string{"-q", "pro", "backup", "--output", "/x"}, "jamf-cli pro backup"},
	{[]string{"pro", "-q", "backup"}, "jamf-cli pro backup"},
	{[]string{"-v", "protect", "backup", "--output", "/x"}, "jamf-cli protect backup"},
}

func TestBuildChildArgs_RefusesEveryResolvedSpellingOfARefusedCommand(t *testing.T) {
	root := NewRootCmd("test", "test", "test", "test")
	for _, tc := range refusedCommandSpellings {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			found, _, err := root.Find(tc.args)
			if err != nil || found.CommandPath() != tc.want {
				t.Fatalf("precondition: cobra's Find resolves %q to %v (err %v), want %q", tc.args, found, err, tc.want)
			}
			traversed, _, err := root.Traverse(tc.args)
			if err != nil || traversed.CommandPath() != tc.want {
				t.Fatalf("precondition: cobra's Traverse resolves %q to %v (err %v), want %q", tc.args, traversed, err, tc.want)
			}
			if child, err := buildChildArgs("pinned", tc.args); err == nil {
				t.Errorf("buildChildArgs accepted %q, which cobra runs as %q; child argv %q", tc.args, tc.want, child)
			}
		})
	}
}

func TestRefuseReportThroughRunCommand_RefusesFlagFirstDashboard(t *testing.T) {
	for _, args := range [][]string{
		{"--no-color", "dashboard"},
		{"-q", "db"},
	} {
		if err := refuseReportThroughRunCommand(args); err == nil {
			t.Errorf("refuseReportThroughRunCommand accepted %q, which cobra runs as `jamf-cli dashboard`", args)
		}
	}
}

// `pro diff --source/--target` each take a backup directory or a config
// profile name, and a profile name makes the child resolve that profile's URL
// and credential. Over MCP only the pinned profile may be reached that way.

func TestBuildChildArgs_RefusesDiffAgainstAForeignProfile(t *testing.T) {
	refused := [][]string{
		{"pro", "diff", "--source", "/var/empty", "--target", "other"},
		{"pro", "diff", "--source", "prod", "--target", "other"},
		{"pro", "diff", "--source", "other", "--target", "./backup"},
		{"pro", "diff", "--source=/var/empty", "--target=other"},
		{"pro", "diff", "--target", "other", "--source", "prod", "--resources", "scripts"},
	}
	for _, args := range refused {
		if _, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("buildChildArgs(%q, %v) = nil error; a diff side naming a profile other than the pinned one must be refused", "prod", args)
		}
	}
}

func TestBuildChildArgs_AllowsDiffWithinThePinnedProfile(t *testing.T) {
	allowed := [][]string{
		{"pro", "diff", "--source", "/backups/a", "--target", "./backups/b"},
		{"pro", "diff", "--source", "prod", "--target", "/backups/a"},
		{"pro", "diff", "--source=~/backups/a", "--target=prod"},
	}
	for _, args := range allowed {
		if _, err := buildChildArgs("prod", args); err != nil {
			t.Errorf("buildChildArgs(%q, %v) = %v; a diff between directories or against the pinned profile should be allowed", "prod", args, err)
		}
	}
}

// A run_command child reads whatever local path the model names in an input
// flag, and -n / -vvv print the request body to stderr, which runChild returns
// as the tool result. The model must not obtain an arbitrary local file's bytes.

const inputFileMarker = "F5-MARKER-a1b2c3-PRIVATE-KEY-BYTES"

func TestBuildChildArgs_RefusesLocalInputPaths(t *testing.T) {
	refused := [][]string{
		{"pro", "scripts", "create", "--script-file", "/etc/passwd", "-n"},
		{"pro", "scripts", "create", "--script-file=/etc/passwd"},
		{"pro", "classic-policies", "create", "--from-file", "/etc/passwd"},
	}
	for _, args := range refused {
		if _, err := buildChildArgs("", args); err == nil {
			t.Errorf("buildChildArgs accepted %v; a local input path must be refused over MCP", args)
		}
	}
}

func TestRunChild_DoesNotReturnLocalFileBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the jamf-cli binary")
	}

	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "token") {
			_, _ = fmt.Fprint(w, `{"access_token":"fake","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":"1","href":"x"}`)
	}))
	defer srv.Close()

	bin := filepath.Join(t.TempDir(), "jamf-cli")
	build := exec.Command("go", "build", "-o", bin, "github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"+inputFileMarker+"\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("JAMF_URL", srv.URL)
	t.Setenv("JAMF_CLIENT_ID", "fakeid")
	t.Setenv("JAMF_CLIENT_SECRET", "fakesecret")
	t.Setenv("JAMF_PROFILE", "")

	cases := [][]string{
		{"pro", "scripts", "create", "--script-file", secret, "-n"},
		{"pro", "scripts", "create", "--script-file", secret, "-vvv"},
		{"pro", "classic-policies", "create", "--from-file", secret, "-n"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[len(args)-1:], ""), func(t *testing.T) {
			mu.Lock()
			seen = nil
			mu.Unlock()

			res := runChild(context.Background(), bin, "", args)
			text := mcpResultText(res)

			mu.Lock()
			t.Logf("args=%v isError=%v server saw %d request(s): %v\n%s", args, res.IsError, len(seen), seen, text)
			mu.Unlock()

			if strings.Contains(text, inputFileMarker) {
				t.Errorf("run_command returned the local file's bytes to the model:\n%s", text)
			}
		})
	}
}

// The path deliberately carries no 'p', so a short-token rule keyed on the
// --profile shorthand cannot mask the gap for an attached -O/path.
const modelChosenPath = "/Users/admin/.zshrc"

func TestBuildChildArgs_RejectsLocalOutputPathFlags(t *testing.T) {
	cases := [][]string{
		{"pro", "packages", "export", "--save-to", modelChosenPath},
		{"pro", "packages", "export", "--save-to=" + modelChosenPath},
		{"pro", "packages", "export", "-O", modelChosenPath},
		{"pro", "packages", "export", "-O" + modelChosenPath},
		{"pro", "scripts", "download", "1", "--save-to", modelChosenPath},
		{"protect", "downloads", "installer", "--output", modelChosenPath},
		{"protect", "downloads", "installer", "-O", modelChosenPath},
		{"protect", "plans", "config-profile", "x", "--output", modelChosenPath},
		{"pro", "jamf-cloud-distribution-service", "download", "f.pkg", "--output", modelChosenPath},
	}
	for _, args := range cases {
		if got, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("buildChildArgs(%q) accepted a model-chosen local output path; child argv: %q", args, got)
		}
	}
}

// isLocalOutputPathFlag names every flag in the tree whose value is a path the
// command writes to (or, for sync --dir, writes into and may delete from).
func isLocalOutputPathFlag(f *pflag.Flag) bool {
	switch f.Name {
	case "save-to", "out-file":
		return true
	case "output":
		return !strings.HasPrefix(strings.ToLower(f.Usage), "output format")
	case "dir":
		return strings.Contains(f.Usage, "sync into")
	}
	return false
}

func TestBuildChildArgs_RejectsEveryLocalOutputPathFlagInTree(t *testing.T) {
	root := NewRootCmd("test", "t", "t", "t")
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != root {
			path := strings.Fields(strings.TrimPrefix(c.CommandPath(), root.Name()+" "))
			c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
				if !isLocalOutputPathFlag(f) {
					return
				}
				forms := [][]string{{"--" + f.Name, modelChosenPath}}
				if f.Shorthand != "" {
					forms = append(forms, []string{"-" + f.Shorthand, modelChosenPath})
				}
				for _, form := range forms {
					checked++
					args := append(append([]string{}, path...), form...)
					if _, err := buildChildArgs("prod", args); err == nil {
						t.Errorf("run_command would forward %q: %s writes to a model-chosen path", args, form[0])
					}
				}
			})
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	if checked < 40 {
		t.Fatalf("walk checked only %d output-path flag forms; the classifier has stopped matching the tree", checked)
	}
}

// jcds sync --dir is a model-chosen local directory, and --delete removes every
// file in it that the distribution point does not carry.
var jcdsSyncMounts = [][]string{
	{"pro", "jcds", "sync"},
	{"pro", "jamf-cloud-distribution-service", "sync"},
	{"pro", "packages", "sync"},
}

func TestBuildChildArgs_RefusesJcdsSyncLocalDestination(t *testing.T) {
	for _, mount := range jcdsSyncMounts {
		args := append(append([]string{}, mount...), "--dir", "/x", "--delete")
		if got, err := buildChildArgs("prod", args); err == nil {
			t.Errorf("%v was accepted as %v; --dir is a model-chosen local directory and --delete removes every file in it", args, got)
		}
	}
}

func TestMCPChild_JcdsSyncDeleteLeavesOperatorFilesInPlace(t *testing.T) {
	for _, mount := range jcdsSyncMounts {
		t.Run(strings.Join(mount, " "), func(t *testing.T) {
			isolated := t.TempDir()
			victim := filepath.Join(isolated, "victim")
			if err := os.MkdirAll(victim, 0o755); err != nil {
				t.Fatal(err)
			}
			names := []string{"notes.txt", "id_ed25519", "project.go"}
			for _, n := range names {
				if err := os.WriteFile(filepath.Join(victim, n), []byte("operator data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/jcds/files", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode([]jcdsFileData{})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			resetGlobals()
			t.Setenv("HOME", isolated)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolated, "config"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(isolated, "cache"))
			t.Setenv("JAMF_PROFILE", "")
			t.Setenv("JAMF_URL", srv.URL)
			t.Setenv("JAMF_TOKEN", "fake-token")
			t.Setenv("JAMF_CLIENT_ID", "")
			t.Setenv("JAMF_CLIENT_SECRET", "")
			t.Setenv("JAMF_CLI_ARGS", "")

			args := append(append([]string{}, mount...), "--dir", victim, "--delete")
			childArgs, err := buildChildArgs("", args)
			if err != nil {
				return
			}

			root := NewRootCmd("test", "abc123", "2024-01-01", "unknown")
			root.SetArgs(childArgs)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			runErr := root.Execute()

			var gone []string
			for _, n := range names {
				if _, err := os.Stat(filepath.Join(victim, n)); os.IsNotExist(err) {
					gone = append(gone, n)
				}
			}
			if len(gone) > 0 {
				t.Errorf("MCP child %v (exit err: %v) deleted operator files %v with no confirmation", childArgs, runErr, gone)
			}
		})
	}
}
