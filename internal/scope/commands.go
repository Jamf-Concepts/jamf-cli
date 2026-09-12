// Copyright 2026, Jamf Software LLC

package scope

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// NewScopeCmd creates the "scope" subcommand group with get, add, and remove
// subcommands for the given Classic API resource.
//
// Each leaf takes `[<id>]` plus `--name`, matching every other command in this
// CLI. It used to take the name as a bare positional — the only place in the
// binary that did — which meant a caller who had an ID in hand from `list` had
// to go and find the name for it, and a name that looks like an ID could not
// be addressed at all.
func NewScopeCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "View and modify scope",
		Long: fmt.Sprintf(`View and modify the scope of %s.

Scope sections: %s.
Categories this resource accepts: %s.`,
			describe(res), humanList(SectionsFor(res.SingularKey)), flagList(ScopeFlagsFor(res.SingularKey))),
	}

	cmd.AddCommand(newScopeGetCmd(ctx, res))
	cmd.AddCommand(newScopeAddCmd(ctx, res))
	cmd.AddCommand(newScopeRemoveCmd(ctx, res))

	return cmd
}

// describe names the resource for help text, preferring the shape label so the
// text says what kind of scope the resource has rather than repeating its key.
func describe(res Resource) string {
	if sh, ok := shapes[res.SingularKey]; ok {
		return sh.label
	}
	return strings.ReplaceAll(res.SingularKey, "_", " ")
}

func outputFormat(cmd *cobra.Command) string {
	if f := cmd.Flag("output"); f != nil {
		return f.Value.String()
	}
	return "json"
}

func newScopeGetCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "get [<id>]",
		Short: "Display the current scope",
		Long: fmt.Sprintf(`Display the current scope of %s.

Every category the resource carries is listed, including the ones only the
admin UI used to show: iBeacon limitations and exclusions, and target classes.`,
			describe(res)),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := NewRef(args, flagName)
			if err != nil {
				return err
			}
			_, s, err := FetchScope(cmd.Context(), ctx.Client, res, ref)
			if err != nil {
				return err
			}
			return OutputScope(ctx.Output, s, outputFormat(cmd))
		},
	}

	addNameFlag(cmd, res, &flagName)
	return cmd
}

func newScopeAddCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	var section, flagName string

	cmd := &cobra.Command{
		Use:     "add [<id>]",
		Short:   "Add an item to the scope",
		Long:    mutateLong(res, "Add an item to"),
		Example: scopeExample(res, "add"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := NewRef(args, flagName)
			if err != nil {
				return err
			}
			target, err := DetermineScopeTarget(cmd, res)
			if err != nil {
				return err
			}
			if err := ValidateScopeCombination(res.SingularKey, section, target.FlagName); err != nil {
				return err
			}

			id, s, err := FetchScope(cmd.Context(), ctx.Client, res, ref)
			if err != nil {
				return err
			}

			if err := CheckAllFlagConflict(s, res.SingularKey, section, target.FlagName); err != nil {
				return err
			}

			if !AddToScope(s, section, target.FlagName, target.Name) {
				fmt.Fprintf(os.Stderr, "%s %q already in %s scope of %s\n",
					target.FlagName, target.Name, section, ref)
				return nil
			}

			if err := PutScope(cmd.Context(), ctx.Client, res, id, s); err != nil {
				return err
			}

			// Verify against the ID the fetch resolved, not the caller's
			// reference: a name lookup that was ambiguous or that the server
			// resolved differently would otherwise be re-run and could read a
			// different object than the one just written.
			if err := verifyWritten(cmd, ctx, res, id, section, target, s, true); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Added %s %q to %s scope of %s\n",
				target.FlagName, target.Name, section, ref)
			return OutputScope(ctx.Output, s, outputFormat(cmd))
		},
	}

	addNameFlag(cmd, res, &flagName)
	AddScopeFlags(cmd, res, &section)
	return cmd
}

func newScopeRemoveCmd(ctx *registry.CLIContext, res Resource) *cobra.Command {
	var section, flagName string

	cmd := &cobra.Command{
		Use:     "remove [<id>]",
		Short:   "Remove an item from the scope",
		Long:    mutateLong(res, "Remove an item from"),
		Example: scopeExample(res, "remove"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := NewRef(args, flagName)
			if err != nil {
				return err
			}
			target, err := DetermineScopeTarget(cmd, res)
			if err != nil {
				return err
			}
			if err := ValidateScopeCombination(res.SingularKey, section, target.FlagName); err != nil {
				return err
			}

			id, s, err := FetchScope(cmd.Context(), ctx.Client, res, ref)
			if err != nil {
				return err
			}

			if !RemoveFromScope(s, section, target.FlagName, target.Name) {
				fmt.Fprintf(os.Stderr, "%s %q not found in %s scope of %s\n",
					target.FlagName, target.Name, section, ref)
				return nil
			}

			if err := PutScope(cmd.Context(), ctx.Client, res, id, s); err != nil {
				return err
			}

			if err := verifyWritten(cmd, ctx, res, id, section, target, s, false); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Removed %s %q from %s scope of %s\n",
				target.FlagName, target.Name, section, ref)
			return OutputScope(ctx.Output, s, outputFormat(cmd))
		},
	}

	addNameFlag(cmd, res, &flagName)
	AddScopeFlags(cmd, res, &section)
	return cmd
}

// verifyWritten re-reads the scope and confirms the write landed, unless this
// is a dry run.
//
// The dry-run skip is not cosmetic: `dryRunClient` suppresses the PUT and
// returns success, so the verification read then finds the scope unchanged and
// reports "the server accepted the write but did not persist …" — a dry run
// failing with a message describing a server fault that did not happen, at
// exit 1. `-n` on a scope command previewed the request and then always
// errored.
func verifyWritten(cmd *cobra.Command, ctx *registry.CLIContext, res Resource, id, section string, target ScopeTarget, sent *ScopeXML, expectPresent bool) error {
	if ctx.DryRun {
		return nil
	}
	return VerifyScopeWrite(cmd.Context(), ctx.Client, res, id, sent, target, section, expectPresent)
}

// mutateLong renders the shared body of add/remove help, naming the sections
// and categories THIS resource accepts rather than the union across all eight.
func mutateLong(res Resource, verb string) string {
	sections := SectionsFor(res.SingularKey)
	return fmt.Sprintf(`%s the scope of %s.

Sections (--section): %s. Default: %s.
Categories: %s.

Only the <scope> element is sent, so nothing else about the object is
rewritten. The scope itself is replaced whole, which is why the current scope
is read first.`,
		verb, describe(res), humanList(sections), sections[0], flagList(ScopeFlagsFor(res.SingularKey)))
}

// scopeExample renders examples using categories the resource really has, so
// --help never demonstrates a flag the command would refuse, and as whole
// invocations so they can be pasted.
func scopeExample(res Resource, verb string) string {
	// CLIName is stamped by the generator on every shipped scope command; the
	// fallback keeps a hand-constructed Resource (a test, or a future
	// hand-written caller) from rendering "jamf-cli pro  scope add".
	name := res.CLIName
	if name == "" {
		name = "<resource>"
	}
	prefix := "jamf-cli pro " + name + " scope " + verb
	targets, _ := sectionFlags(res.SingularKey, SectionTarget)
	target := exampleFlag(targets, 0)

	lines := []string{
		fmt.Sprintf("  # %s the target section, by ID", verbPhrase(verb, "a "+target+" to", "a "+target+" from")),
		fmt.Sprintf("  %s 1 --%s %q", prefix, target, "Example"),
	}
	if excl, _ := sectionFlags(res.SingularKey, SectionExclusion); len(excl) > 0 {
		lines = append(lines,
			"",
			"  # ...or by name, in the exclusions section",
			fmt.Sprintf("  %s --name %q --section %s --%s %q", prefix, "My Object", SectionExclusion, exampleFlag(excl, 0), "Example"),
		)
	}
	return strings.Join(lines, "\n")
}

// verbPhrase renders the example's comment for add or remove without
// title-casing anything — the two verbs take different prepositions.
func verbPhrase(verb, addPhrase, removePhrase string) string {
	if verb == "remove" {
		return "Remove " + removePhrase
	}
	return "Add " + addPhrase
}

// exampleFlag picks a flag for the example text, falling back to a category
// every shape has so an unmapped resource still renders something sensible.
func exampleFlag(flags []string, i int) string {
	if i < len(flags) {
		return flags[i]
	}
	return flagBuilding
}

// addNameFlag registers --name with resource-specific help.
func addNameFlag(cmd *cobra.Command, res Resource, target *string) {
	cmd.Flags().StringVar(target, "name", "", fmt.Sprintf("Look up %s by name", strings.ReplaceAll(res.SingularKey, "_", " ")))
}

// DetermineScopeTarget inspects the command's flags to find exactly one scope
// item flag that was set. Returns an error if zero or multiple flags are set.
//
// It walks the resource's own flag set rather than the global one, so an
// unset-but-registered flag from another resource's shape cannot be reported
// and the "specify one of" list names only what this command has.
func DetermineScopeTarget(cmd *cobra.Command, res Resource) (ScopeTarget, error) {
	available := ScopeFlagsFor(res.SingularKey)
	var found []ScopeTarget
	for _, flag := range available {
		if v, err := cmd.Flags().GetString(flag); err == nil && v != "" {
			found = append(found, ScopeTarget{FlagName: flag, Name: v})
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return ScopeTarget{}, fmt.Errorf("specify one of: %s", flagList(available))
	}
	names := make([]string, len(found))
	for i, f := range found {
		names[i] = "--" + f.FlagName
	}
	return ScopeTarget{}, fmt.Errorf("specify only one scope category per invocation; got %s", humanList(names))
}

// AnnotationCategories carries a scope command's own item-flag vocabulary, so
// the root's unknown-flag handler can name the categories THIS resource has.
//
// Registering only the resource's own flags is what makes --help and shell
// completion honest, but it costs the explanatory refusal: cobra rejects an
// unregistered flag as "unknown flag: --computer-group" before RunE and the
// matrix never sees it. That is accurate and unhelpful on exactly the mistake
// the matrix exists to explain — a category that belongs to another device
// family — so the vocabulary travels on an annotation rather than being
// re-derived in root.go, which has no access to the resource.
const AnnotationCategories = "jamf:scope-categories"

// AddScopeFlags registers --section and the item flags this resource accepts.
//
// Registering only the resource's own categories is what makes `--help` and
// shell completion honest: a mobile configuration profile no longer offers
// --computer-group, and restricted software no longer offers a limitations
// section it has no tab for.
func AddScopeFlags(cmd *cobra.Command, res Resource, section *string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[AnnotationCategories] = flagList(ScopeFlagsFor(res.SingularKey))

	sections := SectionsFor(res.SingularKey)
	cmd.Flags().StringVar(section, "section", sections[0],
		fmt.Sprintf("scope section: %s", strings.Join(sections, ", ")))
	_ = cmd.RegisterFlagCompletionFunc("section", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return sections, cobra.ShellCompDirectiveNoFileComp
	})

	for _, flag := range ScopeFlagsFor(res.SingularKey) {
		cmd.Flags().String(flag, "", scopeFlagHelp(res.SingularKey, flag))
	}
}

// scopeFlagHelp describes one item flag, naming the sections of THIS resource
// that accept it — the fact a caller most often gets wrong, and the one a
// global description cannot carry (--user-group is a limitation and exclusion
// category everywhere, and the only limitation a VPP assignment has).
func scopeFlagHelp(singularKey, flag string) string {
	var in []string
	for _, section := range Sections {
		if allowed, ok := sectionFlags(singularKey, section); ok && contains(allowed, flag) {
			in = append(in, section)
		}
	}
	where := ""
	if len(in) > 0 && len(in) < len(Sections) {
		where = fmt.Sprintf(" (%s only)", humanList(in))
	}
	return scopeFlagNoun[flag] + where
}

// scopeFlagNoun describes what each flag's value identifies. The device and
// directory entries carry the detail a caller cannot guess: which flags accept
// an ID or UDID as well as a name, and which two are free text resolved
// against the directory rather than Jamf Pro object names.
var scopeFlagNoun = map[string]string{
	flagComputer:          "individual computer (id, name, or UDID)",
	flagComputerGroup:     "computer group name",
	flagMobileDevice:      "individual mobile device (id, name, or UDID)",
	flagMobileDeviceGroup: "mobile device group name",
	flagBuilding:          "building name",
	flagDepartment:        "department name",
	flagNetworkSegment:    "network segment name",
	flagUser:              "directory or local username (free text, not a Jamf Pro user)",
	flagUserGroup:         "directory (LDAP/IdP) user group name",
	flagJSSUserGroup:      "Jamf Pro user group name",
	flagJSSUser:           "Jamf Pro user name",
	flagIBeacon:           "iBeacon name",
	flagClass:             "class name",
}
