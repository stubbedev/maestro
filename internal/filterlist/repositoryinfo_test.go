// Ports tests/Composer/Test/FilterList/ComposerRepositoryFilterInformationTest.php
// (MIT, testdata/LICENSE-composer).

package filterlist_test

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
)

func enabled(v bool) *php.Array { return php.ArrayOf("enabled", v) }

func assertLists(t *testing.T, info *filterlist.ComposerRepositoryFilterInformation, want ...string) {
	t.Helper()

	if !slices.Equal(info.Lists, want) {
		t.Errorf("lists = %q, want %q", info.Lists, want)
	}
}

func TestComposerRepositoryFilterInformation_FromDataPassesThroughCustomLists(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"metadata", true,
		"lists", php.ArrayOf("company-policy", enabled(true), "aikido", enabled(true)),
	), nil)

	if !info.Metadata {
		t.Error("metadata = false")
	}
	assertLists(t, info, "company-policy", "aikido")
}

func TestComposerRepositoryFilterInformation_FromDataSkipsListsNotEnabled(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"lists", php.ArrayOf("company-policy", enabled(true), "aikido", enabled(false)),
	), nil)
	assertLists(t, info, "company-policy")
}

func TestComposerRepositoryFilterInformation_FromDataSkipsListsWithMissingEnabledFlag(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"lists", php.ArrayOf("company-policy", enabled(true), "malware", php.NewArray()),
	), nil)
	assertLists(t, info, "company-policy")
}

func TestComposerRepositoryFilterInformation_FromDataSkipsListsWithScalarConfig(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"lists", php.ArrayOf("company-policy", enabled(true), "malware", true),
	), nil)
	assertLists(t, info, "company-policy")
}

func TestComposerRepositoryFilterInformation_FromDataDropsReservedListNamesAdvertisedByRepository(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"lists", php.ArrayOf("advisories", enabled(true), "company-policy", enabled(true), "abandoned", enabled(true)),
	), nil)
	assertLists(t, info, "company-policy")
}

func TestComposerRepositoryFilterInformation_FromDataDropsListNamesWithReservedPrefixAdvertisedByRepository(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf(
		"lists", php.ArrayOf("ignore-foo", enabled(true), "ignoremalware", enabled(true), "company-policy", enabled(true)),
	), nil)
	assertLists(t, info, "company-policy")
}

func malwareLists() *php.Array { return php.ArrayOf("malware", enabled(true)) }

func prefixExample(url string) string { return "https://example.org" + url }

func TestComposerRepositoryFilterInformation_FromDataDefaultsSummaryUrlToNull(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf("lists", malwareLists()), nil)
	if info.SummaryURL != "" {
		t.Errorf("summaryUrl = %q", info.SummaryURL)
	}
}

func TestComposerRepositoryFilterInformation_FromDataAppliesCanonicalizerToSummaryUrl(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf("lists", malwareLists(), "summary-url", "/p2/filter-summary.json"), prefixExample)
	if info.SummaryURL != "https://example.org/p2/filter-summary.json" {
		t.Errorf("summaryUrl = %q", info.SummaryURL)
	}
}

func TestComposerRepositoryFilterInformation_FromDataIgnoresNonStringSummaryUrl(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf("lists", malwareLists(), "summary-url", php.ListOf("oops")), func(url string) string { return url })
	if info.SummaryURL != "" {
		t.Errorf("summaryUrl = %q", info.SummaryURL)
	}
}

func TestComposerRepositoryFilterInformation_FromDataDefaultsApiUrlToNull(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf("lists", malwareLists()), nil)
	if info.APIURL != "" {
		t.Errorf("apiUrl = %q", info.APIURL)
	}
}

func TestComposerRepositoryFilterInformation_FromDataAppliesCanonicalizerToApiUrl(t *testing.T) {
	info := filterlist.ComposerRepositoryFilterInformationFromData(php.ArrayOf("lists", malwareLists(), "api-url", "/api/filter"), prefixExample)
	if info.APIURL != "https://example.org/api/filter" {
		t.Errorf("apiUrl = %q", info.APIURL)
	}
}
