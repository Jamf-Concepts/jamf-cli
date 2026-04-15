// Copyright 2026, Jamf Software LLC

package commands

import (
	"testing"

	"github.com/Jamf-Concepts/jamfschool-go-sdk/jamfschool"
)

// ─── flattenSchoolDevice ─────────────────────────────────────────────────────

func TestFlattenSchoolDevice_BasicFields(t *testing.T) {
	d := jamfschool.Device{
		UDID:         "AAAA-BBBB-CCCC",
		Name:         "iPad Lab 1",
		SerialNumber: "C02XYZ123",
		IsManaged:    true,
		IsSupervised: true,
		InTrash:      false,
		LastCheckin:  "2026-04-10",
		Model: jamfschool.DeviceModel{
			Name: "iPad Pro",
		},
		OS: jamfschool.DeviceOS{
			Prefix:  "iPadOS",
			Version: "17.4",
		},
	}
	m := flattenSchoolDevice(d)

	if m["name"] != "iPad Lab 1" {
		t.Errorf("name = %v, want %q", m["name"], "iPad Lab 1")
	}
	if m["udid"] != "AAAA-BBBB-CCCC" {
		t.Errorf("udid = %v, want %q", m["udid"], "AAAA-BBBB-CCCC")
	}
	if m["serialNumber"] != "C02XYZ123" {
		t.Errorf("serialNumber = %v, want %q", m["serialNumber"], "C02XYZ123")
	}
	if m["model"] != "iPad Pro" {
		t.Errorf("model = %v, want %q", m["model"], "iPad Pro")
	}
	if m["os"] != "iPadOS 17.4" {
		t.Errorf("os = %v, want %q", m["os"], "iPadOS 17.4")
	}
	if m["isManaged"] != true {
		t.Errorf("isManaged = %v, want true", m["isManaged"])
	}
	if m["isSupervised"] != true {
		t.Errorf("isSupervised = %v, want true", m["isSupervised"])
	}
	if m["inTrash"] != false {
		t.Errorf("inTrash = %v, want false", m["inTrash"])
	}
}

// ─── flattenSchoolUser ───────────────────────────────────────────────────────

func TestFlattenSchoolUser_BasicFields(t *testing.T) {
	u := jamfschool.User{
		ID:          42,
		Username:    "jdoe",
		Email:       "jdoe@school.edu",
		FirstName:   "John",
		LastName:    "Doe",
		Status:      "Active",
		DeviceCount: 2,
		LocationID:  1,
		InTrash:     false,
	}
	m := flattenSchoolUser(u)

	if m["id"] != int64(42) {
		t.Errorf("id = %v, want 42", m["id"])
	}
	if m["username"] != "jdoe" {
		t.Errorf("username = %v, want %q", m["username"], "jdoe")
	}
	if m["email"] != "jdoe@school.edu" {
		t.Errorf("email = %v, want %q", m["email"], "jdoe@school.edu")
	}
	if m["firstName"] != "John" {
		t.Errorf("firstName = %v, want %q", m["firstName"], "John")
	}
	if m["deviceCount"] != int64(2) {
		t.Errorf("deviceCount = %v, want 2", m["deviceCount"])
	}
}

// ─── flattenSchoolProfile ────────────────────────────────────────────────────

func TestFlattenSchoolProfile_BasicFields(t *testing.T) {
	p := jamfschool.Profile{
		ID:          10,
		Name:        "WiFi Profile",
		Identifier:  "com.school.wifi",
		Description: "Auto-join campus WiFi",
		Platform:    "iOS",
		LocationID:  1,
	}
	m := flattenSchoolProfile(p)

	if m["name"] != "WiFi Profile" {
		t.Errorf("name = %v, want %q", m["name"], "WiFi Profile")
	}
	if m["identifier"] != "com.school.wifi" {
		t.Errorf("identifier = %v, want %q", m["identifier"], "com.school.wifi")
	}
	if m["platform"] != "iOS" {
		t.Errorf("platform = %v, want %q", m["platform"], "iOS")
	}
}

// ─── flattenSchoolApp ────────────────────────────────────────────────────────

func TestFlattenSchoolApp_BasicFields(t *testing.T) {
	a := jamfschool.App{
		ID:       5,
		Name:     "Pages",
		BundleID: "com.apple.Pages",
		AdamID:   361309726,
		Vendor:   "Apple",
		Version:  "14.0",
		Platform: "iOS",
	}
	m := flattenSchoolApp(a)

	if m["name"] != "Pages" {
		t.Errorf("name = %v, want %q", m["name"], "Pages")
	}
	if m["bundleId"] != "com.apple.Pages" {
		t.Errorf("bundleId = %v, want %q", m["bundleId"], "com.apple.Pages")
	}
	if m["adamId"] != int64(361309726) {
		t.Errorf("adamId = %v, want 361309726", m["adamId"])
	}
}

// ─── flattenSchoolClass ──────────────────────────────────────────────────────

func TestFlattenSchoolClass_BasicFields(t *testing.T) {
	c := jamfschool.Class{
		UUID:         "cls-uuid-123",
		Name:         "Math 101",
		Description:  "Intro to math",
		Source:       "manual",
		StudentCount: 25,
		TeacherCount: 2,
		DeviceCount:  10,
		LocationID:   1,
	}
	m := flattenSchoolClass(c)

	if m["uuid"] != "cls-uuid-123" {
		t.Errorf("uuid = %v, want %q", m["uuid"], "cls-uuid-123")
	}
	if m["name"] != "Math 101" {
		t.Errorf("name = %v, want %q", m["name"], "Math 101")
	}
	if m["studentCount"] != int64(25) {
		t.Errorf("studentCount = %v, want 25", m["studentCount"])
	}
	if m["teacherCount"] != int64(2) {
		t.Errorf("teacherCount = %v, want 2", m["teacherCount"])
	}
}

// ─── flattenSchoolDeviceGroup ────────────────────────────────────────────────

func TestFlattenSchoolDeviceGroup_BasicFields(t *testing.T) {
	g := jamfschool.DeviceGroup{
		ID:           7,
		Name:         "Lab iPads",
		Description:  "All iPads in lab 1",
		IsSmartGroup: false,
		Members:      15,
		Shared:       true,
		Type:         "static",
	}
	m := flattenSchoolDeviceGroup(g)

	if m["name"] != "Lab iPads" {
		t.Errorf("name = %v, want %q", m["name"], "Lab iPads")
	}
	if m["isSmartGroup"] != false {
		t.Errorf("isSmartGroup = %v, want false", m["isSmartGroup"])
	}
	if m["members"] != int64(15) {
		t.Errorf("members = %v, want 15", m["members"])
	}
	if m["shared"] != true {
		t.Errorf("shared = %v, want true", m["shared"])
	}
}

// ─── flattenSchoolLocation ───────────────────────────────────────────────────

func TestFlattenSchoolLocation_BasicFields(t *testing.T) {
	city := "Springfield"
	l := jamfschool.Location{
		ID:         1,
		Name:       "Main Campus",
		IsDistrict: false,
		Source:     "manual",
		City:       &city,
	}
	m := flattenSchoolLocation(l)

	if m["name"] != "Main Campus" {
		t.Errorf("name = %v, want %q", m["name"], "Main Campus")
	}
	if m["city"] != "Springfield" {
		t.Errorf("city = %v, want %q", m["city"], "Springfield")
	}
}

func TestFlattenSchoolLocation_NilCity(t *testing.T) {
	l := jamfschool.Location{
		ID:   2,
		Name: "Remote",
	}
	m := flattenSchoolLocation(l)

	if _, ok := m["city"]; ok {
		t.Error("city should be absent when nil")
	}
}

// ─── flattenSchoolIBeacon ────────────────────────────────────────────────────

func TestFlattenSchoolIBeacon_BasicFields(t *testing.T) {
	b := jamfschool.IBeacon{
		ID:          3,
		Name:        "Library Beacon",
		UUID:        "E2C56DB5-DFFB-48D2-B060-D0F5A71096E0",
		Major:       1,
		Minor:       100,
		Description: "Main library entrance",
	}
	m := flattenSchoolIBeacon(b)

	if m["name"] != "Library Beacon" {
		t.Errorf("name = %v, want %q", m["name"], "Library Beacon")
	}
	if m["uuid"] != "E2C56DB5-DFFB-48D2-B060-D0F5A71096E0" {
		t.Errorf("uuid = %v, want %q", m["uuid"], "E2C56DB5-DFFB-48D2-B060-D0F5A71096E0")
	}
	if m["major"] != int64(1) {
		t.Errorf("major = %v, want 1", m["major"])
	}
}

// ─── flattenSchoolDEPDevice ──────────────────────────────────────────────────

func TestFlattenSchoolDEPDevice_BasicFields(t *testing.T) {
	d := jamfschool.DEPDevice{
		ID:           1,
		SerialNumber: "C02ABC123",
		Model:        "MacBook Pro",
		Status:       "assigned",
		DeviceName:   "Teacher MacBook",
		ProfileName:  "Default DEP Profile",
	}
	m := flattenSchoolDEPDevice(d)

	if m["serialNumber"] != "C02ABC123" {
		t.Errorf("serialNumber = %v, want %q", m["serialNumber"], "C02ABC123")
	}
	if m["model"] != "MacBook Pro" {
		t.Errorf("model = %v, want %q", m["model"], "MacBook Pro")
	}
	if m["status"] != "assigned" {
		t.Errorf("status = %v, want %q", m["status"], "assigned")
	}
}

// ─── schoolUserToInput ───────────────────────────────────────────────────────

func TestSchoolUserToInput_MapsFields(t *testing.T) {
	u := &jamfschool.User{
		ID:         42,
		Username:   "jdoe",
		Email:      "jdoe@school.edu",
		FirstName:  "John",
		LastName:   "Doe",
		Domain:     "school.edu",
		Notes:      "Teacher",
		Exclude:    false,
		LocationID: 1,
	}
	input := schoolUserToInput(u)

	if input.Username != "jdoe" {
		t.Errorf("Username = %q, want %q", input.Username, "jdoe")
	}
	if input.Email != "jdoe@school.edu" {
		t.Errorf("Email = %q, want %q", input.Email, "jdoe@school.edu")
	}
	if input.LocationID == nil || *input.LocationID != 1 {
		t.Errorf("LocationID = %v, want 1", input.LocationID)
	}
}

// ─── schoolClassToInput ──────────────────────────────────────────────────────

func TestSchoolClassToInput_MapsFields(t *testing.T) {
	c := &jamfschool.Class{
		UUID:        "cls-uuid",
		Name:        "Science 201",
		Description: "Advanced science",
		LocationID:  2,
		Students: []jamfschool.ClassMember{
			{ID: 10},
			{ID: 20},
		},
		Teachers: []jamfschool.ClassMember{
			{ID: 5},
		},
	}
	input := schoolClassToInput(c)

	if input.Name != "Science 201" {
		t.Errorf("Name = %q, want %q", input.Name, "Science 201")
	}
	if len(input.Students) != 2 {
		t.Fatalf("Students length = %d, want 2", len(input.Students))
	}
	if input.Students[0] != 10 {
		t.Errorf("Students[0] = %d, want 10", input.Students[0])
	}
	if len(input.Teachers) != 1 || input.Teachers[0] != 5 {
		t.Errorf("Teachers = %v, want [5]", input.Teachers)
	}
}

// ─── schoolGroupToInput ──────────────────────────────────────────────────────

func TestSchoolGroupToInput_MapsFields(t *testing.T) {
	g := &jamfschool.Group{
		ID:          3,
		Name:        "Teachers",
		Description: "All teachers",
		LocationID:  1,
		ACL: jamfschool.ACL{
			Teacher: "Allow",
		},
	}
	input := schoolGroupToInput(g)

	if input.Name != "Teachers" {
		t.Errorf("Name = %q, want %q", input.Name, "Teachers")
	}
	if input.ACL == nil {
		t.Fatal("ACL should not be nil")
	}
	if input.ACL.Teacher != "Allow" {
		t.Errorf("ACL.Teacher = %q, want %q", input.ACL.Teacher, "Allow")
	}
}

// ─── schoolDeviceGroupToInput ────────────────────────────────────────────────

func TestSchoolDeviceGroupToInput_MapsFields(t *testing.T) {
	g := &jamfschool.DeviceGroup{
		ID:          7,
		Name:        "Lab iPads",
		Description: "All iPads in lab 1",
		Information: "Room 101",
		Shared:      true,
		LocationID:  1,
	}
	input := schoolDeviceGroupToInput(g)

	if input.Name != "Lab iPads" {
		t.Errorf("Name = %q, want %q", input.Name, "Lab iPads")
	}
	if input.Information != "Room 101" {
		t.Errorf("Information = %q, want %q", input.Information, "Room 101")
	}
	if input.Shared != true {
		t.Errorf("Shared = %v, want true", input.Shared)
	}
}

// ─── schoolIBeaconToInput ────────────────────────────────────────────────────

func TestSchoolIBeaconToInput_MapsFields(t *testing.T) {
	b := &jamfschool.IBeacon{
		ID:          3,
		Name:        "Library",
		UUID:        "E2C56DB5",
		Major:       1,
		Minor:       100,
		Description: "Entrance",
	}
	input := schoolIBeaconToInput(b)

	if input.Name != "Library" {
		t.Errorf("Name = %q, want %q", input.Name, "Library")
	}
	if input.UUID != "E2C56DB5" {
		t.Errorf("UUID = %q, want %q", input.UUID, "E2C56DB5")
	}
	if input.Major == nil || *input.Major != 1 {
		t.Errorf("Major = %v, want 1", input.Major)
	}
	if input.Minor == nil || *input.Minor != 100 {
		t.Errorf("Minor = %v, want 100", input.Minor)
	}
}

// ─── parseInt64List ──────────────────────────────────────────────────────────

func TestParseInt64List_Valid(t *testing.T) {
	result, err := parseInt64List("1,2,3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("length = %d, want 3", len(result))
	}
	if result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("result = %v, want [1 2 3]", result)
	}
}

func TestParseInt64List_WithSpaces(t *testing.T) {
	result, err := parseInt64List(" 10 , 20 , 30 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("length = %d, want 3", len(result))
	}
}

func TestParseInt64List_Empty(t *testing.T) {
	result, err := parseInt64List("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestParseInt64List_Invalid(t *testing.T) {
	_, err := parseInt64List("1,abc,3")
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

// ─── splitAndTrimSchool ──────────────────────────────────────────────────────

func TestSplitAndTrimSchool_Valid(t *testing.T) {
	result := splitAndTrimSchool("aaa,bbb,ccc")
	if len(result) != 3 {
		t.Fatalf("length = %d, want 3", len(result))
	}
	if result[0] != "aaa" || result[1] != "bbb" || result[2] != "ccc" {
		t.Errorf("result = %v", result)
	}
}

func TestSplitAndTrimSchool_Empty(t *testing.T) {
	result := splitAndTrimSchool("")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestSplitAndTrimSchool_SkipsBlanks(t *testing.T) {
	result := splitAndTrimSchool("aaa,,bbb, ,ccc")
	if len(result) != 3 {
		t.Fatalf("length = %d, want 3", len(result))
	}
}
