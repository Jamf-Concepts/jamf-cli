package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// ─── XML types ─────────────────────────────────────────────────────────────────
// These model the Classic API scope XML structure. Custom XML marshalers on the
// slice types handle the parent/child nesting (e.g. <computer_groups> wrapping
// multiple <computer_group> elements) that Go's built-in encoding cannot express
// with tags alone.

// namedItem is an item identified by name (and optionally ID) in scope XML.
type namedItem struct {
	ID   int    `xml:"id,omitempty" json:"id,omitempty"`
	Name string `xml:"name" json:"name"`
}

// scopeItemSlice is a list of namedItem elements under a single XML parent.
// The child element name (e.g. "computer_group") is learned during unmarshal
// and reused during marshal. For newly-created lists it falls back to the
// parent element name with trailing "s" stripped.
type scopeItemSlice struct {
	Items    []namedItem
	elemName string
}

func (s *scopeItemSlice) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			s.elemName = t.Name.Local
			var item namedItem
			if err := d.DecodeElement(&item, &t); err != nil {
				return err
			}
			s.Items = append(s.Items, item)
		case xml.EndElement:
			return nil
		}
	}
}

func (s scopeItemSlice) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	elemName := s.elemName
	if elemName == "" {
		elemName = strings.TrimSuffix(start.Name.Local, "s")
	}
	for _, item := range s.Items {
		if err := e.EncodeElement(item, xml.StartElement{Name: xml.Name{Local: elemName}}); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

func (s scopeItemSlice) MarshalJSON() ([]byte, error) {
	if s.Items == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(s.Items)
}

// scopeStringSlice is a list of plain string elements under a single XML parent.
// Used for policy limitation user groups (limit_to_users/user_groups), where
// items are bare strings rather than objects with name sub-elements.
type scopeStringSlice struct {
	Items    []string
	elemName string
}

func (s *scopeStringSlice) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			s.elemName = t.Name.Local
			var val string
			if err := d.DecodeElement(&val, &t); err != nil {
				return err
			}
			s.Items = append(s.Items, val)
		case xml.EndElement:
			return nil
		}
	}
}

func (s scopeStringSlice) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	elemName := s.elemName
	if elemName == "" {
		elemName = strings.TrimSuffix(start.Name.Local, "s")
	}
	for _, val := range s.Items {
		if err := e.EncodeElement(val, xml.StartElement{Name: xml.Name{Local: elemName}}); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

func (s scopeStringSlice) MarshalJSON() ([]byte, error) {
	if s.Items == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(s.Items)
}

// scopeXML models the complete <scope> section of a Classic API resource.
type scopeXML struct {
	XMLName            xml.Name         `xml:"scope" json:"-"`
	AllComputers       bool             `xml:"all_computers" json:"all_computers"`
	AllJSSUsers        bool             `xml:"all_jss_users" json:"all_jss_users"`
	Computers          scopeItemSlice   `xml:"computers" json:"computers"`
	ComputerGroups     scopeItemSlice   `xml:"computer_groups" json:"computer_groups"`
	JSSUsers           scopeItemSlice   `xml:"jss_users" json:"jss_users"`
	JSSUserGroups      scopeItemSlice   `xml:"jss_user_groups" json:"jss_user_groups"`
	MobileDeviceGroups scopeItemSlice   `xml:"mobile_device_groups" json:"mobile_device_groups"`
	Buildings          scopeItemSlice   `xml:"buildings" json:"buildings"`
	Departments        scopeItemSlice   `xml:"departments" json:"departments"`
	LimitToUsers       *limitToUsersXML `xml:"limit_to_users,omitempty" json:"limit_to_users,omitempty"`
	Limitations        *limitationsXML  `xml:"limitations,omitempty" json:"limitations,omitempty"`
	Exclusions         *exclusionsXML   `xml:"exclusions,omitempty" json:"exclusions,omitempty"`
}

type limitToUsersXML struct {
	UserGroups scopeStringSlice `xml:"user_groups" json:"user_groups"`
}

type limitationsXML struct {
	Users           scopeItemSlice `xml:"users" json:"users"`
	UserGroups      scopeItemSlice `xml:"user_groups" json:"user_groups"`
	NetworkSegments scopeItemSlice `xml:"network_segments" json:"network_segments"`
	ComputerGroups  scopeItemSlice `xml:"computer_groups" json:"computer_groups"`
	IBeacons        scopeItemSlice `xml:"ibeacons" json:"ibeacons"`
}

type exclusionsXML struct {
	Computers          scopeItemSlice `xml:"computers" json:"computers"`
	ComputerGroups     scopeItemSlice `xml:"computer_groups" json:"computer_groups"`
	MobileDeviceGroups scopeItemSlice `xml:"mobile_device_groups" json:"mobile_device_groups"`
	Users              scopeItemSlice `xml:"users" json:"users"`
	UserGroups         scopeItemSlice `xml:"user_groups" json:"user_groups"`
	NetworkSegments    scopeItemSlice `xml:"network_segments" json:"network_segments"`
	Buildings          scopeItemSlice `xml:"buildings" json:"buildings"`
	Departments        scopeItemSlice `xml:"departments" json:"departments"`
	JSSUsers           scopeItemSlice `xml:"jss_users" json:"jss_users"`
	JSSUserGroups      scopeItemSlice `xml:"jss_user_groups" json:"jss_user_groups"`
	IBeacons           scopeItemSlice `xml:"ibeacons" json:"ibeacons"`
}

// classicResourceXML captures general.id and scope from a Classic API GET.
type classicResourceXML struct {
	XMLName xml.Name
	General struct {
		ID   int    `xml:"id"`
		Name string `xml:"name"`
	} `xml:"general"`
	Scope scopeXML `xml:"scope"`
}

// scopeUpdateXML wraps a scope for a Classic API subset PUT.
type scopeUpdateXML struct {
	XMLName xml.Name
	Scope   scopeXML `xml:"scope"`
}

// ─── Resource registry ─────────────────────────────────────────────────────────

type scopeResource struct {
	apiPath     string // e.g. "policies"
	singularKey string // e.g. "policy"
}

var scopeResources = map[string]scopeResource{
	"policy":                {apiPath: "policies", singularKey: "policy"},
	"macos-config-profile":  {apiPath: "osxconfigurationprofiles", singularKey: "os_x_configuration_profile"},
	"restricted-software":   {apiPath: "restrictedsoftware", singularKey: "restricted_software"},
	"mac-app":               {apiPath: "macapplications", singularKey: "mac_application"},
	"mobile-config-profile": {apiPath: "mobiledeviceconfigurationprofiles", singularKey: "mobile_device_configuration_profile"},
	"mobile-app":            {apiPath: "mobiledeviceapplications", singularKey: "mobile_device_application"},
}

// flagToElemName maps a CLI flag to the XML child element name used when
// adding new items to a scope list.
var flagToElemName = map[string]string{
	"computer-group":      "computer_group",
	"mobile-device-group": "mobile_device_group",
	"building":            "building",
	"department":          "department",
	"network-segment":     "network_segment",
	"user-group":          "user_group",
}

// ─── Commands ──────────────────────────────────────────────────────────────────

func newProScopeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "View and modify scope on Classic API resources",
		Long: `View, add to, or remove from the scope of policies, configuration profiles,
and other Classic API resources.

Supported resource types: policy, macos-config-profile, mobile-config-profile,
restricted-software, mac-app, mobile-app`,
	}

	cmd.AddCommand(newProScopeGetCmd(cliCtx))
	cmd.AddCommand(newProScopeAddCmd(cliCtx))
	cmd.AddCommand(newProScopeRemoveCmd(cliCtx))

	return cmd
}

func newProScopeGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <resource-type> <name>",
		Short: "Display the current scope of a resource",
		Example: `  jamf-cli pro scope get policy "Deploy Chrome"
  jamf-cli pro scope get macos-config-profile "Wi-Fi Settings" -o yaml
  jamf-cli pro scope get policy "Deploy Chrome" -o table`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return scopeResourceNames(), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := lookupScopeResource(args[0])
			if err != nil {
				return err
			}
			_, scope, err := fetchResourceScope(cmd.Context(), cliCtx.Client, res, args[1])
			if err != nil {
				return err
			}
			return outputScope(cliCtx.Output, scope, res.singularKey)
		},
	}
}

func newProScopeAddCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var section string

	cmd := &cobra.Command{
		Use:   "add <resource-type> <name>",
		Short: "Add an item to the scope of a resource",
		Example: `  # Add computer group to policy targets (default section)
  jamf-cli pro scope add policy "Deploy Chrome" --computer-group "All Managed Clients"

  # Add building to profile exclusions
  jamf-cli pro scope add macos-config-profile "Wi-Fi" --section exclusion --building "London"

  # Add network segment to policy limitations
  jamf-cli pro scope add policy "Deploy Chrome" --section limitation --network-segment "Guest"

  # Add user group to policy limitations (uses limit_to_users for policies)
  jamf-cli pro scope add policy "Deploy Chrome" --section limitation --user-group "Staff"`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return scopeResourceNames(), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := lookupScopeResource(args[0])
			if err != nil {
				return err
			}

			target, err := determineScopeTarget(cmd)
			if err != nil {
				return err
			}

			if err := validateScopeCombination(res.singularKey, section, target.flagName); err != nil {
				return err
			}

			id, scope, err := fetchResourceScope(cmd.Context(), cliCtx.Client, res, args[1])
			if err != nil {
				return err
			}

			if !addToScope(scope, res.singularKey, section, target.flagName, target.name) {
				fmt.Fprintf(os.Stderr, "%s %q already in %s scope of %s %q\n",
					target.flagName, target.name, section, args[0], args[1])
				return nil
			}

			if err := putResourceScope(cmd.Context(), cliCtx.Client, res, id, scope); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Added %s %q to %s scope of %s %q\n",
				target.flagName, target.name, section, args[0], args[1])
			return outputScope(cliCtx.Output, scope, res.singularKey)
		},
	}

	addScopeFlags(cmd, &section)
	return cmd
}

func newProScopeRemoveCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var section string

	cmd := &cobra.Command{
		Use:   "remove <resource-type> <name>",
		Short: "Remove an item from the scope of a resource",
		Example: `  jamf-cli pro scope remove policy "Deploy Chrome" --computer-group "Test Group"
  jamf-cli pro scope remove policy "Deploy Chrome" --section exclusion --building "London"`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return scopeResourceNames(), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := lookupScopeResource(args[0])
			if err != nil {
				return err
			}

			target, err := determineScopeTarget(cmd)
			if err != nil {
				return err
			}

			if err := validateScopeCombination(res.singularKey, section, target.flagName); err != nil {
				return err
			}

			id, scope, err := fetchResourceScope(cmd.Context(), cliCtx.Client, res, args[1])
			if err != nil {
				return err
			}

			if !removeFromScope(scope, res.singularKey, section, target.flagName, target.name) {
				fmt.Fprintf(os.Stderr, "%s %q not found in %s scope of %s %q\n",
					target.flagName, target.name, section, args[0], args[1])
				return nil
			}

			if err := putResourceScope(cmd.Context(), cliCtx.Client, res, id, scope); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Removed %s %q from %s scope of %s %q\n",
				target.flagName, target.name, section, args[0], args[1])
			return outputScope(cliCtx.Output, scope, res.singularKey)
		},
	}

	addScopeFlags(cmd, &section)
	return cmd
}

func addScopeFlags(cmd *cobra.Command, section *string) {
	cmd.Flags().StringVar(section, "section", "target", "scope section: target, limitation, exclusion")
	_ = cmd.RegisterFlagCompletionFunc("section", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"target", "limitation", "exclusion"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.Flags().String("computer-group", "", "computer group name")
	cmd.Flags().String("mobile-device-group", "", "mobile device group name")
	cmd.Flags().String("building", "", "building name")
	cmd.Flags().String("department", "", "department name")
	cmd.Flags().String("network-segment", "", "network segment name")
	cmd.Flags().String("user-group", "", "user group name")
}

// ─── API helpers ───────────────────────────────────────────────────────────────

func fetchResourceScope(ctx context.Context, client registry.HTTPClient, res scopeResource, name string) (string, *scopeXML, error) {
	path := fmt.Sprintf("/JSSResource/%s/name/%s", res.apiPath, url.PathEscape(name))
	resp, err := client.Do(ctx, "GET", path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("fetching %s %q: %w", res.singularKey, name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", nil, fmt.Errorf("reading response: %w", err)
	}

	var envelope classicResourceXML
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return "", nil, fmt.Errorf("parsing %s XML: %w", res.singularKey, err)
	}

	if envelope.General.ID == 0 {
		return "", nil, fmt.Errorf("no ID in %s %q", res.singularKey, name)
	}

	return fmt.Sprintf("%d", envelope.General.ID), &envelope.Scope, nil
}

func putResourceScope(ctx context.Context, client registry.HTTPClient, res scopeResource, id string, scope *scopeXML) error {
	envelope := scopeUpdateXML{
		XMLName: xml.Name{Local: res.singularKey},
		Scope:   *scope,
	}

	data, err := xml.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling scope XML: %w", err)
	}
	xmlBody := append([]byte(xml.Header), data...)

	path := fmt.Sprintf("/JSSResource/%s/id/%s/subset/Scope", res.apiPath, url.PathEscape(id))
	resp, err := client.Do(ctx, "PUT", path, bytes.NewReader(xmlBody))
	if err != nil {
		return fmt.Errorf("updating scope: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// ─── Scope manipulation (pure, tested directly) ───────────────────────────────

// addToScope adds a named item to the given scope section. Returns true if the
// item was added, false if already present (idempotent no-op).
func addToScope(scope *scopeXML, singularKey, section, flagName, name string) bool {
	if isPolicyLimitUserGroup(singularKey, section, flagName) {
		return addPolicyLimitUserGroup(scope, name)
	}

	items := getOrCreateScopeItems(scope, section, flagName)
	if items == nil {
		return false
	}

	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) {
			return false
		}
	}

	if items.elemName == "" {
		items.elemName = flagToElemName[flagName]
	}
	items.Items = append(items.Items, namedItem{Name: name})
	return true
}

// removeFromScope removes a named item from the given scope section. Returns
// true if removed, false if not found (idempotent no-op).
//
// For the policy limitation user_group case, user groups can live in BOTH
// limit_to_users/user_groups (plain strings) AND limitations/user_groups
// (named items). Both locations are checked, matching the Classic API's
// behavior and the Python JamfScopeAdjuster's fallthrough logic.
func removeFromScope(scope *scopeXML, singularKey, section, flagName, name string) bool {
	if isPolicyLimitUserGroup(singularKey, section, flagName) {
		a := removePolicyLimitUserGroup(scope, name)
		b := removeNamedItem(readScopeItems(scope, section, flagName), name)
		return a || b
	}

	return removeNamedItem(readScopeItems(scope, section, flagName), name)
}

// removeNamedItem removes an item by name from a scopeItemSlice. Returns true
// if found and removed.
func removeNamedItem(items *scopeItemSlice, name string) bool {
	if items == nil {
		return false
	}

	var keep []namedItem
	found := false
	for _, item := range items.Items {
		if strings.EqualFold(item.Name, name) {
			found = true
			continue
		}
		keep = append(keep, item)
	}
	if !found {
		return false
	}

	items.Items = keep
	return true
}

// addPolicyLimitUserGroup handles the special case: policy limitation user
// groups are stored as plain strings in <limit_to_users><user_groups>.
func addPolicyLimitUserGroup(scope *scopeXML, name string) bool {
	if scope.LimitToUsers == nil {
		scope.LimitToUsers = &limitToUsersXML{}
	}
	ltu := scope.LimitToUsers
	if ltu.UserGroups.elemName == "" {
		ltu.UserGroups.elemName = "user_group"
	}
	for _, g := range ltu.UserGroups.Items {
		if strings.EqualFold(g, name) {
			return false
		}
	}
	ltu.UserGroups.Items = append(ltu.UserGroups.Items, name)
	return true
}

func removePolicyLimitUserGroup(scope *scopeXML, name string) bool {
	if scope.LimitToUsers == nil {
		return false
	}
	groups := &scope.LimitToUsers.UserGroups
	var keep []string
	found := false
	for _, g := range groups.Items {
		if strings.EqualFold(g, name) {
			found = true
			continue
		}
		keep = append(keep, g)
	}
	if !found {
		return false
	}
	groups.Items = keep
	return true
}

// ─── Scope item lookup ─────────────────────────────────────────────────────────

func getOrCreateScopeItems(scope *scopeXML, section, flagName string) *scopeItemSlice {
	switch section {
	case "target":
		return targetItems(scope, flagName)
	case "limitation":
		if scope.Limitations == nil {
			scope.Limitations = &limitationsXML{}
		}
		return limitationItems(scope.Limitations, flagName)
	case "exclusion":
		if scope.Exclusions == nil {
			scope.Exclusions = &exclusionsXML{}
		}
		return exclusionItems(scope.Exclusions, flagName)
	}
	return nil
}

func readScopeItems(scope *scopeXML, section, flagName string) *scopeItemSlice {
	switch section {
	case "target":
		return targetItems(scope, flagName)
	case "limitation":
		if scope.Limitations == nil {
			return nil
		}
		return limitationItems(scope.Limitations, flagName)
	case "exclusion":
		if scope.Exclusions == nil {
			return nil
		}
		return exclusionItems(scope.Exclusions, flagName)
	}
	return nil
}

func targetItems(scope *scopeXML, flagName string) *scopeItemSlice {
	switch flagName {
	case "computer-group":
		return &scope.ComputerGroups
	case "mobile-device-group":
		return &scope.MobileDeviceGroups
	case "building":
		return &scope.Buildings
	case "department":
		return &scope.Departments
	}
	return nil
}

func limitationItems(lim *limitationsXML, flagName string) *scopeItemSlice {
	switch flagName {
	case "network-segment":
		return &lim.NetworkSegments
	case "user-group":
		return &lim.UserGroups
	case "computer-group":
		return &lim.ComputerGroups
	}
	return nil
}

func exclusionItems(exc *exclusionsXML, flagName string) *scopeItemSlice {
	switch flagName {
	case "computer-group":
		return &exc.ComputerGroups
	case "mobile-device-group":
		return &exc.MobileDeviceGroups
	case "user-group":
		return &exc.UserGroups
	case "network-segment":
		return &exc.NetworkSegments
	case "building":
		return &exc.Buildings
	case "department":
		return &exc.Departments
	}
	return nil
}

// ─── Validation ────────────────────────────────────────────────────────────────

func isPolicyLimitUserGroup(singularKey, section, flagName string) bool {
	return singularKey == "policy" && section == "limitation" && flagName == "user-group"
}

func validateScopeCombination(singularKey, section, flagName string) error {
	isRestricted := singularKey == "restricted_software"

	switch section {
	case "target":
		switch flagName {
		case "computer-group", "mobile-device-group", "building", "department":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a target; use --computer-group, --mobile-device-group, --building, or --department", flagName)

	case "limitation":
		if isRestricted {
			return fmt.Errorf("restricted software does not support limitations")
		}
		switch flagName {
		case "network-segment", "user-group", "computer-group":
			return nil
		}
		return fmt.Errorf("--%s is not valid as a limitation; use --network-segment, --user-group, or --computer-group", flagName)

	case "exclusion":
		if isRestricted {
			switch flagName {
			case "computer-group", "building", "department":
				return nil
			}
			return fmt.Errorf("--%s is not valid as an exclusion for restricted software; use --computer-group, --building, or --department", flagName)
		}
		switch flagName {
		case "computer-group", "mobile-device-group", "user-group", "network-segment", "building", "department":
			return nil
		}
		return fmt.Errorf("--%s is not valid as an exclusion", flagName)
	}

	return fmt.Errorf("invalid section %q; use target, limitation, or exclusion", section)
}

// ─── Flag/resource helpers ─────────────────────────────────────────────────────

type scopeTarget struct {
	flagName string
	name     string
}

var scopeFlagNames = []string{
	"computer-group", "mobile-device-group", "building",
	"department", "network-segment", "user-group",
}

func determineScopeTarget(cmd *cobra.Command) (scopeTarget, error) {
	var found scopeTarget
	count := 0
	for _, flag := range scopeFlagNames {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			found = scopeTarget{flagName: flag, name: v}
			count++
		}
	}
	if count == 0 {
		return scopeTarget{}, fmt.Errorf("specify one of: --computer-group, --mobile-device-group, --building, --department, --network-segment, --user-group")
	}
	if count > 1 {
		return scopeTarget{}, fmt.Errorf("specify only one scopeable type per invocation")
	}
	return found, nil
}

func lookupScopeResource(name string) (scopeResource, error) {
	res, ok := scopeResources[name]
	if !ok {
		return scopeResource{}, fmt.Errorf("unsupported resource type %q; valid types: %s",
			name, strings.Join(scopeResourceNames(), ", "))
	}
	return res, nil
}

func scopeResourceNames() []string {
	names := make([]string, 0, len(scopeResources))
	for name := range scopeResources {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ─── Output ────────────────────────────────────────────────────────────────────

func outputScope(out registry.OutputFormatter, scope *scopeXML, singularKey string) error {
	switch outputFmt {
	case "table", "csv", "plain":
		rows := flattenScope(scope, singularKey)
		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "Scope is empty")
			return nil
		}
		data, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		return out.PrintRaw(data)
	default:
		data, err := json.Marshal(scope)
		if err != nil {
			return err
		}
		return out.PrintRaw(data)
	}
}

func flattenScope(scope *scopeXML, singularKey string) []map[string]any {
	var rows []map[string]any

	if scope.AllComputers {
		rows = append(rows, map[string]any{"section": "target", "type": "all_computers", "name": "true"})
	}
	if scope.AllJSSUsers {
		rows = append(rows, map[string]any{"section": "target", "type": "all_jss_users", "name": "true"})
	}

	appendNamedRows(&rows, "target", "computer", scope.Computers.Items)
	appendNamedRows(&rows, "target", "computer_group", scope.ComputerGroups.Items)
	appendNamedRows(&rows, "target", "mobile_device_group", scope.MobileDeviceGroups.Items)
	appendNamedRows(&rows, "target", "building", scope.Buildings.Items)
	appendNamedRows(&rows, "target", "department", scope.Departments.Items)

	// Policy special case: limit_to_users holds plain strings
	if singularKey == "policy" && scope.LimitToUsers != nil {
		for _, g := range scope.LimitToUsers.UserGroups.Items {
			rows = append(rows, map[string]any{"section": "limitation", "type": "user_group", "name": g})
		}
	}

	if scope.Limitations != nil {
		appendNamedRows(&rows, "limitation", "network_segment", scope.Limitations.NetworkSegments.Items)
		appendNamedRows(&rows, "limitation", "user_group", scope.Limitations.UserGroups.Items)
		appendNamedRows(&rows, "limitation", "computer_group", scope.Limitations.ComputerGroups.Items)
	}

	if scope.Exclusions != nil {
		appendNamedRows(&rows, "exclusion", "computer", scope.Exclusions.Computers.Items)
		appendNamedRows(&rows, "exclusion", "computer_group", scope.Exclusions.ComputerGroups.Items)
		appendNamedRows(&rows, "exclusion", "mobile_device_group", scope.Exclusions.MobileDeviceGroups.Items)
		appendNamedRows(&rows, "exclusion", "user_group", scope.Exclusions.UserGroups.Items)
		appendNamedRows(&rows, "exclusion", "network_segment", scope.Exclusions.NetworkSegments.Items)
		appendNamedRows(&rows, "exclusion", "building", scope.Exclusions.Buildings.Items)
		appendNamedRows(&rows, "exclusion", "department", scope.Exclusions.Departments.Items)
	}

	return rows
}

func appendNamedRows(rows *[]map[string]any, section, typeName string, items []namedItem) {
	for _, item := range items {
		if item.Name != "" {
			*rows = append(*rows, map[string]any{"section": section, "type": typeName, "name": item.Name})
		}
	}
}
