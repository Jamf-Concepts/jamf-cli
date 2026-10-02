// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Jamf-Concepts/jamf-cli/generator/parser"
	platformgen "github.com/Jamf-Concepts/jamf-cli/generator/platform"
	securitygen "github.com/Jamf-Concepts/jamf-cli/generator/security"
	"github.com/spf13/cobra"
)

// responseSecretVerdict classifies one operation whose 2xx response declares a
// readable string property named like a secret. TestMCPSecretNamingLeaves_AreClassified
// judges a leaf on its help, and help need not name what the response
// carries: `pro cloud-distribution-point list` returns the CloudFront private
// key and passed that guard. This one judges the response.
type responseSecretVerdict struct {
	// props is every flagged property, space-separated in sorted order, so a
	// secret added to an already-classified response fails until judged.
	props string
	// leaf is the command printing the response, for a verdict that depends on
	// what that command does over MCP.
	leaf string
	// refused requires leaf to be refused over MCP.
	refused bool
	// reason says why the properties may reach the model; empty only when refused.
	reason string
}

const (
	exemptTimestamp      = "a date or expiry, not a secret"
	exemptLabel          = "the label shown beside a password prompt, not a password"
	exemptRedactedCDP    = "printed with the key as <redacted> by cdnKeyRedactingClient in an MCP child"
	exemptDDMServerToken = "the declaration's ServerToken, a version hash a device echoes back, not a credential"
)

// responseSecretVerdicts is keyed "METHOD path" as the spec declares the path.
var responseSecretVerdicts = map[string]responseSecretVerdict{
	"GET /devices/v1/devices/{id}":               {props: ".security.bootstrapTokenEscrowedStatus", reason: "an escrow status enum, not the token"},
	"GET /partners/v1/distributor/configuration": {props: ".webhook.secretName", reason: "names the webhook's secret, not its value"},
	"GET /sso/v1/connections/{connectionId}":     {props: ".oidcOptions.tokenEndpoint .oktaOptions.tokenEndpoint .tokenEndpointAuthMethod", reason: "the token endpoint's URL and its auth method enum"},
	"POST /sso/v1/connections":                   {props: ".oidcOptions.tokenEndpoint .oktaOptions.tokenEndpoint .tokenEndpointAuthMethod", reason: "the token endpoint's URL and its auth method enum"},
	"PUT /sso/v1/connections/{connectionId}":     {props: ".oidcOptions.tokenEndpoint .oktaOptions.tokenEndpoint .tokenEndpointAuthMethod", reason: "the token endpoint's URL and its auth method enum"},

	"GET /v1/accounts/{id}":                        {props: ".lastPasswordChange", reason: exemptTimestamp},
	"POST /v1/accounts":                            {props: ".lastPasswordChange", reason: exemptTimestamp},
	"PUT /v1/accounts/{id}":                        {props: ".lastPasswordChange", reason: exemptTimestamp},
	"GET /v1/device-enrollments/{id}":              {props: ".tokenExpirationDate", reason: exemptTimestamp},
	"PUT /v1/device-enrollments/{id}":              {props: ".tokenExpirationDate", reason: exemptTimestamp},
	"PUT /v1/device-enrollments/{id}/upload-token": {props: ".tokenExpirationDate", reason: exemptTimestamp},
	"GET /v1/volume-purchasing-locations/{id}":     {props: ".tokenExpiration", reason: exemptTimestamp},
	"PATCH /v1/volume-purchasing-locations/{id}":   {props: ".tokenExpiration", reason: exemptTimestamp},

	"GET /v1/enrollment-customization/{id}/ldap/{panel-id}": {props: ".passwordLabel", reason: exemptLabel},
	"POST /v1/enrollment-customization/{id}/ldap":           {props: ".passwordLabel", reason: exemptLabel},
	"PUT /v1/enrollment-customization/{id}/ldap/{panel-id}": {props: ".passwordLabel", reason: exemptLabel},
	"GET /v3/enrollment/languages/{languageId}":             {props: ".password", reason: exemptLabel},
	"PUT /v3/enrollment/languages/{languageId}":             {props: ".password", reason: exemptLabel},
	"GET /v3/computer-prestages/{id}":                       {props: ".recoveryLockPasswordType", reason: "an enum naming how the recovery lock password is set"},
	"PUT /v3/computer-prestages/{id}":                       {props: ".recoveryLockPasswordType", reason: "an enum naming how the recovery lock password is set"},

	"GET /v2/local-admin-password/{clientManagementId}/account/{username}/password":        {props: ".password", leaf: "pro local-admin-password password", reason: exemptDeviceSecret},
	"GET /v2/local-admin-password/{clientManagementId}/account/{username}/{guid}/password": {props: ".password", leaf: "pro local-admin-password password-by-guid", reason: exemptDeviceSecret},
	"GET /v2/mobile-devices/{id}/detail":                                                   {props: mobileDeviceSecretProps, leaf: "pro mobile-devices detail-by-id", reason: exemptDeviceSecret},
	"PATCH /v2/mobile-devices/{id}":                                                        {props: mobileDeviceSecretProps, leaf: "pro mobile-devices patch", reason: exemptSetsDeviceSecret},
	"POST /v1/jcds/files":                                                                  {props: ".secretAccessKey .sessionToken", leaf: "pro jamf-cloud-distribution-service-files create", reason: exemptJCDS},
	"POST /v1/jcds/renew-credentials":                                                      {props: ".secretAccessKey .sessionToken", leaf: "pro jamf-cloud-distribution-service renew-credentials", reason: exemptJCDS},
	"GET /v1/cloud-distribution-point":                                                     {props: ".privateKey", leaf: "pro cloud-distribution-point list", reason: exemptRedactedCDP},

	"GET /ddm/report/v1/declarations/{declarationIdentifier}/devices": {props: ".results.serverToken", reason: exemptDDMServerToken},
	"GET /ddm/report/v1/devices/{deviceId}/declarations":              {props: ".results.serverToken", reason: exemptDDMServerToken},
	"GET /sso/v1/connections":                                         {props: ".results.tokenEndpointAuthMethod", reason: "the token endpoint's auth method enum"},
	"GET /v1/accounts":                                                {props: ".results.lastPasswordChange", reason: exemptTimestamp},
	"GET /v1/device-enrollments":                                      {props: ".results.tokenExpirationDate", reason: exemptTimestamp},
	"GET /v1/volume-purchasing-locations":                             {props: ".results.tokenExpiration", reason: exemptTimestamp},
	"GET /v3/computer-prestages":                                      {props: ".results.recoveryLockPasswordType", reason: "an enum naming how the recovery lock password is set"},
	"GET /v3/enrollment/languages":                                    {props: ".results.password", reason: exemptLabel},
	"GET /v4/computers-inventory":                                     {props: ".results.security.bootstrapTokenEscrowedStatus", reason: "an escrow status enum, not the token"},
	"GET /v4/computers-inventory/{id}":                                {props: ".security.bootstrapTokenEscrowedStatus", reason: "an escrow status enum, not the token"},
	"GET /v4/computers-inventory-detail/{id}":                         {props: ".security.bootstrapTokenEscrowedStatus", reason: "an escrow status enum, not the token"},
	"GET /v2/mobile-devices/detail":                                   {props: ".results.security.bootstrapTokenEscrowed", reason: "an escrow flag, not the token"},
	"GET /v2/mobile-devices/{id}/paired-devices":                      {props: ".results.security.bootstrapTokenEscrowed", reason: "an escrow flag, not the token"},

	"GET /v2/local-admin-password/{clientManagementId}/account/{username}/audit":        {props: ".results.password", leaf: "pro local-admin-password audit", reason: exemptDeviceSecret},
	"GET /v2/local-admin-password/{clientManagementId}/account/{username}/{guid}/audit": {props: ".results.password", leaf: "pro local-admin-password audit-by-guid", reason: exemptDeviceSecret},
	"GET /v4/computers-inventory/{id}/view-recovery-lock-password":                      {props: ".recoveryLockPassword", leaf: "pro computer-inventory view-recovery-lock-password", reason: exemptDeviceSecret},
	"GET /v2/mobile-device-groups/smart-group-membership/{id}":                          {props: ".results.airPlayPassword", reason: exemptDeviceSecret},
	"GET /v2/mobile-device-groups/static-group-membership/{id}":                         {props: ".results.airPlayPassword", reason: exemptDeviceSecret},

	"POST /auth/keepAlive":                              {props: ".token", leaf: "pro api-authentication keep-alive", refused: true},
	"POST /v1/cloud-distribution-point":                 {props: ".privateKey", leaf: "pro cloud-distribution-point create", refused: true},
	"PATCH /v1/cloud-distribution-point":                {props: ".privateKey", leaf: "pro cloud-distribution-point patch", refused: true},
	"GET /v1/oauth2/session-tokens":                     {props: ".accessToken .idToken", leaf: "pro sso-oauth-session-tokens list", refused: true},
	"POST /v1/api-integrations/{id}/client-credentials": {props: ".clientSecret", leaf: "pro api-integrations client-credentials", refused: true},
	"POST /v1/auth/token":                               {props: ".token", leaf: "pro api-authentication token", refused: true},
	"POST /v1/auth/keep-alive":                          {props: ".token", leaf: "pro api-authentication keep-alive", refused: true},
	"POST /v1/oauth/token":                              {props: ".access_token .token_type", leaf: "pro api-authentication oauth-token", refused: true},
}

const mobileDeviceSecretProps = ".ios.security.bootstrapToken .ios.security.bootstrapTokenEscrowed .ios.unlockToken .tvos.airplayPassword .visionos.security.bootstrapToken .visionos.security.bootstrapTokenEscrowed .visionos.unlockToken .watchos.security.bootstrapToken .watchos.security.bootstrapTokenEscrowed .watchos.unlockToken"

// loadSpecResources is every Pro, Platform and Security Cloud resource the
// generators derive from the committed specs.
var loadSpecResources = sync.OnceValues(func() ([]*parser.Resource, error) {
	specs := filepath.Join("..", "..", "specs")
	proSpecs, err := filepath.Glob(filepath.Join(specs, "*.yaml"))
	if err != nil {
		return nil, err
	}
	pro, _, err := parser.LoadDocuments(proSpecs)
	if err != nil {
		return nil, fmt.Errorf("loading the Pro specs: %w", err)
	}
	plat, _, err := platformgen.LoadResources(filepath.Join(specs, "platform"))
	if err != nil {
		return nil, fmt.Errorf("loading the Platform specs: %w", err)
	}
	sec, _, _, err := securitygen.LoadResources(filepath.Join(specs, "security"))
	if err != nil {
		return nil, fmt.Errorf("loading the Security Cloud specs: %w", err)
	}
	return slices.Concat(pro, plat, sec), nil
})

// responseSecretProps maps "METHOD path" to the sorted, space-separated
// readable string properties of any 2xx response whose name matches
// namesASecret. A writeOnly property is never in a response and is skipped.
func responseSecretProps(resources []*parser.Resource) map[string]string {
	found := map[string]map[string]bool{}
	for _, r := range parser.FlattenResources(resources) {
		for _, op := range r.Operations {
			for code, resp := range op.Responses {
				if !strings.HasPrefix(code, "2") || resp == nil {
					continue
				}
				key := op.Method + " " + op.Path
				walkSecretProps(resp.Schema, "", map[*parser.Schema]bool{}, func(p string) {
					if found[key] == nil {
						found[key] = map[string]bool{}
					}
					found[key][p] = true
				})
			}
		}
	}
	out := make(map[string]string, len(found))
	for key, props := range found {
		names := make([]string, 0, len(props))
		for p := range props {
			names = append(names, p)
		}
		sort.Strings(names)
		out[key] = strings.Join(names, " ")
	}
	return out
}

func walkSecretProps(s *parser.Schema, prefix string, seen map[*parser.Schema]bool, hit func(string)) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	walkSecretProps(s.Items, prefix, seen, hit)
	for name, p := range s.Properties {
		if p == nil {
			continue
		}
		walkSecretProps(p.Nested, prefix+"."+name, seen, hit)
		walkSecretProps(p.Items, prefix+"."+name, seen, hit)
		if p.Type == "string" && !p.WriteOnly && namesASecret.MatchString(name) {
			hit(prefix + "." + name)
		}
	}
}

// checkResponseSecrets returns one problem per operation in found that verdicts
// does not classify correctly, and per verdict found no longer needs.
func checkResponseSecrets(found map[string]string, verdicts map[string]responseSecretVerdict, root *cobra.Command) []string {
	leaves := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			walk(s)
		}
		if !c.HasSubCommands() {
			leaves[strings.TrimPrefix(c.CommandPath(), root.Name()+" ")] = c
		}
	}
	walk(root)

	var problems []string
	for key, props := range found {
		v, ok := verdicts[key]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s returns %s, named like a secret and not writeOnly: refuse the command that prints it, redact it over MCP, or add a verdict with the reason it may reach the model", key, props))
			continue
		case v.props != props:
			problems = append(problems, fmt.Sprintf("%s returns %s, but its verdict judged %s; judge the difference", key, props, v.props))
		}
		if v.leaf == "" {
			if v.refused || v.reason == "" {
				problems = append(problems, fmt.Sprintf("%s: a verdict without a leaf needs a reason and cannot be refused", key))
			}
			continue
		}
		c, ok := leaves[v.leaf]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: verdict names %q, which is not a leaf", key, v.leaf))
			continue
		}
		refused := refuseOverMCP(childInvocation{path: c.CommandPath()}, "prod") != nil
		switch {
		case v.refused && !refused:
			problems = append(problems, fmt.Sprintf("%s: %q prints %s and must be refused over MCP", key, v.leaf, props))
		case !v.refused && refused:
			problems = append(problems, fmt.Sprintf("%s: %q is refused over MCP; mark the verdict refused", key, v.leaf))
		case !v.refused && v.reason == "":
			problems = append(problems, fmt.Sprintf("%s: %q runs over MCP and its verdict gives no reason", key, v.leaf))
		}
	}
	for key := range verdicts {
		if _, ok := found[key]; !ok {
			problems = append(problems, fmt.Sprintf("responseSecretVerdicts names %q, which no 2xx response flags any more; remove it", key))
		}
	}
	sort.Strings(problems)
	return problems
}

// TestMCPResponseSecrets_AreClassified walks the 2xx response schemas of the
// committed Pro, Platform and Security Cloud specs for a readable string
// property named like a secret, and requires each such operation to be
// refused, redacted or exempted with a reason in responseSecretVerdicts.
func TestMCPResponseSecrets_AreClassified(t *testing.T) {
	resources, err := loadSpecResources()
	if err != nil {
		t.Fatal(err)
	}
	found := responseSecretProps(resources)
	if len(found) < 20 {
		t.Fatalf("only %d operations flagged; the walk is not reaching the specs", len(found))
	}
	for _, p := range checkResponseSecrets(found, responseSecretVerdicts, NewRootCmd("test", "t", "t", "t")) {
		t.Error(p)
	}
}

// TestMCPResponseSecrets_FailsOnAnUnclassifiedResponse feeds the guard one
// fabricated operation answering a readable clientSecret beside a writeOnly
// password, and requires it to name the first and not the second.
func TestMCPResponseSecrets_FailsOnAnUnclassifiedResponse(t *testing.T) {
	op := &parser.Operation{Method: "GET", Path: "/v1/synthetic", Responses: map[string]*parser.Response{
		"200": {StatusCode: "200", Schema: &parser.Schema{Type: "object", Properties: map[string]*parser.Property{
			"results": {Name: "results", Type: "array", Items: &parser.Schema{Type: "object", Properties: map[string]*parser.Property{
				"clientSecret": {Name: "clientSecret", Type: "string"},
				"password":     {Name: "password", Type: "string", WriteOnly: true},
			}}},
		}}},
	}}
	found := responseSecretProps([]*parser.Resource{{Name: "synthetic", Operations: []*parser.Operation{op}}})
	if found["GET /v1/synthetic"] != ".results.clientSecret" {
		t.Fatalf("the walk found %v; want only .results.clientSecret", found)
	}
	problems := checkResponseSecrets(found, map[string]responseSecretVerdict{}, NewRootCmd("test", "t", "t", "t"))
	if len(problems) != 1 || !strings.Contains(problems[0], "GET /v1/synthetic returns .results.clientSecret") {
		t.Errorf("an unclassified response secret should be the one problem reported, got %q", problems)
	}

	refusedNoMore := map[string]responseSecretVerdict{"GET /v1/synthetic": {props: ".results.clientSecret", leaf: "pro cloud-distribution-point list", refused: true}}
	if problems := checkResponseSecrets(found, refusedNoMore, NewRootCmd("test", "t", "t", "t")); len(problems) != 1 || !strings.Contains(problems[0], "must be refused") {
		t.Errorf("a verdict claiming an allowed leaf is refused should fail, got %q", problems)
	}
}
