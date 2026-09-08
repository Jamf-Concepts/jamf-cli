// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	platformgen "github.com/Jamf-Concepts/jamf-cli/internal/commands/platform/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

// applyProbe wires a device-groups apply command against a mux that records
// which of the three composed requests actually went out.
//
// device-groups is the resource used throughout because it exercises the whole
// shape: its list is a v2 envelope (`{"groups":[…]}`), its create is a v1
// collection POST answering 201 with a body, and its update is a v2 item PUT
// answering 204 with none — so a test that passes here is not passing on the
// simplest possible spec.
type applyProbe struct {
	lists, creates, updates int
	updatePath              string
	updateContentType       string
}

func newApplyProbe(t *testing.T, listItems []map[string]any, listStatus int) (*cobra.Command, *applyProbe, *bytes.Buffer, *registry.CLIContext) {
	t.Helper()
	sdk, mux := newTestPlatformSDK(t)
	p := &applyProbe{}

	mux.HandleFunc("/securitycloud/v2/groups", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s on the list path", r.Method)
			return
		}
		p.lists++
		if listStatus != http.StatusOK {
			writeJSONStatus(w, listStatus, map[string]any{"message": "boom"})
			return
		}
		writeJSON(w, map[string]any{"groups": listItems})
	})
	mux.HandleFunc("/securitycloud/v1/groups", func(w http.ResponseWriter, _ *http.Request) {
		p.creates++
		writeJSONStatus(w, http.StatusCreated, map[string]any{"id": "new-id", "name": "probe"})
	})
	mux.HandleFunc("/securitycloud/v2/groups/", func(w http.ResponseWriter, r *http.Request) {
		p.updates++
		p.updatePath = r.URL.Path
		p.updateContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	})

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}}
	cmd := platformgen.NewDeviceGroupsCmd(cliCtx)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	return cmd, p, &stderr, cliCtx
}

// TestGeneratedApplyForksOnTheNameLookup covers the create-versus-update fork,
// which had no test at all: internal/commands/platform/generated carries no
// _test.go files, and a mutation flipping the not-found check left the whole
// scoped suite green in both directions.
//
// The assertions are on which requests reached the server, not on the message
// printed, because "Created" and "Updated" are one Fprintf apart and a preview
// that says the right word while sending the wrong request is the failure this
// is written to catch.
func TestGeneratedApplyForksOnTheNameLookup(t *testing.T) {
	t.Run("no match creates", func(t *testing.T) {
		cmd, p, stderr, _ := newApplyProbe(t, []map[string]any{{"id": "other", "name": "somebody-else"}}, http.StatusOK)
		cmd.SetArgs([]string{"apply", "--set", "name=probe"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if p.creates != 1 || p.updates != 0 {
			t.Errorf("creates=%d updates=%d, want 1/0", p.creates, p.updates)
		}
		if !strings.Contains(stderr.String(), "Created") {
			t.Errorf("stderr = %q, want it to report a create", stderr.String())
		}
	})

	t.Run("one match updates that id", func(t *testing.T) {
		cmd, p, stderr, _ := newApplyProbe(t, []map[string]any{{"id": "abc123", "name": "probe"}}, http.StatusOK)
		cmd.SetArgs([]string{"apply", "--set", "name=probe", "--yes"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if p.creates != 0 || p.updates != 1 {
			t.Fatalf("creates=%d updates=%d, want 0/1", p.creates, p.updates)
		}
		if p.updatePath != "/securitycloud/v2/groups/abc123" {
			t.Errorf("update path = %q, want the resolved id substituted", p.updatePath)
		}
		if !strings.Contains(stderr.String(), "Updated") {
			t.Errorf("stderr = %q, want it to report an update", stderr.String())
		}
	})

	// Two items sharing the name must stop the run. Creating would add a third
	// copy and updating would pick one arbitrarily, so neither is a safe guess
	// — and the test asserts nothing was written, not merely that an error came
	// back.
	t.Run("ambiguous name writes nothing", func(t *testing.T) {
		items := []map[string]any{{"id": "a", "name": "probe"}, {"id": "b", "name": "probe"}}
		cmd, p, _, _ := newApplyProbe(t, items, http.StatusOK)
		cmd.SetArgs([]string{"apply", "--set", "name=probe", "--yes"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("apply on an ambiguous name succeeded, want an error")
		}
		if !strings.Contains(err.Error(), "ambiguous match") {
			t.Errorf("error = %v, want it to name the ambiguity", err)
		}
		// The remedy must be one apply can satisfy: apply takes no positional,
		// so an error telling the caller to "pass the positional ID" names
		// something the command has no argument for.
		if strings.Contains(err.Error(), "positional") {
			t.Errorf("error = %v, want a remedy apply itself supports", err)
		}
		if p.creates != 0 || p.updates != 0 {
			t.Errorf("creates=%d updates=%d, want 0/0", p.creates, p.updates)
		}
	})

	// A name that matches an item the list gives no ID for is not absence.
	// Reading it as absence is how apply creates a duplicate of something that
	// already exists, every run, silently — Security Cloud's implicit "Default
	// Group" is returned exactly this way.
	t.Run("matched but idless writes nothing", func(t *testing.T) {
		cmd, p, _, _ := newApplyProbe(t, []map[string]any{{"name": "Default Group"}}, http.StatusOK)
		cmd.SetArgs([]string{"apply", "--set", "name=Default Group", "--yes"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("apply on a nameless-id match succeeded, want an error")
		}
		if !strings.Contains(err.Error(), "no ID") {
			t.Errorf("error = %v, want it to say the list returns no ID", err)
		}
		if p.creates != 0 || p.updates != 0 {
			t.Errorf("creates=%d updates=%d, want 0/0", p.creates, p.updates)
		}
	})

	// A failed read is not an absent resource. Treating a 500 (or a 403) as
	// "not found" turns a lookup failure into an unwanted create.
	t.Run("list failure writes nothing", func(t *testing.T) {
		cmd, p, _, _ := newApplyProbe(t, nil, http.StatusInternalServerError)
		cmd.SetArgs([]string{"apply", "--set", "name=probe", "--yes"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("apply succeeded after a failed list, want an error")
		}
		if p.creates != 0 || p.updates != 0 {
			t.Errorf("creates=%d updates=%d, want 0/0", p.creates, p.updates)
		}
	})
}

// TestGeneratedApplyHonoursDryRunOnBothBranches extends the dry-run guarantee
// to apply. The exists check is a read and still runs — that is what lets the
// preview say which of create or update would happen — so the assertion is that
// no *write* was sent, not that no request was.
func TestGeneratedApplyHonoursDryRunOnBothBranches(t *testing.T) {
	cases := []struct {
		name  string
		items []map[string]any
		want  string
	}{
		{name: "would create", items: []map[string]any{}, want: "[dry-run] POST /securitycloud/v1/groups"},
		{name: "would update", items: []map[string]any{{"id": "abc123", "name": "probe"}}, want: "[dry-run] PUT /securitycloud/v2/groups/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, p, stderr, cliCtx := newApplyProbe(t, tc.items, http.StatusOK)
			cliCtx.DryRun = true
			cmd.SetArgs([]string{"apply", "--set", "name=probe"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("apply --dry-run: %v", err)
			}
			if p.creates != 0 || p.updates != 0 {
				t.Errorf("creates=%d updates=%d under --dry-run, want 0/0", p.creates, p.updates)
			}
			if p.lists == 0 {
				t.Error("the exists check did not run, so the preview cannot know which branch it is on")
			}
			if got := stderr.String(); !strings.Contains(got, tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// TestGeneratedApplySendsThePatchContentTypeItsSiblingDoes pins the header,
// which is the half of the update nothing else can see.
//
// The SDK's transport defaults any bodied PATCH with no explicit content type
// to application/merge-patch+json, so the resource's own `patch` command sends
// that. apply passes a content type explicitly, and deriving it from the spec's
// declared media type alone made ai-policies' apply send application/json to
// the identical URL — a divergence on the wire between two commands whose help
// describes the same semantics. Derived from the method now, and a mutation
// flipping either branch has to fail something.
func TestGeneratedApplySendsThePatchContentTypeItsSiblingDoes(t *testing.T) {
	sdk, mux := newTestPlatformSDK(t)
	var contentType string
	mux.HandleFunc("/ai/governance/policies/v1/policies", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"totalCount": 1, "results": []map[string]any{{"id": "pol1", "name": "probe"}}})
	})
	mux.HandleFunc("/ai/governance/policies/v1/policies/pol1", func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	})

	cliCtx := &registry.CLIContext{PlatformSDKClient: sdk, Output: &captureOutput{}}
	cmd := platformgen.NewAiPoliciesCmd(cliCtx)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--set", "name=probe", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if contentType != "application/merge-patch+json" {
		t.Errorf("Content-Type = %q, want application/merge-patch+json — the same header ai-policies patch sends to this URL", contentType)
	}
}

// TestGeneratedApplyCarriesTheScopeAnnotationItsSiblingsDo covers the
// annotation gap: scopesOf returns nil when jamf:scopes is absent, so without
// it AnnotateScopeLevelError can never fire for apply and apply drops out of
// the commands catalog's scopes field — leaving the one verb composed of three
// requests as the only one on its resource with no declared level.
func TestGeneratedApplyCarriesTheScopeAnnotationItsSiblingsDo(t *testing.T) {
	cliCtx := &registry.CLIContext{}
	for _, newCmd := range []func(*registry.CLIContext) *cobra.Command{
		platformgen.NewDeviceGroupsCmd,
		platformgen.NewAiPoliciesCmd,
		platformgen.NewDnsZonesCmd,
		platformgen.NewZtnaAppsCmd,
		platformgen.NewZtnaGatewaysCmd,
		platformgen.NewZtnaGroupedGatewaysCmd,
	} {
		parent := newCmd(cliCtx)
		var apply, sibling *cobra.Command
		for _, sub := range parent.Commands() {
			switch sub.Name() {
			case "apply":
				apply = sub
			case "create":
				sibling = sub
			}
		}
		if apply == nil {
			t.Errorf("%s ships no apply", parent.Name())
			continue
		}
		if sibling == nil {
			t.Errorf("%s ships no create to compare apply against", parent.Name())
			continue
		}
		want := sibling.Annotations["jamf:scopes"]
		if want == "" {
			t.Errorf("%s create carries no jamf:scopes, so this test cannot tell an inherited annotation from a missing one", parent.Name())
			continue
		}
		if got := apply.Annotations["jamf:scopes"]; got != want {
			t.Errorf("%s apply jamf:scopes = %q, want %q (its create's)", parent.Name(), got, want)
		}
	}
}
