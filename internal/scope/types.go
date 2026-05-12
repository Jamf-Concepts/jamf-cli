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
	APIPath       string // URL segment under /JSSResource/, e.g. "policies"
	SingularKey   string // XML root key for a single object, e.g. "policy"
	ResolveByList bool   // when true, resolve name→ID by listing all (no /name/ endpoint)
	NoSubsetPut   bool   // when true, PUT full document instead of /subset/Scope
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

// NamedItem is an item identified by name (and optionally ID) in scope XML.
// ID is a string to accommodate both integer IDs (most resources) and UUID
// IDs (e.g. ebook scope user groups) returned by the Classic API.
type NamedItem struct {
	ID   string `xml:"id,omitempty" json:"id,omitempty"`
	Name string `xml:"name" json:"name"`
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

// ScopeStringSlice is a list of plain string elements under a single XML parent.
// Used for policy limitation user groups (limit_to_users/user_groups), where
// items are bare strings rather than objects with name sub-elements.
type ScopeStringSlice struct {
	Items    []string
	ElemName string
}

func (s *ScopeStringSlice) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			s.ElemName = t.Name.Local
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

func (s ScopeStringSlice) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	elemName := s.ElemName
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

func (s ScopeStringSlice) MarshalJSON() ([]byte, error) {
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
// the user set elsewhere (e.g. individual mobile devices added via UI).
type ScopeXML struct {
	XMLName            xml.Name         `xml:"scope" json:"-"`
	AllComputers       bool             `xml:"all_computers" json:"all_computers"`
	AllMobileDevices   bool             `xml:"all_mobile_devices,omitempty" json:"all_mobile_devices,omitempty"`
	AllJSSUsers        bool             `xml:"all_jss_users" json:"all_jss_users"`
	Computers          ScopeItemSlice   `xml:"computers" json:"computers"`
	ComputerGroups     ScopeItemSlice   `xml:"computer_groups" json:"computer_groups"`
	MobileDevices      ScopeItemSlice   `xml:"mobile_devices" json:"mobile_devices"`
	MobileDeviceGroups ScopeItemSlice   `xml:"mobile_device_groups" json:"mobile_device_groups"`
	JSSUsers           ScopeItemSlice   `xml:"jss_users" json:"jss_users"`
	JSSUserGroups      ScopeItemSlice   `xml:"jss_user_groups" json:"jss_user_groups"`
	Buildings          ScopeItemSlice   `xml:"buildings" json:"buildings"`
	Departments        ScopeItemSlice   `xml:"departments" json:"departments"`
	Classes            ScopeItemSlice   `xml:"classes" json:"classes,omitempty"`
	LimitToUsers       *LimitToUsersXML `xml:"limit_to_users,omitempty" json:"limit_to_users,omitempty"`
	Limitations        *LimitationsXML  `xml:"limitations,omitempty" json:"limitations,omitempty"`
	Exclusions         *ExclusionsXML   `xml:"exclusions,omitempty" json:"exclusions,omitempty"`
}

// LimitToUsersXML models the <limit_to_users> section (policies only).
type LimitToUsersXML struct {
	UserGroups ScopeStringSlice `xml:"user_groups" json:"user_groups"`
}

// LimitationsXML models the <limitations> section.
type LimitationsXML struct {
	Users           ScopeItemSlice `xml:"users" json:"users"`
	UserGroups      ScopeItemSlice `xml:"user_groups" json:"user_groups"`
	NetworkSegments ScopeItemSlice `xml:"network_segments" json:"network_segments"`
	ComputerGroups  ScopeItemSlice `xml:"computer_groups" json:"computer_groups"`
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

// scopeUpdateXML wraps a scope for a Classic API subset PUT.
type scopeUpdateXML struct {
	XMLName xml.Name
	Scope   ScopeXML `xml:"scope"`
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
}

// scopeFlagNames is the ordered list of scope item flags.
var scopeFlagNames = []string{
	"computer", "computer-group", "mobile-device", "mobile-device-group",
	"building", "department", "network-segment",
	"user", "user-group", "jss-user-group", "jss-user",
}
