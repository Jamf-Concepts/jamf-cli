// Copyright 2026, Jamf Software LLC

package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/exitcode"
)

// The invocations a resource alias cannot cover, because the operation's name
// moved as well as the resource's.
//
// deprecatedNames resolves the first token, so `pro schedulers triggers` reaches
// the `scheduler` command and cobra then reports `unknown command "triggers"`
// and exits 2 — the same answer a typo gets, on the class that is hardest to
// self-diagnose, because the resource name is right and only the verb moved.
// The CHANGELOG has the table; the CHANGELOG is not read at runtime.
//
// Governed by deprecatedNamesRemovedAfter along with deprecatedNames: one change
// created both, so one date retires both.

// movedInvocation records where an invocation's operations went.
type movedInvocation struct {
	// Now is every command that serves what the old invocation served, as a
	// command path beneath `pro`.
	//
	// A slice rather than one string because two old invocations can collapse
	// onto one name: `pro access-managements list` and
	// `pro enrollment-settings list` both resolve to `pro enrollment list`,
	// and their endpoints went to different places. Naming one of the two
	// would send half the callers somewhere wrong, so the refusal names both
	// and lets the reader pick.
	Now []string
}

// movedInvocations maps a `pro` invocation that no longer exists onto the
// command that replaced it, keyed on the command path **after** resource-alias
// resolution — `csa delete` rather than `csas delete`.
//
// Keyed post-resolution deliberately. It is the path cobra actually looks up,
// so one entry covers every spelling of the resource that reaches it (`pro csas
// delete` and `pro csa delete` alike), and a collision between two old
// invocations is visible in the table rather than discovered at runtime.
//
// The endpoint is unchanged for every entry — only the command name moved — so
// each is a pure redirect. TestMovedInvocationsNameCommandsThatShip is what
// keeps that true: it fails when a key names a command that exists (the entry
// would shadow it) or a replacement names one that does not.
var movedInvocations = map[string]movedInvocation{
	"activation-code patch":                             {Now: []string{"activation-code organization-name patch"}},                       // PATCH /v1/activation-code/organization-name
	"api-role-privileges api-role-privileges":           {Now: []string{"api-role-privileges list"}},                                      // GET /v1/api-role-privileges
	"app-request create":                                {Now: []string{"app-request-form-input-fields create"}},                          // POST /v1/app-request/form-input-fields
	"app-request delete":                                {Now: []string{"app-request-form-input-fields delete"}},                          // DELETE /v1/app-request/form-input-fields/{id}
	"app-request list":                                  {Now: []string{"app-request-form-input-fields list"}},                            // GET /v1/app-request/form-input-fields
	"app-request settings":                              {Now: []string{"app-request get"}},                                               // GET /v1/app-request/settings
	"app-request update-settings":                       {Now: []string{"app-request update"}},                                            // PUT /v1/app-request/settings
	"cloud-distribution-point cloud-distribution-point": {Now: []string{"cloud-distribution-point list"}},                                 // GET /v1/cloud-distribution-point
	"computer-inventory-collection-settings create":     {Now: []string{"computer-inventory-collection-settings-custom-path create"}},     // POST /v2/computer-inventory-collection-settings/custom-path
	"computer-inventory-collection-settings delete":     {Now: []string{"computer-inventory-collection-settings-custom-path delete"}},     // DELETE /v2/computer-inventory-collection-settings/custom-path/{id}
	"csa delete":                                        {Now: []string{"csa token delete"}},                                              // DELETE /v1/csa/token
	"enrollment create":                                 {Now: []string{"enrollment-access-groups create"}},                               // POST /v3/enrollment/access-groups
	"enrollment delete":                                 {Now: []string{"enrollment-access-groups delete"}},                               // DELETE /v3/enrollment/access-groups/{id}
	"enrollment enrollment":                             {Now: []string{"enrollment get"}},                                                // GET /v4/enrollment
	"enrollment list":                                   {Now: []string{"enrollment access-management", "enrollment-access-groups list"}}, // GET /v4/enrollment/access-management, GET /v3/enrollment/access-groups
	"enrollment update-enrollment":                      {Now: []string{"enrollment update"}},                                             // PUT /v4/enrollment
	"enrollment-languages filtered-language-codes":      {Now: []string{"enrollment filtered-language-codes"}},                            // GET /v3/enrollment/filtered-language-codes
	"enrollment-languages language-codes":               {Now: []string{"enrollment language-codes"}},                                     // GET /v3/enrollment/language-codes
	"health-check health-check":                         {Now: []string{"health-check list"}},                                             // GET /v1/health-check
	"jamf-cloud-distribution-service delete":            {Now: []string{"jamf-cloud-distribution-service-files delete"}},                  // DELETE /v1/jcds/files/{fileName}
	"jamf-cloud-distribution-service files":             {Now: []string{"jamf-cloud-distribution-service-files create"}},                  // POST /v1/jcds/files
	"jamf-cloud-distribution-service get":               {Now: []string{"jamf-cloud-distribution-service-files get"}},                     // GET /v1/jcds/files/{fileName}
	"jamf-cloud-distribution-service list":              {Now: []string{"jamf-cloud-distribution-service-files list"}},                    // GET /v1/jcds/files
	"jamf-connect jamf-connect":                         {Now: []string{"jamf-connect list"}},                                             // GET /v1/jamf-connect
	"jamf-connect update":                               {Now: []string{"jamf-connect-config-profiles update"}},                           // PUT /v1/jamf-connect/config-profiles/{id}
	"local-admin-password update":                       {Now: []string{"local-admin-password settings update"}},                          // PUT /v2/local-admin-password/settings
	"log-flushing delete":                               {Now: []string{"log-flushing-task delete"}},                                      // DELETE /v1/log-flushing/task/{id}
	"log-flushing get":                                  {Now: []string{"log-flushing-task get"}},                                         // GET /v1/log-flushing/task/{id}
	"log-flushing log-flushing":                         {Now: []string{"log-flushing list"}},                                             // GET /v1/log-flushing
	"log-flushing task":                                 {Now: []string{"log-flushing-task create"}},                                      // POST /v1/log-flushing/task
	"managed-software-updates-plans abandon":            {Now: []string{"managed-software-updates-plans feature-toggle abandon"}},         // POST /v1/managed-software-updates/plans/feature-toggle/abandon
	"managed-software-updates-plans status":             {Now: []string{"managed-software-updates-plans feature-toggle status"}},          // GET /v1/managed-software-updates/plans/feature-toggle/status
	"managed-software-updates-plans update":             {Now: []string{"managed-software-updates-plans feature-toggle update"}},          // PUT /v1/managed-software-updates/plans/feature-toggle
	"mdm-renewal patch":                                 {Now: []string{"mdm-renewal-device-common-details patch"}},                       // PATCH /v1/mdm-renewal/device-common-details
	"policy-properties policy-properties":               {Now: []string{"policy-properties get"}},                                         // GET /v1/policy-properties
	"policy-properties update-policy-properties":        {Now: []string{"policy-properties update"}},                                      // PUT /v1/policy-properties
	"scheduler summary":                                 {Now: []string{"scheduler list"}},                                                // GET /v1/scheduler/summary
	"scheduler triggers":                                {Now: []string{"scheduler-jobs triggers"}},                                       // GET /v1/scheduler/jobs/{jobKey}/triggers
	"self-service-plus get":                             {Now: []string{"self-service-plus settings get"}},                                // GET /v1/self-service-plus/settings
	"self-service-plus update":                          {Now: []string{"self-service-plus settings update"}},                             // PUT /v1/self-service-plus/settings
	"sso-settings cert cert":                            {Now: []string{"sso-settings cert create"}},                                      // POST /v2/sso/cert
	"sso-settings list":                                 {Now: []string{"sso-settings failover"}},                                         // GET /v1/sso/failover
}

// formerLeafGroups are the three commands that returned data before the
// sub-resource split and are command *groups* after it.
//
// The sharpest edge of the rename, because the path still resolves and still
// exits 0: a job that ran `pro csas token -o json | jq -r .value` now pipes
// cobra's help text into jq, and the failure surfaces in jq with nothing
// pointing back at the CLI. Every other breaking class in this change has a
// runtime answer — a renamed resource warns and resolves, a withdrawn one
// refuses and explains — and this one had only a CHANGELOG row.
//
// Refused only when the invocation asked for data: a structured output format,
// `--field`, `--select` or `--out-file`. A bare `pro csa token` still prints
// help and exits 0, which is this CLI's convention for every group parent and
// is the right answer for a human exploring the tree. Keyed the same way as
// movedInvocations, on the resolved path.
//
// Governed by deprecatedNamesRemovedAfter: the group is permanent, but pointing
// at the leaf is migration help and retires with the aliases.
var formerLeafGroups = map[string]string{
	"csa token":                     "csa token get",
	"local-admin-password settings": "local-admin-password settings get",
	"managed-software-updates-plans feature-toggle": "managed-software-updates-plans feature-toggle get",
}

// applyMovedInvocations registers each moved invocation as a hidden command that
// refuses and names its replacement, in place of cobra's "unknown command".
//
// Called from applyDeprecatedNames, after every subcommand is registered,
// because it resolves each key against the assembled tree — the same reason and
// the same guard shape as the tables beside it.
func applyMovedInvocations(pro *cobra.Command) {
	for _, key := range sortedKeysOfMoved() {
		for _, path := range movedKeySpellings(key) {
			parent, leaf, ok := resolveMovedParent(pro, path)
			if !ok {
				// A path whose parent no longer resolves: the resource moved or
				// stopped existing. A visible no-op rather than a silent one —
				// TestMovedInvocationsNameCommandsThatShip is the guard.
				continue
			}
			if childIsTaken(parent, leaf) {
				// Registering a second command under a live name makes cobra
				// resolve by declaration order, which is not a choice this
				// table gets to make. The same test refuses such an entry
				// outright.
				continue
			}
			parent.AddCommand(newMovedInvocationCmd(key, leaf, movedInvocations[key].Now))
		}
	}
}

// movedKeySpellings returns every command path that has to carry a key's stub:
// the canonical one, plus one per nestedAliases entry whose path prefixes it.
//
// A nested alias is a *second instance* of the sub-resource, built from the
// generated constructor rather than being the same command under two parents —
// so a stub added to the canonical subtree is not on the alias's copy.
// `pro sso-settings-cert cert` is the one live case: it resolved and returned
// data before the split, and without this it gets cobra's "unknown command" on
// the very spelling the alias exists to keep working. TestMovedInvocations-
// ReachTheNestedAliasSpellings pins it, and fails if the overlap disappears
// rather than passing vacuously.
func movedKeySpellings(key string) []string {
	out := []string{key}
	for old, na := range nestedAliases {
		if rest, ok := cutPathPrefix(key, na.Path); ok {
			out = append(out, old+" "+rest)
		}
	}
	sort.Strings(out)
	return out
}

// cutPathPrefix reports whether path begins with prefix on a token boundary,
// returning the remainder.
func cutPathPrefix(path, prefix string) (string, bool) {
	if rest, ok := strings.CutPrefix(path, prefix+" "); ok && rest != "" {
		return rest, true
	}
	return "", false
}

// resolveMovedParent walks a key's path and returns the command its last token
// should hang off, plus that token.
func resolveMovedParent(pro *cobra.Command, key string) (*cobra.Command, string, bool) {
	tokens := strings.Fields(key)
	if len(tokens) < 2 {
		return nil, "", false
	}
	cur := pro
	for _, tok := range tokens[:len(tokens)-1] {
		next := childNamed(cur, tok)
		if next == nil {
			return nil, "", false
		}
		cur = next
	}
	return cur, tokens[len(tokens)-1], true
}

// childNamed returns the child of cmd answering to name, as its own name or as
// an alias.
func childNamed(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name || slicesContains(sub.Aliases, name) {
			return sub
		}
	}
	return nil
}

// childIsTaken reports whether cmd already has a child answering to name.
func childIsTaken(cmd *cobra.Command, name string) bool {
	return childNamed(cmd, name) != nil
}

// newMovedInvocationCmd is a stub that refuses a moved invocation and names
// where its operations went.
//
// Exit 2, the same code every other "this command does not exist" answers.
// Deliberately not a code of its own: a wrapper that branches on the exit code
// would then see two codes for one class, and what the caller is missing here
// is not a classification but the pointer. The pointer is the message and the
// hint.
func newMovedInvocationCmd(key, leaf string, now []string) *cobra.Command {
	return &cobra.Command{
		Use:     leaf,
		Short:   fmt.Sprintf("Moved — use `pro %s`", now[0]),
		Hidden:  true,
		GroupID: "",
		Annotations: map[string]string{
			noAuthAnnotation: "true",
		},
		// The stub answers however it is invoked: a moved operation took
		// positionals and flags, and refusing the arity first would report a
		// wrong argument count for a command that does not exist.
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return &exitcode.Error{
				Code:    exitcode.Usage,
				Message: fmt.Sprintf("`pro %s` no longer exists: the operation moved when command names started coming from the spec", key),
				Hint:    movedHint(now),
			}
		},
	}
}

// movedHint names every command that serves what the old invocation served.
func movedHint(now []string) string {
	quoted := make([]string, 0, len(now))
	for _, n := range now {
		quoted = append(quoted, "`jamf-cli pro "+n+"`")
	}
	switch len(quoted) {
	case 1:
		return "the endpoint is unchanged; run " + quoted[0]
	case 2:
		return "two commands share the old name: " + quoted[0] + " and " + quoted[1]
	default:
		return "the endpoint is unchanged; run one of " + strings.Join(quoted, ", ")
	}
}

// sortedKeysOfMoved keeps registration order deterministic, so two entries
// claiming one name resolve the same way on every run.
func sortedKeysOfMoved() []string {
	out := make([]string, 0, len(movedInvocations))
	for key := range movedInvocations {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// guardFormerLeafGroups refuses a structured-output request against one of the
// three commands that returned data before the sub-resource split and are
// groups after it.
//
// It wraps the RunE guardUnknownSubcommands installed rather than replacing it,
// and so has to run after that walk: a group parent this made runnable first
// would be skipped there, losing the "did you mean" refusal for a typo beneath
// it. Bare and typo invocations are passed straight through.
//
// Takes the root rather than `pro`, because that walk runs from the root in
// NewRootCmd and there is no `pro` in hand there. A tree with no `pro` is a
// broken build rather than a case to handle, so it resolves one and returns —
// TestFormerLeafGroupsAreStillGroups fails if the three stop resolving.
func guardFormerLeafGroups(root *cobra.Command) {
	pro := childNamed(root, "pro")
	if pro == nil {
		return
	}
	for key, leaf := range formerLeafGroups {
		parent, name, ok := resolveMovedParent(pro, key)
		if !ok {
			continue
		}
		group := childNamed(parent, name)
		if group == nil || !group.HasSubCommands() {
			// Not a group any more, so there is nothing to refuse —
			// TestFormerLeafGroupsAreStillGroups is the guard.
			continue
		}
		inner := group.RunE
		wanted := leaf
		path := key
		group.RunE = func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && structuredOutputRequested(cmd) {
				return &exitcode.Error{
					Code: exitcode.Usage,
					Message: fmt.Sprintf(
						"`pro %s` is a command group and returns no data; it returned data before the sub-resource split", path),
					Hint: "run `jamf-cli pro " + wanted + "`",
				}
			}
			if inner == nil {
				return cmd.Help()
			}
			return inner(cmd, args)
		}
	}
}

// structuredOutputRequested reports whether the invocation asked for data
// rather than for help.
//
// Every output flag counts, `--output table` included, and none is read by
// value. A group renders nothing whatever format is named, so narrowing to
// json/yaml/csv would draw a line the failure does not have — a caller piping
// `-o table` into `awk` is in exactly the position the refusal exists for.
//
// Changed rather than value is what keeps a bare invocation working:
// PersistentPreRunE resolves an unset `--output` from the profile's
// default-output and a TTY check, so reading outputFmt would make every bare
// `pro csa token` a refusal for anyone whose config sets `default-output`.
func structuredOutputRequested(cmd *cobra.Command) bool {
	// Read from the root's persistent set, which is where all four are
	// declared, rather than from cmd.Flags(). Cobra merges the inherited set
	// into a command's own by reference during ParseFlags, so both answer the
	// same at runtime — but only the root's set answers before a parse, and a
	// guard that depends on having been parsed cannot be asserted directly.
	flags := cmd.Root().PersistentFlags()
	for _, name := range []string{"output", "field", "select", "out-file"} {
		if flags.Changed(name) {
			return true
		}
	}
	return false
}
