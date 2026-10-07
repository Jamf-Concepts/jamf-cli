// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	jamfplatform "github.com/jamf/jamfplatform-go-sdk/jamfplatform"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
)

const (
	planTopID  = "AAFA0EDA-1978-4FCB-91E2-5CE3A72367D7"
	planFinder = "26D7CC73-98B3-4ED1-8235-649E41003E36"
	planDash   = "DDDDDDDD-98B3-4ED1-8235-649E41003E36"
	planDesk   = "EEEEEEEE-98B3-4ED1-8235-649E41003E36"
)

type noSchemas struct{}

func (noSchemas) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

// offlineFetcher answers every schema lookup with a 404, so a plan can be built
// without reaching GitHub. Validation then has nothing to complain about.
func offlineFetcher() *profileconvert.SchemaFetcher {
	return profileconvert.NewSchemaFetcher(&http.Client{Transport: noSchemas{}})
}

func planProfile(payloads ...[3]string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>PayloadContent</key><array>`)
	for _, p := range payloads {
		fmt.Fprintf(&b, "<dict><key>PayloadType</key><string>%s</string><key>PayloadIdentifier</key><string>%s</string>"+
			"<key>PayloadUUID</key><string>%s</string><key>PayloadVersion</key><integer>1</integer>", p[0], p[1], p[1])
		b.WriteString(p[2])
		b.WriteString("</dict>")
	}
	fmt.Fprintf(&b, "</array><key>PayloadDisplayName</key><string>Plan</string><key>PayloadType</key><string>Configuration</string>"+
		"<key>PayloadIdentifier</key><string>%s</string><key>PayloadUUID</key><string>%s</string>"+
		"<key>PayloadVersion</key><integer>1</integer></dict></plist>", planTopID, planTopID)
	return []byte(b.String())
}

var (
	planFinderPayload = [3]string{"com.apple.finder", planFinder, `<key>ProhibitBurn</key><true/>`}
	planDashPayload   = [3]string{"com.apple.dashboard", planDash, `<key>whiteListEnabled</key><false/>`}
	planDeskPayload   = [3]string{"com.apple.desktop", planDesk, ``}
)

func TestBuildImportPlan_PreservedCanTakeOverWhereConvertedCannot(t *testing.T) {
	profile := planProfile(planFinderPayload, planDashPayload)
	preserved, err := buildImportPlan(profile, importConvertOptions{}, true, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	converted, err := buildImportPlan(profile, importConvertOptions{}, false, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	// A payload type jamf-cli does not list is still sent as itself first, so
	// that the API gets to say whether it has started accepting it.
	if !preserved.takeover.Supported {
		t.Errorf("preserved plan should be able to take over: %v", preserved.takeover.Reasons)
	}
	if converted.takeover.Supported {
		t.Error("converted plan must not claim takeover")
	}
	if sameComponents(preserved, converted) {
		t.Error("the plans differ, so there is something to fall back to")
	}
	got := string(preserved.components[0].Configuration)
	if !strings.Contains(got, `"payloadType": "com.apple.dashboard"`) || strings.Contains(got, "ManagedClient") {
		t.Errorf("preserved plan changed a payload type:\n%s", got)
	}
	if !strings.Contains(string(converted.components[0].Configuration), "com.apple.ManagedClient.preferences") {
		t.Error("converted plan should deliver dashboard as Custom Settings")
	}
	if strings.Contains(string(converted.components[0].Configuration), planTopID) {
		t.Error("identity kept on a plan that cannot take over")
	}
}

// An MCX payload wrapping a standalone domain is unwrapped by conversion, which
// changes its type and so ends takeover. Preserving it keeps the type.
func TestBuildImportPlan_PreservedKeepsAnMCXPayloadWrapped(t *testing.T) {
	mcx := [3]string{
		"com.apple.ManagedClient.preferences", planDash,
		`<key>PayloadContent</key><dict><key>com.apple.finder</key><dict><key>Forced</key><array><dict>` +
			`<key>mcx_preference_settings</key><dict><key>ProhibitBurn</key><true/></dict></dict></array></dict></dict>`,
	}
	profile := planProfile(mcx)
	preserved, err := buildImportPlan(profile, importConvertOptions{}, true, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	converted, err := buildImportPlan(profile, importConvertOptions{}, false, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	if !preserved.takeover.Supported {
		t.Errorf("a preserved MCX payload should take over: %v", preserved.takeover.Reasons)
	}
	if converted.takeover.Supported {
		t.Error("unwrapping changes the payload type, so the converted plan cannot take over")
	}
}

func TestBuildImportPlan_EmptyPayloadIsKeptWhenPreserved(t *testing.T) {
	profile := planProfile(planFinderPayload, planDeskPayload)
	preserved, err := buildImportPlan(profile, importConvertOptions{}, true, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	converted, err := buildImportPlan(profile, importConvertOptions{}, false, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	if !preserved.takeover.Supported {
		t.Errorf("an empty payload no longer changes the count: %v", preserved.takeover.Reasons)
	}
	if converted.takeover.Supported {
		t.Error("dropping the empty payload changes the count, so the converted plan cannot take over")
	}
}

func TestBuildImportPlan_NothingToFallBackTo(t *testing.T) {
	profile := planProfile(planFinderPayload)
	a, err := buildImportPlan(profile, importConvertOptions{}, true, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildImportPlan(profile, importConvertOptions{}, false, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	if !sameComponents(a, b) {
		t.Error("a profile of listed types converts identically either way")
	}
}

func TestIsConfigurationRejection(t *testing.T) {
	const field = "steps[0].components[0].configuration"
	reject := jamfplatform.ErrorDetail{Code: "VALIDATION_FAILURE", Field: field, Description: "Failed to validate configuration."}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"the unsupported-type refusal", &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{reject}}, true},
		{"on a later component", &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{{Field: "steps[1].components[2].configuration", Description: "Failed to validate configuration."}}}, true},
		{"wrapped", fmt.Errorf("creating: %w", &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{reject}}), true},
		{"not a 400", &fakePlatformAPIError{status: 500, details: []jamfplatform.ErrorDetail{reject}}, false},
		{"a 403", &fakePlatformAPIError{status: 403, details: []jamfplatform.ErrorDetail{reject}}, false},
		{"a different 400", &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{{Field: "name", Description: "must not be blank"}}}, false},
		{"another problem beside it", &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{reject, {Field: "scope.deviceGroups", Description: "unknown group"}}}, false},
		{"no detail at all", &fakePlatformAPIError{status: 400}, false},
		{"not an API error", errors.New("context deadline exceeded"), false},
	}
	for _, tc := range cases {
		if got := isConfigurationRejection(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func fallbackFixture(t *testing.T) (*importPlan, *importPlan) {
	t.Helper()
	profile := planProfile(planFinderPayload, planDashPayload)
	asInstalled, err := buildImportPlan(profile, importConvertOptions{}, true, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := buildImportPlan(profile, importConvertOptions{}, false, offlineFetcher())
	if err != nil {
		t.Fatal(err)
	}
	return asInstalled, wrapped
}

func TestSendWithFallback(t *testing.T) {
	rejection := &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{
		{Field: "steps[0].components[0].configuration", Description: "Failed to validate configuration."},
	}}

	t.Run("accepted as installed", func(t *testing.T) {
		first, second := fallbackFixture(t)
		var sent []*importPlan
		var out bytes.Buffer
		got, id, err := sendWithFallback(&out, first, second, func(p *importPlan) (string, error) {
			sent = append(sent, p)
			return "bp-1", nil
		})
		if err != nil || id != "bp-1" || got != first || len(sent) != 1 {
			t.Fatalf("got plan=%v id=%q err=%v sent=%d", got == first, id, err, len(sent))
		}
		// dashboard is not in jamf-cli's list but the API took it: say so.
		if !strings.Contains(out.String(), "com.apple.dashboard") || !strings.Contains(out.String(), "may be out of date") {
			t.Errorf("no note that the list is out of date:\n%s", out.String())
		}
	})

	t.Run("rejected then sent wrapped", func(t *testing.T) {
		first, second := fallbackFixture(t)
		var sent []*importPlan
		var out bytes.Buffer
		got, id, err := sendWithFallback(&out, first, second, func(p *importPlan) (string, error) {
			sent = append(sent, p)
			if p == first {
				return "", rejection
			}
			return "bp-2", nil
		})
		if err != nil || id != "bp-2" || got != second || len(sent) != 2 {
			t.Fatalf("got fallback=%v id=%q err=%v sent=%d", got == second, id, err, len(sent))
		}
		if got.takeover.Supported {
			t.Error("the fallback blueprint cannot take over")
		}
		if !strings.Contains(out.String(), "rejected the payloads as installed") {
			t.Errorf("fallback not announced:\n%s", out.String())
		}
	})

	t.Run("both rejected reports both", func(t *testing.T) {
		first, second := fallbackFixture(t)
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, func(p *importPlan) (string, error) {
			return "", rejection
		})
		if err == nil || !strings.Contains(err.Error(), "also rejected") {
			t.Errorf("want both failures reported, got %v", err)
		}
	})

	t.Run("other failures are not retried", func(t *testing.T) {
		for name, e := range map[string]error{
			"server error": &fakePlatformAPIError{status: 500},
			"timeout":      errors.New("context deadline exceeded"),
			"bad scope":    &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{{Field: "scope.deviceGroups", Description: "unknown"}}},
		} {
			first, second := fallbackFixture(t)
			calls := 0
			_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, func(*importPlan) (string, error) {
				calls++
				return "", e
			})
			if !errors.Is(err, e) || calls != 1 {
				t.Errorf("%s: err=%v calls=%d, want the original error after one send", name, err, calls)
			}
		}
	})

	t.Run("no fallback to send", func(t *testing.T) {
		first, _ := fallbackFixture(t)
		calls := 0
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, nil, func(*importPlan) (string, error) {
			calls++
			return "", rejection
		})
		if !errors.Is(err, rejection) || calls != 1 {
			t.Errorf("err=%v calls=%d", err, calls)
		}
	})
}

// A screensaver payload with no moduleName is refused by the API for the whole
// blueprint. Keeping the profile as installed used to send it anyway and fail;
// it now drops it, as the converted import does, and says so.
func TestBuildImportPlan_PreservedValidatesLikeTheConvertedImport(t *testing.T) {
	profile := planProfile(planFinderPayload, [3]string{"com.apple.screensaver", "FFFFFFFF-98B3-4ED1-8235-649E41003E36", `<key>idleTime</key><integer>600</integer>`})
	fetcher := profileconvert.NewSchemaFetcher(&http.Client{Transport: screensaverSchema{}})

	plan, err := buildImportPlan(profile, importConvertOptions{}, true, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plan.components[0].Configuration), "screensaver") {
		t.Errorf("payload missing a required key was sent:\n%s", plan.components[0].Configuration)
	}
	if !strings.Contains(strings.Join(plan.messages, "\n"), "removed payload com.apple.screensaver") {
		t.Errorf("the removal was not reported:\n%v", plan.messages)
	}
	if plan.takeover.Supported {
		t.Error("a removed payload changes the count, so takeover must be refused")
	}
}

type screensaverSchema struct{}

func (screensaverSchema) RoundTrip(r *http.Request) (*http.Response, error) {
	body := ""
	if strings.Contains(r.URL.Path, "screensaver") {
		body = "payload:\n  payloadtype: com.apple.screensaver\npayloadkeys:\n- key: moduleName\n  type: <string>\n  presence: required\n"
	}
	status := http.StatusNotFound
	if body != "" {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}
