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
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/resolve"
)

// isFailedTaskStatus reports whether a deployment task's status string
// represents a failed install: status == GAVE_UP, confirmed against a live
// tenant's real task data. The server-side RSQL "status" filter on this
// endpoint 500s regardless of the value or quoting used (filtering on other
// fields like "updated" works fine), so this match happens entirely
// client-side after an unfiltered fetch.
//
// Do not confuse this with the filter param's own (unrelated, and separately
// broken) validation enum of Installed/Pending/Failed/Acknowledged — that's
// what `filter=status==X` validates X against before 500ing, not what the
// task's actual "status" field contains. Real observed status values:
// VERIFIED_INSTALL (success), GAVE_UP (failed), INSTALL_IN_PROGRESS
// (pending), COMPLETE.
func isFailedTaskStatus(status string) bool {
	return strings.EqualFold(status, "GAVE_UP")
}

// newJamfProtectDeploymentRetryFailedCmd creates the `jamf-protect-deployment-tasks
// retry-failed` subcommand. It is hand-written business logic wired onto the
// generated "jamf-protect-deployment-tasks" command group (see pro.go):
// the retry API takes deployment TASK ids, not computer ids, so retrying by
// serial/management-id/all-failed requires resolving the computer and/or
// listing+filtering tasks before calling the generated retry endpoint's
// underlying POST.
func newJamfProtectDeploymentRetryFailedCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var (
		serial           string
		managementID     string
		udid             string
		allFailed        bool
		taskIDs          []string
		includeSucceeded bool
		yes              bool
	)

	cmd := &cobra.Command{
		Use:   "retry-failed <deployment-id>",
		Short: "Retry failed Jamf Protect install tasks for a deployment",
		Long: `Retry failed Jamf Protect deployment install tasks (status GAVE_UP).
Status matching happens client-side, not via the server's "status" filter,
which 500s on this endpoint regardless of value.

<deployment-id> is the deployment UUID for a Jamf Protect plan, found via
"jamf-cli pro jamf-protect-plans list" (the "uuid" field) — not the config
profile's numeric ID.

Exactly one targeting mode is required:
  --serial            retry the failed task for one computer, by serial number
  --management-id      retry the failed task for one computer, by management ID (UUID)
  --udid               retry the failed task for one computer, by UDID
  --all-failed         retry every failed task in the deployment
  --task-ids           retry specific deployment task IDs directly (advanced; skips lookup)

--include-succeeded drops the default failed-only filter when resolving
--serial/--management-id/--udid, matching that computer's task regardless of status.

--all-failed can re-queue installs across many devices — it requires --yes.`,
		Example: `  # Retry the failed install task for one computer, by serial number
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --serial C02X1234ABCD

  # Retry by management ID (UUID)
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --management-id 6f2c1e3a-...

  # Retry by UDID
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --udid 00008030-001A2D...

  # Retry every failed task in the deployment
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --all-failed --yes

  # Retry specific deployment task IDs directly (advanced)
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --task-ids 82,83

  # Preview without executing
  jamf-cli pro jamf-protect-deployment-tasks retry-failed 24a7bb2a-9871-4895-9009-d1be07ed31b1 --all-failed --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]
			stderr := cmd.ErrOrStderr()

			modesSet := 0
			for _, set := range []bool{serial != "", managementID != "", udid != "", allFailed, len(taskIDs) > 0} {
				if set {
					modesSet++
				}
			}
			if modesSet == 0 {
				return fmt.Errorf("one of --serial, --management-id, --udid, --all-failed, or --task-ids is required")
			}
			if includeSucceeded && serial == "" && managementID == "" && udid == "" {
				return fmt.Errorf("--include-succeeded requires --serial, --management-id, or --udid")
			}

			ctx := cmd.Context()

			var (
				ids        []string
				targetDesc string
				requireYes bool
			)

			switch {
			case len(taskIDs) > 0:
				ids = taskIDs
				targetDesc = fmt.Sprintf("%d task ID(s) given directly", len(ids))

			case allFailed:
				requireYes = true
				tasks, err := fetchDeploymentTasks(ctx, cliCtx.Client, deploymentID)
				if err != nil {
					return err
				}
				for _, t := range tasks {
					if isFailedTaskStatus(extractField(t, "status")) {
						ids = append(ids, extractField(t, "id"))
					}
				}
				if len(ids) == 0 {
					_, _ = fmt.Fprintln(stderr, "Nothing to retry: no failed tasks found for this deployment.")
					return nil
				}
				targetDesc = fmt.Sprintf("all %d failed task(s) in the deployment", len(ids))

			default: // --serial, --management-id, or --udid
				d, err := resolveDeploymentTargetComputer(ctx, cliCtx.Client, serial, managementID, udid)
				if err != nil {
					return err
				}

				tasks, err := fetchDeploymentTasks(ctx, cliCtx.Client, deploymentID)
				if err != nil {
					return err
				}

				for _, t := range tasks {
					if extractField(t, "computerId") != d.ID {
						continue
					}
					if includeSucceeded || isFailedTaskStatus(extractField(t, "status")) {
						ids = append(ids, extractField(t, "id"))
					}
				}
				if len(ids) == 0 {
					hint := ", or retry with --include-succeeded to see its current status"
					if includeSucceeded {
						hint = ""
					}
					return fmt.Errorf("no deployment task found for computer %s in deployment %s — check the deployment UUID, confirm the computer is scoped to this plan%s", resolve.FormatDeviceDesc(d), deploymentID, hint)
				}
				targetDesc = fmt.Sprintf("computer %s (%d task(s))", resolve.FormatDeviceDesc(d), len(ids))
			}

			if dryRun {
				_, _ = fmt.Fprintf(stderr, "[dry-run] Would retry %s: task IDs %s\n", targetDesc, strings.Join(ids, ", "))
				return nil
			}

			if requireYes && !yes {
				_, _ = fmt.Fprintf(stderr, "This will retry %s. Use --yes to execute.\n", targetDesc)
				return nil
			}

			return retryDeploymentTasks(ctx, cliCtx, stderr, deploymentID, ids, targetDesc)
		},
	}

	cmd.Flags().StringVar(&serial, "serial", "", "target computer by serial number")
	cmd.Flags().StringVar(&managementID, "management-id", "", "target computer by management ID (UUID)")
	cmd.Flags().StringVar(&udid, "udid", "", "target computer by UDID")
	cmd.Flags().BoolVar(&allFailed, "all-failed", false, "retry every failed task in the deployment")
	cmd.Flags().StringSliceVar(&taskIDs, "task-ids", nil, "retry specific deployment task IDs directly (advanced)")
	cmd.Flags().BoolVar(&includeSucceeded, "include-succeeded", false, "match the target computer's task regardless of status (use with --serial/--management-id/--udid)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt (required for --all-failed)")
	cmd.MarkFlagsMutuallyExclusive("serial", "management-id", "udid", "all-failed", "task-ids")

	return cmd
}

// resolveDeploymentTargetComputer resolves exactly one of serial/managementID/udid
// to a computer's full identifiers.
func resolveDeploymentTargetComputer(ctx context.Context, client registry.HTTPClient, serial, managementID, udid string) (*resolve.DeviceIdentifiers, error) {
	switch {
	case serial != "":
		return resolve.ResolveComputer(ctx, client, serial, "", "")
	case managementID != "":
		return resolve.ResolveComputerByManagementID(ctx, client, managementID)
	default:
		return resolve.ResolveComputerByUDID(ctx, client, udid)
	}
}

// fetchDeploymentTasks pages through every task for a deployment, unfiltered.
// Filtering happens entirely client-side (see isFailedTaskStatus): the
// server-side RSQL filter cannot express computerId at all, and its "status"
// filter 500s regardless of value, so there is no filter this call can
// usefully send.
func fetchDeploymentTasks(ctx context.Context, client registry.HTTPClient, deploymentID string) ([]map[string]any, error) {
	const pageSize = 100
	var all []map[string]any

	for page := 0; ; page++ {
		path := fmt.Sprintf("/v1/jamf-protect/deployments/%s/tasks?page=%d&page-size=%d",
			url.PathEscape(deploymentID), page, pageSize)

		resp, err := client.Do(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetching deployment tasks: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var pageResp struct {
			TotalCount int              `json:"totalCount"`
			Results    []map[string]any `json:"results"`
		}
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return nil, fmt.Errorf("parsing deployment tasks response: %w", err)
		}

		all = append(all, pageResp.Results...)
		if len(pageResp.Results) < pageSize || len(all) >= pageResp.TotalCount {
			break
		}
	}

	return all, nil
}

// retryDeploymentTasks POSTs the given deployment task IDs to the retry
// endpoint and prints a summary on success.
func retryDeploymentTasks(ctx context.Context, cliCtx *registry.CLIContext, stderr io.Writer, deploymentID string, ids []string, targetDesc string) error {
	reqBody, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/jamf-protect/deployments/%s/tasks/retry", url.PathEscape(deploymentID))
	resp, err := cliCtx.Client.Do(ctx, "POST", path, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("retry failed: this tenant isn't registered with Jamf Protect (Cloud Services Connection not established): %s", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("retry failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	_, _ = fmt.Fprintf(stderr, "Retried %s.\n", targetDesc)
	out, err := json.Marshal(map[string]any{
		"deploymentId":   deploymentID,
		"retriedTaskIds": ids,
		"count":          len(ids),
	})
	if err != nil {
		return err
	}
	return cliCtx.Output.PrintRaw(out)
}
