// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// The modern static-group writes refuse bodies their spec calls valid, and
// they refuse them with a bare 500 (empty errors) or a 400 that names no
// field, so a generated command sending exactly what its --help documents
// failed with nothing to act on. Wire-checked 2026-10-04 on Jamf Pro 11.32:
//
//   - POST /v3/computer-groups/static-groups without `assignments`: 500.
//   - PUT /v3/computer-groups/static-groups/{id} without `assignments`: 500.
//     Its GET returns no members, so the generated fetch-merge `update --set`
//     never carried them and always failed. With `assignments` the PUT
//     *replaces* the member list.
//   - POST /v2/mobile-device-groups/static-groups without `assignments`: 500;
//     without `siteId`: 403 INVALID_PRIVILEGE.
//   - PATCH /v2/mobile-device-groups/static-groups/{id} without `groupName`,
//     `siteId` or `assignments`: 500, or 400 "Cannot parse null string". The
//     PATCH is incremental, so `assignments: []` leaves the members alone.
//
// The spec marks only the name required on each. Rather than fork four
// generated commands, the requests they send are completed on the way out,
// and only with a field the caller left out: an explicit value always wins.
//
// The fault is the Jamf Pro spec and server's, and these commands are
// generated from the jamf-pro-server spec, so this goes when that spec
// declares the fields required (or the server stops needing them). The same
// gap in the SDK's typed methods is jamf/jamfplatform-go-sdk#86.

// staticGroupBodyRule completes one request shape.
type staticGroupBodyRule struct {
	method string
	path   *regexp.Regexp // submatch 1, when present, is the group ID
	fill   func(ctx context.Context, inner registry.HTTPClient, id string, body map[string]any) error
}

var (
	v3StaticComputerGroups    = regexp.MustCompile(`^/v3/computer-groups/static-groups$`)
	v3StaticComputerGroupItem = regexp.MustCompile(`^/v3/computer-groups/static-groups/([0-9]+)$`)
	v2StaticMobileGroups      = regexp.MustCompile(`^/v2/mobile-device-groups/static-groups$`)
	v2StaticMobileGroupItem   = regexp.MustCompile(`^/v2/mobile-device-groups/static-groups/([0-9]+)$`)
)

var staticGroupBodyRules = []staticGroupBodyRule{
	{http.MethodPost, v3StaticComputerGroups, func(_ context.Context, _ registry.HTTPClient, _ string, b map[string]any) error {
		setIfAbsent(b, "assignments", []string{})
		return nil
	}},
	{http.MethodPut, v3StaticComputerGroupItem, func(ctx context.Context, inner registry.HTTPClient, id string, b map[string]any) error {
		if _, ok := b["assignments"]; ok {
			return nil
		}
		// The PUT replaces the member list, so leaving it out has to mean
		// "keep the members", which takes reading them. The v3 GET does not
		// return them; the Classic group does.
		members, err := classicStaticComputerGroupMemberIDs(ctx, inner, id)
		if err != nil {
			return fmt.Errorf("reading the members of static computer group %s, which the update must carry to keep them: %w", id, err)
		}
		b["assignments"] = members
		return nil
	}},
	{http.MethodPost, v2StaticMobileGroups, func(_ context.Context, _ registry.HTTPClient, _ string, b map[string]any) error {
		setIfAbsent(b, "assignments", []string{})
		setIfAbsent(b, "siteId", "-1")
		return nil
	}},
	{http.MethodPatch, v2StaticMobileGroupItem, func(ctx context.Context, inner registry.HTTPClient, id string, b map[string]any) error {
		setIfAbsent(b, "assignments", []any{})
		_, hasName := b["groupName"]
		_, hasSite := b["siteId"]
		if hasName && hasSite {
			return nil
		}
		current, err := fetchJSON(ctx, inner, fmt.Sprintf("/v2/mobile-device-groups/static-groups/%s", url.PathEscape(id)))
		if err != nil {
			return fmt.Errorf("reading static mobile device group %s for the groupName and siteId every PATCH must carry: %w", id, err)
		}
		setIfAbsent(b, "groupName", current["groupName"])
		setIfAbsent(b, "siteId", current["siteId"])
		return nil
	}},
}

func setIfAbsent(m map[string]any, key string, v any) {
	if _, ok := m[key]; !ok {
		m[key] = v
	}
}

// classicStaticComputerGroupMemberIDs reads a static computer group's member
// IDs from the Classic API, as strings for the modern `assignments` array.
func classicStaticComputerGroupMemberIDs(ctx context.Context, client registry.HTTPClient, id string) ([]string, error) {
	data, err := fetchJSON(ctx, client, fmt.Sprintf("/JSSResource/computergroups/id/%s", url.PathEscape(id)))
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, m := range extractScopeItems(unwrapClassicDetail(data), "computers", "computer") {
		if mid := extractID(m); mid != "" {
			ids = append(ids, mid)
		}
	}
	return ids, nil
}

// staticGroupBodyClient completes the requests staticGroupBodyRules match and
// passes every other request through untouched.
type staticGroupBodyClient struct {
	inner registry.HTTPClient
}

func (c *staticGroupBodyClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	bare, _, _ := strings.Cut(path, "?")
	for _, rule := range staticGroupBodyRules {
		if !strings.EqualFold(rule.method, method) || body == nil {
			continue
		}
		m := rule.path.FindStringSubmatch(bare)
		if m == nil {
			continue
		}
		raw, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			// Not a JSON object: the server's own error is the better answer.
			return c.inner.Do(ctx, method, path, bytes.NewReader(raw))
		}
		id := ""
		if len(m) > 1 {
			id = m[1]
		}
		if err := rule.fill(ctx, c.inner, id, obj); err != nil {
			return nil, err
		}
		completed, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		return c.inner.Do(ctx, method, path, bytes.NewReader(completed))
	}
	return c.inner.Do(ctx, method, path, body)
}

// completeStaticGroupBodies routes the named generated leaves of one resource
// through staticGroupBodyClient for the duration of their RunE, and says so in
// their help. A leaf that is not there is recorded as stale wiring, as every
// other name-keyed wiring step is.
func completeStaticGroupBodies(root *cobra.Command, cliCtx *registry.CLIContext, resource string, note string, leaves ...string) {
	parent := findWiringParent(root, "wrap", []string{resource}, "")
	if parent == nil {
		return
	}
	for _, name := range leaves {
		var leaf *cobra.Command
		for _, c := range parent.Commands() {
			if c.Name() == name {
				leaf = c
				break
			}
		}
		if leaf == nil || leaf.RunE == nil {
			recordStaleProWiring("wrap", []string{resource}, name)
			continue
		}
		run := leaf.RunE
		leaf.RunE = func(cmd *cobra.Command, args []string) error {
			inner := cliCtx.Client
			cliCtx.Client = &staticGroupBodyClient{inner: inner}
			defer func() { cliCtx.Client = inner }()
			return run(cmd, args)
		}
		leaf.Long = strings.TrimRight(leaf.Long, "\n") + "\n\n" + note
	}
}

const (
	staticComputerGroupBodyNote = `The server refuses a body without "assignments" (HTTP 500), though the
spec does not mark it required. When it is left out, create sends an empty
member list and update sends the group's current members, so an update that
names no members keeps them. An explicit "assignments" replaces the member
list; to add or remove members, use classic-computer-groups add-members or
remove-members.`

	staticMobileGroupBodyNote = `The server refuses a body without "groupName", "siteId" and "assignments",
though the spec marks only "groupName" required. Whichever are left out are
filled in: on create an empty member list and site -1 (none); on patch the
group's current name and site, and an empty assignment list, which changes no
members.`
)
