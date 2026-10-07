// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"

	jamfclient "github.com/Jamf-Concepts/jamf-cli/internal/client"
	"github.com/Jamf-Concepts/jamf-cli/internal/platform"
	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// importOutcome is what importing one profile did, for the single-profile output
// and for the --all table.
type importOutcome struct {
	Type      string // "computer" or "mobile"
	ProfileID string
	Profile   string
	Takeover  string // "supported", "not supported", or "" when it was not reached
	// TakeoverUncertain marks a profile that takes over only if the API accepts
	// payload types jamf-cli lists as unsupported, which it has not so far.
	TakeoverUncertain bool
	BlueprintID       string
	Deployed          bool
	DryRun            bool
	// ScopeWidened counts the exclusions and limitations dropped from the scope,
	// each one a way the blueprint reaches devices the profile did not.
	ScopeWidened int
	// NotDeployed says why a created blueprint was left undeployed under --deploy.
	NotDeployed string
	bp          *blueprints.BlueprintDetail
}

// importRun is how one import is run.
type importRun struct {
	// profileType is "computer" or "mobile"; empty means the command's --type.
	profileType string
	// preview computes everything and creates nothing: the first pass of --all
	// --deploy, which counts what the confirmation is about.
	preview bool
	// confirmAlongside asks before deploying a blueprint that installs beside the
	// Classic profile. --all asks once for the whole set instead.
	confirmAlongside bool
}

// skipError is a refusal that is the command working as asked, not a failure: a
// profile that --takeover-only, --skip-exclusions or --skip-limitations keeps
// out, or one with no device-group scope to import. --all counts these as
// skipped and does not let them fail the run.
type skipError struct {
	msg string
	err error // the underlying refusal, when there is one
}

func (e *skipError) Error() string { return e.msg }

func (e *skipError) Unwrap() error { return e.err }

// takeoverLabel is the takeover column of the --all table.
func takeoverLabel(t profileconvert.TakeoverReport) string {
	if t.Supported {
		return "supported"
	}
	return "not supported"
}

// classicProfileRef is a profile in the Classic list.
type classicProfileRef struct {
	ID   string
	Name string
}

// listClassicProfiles returns every configuration profile of a type, in the
// order the Classic API lists them.
func listClassicProfiles(ctx context.Context, client registry.HTTPClient, profileType string) ([]classicProfileRef, error) {
	resp, err := client.Do(ctx, "GET", "/JSSResource/"+classicProfileCollection(profileType), nil)
	if err != nil {
		return nil, fmt.Errorf("listing %s configuration profiles: %w", profileType, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := jamfclient.ReadResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("reading the profile list: %w", err)
	}
	return parseClassicProfileList(body)
}

// parseClassicProfileList reads the id and name of each child of a Classic
// collection, whichever element the type names them.
func parseClassicProfileList(body []byte) ([]classicProfileRef, error) {
	var list struct {
		Items []struct {
			ID   string `xml:"id"`
			Name string `xml:"name"`
		} `xml:",any"`
	}
	if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&list); err != nil {
		return nil, fmt.Errorf("parsing the profile list: %w", err)
	}
	refs := make([]classicProfileRef, 0, len(list.Items))
	for _, it := range list.Items {
		if strings.TrimSpace(it.ID) != "" {
			refs = append(refs, classicProfileRef{ID: strings.TrimSpace(it.ID), Name: it.Name})
		}
	}
	return refs, nil
}

// importRow is one line of the --all table. Every key is always present: the
// first row decides the table's columns.
type importRow struct {
	ID, Type, Profile, Result, Takeover, Blueprint, Deployed, Detail string
}

// firstLine is the part of an error worth a table cell.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

// deployCounts is what --all --deploy would do, tallied from the preview pass.
type deployCounts struct{ takeOver, uncertain, alongside, held int }

func (c *deployCounts) add(out *importOutcome) {
	switch {
	case out.ScopeWidened > 0:
		c.held++
	case out.TakeoverUncertain:
		c.uncertain++
	case out.Takeover == "supported":
		c.takeOver++
	default:
		c.alongside++
	}
}

// deployable is every blueprint the real pass would deploy, held ones excluded.
func (c deployCounts) deployable() int { return c.takeOver + c.uncertain + c.alongside }

func (c deployCounts) summary() string {
	return fmt.Sprintf("Deploying would: take over the installed profile for %d; take it over for up to %d more only if the API "+
		"accepts payload types jamf-cli lists as unsupported (otherwise they install alongside); install alongside the Classic profile "+
		"(both stay active) for %d; create but not deploy %d whose scope would widen.", c.takeOver, c.uncertain, c.alongside, c.held)
}

// runImportAll imports every profile of the given types. A profile that fails or
// is skipped does not stop the rest. Each profile's detail goes to a buffer that
// is shown only when it fails, and the run ends with a table of the outcomes.
func runImportAll(ctx context.Context, cliCtx *registry.CLIContext, types []string, deploy, yes bool,
	importOne func(context.Context, string, io.Writer, importRun) (*importOutcome, error),
) error {
	type target struct {
		profileType string
		ref         classicProfileRef
	}
	var targets []target
	var perType []string
	for _, t := range types {
		profiles, err := listClassicProfiles(ctx, cliCtx.Client, t)
		if err != nil {
			return err
		}
		perType = append(perType, fmt.Sprintf("%d %s", len(profiles), t))
		for _, p := range profiles {
			targets = append(targets, target{t, p})
		}
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "No %s configuration profiles found.\n", strings.Join(types, " or "))
		return nil
	}
	fmt.Fprintf(os.Stderr, "Found %d profile(s): %s.\n", len(targets), strings.Join(perType, ", "))

	if deploy && !cliCtx.DryRun {
		// Deploying is the part that reaches devices, and --all does it in bulk. Count
		// what it would do first, with a pass that creates nothing, so the
		// confirmation can say how many blueprints take over the installed profile,
		// how many install beside it, and how many will not be deployed at all.
		var counts deployCounts
		for _, t := range targets {
			out, err := importOne(ctx, t.ref.ID, io.Discard, importRun{preview: true, profileType: t.profileType})
			if err != nil {
				continue // skipped or failing: nothing will be created for it
			}
			counts.add(out)
		}
		fmt.Fprintln(os.Stderr, counts.summary())
		if counts.deployable() == 0 {
			fmt.Fprintln(os.Stderr, "Nothing to deploy.")
		}
		if err := platform.ConfirmAction("deploy", fmt.Sprintf("up to %d blueprint(s)", counts.deployable()), yes); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, profileconvert.ConflictWarning)

	var rows []importRow
	var created, skipped, failed int
	var firstErr error
	for i, t := range targets {
		var log bytes.Buffer
		out, err := importOne(ctx, t.ref.ID, &log, importRun{profileType: t.profileType})
		row := importRow{ID: t.ref.ID, Type: t.profileType, Profile: t.ref.Name, Takeover: out.Takeover, Blueprint: out.BlueprintID, Deployed: "no"}
		if out.Profile != "" {
			row.Profile = out.Profile
		}
		var skip *skipError
		switch {
		case errors.As(err, &skip):
			skipped++
			row.Result, row.Detail = "skipped", firstLine(err)
		case err != nil:
			failed++
			if firstErr == nil {
				firstErr = err
			}
			row.Result, row.Detail = "failed", firstLine(err)
			fmt.Fprintf(os.Stderr, "--- %s (%s id %s)\n%s\n", row.Profile, t.profileType, t.ref.ID, strings.TrimRight(log.String(), "\n"))
		case out.DryRun:
			created++
			row.Result = "would create"
		default:
			created++
			row.Result = "created"
			if out.Deployed {
				row.Deployed = "yes"
			}
			row.Detail = out.NotDeployed
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %s (id %s): %s\n", i+1, len(targets), t.profileType, row.Profile, t.ref.ID, row.Result)
		rows = append(rows, row)
	}

	table := make([]map[string]any, len(rows))
	for i, r := range rows {
		table[i] = map[string]any{
			"id": r.ID, "type": r.Type, "profile": r.Profile, "result": r.Result, "takeover": r.Takeover,
			"blueprint": r.Blueprint, "deployed": r.Deployed, "detail": r.Detail,
		}
	}
	if err := printRows(cliCtx, table); err != nil {
		return err
	}
	verb := "created"
	note := ""
	if cliCtx.DryRun {
		verb = "would be created"
		note = " (dry run: whether the API accepts each profile as installed is not tested)"
	}
	fmt.Fprintf(os.Stderr, "%d profile(s): %d %s, %d skipped, %d failed%s\n", len(targets), created, verb, skipped, failed, note)
	return finishBatch(os.Stderr, "profiles", created, failed, firstErr)
}

// nothingToImport turns the conversion's "every payload was dropped" errors into
// a skip: a profile made only of types blueprints disables has nothing to
// import, which is not a failure of the run. Any other error is returned as is.
func nothingToImport(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "no payloads remain") || strings.Contains(msg, "no components produced") {
		return &skipError{msg: "nothing to import: " + msg, err: err}
	}
	return err
}
