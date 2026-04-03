// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// newPolicyExecuteCmd builds the "policy-execute" command that triggers a Jamf
// Pro policy on specific devices.
func newPolicyExecuteCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		targets []string
		group   string
		yes     bool
	)

	cmd := &cobra.Command{
		Use:   "policy-execute <policy-name-or-id>",
		Short: "Trigger a Jamf Pro policy on specific devices",
		Long: `Execute a Jamf Pro policy on one or more target devices.

The policy can be specified by its Jamf ID or display name (case-insensitive).
Targets are specified with --target (repeatable, device name/serial/ID) or
--group (computer group name). These flags are mutually exclusive.

Default behavior is a dry-run preview — no commands are sent unless --yes is
provided.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicyExecute(cmd.Context(), cliCtx.Client, args[0], targets, group, yes)
		},
	}

	cmd.Flags().StringSliceVar(&targets, "target", nil, "device name, serial number, or Jamf ID (repeatable)")
	cmd.Flags().StringVar(&group, "group", "", "computer group name (mutually exclusive with --target)")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute the policy (default: dry-run preview only)")

	return cmd
}

// runPolicyExecute validates inputs, resolves the policy and targets, then
// either prints a dry-run preview or executes the policy on each target.
func runPolicyExecute(
	ctx context.Context,
	client registry.HTTPClient,
	policyRef string,
	targets []string,
	group string,
	yes bool,
) error {
	// 1. Validate: at least one targeting method required, mutually exclusive.
	if len(targets) == 0 && group == "" {
		return fmt.Errorf("at least one --target or --group is required")
	}
	if len(targets) > 0 && group != "" {
		return fmt.Errorf("--target and --group are mutually exclusive")
	}

	// 2. Resolve policy.
	policyID, policyName, err := resolvePolicyByNameOrID(ctx, client, policyRef)
	if err != nil {
		return fmt.Errorf("resolving policy %q: %w", policyRef, err)
	}

	// 3. Resolve target device IDs.
	type target struct {
		id   string
		name string
	}
	var resolved []target

	if group != "" {
		ids, err := fetchComputerGroupMemberIDs(ctx, client, group)
		if err != nil {
			return fmt.Errorf("resolving group %q: %w", group, err)
		}
		if len(ids) == 0 {
			return fmt.Errorf("group %q has no members", group)
		}
		for _, id := range ids {
			resolved = append(resolved, target{id: id, name: "id=" + id})
		}
	} else {
		for _, t := range targets {
			id, name, err := resolveDeviceByIdentifier(ctx, client, t)
			if err != nil {
				return fmt.Errorf("resolving target %q: %w", t, err)
			}
			resolved = append(resolved, target{id: id, name: name})
		}
	}

	// 4. Dry-run: preview and return.
	if !yes {
		_, _ = fmt.Fprintf(os.Stderr, "[dry-run] Would execute policy %q (id=%s) on %d device(s):\n",
			policyName, policyID, len(resolved))
		for _, t := range resolved {
			_, _ = fmt.Fprintf(os.Stderr, "  - %s (id=%s)\n", t.name, t.id)
		}
		_, _ = fmt.Fprintf(os.Stderr, "Use --yes to execute.\n")
		return nil
	}

	// 5. Execute: POST to each target, track results.
	_, _ = fmt.Fprintf(os.Stderr, "Executing policy %q (id=%s) on %d device(s)...\n",
		policyName, policyID, len(resolved))

	var failed int
	for _, t := range resolved {
		path := fmt.Sprintf(
			"/JSSResource/computercommands/command/PolicyExecution/action/command/id/%s/policyid/%s",
			t.id, policyID,
		)
		if err := postCommand(ctx, client, path); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "  FAIL %s (id=%s): %v\n", t.name, t.id, err)
			failed++
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "  OK   %s (id=%s)\n", t.name, t.id)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d device(s) failed", failed, len(resolved))
	}
	_, _ = fmt.Fprintf(os.Stderr, "Successfully executed policy on all %d device(s).\n", len(resolved))
	return nil
}

// resolvePolicyByNameOrID resolves a policy reference to its numeric ID and
// display name.
//
// Resolution order:
//  1. Try as Jamf ID via Classic API detail endpoint.
//  2. If that fails, list all policies and do a case-insensitive name match.
func resolvePolicyByNameOrID(ctx context.Context, client registry.HTTPClient, ref string) (string, string, error) {
	// 1. Try as ID using the existing Classic detail helper.
	detail, err := fetchClassicPolicyDetail(ctx, client, ref)
	if err == nil {
		general, _ := detail["general"].(map[string]any)
		if general != nil {
			id := extractID(general)
			name, _ := general["name"].(string)
			if id != "" {
				return id, name, nil
			}
		}
		// Fallback: top-level fields (some Classic responses have flat structure).
		id := extractID(detail)
		name, _ := detail["name"].(string)
		if id != "" {
			return id, name, nil
		}
	}

	// 2. Try as name via list.
	raw, err := FetchClassicList(ctx, client, "/JSSResource/policies", "policies")
	if err != nil {
		return "", "", fmt.Errorf("listing policies: %w", err)
	}

	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.EqualFold(name, ref) {
			id := extractID(m)
			return id, name, nil
		}
	}

	return "", "", fmt.Errorf("policy %q not found", ref)
}

// postCommand sends a POST with nil body and checks for a 2xx response.
func postCommand(ctx context.Context, client registry.HTTPClient, path string) error {
	resp, err := client.Do(ctx, "POST", path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
