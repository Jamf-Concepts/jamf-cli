// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	jamfplatform "github.com/jamf/jamfplatform-go-sdk/jamfplatform"
	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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
		got, id, err := sendWithFallback(&out, first, second, false, nil, func(p *importPlan) (string, error) {
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
		got, id, err := sendWithFallback(&out, first, second, false, nil, func(p *importPlan) (string, error) {
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

	t.Run("the hook runs against the fallback before it is sent", func(t *testing.T) {
		first, second := fallbackFixture(t)
		var gated *importPlan
		var sent []*importPlan
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, false, func(p *importPlan) error {
			gated = p
			return nil
		}, func(p *importPlan) (string, error) {
			sent = append(sent, p)
			if p == first {
				return "", rejection
			}
			return "bp-2", nil
		})
		if err != nil || gated != second || len(sent) != 2 {
			t.Fatalf("gated=%v sent=%d err=%v", gated == second, len(sent), err)
		}
	})

	t.Run("a declined hook sends no fallback and reports the decline untouched", func(t *testing.T) {
		first, second := fallbackFixture(t)
		declined := errors.New("declined")
		var sent int
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, false, func(*importPlan) error { return declined }, func(*importPlan) (string, error) {
			sent++
			return "", rejection
		})
		if !errors.Is(err, declined) || err.Error() != "declined" || sent != 1 {
			t.Errorf("err=%v sent=%d, want the decline as is after one send", err, sent)
		}
	})

	t.Run("both rejected reports both", func(t *testing.T) {
		first, second := fallbackFixture(t)
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, false, nil, func(p *importPlan) (string, error) {
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
			_, _, err := sendWithFallback(&bytes.Buffer{}, first, second, false, nil, func(*importPlan) (string, error) {
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
		_, _, err := sendWithFallback(&bytes.Buffer{}, first, nil, false, nil, func(*importPlan) (string, error) {
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

func TestSendWithFallback_TakeoverOnlyNeverSendsTheConvertedProfile(t *testing.T) {
	rejection := &fakePlatformAPIError{status: 400, details: []jamfplatform.ErrorDetail{
		{Field: "steps[0].components[0].configuration", Description: "Failed to validate configuration."},
	}}
	first, second := fallbackFixture(t)
	calls := 0
	_, id, err := sendWithFallback(&bytes.Buffer{}, first, second, true, nil, func(*importPlan) (string, error) {
		calls++
		return "", rejection
	})
	if calls != 1 || id != "" {
		t.Errorf("calls = %d id = %q, want one send and nothing created", calls, id)
	}
	if err == nil || !strings.Contains(err.Error(), "--takeover-only") || !errors.Is(err, rejection) {
		t.Errorf("want the rejection wrapped with the reason, got %v", err)
	}

	// An accepted profile is unaffected by the flag.
	first, second = fallbackFixture(t)
	if _, id, err := sendWithFallback(&bytes.Buffer{}, first, second, true, nil, func(*importPlan) (string, error) { return "bp-1", nil }); err != nil || id != "bp-1" {
		t.Errorf("accepted profile: id = %q err = %v", id, err)
	}
}

func TestNotImportedForTakeover(t *testing.T) {
	err := notImportedForTakeover("id 9", []string{"payload 1 (x) is delivered as y", "r2"})
	for _, want := range []string{"id 9 was not imported", "--takeover-only", "payload 1 (x) is delivered as y", "r2", "Drop the flag"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestParseClassicProfileList(t *testing.T) {
	computer := `<os_x_configuration_profiles><size>2</size>
<os_x_configuration_profile><id>9696</id><name>Finder Takeover</name></os_x_configuration_profile>
<os_x_configuration_profile><id>9697</id><name>Energy Saver Takeover</name></os_x_configuration_profile></os_x_configuration_profiles>`
	mobile := `<configuration_profiles><size>1</size><configuration_profile><id>1006</id><name>Passcode - 1:1</name></configuration_profile></configuration_profiles>`
	for name, tc := range map[string]struct {
		body string
		want []classicProfileRef
	}{
		"computer": {computer, []classicProfileRef{{"9696", "Finder Takeover"}, {"9697", "Energy Saver Takeover"}}},
		"mobile":   {mobile, []classicProfileRef{{"1006", "Passcode - 1:1"}}},
		"empty":    {`<os_x_configuration_profiles><size>0</size></os_x_configuration_profiles>`, []classicProfileRef{}},
	} {
		got, err := parseClassicProfileList([]byte(tc.body))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s: entry %d = %v, want %v", name, i, got[i], tc.want[i])
			}
		}
	}
}

func TestRunImportAll(t *testing.T) {
	// printRows renders in the global -o format.
	prevFormat := outputFmt
	outputFmt = "json"
	t.Cleanup(func() { outputFmt = prevFormat })
	list := `<os_x_configuration_profiles><os_x_configuration_profile><id>1</id><name>Ok</name></os_x_configuration_profile>` +
		`<os_x_configuration_profile><id>2</id><name>Refused</name></os_x_configuration_profile>` +
		`<os_x_configuration_profile><id>3</id><name>Broken</name></os_x_configuration_profile></os_x_configuration_profiles>`
	newCtx := func(buf *bytes.Buffer) *registry.CLIContext {
		ctx := newTestCtx(buf, "json")
		ctx.Client = &classicRouteMock{routes: map[string]string{"/JSSResource/osxconfigurationprofiles": list}}
		return ctx
	}
	var calls []string
	one := func(results map[string]error) func(context.Context, string, io.Writer, importRun) (*importOutcome, error) {
		return func(_ context.Context, id string, w io.Writer, run importRun) (*importOutcome, error) {
			calls = append(calls, fmt.Sprintf("%s/preview=%v", id, run.preview))
			if run.profileType != "" {
				calls[len(calls)-1] += "/" + run.profileType
			}
			_, _ = io.WriteString(w, "detail for "+id+"\n")
			err := results[id]
			if err != nil {
				return &importOutcome{ProfileID: id}, err
			}
			out := &importOutcome{ProfileID: id, Profile: "Ok", Takeover: "supported", BlueprintID: "bp-" + id, Deployed: !run.preview}
			if id == "3" {
				// Dropped exclusions: created, but not deployed.
				out.ScopeWidened, out.Deployed = 2, false
				out.NotDeployed = "not deployed: 2 exclusion(s)/limitation(s) were dropped"
			}
			return out, nil
		}
	}

	t.Run("a skipped profile is not a failure and the table lists every profile", func(t *testing.T) {
		var buf bytes.Buffer
		err := runImportAll(context.Background(), newCtx(&buf), []string{"computer"}, true, true,
			one(map[string]error{"2": &skipError{msg: "takeover is not supported for it\nreasons"}, "3": nil}))
		if err != nil {
			t.Fatalf("a skip must not fail the run: %v", err)
		}
		out := buf.String()
		for _, want := range []string{`"result": "created"`, `"result": "skipped"`, `"detail": "takeover is not supported for it"`, `"deployed": "yes"`, `"blueprint": "bp-1"`, `not deployed: 2 exclusion(s)`} {
			if !strings.Contains(out, want) {
				t.Errorf("table lacks %s:\n%s", want, out)
			}
		}
	})

	t.Run("--deploy counts first with a pass that creates nothing", func(t *testing.T) {
		calls = nil
		var buf bytes.Buffer
		if err := runImportAll(context.Background(), newCtx(&buf), []string{"computer"}, true, true, one(nil)); err != nil {
			t.Fatal(err)
		}
		want := []string{"1/preview=true/computer", "2/preview=true/computer", "3/preview=true/computer", "1/preview=false/computer", "2/preview=false/computer", "3/preview=false/computer"}
		if strings.Join(calls, " ") != strings.Join(want, " ") {
			t.Errorf("calls = %v, want a preview pass then the real one: %v", calls, want)
		}
		calls = nil
		if err := runImportAll(context.Background(), newCtx(&buf), []string{"computer"}, false, true, one(nil)); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 3 {
			t.Errorf("without --deploy there is no counting pass: %v", calls)
		}
	})

	t.Run("both types are imported and the table says which is which", func(t *testing.T) {
		calls = nil
		var buf bytes.Buffer
		ctx := newCtx(&buf)
		ctx.Client = &classicRouteMock{routes: map[string]string{
			"/JSSResource/osxconfigurationprofiles":          `<os_x_configuration_profiles><os_x_configuration_profile><id>10</id><name>Mac</name></os_x_configuration_profile></os_x_configuration_profiles>`,
			"/JSSResource/mobiledeviceconfigurationprofiles": `<configuration_profiles><configuration_profile><id>20</id><name>Phone</name></configuration_profile></configuration_profiles>`,
		}}
		if err := runImportAll(context.Background(), ctx, []string{"computer", "mobile"}, false, false, one(nil)); err != nil {
			t.Fatal(err)
		}
		if strings.Join(calls, " ") != "10/preview=false/computer 20/preview=false/mobile" {
			t.Errorf("calls = %v, want the computer profile then the mobile one", calls)
		}
		for _, want := range []string{`"type": "computer"`, `"type": "mobile"`} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("table lacks %s:\n%s", want, buf.String())
			}
		}
	})

	t.Run("a failure beside a success is a partial failure", func(t *testing.T) {
		var buf bytes.Buffer
		err := runImportAll(context.Background(), newCtx(&buf), []string{"computer"}, false, true,
			one(map[string]error{"3": errors.New("boom")}))
		if err == nil || exitcode.CodeFrom(err) != exitcode.PartialFailure {
			t.Errorf("want a partial-failure exit, got %v", err)
		}
		if !strings.Contains(buf.String(), `"result": "failed"`) {
			t.Errorf("table lacks the failure:\n%s", buf.String())
		}
	})

	t.Run("nothing to import is not an error", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)
		ctx.Client = &classicRouteMock{routes: map[string]string{"/JSSResource/osxconfigurationprofiles": `<os_x_configuration_profiles><size>0</size></os_x_configuration_profiles>`}}
		if err := runImportAll(context.Background(), ctx, []string{"computer"}, false, false, one(nil)); err != nil {
			t.Errorf("an empty list: %v", err)
		}
	})
}

func TestSkipRefusalsAreSkipErrors(t *testing.T) {
	var skip *skipError
	if err := notImportedForTakeover("id 9", []string{"r"}); !errors.As(err, &skip) {
		t.Errorf("--takeover-only refusal is not a skip: %v", err)
	}
	if err := refuseUnexpressibleScope("id 9", scopeNarrowing{limitations: 1}, false, true); !errors.As(err, &skip) {
		t.Errorf("--skip-limitations refusal is not a skip: %v", err)
	}
}

func TestNothingToImportIsASkip(t *testing.T) {
	var skip *skipError
	for _, msg := range []string{
		"converting profile: no payloads remain after skipping types blueprints does not support",
		"no components produced — all payloads were stripped or unsupported",
	} {
		if err := nothingToImport(errors.New(msg)); !errors.As(err, &skip) {
			t.Errorf("%q should be a skip, got %v", msg, err)
		}
	}
	boom := errors.New("connection reset")
	if got := nothingToImport(boom); got != boom {
		t.Errorf("another error must pass through unchanged, got %v", got)
	}
	if nothingToImport(nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestDeployCountsClassifyEveryOutcome(t *testing.T) {
	var c deployCounts
	for _, o := range []*importOutcome{
		{Takeover: "supported"},
		{Takeover: "supported"},
		{Takeover: "supported", TakeoverUncertain: true},
		{Takeover: "not supported"},
		{Takeover: "not supported"},
		{Takeover: "not supported"},
		{Takeover: "supported", ScopeWidened: 2},
		{Takeover: "not supported", ScopeWidened: 1},
		{Takeover: "supported", TakeoverUncertain: true, ScopeWidened: 1},
		{Takeover: "supported", ScopeWidened: 1},
	} {
		c.add(o)
	}
	if got, want := c, (deployCounts{takeOver: 2, uncertain: 1, alongside: 3, held: 4}); got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}
	if c.deployable() != 6 {
		t.Errorf("deployable = %d, want 6: held blueprints are not deployed", c.deployable())
	}
	want := "take over the installed profile for 2; take it over for up to 1 more only if the API accepts payload types jamf-cli lists as unsupported " +
		"(otherwise they install alongside); install alongside the Classic profile (both stay active) for 3; create but not deploy 4 whose scope would widen."
	if !strings.Contains(c.summary(), want) {
		t.Errorf("summary = %q, want it to contain %q", c.summary(), want)
	}
}

func TestRunImportAllDeployConfirmationReportsTheCounts(t *testing.T) {
	prevFormat := outputFmt
	outputFmt = "json"
	t.Cleanup(func() { outputFmt = prevFormat })
	list := `<os_x_configuration_profiles>`
	for _, id := range []string{"1", "2", "3", "4"} {
		list += `<os_x_configuration_profile><id>` + id + `</id><name>P` + id + `</name></os_x_configuration_profile>`
	}
	list += `</os_x_configuration_profiles>`
	var buf bytes.Buffer
	cliCtx := newTestCtx(&buf, "json")
	cliCtx.Client = &classicRouteMock{routes: map[string]string{"/JSSResource/osxconfigurationprofiles": list}}
	outcomes := map[string]importOutcome{
		"1": {Takeover: "supported"},
		"2": {Takeover: "supported", ScopeWidened: 1},
		"3": {Takeover: "supported", TakeoverUncertain: true},
		"4": {Takeover: "not supported"},
	}
	importOne := func(_ context.Context, id string, _ io.Writer, _ importRun) (*importOutcome, error) {
		o := outcomes[id]
		o.ProfileID = id
		return &o, nil
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevStderr := os.Stderr
	os.Stderr = w
	runErr := runImportAll(context.Background(), cliCtx, []string{"computer"}, true, true, importOne)
	os.Stderr = prevStderr
	_ = w.Close()
	captured, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatal(runErr)
	}
	want := "take over the installed profile for 1; take it over for up to 1 more only if the API accepts payload types jamf-cli lists as unsupported " +
		"(otherwise they install alongside); install alongside the Classic profile (both stay active) for 1; create but not deploy 1 whose scope would widen."
	if !strings.Contains(string(captured), want) {
		t.Errorf("stderr lacks the mixed counts %q:\n%s", want, captured)
	}
}

func TestRefuseExistingBlueprint(t *testing.T) {
	list := func(items ...blueprints.BlueprintOverview) func(context.Context, []string, string) ([]blueprints.BlueprintOverview, error) {
		return func(_ context.Context, _ []string, search string) ([]blueprints.BlueprintOverview, error) {
			if search != "WiFi" {
				t.Errorf("searched for %q, want the blueprint name", search)
			}
			return items, nil
		}
	}
	t.Run("an exact name is a skip that names the blueprint", func(t *testing.T) {
		err := refuseExistingBlueprint(context.Background(), list(blueprints.BlueprintOverview{ID: "bp-9", Name: "WiFi"}), "WiFi")
		var skip *skipError
		if !errors.As(err, &skip) || !strings.Contains(err.Error(), "bp-9") {
			t.Errorf("err = %v, want a skip naming bp-9", err)
		}
	})
	t.Run("the search also matches descriptions, which are not a collision", func(t *testing.T) {
		err := refuseExistingBlueprint(context.Background(), list(blueprints.BlueprintOverview{ID: "bp-1", Name: "WiFi (copy)"}), "WiFi")
		if err != nil {
			t.Errorf("err = %v, want none: only an exact name collides", err)
		}
	})
	t.Run("a failed lookup stops the import rather than guessing", func(t *testing.T) {
		boom := errors.New("boom")
		err := refuseExistingBlueprint(context.Background(), func(context.Context, []string, string) ([]blueprints.BlueprintOverview, error) {
			return nil, boom
		}, "WiFi")
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want the lookup failure", err)
		}
	})
}
