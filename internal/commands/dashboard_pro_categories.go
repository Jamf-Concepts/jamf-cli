// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// A category in Jamf Pro can be held by ten object types. The Org Structure
// section's category table used to render an always-zero count for every one of
// them, which reads as "these categories are empty" rather than as "nothing was
// measured" — so the column was dropped, and this counts it properly instead.
//
// Three shapes of source, and the split is what decides the cost:
//
//   - Already in hand. Policies and macOS configuration profiles come from the
//     shared Classic detail passes the --full tier already makes, so they cost
//     nothing.
//   - One list request. The Pro API returns the category inline on its
//     collection endpoints for scripts, packages, eBooks and patch software
//     title configurations, so each is one page sequence and no per-object
//     fetch.
//   - One list plus a detail per object. The Classic API returns id and name
//     only on a collection, so mobile device configuration profiles, Mac
//     applications, mobile device applications and printers need a detail
//     request each. These are the expensive four, and they are why the tally
//     lives in the --full tier and nowhere else.
const (
	catSourcePolicies       = "policies"
	catSourceMacProfiles    = "macOS configuration profiles"
	catSourceMobileProfiles = "mobile device configuration profiles"
	catSourceEbooks         = "eBooks"
	catSourceMacApps        = "Mac applications"
	catSourceMobileApps     = "mobile device applications"
	catSourcePackages       = "packages"
	catSourceScripts        = "scripts"
	catSourcePrinters       = "printers"
	catSourcePatchTitles    = "patch software titles"
)

// categoryUsage is the per-category object count, with enough provenance to
// decide whether it can be published.
//
// A count assembled from a source that could not be read, or from a list whose
// details were partly skipped, is a floor rather than a figure. Publishing a
// floor as a number is the same defect as the always-zero column it replaces,
// so Reliable gates the whole column.
type categoryUsage struct {
	mu      sync.Mutex
	counts  map[string]int
	sources []string
	// failed names the object types whose list could not be read at all.
	failed []string
	// skipped counts the individual detail fetches that failed.
	skipped int
}

func newCategoryUsage() *categoryUsage {
	return &categoryUsage{counts: map[string]int{}}
}

func (c *categoryUsage) add(name string) {
	if name == "" || name == categoryUnassigned {
		return
	}
	c.mu.Lock()
	c.counts[name]++
	c.mu.Unlock()
}

func (c *categoryUsage) countedSource(source string) {
	c.mu.Lock()
	c.sources = append(c.sources, source)
	c.mu.Unlock()
}

func (c *categoryUsage) failedSource(source string, err error) {
	fmt.Fprintf(os.Stderr, "WARNING: category usage: %s: %v\n", source, err)
	c.mu.Lock()
	c.failed = append(c.failed, source)
	c.mu.Unlock()
}

func (c *categoryUsage) skip(source, id string, err error) {
	fmt.Fprintf(os.Stderr, "WARNING: category usage: %s %s detail: %v\n", source, id, err)
	c.mu.Lock()
	c.skipped++
	c.mu.Unlock()
}

// reliable reports whether every source landed and no detail was skipped.
func (c *categoryUsage) reliable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.failed) == 0 && c.skipped == 0
}

func (c *categoryUsage) countFor(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

func (c *categoryUsage) countedSources() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sources...)
}

// classicCategorySource describes a Classic family whose collection carries id
// and name only, so the category is reachable only from a per-object detail.
type classicCategorySource struct {
	source     string
	listPath   string
	listKey    string
	detailPath string
	detailKey  string
}

// classicCategorySources are the four families that need a detail per object.
// Each one's detail wraps the object under its own key, and the category sits
// at general.category — the same shape policies and macOS profiles use.
func classicCategorySources() []classicCategorySource {
	return []classicCategorySource{
		{catSourceMobileProfiles, "/JSSResource/mobiledeviceconfigurationprofiles", "configuration_profile", "/JSSResource/mobiledeviceconfigurationprofiles/id/", "configuration_profile"},
		{catSourceMacApps, "/JSSResource/macapplications", "mac_application", "/JSSResource/macapplications/id/", "mac_application"},
		{catSourceMobileApps, "/JSSResource/mobiledeviceapplications", "mobile_device_application", "/JSSResource/mobiledeviceapplications/id/", "mobile_device_application"},
		{catSourcePrinters, "/JSSResource/printers", "printer", "/JSSResource/printers/id/", "printer"},
	}
}

// proCategorySource describes a Pro API collection that carries the category
// inline, so one page sequence answers the whole family.
type proCategorySource struct {
	source string
	path   string
	// field is the property holding the category. A name is used directly; an
	// id is resolved through the id→name map built from /v1/categories.
	nameField string
	idField   string
}

func proCategorySources() []proCategorySource {
	return []proCategorySource{
		{catSourceScripts, "/v1/scripts", "categoryName", "categoryId"},
		{catSourcePackages, "/v1/packages", "", "categoryId"},
		{catSourceEbooks, "/v1/ebooks", "", "categoryId"},
		{catSourcePatchTitles, "/v3/patch-software-title-configurations", "", "categoryId"},
	}
}

// collectCategoryUsage tallies how many objects each category holds, across
// every object type that can carry one.
//
// byID maps a category id to its name, built from the /v1/categories list the
// caller already fetched — the Pro collections carry an id rather than a name
// for three of the four families.
func collectCategoryUsage(ctx context.Context, client registry.HTTPClient, byID map[string]string, policies, profiles *policyDetailSet) *categoryUsage {
	usage := newCategoryUsage()

	// Already in hand: the two shared Classic detail passes.
	for _, pair := range []struct {
		source string
		set    *policyDetailSet
	}{
		{catSourcePolicies, policies},
		{catSourceMacProfiles, profiles},
	} {
		if pair.set.err != nil {
			usage.failedSource(pair.source, pair.set.err)
			continue
		}
		for _, detail := range pair.set.details {
			usage.add(classicCategoryName(detail))
		}
		if pair.set.skipped > 0 {
			usage.mu.Lock()
			usage.skipped += pair.set.skipped
			usage.mu.Unlock()
		}
		usage.countedSource(pair.source)
	}

	var wg sync.WaitGroup

	// One list request each.
	for _, src := range proCategorySources() {
		wg.Add(1)
		go func(src proCategorySource) {
			defer wg.Done()
			items, err := FetchAllPaginated(ctx, client, src.path, 200)
			if err != nil {
				usage.failedSource(src.source, err)
				return
			}
			for _, item := range items {
				if src.nameField != "" {
					if name, _ := item[src.nameField].(string); name != "" {
						usage.add(name)
						continue
					}
				}
				usage.add(byID[categoryIDString(item[src.idField])])
			}
			usage.countedSource(src.source)
		}(src)
	}

	// One list plus a detail per object.
	for _, src := range classicCategorySources() {
		wg.Add(1)
		go func(src classicCategorySource) {
			defer wg.Done()
			items, err := FetchClassicList(ctx, client, src.listPath, src.listKey)
			if err != nil {
				usage.failedSource(src.source, err)
				return
			}
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item == nil {
					continue
				}
				id := extractClassicID(item)
				if id == "" {
					continue
				}
				detail, err := fetchJSON(ctx, client, src.detailPath+id)
				if err != nil {
					usage.skip(src.source, id, err)
					continue
				}
				obj, _ := detail[src.detailKey].(map[string]any)
				if obj == nil {
					obj = detail
				}
				usage.add(classicCategoryName(obj))
			}
			usage.countedSource(src.source)
		}(src)
	}

	wg.Wait()
	return usage
}

// categoryIDString normalises a category id, which arrives as a JSON number
// from the Pro API and occasionally as a string. Jamf's no-category sentinel is
// -1, which names no category.
func categoryIDString(v any) string {
	switch id := v.(type) {
	case float64:
		if id <= 0 {
			return ""
		}
		return fmt.Sprintf("%.0f", id)
	case string:
		if id == "" || id == "-1" || id == "0" {
			return ""
		}
		return id
	}
	return ""
}
