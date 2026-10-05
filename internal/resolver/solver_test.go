package resolver

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/SolverTest.php.

type solverTest struct {
	t          *testing.T
	repoSet    *repository.RepositorySet
	repo       *repository.ArrayRepository
	repoLocked *repository.LockArrayRepository
	request    *Request
	policy     *DefaultPolicy
	solver     *Solver
	pool       *Pool
}

func newSolverTest(t *testing.T) *solverTest {
	t.Helper()
	st := &solverTest{
		t:          t,
		repoSet:    newRepositorySet(t, "stable"),
		repo:       newArrayRepository(t),
		repoLocked: newLockArrayRepository(t),
		policy:     NewDefaultPolicy(false, false, nil),
	}
	st.request = NewRequest(st.repoLocked)

	return st
}

func (st *solverTest) add(p pkg.PackageInterface) pkg.PackageInterface {
	st.t.Helper()
	addPackage(st.t, st.repo, p)

	return p
}

func (st *solverTest) addLocked(p pkg.PackageInterface) pkg.PackageInterface {
	st.t.Helper()
	addPackage(st.t, st.repoLocked, p)

	return p
}

func (st *solverTest) pkg(name, version string) *pkg.CompletePackage {
	st.t.Helper()
	p := getPackage(st.t, name, version)
	st.add(p)

	return p
}

func (st *solverTest) lockedPkg(name, version string) *pkg.CompletePackage {
	st.t.Helper()
	p := getPackage(st.t, name, version)
	st.addLocked(p)

	return p
}

func (st *solverTest) constraint(op, version string) *semver.Constraint {
	return getVersionConstraint(st.t, op, version)
}

func (st *solverTest) require(name string, constraint semver.ConstraintInterface) {
	st.t.Helper()
	requireName(st.t, st.request, name, constraint)
}

func (st *solverTest) reposComplete() {
	st.t.Helper()
	addRepository(st.t, st.repoSet, st.repo)
	addRepository(st.t, st.repoSet, st.repoLocked)
}

func (st *solverTest) createSolver() {
	st.t.Helper()
	pool, err := CreatePool(st.repoSet, st.request, nullIO, CreatePoolOptions{})
	if err != nil {
		st.t.Fatal(err)
	}
	st.pool = pool
	st.solver = NewSolver(st.policy, st.pool, nullIO)
}

func (st *solverTest) solveFails() *SolverProblemsError {
	st.t.Helper()
	st.createSolver()
	_, err := st.solver.Solve(st.request, nil)
	problems, ok := errors.AsType[*SolverProblemsError](err)
	if !ok {
		st.t.Fatalf("Unsolvable conflict did not result in exception: %v", err)
	}

	return problems
}

func (st *solverTest) prettyString(e *SolverProblemsError) string {
	st.t.Helper()
	s, err := e.PrettyString(st.repoSet, st.request, st.pool, false, false, nil)
	if err != nil {
		st.t.Fatal(err)
	}

	return s
}

func (st *solverTest) checkSolverResult(expected ...opResult) {
	st.t.Helper()
	st.createSolver()
	transaction, err := st.solver.Solve(st.request, nil)
	if err != nil {
		if problems, ok := errors.AsType[*SolverProblemsError](err); ok {
			st.t.Fatal(st.prettyString(problems))
		}
		st.t.Fatal(err)
	}

	assertOperations(st.t, expected, operationResults(st.t, transaction.Operations(), "remove"))
}

func install(p pkg.PackageInterface) opResult { return opResult{job: "install", pkg: p} }
func remove(p pkg.PackageInterface) opResult  { return opResult{job: "remove", pkg: p} }
func update(from, to pkg.PackageInterface) opResult {
	return opResult{job: "update", from: from, to: to}
}

func markAliasInstalled(p pkg.PackageInterface) opResult {
	return opResult{job: "markAliasInstalled", pkg: p}
}

func requires(p *pkg.CompletePackage, links ...*pkg.Link) { p.SetRequires(pkg.LinksOf(links...)) }

func TestSolver_SolverInstallSingle(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.reposComplete()
	st.require("a/a", nil)

	st.checkSolverResult(install(packageA))
}

func TestSolver_SolverRemoveIfNotRequested(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	st.reposComplete()

	st.checkSolverResult(remove(packageA))
}

func TestSolver_InstallNonExistingPackageFails(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("a/a", "1.0")
	st.reposComplete()
	st.require("b/b", st.constraint("==", "1"))

	e := st.solveFails()
	if len(e.Problems()) != 1 || e.Code() != 2 {
		t.Fatalf("problems %d, code %d", len(e.Problems()), e.Code())
	}
	installedMap, _ := st.request.PresentIDMap()
	got, err := e.Problems()[0].PrettyString(&PrettyContext{RepositorySet: st.repoSet, Request: st.request, Pool: st.pool, InstalledMap: installedMap})
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n    - Root composer.json requires b/b, it could not be found in any version, there may be a typo in the package name."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSolver_SolverInstallSamePackageFromDifferentRepositories(t *testing.T) {
	st := newSolverTest(t)
	foo1 := getPackage(t, "foo/foo", "1")
	foo2 := getPackage(t, "foo/foo", "1")
	addRepository(t, st.repoSet, newArrayRepository(t, foo1))
	addRepository(t, st.repoSet, newArrayRepository(t, foo2))

	st.require("foo/foo", nil)

	st.checkSolverResult(install(foo1))
}

func TestSolver_SolverInstallWithDeps(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	st.pkg("b/b", "1.1")

	requires(packageA, newLink("a/a", "b/b", st.constraint("<", "1.1"), pkg.TypeRequire))

	st.reposComplete()
	st.require("a/a", nil)

	st.checkSolverResult(install(packageB), install(packageA))
}

func TestSolver_SolverInstallHonoursNotEqualOperator(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("b/b", "1.0")
	newPackageB11 := st.pkg("b/b", "1.1")
	st.pkg("b/b", "1.2")
	st.pkg("b/b", "1.3")

	requires(packageA, newLink("a/a", "b/b", multi(t, true,
		st.constraint("<=", "1.3"),
		st.constraint("<>", "1.3"),
		st.constraint("!=", "1.2"),
	), pkg.TypeRequire))

	st.reposComplete()
	st.require("a/a", nil)

	st.checkSolverResult(install(newPackageB11), install(packageA))
}

func TestSolver_SolverInstallWithDepsInOrder(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	packageC := st.pkg("c/c", "1.0")

	requires(packageB,
		newLink("b/b", "a/a", st.constraint(">=", "1.0"), pkg.TypeRequire),
		newLink("b/b", "c/c", st.constraint(">=", "1.0"), pkg.TypeRequire),
	)
	requires(packageC, newLink("c/c", "a/a", st.constraint(">=", "1.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)
	st.require("b/b", nil)
	st.require("c/c", nil)

	st.checkSolverResult(install(packageA), install(packageC), install(packageB))
}

// CAUTION: IF THIS TEST EVER FAILS, SOLVER BEHAVIOR HAS CHANGED AND MAY BREAK DOWNSTREAM USERS
func TestSolver_SolverMultiPackageNameVersionResolutionDependsOnRequireOrder(t *testing.T) {
	st := newSolverTest(t)
	php74 := st.pkg("ourcustom/PHP", "7.4.23")
	php80 := st.pkg("ourcustom/PHP", "8.0.10")
	extForPhp74 := st.pkg("ourcustom/ext-foobar", "1.0")
	extForPhp80 := st.pkg("ourcustom/ext-foobar", "1.0")

	requires(extForPhp74, newLink("ourcustom/ext-foobar", "ourcustom/PHP", multi(t, true,
		st.constraint(">=", "7.4.0"),
		st.constraint("<", "7.5.0"),
	), pkg.TypeRequire))
	requires(extForPhp80, newLink("ourcustom/ext-foobar", "ourcustom/PHP", multi(t, true,
		st.constraint(">=", "8.0.0"),
		st.constraint("<", "8.1.0"),
	), pkg.TypeRequire))

	st.reposComplete()

	st.require("ourcustom/PHP", nil)
	st.require("ourcustom/ext-foobar", nil)

	st.checkSolverResult(install(php80), install(extForPhp80))

	// now we flip the requirements around: we request "ext-foobar" before "php"
	// because the ext-foobar package that requires php74 comes first in the repo, and the one that requires php80 second, the solver will pick the one for php74, and then, as it is a dependency, also php74
	// this is because both packages have the same name and version; just their requirements differ
	// and because no other constraint forces a particular version of package "php"
	st.request = NewRequest(st.repoLocked)
	st.require("ourcustom/ext-foobar", nil)
	st.require("ourcustom/PHP", nil)

	st.checkSolverResult(install(php74), install(extForPhp74))
}

// CAUTION: IF THIS TEST EVER FAILS, SOLVER BEHAVIOR HAS CHANGED AND MAY BREAK DOWNSTREAM USERS
func TestSolver_SolverMultiPackageNameVersionResolutionIsIndependentOfRequireOrderIfOrderedDescendingByRequirement(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("ourcustom/PHP", "7.4")
	php80 := st.pkg("ourcustom/PHP", "8.0")
	extForPhp80 := st.pkg("ourcustom/ext-foobar", "1.0") // note we are inserting this one into the repo first, unlike in the previous test
	extForPhp74 := st.pkg("ourcustom/ext-foobar", "1.0")

	requires(extForPhp80, newLink("ourcustom/ext-foobar", "ourcustom/PHP", multi(t, true,
		st.constraint(">=", "8.0.0"),
		st.constraint("<", "8.1.0"),
	), pkg.TypeRequire))
	requires(extForPhp74, newLink("ourcustom/ext-foobar", "ourcustom/PHP", multi(t, true,
		st.constraint(">=", "7.4.0"),
		st.constraint("<", "7.5.0"),
	), pkg.TypeRequire))

	st.reposComplete()

	st.require("ourcustom/PHP", nil)
	st.require("ourcustom/ext-foobar", nil)

	st.checkSolverResult(install(php80), install(extForPhp80))

	// unlike in the previous test, the order of requirements no longer matters now
	st.request = NewRequest(st.repoLocked)
	st.require("ourcustom/ext-foobar", nil)
	st.require("ourcustom/PHP", nil)

	st.checkSolverResult(install(php80), install(extForPhp80))
}

func TestSolver_SolverFixLocked(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	st.reposComplete()

	st.request.FixPackage(packageA)

	st.checkSolverResult()
}

func TestSolver_SolverFixLockedWithAlternative(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("a/a", "1.0")
	packageA := st.lockedPkg("a/a", "1.0")

	st.reposComplete()

	st.request.FixPackage(packageA)

	st.checkSolverResult()
}

func TestSolver_SolverUpdateDoesOnlyUpdate(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	packageB := st.lockedPkg("b/b", "1.0")
	newPackageB := st.pkg("b/b", "1.1")
	st.reposComplete()

	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0.0.0"), pkg.TypeRequire))

	st.request.FixPackage(packageA)
	st.require("b/b", st.constraint("=", "1.1.0.0"))

	st.checkSolverResult(update(packageB, newPackageB))
}

func TestSolver_SolverUpdateSingle(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	newPackageA := st.pkg("a/a", "1.1")
	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(update(packageA, newPackageA))
}

func TestSolver_SolverUpdateAll(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	packageB := st.lockedPkg("b/b", "1.0")
	newPackageA := st.pkg("a/a", "1.1")
	newPackageB := st.pkg("b/b", "1.1")

	requires(packageA, newLink("a/a", "b/b", semver.NewMatchAllConstraint(), pkg.TypeRequire))
	requires(newPackageA, newLink("a/a", "b/b", semver.NewMatchAllConstraint(), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(update(packageB, newPackageB), update(packageA, newPackageA))
}

func TestSolver_SolverUpdateCurrent(t *testing.T) {
	st := newSolverTest(t)
	st.lockedPkg("a/a", "1.0")
	st.pkg("a/a", "1.0")

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult()
}

func TestSolver_SolverUpdateOnlyUpdatesSelectedPackage(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	packageB := st.lockedPkg("b/b", "1.0")
	packageAnewer := st.pkg("a/a", "1.1")
	st.pkg("b/b", "1.1")

	st.reposComplete()

	st.require("a/a", nil)
	st.request.FixPackage(packageB)

	st.checkSolverResult(update(packageA, packageAnewer))
}

func TestSolver_SolverUpdateConstrained(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	newPackageA := st.pkg("a/a", "1.2")
	st.pkg("a/a", "2.0")
	st.reposComplete()

	st.require("a/a", st.constraint("<", "2.0.0.0"))

	st.checkSolverResult(update(packageA, newPackageA))
}

func TestSolver_SolverUpdateFullyConstrained(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	newPackageA := st.pkg("a/a", "1.2")
	st.pkg("a/a", "2.0")
	st.reposComplete()

	st.require("a/a", st.constraint("<", "2.0.0.0"))

	st.checkSolverResult(update(packageA, newPackageA))
}

func TestSolver_SolverUpdateFullyConstrainedPrunesInstalledPackages(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	packageB := st.lockedPkg("b/b", "1.0")
	newPackageA := st.pkg("a/a", "1.2")
	st.pkg("a/a", "2.0")
	st.reposComplete()

	st.require("a/a", st.constraint("<", "2.0.0.0"))

	st.checkSolverResult(remove(packageB), update(packageA, newPackageA))
}

func TestSolver_SolverAllJobs(t *testing.T) {
	st := newSolverTest(t)
	packageD := st.lockedPkg("d/d", "1.0")
	oldPackageC := st.lockedPkg("c/c", "1.0")

	packageA := st.pkg("a/a", "2.0")
	packageB := st.pkg("b/b", "1.0")
	st.pkg("b/b", "1.1")
	packageC := st.pkg("c/c", "1.1")
	st.pkg("d/d", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint("<", "1.1"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)
	st.require("c/c", nil)

	st.checkSolverResult(remove(packageD), install(packageB), install(packageA), update(oldPackageC, packageC))
}

func TestSolver_SolverThreeAlternativeRequireAndConflict(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "2.0")
	middlePackageB := st.pkg("b/b", "1.0")
	st.pkg("b/b", "1.1")
	st.pkg("b/b", "0.9")
	requires(packageA, newLink("a/a", "b/b", st.constraint("<", "1.1"), pkg.TypeRequire))
	packageA.SetConflicts(pkg.LinksOf(newLink("a/a", "b/b", st.constraint("<", "1.0"), pkg.TypeConflict)))

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(install(middlePackageB), install(packageA))
}

func TestSolver_SolverObsolete(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.lockedPkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	packageB.SetReplaces(pkg.LinksOf(newLink("b/b", "a/a", semver.NewMatchAllConstraint(), pkg.TypeUnknown)))

	st.reposComplete()

	st.require("b/b", nil)

	st.checkSolverResult(remove(packageA), install(packageB))
}

func TestSolver_InstallOneOfTwoAlternatives(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("a/a", "1.0")

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(install(packageA))
}

func TestSolver_InstallProvider(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageQ := st.pkg("q/q", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	packageQ.SetProvides(pkg.LinksOf(newLink("q/q", "b/b", st.constraint("=", "1.0"), pkg.TypeProvide)))

	st.reposComplete()

	st.require("a/a", nil)

	// must explicitly pick the provider, so error in this case
	st.solveFails()
}

func TestSolver_SkipReplacerOfExistingPackage(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageQ := st.pkg("q/q", "1.0")
	packageB := st.pkg("b/b", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	packageQ.SetReplaces(pkg.LinksOf(newLink("q/q", "b/b", st.constraint(">=", "1.0"), pkg.TypeReplace)))

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(install(packageB), install(packageA))
}

func TestSolver_NoInstallReplacerOfMissingPackage(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageQ := st.pkg("q/q", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	packageQ.SetReplaces(pkg.LinksOf(newLink("q/q", "b/b", st.constraint(">=", "1.0"), pkg.TypeReplace)))

	st.reposComplete()

	st.require("a/a", nil)

	st.solveFails()
}

func TestSolver_SkipReplacedPackageIfReplacerIsSelected(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageQ := st.pkg("q/q", "1.0")
	st.pkg("b/b", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	packageQ.SetReplaces(pkg.LinksOf(newLink("q/q", "b/b", st.constraint(">=", "1.0"), pkg.TypeReplace)))

	st.reposComplete()

	st.require("a/a", nil)
	st.require("q/q", nil)

	st.checkSolverResult(install(packageQ), install(packageA))
}

func TestSolver_PickOlderIfNewerConflicts(t *testing.T) {
	st := newSolverTest(t)
	packageX := st.pkg("x/x", "1.0")
	requires(packageX,
		newLink("x/x", "a/a", st.constraint(">=", "2.0.0.0"), pkg.TypeRequire),
		newLink("x/x", "b/b", st.constraint(">=", "2.0.0.0"), pkg.TypeRequire),
	)

	packageA := st.pkg("a/a", "2.0.0")
	newPackageA := st.pkg("a/a", "2.1.0")
	newPackageB := st.pkg("b/b", "2.1.0")

	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "2.0.0.0"), pkg.TypeRequire))

	// new package A depends on version of package B that does not exist
	// => new package A is not installable
	requires(newPackageA, newLink("a/a", "b/b", st.constraint(">=", "2.2.0.0"), pkg.TypeRequire))

	// add a package S replacing both A and B, so that S and B or S and A cannot be simultaneously installed
	// but an alternative option for A and B both exists
	// this creates a more difficult so solve conflict
	packageS := st.pkg("s/s", "2.0.0")
	packageS.SetReplaces(pkg.LinksOf(
		newLink("s/s", "a/a", st.constraint(">=", "2.0.0.0"), pkg.TypeReplace),
		newLink("s/s", "b/b", st.constraint(">=", "2.0.0.0"), pkg.TypeReplace),
	))

	st.reposComplete()

	st.require("x/x", nil)

	st.checkSolverResult(install(newPackageB), install(packageA), install(packageX))
}

func TestSolver_InstallCircularRequire(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("b/b", "0.9")
	packageB2 := st.pkg("b/b", "1.1")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	requires(packageB2, newLink("b/b", "a/a", st.constraint(">=", "1.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)

	st.checkSolverResult(install(packageB2), install(packageA))
}

func TestSolver_InstallAlternativeWithCircularRequire(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	packageC := st.pkg("c/c", "1.0")
	packageD := st.pkg("d/d", "1.0")
	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	requires(packageB, newLink("b/b", "virtual/virtual", st.constraint(">=", "1.0"), pkg.TypeRequire))
	packageC.SetProvides(pkg.LinksOf(newLink("c/c", "virtual/virtual", st.constraint("==", "1.0"), pkg.TypeProvide)))
	packageD.SetProvides(pkg.LinksOf(newLink("d/d", "virtual/virtual", st.constraint("==", "1.0"), pkg.TypeProvide)))

	requires(packageC, newLink("c/c", "a/a", st.constraint("==", "1.0"), pkg.TypeRequire))
	requires(packageD, newLink("d/d", "a/a", st.constraint("==", "1.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)
	st.require("c/c", nil)

	st.checkSolverResult(install(packageB), install(packageA), install(packageC))
}

// If a replacer D replaces B and C with C not otherwise available,
// D must be installed instead of the original B.
func TestSolver_UseReplacerIfNecessary(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("b/b", "1.0")
	packageD := st.pkg("d/d", "1.0")
	packageD2 := st.pkg("d/d", "1.1")

	requires(packageA,
		newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire),
		newLink("a/a", "c/c", st.constraint(">=", "1.0"), pkg.TypeRequire),
	)

	packageD.SetReplaces(pkg.LinksOf(
		newLink("d/d", "b/b", st.constraint(">=", "1.0"), pkg.TypeReplace),
		newLink("d/d", "c/c", st.constraint(">=", "1.0"), pkg.TypeReplace),
	))

	packageD2.SetReplaces(pkg.LinksOf(
		newLink("d/d", "b/b", st.constraint(">=", "1.0"), pkg.TypeReplace),
		newLink("d/d", "c/c", st.constraint(">=", "1.0"), pkg.TypeReplace),
	))

	st.reposComplete()

	st.require("a/a", nil)
	st.require("d/d", nil)

	st.checkSolverResult(install(packageD2), install(packageA))
}

func TestSolver_Issue265(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("a/a", "2.0.999999-dev")
	st.pkg("a/a", "2.1-dev")
	st.pkg("a/a", "2.2-dev")
	packageB1 := st.pkg("b/b", "2.0.10")
	packageB2 := st.pkg("b/b", "2.0.9")
	packageC := st.pkg("c/c", "2.0-dev")
	packageD := st.pkg("d/d", "2.0.9")

	requires(packageC,
		newLink("c/c", "a/a", st.constraint(">=", "2.0"), pkg.TypeRequire),
		newLink("c/c", "d/d", st.constraint(">=", "2.0"), pkg.TypeRequire),
	)

	requires(packageD,
		newLink("d/d", "a/a", st.constraint(">=", "2.1"), pkg.TypeRequire),
		newLink("d/d", "b/b", st.constraint(">=", "2.0-dev"), pkg.TypeRequire),
	)

	requires(packageB1, newLink("b/b", "a/a", st.constraint("==", "2.1.0.0-dev"), pkg.TypeRequire))
	requires(packageB2, newLink("b/b", "a/a", st.constraint("==", "2.1.0.0-dev"), pkg.TypeRequire))

	packageB2.SetReplaces(pkg.LinksOf(newLink("b/b", "d/d", st.constraint("==", "2.0.9.0"), pkg.TypeReplace)))

	st.reposComplete()

	st.require("c/c", st.constraint("==", "2.0.0.0-dev"))

	st.solveFails()
}

func TestSolver_ConflictResultEmpty(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("b/b", "1.0")

	packageA.SetConflicts(pkg.LinksOf(newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeConflict)))

	st.reposComplete()

	st.require("a/a", matchAll("*"))
	st.require("b/b", matchAll("*"))

	e := st.solveFails()
	if len(e.Problems()) != 1 {
		t.Fatalf("%d problems", len(e.Problems()))
	}

	msg := "\n"
	msg += "  Problem 1\n"
	msg += "    - Root composer.json requires a/a * -> satisfiable by a/a[1.0].\n"
	msg += "    - Root composer.json requires b/b * -> satisfiable by b/b[1.0].\n"
	msg += "    - a/a 1.0 conflicts with b/b 1.0.\n"
	if got := st.prettyString(e); got != msg {
		t.Fatalf("got %q\nwant %q", got, msg)
	}
}

func TestSolver_UnsatisfiableRequires(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	st.pkg("b/b", "1.0")

	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "2.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)

	e := st.solveFails()
	if len(e.Problems()) != 1 {
		t.Fatalf("%d problems", len(e.Problems()))
	}
	// TODO assert problem properties

	msg := "\n"
	msg += "  Problem 1\n"
	msg += "    - Root composer.json requires a/a * -> satisfiable by a/a[1.0].\n"
	msg += "    - a/a 1.0 requires b/b >= 2.0 -> found b/b[1.0] but it does not match the constraint.\n"
	if got := st.prettyString(e); got != msg {
		t.Fatalf("got %q\nwant %q", got, msg)
	}
}

func TestSolver_RequireMismatchException(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	st.pkg("b/b", "0.9")
	packageC := st.pkg("c/c", "1.0")
	packageD := st.pkg("d/d", "1.0")

	requires(packageA, newLink("a/a", "b/b", st.constraint(">=", "1.0"), pkg.TypeRequire))
	requires(packageB, newLink("b/b", "c/c", st.constraint(">=", "1.0"), pkg.TypeRequire))
	requires(packageC, newLink("c/c", "d/d", st.constraint(">=", "1.0"), pkg.TypeRequire))
	requires(packageD, newLink("d/d", "b/b", st.constraint("<", "1.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", matchAll("*"))

	e := st.solveFails()
	if len(e.Problems()) != 1 {
		t.Fatalf("%d problems", len(e.Problems()))
	}

	msg := "\n"
	msg += "  Problem 1\n"
	msg += "    - Root composer.json requires a/a * -> satisfiable by a/a[1.0].\n"
	msg += "    - a/a 1.0 requires b/b >= 1.0 -> satisfiable by b/b[1.0].\n"
	msg += "    - b/b 1.0 requires c/c >= 1.0 -> satisfiable by c/c[1.0].\n"
	msg += "    - c/c 1.0 requires d/d >= 1.0 -> satisfiable by d/d[1.0].\n"
	msg += "    - d/d 1.0 requires b/b < 1.0 -> satisfiable by b/b[0.9].\n"
	msg += "    - You can only install one version of a package, so only one of these can be installed: b/b[0.9, 1.0].\n"
	if got := st.prettyString(e); got != msg {
		t.Fatalf("got %q\nwant %q", got, msg)
	}
}

func TestSolver_LearnLiteralsWithSortedRuleLiterals(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("twig/twig", "2.0")
	packageTwig16 := st.pkg("twig/twig", "1.6")
	st.pkg("twig/twig", "1.5")
	packageSymfony := st.pkg("symfony/symfony", "2.0")
	packageTwigBridge := st.pkg("symfony/twig-bridge", "2.0")

	requires(packageTwigBridge, newLink("symfony/twig-bridge", "twig/twig", st.constraint("<", "2.0"), pkg.TypeRequire))

	packageSymfony.SetReplaces(pkg.LinksOf(newLink("symfony/symfony", "symfony/twig-bridge", st.constraint("==", "2.0"), pkg.TypeReplace)))

	st.reposComplete()

	st.require("symfony/twig-bridge", nil)
	st.require("twig/twig", nil)

	st.checkSolverResult(install(packageTwig16), install(packageTwigBridge))
}

func TestSolver_InstallRecursiveAliasDependencies(t *testing.T) {
	st := newSolverTest(t)
	st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "2.0")
	packageA2 := st.pkg("a/a", "2.0")

	requires(packageA2, pkg.NewLink("a/a", "b/b", st.constraint("==", "2.0"), pkg.TypeRequire, pkg.Str("== 2.0")))
	requires(packageB, newLink("b/b", "a/a", st.constraint(">=", "2.0"), pkg.TypeRequire))

	packageA2Alias := st.add(getAliasPackage(t, packageA2, "1.1"))

	st.reposComplete()

	st.require("a/a", st.constraint("==", "1.1.0.0"))

	st.checkSolverResult(install(packageB), install(packageA2), markAliasInstalled(packageA2Alias))
}

func TestSolver_InstallDevAlias(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "2.0")
	packageB := st.pkg("b/b", "1.0")

	requires(packageB, newLink("b/b", "a/a", st.constraint("<", "2.0"), pkg.TypeRequire))

	packageAAlias := st.add(getAliasPackage(t, packageA, "1.1"))

	st.reposComplete()

	st.require("a/a", st.constraint("==", "2.0"))
	st.require("b/b", nil)

	st.checkSolverResult(install(packageA), markAliasInstalled(packageAAlias), install(packageB))
}

func TestSolver_InstallRootAliasesIfAliasOfIsInstalled(t *testing.T) {
	st := newSolverTest(t)
	// root aliased, required
	packageA := st.pkg("a/a", "1.0")
	packageAAlias := getAliasPackage(t, packageA, "1.1")
	st.add(packageAAlias)
	packageAAlias.SetRootPackageAlias(true)
	// root aliased, not required, should still be installed as it is root alias
	packageB := st.pkg("b/b", "1.0")
	packageBAlias := getAliasPackage(t, packageB, "1.1")
	st.add(packageBAlias)
	packageBAlias.SetRootPackageAlias(true)
	// regular alias, not required, alias should not be installed
	packageC := st.pkg("c/c", "1.0")
	packageCAlias := st.add(getAliasPackage(t, packageC, "1.1"))

	st.reposComplete()

	st.require("a/a", st.constraint("==", "1.1"))
	st.require("b/b", st.constraint("==", "1.0"))
	st.require("c/c", st.constraint("==", "1.0"))

	st.checkSolverResult(
		install(packageA),
		markAliasInstalled(packageAAlias),
		install(packageB),
		markAliasInstalled(packageBAlias),
		install(packageC),
		markAliasInstalled(packageCAlias),
	)
}

// Tests for a bug introduced in commit 451bab1c2cd58e05af6e21639b829408ad023463 Solver.php line 554/523
//
// Every package and link in this test matters, only a combination this complex will run into the situation in which
// a negatively decided literal will need to be learned inverted as a positive assertion.
//
// In particular in this case the goal is to first have the solver decide X 2.0 should not be installed to later
// decide to learn that X 2.0 must be installed and revert decisions to retry solving with this new assumption.
func TestSolver_LearnPositiveLiteral(t *testing.T) {
	st := newSolverTest(t)
	packageA := st.pkg("a/a", "1.0")
	packageB := st.pkg("b/b", "1.0")
	packageC1 := st.pkg("c/c", "1.0")
	packageC2 := st.pkg("c/c", "2.0")
	packageD := st.pkg("d/d", "1.0")
	packageE := st.pkg("e/e", "1.0")
	packageF1 := st.pkg("f/f", "1.0")
	st.pkg("f/f", "2.0")
	st.pkg("g/g", "1.0")
	packageG2 := st.pkg("g/g", "2.0")
	st.pkg("g/g", "3.0")

	requires(packageA,
		newLink("a/a", "b/b", st.constraint("==", "1.0"), pkg.TypeRequire),
		newLink("a/a", "c/c", st.constraint(">=", "1.0"), pkg.TypeRequire),
		newLink("a/a", "d/d", st.constraint("==", "1.0"), pkg.TypeRequire),
	)

	requires(packageB, newLink("b/b", "e/e", st.constraint("==", "1.0"), pkg.TypeRequire))

	requires(packageC1, newLink("c/c", "f/f", st.constraint("==", "1.0"), pkg.TypeRequire))
	requires(packageC2,
		newLink("c/c", "f/f", st.constraint("==", "1.0"), pkg.TypeRequire),
		newLink("c/c", "g/g", st.constraint(">=", "1.0"), pkg.TypeRequire),
	)

	requires(packageD, newLink("d/d", "f/f", st.constraint(">=", "1.0"), pkg.TypeRequire))

	requires(packageE, newLink("e/e", "g/g", st.constraint("<=", "2.0"), pkg.TypeRequire))

	st.reposComplete()

	st.require("a/a", nil)

	st.createSolver()

	// check correct setup for assertion later
	if st.solver.TestFlagLearnedPositiveLiteral {
		t.Fatal("flag set before solving")
	}

	st.createSolver()
	transaction, err := st.solver.Solve(st.request, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOperations(t, []opResult{
		install(packageF1),
		install(packageD),
		install(packageG2),
		install(packageC2),
		install(packageE),
		install(packageB),
		install(packageA),
	}, operationResults(t, transaction.Operations(), "remove"))

	// verify that the code path leading to a negative literal resulting in a positive learned literal is actually
	// executed
	if !st.solver.TestFlagLearnedPositiveLiteral {
		t.Fatal("the learned positive literal path was not taken")
	}
}

func TestSolver_SolveSurfacesFilterListRemovedLockedPackagesAsProblems(t *testing.T) {
	st := newSolverTest(t)
	p := getPackage(t, "vendor/malware", "1.0.0")
	versions := &repository.NameMap[[]*repository.FilterListEntry]{}
	versions.Set("1.0.0.0", []*repository.FilterListEntry{{PackageName: "vendor/malware", Constraint: semver.NewMatchAllConstraint(), ListName: "malware"}})
	removed := &repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]{}
	removed.Set("vendor/malware", versions)
	pool := NewPool(nil, nil, &Removed{FilterList: removed})

	request := NewRequest(st.repoLocked)
	request.FixPackage(p)
	request.LockPackage(p)

	solver := NewSolver(st.policy, pool, nullIO)
	_, err := solver.Solve(request, nil)
	e, ok := errors.AsType[*SolverProblemsError](err)
	if !ok {
		t.Fatalf("Expected SolverProblemsException for filter-list-removed package, got %v", err)
	}
	if len(e.Problems()) != 1 {
		t.Fatalf("%d problems", len(e.Problems()))
	}
	reasons := e.Problems()[0].Reasons()
	if len(reasons) == 0 || len(reasons[0]) == 0 {
		t.Fatal("no reasons")
	}
	rule := reasons[0][0]
	if rule.Reason() != RuleLockedFilterListRemoved || rule.Package() != pkg.PackageInterface(p) {
		t.Fatalf("rule %d %v", rule.Reason(), rule.ReasonData())
	}
}
