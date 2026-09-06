// Copyright 2026, Jamf Software LLC

package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

func TestResolveVersion(t *testing.T) {
	buildInfo := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			info := &debug.BuildInfo{}
			info.Main.Version = v
			return info, true
		}
	}
	noBuildInfo := func() (*debug.BuildInfo, bool) { return nil, false }

	tests := []struct {
		name           string
		ldflagsVersion string
		readBuildInfo  func() (*debug.BuildInfo, bool)
		want           string
	}{
		{
			name:           "ldflags version wins",
			ldflagsVersion: "v1.25.2",
			readBuildInfo:  buildInfo("v1.0.0"),
			want:           "v1.25.2",
		},
		{
			name:           "go install falls back to the module version",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.26.0",
		},
		{
			name:           "local go build stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo("(devel)"),
			want:           "dev",
		},
		{
			name:           "empty module version stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  buildInfo(""),
			want:           "dev",
		},
		{
			name:           "missing build info stays dev",
			ldflagsVersion: "dev",
			readBuildInfo:  noBuildInfo,
			want:           "dev",
		},
		{
			name:           "empty ldflags version also falls back",
			ldflagsVersion: "",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.26.0",
		},
		{
			name:           "git describe version is left alone",
			ldflagsVersion: "v1.25.2-3-gabc1234-dirty",
			readBuildInfo:  buildInfo("v1.26.0"),
			want:           "v1.25.2-3-gabc1234-dirty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflagsVersion, tt.readBuildInfo); got != tt.want {
				t.Errorf("resolveVersion(%q) = %q, want %q", tt.ldflagsVersion, got, tt.want)
			}
		})
	}
}

func TestInjectEnvArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string
		want []string
	}{
		{
			name: "empty env leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "",
			want: []string{"jamf-cli", "pro", "computers", "list"},
		},
		{
			name: "single flag is prepended after program name",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet", "pro", "computers", "list"},
		},
		{
			name: "multiple flags are prepended in order",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "--quiet --no-input --no-color",
			want: []string{"jamf-cli", "--quiet", "--no-input", "--no-color", "pro", "computers", "list"},
		},
		{
			name: "extra whitespace in env is ignored",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "  --quiet   --no-input  ",
			want: []string{"jamf-cli", "--quiet", "--no-input", "pro", "computers", "list"},
		},
		{
			name: "user args are not clobbered",
			args: []string{"jamf-cli", "--output", "json", "pro", "computers", "list"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet", "--output", "json", "pro", "computers", "list"},
		},
		{
			name: "env flags injected with no trailing user args",
			args: []string{"jamf-cli"},
			env:  "--quiet",
			want: []string{"jamf-cli", "--quiet"},
		},
		{
			name: "whitespace-only env leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  "   ",
			want: []string{"jamf-cli", "pro", "computers", "list"},
		},
		{
			name: "quoted value is treated as single token",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  `--profile "My CI Profile"`,
			want: []string{"jamf-cli", "--profile", "My CI Profile", "pro", "computers", "list"},
		},
		{
			name: "invalid shlex leaves args unchanged",
			args: []string{"jamf-cli", "pro", "computers", "list"},
			env:  `--profile "unterminated`,
			want: []string{"jamf-cli", "pro", "computers", "list"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectEnvArgs(tt.args, tt.env)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// silenceOutput points os.Stdout and os.Stderr at a pipe for the duration of
// one test. run reports through both, and its callee FormatError writes to
// os.Stdout directly rather than through cobra's writers, so a cobra writer
// alone would not cover it.
func silenceOutput(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(drained)
	}()
	t.Cleanup(func() {
		os.Stdout, os.Stderr = origOut, origErr
		_ = w.Close()
		<-drained
		_ = r.Close()
	})
}

// TestRunClassifiesTheExitCode is the only test that exercises the sequence
// main runs. The four ClassifyError tests in internal/commands call Execute and
// then call ClassifyError themselves, which verifies the function and re-creates
// the wiring around it — so deleting the ClassifyError call from run left the
// whole suite green while every usage exit code in the CLI reverted to 1.
//
// `nosuchcommand` is the invocation that pins it. Cobra reports an unknown root
// command from legacyArgs inside Find, so it is the one usage error that reaches
// no validator and is coded by ClassifyError alone. It also needs no
// credentials, since Find fails before PersistentPreRunE runs.
//
// The general-failure row is what stops the test passing against a run that
// returns a constant, and it needs credentials that resolve: the directory check
// it lands on sits in RunE, after PersistentPreRunE.
func TestRunClassifiesTheExitCode(t *testing.T) {
	t.Setenv("JAMF_URL", "https://test.jamfcloud.com")
	t.Setenv("JAMF_TOKEN", "test-token")
	t.Setenv("JAMF_CLIENT_ID", "")
	t.Setenv("JAMF_CLIENT_SECRET", "")
	t.Setenv("JAMF_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// A directory, so `pro packages upload` fails with the caller's own path at
	// character zero of a plain error. That has to stay a general failure; it is
	// what ruled out matching cobra's "accepts " prefix.
	dir := filepath.Join(t.TempDir(), "accepts x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		argv []string
		want int
	}{
		{"unknown root command", []string{"jamf-cli", "nosuchcommand"}, exitcode.Usage},
		{"a plain failure carrying a path", []string{"jamf-cli", "pro", "packages", "upload", "--file", dir}, exitcode.General},
	} {
		t.Run(tc.name, func(t *testing.T) {
			silenceOutput(t)
			if got := run(tc.argv, ""); got != tc.want {
				t.Errorf("run(%v) = %d, want %d", tc.argv[1:], got, tc.want)
			}
		})
	}
}

// TestRunReportsSuccess pins the nil-error branch, which is the one place run
// can return a code without consulting the error chain at all.
func TestRunReportsSuccess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	silenceOutput(t)
	if got := run([]string{"jamf-cli", "version"}, ""); got != exitcode.Success {
		t.Errorf("run(version) = %d, want %d", got, exitcode.Success)
	}
}

// TestRunAppliesJAMFCLIArgs pins that run still prepends JAMF_CLI_ARGS. main no
// longer assigns the result back over os.Args, so the injected flags reach cobra
// through SetArgs or not at all — and a flag that silently stopped being applied
// would leave every documented CI invocation running with different defaults.
func TestRunAppliesJAMFCLIArgs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	silenceOutput(t)
	// --nosuchflag is rejected by pflag, which is only reachable if the injected
	// argument arrived at all; without it `version` succeeds.
	if got := run([]string{"jamf-cli", "version"}, "--nosuchflag"); got != exitcode.Usage {
		t.Errorf("run(version) with JAMF_CLI_ARGS=--nosuchflag = %d, want %d", got, exitcode.Usage)
	}
}

// captureOutput is silenceOutput's sibling: it points os.Stdout and os.Stderr
// at a pipe and returns what was written, so a test can assert how run
// PRESENTED an error rather than only what error Execute returned. That
// distinction is the point — every existing test here inspects the exit code
// or the error value, and the rendering is a separate decision made after both.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()

	fn()

	os.Stdout, os.Stderr = origOut, origErr
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// TestRunRendersAStrayPositionalRefusal pins how run presents an Args refusal,
// which no other test here reaches: cobra validates args before
// PersistentPreRunE, so outputFmt is still unresolved and the rendering is
// decided by errorFormat rather than by the resolved format.
//
// The test's stdout is a pipe, so this is the piped answer: the envelope, the
// same shape a RunE error gets when piped. That equality is the invariant —
// before this, a piped run answered two ways depending on which side of
// PersistentPreRunE the error came from, and an interactive run got the
// envelope for a typo a human had just made. The terminal half of the decision
// is covered by TestErrorFormat, which takes stdoutTTY as an argument because a
// pipe can never be a terminal.
func TestRunRendersAStrayPositionalRefusal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("JAMF_URL", "")
	t.Setenv("JAMF_TOKEN", "")
	t.Setenv("JAMF_PROFILE", "")

	var code int
	out := captureOutput(t, func() {
		code = run([]string{"jamf-cli", "pro", "diff", "somegarbage"}, "")
	})

	if code != exitcode.Usage {
		t.Errorf("exit code = %d, want %d (usage)", code, exitcode.Usage)
	}
	// The envelope, because the pipe is not a terminal.
	for _, want := range []string{`"error": "usage"`, `"exitCode": 2`, "takes no positional arguments", `"hint"`} {
		if !strings.Contains(out, want) {
			t.Errorf("piped refusal should carry %s, got:\n%s", want, out)
		}
	}
	// The required-flag remedy has to survive the presentation chain, not just
	// the error value: the Args validator pre-empts cobra's own flag check.
	for _, want := range []string{"source", "target"} {
		if !strings.Contains(out, want) {
			t.Errorf("piped refusal should still name the required flag %q, got:\n%s", want, out)
		}
	}
}
