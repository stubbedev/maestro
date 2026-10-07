package resolver

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

// Ports tests/Composer/Test/DependencyResolver/SecurityAdvisoryPoolFilterTest.php
// and FilterListPoolFilterTest.php.

func newPackageRepository(t testing.TB, config *php.Array) *repository.PackageRepository {
	t.Helper()
	repo, err := repository.NewPackageRepository(config)
	if err != nil {
		t.Fatal(err)
	}

	return repo
}

func newBufferIO(t testing.TB) *io.BufferIO {
	t.Helper()
	out, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func eq(version string) *semver.Constraint { return semver.NewConstraintOp(semver.OpEQ, version) }

func assertPackages(t *testing.T, expected, got []pkg.PackageInterface) {
	t.Helper()
	if !slices.Equal(expected, got) {
		t.Fatalf("packages %v, want %v", got, expected)
	}
}

func advisoryPolicyConfig(advisoriesBlock, abandonedBlock bool, advisoriesIgnore, abandonedIgnore *policy.IgnoreMap) *policy.PolicyConfig {
	return &policy.PolicyConfig{
		Enabled:           true,
		Advisories:        policy.NewAdvisoriesPolicyConfig(advisoriesBlock, policy.AuditFail, advisoriesIgnore, nil, nil),
		Malware:           policy.MalwarePolicyConfigDisabled(),
		Abandoned:         policy.NewAbandonedPolicyConfig(abandonedBlock, policy.AuditFail, abandonedIgnore),
		CustomLists:       &policy.OrderedMap[*policy.CustomListPolicyConfig]{},
		IgnoreUnreachable: policy.IgnoreUnreachableDefault(),
	}
}

func generateSecurityAdvisory(id, packageName, cve, affectedVersions string) *php.Array {
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

func advisoryRepository(t testing.TB, advisories ...*php.Array) *repository.PackageRepository {
	t.Helper()
	list := php.NewArray()
	for _, a := range advisories {
		list.Append(a)
	}

	return newPackageRepository(t, php.ArrayOf("package", php.NewArray(), "security-advisories", php.ArrayOf("acme/package", list)))
}

func TestSecurityAdvisoryPoolFilter_FilterPackagesByAdvisories(t *testing.T) {
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, advisoryPolicyConfig(true, true, nil, nil), nullIO)
	advisory1 := generateSecurityAdvisory("PKSA-1", "acme/package", "CVE-1999-1000", ">=1.0.0,<1.1.0")
	advisory2 := generateSecurityAdvisory("PKSA-2", "acme/package", "CVE-1999-1001", ">=1.0.0,<1.1.0")
	repo := advisoryRepository(t, advisory1, advisory2)

	expectedPackage1 := pkg.NewPackage("acme/package", "2.0.0.0", "2.0")
	expectedPackage2 := pkg.NewPackage("acme/other", "1.0.0.0", "1.0")
	pool := NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0"), expectedPackage1, expectedPackage2}, nil, nil)
	filteredPool, err := filter.Filter(pool, []repository.RepositoryInterface{repo}, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
	if !filteredPool.IsSecurityRemovedPackageVersion("acme/package", eq("1.0.0.0")) {
		t.Fatal("1.0.0.0 not security removed")
	}
	if filteredPool.AllAbandonedRemovedPackageVersions().Len() != 0 {
		t.Fatal("abandoned versions removed")
	}

	advisoryMap := filteredPool.AllSecurityRemovedPackageVersions()
	versions, ok := advisoryMap.Get("acme/package")
	if !ok || !versions.Has("1.0.0.0") {
		t.Fatalf("advisory map %v", advisoryMap.Keys())
	}
	if got := filteredPool.SecurityAdvisoryIdentifiersForPackageVersion("acme/package", eq("1.0.0.0")); !slices.Equal(got, []string{"PKSA-1", "PKSA-2"}) {
		t.Fatalf("identifiers %v", got)
	}
}

// TestSecurityAdvisoryPoolFilter_Parallel runs the filter tests with the
// matching advisories computed on several goroutines whatever the pool
// size.
func TestSecurityAdvisoryPoolFilter_Parallel(t *testing.T) {
	defer func(n, chunk int) { minParallelPackages, parallelChunk = n, chunk }(minParallelPackages, parallelChunk)
	minParallelPackages, parallelChunk = 1, 1

	t.Run("FilterPackagesByAdvisories", TestSecurityAdvisoryPoolFilter_FilterPackagesByAdvisories)
	t.Run("DontFilterPackagesByIgnoredAdvisories", TestSecurityAdvisoryPoolFilter_DontFilterPackagesByIgnoredAdvisories)
	t.Run("DontFilterPackagesWithAbandonedPackage", TestSecurityAdvisoryPoolFilter_DontFilterPackagesWithAbandonedPackage)
}

func TestSecurityAdvisoryPoolFilter_DontFilterPackagesByIgnoredAdvisories(t *testing.T) {
	ignoreID := &policy.OrderedMap[*policy.IgnoreIDRule]{}
	ignoreID.Set("CVE-2024-1234", policy.NewIgnoreIDRule("CVE-2024-1234", nil, true, true))
	policyConfig := &policy.PolicyConfig{
		Enabled:           true,
		Advisories:        policy.NewAdvisoriesPolicyConfig(true, policy.AuditFail, nil, ignoreID, nil),
		Malware:           policy.MalwarePolicyConfigDisabled(),
		Abandoned:         policy.NewAbandonedPolicyConfig(true, policy.AuditFail, nil),
		CustomLists:       &policy.OrderedMap[*policy.CustomListPolicyConfig]{},
		IgnoreUnreachable: policy.IgnoreUnreachableDefault(),
	}
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, policyConfig, nullIO)
	repo := advisoryRepository(t, generateSecurityAdvisory("PKSA-1", "acme/package", "CVE-2024-1234", ">=1.0.0,<1.1.0"))

	expectedPackage1 := pkg.NewPackage("acme/package", "1.0.0.0", "1.0")
	expectedPackage2 := pkg.NewPackage("acme/package", "1.1.0.0", "1.1")
	pool := NewPool([]pkg.PackageInterface{expectedPackage1, expectedPackage2}, nil, nil)
	filteredPool, err := filter.Filter(pool, []repository.RepositoryInterface{repo}, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
	if filteredPool.AllAbandonedRemovedPackageVersions().Len() != 0 || filteredPool.AllSecurityRemovedPackageVersions().Len() != 0 {
		t.Fatal("versions removed")
	}
}

func TestSecurityAdvisoryPoolFilter_DontFilterPackagesWithBlockInsecureDisabled(t *testing.T) {
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, advisoryPolicyConfig(false, true, nil, nil), nullIO)
	repo := advisoryRepository(t, generateSecurityAdvisory("PKSA-1", "acme/package", "CVE-2024-1234", ">=1.0.0,<1.1.0"))

	expectedPackage1 := pkg.NewPackage("acme/package", "1.0.0.0", "1.0")
	expectedPackage2 := pkg.NewPackage("acme/package", "1.1.0.0", "1.1")
	pool := NewPool([]pkg.PackageInterface{expectedPackage1, expectedPackage2}, nil, nil)
	filteredPool, err := filter.Filter(pool, []repository.RepositoryInterface{repo}, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
	if filteredPool.AllAbandonedRemovedPackageVersions().Len() != 0 || filteredPool.AllSecurityRemovedPackageVersions().Len() != 0 {
		t.Fatal("versions removed")
	}
}

func TestSecurityAdvisoryPoolFilter_DontFilterPackagesWithAbandonedPackage(t *testing.T) {
	packageNameIgnoreAbandoned := "acme/ignore-abandoned"
	abandonedIgnore := &policy.IgnoreMap{}
	abandonedIgnore.Set(packageNameIgnoreAbandoned, []*policy.IgnorePackageRule{policy.NewIgnorePackageRule(packageNameIgnoreAbandoned, semver.NewMatchAllConstraint(), nil, true, true)})
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, advisoryPolicyConfig(true, true, nil, abandonedIgnore), nullIO)

	abandonedPackage := pkg.NewCompletePackage("acme/package", "1.0.0.0", "1.0")
	abandonedPackage.SetAbandoned(true)

	ignoreAbandonedPackage := pkg.NewCompletePackage(packageNameIgnoreAbandoned, "1.0.0.0", "1.0")
	ignoreAbandonedPackage.SetAbandoned(true)

	expectedPackage := pkg.NewPackage("acme/other", "1.1.0.0", "1.1")
	pool := NewPool([]pkg.PackageInterface{expectedPackage, abandonedPackage, ignoreAbandonedPackage}, nil, nil)
	filteredPool, err := filter.Filter(pool, nil, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage, ignoreAbandonedPackage}, filteredPool.Packages())
	if filteredPool.AllAbandonedRemovedPackageVersions().Len() != 1 || filteredPool.AllSecurityRemovedPackageVersions().Len() != 0 {
		t.Fatal("wrong removals")
	}
}

// unreachableRepo is a PackageRepository whose advisories and filter
// lists cannot be fetched.
type unreachableRepo struct {
	*repository.PackageRepository
	name string
	err  error
}

func (r *unreachableRepo) RepoName() string { return r.name }

func (r *unreachableRepo) SecurityAdvisories(*repository.ConstraintMap, bool) (repository.AdvisoryResult, error) {
	return repository.AdvisoryResult{}, r.err
}

func (r *unreachableRepo) Filter(*repository.ConstraintMap, []string) (*repository.NameMap[[]*repository.FilterListEntry], error) {
	return nil, r.err
}

func TestSecurityAdvisoryPoolFilter_WarnsWhenUnreachableRepositoriesAreIgnored(t *testing.T) {
	unreachable := &unreachableRepo{
		PackageRepository: advisoryRepository(t, generateSecurityAdvisory("PKSA-test", "acme/package", "CVE-2024-9999", ">=1.0.0,<2.0.0")),
		name:              "unreachable advisory repo",
		err:               util.NewTransportError(`The "https://example.org/security.json" file could not be downloaded: HTTP/1.1 502 Bad Gateway`, 502),
	}

	// ignore-unreachable defaults to ["update", "install"], so the transport error is swallowed.
	out := newBufferIO(t)
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, advisoryPolicyConfig(true, true, nil, nil), out)
	if _, err := filter.Filter(NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, nil, nil), []repository.RepositoryInterface{unreachable}, NewRequest(nil)); err != nil {
		t.Fatal(err)
	}

	output := out.Output()
	for _, want := range []string{"Security advisory data could not be fetched from some repositories", "HTTP/1.1 502 Bad Gateway"} {
		if !strings.Contains(output, want) {
			t.Errorf("%q does not contain %q", output, want)
		}
	}
}

func TestSecurityAdvisoryPoolFilter_RethrowsTransportErrorWhenUnreachableIsNotIgnored(t *testing.T) {
	unreachable := &unreachableRepo{
		PackageRepository: newPackageRepository(t, php.ArrayOf("package", php.NewArray(), "security-advisories", php.ArrayOf("acme/package", php.NewArray()))),
		name:              "unreachable advisory repo",
		err:               util.NewTransportError("boom", 500),
	}

	policyConfig := advisoryPolicyConfig(true, true, nil, nil)
	policyConfig.IgnoreUnreachable = policy.IgnoreUnreachableNone()
	filter := NewSecurityAdvisoryPoolFilter(advisory.Auditor{}, policyConfig, newBufferIO(t))

	_, err := filter.Filter(NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, nil, nil), []repository.RepositoryInterface{unreachable}, NewRequest(nil))
	if _, ok := errors.AsType[*util.TransportError](err); !ok {
		t.Fatalf("got %v", err)
	}
}

func policyConfigFromRaw(t testing.TB, policyRaw *php.Array) *policy.PolicyConfig {
	t.Helper()
	cfg := config.New(false, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("policy", policyRaw)), "test"); err != nil {
		t.Fatal(err)
	}
	policyConfig, err := policy.FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return policyConfig
}

func filterListRepository(t testing.TB, listName, packageName, constraint string) *repository.PackageRepository {
	t.Helper()

	return newPackageRepository(t, php.ArrayOf(
		"package", php.NewArray(),
		"filter", php.ArrayOf(listName, php.ListOf(php.ArrayOf(
			"package", packageName,
			"constraint", constraint,
			"reason", "malware",
			"url", "https://example.org/malware/"+packageName,
		))),
	))
}

func generatePackageRepository(t testing.TB, constraint string) *repository.PackageRepository {
	t.Helper()

	return filterListRepository(t, "test-list", "acme/package", constraint)
}

func newFilterListPoolFilter(policyConfig *policy.PolicyConfig, httpDownloader *httpmock.Downloader, blockScope string, out io.IO, repos ...repository.RepositoryInterface) *FilterListPoolFilter {
	return NewFilterListPoolFilter(policyConfig, filterlist.FilterListAuditor{}, httpDownloader, blockScope, repos, out)
}

func TestFilterListPoolFilter_FilterPackages(t *testing.T) {
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", true))
	filter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, generatePackageRepository(t, "1.0"))

	expectedPackage1 := pkg.NewPackage("acme/package", "2.0.0.0", "2.0")
	expectedPackage2 := pkg.NewPackage("acme/other", "1.0.0.0", "1.0")
	pool := NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0"), expectedPackage1, expectedPackage2}, nil, nil)
	filteredPool, err := filter.Filter(pool, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
	if !filteredPool.IsFilterListRemovedPackageVersion("acme/package", eq("1.0.0.0")) || filteredPool.AllFilterListRemovedPackageVersions().Len() != 1 {
		t.Fatal("wrong removals")
	}
}

func TestFilterListPoolFilter_UnfilteredPackagesConfig(t *testing.T) {
	for name, filterConfig := range map[string]*php.Array{
		"ignore-packages":         php.ArrayOf("ignore", php.ArrayOf(0, "acme/package")),
		"ignore-packages-version": php.ArrayOf("ignore", php.ArrayOf("acme/package", php.ArrayOf("constraint", "*"))),
	} {
		t.Run(name, func(t *testing.T) {
			policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", filterConfig))
			filter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, generatePackageRepository(t, "*"))

			expectedPackage1 := pkg.NewPackage("acme/package", "1.0.0.0", "1.0")
			expectedPackage2 := pkg.NewPackage("acme/package", "1.1.0.0", "1.1")
			filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{expectedPackage1, expectedPackage2}, nil, nil), NewRequest(nil))
			if err != nil {
				t.Fatal(err)
			}

			assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
			if filteredPool.AllFilterListRemovedPackageVersions().Len() != 0 {
				t.Fatal("versions removed")
			}
		})
	}
}

func TestFilterListPoolFilter_UnfilteredPackagesConfigIntersection(t *testing.T) {
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", php.ArrayOf("ignore", php.ArrayOf("acme/package", php.ArrayOf("constraint", "<=1.0")))))
	filter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, generatePackageRepository(t, ">=1.0"))

	expectedPackage := pkg.NewPackage("acme/package", "1.0.0.0", "1.0")
	filteredPool, err := filter.Filter(NewPool([]pkg.PackageInterface{expectedPackage, pkg.NewPackage("acme/package", "1.1.0.0", "1.1")}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage}, filteredPool.Packages())
	if filteredPool.AllFilterListRemovedPackageVersions().Len() != 1 {
		t.Fatal("wrong removals")
	}
}

func TestFilterListPoolFilter_FilterWithAdditionalSources(t *testing.T) {
	httpDownloader := httpmock.New()
	body, err := php.JSONEncode(php.ArrayOf("filter", php.ListOf(php.ArrayOf("package", "acme/package", "constraint", "3.0.0.0", "reason", "malware"))), 0)
	if err != nil {
		t.Fatal(err)
	}
	httpDownloader.Expects([]httpmock.Expectation{{URL: "https://example.org/malware/acme/package", Body: body, Status: 200}}, false, nil)

	policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", php.ArrayOf(
		"sources", php.ArrayOf("source-list", php.ArrayOf("type", "url", "url", "https://example.org/malware/acme/package")),
	)))
	filter := newFilterListPoolFilter(policyConfig, httpDownloader, policy.BlockScopeUpdate, nullIO, generatePackageRepository(t, "0.0.1"))

	expectedPackage1 := pkg.NewPackage("acme/package", "2.0.0.0", "2.0")
	expectedPackage2 := pkg.NewPackage("acme/other", "1.0.0.0", "1.0")
	pool := NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "3.0.0.0", "3.0"), expectedPackage1, expectedPackage2}, nil, nil)
	filteredPool, err := filter.Filter(pool, NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{expectedPackage1, expectedPackage2}, filteredPool.Packages())
	if !filteredPool.IsFilterListRemovedPackageVersion("acme/package", eq("3.0.0.0")) || filteredPool.AllFilterListRemovedPackageVersions().Len() != 1 {
		t.Fatal("wrong removals")
	}
}

func TestFilterListPoolFilter_InstallScopeFiltersLockedPackagesAgainstMalwareList(t *testing.T) {
	repo := filterListRepository(t, "malware", "acme/locked", "*")
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("malware", true))

	p := pkg.NewPackage("acme/locked", "1.0.0.0", "1.0")
	request := NewRequest(nil)
	request.FixLockedPackage(p)

	// Install scope: locked package IS checked, gets filter-list-removed.
	installFilter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeInstall, nullIO, repo)
	installPool, err := installFilter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(installPool.Packages()) != 0 || !installPool.IsFilterListRemovedPackageVersion("acme/locked", eq("1.0.0.0")) {
		t.Fatal("install-scope filter must include locked packages")
	}

	// Update scope: locked packages are checked against install-scope filter lists too,
	// so a malware-flagged locked package is still removed from the pool.
	updateFilter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, repo)
	updatePool, err := updateFilter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatePool.Packages()) != 0 || !updatePool.IsFilterListRemovedPackageVersion("acme/locked", eq("1.0.0.0")) {
		t.Fatal("update-scope filter must apply install-scope rules to locked packages")
	}
}

func TestFilterListPoolFilter_UpdateScopeAppliesInstallScopeToPackagesInLockedRepository(t *testing.T) {
	repo := filterListRepository(t, "malware", "acme/mirrored", "*")

	// block-scope: install means the malware list is NOT in getActiveBlockFilterListNames(UPDATE)
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("malware", php.ArrayOf("block", true, "block-scope", policy.BlockScopeInstall)))

	// PoolBuilder loads fresh package instances from configured repos for `update mirrors`
	// Use distinct instances to mirror that in this unit test — the locked
	// package and the pool package have the same name+version but are not the same object.
	lockedPackage := pkg.NewPackage("acme/mirrored", "1.0.0.0", "1.0")
	poolPackage := pkg.NewPackage("acme/mirrored", "1.0.0.0", "1.0")
	lockedRepo := newLockArrayRepository(t)
	addPackage(t, lockedRepo, lockedPackage)
	request := NewRequest(lockedRepo)
	requireName(t, request, "acme/mirrored", eq("1.0.0.0"))

	updateFilter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, repo)
	updatePool, err := updateFilter.Filter(NewPool([]pkg.PackageInterface{poolPackage}, nil, nil), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatePool.Packages()) != 0 || !updatePool.IsFilterListRemovedPackageVersion("acme/mirrored", eq("1.0.0.0")) {
		t.Fatal("packages from the locked repo must be checked against install-scope filter lists")
	}
}

func TestFilterListPoolFilter_UpdateScopeIgnoresInstallOnlyListsForNonLockedPackages(t *testing.T) {
	repo := filterListRepository(t, "malware", "acme/free", "*")

	// block-scope: install — only locked-equivalent packages should be
	// filtered. A package that is not in the locked repository must pass
	// through the UPDATE-scope filter unmodified.
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("malware", php.ArrayOf("block", true, "block-scope", policy.BlockScopeInstall)))

	p := pkg.NewPackage("acme/free", "1.0.0.0", "1.0")
	updateFilter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, nullIO, repo)
	updatePool, err := updateFilter.Filter(NewPool([]pkg.PackageInterface{p}, nil, nil), NewRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	assertPackages(t, []pkg.PackageInterface{p}, updatePool.Packages())
	if updatePool.AllFilterListRemovedPackageVersions().Len() != 0 {
		t.Fatal("install-only lists must not block non-locked packages during update scope")
	}
}

func TestFilterListPoolFilter_WarnsWhenUnreachableSourcesAreIgnored(t *testing.T) {
	unreachable := &unreachableRepo{
		PackageRepository: newPackageRepository(t, php.ArrayOf("package", php.NewArray(), "filter", php.ArrayOf("test-list", php.ListOf(php.ArrayOf("package", "acme/package", "constraint", "*", "reason", "malware"))))),
		name:              "unreachable filter list repo",
		err:               util.NewTransportError(`The "https://example.org/filter.json" file could not be downloaded: HTTP/1.1 500 Internal Server Error`, 500),
	}

	// ignore-unreachable defaults to ["update", "install"], so the transport error is swallowed.
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", true))
	out := newBufferIO(t)
	filter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, out, unreachable)
	if _, err := filter.Filter(NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, nil, nil), NewRequest(nil)); err != nil {
		t.Fatal(err)
	}

	output := out.Output()
	for _, want := range []string{"Filter list data could not be fetched from some sources", "HTTP/1.1 500 Internal Server Error"} {
		if !strings.Contains(output, want) {
			t.Errorf("%q does not contain %q", output, want)
		}
	}
}

func TestFilterListPoolFilter_RethrowsTransportErrorWhenUnreachableIsNotIgnored(t *testing.T) {
	unreachable := &unreachableRepo{
		PackageRepository: newPackageRepository(t, php.ArrayOf("package", php.NewArray(), "filter", php.ArrayOf("test-list", php.ListOf(php.ArrayOf("package", "acme/package", "constraint", "*", "reason", "malware"))))),
		name:              "unreachable filter list repo",
		err:               util.NewTransportError("boom", 500),
	}

	// ignore-unreachable: false — transport errors bubble up; no warning needed.
	policyConfig := policyConfigFromRaw(t, php.ArrayOf("test-list", true, "ignore-unreachable", false))
	filter := newFilterListPoolFilter(policyConfig, httpmock.New(), policy.BlockScopeUpdate, newBufferIO(t), unreachable)

	_, err := filter.Filter(NewPool([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, nil, nil), NewRequest(nil))
	if _, ok := errors.AsType[*util.TransportError](err); !ok {
		t.Fatalf("got %v", err)
	}
}
