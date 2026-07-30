// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/Jamf-Concepts/jamf-cli/internal/config"
	"github.com/Jamf-Concepts/jamf-cli/internal/output"
)

const (
	updateModulePath = "github.com/Jamf-Concepts/jamf-cli"
	updateRepoSlug   = "Jamf-Concepts/jamf-cli"

	// updateCacheTTL bounds how often a release probe leaves the machine.
	// A failed probe cools down for less time so a transient outage doesn't
	// mute the notice for a whole day.
	updateCacheTTL   = 24 * time.Hour
	updateFailureTTL = time.Hour

	// updateProbeTimeout caps what the check can cost. It is paid at most
	// once per updateCacheTTL, and only on a command that already succeeded.
	updateProbeTimeout = 2 * time.Second

	// updateBrewGrace suppresses the notice for Homebrew installs while a
	// release is still fresh: the tap is bumped after the GitHub release, so
	// telling a brew user to upgrade immediately points at a version
	// `brew upgrade` cannot resolve yet.
	updateBrewGrace = 24 * time.Hour
)

// Endpoints are variables, not constants, so tests can point them at an
// httptest server.
var (
	updateProxyBaseURL  = "https://proxy.golang.org"
	updateGitHubBaseURL = "https://github.com"
)

// updateState is the on-disk record of the last release probe. An empty
// LatestVersion records a *failed* probe, which is what keeps repeated
// invocations on a blocked network from each paying updateProbeTimeout.
type updateState struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
	ReleasedAt    time.Time `json:"released_at,omitempty"`
	ReleaseURL    string    `json:"release_url,omitempty"`
}

// fresh reports whether the cached answer is recent enough to reuse. A
// CheckedAt in the future (clock skew, a cache copied between machines) counts
// as fresh rather than triggering a probe on every invocation.
func (s updateState) fresh(now time.Time) bool {
	if s.CheckedAt.IsZero() {
		return false
	}
	ttl := updateCacheTTL
	if s.LatestVersion == "" {
		ttl = updateFailureTTL
	}
	return now.Sub(s.CheckedAt) < ttl
}

// updateCachePath is a sibling of the config file, matching the tenant
// version cache (see versionCachePath).
func updateCachePath() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), ".update-cache.json")
}

func readUpdateCache() updateState {
	data, err := os.ReadFile(updateCachePath())
	if err != nil {
		return updateState{}
	}
	var s updateState
	if json.Unmarshal(data, &s) != nil {
		return updateState{}
	}
	return s
}

// writeUpdateCache persists the probe result with mode 0600. The write is
// atomic (temp file then rename) so parallel invocations never read a partial
// file. Failures are ignored — the cache is best-effort.
func writeUpdateCache(s updateState) {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	p := updateCachePath()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// normalizeVersion tolerates a version string built without the tag's "v"
// prefix (VERSION=1.25.2 make build). Everything else is returned unchanged so
// invalid input stays invalid.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v[0] < '0' || v[0] > '9' {
		return v
	}
	return "v" + v
}

// isReleaseVersion reports whether v names an exact published release.
// "dev", a `git describe` suffix (v1.25.2-3-gabc1234), a -dirty working tree,
// and prerelease tags are all builds whose "upgrade" is undefined, so they
// never notify.
func isReleaseVersion(v string) bool {
	v = normalizeVersion(v)
	return semver.IsValid(v) && semver.Prerelease(v) == "" && semver.Build(v) == ""
}

// newerAvailable reports whether latest supersedes current. Both sides must be
// exact releases: nobody should be nudged from a stable build onto an rc, and a
// non-release current version has no defined upgrade.
func newerAvailable(current, latest string) bool {
	if !isReleaseVersion(current) || !isReleaseVersion(latest) {
		return false
	}
	return semver.Compare(semver.Canonical(normalizeVersion(latest)), semver.Canonical(normalizeVersion(current))) > 0
}

// releaseTagURL builds the human-facing release page URL for a tag.
func releaseTagURL(tag string) string {
	return fmt.Sprintf("%s/%s/releases/tag/%s", strings.TrimSuffix(updateGitHubBaseURL, "/"), updateRepoSlug, tag)
}

// fetchLatestRelease resolves the newest published jamf-cli release.
//
// The Go module proxy is the primary source: CDN-backed, no credentials, and —
// unlike api.github.com's 60-requests-per-hour unauthenticated cap, which every
// client behind one corporate NAT shares — no per-IP budget an admin team can
// exhaust. When the proxy can't answer (GOPROXY blocked by policy, module not
// yet indexed), fall back to the github.com/<repo>/releases/latest redirect,
// which is a plain web request rather than an API call.
//
// Neither request carries tenant, profile, or credential data: the module path
// and the current version (in User-Agent) are all that leave the machine.
func fetchLatestRelease(ctx context.Context, hc *http.Client, userAgent string) (updateState, error) {
	s, err := fetchLatestFromProxy(ctx, hc, userAgent)
	if err == nil {
		return s, nil
	}
	s2, err2 := fetchLatestFromGitHub(ctx, hc, userAgent)
	if err2 != nil {
		// Report the primary failure — the fallback's error is usually the
		// same underlying network problem.
		return updateState{}, err
	}
	return s2, nil
}

// fetchLatestFromProxy queries the module proxy's @latest endpoint.
func fetchLatestFromProxy(ctx context.Context, hc *http.Client, userAgent string) (updateState, error) {
	escaped, err := module.EscapePath(updateModulePath)
	if err != nil {
		return updateState{}, err
	}
	url := fmt.Sprintf("%s/%s/@latest", strings.TrimSuffix(updateProxyBaseURL, "/"), escaped)
	body, err := updateGet(ctx, hc, url, userAgent, http.StatusOK)
	if err != nil {
		return updateState{}, err
	}

	var payload struct {
		Version string    `json:"Version"`
		Time    time.Time `json:"Time"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return updateState{}, fmt.Errorf("parsing module proxy response: %w", err)
	}
	if !semver.IsValid(payload.Version) {
		return updateState{}, fmt.Errorf("module proxy returned invalid version %q", payload.Version)
	}
	return updateState{
		LatestVersion: payload.Version,
		ReleasedAt:    payload.Time,
		ReleaseURL:    releaseTagURL(payload.Version),
	}, nil
}

// fetchLatestFromGitHub reads the tag out of the /releases/latest redirect.
// Redirects are deliberately not followed: the 302's Location is the answer,
// and stopping there avoids downloading the release page HTML.
func fetchLatestFromGitHub(ctx context.Context, hc *http.Client, userAgent string) (updateState, error) {
	url := fmt.Sprintf("%s/%s/releases/latest", strings.TrimSuffix(updateGitHubBaseURL, "/"), updateRepoSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return updateState{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	noRedirect := &http.Client{
		Timeout:   hc.Timeout,
		Transport: hc.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return updateState{}, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// The tag is in Location on a redirect; if something upstream already
	// followed it, the final URL carries the same tag.
	target := resp.Header.Get("Location")
	if target == "" && resp.StatusCode == http.StatusOK && resp.Request != nil {
		target = resp.Request.URL.Path
	}
	if target == "" {
		return updateState{}, fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, url)
	}

	tag := path.Base(strings.TrimSuffix(target, "/"))
	if !semver.IsValid(tag) {
		return updateState{}, fmt.Errorf("could not read a release tag from %q", target)
	}
	return updateState{LatestVersion: tag, ReleaseURL: releaseTagURL(tag)}, nil
}

// updateGet performs a GET and returns the body, capped so a misrouted
// response (a captive portal, a proxy error page) can't be read unbounded.
func updateGet(ctx context.Context, hc *http.Client, url, userAgent string, wantStatus int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != wantStatus {
		return nil, fmt.Errorf("unexpected HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<10))
}

// installKind classifies how this binary got onto the machine, which decides
// what the upgrade instruction should say. Detection is path-based on purpose:
// shelling out to `brew --prefix` would cost more than the whole check.
type installKind int

const (
	installUnknown installKind = iota
	installHomebrew
	installGoInstall
)

// detectInstallKind inspects the resolved executable path. execPath should
// already have symlinks evaluated, which is what turns a Homebrew shim in
// <prefix>/bin into its .../Cellar/... target.
func detectInstallKind(execPath string) installKind {
	if execPath == "" {
		return installUnknown
	}
	p := filepath.ToSlash(execPath)
	if strings.Contains(p, "/Cellar/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "/.linuxbrew/") {
		return installHomebrew
	}
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" && strings.HasPrefix(p, filepath.ToSlash(prefix)+"/") {
		return installHomebrew
	}
	dir := path.Dir(p)
	if gobin := os.Getenv("GOBIN"); gobin != "" && dir == filepath.ToSlash(gobin) {
		return installGoInstall
	}
	for _, gopath := range filepath.SplitList(os.Getenv("GOPATH")) {
		if gopath != "" && dir == filepath.ToSlash(filepath.Join(gopath, "bin")) {
			return installGoInstall
		}
	}
	if strings.HasSuffix(dir, "/go/bin") {
		return installGoInstall
	}
	return installUnknown
}

// upgradeHint returns the command (or destination) that actually upgrades this
// particular install.
func upgradeHint(kind installKind) string {
	switch kind {
	case installHomebrew:
		return "brew upgrade jamf-cli"
	case installGoInstall:
		return "go install " + updateModulePath + "/cmd/jamf-cli@latest"
	default:
		return fmt.Sprintf("%s/%s/releases/latest", strings.TrimSuffix(updateGitHubBaseURL, "/"), updateRepoSlug)
	}
}

// formatUpdateNotice renders the advisory, or "" when there is nothing to say.
func formatUpdateNotice(current string, latest updateState, kind installKind, now time.Time) string {
	if latest.LatestVersion == "" || !newerAvailable(current, latest.LatestVersion) {
		return ""
	}
	if kind == installHomebrew && !latest.ReleasedAt.IsZero() && now.Sub(latest.ReleasedAt) < updateBrewGrace {
		return ""
	}
	url := latest.ReleaseURL
	if url == "" {
		url = releaseTagURL(latest.LatestVersion)
	}
	return fmt.Sprintf("hint: new jamf-cli release: %s → %s (%s)\n      %s\n",
		normalizeVersion(current), normalizeVersion(latest.LatestVersion), upgradeHint(kind), url)
}

// updateCheckGate collects every condition that decides whether an invocation
// may probe for a new release. Kept as data so the policy is testable without
// a terminal, a network, or a real command tree.
type updateCheckGate struct {
	Version   string // version this binary reports
	Disabled  bool   // --no-update-check, env, or config update-check: false
	Quiet     bool   // --quiet
	NoHints   bool   // --no-hints; the notice is an advisory hint
	StdoutTTY bool
	StderrTTY bool
	CI        bool
	SkipCmd   bool // command whose output must stay machine-clean
}

// allowed reports whether the check may run. Every condition is a veto: the
// notice exists for a human at a terminal running a release build, and nobody
// else.
func (g updateCheckGate) allowed() bool {
	switch {
	case g.Disabled, g.Quiet, g.NoHints, g.CI, g.SkipCmd:
		return false
	case !g.StdoutTTY || !g.StderrTTY:
		return false
	default:
		return isReleaseVersion(g.Version)
	}
}

// updateCheckCIEnv lists the environment variables that mark an automated run.
// Matches the set gh CLI treats as CI, plus the vendor-neutral spellings.
var updateCheckCIEnv = []string{
	"CI",
	"CONTINUOUS_INTEGRATION",
	"BUILD_NUMBER",
	"RUN_ID",
	"GITHUB_ACTIONS",
	"GITLAB_CI",
	"TF_BUILD",
}

// isCIEnvironment reports whether any CI marker is set to a non-empty value.
// Presence is what matters — CI systems set these to "true", "1", or a build
// number interchangeably.
func isCIEnvironment() bool {
	for _, name := range updateCheckCIEnv {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// updateCheckSkipCommands are commands whose output is consumed by a machine
// (or that exist to answer a question about this binary), so an advisory line
// on stderr would be noise at best. `mcp` is mandatory: it speaks JSON-RPC.
var updateCheckSkipCommands = map[string]bool{
	"mcp":                           true,
	"completion":                    true,
	"agent-context":                 true,
	"version":                       true,
	"help":                          true,
	cobra.ShellCompRequestCmd:       true,
	cobra.ShellCompNoDescRequestCmd: true,
}

// isUpdateCheckSkippedCommand walks the command chain, so `pro ... mcp`-style
// nesting is caught the same way chainSkip does it for auth.
func isUpdateCheckSkippedCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if updateCheckSkipCommands[c.Name()] {
			return true
		}
	}
	return false
}

// updateCheckDisabled resolves the three opt-outs: the --no-update-check flag,
// JAMF_CLI_NO_UPDATE_CHECK (value-parsed, so =0 leaves the check on, matching
// JAMF_CLI_NO_HINTS), and `update-check: false` in the config file for an
// admin who wants it off across a deployed fleet.
func updateCheckDisabled(flag bool, cfg *config.Config) bool {
	if flag {
		return true
	}
	if b, err := strconv.ParseBool(os.Getenv("JAMF_CLI_NO_UPDATE_CHECK")); err == nil && b {
		return true
	}
	return cfg != nil && cfg.UpdateCheck != nil && !*cfg.UpdateCheck
}

// pendingUpdateCheck carries the probe launched in PersistentPreRunE through to
// PersistentPostRunE. Package-level to match root.go's global-state
// convention; a nil value is safe to notify().
var pendingUpdateCheck *updateChecker

// updateChecker carries the in-flight probe (if one was needed) from
// PersistentPreRunE to PersistentPostRunE.
type updateChecker struct {
	current  string
	kind     installKind
	cached   updateState
	result   chan updateState
	cancel   context.CancelFunc
	deadline time.Duration
}

// startUpdateCheck evaluates the gate and, when the cached answer is stale,
// launches the probe in the background so the command itself never waits on
// the network. Returns nil when this invocation must stay silent.
func startUpdateCheck(cmd *cobra.Command, cfg *config.Config, version string, flagDisabled bool) *updateChecker {
	gate := updateCheckGate{
		Version:   version,
		Disabled:  updateCheckDisabled(flagDisabled, cfg),
		Quiet:     quiet,
		NoHints:   noHints,
		StdoutTTY: output.IsTerminal(os.Stdout.Fd()),
		StderrTTY: output.IsTerminal(os.Stderr.Fd()),
		CI:        isCIEnvironment(),
		SkipCmd:   isUpdateCheckSkippedCommand(cmd),
	}
	if !gate.allowed() {
		return nil
	}
	return newUpdateChecker(version, time.Now())
}

// newUpdateChecker reads the cache and, only when the cached answer is stale,
// launches the probe. Split from startUpdateCheck so the probe/cache behaviour
// is testable without a terminal.
func newUpdateChecker(version string, now time.Time) *updateChecker {
	c := &updateChecker{
		current:  version,
		kind:     detectInstallKind(resolvedExecutablePath()),
		cached:   readUpdateCache(),
		deadline: updateProbeTimeout,
	}
	if c.cached.fresh(now) {
		return c
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateProbeTimeout)
	c.cancel = cancel
	c.result = make(chan updateState, 1)
	go func() {
		state, err := fetchLatestRelease(ctx, &http.Client{Timeout: updateProbeTimeout}, "jamf-cli/"+version)
		if err != nil {
			// Record the failure so an offline machine cools down instead of
			// paying the timeout on every command.
			state = updateState{}
		}
		state.CheckedAt = time.Now()
		writeUpdateCache(state)
		c.result <- state
	}()
	return c
}

// notify writes the advisory to w, if there is one. Called after the command's
// own output so a probe can never interleave with, or delay, real results.
func (c *updateChecker) notify(w io.Writer) {
	if c == nil {
		return
	}
	state := c.cached
	if c.result != nil {
		if c.cancel != nil {
			defer c.cancel()
		}
		select {
		case state = <-c.result:
		case <-time.After(c.deadline):
			// Probe outlived its budget; say nothing and try again next run.
			return
		}
	}
	if notice := formatUpdateNotice(c.current, state, c.kind, time.Now()); notice != "" {
		_, _ = fmt.Fprint(w, notice)
	}
}

// resolvedExecutablePath returns this binary's path with symlinks resolved,
// or "" when it can't be determined. Symlink resolution is what turns a
// Homebrew shim into its Cellar target.
func resolvedExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
