// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
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

const scopeXMLWithNarrowing = `<os_x_configuration_profile><scope><all_computers>false</all_computers>
<computer_groups><computer_group><id>1</id><name>Target</name></computer_group></computer_groups>
<limitations><user_groups><user_group><id>7</id><name>Staff</name></user_group></user_groups></limitations>
<exclusions><computer_groups><computer_group><id>2</id><name>Excluded</name></computer_group>
<computer_group><id>5</id><name>Also Excluded</name></computer_group></computer_groups>
<mobile_device_groups><mobile_device_group><id>9</id><name>iPads Out</name></mobile_device_group></mobile_device_groups>
<computers><computer><id>3</id><name>One</name></computer><computer><id>4</id><name>Two</name></computer></computers></exclusions>
</scope></os_x_configuration_profile>`

func TestProfileScopeNarrowing(t *testing.T) {
	n := profileScopeNarrowing([]byte(scopeXMLWithNarrowing))
	// Excluded device groups can be carried over; individual computers cannot.
	want := []excludedGroup{{"Excluded", "COMPUTER"}, {"Also Excluded", "COMPUTER"}, {"iPads Out", "MOBILE"}}
	if len(n.excludedGroups) != len(want) {
		t.Fatalf("excluded groups = %v, want %v", n.excludedGroups, want)
	}
	for i, g := range want {
		if n.excludedGroups[i] != g {
			t.Errorf("excluded group %d = %v, want %v", i, n.excludedGroups[i], g)
		}
	}
	if n.otherExclusions != 2 || n.limitations != 1 {
		t.Errorf("other exclusions = %d, limitations = %d; want 2 and 1", n.otherExclusions, n.limitations)
	}
	if n := profileScopeNarrowing([]byte(`<x><scope><all_computers>false</all_computers></scope></x>`)); len(n.excludedGroups)+n.otherExclusions+n.limitations != 0 {
		t.Errorf("a plain scope subtracts nothing: %+v", n)
	}
	if n := profileScopeNarrowing([]byte(`<x/>`)); len(n.excludedGroups)+n.otherExclusions+n.limitations != 0 {
		t.Errorf("no scope at all: %+v", n)
	}
}

func TestRefuseUnexpressibleScope(t *testing.T) {
	cases := []struct {
		name              string
		n                 scopeNarrowing
		skipExcl, skipLim bool
		wantErr           bool
		mustMention       []string
		mustNotName       []string
	}{
		{name: "imports by default, whatever it has", n: scopeNarrowing{otherExclusions: 2, limitations: 1}},
		{name: "exclusions refused", n: scopeNarrowing{otherExclusions: 2}, skipExcl: true, wantErr: true, mustMention: []string{"2 scope exclusion(s)", "--skip-exclusions"}, mustNotName: []string{"--skip-limitations"}},
		{name: "limitations refused", n: scopeNarrowing{limitations: 1}, skipLim: true, wantErr: true, mustMention: []string{"1 scope limitation(s)", "--skip-limitations"}, mustNotName: []string{"--skip-exclusions"}},
		{name: "both named together", n: scopeNarrowing{otherExclusions: 2, limitations: 1}, skipExcl: true, skipLim: true, wantErr: true, mustMention: []string{"--skip-exclusions", "--skip-limitations"}},
		{name: "a flag only judges its own kind", n: scopeNarrowing{limitations: 1}, skipExcl: true},
		{name: "excluded groups are carried over, so they never refuse", n: scopeNarrowing{excludedGroups: []excludedGroup{{"G", "COMPUTER"}}}, skipExcl: true, skipLim: true},
		{name: "nothing to refuse", skipExcl: true, skipLim: true},
	}
	for _, tc := range cases {
		err := refuseUnexpressibleScope("id 1", tc.n, tc.skipExcl, tc.skipLim)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, want error %v", tc.name, err, tc.wantErr)
			continue
		}
		if err == nil {
			continue
		}
		for _, m := range tc.mustMention {
			if !strings.Contains(err.Error(), m) {
				t.Errorf("%s: error lacks %q: %v", tc.name, m, err)
			}
		}
		for _, m := range tc.mustNotName {
			if strings.Contains(err.Error(), m) {
				t.Errorf("%s: error names %q though it does not apply: %v", tc.name, m, err)
			}
		}
	}
}

func TestScopeNarrowingNote(t *testing.T) {
	for _, tc := range []struct {
		carried, other, lim int
		want                string
	}{
		{2, 1, 3, "2 excluded group(s) carried over as an activation condition. 1 exclusion(s) and 3 limitation(s) not carried over."},
		{2, 0, 0, "2 excluded group(s) carried over as an activation condition."},
		{0, 1, 0, "1 exclusion(s) not carried over."},
		{0, 0, 2, "2 limitation(s) not carried over."},
		{0, 0, 0, ""},
	} {
		if got := scopeNarrowingNote(tc.carried, tc.other, tc.lim); got != tc.want {
			t.Errorf("note(%d,%d,%d) = %q, want %q", tc.carried, tc.other, tc.lim, got, tc.want)
		}
	}
}

// One NONE over the whole set excludes a device in any of the groups. A NONE per
// group joined with OR does not, which is the mistake the form guards against.
func TestExclusionPredicate(t *testing.T) {
	got := exclusionPredicate([]string{"69db9494-d1dc-486a-9f90-5936c5bfa2a2", "266dd32e-8802-42e6-8f46-46d00e89ab91"})
	want := "NONE @property(jamf.device.groups) IN {'69db9494-d1dc-486a-9f90-5936c5bfa2a2', '266dd32e-8802-42e6-8f46-46d00e89ab91'}"
	if got != want {
		t.Errorf("predicate = %q, want %q", got, want)
	}
	if strings.Contains(got, " OR ") || strings.Count(got, "NONE") != 1 {
		t.Errorf("must be a single NONE over one set: %q", got)
	}
	if predicatePtr("") != nil {
		t.Error("no exclusions means no predicate at all")
	}
	if p := predicatePtr(got); p == nil || *p != got {
		t.Error("predicatePtr must carry the predicate")
	}
}

func TestResolveExcludedGroups(t *testing.T) {
	mock := &classicHTTPMock{statusCode: 200, body: `{"totalCount":1,"results":[{"groupPlatformId":"uuid-excl","groupName":"Excluded"}]}`}
	ids, warnings := resolveExcludedGroups(context.Background(), mock, []excludedGroup{{"Excluded", "COMPUTER"}, {"Excluded", "COMPUTER"}})
	if len(ids) != 1 || ids[0] != "uuid-excl" || len(warnings) != 0 {
		t.Errorf("ids = %v warnings = %v, want one ID and no warning (duplicates collapse)", ids, warnings)
	}

	ids, warnings = resolveExcludedGroups(context.Background(), &classicHTTPMock{statusCode: 200, body: `{"totalCount":0,"results":[]}`}, []excludedGroup{{"Gone", "MOBILE"}})
	if len(ids) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], `"Gone"`) {
		t.Errorf("an unresolvable group is reported and left out: ids = %v warnings = %v", ids, warnings)
	}
}
