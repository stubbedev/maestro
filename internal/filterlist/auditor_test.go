// Ports tests/Composer/Test/FilterList/FilterListAuditorTest.php
// (MIT, testdata/LICENSE-composer).

package filterlist_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
)

// filterListMap builds package name => list name => entries.
func filterListMap(entries ...*filterlist.FilterListEntry) *filterlist.FilterListMap {
	m := &filterlist.FilterListMap{}
	for _, e := range entries {
		byList, ok := m.Get(e.PackageName)
		if !ok {
			byList = &filterlist.Filter{}
			m.Set(e.PackageName, byList)
		}
		list, _ := byList.Get(e.ListName)
		byList.Set(e.ListName, append(list, e))
	}

	return m
}

func TestFilterListAuditor_GetMatchingEntriesUnfilteredPackages(t *testing.T) {
	cases := []struct {
		name                string
		ignorePackageConfig *php.Array
		expectedCount       int
	}{
		{"acme/other fully ignore", php.ListOf("acme/other"), 1},
		{"acme/package fully ignore", php.ListOf("acme/package"), 0},
		{"acme/package fully ignore but not on-block with block operation", php.ArrayOf("acme/package", php.ArrayOf("on-block", false)), 1},
		{"acme/package fully ignore but not on-audit with block operation", php.ArrayOf("acme/package", php.ArrayOf("on-audit", false)), 0},
		{"acme/* fully ignore", php.ListOf("acme/*"), 0},
		{"acme/package 1.0 ignore", php.ArrayOf("acme/package", php.ArrayOf("constraint", "1.0")), 0},
		{"acme/* 1.0 ignore", php.ArrayOf("acme/*", php.ArrayOf("constraint", "1.0")), 0},
		{"acme/package 1.1 ignore", php.ArrayOf("acme/package", php.ArrayOf("constraint", "1.1")), 1},
		{"acme/* 1.1 ignore", php.ArrayOf("acme/*", php.ArrayOf("constraint", "1.1")), 1},
		{"multiple acme/package entries, first rule matches", php.ArrayOf("acme/package", php.ListOf(php.ArrayOf("constraint", "1.0"), php.ArrayOf("constraint", "1.1"))), 0},
		{"multiple acme/package entries, second rule matches", php.ArrayOf("acme/package", php.ListOf(php.ArrayOf("constraint", "1.1"), php.ArrayOf("constraint", "1.0"))), 0},
		{"multiple acme/package entries, no rule matches", php.ArrayOf("acme/package", php.ListOf(php.ArrayOf("constraint", "1.1"), php.ArrayOf("constraint", "1.2"))), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
			m := filterListMap(
				createEntry(t, "list", php.ArrayOf("package", "acme/package", "constraint", "*")),
				createEntry(t, "list", php.ArrayOf("package", "acme/other", "constraint", "*")),
			)
			pc := policyConfig(t, php.ArrayOf("list", php.ArrayOf("ignore", tc.ignorePackageConfig)))

			entries := filterlist.FilterListAuditor{}.GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate)
			if len(entries) != tc.expectedCount {
				t.Errorf("got %d entries, want %d", len(entries), tc.expectedCount)
			}
		})
	}
}

func TestFilterListAuditor_GetMatchingEntriesIgnoreSource(t *testing.T) {
	type tcase struct {
		name          string
		operation     string
		entrySource   any
		ignoreSource  []string
		expectedCount int
	}
	var cases []tcase
	for _, operation := range []string{"block", "audit"} {
		cases = append(cases,
			tcase{operation + ": ignore matching source", operation, "untrusted", []string{"untrusted"}, 0},
			tcase{operation + ": do not ignore non-matching source", operation, "trusted", []string{"untrusted"}, 1},
			tcase{operation + ": do not ignore null source", operation, nil, []string{"untrusted"}, 1},
			tcase{operation + ": no ignore-source configured", operation, "untrusted", []string{}, 1},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
			m := filterListMap(createEntry(t, "malware", php.ArrayOf("package", "acme/package", "constraint", "*", "source", tc.entrySource)))
			pc := policyConfig(t, php.ArrayOf("malware", php.ArrayOf("ignore-source", php.StringList(tc.ignoreSource))))

			var entries []*filterlist.FilterListEntry
			switch tc.operation {
			case "audit":
				entries = filterlist.FilterListAuditor{}.GetMatchingAuditEntries(p, m, pc)
			case "block":
				entries = filterlist.FilterListAuditor{}.GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate)
			}
			if len(entries) != tc.expectedCount {
				t.Errorf("got %d entries, want %d", len(entries), tc.expectedCount)
			}
		})
	}
}

func TestFilterListAuditor_GetMatchingEntriesKeepsNonIgnoredSourcesAndDropsIgnored(t *testing.T) {
	p := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
	m := filterListMap(
		createEntry(t, "malware", php.ArrayOf("package", "acme/package", "constraint", "*", "source", "untrusted")),
		createEntry(t, "malware", php.ArrayOf("package", "acme/package", "constraint", "*", "source", "trusted")),
	)
	pc := policyConfig(t, php.ArrayOf("malware", php.ArrayOf("ignore-source", php.ListOf("untrusted"))))

	entries := filterlist.FilterListAuditor{}.GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate)
	if len(entries) != 1 || entries[0].Source.S != "trusted" {
		t.Errorf("entries = %+v", entries)
	}
	// the map passed in is left untouched
	if byList, _ := m.Get("acme/package"); byList == nil {
		t.Error("input map modified")
	} else if list, _ := byList.Get("malware"); len(list) != 2 {
		t.Error("input map modified")
	}
}

func TestFilterListAuditor_GetMatchingEntriesIgnoresUnconfiguredLists(t *testing.T) {
	p := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
	m := filterListMap(createEntry(t, "unconfigured", php.ArrayOf("package", "acme/package", "constraint", "*")))
	pc := policyConfig(t, php.ArrayOf("list", php.ArrayOf("ignore", php.ListOf("acme/package"))))

	if entries := (filterlist.FilterListAuditor{}).GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate); len(entries) != 0 {
		t.Errorf("entries = %+v", entries)
	}
}

func TestFilterListAuditor_GetMatchingEntriesDropsUnconfiguredListEntriesForNonIgnoredPackage(t *testing.T) {
	p := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
	m := filterListMap(
		createEntry(t, "configured", php.ArrayOf("package", "acme/package", "constraint", "*")),
		createEntry(t, "unconfigured", php.ArrayOf("package", "acme/package", "constraint", "*")),
	)
	pc := policyConfig(t, php.ArrayOf("configured", true))

	entries := filterlist.FilterListAuditor{}.GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate)
	if len(entries) != 1 || entries[0].ListName != "configured" {
		t.Errorf("entries = %+v", entries)
	}
}
