package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/auth"
)

// The retired-host refusal lives inside newPlatformSDKClient rather than beside
// its callers, because it was beside one of them: ResolveAuthForProfile checked
// it while the `security` and `school` resolvers did not, PersistentPreRunE
// returning early for both products. A guard on the one constructor every path
// must call cannot be forgotten by the next caller.
//
// The review's re-read of root.go:1542 concluded resolveSchoolClient still
// bypassed it. It does not — it calls newPlatformSDKClient, and the refusal
// fires (wire-checked 2026-09-03 with a school profile carrying
// platform-url: https://eu.apigw.jamf.com, which exited 2 naming the GA host).
// But nothing in the suite established that, and nothing stopped a fourth
// construction site from being written without the guard. These two tests are
// that guarantee: one pins the constructor's own behaviour, the other pins
// that the constructor is the only way a platform client gets built.

// TestNewPlatformSDKClientRefusesTheRetiredHost pins the refusal on the one
// constructor every platform-client path passes through, for every region.
func TestNewPlatformSDKClientRefusesTheRetiredHost(t *testing.T) {
	retired := []string{
		"https://eu.apigw.jamf.com",
		"https://us.apigw.jamf.com",
		"https://ap.apigw.jamf.com",
		"https://eu.apigw.jamf.com/",
	}
	scopes := []auth.Scope{
		auth.TenantScope("t"),
		auth.EnvironmentScope("e"),
		{}, // organization scope — no ID, and it must still be refused
	}

	for _, url := range retired {
		for _, scope := range scopes {
			got, err := newPlatformSDKClient(url, "cid", "csecret", scope, false)
			if err == nil {
				t.Errorf("newPlatformSDKClient(%q, scope %v) built a client against the "+
					"retired gateway; it must refuse before any request", url, scope.Kind)
				continue
			}
			if got != nil {
				t.Errorf("newPlatformSDKClient(%q) returned both a client and an error", url)
			}
			// The complaint is worthless without the replacement: the wire
			// symptom is an edge-level 403 naming neither the host nor the reason.
			if !strings.Contains(err.Error(), ".api.jamfcloud.com") {
				t.Errorf("newPlatformSDKClient(%q) does not name the GA host to switch to: %v", url, err)
			}
		}
	}

	// The GA host must still be accepted, or the guard is a denial of service.
	if _, err := newPlatformSDKClient("https://eu.api.jamfcloud.com", "cid", "csecret",
		auth.TenantScope("t"), false); err != nil {
		t.Errorf("the GA host must be accepted: %v", err)
	}
}

// platformClientConstruction matches a call to the jamfplatform SDK's own
// client constructor, which is what newPlatformSDKClient wraps.
var platformClientConstruction = regexp.MustCompile(`jamfplatform\.NewClient\(`)

// platformClientConstructionSites are the files allowed to call the SDK
// constructor directly. An exemption is not a free pass: the file must still
// call refuseRetiredGatewayURL itself, which the test checks — a bare
// "this one is fine" entry is how the guard-beside-one-caller pattern the
// wrapper exists to replace would come back.
var platformClientConstructionSites = map[string]string{
	"pro_platform_helpers.go":   "newPlatformSDKClient — the wrapper the refusal lives in",
	"platform_gateway_setup.go": "platform setup wants no retries, no token cache and none of the dry-run/verbose/spinner transports, so it builds its own client and repeats the refusal",
}

// TestOnlyTheGuardedWrapperConstructsAPlatformClient fails when a new code
// path builds a platform SDK client without going through
// newPlatformSDKClient, which is the only thing that makes the retired-host
// refusal unforgettable.
func TestOnlyTheGuardedWrapperConstructsAPlatformClient(t *testing.T) {
	var files []string
	for _, dir := range handWrittenPathDirs {
		found, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no Go files to scan — the scan directories have moved")
	}

	seen := map[string]bool{}
	matched := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if !platformClientConstruction.MatchString(line) {
				continue
			}
			matched++
			base := filepath.Base(file)
			if reason, ok := platformClientConstructionSites[base]; ok {
				seen[base] = true
				if !strings.Contains(string(src), "refuseRetiredGatewayURL") {
					t.Errorf("%s is exempt from the wrapper (%s) but does not call "+
						"refuseRetiredGatewayURL itself, so the retired host reaches the "+
						"token exchange from this path.", file, reason)
				}
				t.Logf("guarded construction site %s:%d (%s)", file, i+1, reason)
				continue
			}
			t.Errorf("%s:%d constructs a platform SDK client directly.\n"+
				"Call newPlatformSDKClient instead — it carries the retired-host refusal, "+
				"and a profile still naming {region}.apigw.jamf.com would otherwise fail "+
				"inside the token exchange with an edge-level 403 naming neither the host "+
				"nor the reason. If this site genuinely must bypass the wrapper, add %q to "+
				"platformClientConstructionSites with the reason.", file, i+1, base)
		}
	}

	if matched == 0 {
		t.Fatal("matched no platform-client construction — platformClientConstruction no " +
			"longer matches the SDK's constructor, so this guard is vacuous")
	}
	for base, reason := range platformClientConstructionSites {
		if !seen[base] {
			t.Errorf("platformClientConstructionSites has a stale entry: %s (%s) no longer "+
				"constructs a platform client. Remove it.", base, reason)
		}
	}
}
