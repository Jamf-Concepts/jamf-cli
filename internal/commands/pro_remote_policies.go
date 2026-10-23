package commands

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// remotePolicyName matches the name Casper Remote and Jamf Remote give the
// one-off policy row they write for every task:
// "2021-07-19 at 6:59 PM | someone | 1 Computer". Jamf Pro hides these rows in
// the GUI but Classic /JSSResource/policies returns them (PI109008, never
// fixed). Classic does not expose created_by, which is how Jamf's own KB tells
// them apart, so the name is the only filter available from a list response.
var remotePolicyName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} at \d{1,2}:\d{2}\s?[AP]M \| .+ \| \d+ Computers?$`)

// isRemoteAdHocPolicy reports whether a policy name is a legacy Casper/Jamf
// Remote ad-hoc policy row.
func isRemoteAdHocPolicy(name string) bool {
	return remotePolicyName.MatchString(name)
}

// FetchAnalyticPolicyList lists Classic policies for a command that analyses,
// counts or reports on them, with legacy Remote ad-hoc rows removed before any
// detail is fetched. Those rows have no scope, trigger, payload or category by
// construction, so every check that looks for a missing one reports them, and
// a tenant can hold thousands. Commands that manage policies (list, get,
// delete, apply) read the raw list instead: an admin cleaning these up has to
// be able to see them.
func FetchAnalyticPolicyList(ctx context.Context, client registry.HTTPClient, wrapperKey string) ([]any, error) {
	raw, err := FetchClassicList(ctx, client, "/JSSResource/policies", wrapperKey)
	if err != nil {
		return nil, err
	}
	kept, excluded := dropRemoteAdHocPolicies(raw)
	noteExcludedRemotePolicies(excluded)
	return kept, nil
}

// dropRemoteAdHocPolicies returns the list items that are not Remote ad-hoc
// rows, and how many were dropped. An item that is not a map is kept: it is
// not provably one of them.
func dropRemoteAdHocPolicies(items []any) ([]any, int) {
	kept := make([]any, 0, len(items))
	excluded := 0
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if name, _ := m["name"].(string); isRemoteAdHocPolicy(name) {
				excluded++
				continue
			}
		}
		kept = append(kept, it)
	}
	return kept, excluded
}

var remotePolicyNoteOnce sync.Once

// noteExcludedRemotePolicies names the exclusion once per process — the
// dashboard lists policies twice — so a lower count is never unexplained. It is
// an advisory hint: --quiet and --no-hints both silence it.
func noteExcludedRemotePolicies(n int) {
	if n == 0 || quiet || noHints {
		return
	}
	remotePolicyNoteOnce.Do(func() {
		fmt.Fprintf(os.Stderr,
			"Excluded %d legacy Casper/Jamf Remote ad-hoc policies (named \"<date> | <user> | N Computer\"). "+
				"They are hidden in the Jamf Pro GUI and skew these results. "+
				"See them with 'pro classic-policies list'; remove one with 'pro classic-policies delete <id>'.\n", n)
	})
}
