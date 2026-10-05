package resolver

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/DefaultPolicyTest.php and
// TransactionTest.php.

type policyTest struct {
	t             *testing.T
	repositorySet *repository.RepositorySet
	repo          *repository.ArrayRepository
	repoLocked    *repository.LockArrayRepository
	policy        *DefaultPolicy
}

func newPolicyTest(t *testing.T) *policyTest {
	return &policyTest{
		t:             t,
		repositorySet: newRepositorySet(t, "dev"),
		repo:          newArrayRepository(t),
		repoLocked:    newLockArrayRepository(t),
		policy:        NewDefaultPolicy(false, false, nil),
	}
}

func (pt *policyTest) pkg(name, version string) *pkg.CompletePackage {
	pt.t.Helper()
	p := getPackage(pt.t, name, version)
	addPackage(pt.t, pt.repo, p)

	return p
}

func (pt *policyTest) poolFor(names ...string) *Pool {
	pt.t.Helper()
	pool, err := CreatePoolForPackages(pt.repositorySet, names, pt.repoLocked)
	if err != nil {
		pt.t.Fatal(err)
	}

	return pool
}

func ids(packages ...pkg.PackageInterface) []int32 {
	literals := make([]int32, len(packages))
	for i, p := range packages {
		literals[i] = int32(p.ID())
	}

	return literals
}

func assertSelected(t *testing.T, expected, selected []int32) {
	t.Helper()
	if !slices.Equal(expected, selected) {
		t.Fatalf("selected %v, want %v", selected, expected)
	}
}

func TestDefaultPolicy_SelectSingle(t *testing.T) {
	pt := newPolicyTest(t)
	packageA := pt.pkg("A", "1.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	assertSelected(t, ids(packageA), pt.policy.SelectPreferredPackages(pool, ids(packageA), ""))
}

func TestDefaultPolicy_SelectNewest(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0")
	packageA2 := pt.pkg("A", "2.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	assertSelected(t, ids(packageA2), pt.policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_SelectNewestPicksLatest(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0.0")
	packageA2 := pt.pkg("A", "1.0.1-alpha")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	assertSelected(t, ids(packageA2), pt.policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_SelectNewestPicksLatestStableWithPreferStable(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0.0")
	packageA2 := pt.pkg("A", "1.0.1-alpha")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	policy := NewDefaultPolicy(true, false, nil)
	assertSelected(t, ids(packageA1), policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_SelectLowestWithPreferDevOverPrerelease(t *testing.T) {
	for _, stability := range []string{"alpha1", "beta1", "RC1"} {
		t.Run(stability, func(t *testing.T) {
			t.Setenv("COMPOSER_PREFER_DEV_OVER_PRERELEASE", "1")

			pt := newPolicyTest(t)
			devPackage := pt.pkg("A", "dev-master")
			prereleasePackage := pt.pkg("A", "1.0.0-"+stability)
			addRepository(t, pt.repositorySet, pt.repo)

			pool := pt.poolFor("A")

			policy := NewDefaultPolicy(true, true, nil)
			assertSelected(t, ids(devPackage), policy.SelectPreferredPackages(pool, ids(devPackage, prereleasePackage), ""))
		})
	}
}

func TestDefaultPolicy_SelectLowestPrefersPrereleaseOverDev(t *testing.T) {
	for _, stability := range []string{"alpha1", "beta1", "RC1"} {
		t.Run(stability, func(t *testing.T) {
			pt := newPolicyTest(t)
			devPackage := pt.pkg("A", "dev-master")
			prereleasePackage := pt.pkg("A", "1.0.0-"+stability)
			addRepository(t, pt.repositorySet, pt.repo)

			pool := pt.poolFor("A")

			policy := NewDefaultPolicy(true, true, nil)
			assertSelected(t, ids(prereleasePackage), policy.SelectPreferredPackages(pool, ids(prereleasePackage, devPackage), ""))
		})
	}
}

func TestDefaultPolicy_SelectLowestWithPreferStableStillPrefersStable(t *testing.T) {
	t.Setenv("COMPOSER_PREFER_DEV_OVER_PRERELEASE", "1")

	pt := newPolicyTest(t)
	stablePackage := pt.pkg("A", "1.0.0")
	devPackage := pt.pkg("A", "dev-master")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	policy := NewDefaultPolicy(true, true, nil)
	assertSelected(t, ids(stablePackage), policy.SelectPreferredPackages(pool, ids(stablePackage, devPackage), ""))
}

func TestDefaultPolicy_SelectNewestWithDevPicksNonDev(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "dev-foo")
	packageA2 := pt.pkg("A", "1.0.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	assertSelected(t, ids(packageA2), pt.policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_SelectNewestWithPreferredVersionPicksPreferredVersionIfAvailable(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0.0")
	packageA2 := pt.pkg("A", "1.1.0")
	packageA2b := pt.pkg("A", "1.1.0")
	packageA3 := pt.pkg("A", "1.2.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	policy := NewDefaultPolicy(false, false, map[string]string{"a": "1.1.0.0"})
	assertSelected(t, ids(packageA2, packageA2b), policy.SelectPreferredPackages(pool, ids(packageA1, packageA2, packageA2b, packageA3), ""))
}

func TestDefaultPolicy_SelectNewestWithPreferredVersionPicksNewestOtherwise(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0.0")
	packageA2 := pt.pkg("A", "1.2.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	policy := NewDefaultPolicy(false, false, map[string]string{"a": "1.1.0.0"})
	assertSelected(t, ids(packageA2), policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_SelectNewestWithPreferredVersionPicksLowestIfPreferLowest(t *testing.T) {
	pt := newPolicyTest(t)
	packageA1 := pt.pkg("A", "1.0.0")
	packageA2 := pt.pkg("A", "1.2.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	policy := NewDefaultPolicy(false, true, map[string]string{"a": "1.1.0.0"})
	assertSelected(t, ids(packageA1), policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestDefaultPolicy_RepositoryOrderingAffectsPriority(t *testing.T) {
	pt := newPolicyTest(t)
	package1 := getPackage(t, "A", "1.0")
	package2 := getPackage(t, "A", "1.1")
	package3 := getPackage(t, "A", "1.1")
	package4 := getPackage(t, "A", "1.2")
	repo1 := newArrayRepository(t, package1, package2)
	repo2 := newArrayRepository(t, package3, package4)

	addRepository(t, pt.repositorySet, repo1)
	addRepository(t, pt.repositorySet, repo2)

	pool := pt.poolFor("A")

	literals := ids(package1, package2, package3, package4)
	assertSelected(t, ids(package2), pt.policy.SelectPreferredPackages(pool, literals, ""))

	pt.repositorySet = newRepositorySet(t, "dev")
	addRepository(t, pt.repositorySet, repo2)
	addRepository(t, pt.repositorySet, repo1)

	pool = pt.poolFor("A")

	assertSelected(t, ids(package4), pt.policy.SelectPreferredPackages(pool, literals, ""))
}

func TestDefaultPolicy_SelectLocalReposFirst(t *testing.T) {
	pt := newPolicyTest(t)
	packageA := pt.pkg("A", "dev-master")
	addPackage(t, pt.repo, pkg.NewAliasPackage(packageA, "2.1.9999999.9999999-dev", "2.1.x-dev"))

	packageAImportant := getPackage(t, "A", "dev-feature-a")
	packageAAliasImportant := pkg.NewAliasPackage(packageAImportant, "2.1.9999999.9999999-dev", "2.1.x-dev")
	packageA2Important := getPackage(t, "A", "dev-master")
	packageA2AliasImportant := pkg.NewAliasPackage(packageA2Important, "2.1.9999999.9999999-dev", "2.1.x-dev")
	repoImportant := newArrayRepository(t, packageAImportant, packageAAliasImportant, packageA2Important, packageA2AliasImportant)
	packageAAliasImportant.SetRootPackageAlias(true)

	addRepository(t, pt.repositorySet, repoImportant)
	addRepository(t, pt.repositorySet, pt.repo)
	addRepository(t, pt.repositorySet, pt.repoLocked)

	pool := pt.poolFor("A")

	packages := pool.WhatProvides("a", semver.NewConstraintOp(semver.OpEQ, "2.1.9999999.9999999-dev"))
	if len(packages) == 0 {
		t.Fatal("no packages")
	}

	assertSelected(t, ids(packageAAliasImportant), pt.policy.SelectPreferredPackages(pool, ids(packages...), ""))
}

func TestDefaultPolicy_SelectAllProviders(t *testing.T) {
	pt := newPolicyTest(t)
	packageA := pt.pkg("A", "1.0")
	packageB := pt.pkg("B", "2.0")

	packageA.SetProvides(pkg.LinksOf(newLink("A", "X", semver.NewConstraintOp(semver.OpEQ, "1.0"), pkg.TypeProvide)))
	packageB.SetProvides(pkg.LinksOf(newLink("B", "X", semver.NewConstraintOp(semver.OpEQ, "1.0"), pkg.TypeProvide)))

	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A", "B")

	literals := ids(packageA, packageB)
	assertSelected(t, literals, pt.policy.SelectPreferredPackages(pool, literals, ""))
}

func TestDefaultPolicy_PreferNonReplacingFromSameRepo(t *testing.T) {
	pt := newPolicyTest(t)
	packageA := pt.pkg("A", "1.0")
	packageB := pt.pkg("B", "2.0")

	packageB.SetReplaces(pkg.LinksOf(newLink("B", "A", semver.NewConstraintOp(semver.OpEQ, "1.0"), pkg.TypeReplace)))

	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A", "B")

	literals := ids(packageA, packageB)
	assertSelected(t, literals, pt.policy.SelectPreferredPackages(pool, literals, ""))
}

func TestDefaultPolicy_PreferReplacingPackageFromSameVendor(t *testing.T) {
	pt := newPolicyTest(t)
	// test with default order
	packageB := pt.pkg("vendor-b/replacer", "1.0")
	packageA := pt.pkg("vendor-a/replacer", "1.0")

	packageA.SetReplaces(pkg.LinksOf(newLink("vendor-a/replacer", "vendor-a/package", semver.NewConstraintOp(semver.OpEQ, "1.0"), pkg.TypeReplace)))
	packageB.SetReplaces(pkg.LinksOf(newLink("vendor-b/replacer", "vendor-a/package", semver.NewConstraintOp(semver.OpEQ, "1.0"), pkg.TypeReplace)))

	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("vendor-a/replacer", "vendor-b/replacer")

	literals := ids(packageA, packageB)
	assertSelected(t, literals, pt.policy.SelectPreferredPackages(pool, literals, "vendor-a/package"))

	// test with reversed order in repo
	cloneA := pkg.Clone(packageA)
	cloneB := pkg.Clone(packageB)
	newArrayRepository(t, cloneA, cloneB)

	repositorySet := newRepositorySet(t, "dev")
	addRepository(t, repositorySet, pt.repo)

	pool = pt.poolFor("vendor-a/replacer", "vendor-b/replacer")

	literals = ids(cloneA, cloneB)
	assertSelected(t, literals, pt.policy.SelectPreferredPackages(pool, literals, "vendor-a/package"))
}

func TestDefaultPolicy_SelectLowest(t *testing.T) {
	pt := newPolicyTest(t)
	policy := NewDefaultPolicy(false, true, nil)

	packageA1 := pt.pkg("A", "1.0")
	packageA2 := pt.pkg("A", "2.0")
	addRepository(t, pt.repositorySet, pt.repo)

	pool := pt.poolFor("A")

	assertSelected(t, ids(packageA1), policy.SelectPreferredPackages(pool, ids(packageA1, packageA2), ""))
}

func TestTransaction_TransactionGenerationAndSorting(t *testing.T) {
	packageA := getPackage(t, "a/a", "dev-master")
	packageAalias := getAliasPackage(t, packageA, "1.0.x-dev")
	packageB := getPackage(t, "b/b", "1.0.0")
	packageE := getPackage(t, "e/e", "dev-foo")
	packageEalias := getAliasPackage(t, packageE, "1.0.x-dev")
	packageC := getPackage(t, "c/c", "1.0.0")
	presentPackages := []pkg.PackageInterface{packageA, packageAalias, packageB, packageE, packageEalias, packageC}

	packageBnew := getPackage(t, "b/b", "2.1.3")
	packageD := getPackage(t, "d/d", "1.2.3")
	packageF := getPackage(t, "f/f", "1.0.0")
	packageFalias1 := getAliasPackage(t, packageF, "dev-foo")
	packageG := getPackage(t, "g/g", "1.0.0")
	packageA0first := getPackage(t, "a0/first", "1.2.3")
	packageFalias2 := getAliasPackage(t, packageF, "dev-bar")
	plugin := getPackage(t, "x/plugin", "1.0.0")
	plugin2Dep := getPackage(t, "x/plugin2-dep", "1.0.0")
	plugin2 := getPackage(t, "x/plugin2", "1.0.0")
	dlModifyingPlugin := getPackage(t, "x/downloads-modifying", "1.0.0")
	dlModifyingPlugin2Dep := getPackage(t, "x/downloads-modifying2-dep", "1.0.0")
	dlModifyingPlugin2 := getPackage(t, "x/downloads-modifying2", "1.0.0")
	resultPackages := []pkg.PackageInterface{
		packageA, packageAalias, packageBnew, packageD, packageF, packageFalias1, packageG, packageA0first, packageFalias2,
		plugin, plugin2Dep, plugin2, dlModifyingPlugin, dlModifyingPlugin2Dep, dlModifyingPlugin2,
	}

	plugin.SetType("composer-installer")
	for _, pluginPackage := range []*pkg.CompletePackage{plugin2, dlModifyingPlugin, dlModifyingPlugin2} {
		pluginPackage.SetType("composer-plugin")
	}

	requires(plugin2, newLink("x/plugin2", "x/plugin2-dep", getVersionConstraint(t, "=", "1.0.0"), pkg.TypeRequire))
	requires(dlModifyingPlugin2, newLink("x/downloads-modifying2", "x/downloads-modifying2-dep", getVersionConstraint(t, "=", "1.0.0"), pkg.TypeRequire))
	dlModifyingPlugin.SetExtra(php.ArrayOf("plugin-modifies-downloads", true))
	dlModifyingPlugin2.SetExtra(php.ArrayOf("plugin-modifies-downloads", true))

	requires(packageD,
		newLink("d/d", "f/f", getVersionConstraint(t, ">", "0.2"), pkg.TypeRequire),
		newLink("d/d", "g/provider", getVersionConstraint(t, ">", "0.2"), pkg.TypeRequire),
	)
	packageG.SetProvides(pkg.LinksOf(newLink("g/g", "g/provider", getVersionConstraint(t, "==", "1.0.0"), pkg.TypeProvide)))

	expectedOperations := []opResult{
		{job: "uninstall", pkg: packageC},
		{job: "uninstall", pkg: packageE},
		{job: "markAliasUninstalled", pkg: packageEalias},
		install(dlModifyingPlugin),
		install(dlModifyingPlugin2Dep),
		install(dlModifyingPlugin2),
		install(plugin),
		install(plugin2Dep),
		install(plugin2),
		install(packageA0first),
		update(packageB, packageBnew),
		install(packageG),
		install(packageF),
		markAliasInstalled(packageFalias2),
		markAliasInstalled(packageFalias1),
		install(packageD),
	}

	transaction := NewTransaction(presentPackages, resultPackages)
	assertOperations(t, expectedOperations, operationResults(t, transaction.Operations(), "uninstall"))
}
