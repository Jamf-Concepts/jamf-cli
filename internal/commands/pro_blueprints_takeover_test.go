// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
)

func TestReportTakeover_SupportedExplainsTheLifecycle(t *testing.T) {
	var buf bytes.Buffer
	reportTakeover(&buf, profileconvert.TakeoverReport{Supported: true})
	out := buf.String()

	for _, want := range []string{
		"Takeover supported",
		"without reinstalling",
		// Retiring the Classic profile and then undeploying removes the settings
		// from devices; an admin tidying up would otherwise find that out the hard way.
		"removes the profile from devices",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Warning") {
		t.Errorf("a supported takeover is not a warning:\n%s", out)
	}
}

func TestReportTakeover_UnsupportedWarnsAndListsEveryReason(t *testing.T) {
	var buf bytes.Buffer
	reportTakeover(&buf, profileconvert.TakeoverReport{Reasons: []string{
		"payload 1 (com.apple.dashboard) is delivered as com.apple.ManagedClient.preferences",
		"payload 4 (com.apple.finder) has a PayloadUUID that differs from its PayloadIdentifier",
	}})
	out := buf.String()

	if !strings.HasPrefix(out, "Warning: takeover is not supported") {
		t.Errorf("want a warning first, got:\n%s", out)
	}
	if !strings.Contains(out, "alongside the Classic profile") {
		t.Errorf("the warning must say both profiles end up active:\n%s", out)
	}
	for _, reason := range []string{"com.apple.dashboard", "com.apple.finder"} {
		if !strings.Contains(out, reason) {
			t.Errorf("reason for %s missing:\n%s", reason, out)
		}
	}
}

func legacyComponent(cfg string) blueprints.Component {
	return blueprints.Component{Identifier: "com.jamf.ddm-configuration-profile", Configuration: json.RawMessage(cfg)}
}

func TestWarnDroppedKeys(t *testing.T) {
	sent := []blueprints.Component{legacyComponent(
		`{"payloadContent":[{"payloadType":"com.apple.finder","InterfaceLevel":"Full","ProhibitBurn":false}]}`)}
	stored := []blueprints.BlueprintStep{{Components: []blueprints.Component{legacyComponent(
		`{"payloadContent":[{"payloadType":"com.apple.finder","payloadUUID":"A","ProhibitBurn":false}]}`)}}}

	t.Run("after a takeover the keys stop being enforced", func(t *testing.T) {
		var buf bytes.Buffer
		warnDroppedKeys(&buf, sent, stored, true)
		out := buf.String()
		if !strings.Contains(out, "payload 1 (com.apple.finder): InterfaceLevel") {
			t.Errorf("dropped key not named:\n%s", out)
		}
		if !strings.Contains(out, "stop being enforced on devices") {
			t.Errorf("takeover consequence missing:\n%s", out)
		}
	})

	t.Run("without a takeover the wording is plainer", func(t *testing.T) {
		var buf bytes.Buffer
		warnDroppedKeys(&buf, sent, stored, false)
		if out := buf.String(); !strings.Contains(out, "will not be enforced") || strings.Contains(out, "takes over") {
			t.Errorf("unexpected wording:\n%s", out)
		}
	})

	t.Run("nothing dropped, nothing said", func(t *testing.T) {
		var buf bytes.Buffer
		warnDroppedKeys(&buf, sent, []blueprints.BlueprintStep{{Components: sent}}, true)
		if buf.Len() != 0 {
			t.Errorf("unexpected output:\n%s", buf.String())
		}
	})

	t.Run("a blueprint with only native components is skipped", func(t *testing.T) {
		var buf bytes.Buffer
		native := []blueprints.Component{{Identifier: "com.jamf.ddm.passcode-settings", Configuration: json.RawMessage(`{}`)}}
		warnDroppedKeys(&buf, native, []blueprints.BlueprintStep{{Components: native}}, true)
		if buf.Len() != 0 {
			t.Errorf("unexpected output:\n%s", buf.String())
		}
	})
}
