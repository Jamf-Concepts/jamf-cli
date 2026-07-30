// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
)

// clearUpdateEnv neutralises every environment variable the update check
// consults so a developer's shell (or a CI runner) can't flip a result.
func clearUpdateEnv(t *testing.T) {
	t.Helper()
	for _, k := range updateCheckCIEnv {
		t.Setenv(k, "")
	}
	for _, k := range []string{"JAMF_CLI_NO_UPDATE_CHECK", "HOMEBREW_PREFIX", "GOBIN", "GOPATH"} {
		t.Setenv(k, "")
	}
}

// useTempConfigDir points config.ConfigPath() — and therefore the update cache
// — at a scratch directory.
func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// serveEndpoints redirects both release sources at test servers for the
// duration of the test.
func serveEndpoints(t *testing.T, proxy, github string) {
	t.Helper()
	origProxy, origGitHub := updateProxyBaseURL, updateGitHubBaseURL
	t.Cleanup(func() {
		updateProxyBaseURL, updateGitHubBaseURL = origProxy, origGitHub
	})
	if proxy != "" {
		updateProxyBaseURL = proxy
	}
	if github != "" {
		updateGitHubBaseURL = github
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"v1.25.2", "v1.25.2"},
		{"1.25.2", "v1.25.2"},
		{"  1.25.2  ", "v1.25.2"},
		{"dev", "dev"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsReleaseVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"exact tag", "v1.25.2", true},
		{"tag without v prefix", "1.25.2", true},
		{"minor-only tag", "v1.25", true},
		{"ldflags default", "dev", false},
		{"empty", "", false},
		{"git describe suffix", "v1.25.2-3-gabc1234", false},
		{"dirty tree", "v1.25.2-dirty", false},
		{"release candidate", "v1.26.0-rc.1", false},
		{"build metadata", "v1.25.2+abc123", false},
		{"not a version", "banana", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isReleaseVersion(tt.in); got != tt.want {
				t.Errorf("isReleaseVersion(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewerAvailable(t *testing.T) {
	tests := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"newer patch", "v1.25.1", "v1.25.2", true},
		{"newer minor", "v1.25.2", "v1.26.0", true},
		{"newer major", "v1.25.2", "v2.0.0", true},
		{"same version", "v1.25.2", "v1.25.2", false},
		{"older latest", "v1.26.0", "v1.25.2", false},
		{"double-digit minor sorts numerically", "v1.9.0", "v1.10.0", true},
		{"missing v prefix on both sides", "1.25.1", "1.25.2", true},
		{"dev build never notifies", "dev", "v1.26.0", false},
		{"git describe build never notifies", "v1.25.2-4-gdeadbee", "v1.26.0", false},
		{"prerelease latest is not offered", "v1.25.2", "v1.26.0-rc.1", false},
		{"garbage latest", "v1.25.2", "not-a-version", false},
		{"empty latest", "v1.25.2", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newerAvailable(tt.current, tt.latest); got != tt.want {
				t.Errorf("newerAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestUpdateStateFresh(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		state updateState
		want  bool
	}{
		{"zero value is stale", updateState{}, false},
		{
			name:  "recent success is fresh",
			state: updateState{CheckedAt: now.Add(-2 * time.Hour), LatestVersion: "v1.25.2"},
			want:  true,
		},
		{
			name:  "success past the day is stale",
			state: updateState{CheckedAt: now.Add(-25 * time.Hour), LatestVersion: "v1.25.2"},
			want:  false,
		},
		{
			name:  "recent failure cools down",
			state: updateState{CheckedAt: now.Add(-30 * time.Minute)},
			want:  true,
		},
		{
			name:  "failure retries after an hour",
			state: updateState{CheckedAt: now.Add(-90 * time.Minute)},
			want:  false,
		},
		{
			name:  "clock skew into the future counts as fresh",
			state: updateState{CheckedAt: now.Add(48 * time.Hour), LatestVersion: "v1.25.2"},
			want:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.fresh(now); got != tt.want {
				t.Errorf("fresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateCacheRoundTrip(t *testing.T) {
	useTempConfigDir(t)

	if got := readUpdateCache(); got.LatestVersion != "" || !got.CheckedAt.IsZero() {
		t.Fatalf("expected zero state with no cache file; got %+v", got)
	}

	want := updateState{
		CheckedAt:     time.Now().UTC().Truncate(time.Second),
		LatestVersion: "v1.26.0",
		ReleasedAt:    time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		ReleaseURL:    "https://example.test/tag/v1.26.0",
	}
	writeUpdateCache(want)

	got := readUpdateCache()
	if got.LatestVersion != want.LatestVersion || got.ReleaseURL != want.ReleaseURL {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, want)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) || !got.ReleasedAt.Equal(want.ReleasedAt) {
		t.Errorf("timestamps did not survive: got %+v, want %+v", got, want)
	}

	// The cache sits next to the config file and must not be world-readable:
	// it is created in a directory that also holds credential references.
	info, err := os.Stat(updateCachePath())
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %v, want 0600", perm)
	}
	if dir := filepath.Dir(updateCachePath()); dir != filepath.Dir(config.ConfigPath()) {
		t.Errorf("cache dir %q is not the config dir %q", dir, filepath.Dir(config.ConfigPath()))
	}
}

func TestReadUpdateCache_CorruptFileIsIgnored(t *testing.T) {
	useTempConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(updateCachePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(updateCachePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readUpdateCache(); got.LatestVersion != "" {
		t.Errorf("expected zero state for corrupt cache; got %+v", got)
	}
}

func TestFetchLatestFromProxy(t *testing.T) {
	var gotPath, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUA = r.URL.Path, r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Version": "v1.26.0",
			"Time":    "2026-07-29T10:14:32Z",
		})
	}))
	defer srv.Close()
	serveEndpoints(t, srv.URL, "https://github.test")

	state, err := fetchLatestFromProxy(context.Background(), srv.Client(), "jamf-cli/v1.25.2")
	if err != nil {
		t.Fatalf("fetchLatestFromProxy: %v", err)
	}
	if state.LatestVersion != "v1.26.0" {
		t.Errorf("version = %q, want v1.26.0", state.LatestVersion)
	}
	if want := "https://github.test/Jamf-Concepts/jamf-cli/releases/tag/v1.26.0"; state.ReleaseURL != want {
		t.Errorf("release URL = %q, want %q", state.ReleaseURL, want)
	}
	if state.ReleasedAt.IsZero() {
		t.Error("expected the publish time to be captured (it drives the Homebrew grace period)")
	}
	// Module paths escape capitals as !lowercase; getting this wrong 404s.
	if want := "/github.com/!jamf-!concepts/jamf-cli/@latest"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
	if gotUA != "jamf-cli/v1.25.2" {
		t.Errorf("User-Agent = %q, want jamf-cli/v1.25.2", gotUA)
	}
}

func TestFetchLatestFromProxy_Failures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"server error", http.StatusInternalServerError, "boom", "unexpected HTTP 500"},
		{"not found", http.StatusNotFound, "not found", "unexpected HTTP 404"},
		{"unparseable body", http.StatusOK, "<html>captive portal</html>", "parsing module proxy response"},
		{"invalid version", http.StatusOK, `{"Version":"latest"}`, "invalid version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			serveEndpoints(t, srv.URL, "")

			_, err := fetchLatestFromProxy(context.Background(), srv.Client(), "jamf-cli/v1.25.2")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchLatestFromGitHub_ReadsTagFromRedirect(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.Header().Set("Location", "/Jamf-Concepts/jamf-cli/releases/tag/v1.30.1")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	serveEndpoints(t, "", srv.URL)

	state, err := fetchLatestFromGitHub(context.Background(), srv.Client(), "jamf-cli/v1.25.2")
	if err != nil {
		t.Fatalf("fetchLatestFromGitHub: %v", err)
	}
	if state.LatestVersion != "v1.30.1" {
		t.Errorf("version = %q, want v1.30.1", state.LatestVersion)
	}
	if !strings.HasSuffix(state.ReleaseURL, "/releases/tag/v1.30.1") {
		t.Errorf("release URL = %q, want it to point at the tag", state.ReleaseURL)
	}
	// HEAD keeps the fallback from pulling down the release page HTML.
	if gotMethod != http.MethodHead {
		t.Errorf("method = %q, want HEAD", gotMethod)
	}
}

func TestFetchLatestFromGitHub_NoTagIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // no Location, so no tag to read
	}))
	defer srv.Close()
	serveEndpoints(t, "", srv.URL)

	if _, err := fetchLatestFromGitHub(context.Background(), srv.Client(), "jamf-cli/v1.25.2"); err == nil {
		t.Fatal("expected an error when no release tag can be resolved")
	}
}

func TestFetchLatestRelease_FallsBackToGitHub(t *testing.T) {
	var proxyHits, githubHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		w.WriteHeader(http.StatusForbidden) // e.g. GOPROXY blocked by policy
	}))
	defer proxy.Close()
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		githubHits.Add(1)
		w.Header().Set("Location", "/Jamf-Concepts/jamf-cli/releases/tag/v1.27.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer github.Close()
	serveEndpoints(t, proxy.URL, github.URL)

	state, err := fetchLatestRelease(context.Background(), proxy.Client(), "jamf-cli/v1.25.2")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if state.LatestVersion != "v1.27.0" {
		t.Errorf("version = %q, want v1.27.0", state.LatestVersion)
	}
	if proxyHits.Load() != 1 || githubHits.Load() != 1 {
		t.Errorf("expected one attempt at each source; proxy=%d github=%d", proxyHits.Load(), githubHits.Load())
	}
}

func TestFetchLatestRelease_PrefersProxy(t *testing.T) {
	var githubHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Version": "v1.26.0"})
	}))
	defer proxy.Close()
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		githubHits.Add(1)
		w.WriteHeader(http.StatusFound)
	}))
	defer github.Close()
	serveEndpoints(t, proxy.URL, github.URL)

	state, err := fetchLatestRelease(context.Background(), proxy.Client(), "jamf-cli/v1.25.2")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if state.LatestVersion != "v1.26.0" {
		t.Errorf("version = %q, want v1.26.0", state.LatestVersion)
	}
	if githubHits.Load() != 0 {
		t.Errorf("GitHub was contacted %d times; the proxy answer should be enough", githubHits.Load())
	}
}

func TestFetchLatestRelease_BothSourcesDown(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	serveEndpoints(t, down.URL, down.URL)

	if _, err := fetchLatestRelease(context.Background(), down.Client(), "jamf-cli/v1.25.2"); err == nil {
		t.Fatal("expected an error when neither source answers")
	}
}

func TestFetchLatestRelease_HonoursContextCancellation(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer blocked.Close()
	serveEndpoints(t, blocked.URL, blocked.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := fetchLatestRelease(ctx, blocked.Client(), "jamf-cli/v1.25.2"); err == nil {
		t.Fatal("expected an error once the context expires")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("probe ran for %v after a 50ms deadline", elapsed)
	}
}

func TestDetectInstallKind(t *testing.T) {
	tests := []struct {
		name string
		path string
		env  map[string]string
		want installKind
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/jamf-cli/1.25.2/bin/jamf-cli", nil, installHomebrew},
		{"homebrew prefix path", "/opt/homebrew/bin/jamf-cli", nil, installHomebrew},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/jamf-cli", nil, installHomebrew},
		{
			name: "custom homebrew prefix from env",
			path: "/custom/brew/bin/jamf-cli",
			env:  map[string]string{"HOMEBREW_PREFIX": "/custom/brew"},
			want: installHomebrew,
		},
		{
			name: "gobin",
			path: "/Users/dev/bin/jamf-cli",
			env:  map[string]string{"GOBIN": "/Users/dev/bin"},
			want: installGoInstall,
		},
		{
			name: "gopath bin",
			path: "/workspace/gopath/bin/jamf-cli",
			env:  map[string]string{"GOPATH": "/workspace/gopath"},
			want: installGoInstall,
		},
		{"default gopath layout", "/Users/dev/go/bin/jamf-cli", nil, installGoInstall},
		{"packaged install", "/usr/local/bin/jamf-cli", nil, installUnknown},
		{"manual download", "/Users/dev/Downloads/jamf-cli", nil, installUnknown},
		{"unknown when path is unavailable", "", nil, installUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearUpdateEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := detectInstallKind(tt.path); got != tt.want {
				t.Errorf("detectInstallKind(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestUpgradeHint(t *testing.T) {
	tests := []struct {
		kind installKind
		want string
	}{
		{installHomebrew, "brew upgrade jamf-cli"},
		{installGoInstall, "go install github.com/Jamf-Concepts/jamf-cli/cmd/jamf-cli@latest"},
		{installUnknown, "https://github.com/Jamf-Concepts/jamf-cli/releases/latest"},
	}
	for _, tt := range tests {
		if got := upgradeHint(tt.kind); got != tt.want {
			t.Errorf("upgradeHint(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestFormatUpdateNotice(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	newRelease := updateState{
		LatestVersion: "v1.26.0",
		ReleasedAt:    now.Add(-72 * time.Hour),
		ReleaseURL:    "https://github.com/Jamf-Concepts/jamf-cli/releases/tag/v1.26.0",
	}

	t.Run("newer release names both versions, the upgrade command and the URL", func(t *testing.T) {
		got := formatUpdateNotice("v1.25.2", newRelease, installHomebrew, now)
		for _, want := range []string{"v1.25.2", "v1.26.0", "brew upgrade jamf-cli", newRelease.ReleaseURL} {
			if !strings.Contains(got, want) {
				t.Errorf("notice %q missing %q", got, want)
			}
		}
		if lines := strings.Count(got, "\n"); lines != 2 {
			t.Errorf("notice should be exactly two lines; got %d in %q", lines, got)
		}
	})

	t.Run("silent cases", func(t *testing.T) {
		tests := []struct {
			name    string
			current string
			latest  updateState
			kind    installKind
		}{
			{"already current", "v1.26.0", newRelease, installUnknown},
			{"ahead of the release", "v1.27.0", newRelease, installUnknown},
			{"no cached answer", "v1.25.2", updateState{}, installUnknown},
			{"dev build", "dev", newRelease, installUnknown},
			{
				name:    "homebrew tap has not caught up yet",
				current: "v1.25.2",
				latest:  updateState{LatestVersion: "v1.26.0", ReleasedAt: now.Add(-time.Hour)},
				kind:    installHomebrew,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := formatUpdateNotice(tt.current, tt.latest, tt.kind, now); got != "" {
					t.Errorf("expected silence; got %q", got)
				}
			})
		}
	})

	t.Run("fresh release still notifies a non-homebrew install", func(t *testing.T) {
		latest := updateState{LatestVersion: "v1.26.0", ReleasedAt: now.Add(-time.Minute)}
		if got := formatUpdateNotice("v1.25.2", latest, installUnknown, now); got == "" {
			t.Error("expected a notice: the tap grace period is Homebrew-only")
		}
	})

	t.Run("release URL is derived when the cache lacks one", func(t *testing.T) {
		latest := updateState{LatestVersion: "v1.26.0"}
		got := formatUpdateNotice("v1.25.2", latest, installUnknown, now)
		if !strings.Contains(got, "/releases/tag/v1.26.0") {
			t.Errorf("notice %q should fall back to the derived tag URL", got)
		}
	})
}

func TestUpdateCheckGate(t *testing.T) {
	allowed := updateCheckGate{Version: "v1.25.2", StdoutTTY: true, StderrTTY: true}
	if !allowed.allowed() {
		t.Fatal("baseline gate should allow the check")
	}

	tests := []struct {
		name   string
		mutate func(g *updateCheckGate)
	}{
		{"opted out", func(g *updateCheckGate) { g.Disabled = true }},
		{"quiet", func(g *updateCheckGate) { g.Quiet = true }},
		{"hints suppressed", func(g *updateCheckGate) { g.NoHints = true }},
		{"CI", func(g *updateCheckGate) { g.CI = true }},
		{"machine-facing command", func(g *updateCheckGate) { g.SkipCmd = true }},
		{"stdout piped", func(g *updateCheckGate) { g.StdoutTTY = false }},
		{"stderr redirected", func(g *updateCheckGate) { g.StderrTTY = false }},
		{"dev build", func(g *updateCheckGate) { g.Version = "dev" }},
		{"local build from a tag", func(g *updateCheckGate) { g.Version = "v1.25.2-3-gabc1234" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := allowed
			tt.mutate(&g)
			if g.allowed() {
				t.Error("expected the gate to veto the check")
			}
		})
	}
}

func TestIsCIEnvironment(t *testing.T) {
	t.Run("no markers", func(t *testing.T) {
		clearUpdateEnv(t)
		if isCIEnvironment() {
			t.Error("expected false with every CI marker cleared")
		}
	})
	for _, name := range updateCheckCIEnv {
		t.Run(name, func(t *testing.T) {
			clearUpdateEnv(t)
			t.Setenv(name, "true")
			if !isCIEnvironment() {
				t.Errorf("%s=true should mark a CI run", name)
			}
		})
	}
	t.Run("build number style value", func(t *testing.T) {
		clearUpdateEnv(t)
		t.Setenv("BUILD_NUMBER", "1423")
		if !isCIEnvironment() {
			t.Error("a numeric build marker should still count as CI")
		}
	})
}

func TestUpdateCheckDisabled(t *testing.T) {
	enabled, disabled := true, false
	tests := []struct {
		name string
		flag bool
		env  string
		cfg  *config.Config
		want bool
	}{
		{name: "default is enabled"},
		{name: "flag", flag: true, want: true},
		{name: "env true", env: "1", want: true},
		{name: "env false leaves it on", env: "0"},
		{name: "unparseable env is ignored", env: "yes-please"},
		{name: "config false", cfg: &config.Config{UpdateCheck: &disabled}, want: true},
		{name: "config true", cfg: &config.Config{UpdateCheck: &enabled}},
		{name: "config unset", cfg: &config.Config{}},
		{name: "nil config", cfg: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearUpdateEnv(t)
			if tt.env != "" {
				t.Setenv("JAMF_CLI_NO_UPDATE_CHECK", tt.env)
			}
			if got := updateCheckDisabled(tt.flag, tt.cfg); got != tt.want {
				t.Errorf("updateCheckDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUpdateCheckSkippedCommand(t *testing.T) {
	root := &cobra.Command{Use: "jamf-cli"}
	pro := &cobra.Command{Use: "pro"}
	computers := &cobra.Command{Use: "computers"}
	list := &cobra.Command{Use: "list"}
	mcp := &cobra.Command{Use: "mcp"}
	mcpServe := &cobra.Command{Use: "serve"}
	complete := &cobra.Command{Use: cobra.ShellCompRequestCmd}

	root.AddCommand(pro, mcp, complete)
	pro.AddCommand(computers)
	computers.AddCommand(list)
	mcp.AddCommand(mcpServe)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"ordinary command", list, false},
		{"parent of ordinary commands", pro, false},
		{"mcp speaks JSON-RPC on stdio", mcp, true},
		{"nested under mcp", mcpServe, true},
		{"shell completion callback", complete, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpdateCheckSkippedCommand(tt.cmd); got != tt.want {
				t.Errorf("isUpdateCheckSkippedCommand(%q) = %v, want %v", tt.cmd.Name(), got, tt.want)
			}
		})
	}
}

func TestNewUpdateChecker_ProbesWhenCacheIsStaleAndNotifies(t *testing.T) {
	clearUpdateEnv(t)
	useTempConfigDir(t)

	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Version": "v9.9.9",
			"Time":    "2026-01-01T00:00:00Z",
		})
	}))
	defer proxy.Close()
	serveEndpoints(t, proxy.URL, "https://github.test")

	c := newUpdateChecker("v1.25.2", time.Now())
	if c == nil {
		t.Fatal("expected a checker")
	}
	var buf bytes.Buffer
	c.notify(&buf)

	if hits.Load() != 1 {
		t.Errorf("expected exactly one probe; got %d", hits.Load())
	}
	if !strings.Contains(buf.String(), "v9.9.9") {
		t.Errorf("expected a notice naming the new release; got %q", buf.String())
	}
	cached := readUpdateCache()
	if cached.LatestVersion != "v9.9.9" || cached.CheckedAt.IsZero() {
		t.Errorf("probe result was not cached: %+v", cached)
	}
}

func TestNewUpdateChecker_FreshCacheSkipsTheNetwork(t *testing.T) {
	clearUpdateEnv(t)
	useTempConfigDir(t)

	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer proxy.Close()
	serveEndpoints(t, proxy.URL, "https://github.test")

	writeUpdateCache(updateState{
		CheckedAt:     time.Now(),
		LatestVersion: "v2.0.0",
		ReleaseURL:    "https://github.test/tag/v2.0.0",
	})

	c := newUpdateChecker("v1.25.2", time.Now())
	var buf bytes.Buffer
	c.notify(&buf)

	if hits.Load() != 0 {
		t.Errorf("a fresh cache must not hit the network; got %d requests", hits.Load())
	}
	if !strings.Contains(buf.String(), "v2.0.0") {
		t.Errorf("expected the cached answer to be reported; got %q", buf.String())
	}
}

func TestNewUpdateChecker_FailedProbeCachesTheFailureAndStaysSilent(t *testing.T) {
	clearUpdateEnv(t)
	useTempConfigDir(t)

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()
	serveEndpoints(t, down.URL, down.URL)

	c := newUpdateChecker("v1.25.2", time.Now())
	var buf bytes.Buffer
	c.notify(&buf)

	if buf.Len() != 0 {
		t.Errorf("a failed probe must be silent; got %q", buf.String())
	}
	cached := readUpdateCache()
	if cached.CheckedAt.IsZero() {
		t.Error("expected the failure to be recorded so the next run cools down")
	}
	if cached.LatestVersion != "" {
		t.Errorf("failure entry should carry no version; got %q", cached.LatestVersion)
	}
	if cached.fresh(time.Now().Add(2 * time.Hour)) {
		t.Error("a failure entry should expire within the hour")
	}
}

func TestUpdateChecker_Notify(t *testing.T) {
	t.Run("nil checker is a no-op", func(t *testing.T) {
		var c *updateChecker
		var buf bytes.Buffer
		c.notify(&buf) // must not panic
		if buf.Len() != 0 {
			t.Errorf("expected no output; got %q", buf.String())
		}
	})

	t.Run("probe result wins over the stale cached answer", func(t *testing.T) {
		result := make(chan updateState, 1)
		result <- updateState{LatestVersion: "v3.0.0", CheckedAt: time.Now()}
		c := &updateChecker{
			current:  "v1.25.2",
			cached:   updateState{LatestVersion: "v1.26.0"},
			result:   result,
			deadline: time.Second,
		}
		var buf bytes.Buffer
		c.notify(&buf)
		if !strings.Contains(buf.String(), "v3.0.0") || strings.Contains(buf.String(), "v1.26.0") {
			t.Errorf("expected the fresh probe result; got %q", buf.String())
		}
	})

	t.Run("a probe that overruns its budget says nothing", func(t *testing.T) {
		c := &updateChecker{
			current:  "v1.25.2",
			cached:   updateState{LatestVersion: "v9.9.9"},
			result:   make(chan updateState), // never delivers
			deadline: 20 * time.Millisecond,
		}
		var buf bytes.Buffer
		start := time.Now()
		c.notify(&buf)
		if buf.Len() != 0 {
			t.Errorf("expected silence when the probe times out; got %q", buf.String())
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("notify blocked for %v; it must respect its deadline", elapsed)
		}
	})
}

// TestRootWiring_UpdateCheck covers the plumbing rather than the policy: the
// opt-out flag has to exist, be documented alongside the env var, and an
// executed command must not emit a notice in a non-interactive process.
func TestRootWiring_UpdateCheck(t *testing.T) {
	clearUpdateEnv(t)
	useTempConfigDir(t)

	root := NewRootCmd("v1.25.2", "abc123", "2026-07-29", "unknown")

	flag := root.PersistentFlags().Lookup("no-update-check")
	if flag == nil {
		t.Fatal("--no-update-check is not registered on the root command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-update-check default = %q, want false (opt-out, not opt-in)", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "JAMF_CLI_NO_UPDATE_CHECK") {
		t.Errorf("flag help should name the env var; got %q", flag.Usage)
	}
	for _, want := range []string{"JAMF_CLI_NO_UPDATE_CHECK", "update-check: false"} {
		if !strings.Contains(root.Long, want) {
			t.Errorf("root help should document %q", want)
		}
	}

	// Executing a command in a test process (stdout is a pipe) must leave the
	// checker unarmed, so nothing can be printed and nothing probed.
	pendingUpdateCheck = &updateChecker{current: "v1.0.0", cached: updateState{LatestVersion: "v9.9.9"}}
	t.Cleanup(func() { pendingUpdateCheck = nil })

	outputFmt, noColor = "json", true
	root.SetArgs([]string{"config", "path"})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if pendingUpdateCheck != nil {
		t.Error("expected PersistentPreRunE to disarm the check when stdout is piped")
	}
}

func TestStartUpdateCheck_GateVetoReturnsNil(t *testing.T) {
	clearUpdateEnv(t)
	useTempConfigDir(t)

	// Piped stdout alone is enough to veto, which is the case every test
	// process is in — assert it explicitly rather than relying on it.
	if got := startUpdateCheck(&cobra.Command{Use: "list"}, &config.Config{}, "v1.25.2", false); got != nil {
		t.Error("expected nil checker when stdout is not a terminal")
	}
	// And an explicit opt-out must win even if everything else lines up.
	if got := startUpdateCheck(&cobra.Command{Use: "list"}, &config.Config{}, "v1.25.2", true); got != nil {
		t.Error("expected nil checker with --no-update-check")
	}
}
