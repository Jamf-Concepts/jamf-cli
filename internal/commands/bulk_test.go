package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamfpro-cli/internal/commands/generated"
)

// ─────────────────────────────────────────────────────────────────
// Test helpers / mock client
// ─────────────────────────────────────────────────────────────────

// bulkMockClient records calls and serves canned responses.
type bulkMockClient struct {
	responses map[string]overviewMockResponse // key: "METHOD /path"
	calls     []string                        // recorded as "METHOD /path"
}

func (m *bulkMockClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	key := method + " " + path
	m.calls = append(m.calls, key)

	// Exact match first
	if resp, ok := m.responses[key]; ok {
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}
	// GET with prefix match (strip query params)
	if method == "GET" {
		if idx := strings.Index(path, "?"); idx != -1 {
			base := "GET " + path[:idx]
			if resp, ok := m.responses[base]; ok {
				return &http.Response{
					StatusCode: resp.statusCode,
					Body:       io.NopCloser(strings.NewReader(resp.body)),
					Header:     make(http.Header),
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("bulk mock: no response for %s", key)
}

func (m *bulkMockClient) hasMutatingCall() bool {
	for _, c := range m.calls {
		if strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "PUT ") ||
			strings.HasPrefix(c, "PATCH ") || strings.HasPrefix(c, "DELETE ") {
			return true
		}
	}
	return false
}

func (m *bulkMockClient) callsMatching(prefix string) []string {
	var out []string
	for _, c := range m.calls {
		if strings.Contains(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// newBulkCLIContext builds a CLIContext backed by the given mock.
func newBulkCLIContext(mock *bulkMockClient) *generated.CLIContext {
	return &generated.CLIContext{Client: mock}
}

// runCobraCmd executes a cobra command with a pre-configured context and
// returns stderr output.  The command's stdout is discarded.
func runCobraCmd(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

// ─────────────────────────────────────────────────────────────────
// policy list JSON helpers
// ─────────────────────────────────────────────────────────────────

const policyListJSON = `{"policies":[
  {"id":1,"name":"Deploy Chrome"},
  {"id":2,"name":"Install Zoom"},
  {"id":3,"name":"Security Baseline"}
]}`

func policyDetailJSON(id int, name string, enabled bool, category, groupName string) string {
	catJSON := fmt.Sprintf(`{"id":10,"name":"%s"}`, category)
	groupJSON := fmt.Sprintf(`{"id":20,"name":"%s"}`, groupName)
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	return fmt.Sprintf(`{"policy":{"id":%d,"name":"%s","enabled":%s,"category":%s,"scope":{"computer_groups":[%s]}}}`,
		id, name, enabledStr, catJSON, groupJSON)
}

// ─────────────────────────────────────────────────────────────────
// Tests: disable-policies
// ─────────────────────────────────────────────────────────────────

func TestDisablePolicies_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	// Run without --yes — should NOT mutate
	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue any mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestEnablePolicies_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, policyListJSON},
			// All disabled → enable-policies should want to enable them
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", false, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", false, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", false, "Security", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "enable-policies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue any mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestDisablePolicies_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
			// Accept any PUT
			"PUT /JSSResource/policies/id/1": {200, `<policy><general><enabled>false</enabled></general></policy>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy><general><enabled>false</enabled></general></policy>`},
			"PUT /JSSResource/policies/id/3": {200, `<policy><general><enabled>false</enabled></general></policy>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) == 0 {
		t.Errorf("expected PUT calls after --yes, got none")
	}
	if strings.Contains(stderr, "[dry-run]") {
		t.Errorf("should not print [dry-run] when --yes is set")
	}
}

func TestDisablePolicies_CategoryFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies":      {200, policyListJSON},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/3": {200, policyDetailJSON(3, "Security Baseline", true, "Security", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--category", "Apps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only policy 1 and 2 are in "Apps" → 2 PUTs expected
	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Apps filter), got %d: %v", len(puts), puts)
	}
	// Policy 3 (Security) should NOT be touched
	for _, c := range puts {
		if strings.Contains(c, "/id/3") {
			t.Error("policy 3 (Security) should not be disabled when filtering by Apps")
		}
	}
}

func TestDisablePolicies_NamePatternFilter(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Deploy Chrome"},
				{"id":2,"name":"Deploy Firefox"},
				{"id":3,"name":"Security Baseline"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Deploy Firefox", true, "Apps", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			"PUT /JSSResource/policies/id/2": {200, `<policy/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "disable-policies", "--yes", "--name-pattern", "Deploy *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/policies/id/")
	if len(puts) != 2 {
		t.Errorf("expected 2 PUTs (Deploy * pattern), got %d: %v", len(puts), puts)
	}
	// Policy 3 must not be touched
	for _, c := range puts {
		if strings.Contains(c, "/id/3") {
			t.Errorf("policy 3 (Security Baseline) should not match Deploy * pattern")
		}
	}
}

func TestDisablePolicies_AlreadyDisabledSkipped(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[{"id":1,"name":"Deploy Chrome"}]}`},
			// Already disabled
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", false, "Apps", "All Computers")},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("already-disabled policy should not generate a PUT")
	}
	if !strings.Contains(stderr, "No policies require changes") {
		t.Errorf("expected 'No policies require changes' message, got: %q", stderr)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: add-to-group / remove-from-group
// ─────────────────────────────────────────────────────────────────

const computerGroupsListJSON = `{"computer_groups":[
  {"id":100,"name":"Lab Macs"},
  {"id":101,"name":"DevTeam"}
]}`

const staticGroupDetailJSON = `{"computer_group":{"id":100,"name":"Lab Macs","is_smart":false,"computers":{"computer":[
  {"id":1,"name":"Mac-01"},
  {"id":2,"name":"Mac-02"}
]}}}`

const targetStaticGroupJSON = `{"computer_group":{"id":200,"name":"Quarantine","is_smart":false,"computers":{"computer":[]}}}`

func TestAddToGroup_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			// lookup source group members
			"GET /JSSResource/computergroups":        {200, computerGroupsListJSON},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			// fetch computer names
			"GET /JSSResource/computers/id/1": {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2": {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			// lookup target static group
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
		},
	}
	// Add "Quarantine" to the group list so lookupStaticGroupID can find it
	mock.responses["GET /JSSResource/computergroups"] = overviewMockResponse{
		200,
		`{"computer_groups":[{"id":100,"name":"Lab Macs"},{"id":200,"name":"Quarantine"}]}`,
	}

	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue mutating calls; got: %v", mock.callsMatching("PUT"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestAddToGroup_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"Quarantine"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/computergroups/id/200")
	if len(puts) == 0 {
		t.Error("expected PUT calls after --yes, got none")
	}
}

func TestRemoveFromGroup_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"Quarantine"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "remove-from-group",
		"--target-group", "Quarantine",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	puts := mock.callsMatching("PUT /JSSResource/computergroups/id/200")
	if len(puts) == 0 {
		t.Error("expected PUT calls after --yes, got none")
	}
}

func TestAddToGroup_SmartGroupRejected(t *testing.T) {
	smartGroupJSON := `{"computer_group":{"id":200,"name":"SmartTarget","is_smart":true,"computers":{"computer":[]}}}`
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"},
				{"id":200,"name":"SmartTarget"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computergroups/id/200": {200, smartGroupJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "add-to-group",
		"--target-group", "SmartTarget",
		"--group", "Lab Macs",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error when targeting a smart group, got nil")
	}
	if !strings.Contains(err.Error(), "smart group") {
		t.Errorf("expected 'smart group' in error, got: %v", err)
	}
	if mock.hasMutatingCall() {
		t.Error("should not make mutating calls when the target is a smart group")
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: from-file parsing
// ─────────────────────────────────────────────────────────────────

func TestAddToGroup_FromFile(t *testing.T) {
	// Write a temp file
	dir := t.TempDir()
	filePath := filepath.Join(dir, "computers.txt")
	content := "Mac-01\nMac-02\n# comment\n\nMac-03\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":200,"name":"Quarantine"}
			]}`},
			"GET /JSSResource/computergroups/id/200": {200, targetStaticGroupJSON},
			"PUT /JSSResource/computergroups/id/200": {200, `<computer_group/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 IDs in file (blank line and comment are skipped) → 3 PUTs
	puts := mock.callsMatching("PUT /JSSResource/computergroups/id/200")
	if len(puts) != 3 {
		t.Errorf("expected 3 PUTs (3 IDs from file), got %d", len(puts))
	}
}

func TestFromFileParsing_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "ids.txt")
	content := "# this is a comment\nABC-001\n\n   DEF-002   \n# another comment\nGHI-003\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	ids, err := readIDsFromFile(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d: %v", len(ids), ids)
	}
	if ids[0] != "ABC-001" || ids[1] != "DEF-002" || ids[2] != "GHI-003" {
		t.Errorf("unexpected ids: %v", ids)
	}
}

func TestFromFileMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "ids.txt")
	_ = os.WriteFile(filePath, []byte("mac-01\n"), 0o600)

	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "add-to-group",
		"--target-group", "Quarantine",
		"--from-file", filePath,
		"--group", "Lab Macs",
	)
	if err == nil {
		t.Fatal("expected error when both --from-file and --group are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: send-command
// ─────────────────────────────────────────────────────────────────

func TestSendCommand_DryRunDefault(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.hasMutatingCall() {
		t.Errorf("dry-run should not issue mutating calls; got: %v", mock.callsMatching("POST"))
	}
	if !strings.Contains(stderr, "[dry-run]") {
		t.Errorf("expected [dry-run] in stderr, got: %q", stderr)
	}
}

func TestSendCommand_YesDispatches(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100":                    {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":                           {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":                           {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			"POST /JSSResource/computercommands/command/BlankPush/id/1": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/2": {200, `<computer_command/>`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	posts := mock.callsMatching("POST /JSSResource/computercommands/command/BlankPush")
	if len(posts) == 0 {
		t.Error("expected POST calls after --yes, got none")
	}
}

func TestSendCommand_DestructiveRequiresConfirm(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	for _, cmd2 := range []string{"EraseDevice", "DeviceLock"} {
		t.Run(cmd2, func(t *testing.T) {
			cmd := newBulkCmd(cliCtx)
			_, _, err := runCobraCmd(t, cmd, "send-command",
				"--command", cmd2,
				"--from-file", "/dev/null",
				"--yes",
				// --confirm-destructive is NOT set
			)
			if err == nil {
				t.Fatalf("expected error for destructive command %q without --confirm-destructive", cmd2)
			}
			if !strings.Contains(err.Error(), "confirm-destructive") {
				t.Errorf("expected 'confirm-destructive' in error, got: %v", err)
			}
			if mock.hasMutatingCall() {
				t.Error("should not issue any calls when the destructive gate is not cleared")
			}
		})
	}
}

func TestSendCommand_DestructiveWithBothFlags(t *testing.T) {
	// /dev/null → 0 targets, so no actual POST, but the gate should be cleared
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "send-command",
		"--command", "EraseDevice",
		"--from-file", "/dev/null",
		"--yes",
		"--confirm-destructive",
	)
	// /dev/null gives 0 IDs → "No target computers found." but no error
	if err != nil {
		t.Fatalf("unexpected error with both flags set: %v", err)
	}
}

func TestSendCommand_DestructiveRequiresYesToo(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "send-command",
		"--command", "EraseDevice",
		"--from-file", "/dev/null",
		// --yes is NOT set, --confirm-destructive IS set
		"--confirm-destructive",
	)
	if err == nil {
		t.Fatal("expected error for EraseDevice without --yes")
	}
	if !strings.Contains(err.Error(), "destructive") {
		t.Errorf("expected 'destructive' in error, got: %v", err)
	}
}

func TestSendCommand_InvalidCommandName(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, _, err := runCobraCmd(t, cmd, "send-command",
		"--command", "SelfDestruct",
		"--from-file", "/dev/null",
		"--yes",
	)
	if err == nil {
		t.Fatal("expected error for unknown command name")
	}
	if !strings.Contains(err.Error(), "SelfDestruct") {
		t.Errorf("expected command name in error, got: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: partial failure
// ─────────────────────────────────────────────────────────────────

func TestDisablePolicies_PartialFailure(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/policies": {200, `{"policies":[
				{"id":1,"name":"Deploy Chrome"},
				{"id":2,"name":"Install Zoom"}
			]}`},
			"GET /JSSResource/policies/id/1": {200, policyDetailJSON(1, "Deploy Chrome", true, "Apps", "All Computers")},
			"GET /JSSResource/policies/id/2": {200, policyDetailJSON(2, "Install Zoom", true, "Apps", "All Computers")},
			"PUT /JSSResource/policies/id/1": {200, `<policy/>`},
			// policy 2 PUT fails
			"PUT /JSSResource/policies/id/2": {500, `Internal Server Error`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "disable-policies", "--yes")
	// Partial failure: command should succeed overall (not return an error)
	if err != nil {
		t.Fatalf("unexpected error on partial failure: %v", err)
	}

	// Stderr should mention both "ok" (policy 1) and "ERROR" (policy 2)
	if !strings.Contains(stderr, "ok") {
		t.Error("expected 'ok' in stderr for successful policy")
	}
	if !strings.Contains(stderr, "ERROR") {
		t.Error("expected 'ERROR' in stderr for failed policy")
	}
}

func TestSendCommand_PartialFailure(t *testing.T) {
	mock := &bulkMockClient{
		responses: map[string]overviewMockResponse{
			"GET /JSSResource/computergroups": {200, `{"computer_groups":[
				{"id":100,"name":"Lab Macs"}
			]}`},
			"GET /JSSResource/computergroups/id/100": {200, staticGroupDetailJSON},
			"GET /JSSResource/computers/id/1":        {200, `{"computer":{"id":1,"name":"Mac-01"}}`},
			"GET /JSSResource/computers/id/2":        {200, `{"computer":{"id":2,"name":"Mac-02"}}`},
			// Mac-01 succeeds, Mac-02 fails
			"POST /JSSResource/computercommands/command/BlankPush/id/1": {200, `<computer_command/>`},
			"POST /JSSResource/computercommands/command/BlankPush/id/2": {500, `Internal Server Error`},
		},
	}
	cliCtx := newBulkCLIContext(mock)

	cmd := newBulkCmd(cliCtx)
	_, stderr, err := runCobraCmd(t, cmd, "send-command",
		"--command", "BlankPush",
		"--group", "Lab Macs",
		"--yes",
	)
	if err != nil {
		t.Fatalf("unexpected error on partial failure: %v", err)
	}

	if !strings.Contains(stderr, "ok") {
		t.Error("expected 'ok' in stderr for successful computer")
	}
	if !strings.Contains(stderr, "ERROR") {
		t.Error("expected 'ERROR' in stderr for failed computer")
	}
}

// ─────────────────────────────────────────────────────────────────
// Tests: helper unit tests
// ─────────────────────────────────────────────────────────────────

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    bool
	}{
		{"Deploy *", "Deploy Chrome", true},
		{"Deploy *", "deploy chrome", true}, // case-insensitive
		{"Deploy *", "Install Chrome", false},
		{"*Security*", "Enforce Security Policy", true},
		{"*Security*", "Install Chrome", false},
		{"Exact", "Exact", true},
		{"Exact", "ExactMatch", false},
		{"*", "anything", true},
	}
	for _, tt := range tests {
		got, err := matchGlob(tt.pattern, tt.input)
		if err != nil {
			t.Errorf("matchGlob(%q, %q) error: %v", tt.pattern, tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"enable", "Enable"},
		{"disable", "Disable"},
		{"", ""},
		{"a", "A"},
	}
	for _, tt := range tests {
		if got := capitalize(tt.in); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"banana": true, "apple": true, "cherry": true}
	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "apple" || keys[1] != "banana" || keys[2] != "cherry" {
		t.Errorf("unexpected order: %v", keys)
	}
}

func TestStaticGroupXMLBodies(t *testing.T) {
	addXML := staticGroupAddComputerXML("42")
	if !strings.Contains(addXML, "<computer_additions>") {
		t.Error("add XML missing computer_additions")
	}
	if !strings.Contains(addXML, "<id>42</id>") {
		t.Error("add XML missing computer id")
	}

	removeXML := staticGroupRemoveComputerXML("42")
	if !strings.Contains(removeXML, "<computer_deletions>") {
		t.Error("remove XML missing computer_deletions")
	}
	if !strings.Contains(removeXML, "<id>42</id>") {
		t.Error("remove XML missing computer id")
	}
}

func TestBulkCmd_HasExpectedSubcommands(t *testing.T) {
	mock := &bulkMockClient{responses: map[string]overviewMockResponse{}}
	cliCtx := newBulkCLIContext(mock)
	cmd := newBulkCmd(cliCtx)

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}

	expected := []string{"enable-policies", "disable-policies", "add-to-group", "remove-from-group", "send-command"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("bulk command missing subcommand %q", name)
		}
	}
}
