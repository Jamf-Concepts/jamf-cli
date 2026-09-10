// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Jamf-Concepts/jamf-cli/internal/commands/pro/generated"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
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
// endpoints were redistributed: `enrollment-languages` and
// `computer-inventory-collection-settings` each kept their name for one half of
// a split, so they resolve without help and an alias for them would make cobra
// ambiguous. `jcds` gets no entry for the neighbouring reason — it is a
// *curated* alias in commandAliases, and so permanent rather than expiring;
// naming it here too would append the alias twice, once from each table.
//
// Generated from a diff of the resource sets either side of the change, then
// reviewed. TestDeprecatedNamesPointAtCommandsThatShip is what keeps it honest.
var deprecatedNames = map[string]deprecatedName{
	"access-managements":                     {Now: "enrollment"},
	"account-preferences":                    {Now: "jamf-pro-account-preferences"},
	"activation-codes":                       {Now: "activation-code"},
	"api-roles-privileges":                   {Now: "api-role-privileges"},
	"app-installer-deployments":              {Now: "app-installers-deployments"},
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
	"patch-policy-logs":                      {Now: "patch-policies"},
	"patch-titles":                           {Now: "patch-management"},
	"reenrollment":                           {Now: "re-enrollment"},
	"remove-computer-mdm-profiles":           {Now: "computer-inventory"},
	"remove-mobile-device-mdm-profiles":      {Now: "mobile-devices"},
	"renew-mdm-profiles":                     {Now: "mdm"},
	"return-to-service-configurations":       {Now: "return-to-service"},
	"schedulers":                             {Now: "scheduler"}, // split: the other half keeps its own name
	"self-service-branding-images":           {Now: "self-service"},
	"service-discovery":                      {Now: "service-discovery-enrollment"},
	"slasas":                                 {Now: "slasa"},
	"sso-failovers":                          {Now: "sso-settings"},
	"static-computer-groups":                 {Now: "computer-groups-static-groups"},
	"systems":                                {Now: "jamf-pro-initialization"},
	"teacher-settings":                       {Now: "teacher-app"},
	"team-viewer-remote-administrations":     {Now: "team-viewer-remote-administration"},
	"user-accounts":                          {Now: "accounts"},
	"user-preferences":                       {Now: "jamf-pro-user-account-settings"},
	"user-smart-groups":                      {Now: "users"},
	"venafis":                                {Now: "venafi"},
	"vpp-locations":                          {Now: "volume-purchasing-locations"},
	"vpp-subscriptions":                      {Now: "volume-purchasing-subscriptions"},
}

// nestedAlias records a retired resource name whose endpoints are now a nested
// sub-resource, so its replacement is two tokens rather than one.
type nestedAlias struct {
	// Path is the command path beneath `pro`, as NestedResourceCommands keys it.
	Path string
}

// nestedAliases maps a retired `pro` resource name onto a nested sub-resource.
//
// These four are separate from deprecatedNames because a cobra alias is a name
// on one command, so it can only ever resolve to a direct child of `pro`, and
// the endpoints these names covered now sit one level deeper. Pointing them at
// the parent instead is not merely imprecise. Three of the four would answer
// the wrong endpoint — `pro sso-settings-cert get` would read the SSO
// configuration rather than its certificate — and the fourth,
// `self-service-settings`, resolved *correctly* to `pro self-service get`
// before the nesting and would answer `unknown command "get"` after it, which
// is a working alias regressing rather than an imprecise one.
//
// All four are exact: each was one spec file in the 165-file layout, every one
// of its operations moved into the sub-resource, and none stayed behind on the
// parent. That is not a coincidence — it is the same partition arriving from
// the other direction, and it is the strongest evidence the sub-resource rule
// picks the right boundary.
//
// Governed by deprecatedNamesRemovedAfter along with deprecatedNames: one
// change created both, so one date retires both.
var nestedAliases = map[string]nestedAlias{
	// GET/POST/PUT/DELETE /v2/sso/cert plus its download and parse actions.
	"sso-settings-cert": {Path: "sso-settings cert"},
	// GET/PUT /v1/app-installers/global-settings plus its history and
	// deployment-controls reads.
	"app-installer-global-settings": {Path: "app-installers global-settings"},
	// GET/PUT/history/add-history-note on /v1/self-service/settings.
	"self-service-settings": {Path: "self-service settings"},
	// GET/PUT /v1/adue-session-token-settings, grouped under the enrollment tag
	// and nowhere near /v4/enrollment.
	"account-driven-user-enrollment-session-token-settings": {Path: "enrollment adue-session-token-settings"},
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
func applyDeprecatedNames(pro *cobra.Command, ctx *registry.CLIContext) {
	byName := map[string]*cobra.Command{}
	for _, sub := range pro.Commands() {
		byName[sub.Name()] = sub
	}
	applyNestedAliases(pro, ctx)
	// After the nested aliases, because a moved invocation's key may name a
	// path that only exists once those are registered (`sso-settings cert
	// cert` reaches the certificate sub-resource), and before the resource
	// aliases, so a key resolves against the real command names.
	applyMovedInvocations(pro)
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
		if nameIsTaken(pro, old) {
			// Registering a second command under a live name makes cobra
			// resolve by declaration order, which is not a choice this table
			// gets to make. TestNestedAliasesDoNotShadowALiveName is the guard.
			continue
		}
		pro.AddCommand(newWithdrawnNameCmd(old, why))
	}
}

// applyNestedAliases registers a retired resource name as a hidden second
// instance of the nested subtree it now points at, so every subcommand under
// the old name keeps working.
//
// A second instance rather than the same command under two parents, because
// cobra's AddCommand reparents: registering one *cobra.Command twice would
// leave CommandPath, --help and usage describing whichever registration came
// last. Built from the generated constructor rather than a hand-written mirror,
// so the redirect cannot drift from what it redirects to — a mirror is a list
// to keep in step, and the whole reason these names exist is that nobody
// updates one.
//
// The instance takes the parent's help group. It is hidden, so it does not
// crowd `pro --help`, but a GroupID cobra's parent has not declared is an
// error, and an empty one files it under "Additional Commands" — visible in
// exactly the listing it should stay out of.
func applyNestedAliases(pro *cobra.Command, ctx *registry.CLIContext) {
	if ctx == nil {
		return
	}
	constructors := generated.NestedResourceCommands()
	for _, old := range sortedNestedAliasNames() {
		na := nestedAliases[old]
		make, ok := constructors[na.Path]
		if !ok {
			// A path that names no nested sub-resource: the sub-resource moved
			// or stopped being one. Left as a visible no-op rather than a
			// silent one — TestNestedAliasesPointAtCommandsThatShip is the
			// guard, for the same reason
			// TestDeprecatedNamesPointAtCommandsThatShip exists.
			continue
		}
		if nameIsTaken(pro, old) {
			// Registering a second command under a live name makes cobra
			// resolve by declaration order, which is not a choice this table
			// gets to make. TestNestedAliasesDoNotShadowALiveName is the guard.
			continue
		}
		sub := make(ctx)
		sub.Use = old
		sub.Hidden = true
		sub.Short = fmt.Sprintf("Deprecated — use `pro %s`", na.Path)
		sub.Long = fmt.Sprintf(
			"`pro %s` is a deprecated name for `pro %s` and stops working after %s.",
			old, na.Path, deprecatedNamesRemovedAfter)
		// From proGroupMap rather than from the parent command's GroupID,
		// because applyProGroups has not run yet: applyDeprecatedNames is
		// deliberately called before applyAliases, so every parent's GroupID is
		// still "" at this point and copying it left all four stubs ungrouped —
		// visible in `pro --help` under "Additional Commands", which is the one
		// listing a hidden compatibility stub must stay out of. The map is the
		// same source applyProGroups reads, so the two cannot disagree.
		sub.GroupID = proGroupMap[firstToken(na.Path)]
		pro.AddCommand(sub)
	}
}

// nameIsTaken reports whether any child of pro already answers to name, as its
// own name or as an alias.
func nameIsTaken(pro *cobra.Command, name string) bool {
	for _, sub := range pro.Commands() {
		if sub.Name() == name || slicesContains(sub.Aliases, name) {
			return true
		}
	}
	return false
}

// sortedNestedAliasNames keeps registration order deterministic, so two
// entries claiming one name resolve the same way on every run.
func sortedNestedAliasNames() []string {
	out := make([]string, 0, len(nestedAliases))
	for old := range nestedAliases {
		out = append(out, old)
	}
	sort.Strings(out)
	return out
}

// firstToken returns the first space-separated token of a command path.
func firstToken(path string) string {
	if i := strings.Index(path, " "); i > 0 {
		return path[:i]
	}
	return path
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
	called := resourceTokenAfter(os.Args, product, visibleFlags(cmd))
	now := ""
	if dep, ok := deprecatedNames[called]; ok {
		now = dep.Now
	} else if na, ok := nestedAliases[called]; ok {
		now = na.Path
	} else {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: `%s` is a deprecated name for `%s` and stops working after %s. Use `%s %s`.\n",
		called, now, deprecatedNamesRemovedAfter, product, now)
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

// resourceTokenAfter returns the resource token the caller typed — the first
// positional argument following product — with flags removed the way cobra
// removes them.
//
// The naive version of this treated any token not starting with "-" as the
// resource, which is only right when no flag sits between the product and the
// resource. Cobra does not require global flags before the subcommand and `-p`
// is the documented way to select a profile, so `pro -p ci-svc icons get 1` is
// an ordinary shape — and it answered "ci-svc". That silently switched off both
// consumers: the deprecation warning for all 100 retired names, which is the
// entire migration signal for the deprecation window, and the moved-verb
// refusal, which fell back to cobra's bare arity error.
//
// Re-deriving arity from spelling cannot be made correct, so this does not try:
// flags is the flag set visible where the token sits, and whether a flag
// consumes the next argument is asked of it. The rules are cobra's stripFlags,
// deliberately including its treatment of an *unknown* long flag as
// value-taking — the guard has to agree with cobra about where the resource
// token is, not about what a correct command line looks like.
func resourceTokenAfter(args []string, product string, flags *pflag.FlagSet) string {
	for i, a := range args {
		if a != product {
			continue
		}
		return firstPositional(args[i+1:], flags)
	}
	return ""
}

// firstPositional returns the first argument that is not a flag or a flag's
// value, or "" when there is none.
func firstPositional(args []string, flags *pflag.FlagSet) string {
	for len(args) > 0 {
		s := args[0]
		args = args[1:]
		switch {
		case s == "--":
			// Everything after the terminator is positional.
			if len(args) > 0 {
				return args[0]
			}
			return ""
		case strings.HasPrefix(s, "--") && !strings.Contains(s, "=") && !longTakesNoValue(s[2:], flags):
			// `--flag value`: the next argument belongs to the flag.
			if len(args) == 0 {
				return ""
			}
			args = args[1:]
		case strings.HasPrefix(s, "-") && !strings.HasPrefix(s, "--") && !strings.Contains(s, "=") && len(s) == 2 && !shortTakesNoValue(s[1:], flags):
			// `-f value`, the shape `-p ci-svc` takes. A longer run of
			// shorthands (`-vvv`) or one with its value attached (`-ojson`)
			// carries no separate value argument.
			if len(args) == 0 {
				return ""
			}
			args = args[1:]
		case s != "" && !strings.HasPrefix(s, "-"):
			return s
		}
	}
	return ""
}

// longTakesNoValue reports whether a long flag can appear without a following
// value — a boolean, or any flag with an optional value. An unrecognised flag
// answers false, matching cobra: it consumes the next argument.
func longTakesNoValue(name string, flags *pflag.FlagSet) bool {
	if flags == nil {
		return false
	}
	f := flags.Lookup(name)
	return f != nil && f.NoOptDefVal != ""
}

// shortTakesNoValue is longTakesNoValue for a single-letter shorthand.
func shortTakesNoValue(name string, flags *pflag.FlagSet) bool {
	if flags == nil {
		return false
	}
	f := flags.ShorthandLookup(name)
	return f != nil && f.NoOptDefVal != ""
}

// typedResourceToken is resourceTokenAfter for a resolved command, reading the
// flag set from the command itself so the caller cannot forget to pass one.
func typedResourceToken(cmd *cobra.Command) string {
	product := productToken(cmd)
	if product == "" {
		return ""
	}
	return resourceTokenAfter(os.Args, product, visibleFlags(cmd))
}

// visibleFlags is every flag that can legitimately appear on the command line
// that reached cmd: its own, and each ancestor's persistent set.
//
// Assembled rather than read off cmd.Flags(), which holds only the command's
// *own* flags until cobra's unexported mergePersistentFlags has run — so a root
// persistent flag such as --quiet or -n looked unrecognised, was treated as
// value-taking, and ate the resource token. That is the bug this replaced,
// reintroduced one layer down and no more visible.
func visibleFlags(cmd *cobra.Command) *pflag.FlagSet {
	fs := pflag.NewFlagSet("visible", pflag.ContinueOnError)
	for c := cmd; c != nil; c = c.Parent() {
		fs.AddFlagSet(c.PersistentFlags())
		fs.AddFlagSet(c.Flags())
	}
	return fs
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
	names := make([]string, 0, len(deprecatedNames)+len(nestedAliases)+len(withdrawnNames))
	for old := range deprecatedNames {
		names = append(names, old)
	}
	for old := range nestedAliases {
		names = append(names, old)
	}
	for old := range withdrawnNames {
		names = append(names, old)
	}
	return true, names
}

// deprecatedNamesNoticePeriod is how long before the removal date the scheduled
// build starts failing, to put the removal PR on someone's list while there is
// still time to write it.
//
// The date-based guard beside it is the right forcing function and it fires
// once, on whatever PR happens to run CI on or after the day — and the work it
// forces is a real PR, not a one-line deletion. There is no way to raise a
// warning from a Go test, so the advance signal is a *separate* failing test
// run only from the scheduled workflow: it never blocks a pull request, and it
// turns the weekly build red 60 days out.
const deprecatedNamesNoticePeriod = 60 * 24 * time.Hour

// deprecatedNamesExpiring reports whether the removal date is inside the notice
// period, and how long is left.
//
// Distinct from deprecatedNamesExpired, which answers "are they past due". This
// answers "is it time to start", and it is deliberately false once the date has
// passed — at that point the hard guard is the one with something to say.
func deprecatedNamesExpiring(now time.Time) (bool, time.Duration) {
	deadline, err := time.Parse(time.DateOnly, deprecatedNamesRemovedAfter)
	if err != nil {
		// An unparseable date is handled by deprecatedNamesExpired, which
		// reports it as expired outright. Saying nothing here avoids two
		// failures for one typo.
		return false, 0
	}
	left := deadline.Sub(now)
	if left <= 0 || left > deprecatedNamesNoticePeriod {
		return false, left
	}
	return true, left
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
