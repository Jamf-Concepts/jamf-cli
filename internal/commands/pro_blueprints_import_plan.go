// Copyright 2026, Jamf Software LLC

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/jamf/jamfplatform-go-sdk/jamfplatform/blueprints"

	"github.com/Jamf-Concepts/jamf-cli/internal/profileconvert"
)

// importPlan is one way of turning a Classic profile into blueprint components,
// with the messages that explain it. import-profile builds two: the profile as
// installed (verbatim) and the profile with unsupported payload types delivered
// as Custom Settings (MCX). It sends the first and falls back to the second only
// when the API rejects the first, so a payload type the API has started
// accepting is found by asking the API rather than by trusting jamf-cli's list.
type importPlan struct {
	components []blueprints.Component
	takeover   profileconvert.TakeoverReport
	// messages are the stderr lines for this plan, in order. They are held back
	// so that a plan that is never sent says nothing.
	messages []string
}

func (p *importPlan) logf(format string, a ...any) {
	p.messages = append(p.messages, fmt.Sprintf(format, a...))
}

// importConvertOptions are the import-profile flags that shape the conversion.
type importConvertOptions struct {
	legacy             bool
	includeUnsupported bool
	stripDefaults      bool
}

// buildImportPlan converts a mobileconfig. verbatim sends payload types as
// installed instead of wrapping the ones the API does not list as standalone,
// and keeps empty payloads, so the result can take over the installed profile.
func buildImportPlan(mobileconfig []byte, o importConvertOptions, verbatim bool, fetcher *profileconvert.SchemaFetcher) (*importPlan, error) {
	plan := &importPlan{}
	if o.legacy {
		convert := profileconvert.ConvertMobileconfig
		if verbatim {
			convert = profileconvert.ConvertMobileconfigVerbatim
		}
		config, warnings, err := convert(mobileconfig, !o.includeUnsupported)
		if err != nil {
			return nil, fmt.Errorf("converting profile: %w", err)
		}
		for _, w := range warnings {
			plan.logf("Warning: %s", w)
		}
		// Validate here as the DDM path does: a payload missing a key Apple's
		// schema requires is refused by the API for the whole blueprint, so a
		// legacy import of one failed outright where the default import dropped it.
		var msgs []string
		if o.stripDefaults {
			config, msgs = profileconvert.StripConfigDefaults(config, fetcher)
		} else {
			config, msgs = profileconvert.ValidatePayloads(config, fetcher)
		}
		for _, m := range msgs {
			plan.logf("  %s", m)
		}
		if err := profileconvert.ConfigHasPayloads(config); err != nil {
			return nil, fmt.Errorf("no payloads remain after processing")
		}
		types := profileconvert.PayloadTypeSummary(mobileconfig)
		plan.logf("Processed %d payload(s) (legacy mode — no DDM conversion)", len(types))
		plan.messages = append(plan.messages, apiOnlyPayloadNote(config)...)
		config, plan.takeover, err = profileconvert.ApplyTakeoverIdentity(config, mobileconfig, 0)
		if err != nil {
			return nil, fmt.Errorf("checking takeover: %w", err)
		}
		plan.components = append(plan.components, blueprints.Component{
			Identifier:    "com.jamf.ddm-configuration-profile",
			Configuration: config,
		})
		return plan, nil
	}

	// DDM mode: promote compatible payloads to native DDM components.
	var defaultsFetcher *profileconvert.SchemaFetcher
	if o.stripDefaults {
		defaultsFetcher = fetcher
	}
	convert := profileconvert.ConvertToDDMComponents
	if verbatim {
		convert = profileconvert.ConvertToDDMComponentsVerbatim
	}
	ddmResult, err := convert(mobileconfig, !o.includeUnsupported, defaultsFetcher)
	if err != nil {
		return nil, fmt.Errorf("converting profile: %w", err)
	}
	for _, w := range ddmResult.Warnings {
		plan.logf("Warning: %s", w)
	}

	// Validate/strip the configuration-profile component (if any)
	if ddmResult.ProfileConfig != nil {
		var msgs []string
		if o.stripDefaults {
			ddmResult.ProfileConfig, msgs = profileconvert.StripConfigDefaults(ddmResult.ProfileConfig, fetcher)
		} else {
			ddmResult.ProfileConfig, msgs = profileconvert.ValidatePayloads(ddmResult.ProfileConfig, fetcher)
		}
		for _, m := range msgs {
			plan.logf("  %s", m)
		}
		if err := profileconvert.ConfigHasPayloads(ddmResult.ProfileConfig); err != nil {
			ddmResult.ProfileConfig = nil
		}
	}

	types := profileconvert.PayloadTypeSummary(mobileconfig)
	plan.logf("Processed %d payload(s)", len(types))
	for _, c := range ddmResult.Conversions {
		plan.logf("  %s (native DDM)", c)
	}
	if ddmResult.ProfileConfig != nil {
		plan.logf("  remaining payloads wrapped in configuration-profile component")
		plan.messages = append(plan.messages, apiOnlyPayloadNote(ddmResult.ProfileConfig)...)
		ddmResult.ProfileConfig, plan.takeover, err = profileconvert.ApplyTakeoverIdentity(ddmResult.ProfileConfig, mobileconfig, len(ddmResult.NativeComponents))
		if err != nil {
			return nil, fmt.Errorf("checking takeover: %w", err)
		}
	} else {
		plan.takeover = profileconvert.NativeConversionReport(len(ddmResult.NativeComponents))
	}

	for _, nc := range ddmResult.NativeComponents {
		plan.components = append(plan.components, blueprints.Component{
			Identifier:    nc.Identifier,
			Configuration: nc.Configuration,
		})
	}
	if ddmResult.ProfileConfig != nil {
		plan.components = append(plan.components, blueprints.Component{
			Identifier:    "com.jamf.ddm-configuration-profile",
			Configuration: ddmResult.ProfileConfig,
		})
	}
	if len(plan.components) == 0 {
		return nil, fmt.Errorf("no components produced — all payloads were stripped or unsupported")
	}
	return plan, nil
}

// sameComponents reports whether two plans would send the same blueprint, in
// which case there is nothing to fall back to.
func sameComponents(a, b *importPlan) bool {
	ab, errA := json.Marshal(a.components)
	bb, errB := json.Marshal(b.components)
	return errA == nil && errB == nil && string(ab) == string(bb)
}

// unlistedPayloadTypes is the payload types an accepted plan carries as
// standalone payloads that profileconvert.SupportedPayloadTypes does not list.
func unlistedPayloadTypes(components []blueprints.Component) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range components {
		if c.Identifier != "com.jamf.ddm-configuration-profile" {
			continue
		}
		for _, t := range profileconvert.UnlistedPayloadTypes(c.Configuration) {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// configurationRejectionField is the field the gateway names when a
// configuration-profile component fails its validation, whichever component and
// step it is.
var configurationRejectionField = regexp.MustCompile(`^steps\[\d+\]\.components\[\d+\]\.configuration$`)

// isConfigurationRejection reports whether err is the blueprints API refusing a
// configuration-profile component's payloads: HTTP 400 whose every detail is
// "Failed to validate configuration." on a component's configuration.
//
// This is deliberately narrow. The message does not name the payload, and the
// same wording answers an unsupported payload type, which is the case the
// fallback is for. Anything else, a timeout, a 5xx, a 401/403, a bad scope or a
// name that is too long, must surface as it is: the fallback would only send the
// same body again.
func isConfigurationRejection(err error) bool {
	var s statusCarrier
	if !errors.As(err, &s) || !s.HasStatus(http.StatusBadRequest) {
		return false
	}
	var d detailCarrier
	if !errors.As(err, &d) {
		return false
	}
	details := d.Details()
	if len(details) == 0 {
		return false
	}
	for _, detail := range details {
		if !configurationRejectionField.MatchString(detail.Field) ||
			!strings.HasPrefix(strings.ToLower(strings.TrimSpace(detail.Description)), "failed to validate configuration") {
			return false
		}
	}
	return true
}

// apiOnlyPayloadNote is warnAPIOnlyPayloads' text, as lines.
func apiOnlyPayloadNote(config json.RawMessage) []string {
	apiOnly := profileconvert.APIOnlyPayloadTypes(config)
	if len(apiOnly) == 0 {
		return nil
	}
	lines := []string{"Note: the following payload(s) can only be managed through the blueprints API — " +
		"they show as read-only \"Legacy payload\" items in the Jamf Pro UI and cannot be edited there:"}
	for _, pt := range apiOnly {
		lines = append(lines, "  - "+pt)
	}
	return lines
}

// printPlanMessages writes a plan's explanation to stderr.
func printPlanMessages(p *importPlan) {
	for _, m := range p.messages {
		fmt.Fprintln(os.Stderr, m)
	}
}

// sendWithFallback sends the plan as installed and, only when the API refuses
// its payloads (isConfigurationRejection), sends the fallback instead. It
// returns the plan that was accepted.
//
// A 400 creates nothing, so sending again cannot duplicate the blueprint. Every
// other failure is returned untouched: the fallback would not change it, and a
// timed-out or 5xx write may already have been applied.
func sendWithFallback(w io.Writer, plan, fallback *importPlan, send func(*importPlan) (string, error)) (*importPlan, string, error) {
	id, err := send(plan)
	if err == nil {
		if unlisted := unlistedPayloadTypes(plan.components); len(unlisted) > 0 && fallback != nil {
			_, _ = fmt.Fprintf(w, "Note: the API accepted %s as standalone payload type(s) that jamf-cli lists as unsupported; "+
				"profileconvert.SupportedPayloadTypes may be out of date.\n", strings.Join(unlisted, ", "))
		}
		return plan, id, nil
	}
	if fallback == nil || !isConfigurationRejection(err) {
		return plan, "", err
	}
	_, _ = fmt.Fprintln(w, "Warning: the blueprints API rejected the payloads as installed. "+
		"Retrying with the payload types it does not accept standalone delivered as Custom Settings (MCX), "+
		"which cannot take over the installed profile.")
	for _, m := range fallback.messages {
		_, _ = fmt.Fprintln(w, m)
	}
	id, retryErr := send(fallback)
	if retryErr != nil {
		return fallback, "", fmt.Errorf("%w (the profile as installed was also rejected: %v)", retryErr, err)
	}
	return fallback, id, nil
}
