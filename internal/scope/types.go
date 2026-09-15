// Copyright 2026, Jamf Software LLC

// Package scope provides scope manipulation for Jamf Classic API resources.
// It handles reading, modifying, and writing scope sections (targets, limitations,
// exclusions) on policies, configuration profiles, and other scopeable resources.
package scope

import (
	"encoding/json"
	"encoding/xml"
	"strings"
)

// Resource identifies a Classic API resource that supports scope operations.
type Resource struct {
	APIPath     string // URL segment under /JSSResource/, e.g. "policies"
	SingularKey string // XML root key for a single object, e.g. "policy"

	// CLIName is the command name this resource ships under, e.g.
	// "classic-policies". It exists so --help examples are invocations a
	// caller can paste rather than fragments starting at "scope add".
	CLIName string

	// ResolveByList resolves name→ID by listing the collection, for the two
	// resources with no /name/ endpoint.
	ResolveByList bool
}

// ScopeTarget holds a resolved flag name and value from a scope add/remove command.
type ScopeTarget struct {
	FlagName string
	Name     string
}

// ─── XML types ─────────────────────────────────────────────────────────────────
// These model the Classic API scope XML structure. Custom XML marshalers on the
// slice types handle the parent/child nesting (e.g. <computer_groups> wrapping
// multiple <computer_group> elements) that Go's built-in encoding cannot express
// with tags alone.

// NamedItem is an item identified by name (and optionally ID or UDID) in scope XML.
// ID is a string to accommodate both integer IDs (most resources) and UUID
// IDs (e.g. ebook scope user groups) returned by the Classic API.
// UDID is populated for individual mobile devices and computers.
type NamedItem struct {
	ID   string `xml:"id,omitempty" json:"id,omitempty"`
	Name string `xml:"name" json:"name"`
	UDID string `xml:"udid,omitempty" json:"udid,omitempty"`
}

// ScopeItemSlice is a list of NamedItem elements under a single XML parent.
// The child element name (e.g. "computer_group") is learned during unmarshal
// and reused during marshal. For newly-created lists it falls back to the
// parent element name with trailing "s" stripped.
type ScopeItemSlice struct {
	Items    []NamedItem
	ElemName string
}

func (s *ScopeItemSlice) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			s.ElemName = t.Name.Local
			var item NamedItem
			if err := d.DecodeElement(&item, &t); err != nil {
				return err
			}
			s.Items = append(s.Items, item)
		case xml.EndElement:
			return nil
		}
	}
}

func (s ScopeItemSlice) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	elemName := s.ElemName
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

func (s ScopeItemSlice) MarshalJSON() ([]byte, error) {
	if s.Items == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(s.Items)
}

// ScopeXML models the complete <scope> section of a Classic API resource.
//
// All scopeable item slices are present unconditionally so that an unmarshal →
// modify → marshal round-trip preserves every section the server returned. A
// missing field here would cause CLI scope add/remove to silently wipe data
// the user set elsewhere (e.g. individual mobile devices added via UI), since
// a scope PUT replaces the whole <scope> subtree.
//
// FIELD ORDER IS LOAD-BEARING. The Classic API's XML binding reads scope
// children in schema order and silently ignores whatever arrives out of it,
// answering 200 either way — so a body assembled in the order a GET happens to
// return (which differs per resource: macapplications answers <exclusions> as
// buildings, departments, mobile_device_groups, users, …) is accepted and
// applied to nothing. This order is the schema order and is what makes
// PutScope's marshalled body take effect; re-ordering these fields to match
// any one resource's GET breaks the others. Wire-checked 2026-09-12.
//
// Fields a given resource does not carry are emitted empty and ignored by the
// server; only a POPULATED foreign category is refused, with a 409 naming it
// ("Mobile device groups cannot be assigned to an macOS profile"), which is
// what the matrix in matrix.go refuses client-side.
//
// <limit_to_users> is deliberately absent. The server denormalises
// <limitations><user_groups> into <limit_to_users><user_groups> on every write
// and back again on every read, so the two wire paths always carry identical
// values and modelling both meant a policy-only special case in five
// functions. Wire-checked 2026-09-12 in both directions: writing only
// limitations.user_groups populates limit_to_users, writing only
// limit_to_users populates limitations.user_groups, and an empty
// limitations.user_groups with limit_to_users omitted clears both.
type ScopeXML struct {
	XMLName            xml.Name        `xml:"scope" json:"-"`
	AllComputers       bool            `xml:"all_computers" json:"all_computers"`
	AllMobileDevices   bool            `xml:"all_mobile_devices,omitempty" json:"all_mobile_devices,omitempty"`
	AllJSSUsers        bool            `xml:"all_jss_users" json:"all_jss_users"`
	Computers          ScopeItemSlice  `xml:"computers" json:"computers"`
	ComputerGroups     ScopeItemSlice  `xml:"computer_groups" json:"computer_groups"`
	MobileDevices      ScopeItemSlice  `xml:"mobile_devices" json:"mobile_devices"`
	MobileDeviceGroups ScopeItemSlice  `xml:"mobile_device_groups" json:"mobile_device_groups"`
	JSSUsers           ScopeItemSlice  `xml:"jss_users" json:"jss_users"`
	JSSUserGroups      ScopeItemSlice  `xml:"jss_user_groups" json:"jss_user_groups"`
	Buildings          ScopeItemSlice  `xml:"buildings" json:"buildings"`
	Departments        ScopeItemSlice  `xml:"departments" json:"departments"`
	Classes            ScopeItemSlice  `xml:"classes" json:"classes"`
	Limitations        *LimitationsXML `xml:"limitations,omitempty" json:"limitations,omitempty"`
	Exclusions         *ExclusionsXML  `xml:"exclusions,omitempty" json:"exclusions,omitempty"`
}

// LimitationsXML models the <limitations> section.
//
// There is no computer_groups field: no scopeable resource returns one in its
// limitations block (a computer group narrows nothing — it is a target), and
// the field this struct used to carry was write-only noise nothing populated
// and nothing read.
type LimitationsXML struct {
	Users           ScopeItemSlice `xml:"users" json:"users"`
	UserGroups      ScopeItemSlice `xml:"user_groups" json:"user_groups"`
	NetworkSegments ScopeItemSlice `xml:"network_segments" json:"network_segments"`
	IBeacons        ScopeItemSlice `xml:"ibeacons" json:"ibeacons"`
}

// ExclusionsXML models the <exclusions> section.
type ExclusionsXML struct {
	Computers          ScopeItemSlice `xml:"computers" json:"computers"`
	ComputerGroups     ScopeItemSlice `xml:"computer_groups" json:"computer_groups"`
	MobileDevices      ScopeItemSlice `xml:"mobile_devices" json:"mobile_devices"`
	MobileDeviceGroups ScopeItemSlice `xml:"mobile_device_groups" json:"mobile_device_groups"`
	Users              ScopeItemSlice `xml:"users" json:"users"`
	UserGroups         ScopeItemSlice `xml:"user_groups" json:"user_groups"`
	NetworkSegments    ScopeItemSlice `xml:"network_segments" json:"network_segments"`
	Buildings          ScopeItemSlice `xml:"buildings" json:"buildings"`
	Departments        ScopeItemSlice `xml:"departments" json:"departments"`
	JSSUsers           ScopeItemSlice `xml:"jss_users" json:"jss_users"`
	JSSUserGroups      ScopeItemSlice `xml:"jss_user_groups" json:"jss_user_groups"`
	IBeacons           ScopeItemSlice `xml:"ibeacons" json:"ibeacons"`
}

// classicResourceXML captures general.id and scope from a Classic API GET.
// ID is a string to accommodate both integer IDs (most resources) and UUID
// IDs (e.g. ebooks) returned by the Classic API.
type classicResourceXML struct {
	XMLName xml.Name
	General struct {
		ID   string `xml:"id"`
		Name string `xml:"name"`
	} `xml:"general"`
	Scope ScopeXML `xml:"scope"`
}

// flagToElemName maps a CLI flag to the XML child element name used when
// adding new items to a scope list.
//
// `--user-group` and `--jss-user-group` both serialize as <user_group> children
// — their parent element (<user_groups> in limitations/exclusions vs
// <jss_user_groups> in target/exclusion) is what disambiguates the semantics.
//
// `--jss-user` writes <jss_user> children; the server's GET response returns
// the same items as <user> children of <jss_users> instead. The server accepts
// both shapes on PUT, so the asymmetric write is harmless.
var flagToElemName = map[string]string{
	"computer":            "computer",
	"computer-group":      "computer_group",
	"mobile-device":       "mobile_device",
	"mobile-device-group": "mobile_device_group",
	"building":            "building",
	"department":          "department",
	"network-segment":     "network_segment",
	"user":                "user",
	"user-group":          "user_group",
	"jss-user-group":      "user_group",
	"jss-user":            "jss_user",
	"ibeacon":             "ibeacon",
	"class":               "class",
}

// scopeFlagNames is the ordered list of scope item flags.
var scopeFlagNames = []string{
	"computer", "computer-group", "mobile-device", "mobile-device-group",
	"building", "department", "network-segment",
	"user", "user-group", "jss-user-group", "jss-user",
	"ibeacon", "class",
}
