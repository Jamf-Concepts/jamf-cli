// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamf-cli/internal/xmlconv"
)

// Classic log flush. Wire-checked 2026-11-06 on a gateway profile:
//
//   - All three forms answer 201 and echo the request as <logflush>, with the
//     interval normalised ("Three Months" -> "3 MONTH").
//   - Only {Zero,One,Two,Three,Six} x {Days,Weeks,Months,Years} is accepted. Any
//     other count answers 500. "Zero" flushes every log, not none.
//   - The body form defaults an omitted interval to "0 DAY", which is a flush of
//     everything, so the interval is always sent.
//   - Unknown policy and computer ids answer 201 and flush nothing, so a typo
//     reads as success; both are looked up first.
//   - A non-numeric policy id in the path answers 201 and the echo drops the id.
//     That is the global flush, so every id is validated as a positive integer
//     before a request is built.
//   - The body form requires <computers>; without it the API answers 400, so no
//     unscoped flush is reachable through it.
//   - The API reports no count of what it removed.

// logflushCounts and logflushUnits are the interval words the API accepts.
var (
	logflushCounts = map[int]string{0: "Zero", 1: "One", 2: "Two", 3: "Three", 6: "Six"}
	logflushUnits  = []string{"DAYS", "WEEKS", "MONTHS", "YEARS"}
)

// logflushInterval renders --older-than and --unit as the API's interval, e.g.
// "Three Months". An unsupported count or unit is refused with the legal set,
// because the API answers 500 for one.
func logflushInterval(olderThan int, unit string) (string, error) {
	word, ok := logflushCounts[olderThan]
	if !ok {
		return "", fmt.Errorf("--older-than %d is not supported: the Classic API accepts 0, 1, 2, 3 or 6", olderThan)
	}
	u := strings.ToUpper(strings.TrimSpace(unit))
	if !strings.HasSuffix(u, "S") {
		u += "S" // accept DAY, WEEK, MONTH, YEAR
	}
	for _, legal := range logflushUnits {
		if u == legal {
			return word + " " + strings.ToUpper(u[:1]) + strings.ToLower(u[1:]), nil
		}
	}
	return "", fmt.Errorf("--unit %q is not supported: use %s", unit, strings.Join(logflushUnits, ", "))
}

// logflushID accepts a positive integer id and nothing else. See the wire notes
// above for why a non-numeric id must never reach the path.
func logflushID(kind, raw string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return "", fmt.Errorf("%s id %q is not a positive integer", kind, raw)
	}
	return strconv.Itoa(n), nil
}

// logflushPolicyPath is the per-policy endpoint. The interval is query-escaped
// ("Three+Months"), which is the form the API documents.
func logflushPolicyPath(id, interval string) string {
	return fmt.Sprintf("/JSSResource/logflush/policy/id/%s/interval/%s", id, strings.ReplaceAll(interval, " ", "+"))
}

// logflushComputersBody is the body of the per-computer form.
func logflushComputersBody(ids []string, interval string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><logflush><log>policy</log><interval>`)
	b.WriteString(interval)
	b.WriteString(`</interval><computers>`)
	for _, id := range ids {
		b.WriteString("<computer><id>" + id + "</id></computer>")
	}
	b.WriteString(`</computers></logflush>`)
	return b.String()
}

func newClassicLogflushCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "classic-logflush",
		Short: "Flush policy logs for one policy or some computers (Classic API)",
		Long: `Flush policy logs through the Classic API, so a policy set to "Once per
computer" can run again on devices that already ran it.

This is narrower than 'pro log-flushing-task create', which removes the logs of
every policy older than a retention period.

The Classic API accepts only these intervals: 0, 1, 2, 3 or 6, in days, weeks,
months or years. Flushing with --older-than 0 (the default) removes every log
for the target.`,
	}
	cmd.AddCommand(newClassicLogflushPolicyCmd(cliCtx))
	cmd.AddCommand(newClassicLogflushComputerCmd(cliCtx))
	return cmd
}

type logflushFlags struct {
	olderThan int
	unit      string
	yes       bool
}

func (f *logflushFlags) register(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.olderThan, "older-than", 0, "only logs older than this many units; the API accepts 0, 1, 2, 3 or 6 (0 flushes everything)")
	cmd.Flags().StringVar(&f.unit, "unit", "DAYS", "unit for --older-than: DAYS, WEEKS, MONTHS or YEARS")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "skip the confirmation prompt")
}

func newClassicLogflushPolicyCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var f logflushFlags
	cmd := &cobra.Command{
		Use:         "policy <policy-id>",
		Short:       "Flush the logs of one policy on every computer",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long: `Flush the logs of one policy on every computer. The policy then counts as
not yet run on those computers, so an "Once per computer" policy runs again.

The policy must exist: the API answers success for an id that matches nothing.
The API does not report how many logs it removed.`,
		Example: `  jamf-cli pro classic-logflush policy 42
  jamf-cli pro classic-logflush policy 42 --older-than 3 --unit MONTHS --yes
  jamf-cli -n pro classic-logflush policy 42`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := logflushID("policy", args[0])
			if err != nil {
				return err
			}
			interval, err := logflushInterval(f.olderThan, f.unit)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			name, err := logflushLookup(ctx, cliCtx.Client, "policy", "/JSSResource/policies/id/"+id)
			if err != nil {
				return err
			}
			target := fmt.Sprintf("the logs of policy %s (%s) older than %s", id, name, interval)
			return runLogflush(cliCtx, f.yes, target, func() (*http.Response, error) {
				return cliCtx.Client.Do(ctx, "DELETE", logflushPolicyPath(id, interval), nil)
			}, map[string]any{"log": "policy", "policy": id, "name": name, "interval": interval})
		},
	}
	f.register(cmd)
	return cmd
}

func newClassicLogflushComputerCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var f logflushFlags
	cmd := &cobra.Command{
		Use:         "computer <computer-id>...",
		Short:       "Flush the policy logs of one or more computers",
		Annotations: map[string]string{"jamf:destructive": "true"},
		Long: `Flush the policy logs of the named computers, across every policy.

Every computer must exist: the API answers success for an id that matches
nothing. The API does not report how many logs it removed.`,
		Example: `  jamf-cli pro classic-logflush computer 308
  jamf-cli pro classic-logflush computer 308 309 --older-than 1 --unit WEEKS --yes
  jamf-cli -n pro classic-logflush computer 308`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids := make([]string, 0, len(args))
			for _, a := range args {
				id, err := logflushID("computer", a)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			interval, err := logflushInterval(f.olderThan, f.unit)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			for _, id := range ids {
				if _, err := logflushLookup(ctx, cliCtx.Client, "computer", "/v4/computers-inventory/"+id+"?section=GENERAL"); err != nil {
					return err
				}
			}
			target := fmt.Sprintf("the policy logs of computer(s) %s older than %s", strings.Join(ids, ", "), interval)
			return runLogflush(cliCtx, f.yes, target, func() (*http.Response, error) {
				return cliCtx.Client.Do(ctx, "DELETE", "/JSSResource/logflush", strings.NewReader(logflushComputersBody(ids, interval)))
			}, map[string]any{"log": "policy", "computers": ids, "interval": interval})
		},
	}
	f.register(cmd)
	return cmd
}

// runLogflush previews under -n, then confirms, then sends. The preview comes
// first so `-n` never depends on --yes.
func runLogflush(cliCtx *registry.CLIContext, yes bool, target string, send func() (*http.Response, error), row map[string]any) error {
	if !cliCtx.DryRun {
		if err := platform.ConfirmAction("flush", target, yes); err != nil {
			return err
		}
	}
	resp, err := send()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if cliCtx.DryRun {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("flush failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	row["flushed"] = true
	return printRows(cliCtx, []map[string]any{row})
}

// logflushLookup confirms the target exists and returns its name. The flush
// endpoints answer 201 for an id that matches nothing, so this is the only
// place a typo is caught.
func logflushLookup(ctx context.Context, client registry.HTTPClient, kind, path string) (string, error) {
	resp, err := client.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("%s not found: %s", kind, path[strings.LastIndex(path, "/id/")+4:])
	default:
		return "", fmt.Errorf("looking up %s: HTTP %d", kind, resp.StatusCode)
	}
	return logflushName(resp.Body), nil
}

// logflushName pulls the record's name out of a lookup response, best effort:
// it is only there to make the confirmation prompt readable. A policy answers
// XML (<policy><general><name>) and a computer's inventory answers JSON
// ({"general":{"name":...}}).
func logflushName(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 10<<20))
	var m map[string]any
	if xmlconv.IsXML(data) {
		m, _ = xmlconv.ToMap(data)
	} else {
		_ = json.Unmarshal(data, &m)
	}
	if g, ok := m["general"].(map[string]any); ok {
		m = map[string]any{"": g}
	}
	for _, v := range m { // the XML root, when present, is the single key
		inner, _ := v.(map[string]any)
		if g, ok := inner["general"].(map[string]any); ok {
			inner = g
		}
		if name, ok := inner["name"].(string); ok {
			return name
		}
	}
	return ""
}
