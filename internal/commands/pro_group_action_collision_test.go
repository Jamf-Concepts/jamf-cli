// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// collidingGroupsServer is a Jamf Pro whose computer and mobile device groups
// each carry two groups named "Decom": static computer groups 21 and 22, and
// smart mobile device groups 31 and 32. Static computer group 23, "Lab", is
// unique and holds computer 42. The Classic /name/ endpoints answer
// with one of each pair, as the server does. Every non-GET request is recorded.
type collidingGroupsServer struct {
	mu        sync.Mutex
	mutations []string
}

func (s *collidingGroupsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.mu.Lock()
		s.mutations = append(s.mutations, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		return
	}
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/computer-groups/smart-groups"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalCount":0,"results":[]}`))
	case strings.HasSuffix(p, "/mobile-device-groups/smart-groups"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalCount":2,"results":[{"groupId":"31","groupName":"Decom"},{"groupId":"32","groupName":"Decom"}]}`))
	case p == "/JSSResource/computergroups":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<computer_groups><size>2</size>` +
			`<computer_group><id>21</id><name>Decom</name><is_smart>false</is_smart></computer_group>` +
			`<computer_group><id>22</id><name>Decom</name><is_smart>false</is_smart></computer_group>` +
			`<computer_group><id>23</id><name>Lab</name><is_smart>false</is_smart></computer_group></computer_groups>`))
	case p == "/JSSResource/mobiledevicegroups":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<mobile_device_groups><size>2</size>` +
			`<mobile_device_group><id>31</id><name>Decom</name><is_smart>true</is_smart></mobile_device_group>` +
			`<mobile_device_group><id>32</id><name>Decom</name><is_smart>true</is_smart></mobile_device_group></mobile_device_groups>`))
	case p == "/JSSResource/computergroups/name/Decom":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<computer_group><id>21</id><name>Decom</name></computer_group>`))
	case p == "/JSSResource/mobiledevicegroups/name/Decom":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<mobile_device_group><id>31</id><name>Decom</name></mobile_device_group>`))
	case strings.HasPrefix(p, "/JSSResource/computergroups/id/"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<computer_group><id>21</id><name>Decom</name><computers><size>1</size>` +
			`<computer><id>42</id></computer></computers></computer_group>`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *collidingGroupsServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.mutations...)
}

// The shipped erase commands are the hand-written ones pro.go wires over the
// generated pair, so every --group action is driven through the assembled root.
func TestProGroupAction_DuplicateGroupNameIsRefusedThroughTheRoot(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantIDs []string
	}{
		{"computer erase", []string{"pro", "computer-inventory", "erase", "--group", "Decom", "--yes", "--confirm-destructive"}, []string{"21", "22"}},
		{"mobile erase", []string{"pro", "mobile-devices", "erase", "--group", "Decom", "--yes", "--confirm-destructive"}, []string{"31", "32"}},
		{"computer flush-commands", []string{"pro", "computers", "flush-commands", "--group", "Decom", "--yes"}, []string{"21", "22"}},
		{"mobile flush-commands", []string{"pro", "mobile-devices", "flush-commands", "--group", "Decom", "--yes"}, []string{"31", "32"}},
		{"bulk send-command", []string{"pro", "bulk", "send-command", "--command", "EraseDevice", "--group", "Decom", "--yes", "--confirm-destructive"}, []string{"21", "22"}},
		{"bulk add-to-group target", []string{"pro", "bulk", "add-to-group", "--target-group", "Decom", "--group", "Lab", "--yes"}, []string{"21", "22"}},
		{"bulk remove-from-group source", []string{"pro", "bulk", "remove-from-group", "--target-group", "Lab", "--group", "Decom", "--yes"}, []string{"21", "22"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := runAgainstCollidingGroups(t, tc.args)

			if sent := fake.sent(); len(sent) != 0 {
				t.Errorf("%s sent %v; two groups share the name, so nothing may be sent", strings.Join(tc.args, " "), sent)
			}
			if err == nil {
				t.Fatalf("%s succeeded; want a refusal naming both groups", strings.Join(tc.args, " "))
			}
			for _, id := range tc.wantIDs {
				if !strings.Contains(err.Error(), id) {
					t.Errorf("refusal %q does not name group id %s", err, id)
				}
			}
		})
	}
}

func runAgainstCollidingGroups(t *testing.T, args []string) (*collidingGroupsServer, error) {
	t.Helper()
	fake := &collidingGroupsServer{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("JAMF_URL", srv.URL)
	t.Setenv("JAMF_TOKEN", "test-token")
	for _, k := range []string{
		"JAMF_CLIENT_ID", "JAMF_CLIENT_SECRET", "JAMF_PROFILE",
		"JAMF_TENANT_ID", "JAMF_ENVIRONMENT_ID", "JAMF_CLI_ARGS",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("JAMF_CLI_NO_UPDATE_CHECK", "1")

	root := NewRootCmd("test", "none", "none", "none")
	root.SetArgs(append([]string{"--no-input"}, args...))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	return fake, root.Execute()
}

// The positive control: a unique group name sends exactly one write, to that
// group or its one member.
func TestProGroupAction_UniqueGroupNameSendsOneWriteThroughTheRoot(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"computer flush-commands", []string{"pro", "computers", "flush-commands", "--group", "Lab", "--yes"}, "DELETE /JSSResource/commandflush/computergroups/id/23/status/Failed"},
		{"bulk send-command", []string{"pro", "bulk", "send-command", "--command", "BlankPush", "--group", "Lab", "--yes"}, "POST /JSSResource/computercommands/command/BlankPush/id/42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, err := runAgainstCollidingGroups(t, tc.args)
			if err != nil {
				t.Fatalf("%s: %v", strings.Join(tc.args, " "), err)
			}
			if sent := fake.sent(); len(sent) != 1 || sent[0] != tc.want {
				t.Errorf("%s sent %v; want exactly [%s]", strings.Join(tc.args, " "), sent, tc.want)
			}
		})
	}
}
