// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/spf13/cobra"
)

type recordingGroupClient struct {
	mu       sync.Mutex
	gets     map[string]string
	requests []string
}

func (c *recordingGroupClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, method+" "+path)
	c.mu.Unlock()
	if method == http.MethodGet {
		if body, ok := c.gets[path]; ok {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func (c *recordingGroupClient) writes() []string {
	var out []string
	for _, r := range c.requests {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

type groupFixture struct {
	id, name string
	members  []string
}

func classicGroupsFake(groupsPath, listRoot, itemKey, membersKey, memberKey string, groups []groupFixture) *recordingGroupClient {
	var list strings.Builder
	fmt.Fprintf(&list, `<?xml version="1.0" encoding="UTF-8"?><%s><size>%d</size>`, listRoot, len(groups))
	gets := map[string]string{}
	for _, g := range groups {
		fmt.Fprintf(&list, `<%s><id>%s</id><name>%s</name></%s>`, itemKey, g.id, g.name, itemKey)
		var d strings.Builder
		fmt.Fprintf(&d, `<?xml version="1.0" encoding="UTF-8"?><%s><id>%s</id><name>%s</name><%s>`, itemKey, g.id, g.name, membersKey)
		for _, m := range g.members {
			fmt.Fprintf(&d, `<%s><id>%s</id></%s>`, memberKey, m, memberKey)
		}
		fmt.Fprintf(&d, `</%s></%s>`, membersKey, itemKey)
		gets[groupsPath+"/id/"+g.id] = d.String()
	}
	fmt.Fprintf(&list, `</%s>`, listRoot)
	gets[groupsPath] = list.String()
	return &recordingGroupClient{gets: gets}
}

type groupDeleteSurface struct {
	name       string
	newCmd     func(*registry.CLIContext) *cobra.Command
	groupsPath string
	listRoot   string
	itemKey    string
	membersKey string
	memberKey  string
	deletePath func(id string) string
}

var groupDeleteSurfaces = []groupDeleteSurface{
	{
		name:       "pro computer-inventory delete --group",
		newCmd:     generated.NewComputerInventoryCmd,
		groupsPath: "/JSSResource/computergroups",
		listRoot:   "computer_groups",
		itemKey:    "computer_group",
		membersKey: "computers",
		memberKey:  "computer",
		deletePath: func(id string) string { return "DELETE /v4/computers-inventory/" + id },
	},
	{
		name:       "pro classic-mobile-devices delete --group",
		newCmd:     generated.NewClassicMobileDevicesCmd,
		groupsPath: "/JSSResource/mobiledevicegroups",
		listRoot:   "mobile_device_groups",
		itemKey:    "mobile_device_group",
		membersKey: "mobile_devices",
		memberKey:  "mobile_device",
		deletePath: func(id string) string { return "DELETE /JSSResource/mobiledevices/id/" + id },
	},
}

func runGroupDelete(t *testing.T, s groupDeleteSurface, client *recordingGroupClient, group string) error {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cmd := s.newCmd(&registry.CLIContext{Client: client})
	cmd.SetArgs([]string{"delete", "--group", group, "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

func TestGroupFlagDeletePrefersExactCaseMatch(t *testing.T) {
	for _, s := range groupDeleteSurfaces {
		t.Run(s.name, func(t *testing.T) {
			client := classicGroupsFake(s.groupsPath, s.listRoot, s.itemKey, s.membersKey, s.memberKey, []groupFixture{
				{id: "7", name: "LAB", members: []string{"701", "702"}},
				{id: "8", name: "Lab", members: []string{"801"}},
			})
			err := runGroupDelete(t, s, client, "Lab")
			got := client.writes()
			want := []string{s.deletePath("801")}
			if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("--group Lab with groups LAB(7) and Lab(8): err=%v writes=%v, want writes=%v (exact-case match must win)", err, got, want)
			}
		})
	}
}

func TestGroupFlagDeleteRefusesAmbiguousCaseInsensitiveMatch(t *testing.T) {
	for _, s := range groupDeleteSurfaces {
		t.Run(s.name, func(t *testing.T) {
			client := classicGroupsFake(s.groupsPath, s.listRoot, s.itemKey, s.membersKey, s.memberKey, []groupFixture{
				{id: "7", name: "LAB", members: []string{"701", "702"}},
				{id: "8", name: "lab", members: []string{"801"}},
			})
			err := runGroupDelete(t, s, client, "Lab")
			if got := client.writes(); len(got) != 0 {
				t.Fatalf("--group Lab with groups LAB(7) and lab(8), no exact match: sent %v, want no write", got)
			}
			if err == nil || !strings.Contains(err.Error(), "7") || !strings.Contains(err.Error(), "8") {
				t.Fatalf("--group Lab ambiguous: err=%v, want a refusal naming ids 7 and 8", err)
			}
		})
	}
}

func TestGroupFlagDeleteResolvesSoleCaseInsensitiveMatch(t *testing.T) {
	for _, s := range groupDeleteSurfaces {
		t.Run(s.name, func(t *testing.T) {
			client := classicGroupsFake(s.groupsPath, s.listRoot, s.itemKey, s.membersKey, s.memberKey, []groupFixture{
				{id: "7", name: "LAB", members: []string{"701"}},
				{id: "9", name: "Other", members: []string{"901"}},
			})
			err := runGroupDelete(t, s, client, "lab")
			got := client.writes()
			want := []string{s.deletePath("701")}
			if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("--group lab with sole match LAB(7): err=%v writes=%v, want %v", err, got, want)
			}
		})
	}
}
