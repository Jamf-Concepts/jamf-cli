// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingClient serves fixed GET bodies and records the last write.
type recordingClient struct {
	gets      map[string]string
	method    string
	path      string
	body      string
	getsTaken []string
}

func (r *recordingClient) Do(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if method == http.MethodGet {
		r.getsTaken = append(r.getsTaken, path)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(r.gets[path])), Header: http.Header{}}, nil
	}
	r.method, r.path = method, path
	if body != nil {
		b, _ := io.ReadAll(body)
		r.body = string(b)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
}

func sendThrough(t *testing.T, inner *recordingClient, method, path, body string) map[string]any {
	t.Helper()
	c := &staticGroupBodyClient{inner: inner}
	if _, err := c.Do(context.Background(), method, path, strings.NewReader(body)); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(inner.body), &sent); err != nil {
		t.Fatalf("sent body %q is not JSON: %v", inner.body, err)
	}
	return sent
}

// The v3 PUT replaces the member list, so an update naming no members carries
// the current ones, read from the Classic group.
func TestStaticGroupBodies_V3UpdateKeepsMembers(t *testing.T) {
	inner := &recordingClient{gets: map[string]string{
		"/JSSResource/computergroups/id/7": `{"computer_group":{"id":7,"name":"G","computers":[{"id":82},{"id":106}]}}`,
	}}
	sent := sendThrough(t, inner, http.MethodPut, "/v3/computer-groups/static-groups/7", `{"name":"G","description":"x"}`)
	got, _ := json.Marshal(sent["assignments"])
	if string(got) != `["82","106"]` {
		t.Errorf("assignments = %s, want the current members [\"82\",\"106\"]", got)
	}

	inner = &recordingClient{}
	sent = sendThrough(t, inner, http.MethodPut, "/v3/computer-groups/static-groups/7", `{"name":"G","assignments":["1"]}`)
	if got, _ := json.Marshal(sent["assignments"]); string(got) != `["1"]` || len(inner.getsTaken) != 0 {
		t.Errorf("an explicit assignments was changed (%s) or fetched for (%v)", got, inner.getsTaken)
	}
}

// The fill replaces the member list, so a read it cannot vouch for is refused
// before anything is written, rather than sent as an empty list that clears
// the group. An empty group (Classic sends `[]`) still fills as empty.
func TestStaticGroupBodies_V3UpdateRefusesAnUnreadableMemberList(t *testing.T) {
	for name, detail := range map[string]string{
		"no member list": `{"computer_group":{"id":7,"name":"G"}}`,
		"smart group":    `{"computer_group":{"id":7,"name":"G","is_smart":true,"computers":[]}}`,
		"member no id":   `{"computer_group":{"id":7,"name":"G","computers":[{"name":"Mac"}]}}`,
		"non-object":     `{"computer_group":{"id":7,"name":"G","computers":["82"]}}`,
	} {
		inner := &recordingClient{gets: map[string]string{"/JSSResource/computergroups/id/7": detail}}
		c := &staticGroupBodyClient{inner: inner}
		if _, err := c.Do(context.Background(), http.MethodPut, "/v3/computer-groups/static-groups/7", strings.NewReader(`{"name":"G"}`)); err == nil {
			t.Errorf("%s: the update was sent (%s)", name, inner.body)
		}
		if inner.method != "" {
			t.Errorf("%s: wrote %s %s", name, inner.method, inner.path)
		}
	}

	inner := &recordingClient{gets: map[string]string{"/JSSResource/computergroups/id/7": `{"computer_group":{"id":7,"name":"G","is_smart":false,"computers":[]}}`}}
	sent := sendThrough(t, inner, http.MethodPut, "/v3/computer-groups/static-groups/7", `{"name":"G"}`)
	if got, _ := json.Marshal(sent["assignments"]); string(got) != `[]` {
		t.Errorf("empty group assignments = %s, want []", got)
	}
}

// A PATCH fill the read cannot supply is refused rather than sent as null.
func TestStaticGroupBodies_V2PatchRefusesAMissingName(t *testing.T) {
	inner := &recordingClient{gets: map[string]string{
		"/v2/mobile-device-groups/static-groups/9": `{"groupId":"9","siteId":"-1"}`,
	}}
	c := &staticGroupBodyClient{inner: inner}
	_, err := c.Do(context.Background(), http.MethodPatch, "/v2/mobile-device-groups/static-groups/9", strings.NewReader(`{"groupDescription":"d"}`))
	if err == nil || !strings.Contains(err.Error(), `"groupName"`) || inner.method != "" {
		t.Errorf("err = %v, wrote %q; want a refusal naming groupName and nothing sent", err, inner.method)
	}
}

func TestStaticGroupBodies_Creates(t *testing.T) {
	sent := sendThrough(t, &recordingClient{}, http.MethodPost, "/v3/computer-groups/static-groups", `{"name":"G"}`)
	if got, _ := json.Marshal(sent["assignments"]); string(got) != `[]` {
		t.Errorf("v3 create assignments = %s, want []", got)
	}
	sent = sendThrough(t, &recordingClient{}, http.MethodPost, "/v2/mobile-device-groups/static-groups", `{"groupName":"G"}`)
	if got, _ := json.Marshal(sent["assignments"]); string(got) != `[]` || sent["siteId"] != "-1" {
		t.Errorf("v2 create = %v, want assignments [] and siteId -1", sent)
	}
	sent = sendThrough(t, &recordingClient{}, http.MethodPost, "/v2/mobile-device-groups/static-groups", `{"groupName":"G","siteId":"3"}`)
	if sent["siteId"] != "3" {
		t.Errorf("an explicit siteId was replaced: %v", sent["siteId"])
	}
}

// The v2 PATCH needs the group's name and site in every body; the PATCH is
// incremental, so an empty assignment list changes no members.
func TestStaticGroupBodies_V2PatchFillsNameAndSite(t *testing.T) {
	inner := &recordingClient{gets: map[string]string{
		"/v2/mobile-device-groups/static-groups/9": `{"groupId":"9","groupName":"Lab","siteId":"-1"}`,
	}}
	sent := sendThrough(t, inner, http.MethodPatch, "/v2/mobile-device-groups/static-groups/9", `{"groupDescription":"d"}`)
	if sent["groupName"] != "Lab" || sent["siteId"] != "-1" || sent["groupDescription"] != "d" {
		t.Errorf("sent = %v, want groupName Lab, siteId -1 and the description kept", sent)
	}
	if got, _ := json.Marshal(sent["assignments"]); string(got) != `[]` {
		t.Errorf("assignments = %s, want []", got)
	}

	inner = &recordingClient{}
	sendThrough(t, inner, http.MethodPatch, "/v2/mobile-device-groups/static-groups/9", `{"groupName":"New","siteId":"2"}`)
	if len(inner.getsTaken) != 0 {
		t.Errorf("fetched %v although name and site were given", inner.getsTaken)
	}
}

func TestStaticGroupBodies_OtherRequestsUntouched(t *testing.T) {
	inner := &recordingClient{}
	c := &staticGroupBodyClient{inner: inner}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/v3/computer-groups/smart-groups/7", `{"name":"G"}`},
		{http.MethodDelete, "/v3/computer-groups/static-groups/7", ``},
		{http.MethodPut, "/v3/computer-groups/static-groups/7", `not json`},
	} {
		if _, err := c.Do(context.Background(), tc.method, tc.path, strings.NewReader(tc.body)); err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		if inner.body != tc.body {
			t.Errorf("%s %s body = %q, want it passed through as %q", tc.method, tc.path, inner.body, tc.body)
		}
	}
}

// The wrapper is on the six generated leaves and their help says so.
func TestStaticGroupBodies_WiredOnTheGeneratedLeaves(t *testing.T) {
	root := NewRootCmd("test", "none", "none", "none")
	for _, path := range [][]string{
		{"pro", "computer-groups-static-groups", "create"},
		{"pro", "computer-groups-static-groups", "update"},
		{"pro", "computer-groups-static-groups", "apply"},
		{"pro", "mobile-device-groups-static-groups", "create"},
		{"pro", "mobile-device-groups-static-groups", "patch"},
		{"pro", "mobile-device-groups-static-groups", "apply"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Errorf("%v: not found", path)
			continue
		}
		if !strings.Contains(cmd.Long, `"assignments"`) {
			t.Errorf("%v: help does not carry the body note", path)
		}
	}
}
