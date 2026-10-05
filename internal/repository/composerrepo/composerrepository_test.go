// Ports tests/Composer/Test/Repository/ComposerRepositoryTest.php (MIT,
// testdata/LICENSE-composer).

package composerrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

func TestComposerRepository_LoadData(t *testing.T) {
	type nv struct{ name, version string }
	for label, tc := range map[string]struct {
		expected     []nv
		repoPackages *php.Array
	}{
		"old repository format": {
			[]nv{{"foo/bar", "1.0.0"}},
			php.ArrayOf("foo/bar", php.ArrayOf(
				"name", "foo/bar",
				"versions", php.ArrayOf("1.0.0", php.ArrayOf("name", "foo/bar", "version", "1.0.0")),
			)),
		},
		"new repository format": {
			[]nv{{"bar/foo", "3.14"}, {"bar/foo", "3.145"}},
			php.ArrayOf("packages", php.ArrayOf("bar/foo", php.ArrayOf(
				"3.14", php.ArrayOf("name", "bar/foo", "version", "3.14"),
				"3.145", php.ArrayOf("name", "bar/foo", "version", "3.145"),
			))),
		},
		"new repository format but without versions as keys should also be supported": {
			[]nv{{"bar/foo", "3.14"}, {"bar/foo", "3.145"}},
			php.ArrayOf("packages", php.ArrayOf("bar/foo", php.ListOf(
				php.ArrayOf("name", "bar/foo", "version", "3.14"),
				php.ArrayOf("name", "bar/foo", "version", "3.145"),
			))),
		},
	} {
		t.Run(label, func(t *testing.T) {
			repo := newRepo(t, php.ArrayOf("url", "http://example.org"), createConfig(t), httpmock.New())
			// the mocked loadRootServerFile
			repo.rootData, repo.rootLoaded = tc.repoPackages, true

			// Triggers initialization
			packages := must(repo.Packages())

			// Final sanity check, ensure the correct number of packages were added.
			if len(packages) != len(tc.expected) {
				t.Fatalf("got %d packages, want %d", len(packages), len(tc.expected))
			}

			for i, p := range tc.expected {
				if got := packages[i].Name() + " " + packages[i].PrettyVersion(); got != p.name+" "+p.version {
					t.Errorf("#%d: got %q, want %q", i, got, p.name+" "+p.version)
				}
			}
		})
	}
}

func TestComposerRepository_WhatProvides(t *testing.T) {
	body := encode(t, php.ArrayOf("packages", php.ListOf(
		php.ListOf(php.ArrayOf(
			"uid", 1,
			"name", "a",
			"version", "dev-master",
			"extra", php.ArrayOf("branch-alias", php.ArrayOf("dev-master", "1.0.x-dev")),
		)),
		php.ListOf(php.ArrayOf(
			"uid", 2,
			"name", "a",
			"version", "dev-develop",
			"extra", php.ArrayOf("branch-alias", php.ArrayOf("dev-develop", "1.1.x-dev")),
		)),
		php.ListOf(php.ArrayOf(
			"uid", 3,
			"name", "a",
			"version", "0.6",
		)),
	)))
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])

	// Composer mocks fetchFile; here the listing carries the body's real
	// hash so that the real fetchFile accepts it
	repo := newRepo(t, php.ArrayOf("url", "https://dummy.test.link"), createConfig(t),
		mock(t, httpmock.Expectation{URL: "https://dummy.test.link/to/a/file", Body: body}))
	repo.providerListing = &repository.NameMap[string]{}
	repo.providerListing.Set("a", hash)
	repo.providersURL = "https://dummy.test.link/to/%package%/file"

	packages := must(repo.whatProvides("a", nil, nil, nil))

	if got, want := packages.Keys(), []string{"1", "1-alias", "2", "2-alias", "3"}; !slices.Equal(got, want) {
		t.Fatalf("keys %v, want %v", got, want)
	}
	two, _ := packages.Get("2")
	twoAlias, _ := packages.Get("2-alias")
	if twoAlias.(pkg.Alias).AliasOf() != two {
		t.Error("2-alias is not an alias of 2")
	}
}

func searchResults(results []repository.SearchResult) *php.Array {
	out := php.NewArray()
	for _, r := range results {
		if r.Raw != nil {
			out.Append(r.Raw)

			continue
		}
		out.Append(php.ArrayOf("name", r.Name, "description", r.Description.Value()))
	}

	return out
}

func assertSameJSON(t *testing.T, want, got any) {
	t.Helper()

	w, _ := php.JSONEncode(want, 0)
	g, _ := php.JSONEncode(got, 0)
	if w != g {
		t.Errorf("got %s\nwant %s", g, w)
	}
}

func TestComposerRepository_SearchWithType(t *testing.T) {
	result := php.ArrayOf("results", php.ListOf(php.ArrayOf("name", "foo", "description", nil)))

	httpDownloader := mock(t,
		httpmock.Expectation{URL: "http://example.org/packages.json", Body: encode(t, php.ArrayOf("search", "/search.json?q=%query%&type=%type%"))},
		httpmock.Expectation{URL: "http://example.org/search.json?q=foo&type=composer-plugin", Body: encode(t, result)},
		httpmock.Expectation{URL: "http://example.org/search.json?q=foo&type=library", Body: encode(t, php.NewArray())},
	)

	repo := newRepo(t, php.ArrayOf("url", "http://example.org"), createConfig(t, "cache-read-only", true), httpDownloader)

	got := must(repo.Search("foo", repository.SearchFulltext, "composer-plugin"))
	assertSameJSON(t, php.ListOf(php.ArrayOf("name", "foo", "description", nil)), searchResults(got))

	if got := must(repo.Search("foo", repository.SearchFulltext, "library")); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func TestComposerRepository_SearchWithSpecialChars(t *testing.T) {
	httpDownloader := mock(t,
		httpmock.Expectation{URL: "http://example.org/packages.json", Body: encode(t, php.ArrayOf("search", "/search.json?q=%query%&type=%type%"))},
		httpmock.Expectation{URL: "http://example.org/search.json?q=foo+bar&type=", Body: encode(t, php.NewArray())},
	)

	repo := newRepo(t, php.ArrayOf("url", "http://example.org"), createConfig(t, "cache-read-only", true), httpDownloader)

	if got := must(repo.Search("foo bar", repository.SearchFulltext, "")); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func TestComposerRepository_SearchWithAbandonedPackages(t *testing.T) {
	result := php.ArrayOf("results", php.ListOf(
		php.ArrayOf("name", "foo1", "description", nil, "abandoned", true),
		php.ArrayOf("name", "foo2", "description", nil, "abandoned", "bar"),
	))

	httpDownloader := mock(t,
		httpmock.Expectation{URL: "http://example.org/packages.json", Body: encode(t, php.ArrayOf("search", "/search.json?q=%query%"))},
		httpmock.Expectation{URL: "http://example.org/search.json?q=foo", Body: encode(t, result)},
	)

	repo := newRepo(t, php.ArrayOf("url", "http://example.org"), createConfig(t, "cache-read-only", true), httpDownloader)

	got := must(repo.Search("foo", repository.SearchFulltext, ""))
	assertSameJSON(t, php.ListOf(
		php.ArrayOf("name", "foo1", "description", nil, "abandoned", true),
		php.ArrayOf("name", "foo2", "description", nil, "abandoned", "bar"),
	), searchResults(got))
	if got[0].Abandoned != true || got[1].Abandoned != "bar" || got[0].Description.Valid {
		t.Errorf("fields: %+v", got)
	}
}

func TestComposerRepository_CanonicalizeUrl(t *testing.T) {
	for _, tc := range []struct{ expected, url, repositoryURL string }{
		{"https://example.org/path/to/file", "/path/to/file", "https://example.org"},
		{"https://example.org/canonic_url", "https://example.org/canonic_url", "https://should-not-see-me.test"},
		{"file:///path/to/repository/file", "/path/to/repository/file", "file:///path/to/repository"},
		// Assert that the repository URL is returned unchanged if it is
		// not a URL.
		// (Backward compatibility test)
		{"invalid_repo_url", "/path/to/file", "invalid_repo_url"},
		// Assert that URLs can contain sequences resembling pattern
		// references as understood by preg_replace() without messing up
		// the result.
		// (Regression test)
		{"https://example.org/path/to/unusual_$0_filename", "/path/to/unusual_$0_filename", "https://example.org"},
	} {
		repo := newRepo(t, php.ArrayOf("url", tc.repositoryURL), createConfig(t), httpmock.New())

		// ComposerRepository::__construct ensures that the repository URL has a
		// protocol, so reset it here in order to test all cases.
		repo.url = tc.repositoryURL

		if got := must(repo.canonicalizeURL(tc.url)); got != tc.expected {
			t.Errorf("canonicalizeUrl(%q) on %q = %q, want %q", tc.url, tc.repositoryURL, got, tc.expected)
		}
	}
}

func TestComposerRepository_GetProviderNamesWillReturnPartialPackageNames(t *testing.T) {
	httpDownloader := mock(t, httpmock.Expectation{
		URL: "http://example.org/packages.json",
		Body: encode(t, php.ArrayOf(
			"providers-lazy-url", "/foo/p/%package%.json",
			"packages", php.ArrayOf("foo/bar", php.ArrayOf(
				"dev-branch", php.ArrayOf("name", "foo/bar"),
				"v1.0.0", php.ArrayOf("name", "foo/bar"),
			)),
		)),
	})

	repo := newRepo(t, php.ArrayOf("url", "http://example.org/packages.json"), createConfig(t), httpDownloader)

	if got := must(repo.PackageNames("")); !slices.Equal(got, []string{"foo/bar"}) {
		t.Errorf("got %v", got)
	}
}

func securityAdvisoriesPostOptions(names ...string) *php.Array {
	pairs := make([]string, 0, 2*len(names))
	for i, name := range names {
		pairs = append(pairs, "packages["+string(rune('0'+i))+"]", name)
	}

	return php.ArrayOf("http", php.ArrayOf(
		"verify_peer", false,
		"method", "POST",
		"header", php.ListOf("Content-type: application/x-www-form-urlencoded"),
		"timeout", 10,
		"content", http.HTTPBuildQuery(pairs...),
	))
}

func TestComposerRepository_GetSecurityAdvisoriesAssertRepositoryHttpOptionsAreUsed(t *testing.T) {
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL: "https://example.org/packages.json",
			Body: encode(t, php.ArrayOf(
				"packages", php.ArrayOf("foo/bar", php.ArrayOf(
					"dev-branch", php.ArrayOf("name", "foo/bar"),
					"v1.0.0", php.ArrayOf("name", "foo/bar"),
				)),
				"metadata-url", "https://example.org/p2/%package%.json",
				"security-advisories", php.ArrayOf("api-url", "https://example.org/security-advisories"),
			)),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/security-advisories",
			Body:    encode(t, php.ArrayOf("advisories", php.NewArray())),
			Options: securityAdvisoriesPostOptions("foo/bar"),
		},
	)

	repo := newRepo(t, php.ArrayOf("url", "https://example.org/packages.json", "options", verifyPeerOptions()), createConfig(t), httpDownloader)

	result := must(repo.SecurityAdvisories(repository.NewConstraintMap("foo/bar", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), false))
	if len(result.NamesFound) != 0 || result.Advisories.Len() != 0 {
		t.Errorf("got %+v", result)
	}
}

// generateSecurityAdvisory is generateSecurityAdvisory().
func generateSecurityAdvisory(id, packageName string, cve any, affectedVersions string) *php.Array {
	return php.ArrayOf(
		"advisoryId", id,
		"packageName", packageName,
		"remoteId", "test",
		"title", "Security Advisory",
		"link", nil,
		"cve", cve,
		"affectedVersions", affectedVersions,
		"source", "Tests",
		"reportedAt", "2024-04-31 12:37:47",
		"composerRepository", "Package Repository",
		"severity", "high",
		"sources", php.ListOf(php.ArrayOf("name", "Security Advisory", "remoteId", "test")),
	)
}

func TestComposerRepository_GetSecurityAdvisoriesAssertRepositoryAdvisoriesIsZeroIndexedArrayWithConsecutiveKeys(t *testing.T) {
	packageName := "foo/bar"
	advisory1 := generateSecurityAdvisory("PKSA-1", packageName, "CVE-1999-1000", ">=1.0.0,<1.1.0")
	advisory2 := generateSecurityAdvisory("PKSA-2", packageName, "CVE-1999-1000", ">=2.0.0")
	advisory3 := generateSecurityAdvisory("PKSA-3", packageName, "CVE-1999-1000", ">=1.0.0,<1.1.0")

	expectedPackageAdvisories := []*php.Array{advisory1, advisory3}

	httpDownloader := mock(t,
		httpmock.Expectation{
			URL: "https://example.org/packages.json",
			Body: encode(t, php.ArrayOf(
				"packages", php.ArrayOf(packageName, php.ArrayOf(
					"dev-branch", php.ArrayOf("name", packageName),
					"v1.0.0", php.ArrayOf("name", packageName),
				)),
				"metadata-url", "https://example.org/p2/%package%.json",
				"security-advisories", php.ArrayOf("api-url", "https://example.org/security-advisories"),
			)),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/security-advisories",
			Body:    encode(t, php.ArrayOf("advisories", php.ArrayOf(packageName, php.ListOf(advisory1, advisory2, advisory3)))),
			Options: securityAdvisoriesPostOptions(packageName),
		},
	)

	repo := newRepo(t, php.ArrayOf("url", "https://example.org/packages.json", "options", verifyPeerOptions()), createConfig(t), httpDownloader)

	result := must(repo.SecurityAdvisories(repository.NewConstraintMap(packageName, semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), false))
	actualPackageAdvisories, ok := result.Advisories.Get(packageName)
	if !ok {
		t.Fatal("no advisories for " + packageName)
	}
	if len(actualPackageAdvisories) != len(expectedPackageAdvisories) {
		t.Fatalf("got %d advisories, want %d", len(actualPackageAdvisories), len(expectedPackageAdvisories))
	}
	for i, expectedAdvisory := range expectedPackageAdvisories {
		if want, _ := expectedAdvisory.GetString("advisoryId"); actualPackageAdvisories[i].Partial().AdvisoryID != want {
			t.Errorf("#%d: %s, want %s", i, actualPackageAdvisories[i].Partial().AdvisoryID, want)
		}
	}
}

func filterEntry(packageName string, constraint semver.ConstraintInterface, listName, url, reason, id string) *repository.FilterListEntry {
	return &repository.FilterListEntry{PackageName: packageName, ListName: listName, Constraint: constraint, URL: pkg.Str(url), Reason: pkg.Str(reason), ID: pkg.Str(id)}
}

func assertEntries(t *testing.T, filter *repository.NameMap[[]*repository.FilterListEntry], want map[string][]*repository.FilterListEntry) {
	t.Helper()

	if filter.Len() != len(want) {
		t.Fatalf("got lists %v, want %d", filter.Keys(), len(want))
	}
	for listName, wantEntries := range want {
		entries, _ := filter.Get(listName)
		if len(entries) != len(wantEntries) {
			t.Fatalf("%s: got %d entries, want %d", listName, len(entries), len(wantEntries))
		}
		for i, e := range entries {
			w := wantEntries[i]
			if e.PackageName != w.PackageName || e.ListName != w.ListName || e.URL != w.URL || e.Reason != w.Reason || e.ID != w.ID || e.Source != w.Source ||
				e.Constraint.PrettyString() != w.Constraint.PrettyString() || e.Constraint.String() != w.Constraint.String() {
				t.Errorf("%s #%d: got %+v, want %+v", listName, i, e, w)
			}
		}
	}
}

func filterPackagesJSON(t *testing.T, filter *php.Array) string {
	t.Helper()

	return encode(t, php.ArrayOf("metadata-url", "https://example.org/p2/%package%.json", "filter", filter))
}

func filterRepoConfig() *php.Array {
	return php.ArrayOf("url", "https://example.org/packages.json", "options", verifyPeerOptions())
}

func TestComposerRepository_GetFilterWithMatchingLists(t *testing.T) {
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", php.ArrayOf("test", php.ArrayOf("enabled", true)))),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL: "https://example.org/p2/acme/package.json",
			Body: encode(t, php.ArrayOf("filter", php.ArrayOf("test", php.ListOf(php.ArrayOf(
				"constraint", "*",
				"url", "https://example.org/acme/package/filters.json",
				"reason", "Malicious code detected",
				"id", "ID-test",
			))))),
			Options: verifyPeerOptions(),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	filter := must(repo.Filter(repository.NewConstraintMap("acme/package", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"test"}))

	constraint := semver.NewMatchAllConstraint()
	constraint.SetPrettyString("*")
	assertEntries(t, filter, map[string][]*repository.FilterListEntry{"test": {filterEntry("acme/package", constraint, "test", "https://example.org/acme/package/filters.json", "Malicious code detected", "ID-test")}})
}

func TestComposerRepository_UserFilterDisabledFalseShortCircuitsHasFilterAndGetFilterLists(t *testing.T) {
	// No HTTP requests should be issued when the user has set `filter: false`,
	// so we configure the mock with no expectations.
	httpDownloader := mock(t)

	repo := newRepo(t, php.ArrayOf("url", "https://example.org/packages.json", "filter", false), createConfig(t), httpDownloader)

	if must(repo.HasFilter()) {
		t.Error("hasFilter true")
	}
	if lists := must(repo.FilterLists()); len(lists) != 0 {
		t.Errorf("lists %v", lists)
	}
}

func TestComposerRepository_UserFilterPerListOptOut(t *testing.T) {
	httpDownloader := mock(t, httpmock.Expectation{
		URL: "https://example.org/packages.json",
		Body: filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", php.ArrayOf(
			"malware", php.ArrayOf("enabled", true),
			"typosquatting", php.ArrayOf("enabled", true),
			"deprecated", php.ArrayOf("enabled", true),
		))),
		Options: verifyPeerOptions(),
	})

	repo := newRepo(t, php.ArrayOf(
		"url", "https://example.org/packages.json",
		"options", verifyPeerOptions(),
		"filter", php.ArrayOf(
			"typosquatting", false,
			// Opting out of a list this repo doesn't advertise is harmless.
			"unknown-list", false,
		),
	), createConfig(t), httpDownloader)

	if !must(repo.HasFilter()) {
		t.Error("hasFilter false")
	}
	if lists := must(repo.FilterLists()); !slices.Equal(lists, []string{"malware", "deprecated"}) {
		t.Errorf("lists %v", lists)
	}
}

func TestComposerRepository_UserFilterAcceptsTrueAsNoOp(t *testing.T) {
	// `true` is undocumented but accepted silently so layered configs can
	// round-trip a key without losing data; it has no effect because
	// unmentioned lists are already enabled.
	httpDownloader := mock(t, httpmock.Expectation{
		URL: "https://example.org/packages.json",
		Body: filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", php.ArrayOf(
			"malware", php.ArrayOf("enabled", true),
			"typosquatting", php.ArrayOf("enabled", true),
		))),
		Options: verifyPeerOptions(),
	})

	repo := newRepo(t, php.ArrayOf(
		"url", "https://example.org/packages.json",
		"options", verifyPeerOptions(),
		"filter", php.ArrayOf("malware", true, "typosquatting", false),
	), createConfig(t), httpDownloader)

	if !must(repo.HasFilter()) {
		t.Error("hasFilter false")
	}
	if lists := must(repo.FilterLists()); !slices.Equal(lists, []string{"malware"}) {
		t.Errorf("lists %v", lists)
	}
}

func malwareFilter(t *testing.T, url, reason, id string) string {
	t.Helper()

	return encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ListOf(php.ArrayOf(
		"constraint", "*",
		"url", url,
		"reason", reason,
		"id", id,
	)))))
}

func summaryPackagesJSON(t *testing.T, lists *php.Array) string {
	t.Helper()

	return filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", lists, "summary-url", "/lists/all/summary.json"))
}

func TestComposerRepository_GetFilterSkipsMetadataFetchesForPackagesNotInSummary(t *testing.T) {
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    summaryPackagesJSON(t, php.ArrayOf("malware", php.ArrayOf("enabled", true))),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/lists/all/summary.json",
			Body:    encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ArrayOf("evil/pkg", "^1.0")))),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/p2/evil/pkg.json",
			Body:    malwareFilter(t, "https://example.org/evil/pkg/filters.json", "Confirmed malware", "ID-test"),
			Options: verifyPeerOptions(),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	constraints := repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0"))
	constraints.Set("safe/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0"))
	filter := must(repo.Filter(constraints, []string{"malware"}))

	entries, ok := filter.Get("malware")
	if !ok || len(entries) != 1 || entries[0].PackageName != "evil/pkg" {
		t.Errorf("got %v %+v", filter.Keys(), entries)
	}
}

func TestComposerRepository_GetFilterSkipsSummaryListsNotInConfiguredLists(t *testing.T) {
	// typosquatting list is in the summary but not in configuredLists; no metadata fetch should happen.
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    summaryPackagesJSON(t, php.ArrayOf("malware", php.ArrayOf("enabled", true), "typosquatting", php.ArrayOf("enabled", true))),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/lists/all/summary.json",
			Body:    encode(t, php.ArrayOf("filter", php.ArrayOf("typosquatting", php.ArrayOf("lookalike/pkg", "*")))),
			Options: verifyPeerOptions(),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	filter := must(repo.Filter(repository.NewConstraintMap("lookalike/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))
	if filter.Len() != 0 {
		t.Errorf("got %v", filter.Keys())
	}
}

func TestComposerRepository_GetFilterSkipsPackagesWithNonIntersectingSummaryConstraint(t *testing.T) {
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    summaryPackagesJSON(t, php.ArrayOf("malware", php.ArrayOf("enabled", true))),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/lists/all/summary.json",
			Body:    encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ArrayOf("evil/pkg", "^1.0")))),
			Options: verifyPeerOptions(),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	// Requested constraint =2.5.0 does not intersect summary constraint ^1.0; no metadata fetch.
	filter := must(repo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "2.5.0.0")), []string{"malware"}))
	if filter.Len() != 0 {
		t.Errorf("got %v", filter.Keys())
	}
}

func TestComposerRepository_GetFilterSkipsSummaryWhenMetadataAlreadyFetched(t *testing.T) {
	// Only one fetch of each URL is expected: the second getFilter() call must skip the
	// summary entirely (freshMetadataUrls is non-empty) and short-circuit the metadata fetch.
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    summaryPackagesJSON(t, php.ArrayOf("malware", php.ArrayOf("enabled", true))),
			Headers: []string{"Last-Modified: Tue, 01 Jan 2099 00:00:00 GMT"},
		},
		httpmock.Expectation{
			URL:     "https://example.org/lists/all/summary.json",
			Body:    encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ArrayOf("evil/pkg", "*")))),
			Headers: []string{"Last-Modified: Tue, 01 Jan 2099 00:00:00 GMT"},
		},
		httpmock.Expectation{
			URL:     "https://example.org/p2/evil/pkg.json",
			Body:    malwareFilter(t, "https://example.org/evil/pkg/filters.json", "Confirmed malware", "ID-test"),
			Headers: []string{"Last-Modified: Tue, 01 Jan 2030 00:00:00 GMT"},
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	firstFilter := must(repo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))
	if entries, _ := firstFilter.Get("malware"); len(entries) != 1 {
		t.Fatalf("first: %d entries", len(entries))
	}

	// Second call on the same instance: freshMetadataUrls is populated, so no further HTTP requests should be issued.
	secondFilter := must(repo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))
	entries, _ := secondFilter.Get("malware")
	if len(entries) != 1 || entries[0].PackageName != "evil/pkg" {
		t.Fatalf("second: %+v", entries)
	}
}

func TestComposerRepository_GetFilterReusesCachedSummaryOn304(t *testing.T) {
	config := createConfig(t)
	repoArgs := filterRepoConfig()

	packagesJSONBody := summaryPackagesJSON(t, php.ArrayOf("malware", php.ArrayOf("enabled", true)))
	summaryBody := encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ArrayOf("evil/pkg", "*"))))
	metadataBody := malwareFilter(t, "https://example.org/evil/pkg/filters.json", "Confirmed malware", "ID-test")
	lastModified := []string{"Last-Modified: Tue, 01 Jan 2030 00:00:00 GMT"}

	httpDownloader := mock(t,
		// First call: fresh fetches, populating the on-disk cache.
		httpmock.Expectation{URL: "https://example.org/packages.json", Body: packagesJSONBody, Headers: lastModified},
		httpmock.Expectation{URL: "https://example.org/lists/all/summary.json", Body: summaryBody, Headers: lastModified},
		httpmock.Expectation{URL: "https://example.org/p2/evil/pkg.json", Body: metadataBody, Headers: lastModified},
		// Second call: cache age on packages.json keeps it cache-fresh; summary + metadata revalidate via 304.
		httpmock.Expectation{URL: "https://example.org/lists/all/summary.json", Status: 304},
		httpmock.Expectation{URL: "https://example.org/p2/evil/pkg.json", Status: 304},
	)

	firstRepo := newRepo(t, repoArgs, config, httpDownloader)
	firstFilter := must(firstRepo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))
	if entries, _ := firstFilter.Get("malware"); len(entries) != 1 {
		t.Fatalf("first: %d entries", len(entries))
	}

	secondRepo := newRepo(t, repoArgs, config, httpDownloader)
	secondFilter := must(secondRepo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))

	entries, _ := secondFilter.Get("malware")
	if len(entries) != 1 || entries[0].PackageName != "evil/pkg" {
		t.Fatalf("second: %+v", entries)
	}
}

func TestComposerRepository_GetFilterUsesApiUrlInsteadOfSummary(t *testing.T) {
	expectedAPIRequestBody, _ := php.JSONEncode(php.ArrayOf(
		"packages", php.ListOf("pkg://composer/evil/pkg", "pkg://composer/safe/pkg"),
		"lists", php.ListOf("malware"),
	), 0)

	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", php.ArrayOf("malware", php.ArrayOf("enabled", true)), "api-url", "/api/filter")),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL: "https://example.org/api/filter",
			Options: php.ArrayOf("http", php.ArrayOf(
				"verify_peer", false,
				"method", "POST",
				"header", php.ListOf("Content-type: application/json"),
				"timeout", 10,
				"content", expectedAPIRequestBody,
			)),
			Body: encode(t, php.ArrayOf("filter", php.ArrayOf("malware", php.ListOf(php.ArrayOf(
				"package", "evil/pkg",
				"constraint", "*",
				"url", "https://example.org/evil/pkg/filters.json",
				"reason", "Confirmed malware",
				"id", "ID-api",
			))))),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	constraints := repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0"))
	constraints.Set("safe/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0"))
	filter := must(repo.Filter(constraints, []string{"malware"}))

	entries, _ := filter.Get("malware")
	if len(entries) != 1 || entries[0].PackageName != "evil/pkg" || entries[0].ID != pkg.Str("ID-api") {
		t.Fatalf("got %+v", entries)
	}
}

func TestComposerRepository_GetFilterSkipsApiUrlWhenMetadataAlreadyFetched(t *testing.T) {
	// When per-package metadata has already been fetched in this run, api-url must be
	// skipped: the existing per-package metadata loop will short-circuit on the cache and
	// surface the filter entries from there. We simulate that prior fetch by seeding
	// freshMetadataUrls.
	httpDownloader := mock(t,
		httpmock.Expectation{
			URL:     "https://example.org/packages.json",
			Body:    filterPackagesJSON(t, php.ArrayOf("metadata", true, "lists", php.ArrayOf("malware", php.ArrayOf("enabled", true)), "api-url", "/api/filter")),
			Options: verifyPeerOptions(),
		},
		httpmock.Expectation{
			URL:     "https://example.org/p2/evil/pkg.json",
			Body:    malwareFilter(t, "https://example.org/evil/pkg/filters.json", "From metadata", "ID-meta"),
			Options: verifyPeerOptions(),
		},
	)

	repo := newRepo(t, filterRepoConfig(), createConfig(t), httpDownloader)

	// Pretend a previous metadata fetch has already happened in this run.
	repo.freshMetadataUrls = map[string]struct{}{"https://example.org/p2/some-other/pkg.json": {}}

	filter := must(repo.Filter(repository.NewConstraintMap("evil/pkg", semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")), []string{"malware"}))

	// api-url POST is not in the expectations list — strict mode would fail if it was hit.
	// The filter entry comes from the per-package metadata file, not from api-url.
	entries, _ := filter.Get("malware")
	if len(entries) != 1 || entries[0].ID != pkg.Str("ID-meta") {
		t.Fatalf("got %+v", entries)
	}
}
