// Copyright 2026, Jamf Software LLC

package scope

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// scopeEchoClient answers every GET with the document it is given, so a test
// can stage exactly what the server "kept" after a write.
type scopeEchoClient struct {
	doc      string
	requests []string
}

func (c *scopeEchoClient) Do(_ context.Context, method, path string, _ io.Reader) (*http.Response, error) {
	c.requests = append(c.requests, method+" "+path)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(c.doc))}, nil
}

// TestVerifyScopeWriteCatchesCollateralLoss is the regression this guard was
// built for. The single-item check it replaced verified only the category the
// command changed, so a member the server dropped from an untouched category
// was invisible and the command reported success.
//
// The live case: an ebook's <classes> target can only be set while empty. Once
// populated, any later scope write clears it — carrying the identical value or
// omitting it both clear it (wire-checked 2026-09-12, 5/5 each way). So
// `scope add --building` on an ebook that has a class destroys the class, and
// a check scoped to --building sees nothing wrong.
func TestVerifyScopeWriteCatchesCollateralLoss(t *testing.T) {
	sent := &ScopeXML{
		Buildings: ScopeItemSlice{Items: []NamedItem{{Name: "HQ"}}, ElemName: "building"},
		Classes:   ScopeItemSlice{Items: []NamedItem{{ID: "3", Name: "Year 9"}}, ElemName: "class"},
	}
	// What the server kept: the building landed, the class did not.
	client := &scopeEchoClient{doc: `<ebook><general><id>7</id></general><scope>
		<buildings><building><id>1</id><name>HQ</name></building></buildings>
		<classes/>
	</scope></ebook>`}
	res := Resource{APIPath: "ebooks", SingularKey: "ebook"}
	touched := ScopeTarget{FlagName: flagBuilding, Name: "HQ"}

	err := VerifyScopeWrite(context.Background(), client, res, "7", sent, touched, SectionTarget, true)
	if err == nil {
		t.Fatal("a category the server dropped must be reported, not passed over")
	}
	for _, want := range []string{"Year 9", "class", "target"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("report should name the lost member and where it was; got: %v", err)
		}
	}
	// PutScope delivers class targets in two requests precisely so this cannot
	// happen, so reaching this state means that workaround did not hold. The
	// message has to say so rather than restating the server rule as expected
	// behaviour, or an operator reads it as "working as intended" and stops.
	if !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("a class drop is a failure of the two-request delivery and should read as unexpected; got: %v", err)
	}
	if strings.Contains(err.Error(), "limitation") {
		t.Errorf("the class case is worked around, so it must not be reported as an accepted limitation; got: %v", err)
	}
}

// TestVerifyScopeWritePassesACleanWrite is the containment: the server augments
// what it was sent — a member sent by name comes back with an id, a network
// segment gains a uid — so the comparison must match on identity rather than
// on the elements being equal, or every successful write would be reported as
// a loss.
func TestVerifyScopeWritePassesACleanWrite(t *testing.T) {
	sent := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "All Managed"}}, ElemName: "computer_group"},
		Limitations: &LimitationsXML{
			NetworkSegments: ScopeItemSlice{Items: []NamedItem{{ID: "102"}}, ElemName: "network_segment"},
		},
	}
	client := &scopeEchoClient{doc: `<policy><general><id>1</id></general><scope>
		<computer_groups><computer_group><id>11</id><name>All Managed</name></computer_group></computer_groups>
		<limitations>
			<network_segments><network_segment><id>102</id><uid>43_102</uid><name>Corporate</name></network_segment></network_segments>
		</limitations>
	</scope></policy>`}
	res := Resource{APIPath: "policies", SingularKey: "policy"}
	touched := ScopeTarget{FlagName: flagComputerGroup, Name: "All Managed"}

	if err := VerifyScopeWrite(context.Background(), client, res, "1", sent, touched, SectionTarget, true); err != nil {
		t.Errorf("a clean write must not be reported as a loss: %v", err)
	}
}

// TestVerifyScopeWriteStillDiagnosesTheTouchedMember keeps the specific
// diagnosis for the member the caller asked about: an identifier that named no
// record is a different problem from a category the server refused to keep,
// and the remedies differ.
func TestVerifyScopeWriteStillDiagnosesTheTouchedMember(t *testing.T) {
	sent := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Typo Group"}}, ElemName: "computer_group"},
	}
	client := &scopeEchoClient{doc: `<policy><general><id>1</id></general><scope><computer_groups/></scope></policy>`}
	res := Resource{APIPath: "policies", SingularKey: "policy"}
	touched := ScopeTarget{FlagName: flagComputerGroup, Name: "Typo Group"}

	err := VerifyScopeWrite(context.Background(), client, res, "1", sent, touched, SectionTarget, true)
	if err == nil {
		t.Fatal("the touched member's absence must be reported")
	}
	if !strings.Contains(err.Error(), "names no existing record") {
		t.Errorf("the touched member should get the identifier diagnosis, not the collateral one; got: %v", err)
	}
}

// TestDiffScopeExcludesTheTouchedMember stops the same loss being reported
// twice under two different explanations.
func TestDiffScopeExcludesTheTouchedMember(t *testing.T) {
	sent := &ScopeXML{
		ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "Gone"}}},
	}
	got := &ScopeXML{}
	touched := ScopeTarget{FlagName: flagComputerGroup, Name: "Gone"}

	if drops := DiffScope(sent, got, SectionTarget, touched); len(drops) != 0 {
		t.Errorf("the touched member must not also appear as collateral: %+v", drops)
	}
	// A different member in the same category is collateral and is reported.
	sent.ComputerGroups.Items = append(sent.ComputerGroups.Items, NamedItem{Name: "Also Gone"})
	drops := DiffScope(sent, got, SectionTarget, touched)
	if len(drops) != 1 || len(drops[0].Missing) != 1 || drops[0].Missing[0] != "Also Gone" {
		t.Errorf("drops = %+v, want only \"Also Gone\"", drops)
	}
}

// TestDiffScopeCoversEverySection walks a member through all three sections so
// a section left out of the comparison loop fails here rather than silently
// never being checked. The category differs per section because no single one
// is valid in all three — a building selects an audience and a network segment
// narrows one.
func TestDiffScopeCoversEverySection(t *testing.T) {
	perSection := map[string]string{
		SectionTarget:     flagBuilding,
		SectionLimitation: flagNetworkSegment,
		SectionExclusion:  flagDepartment,
	}
	for _, section := range Sections {
		flag := perSection[section]
		sent := &ScopeXML{}
		if !AddToScope(sent, section, flag, "Staged") {
			t.Fatalf("%s: could not stage a --%s", section, flag)
		}
		// Nothing came back, so the member is missing.
		drops := DiffScope(sent, &ScopeXML{}, "", ScopeTarget{})
		if len(drops) != 1 || drops[0].Section != section || drops[0].Category != flag {
			t.Errorf("%s: drops = %+v, want one --%s drop in this section", section, drops, flag)
		}
	}
}

// TestPutScope_ClassTargetsAreDeliveredInTwoRequests pins the workaround for
// the one server rule no single request can satisfy.
//
// Jamf Pro stores <classes> only while the stored category is empty, so a
// write made while it already holds a member clears it — carrying the
// identical value or omitting it both clear it (wire-checked 2026-09-12, 5/5
// each way). Before this, `scope add --building` on an ebook holding a class
// destroyed the class and reported success.
func TestPutScope_ClassTargetsAreDeliveredInTwoRequests(t *testing.T) {
	client := &mockPutClient{}
	res := Resource{APIPath: "ebooks", SingularKey: "ebook"}
	s := &ScopeXML{
		Buildings: ScopeItemSlice{Items: []NamedItem{{Name: "HQ"}}, ElemName: "building"},
		Classes:   ScopeItemSlice{Items: []NamedItem{{Name: "Year 9"}}, ElemName: "class"},
	}

	if err := PutScope(context.Background(), client, res, "7", s); err != nil {
		t.Fatalf("PutScope: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %v, want two PUTs", client.requests)
	}
	for i, r := range client.requests {
		if r != "PUT /JSSResource/ebooks/id/7" {
			t.Errorf("request[%d] = %q", i, r)
		}
	}

	// The first request must carry the caller's real change with the classes
	// emptied — that ordering is what makes an interruption between the two no
	// worse than the single request it replaced.
	first, second := client.bodies[0], client.bodies[1]
	if !strings.Contains(first, "<name>HQ</name>") {
		t.Errorf("the first request must carry the intended change:\n%s", first)
	}
	if strings.Contains(first, "Year 9") {
		t.Errorf("the first request must empty the classes, or the server refuses them:\n%s", first)
	}
	if !strings.Contains(first, "<classes></classes>") {
		t.Errorf("the first request must still send an empty <classes> element:\n%s", first)
	}

	// The second restores them against a now-empty category.
	if !strings.Contains(second, "Year 9") || !strings.Contains(second, "<name>HQ</name>") {
		t.Errorf("the second request must carry the classes and the change:\n%s", second)
	}

	// The caller's scope must not be left mutated by the clearing pass.
	if len(s.Classes.Items) != 1 {
		t.Errorf("PutScope emptied the caller's classes: %+v", s.Classes.Items)
	}
}

// TestPutScope_NoClassesIsStillOneRequest is the containment: the extra write
// happens only where it is the difference between working and losing data, so
// every other resource and every classless scope is unchanged.
func TestPutScope_NoClassesIsStillOneRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  Resource
		s    *ScopeXML
	}{
		{"policy", Resource{APIPath: "policies", SingularKey: "policy"}, &ScopeXML{
			ComputerGroups: ScopeItemSlice{Items: []NamedItem{{Name: "All"}}, ElemName: "computer_group"},
		}},
		{"ebook with an empty classes element", Resource{APIPath: "ebooks", SingularKey: "ebook"}, &ScopeXML{
			Buildings: ScopeItemSlice{Items: []NamedItem{{Name: "HQ"}}, ElemName: "building"},
			Classes:   ScopeItemSlice{ElemName: "class"},
		}},
	} {
		client := &mockPutClient{}
		if err := PutScope(context.Background(), client, tc.res, "1", tc.s); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(client.requests) != 1 {
			t.Errorf("%s: requests = %v, want one", tc.name, client.requests)
		}
	}
}

// TestPutScope_ClearingPassFailureIsReported keeps the two-request delivery
// from swallowing a failed first write and reporting success on the second.
func TestPutScope_ClearingPassFailureIsReported(t *testing.T) {
	client := &failFirstPutClient{}
	res := Resource{APIPath: "ebooks", SingularKey: "ebook"}
	s := &ScopeXML{Classes: ScopeItemSlice{Items: []NamedItem{{Name: "Year 9"}}, ElemName: "class"}}

	if err := PutScope(context.Background(), client, res, "7", s); err == nil {
		t.Error("a failed clearing pass must be reported, not followed by the second write")
	}
	if client.calls != 1 {
		t.Errorf("calls = %d, want the sequence to stop at the failure", client.calls)
	}
}

// failFirstPutClient fails the first write and counts calls.
type failFirstPutClient struct{ calls int }

func (c *failFirstPutClient) Do(_ context.Context, _, _ string, _ io.Reader) (*http.Response, error) {
	c.calls++
	if c.calls == 1 {
		return nil, errAlwaysFails
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
}

var errAlwaysFails = errTest("write refused")

type errTest string

func (e errTest) Error() string { return string(e) }
