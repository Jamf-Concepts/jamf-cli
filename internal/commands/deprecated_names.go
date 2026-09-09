// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Every `pro` resource name that changed when command names started coming from
// the spec instead of from the name of the file its paths were split into, kept
// working as a cobra alias and warned about on use.
//
// The names were never a contract anyone chose: `pro static-computer-groups`
// existed because of `StaticComputerGroups.yaml`, upstream's jss module
// filename, which appears in no spec. That does not make them any less load
// bearing for a script that already types them, hence this table.
//
// It is deliberately temporary. Every entry expires on one date, and
// TestDeprecatedNamesHaveNotExpired fails the build once it passes — naming the
// entries to delete. A comment saying "remove after March" is how dead code
// lives for years; a failing test is the only mechanism that actually removes
// it.

// deprecatedNamesRemovedAfter is the date the aliases below stop being
// supported. Six months from the rename.
//
// Not a per-entry date, because every entry was created by one change and there
// is no case for retiring them at different times. A future rename gets its own
// date and its own table.
const deprecatedNamesRemovedAfter = "2027-03-09"

// deprecatedName records what an old resource name became.
type deprecatedName struct {
	// Now is the resource that serves the old name's endpoints.
	Now string
}

// deprecatedNames maps a retired `pro` resource name to its replacement.
//
// Three shapes are folded together here, and the second and third are why this
// is a table rather than a rule:
//
//   - A rename. `icons` became `icon`, `csas` became `csa`,
//     `computers-inventory` became `computer-inventory` — a name is now the
//     OpenAPI tag, which is the section heading the API reference publishes,
//     rather than an auto-pluralisation of a filename.
//   - A merge. Several resources became one, so several old names point at the
//     same replacement: `computers-inventory` absorbed `computer-smart-groups`,
//     `erase-device-computers` and `remove-computer-mdm-profiles`.
//   - A split. One resource became several, and an alias can only point at one
//     of them. The parent resource inherits the name — the shortest of the
//     candidates — because it is the one a caller of the old name was most
//     likely reaching for. Marked below; the other half has to be found by name.
//
// An old name that is *still* a live resource name gets no entry, however its
// endpoints were redistributed: `enrollment-languages`, `jcds` and
// `computer-inventory-collection-settings` each kept their name for one half of
// a split, so they resolve without help and an alias for them would make cobra
// ambiguous.
//
// Generated from a diff of the resource sets either side of the change, then
// reviewed. TestDeprecatedNamesPointAtCommandsThatShip is what keeps it honest.
var deprecatedNames = map[string]deprecatedName{
	"access-managements": {Now: "enrollment"},
	"account-driven-user-enrollment-session-token-settings": {Now: "enrollment"},
	"account-preferences":                    {Now: "jamf-pro-account-preferences"},
	"activation-codes":                       {Now: "activation-code"},
	"api-roles-privileges":                   {Now: "api-role-privileges"},
	"app-installer-deployments":              {Now: "app-installers-deployments"},
	"app-installer-global-settings":          {Now: "app-installers"},
	"app-installer-titles":                   {Now: "app-installers-titles"},
	"app-requests":                           {Now: "app-request"}, // split: the other half keeps its own name
	"authentications":                        {Now: "api-authentication"},
	"cache":                                  {Now: "cache-settings"},
	"certificate-authorities":                {Now: "certificate-authority"},
	"change-passwords":                       {Now: "jamf-pro-user-account-settings"},
	"classic-ldaps":                          {Now: "classic-ldap"},
	"cloud-azure-defaults":                   {Now: "cloud-azure"},
	"cloud-azures":                           {Now: "cloud-azure"},
	"cloud-distribution-points":              {Now: "cloud-distribution-point"},
	"cloud-id-p-configurations":              {Now: "cloud-idp"},
	"cloud-id-p-histories":                   {Now: "cloud-idp"},
	"cloud-id-p-test-searches":               {Now: "cloud-idp"},
	"cloud-informations":                     {Now: "cloud-information"},
	"cloud-ldap-connections":                 {Now: "cloud-ldap"},
	"cloud-ldap-defaults":                    {Now: "cloud-ldap"},
	"cloud-ldap-key-stores":                  {Now: "cloud-ldap"},
	"cloud-ldap-mappings":                    {Now: "cloud-ldap"},
	"cloud-ldaps":                            {Now: "cloud-ldap"},
	"computer-prestage-scopes":               {Now: "computer-prestages"},
	"computer-smart-groups":                  {Now: "computer-inventory"},
	"computers-inventory":                    {Now: "computer-inventory"},
	"country-codes":                          {Now: "app-store-country-codes"},
	"csas":                                   {Now: "csa"},
	"dashboards":                             {Now: "dashboard"},
	"database-connections":                   {Now: "jamf-pro-initialization"},
	"ddm-status":                             {Now: "declarative-device-management"},
	"ddm-syncs":                              {Now: "declarative-device-management"},
	"device-compliance-informations":         {Now: "conditional-access"},
	"device-enrollment-instance-sync-states": {Now: "device-enrollments"},
	"device-enrollment-instances":            {Now: "device-enrollments"},
	"digi-cert-settings":                     {Now: "digicert"},
	"distribution-points":                    {Now: "distribution-point"},
	"dss-proxies":                            {Now: "declarative-device-management"},
	"enrollment-customization-panels":        {Now: "enrollment-customization"},
	"enrollment-customizations":              {Now: "enrollment-customization"}, // split: the other half keeps its own name
	"enrollment-settings":                    {Now: "enrollment"},               // split: the other half keeps its own name
	"erase-device-computers":                 {Now: "computer-inventory"},
	"erase-device-mobiles":                   {Now: "mobile-devices"},
	"health-checks":                          {Now: "health-check"},
	"icons":                                  {Now: "icon"},
	"inventory-informations":                 {Now: "inventory-information"},
	"inventory-preloads":                     {Now: "inventory-preload"}, // split: the other half keeps its own name
	"jamf-connect-deployment-tasks":          {Now: "jamf-connect"},
	"jamf-connects":                          {Now: "jamf-connect"}, // split: the other half keeps its own name
	"jamf-packages":                          {Now: "jamf-package"},
	"jamf-pro-informations":                  {Now: "jamf-pro-information"},
	"jamf-pro-versions":                      {Now: "jamf-pro-version"},
	"jamf-protect-deployment-tasks":          {Now: "jamf-protect"},
	"jamf-protect-plans":                     {Now: "jamf-protect"},
	"jamf-remote-assist-session-histories":   {Now: "jamf-remote-assist"},
	"jcds":                                   {Now: "jamf-cloud-distribution-service"}, // split: the other half keeps its own name
	"last-logins":                            {Now: "last-login"},
	"ldap-rs":                                {Now: "ldap"},
	"local-admin-passwords":                  {Now: "local-admin-password"},
	"log-flushings":                          {Now: "log-flushing"}, // split: the other half keeps its own name
	"mac-os-managed-software-updates":        {Now: "macos-managed-software-updates"},
	"mdm-commands":                           {Now: "mdm"},
	"mdm-renewals":                           {Now: "mdm-renewal"}, // split: the other half keeps its own name
	"mobile-device-enrollment-profiles":      {Now: "mobile-device-enrollment-profile"},
	"mobile-device-inventory-details":        {Now: "mobile-devices"},
	"mobile-device-prestage-scopes":          {Now: "mobile-device-prestages"},
	"mobile-device-prestage-sync-states":     {Now: "mobile-device-prestages"},
	"mobile-device-smart-groups":             {Now: "mobile-devices"},
	"notifications":                          {Now: "jamf-pro-notifications"},
	"oauth-token-sessions":                   {Now: "sso-oauth-session-tokens"},
	"oidcs":                                  {Now: "oidc"},
	"onboarding-configuration":               {Now: "onboarding"},
	"onboardings":                            {Now: "onboarding"},
	"package-deployments":                    {Now: "mdm"},
	"patch-policies":                         {Now: "patch-policy-logs"},
	"patch-titles":                           {Now: "patch-management"},
	"reenrollment":                           {Now: "re-enrollment"},
	"remove-computer-mdm-profiles":           {Now: "computer-inventory"},
	"remove-mobile-device-mdm-profiles":      {Now: "mobile-devices"},
	"renew-mdm-profiles":                     {Now: "mdm"},
	"return-to-service-configurations":       {Now: "return-to-service"},
	"schedulers":                             {Now: "scheduler"}, // split: the other half keeps its own name
	"self-service-branding-images":           {Now: "self-service"},
	"self-service-settings":                  {Now: "self-service"},
	"service-discovery":                      {Now: "service-discovery-enrollment"},
	"slasas":                                 {Now: "slasa"},
	"sso-failovers":                          {Now: "sso-settings"},
	"sso-settings-cert":                      {Now: "sso-settings"},
	"static-computer-groups":                 {Now: "computer-groups-static-groups"},
	"systems":                                {Now: "jamf-pro-initialization"},
	"teacher-settings":                       {Now: "teacher-app"},
	"team-viewer-remote-administrations":     {Now: "team-viewer-remote-administration"},
	"user-accounts":                          {Now: "accounts"},
	"user-preferences":                       {Now: "jamf-pro-user-account-settings"},
	"user-smart-groups":                      {Now: "smart-user-groups"},
	"users":                                  {Now: "smart-user-groups"},
	"venafis":                                {Now: "venafi"},
	"vpp-locations":                          {Now: "volume-purchasing-locations"},
	"vpp-subscriptions":                      {Now: "volume-purchasing-subscriptions"},
}

// withdrawnNames are old resource names whose endpoints are no longer ingested
// at all, so there is nothing to alias them to.
//
// They were legacy unversioned paths that upstream parked outside the versioned
// API — `/preview/remote-administration-configurations` and
// `/settings/issueTomcatSslCertificate`. They are refused with an explanation
// rather than left to fail as an unknown command, because "usage, exit 2" tells
// a caller they typed something wrong when in fact the endpoint is gone.
//
// `/preview/computers` is deliberately absent. Its resource was suppressed by
// pro.go long before this change, and `computers` is the curated alias for
// `computers-inventory` — the most-used command in the CLI. A stub of that name
// would shadow it, because applyDeprecatedNames runs before applyAliases and a
// real command beats an alias in cobra.
var withdrawnNames = map[string]string{
	"remote-administration-configurations": "the bare `/preview/remote-administration-configurations` collection is no longer ingested; the family under it ships as `pro team-viewer-remote-administration`",
	"servers":                              "`/settings/issueTomcatSslCertificate` was an unversioned legacy endpoint with no replacement in the versioned API",
	// Not a withdrawal but a handwritten replacement, which lands here for the
	// same reason: pro.go removes the generated resource outright, so there is
	// no command to alias to. The handwritten one targets by --serial/--name/
	// --group and confirms, where the generated one took an <id>.
	"redeploy-jamf-management-frameworks": "the redeploy action moved to `pro computer-inventory redeploy-framework` (aliased `comp`), which targets by serial, name or group",
}

// applyDeprecatedNames wires the old names onto the tree: a rename or merge
// becomes a cobra alias on its replacement, and a withdrawal becomes a command
// that explains itself.
//
// Called after every subcommand is registered, because it resolves each
// replacement by name and a missing one has to be a visible no-op rather than a
// silent one — TestDeprecatedNamesPointAtCommandsThatShip is the guard.
func applyDeprecatedNames(pro *cobra.Command) {
	byName := map[string]*cobra.Command{}
	for _, sub := range pro.Commands() {
		byName[sub.Name()] = sub
	}
	for old, dep := range deprecatedNames {
		target, ok := byName[dep.Now]
		if !ok {
			continue
		}
		// A name that is already the replacement's own alias needs nothing, and
		// adding it twice makes cobra ambiguous.
		if target.Name() == old || slicesContains(target.Aliases, old) {
			continue
		}
		target.Aliases = append(target.Aliases, old)
	}
	for old, why := range withdrawnNames {
		if _, taken := byName[old]; taken {
			continue
		}
		pro.AddCommand(newWithdrawnNameCmd(old, why))
	}
}

// newWithdrawnNameCmd is a stub that refuses a withdrawn resource name and says
// why, in place of cobra's "unknown command".
func newWithdrawnNameCmd(name, why string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     name,
		Short:   fmt.Sprintf("Removed — %s", firstClause(why)),
		Hidden:  true,
		GroupID: groupCore,
		Annotations: map[string]string{
			noAuthAnnotation: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("`pro %s` no longer exists: %s", name, why)
		},
	}
	return cmd
}

// firstClause trims an explanation to its first clause, for a one-line Short.
func firstClause(s string) string {
	if i := strings.Index(s, ";"); i > 0 {
		return s[:i]
	}
	return s
}

// warnIfDeprecatedName prints a deprecation warning when the invocation named a
// resource by one of the retired names.
//
// It reads the resource token out of argv rather than asking cobra, and that is
// forced rather than chosen. The alias sits on the *resource*, so `pro icons
// get 1` resolves `icons` to the `icon` command and then `get` beneath it — but
// cobra's CalledAs() returns "" for anything that is not the executed leaf. It
// records the matched name on every command it traverses and then flips
// `called` to true only on the final one, so a parent's alias is unreadable
// through the public API.
//
// The token immediately after the product name is the resource, so the lookup
// is exact for every ordinary invocation. A flag interleaved between the two
// (`pro --output json icons get`) yields the flag's value instead, which misses
// the warning rather than inventing one — the failure that costs least.
func warnIfDeprecatedName(cmd *cobra.Command) {
	product := productToken(cmd)
	if product == "" {
		return
	}
	called := resourceTokenAfter(os.Args, product)
	dep, ok := deprecatedNames[called]
	if !ok {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: `%s` is a deprecated name for `%s` and stops working after %s. Use `%s %s`.\n",
		called, dep.Now, deprecatedNamesRemovedAfter, product, dep.Now)
}

// productToken returns the name of the executed command's top-level namespace —
// the ancestor that is a direct child of the root — or "" when the command is
// the root or one of its own children.
func productToken(cmd *cobra.Command) string {
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		if c.Parent().Parent() == nil {
			return c.Name()
		}
	}
	return ""
}

// resourceTokenAfter returns the first non-flag argument following product.
func resourceTokenAfter(args []string, product string) string {
	for i, a := range args {
		if a != product {
			continue
		}
		for _, next := range args[i+1:] {
			if strings.HasPrefix(next, "-") {
				continue
			}
			return next
		}
		return ""
	}
	return ""
}

// deprecatedNamesExpired reports whether the aliases are past their removal
// date, and which entries would go.
func deprecatedNamesExpired(now time.Time) (bool, []string) {
	deadline, err := time.Parse(time.DateOnly, deprecatedNamesRemovedAfter)
	if err != nil {
		// An unparseable date is a broken guard, so report it as expired rather
		// than letting the aliases live on a typo.
		return true, []string{fmt.Sprintf("deprecatedNamesRemovedAfter is not a date: %q", deprecatedNamesRemovedAfter)}
	}
	if !now.After(deadline) {
		return false, nil
	}
	names := make([]string, 0, len(deprecatedNames)+len(withdrawnNames))
	for old := range deprecatedNames {
		names = append(names, old)
	}
	for old := range withdrawnNames {
		names = append(names, old)
	}
	return true, names
}

// slicesContains is a local helper so this file needs no extra import.
func slicesContains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
